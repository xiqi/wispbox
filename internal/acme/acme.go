// Package acme issues TLS certificates. Two adapters exist:
//
//   - SelfSignedIssuer: development mode and the fallback default cert.
//   - LetsEncryptIssuer: production HTTP-01 issuance via x/crypto/acme,
//     supporting both the production and staging Let's Encrypt endpoints.
package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

// Issued is the result of a successful issuance.
type Issued struct {
	CertPEM   []byte // full chain
	KeyPEM    []byte
	NotBefore time.Time
	NotAfter  time.Time
}

// Issuer obtains a certificate for one hostname.
type Issuer interface {
	Issue(ctx context.Context, hostname string) (*Issued, error)
	// Kind reports the challenge type recorded in the database.
	Kind() string
}

// ---- HTTP-01 challenge plumbing ----

// HTTP01Solver stores active challenge tokens. The wispboxd HTTP (port 80)
// server consults it for /.well-known/acme-challenge/ requests.
type HTTP01Solver struct {
	mu     sync.Mutex
	tokens map[string]string // token -> keyAuth
}

func NewHTTP01Solver() *HTTP01Solver { return &HTTP01Solver{tokens: map[string]string{}} }

func (s *HTTP01Solver) Present(token, keyAuth string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = keyAuth
}

func (s *HTTP01Solver) Cleanup(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}

// Lookup returns the key authorization for a token, if one is active.
func (s *HTTP01Solver) Lookup(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ka, ok := s.tokens[token]
	return ka, ok
}

// ---- self-signed (development / fallback) ----

// SelfSignedIssuer mints short-lived local certificates. Used in development
// mode and for the default fallback certificate served to unknown SNI.
type SelfSignedIssuer struct {
	Validity time.Duration // default 90 days
}

func NewSelfSignedIssuer() *SelfSignedIssuer { return &SelfSignedIssuer{Validity: 90 * 24 * time.Hour} }

func (s *SelfSignedIssuer) Kind() string { return "self-signed" }

func (s *SelfSignedIssuer) Issue(_ context.Context, hostname string) (*Issued, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	validity := s.Validity
	if validity == 0 {
		validity = 90 * 24 * time.Hour
	}
	notBefore := time.Now().Add(-5 * time.Minute)
	notAfter := notBefore.Add(validity)
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hostname, Organization: []string{"wispbox self-signed"}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &Issued{
		CertPEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		NotBefore: notBefore,
		NotAfter:  notAfter,
	}, nil
}

// ---- Let's Encrypt (production) ----

// LetsEncryptIssuer performs real ACME HTTP-01 issuance.
type LetsEncryptIssuer struct {
	DirectoryURL   string
	EmailFunc      func() string // read lazily: the contact is set during setup
	AccountKeyPath string        // persisted so the same account is reused
	Solver         *HTTP01Solver

	mu     sync.Mutex
	client *acme.Client
}

// NewLetsEncryptIssuer builds the production issuer. emailFunc is evaluated
// at account-registration time (which happens on the first issuance, after
// first-run setup has collected the contact address) rather than at startup,
// so a fresh install registers with the address the admin entered.
func NewLetsEncryptIssuer(directoryURL string, emailFunc func() string, accountKeyPath string, solver *HTTP01Solver) *LetsEncryptIssuer {
	return &LetsEncryptIssuer{
		DirectoryURL:   directoryURL,
		EmailFunc:      emailFunc,
		AccountKeyPath: accountKeyPath,
		Solver:         solver,
	}
}

func (l *LetsEncryptIssuer) Kind() string { return "http-01" }

func (l *LetsEncryptIssuer) ensureClient(ctx context.Context) (*acme.Client, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.client != nil {
		return l.client, nil
	}
	key, err := loadOrCreateAccountKey(l.AccountKeyPath)
	if err != nil {
		return nil, fmt.Errorf("acme account key: %w", err)
	}
	client := &acme.Client{Key: key, DirectoryURL: l.DirectoryURL, UserAgent: "wispbox"}
	account := &acme.Account{}
	email := ""
	if l.EmailFunc != nil {
		email = l.EmailFunc()
	}
	if email != "" {
		account.Contact = []string{"mailto:" + email}
	}
	_, err = client.Register(ctx, account, acme.AcceptTOS)
	if err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("acme account registration: %w", err)
	}
	l.client = client
	return client, nil
}

func (l *LetsEncryptIssuer) Issue(ctx context.Context, hostname string) (*Issued, error) {
	client, err := l.ensureClient(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(hostname))
	if err != nil {
		return nil, fmt.Errorf("acme order: %w", err)
	}

	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return nil, fmt.Errorf("acme authorization: %w", err)
		}
		if authz.Status == acme.StatusValid {
			continue
		}
		var challenge *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "http-01" {
				challenge = c
				break
			}
		}
		if challenge == nil {
			return nil, fmt.Errorf("acme: no http-01 challenge offered for %s", hostname)
		}
		keyAuth, err := client.HTTP01ChallengeResponse(challenge.Token)
		if err != nil {
			return nil, err
		}
		l.Solver.Present(challenge.Token, keyAuth)
		defer l.Solver.Cleanup(challenge.Token)

		if _, err := client.Accept(ctx, challenge); err != nil {
			return nil, fmt.Errorf("acme accept challenge: %w", err)
		}
		if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
			return nil, fmt.Errorf("acme validation failed for %s: %w", hostname, err)
		}
	}

	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: hostname},
		DNSNames: []string{hostname},
	}, certKey)
	if err != nil {
		return nil, err
	}
	chainDER, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("acme finalize: %w", err)
	}

	var certPEM []byte
	for _, der := range chainDER {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	leaf, err := x509.ParseCertificate(chainDER[0])
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(certKey)
	if err != nil {
		return nil, err
	}
	return &Issued{
		CertPEM:   certPEM,
		KeyPEM:    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		NotBefore: leaf.NotBefore,
		NotAfter:  leaf.NotAfter,
	}, nil
}

func loadOrCreateAccountKey(path string) (*ecdsa.PrivateKey, error) {
	if raw, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("corrupt account key at %s", path)
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}
