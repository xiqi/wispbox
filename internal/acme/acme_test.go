package acme

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"
)

func TestSelfSignedIssuerIssue(t *testing.T) {
	tests := []struct {
		name         string
		issuer       *SelfSignedIssuer
		hostname     string
		wantValidity time.Duration
	}{
		{
			name:         "default validity",
			issuer:       NewSelfSignedIssuer(),
			hostname:     "mail.example.com",
			wantValidity: 90 * 24 * time.Hour,
		},
		{
			name:         "zero validity falls back to 90 days",
			issuer:       &SelfSignedIssuer{},
			hostname:     "mx.example.org",
			wantValidity: 90 * 24 * time.Hour,
		},
		{
			name:         "custom validity",
			issuer:       &SelfSignedIssuer{Validity: 48 * time.Hour},
			hostname:     "box.example.net",
			wantValidity: 48 * time.Hour,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issued, err := tt.issuer.Issue(context.Background(), tt.hostname)
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}

			// The pair must be loadable as a TLS key pair.
			if _, err := tls.X509KeyPair(issued.CertPEM, issued.KeyPEM); err != nil {
				t.Fatalf("X509KeyPair: %v", err)
			}

			block, rest := pem.Decode(issued.CertPEM)
			if block == nil || block.Type != "CERTIFICATE" {
				t.Fatalf("CertPEM did not decode to a CERTIFICATE block")
			}
			if len(rest) != 0 {
				t.Fatalf("CertPEM has %d trailing bytes", len(rest))
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Fatalf("ParseCertificate: %v", err)
			}
			if err := cert.VerifyHostname(tt.hostname); err != nil {
				t.Errorf("certificate not valid for %s: %v", tt.hostname, err)
			}
			if cert.Subject.CommonName != tt.hostname {
				t.Errorf("CommonName = %q, want %q", cert.Subject.CommonName, tt.hostname)
			}

			keyBlock, _ := pem.Decode(issued.KeyPEM)
			if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" {
				t.Fatalf("KeyPEM did not decode to a PRIVATE KEY block")
			}
			if _, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err != nil {
				t.Fatalf("ParsePKCS8PrivateKey: %v", err)
			}

			// Validity window: NotBefore is backdated a little; the cert must
			// already be valid and expire after the requested lifetime.
			now := time.Now()
			if issued.NotBefore.After(now) {
				t.Errorf("NotBefore %v is in the future", issued.NotBefore)
			}
			if got := issued.NotAfter.Sub(issued.NotBefore); got != tt.wantValidity {
				t.Errorf("validity = %v, want %v", got, tt.wantValidity)
			}
			if !cert.NotAfter.Equal(issued.NotAfter.Truncate(time.Second)) {
				t.Errorf("cert NotAfter = %v, Issued.NotAfter = %v", cert.NotAfter, issued.NotAfter)
			}
		})
	}
}

func TestSelfSignedIssuerKind(t *testing.T) {
	if got := NewSelfSignedIssuer().Kind(); got != "self-signed" {
		t.Errorf("Kind() = %q, want %q", got, "self-signed")
	}
}

func TestHTTP01Solver(t *testing.T) {
	s := NewHTTP01Solver()

	if _, ok := s.Lookup("nope"); ok {
		t.Fatalf("Lookup on empty solver returned ok")
	}

	s.Present("tok-a", "tok-a.keyauth")
	s.Present("tok-b", "tok-b.keyauth")

	if ka, ok := s.Lookup("tok-a"); !ok || ka != "tok-a.keyauth" {
		t.Errorf("Lookup(tok-a) = %q, %v; want %q, true", ka, ok, "tok-a.keyauth")
	}
	if ka, ok := s.Lookup("tok-b"); !ok || ka != "tok-b.keyauth" {
		t.Errorf("Lookup(tok-b) = %q, %v; want %q, true", ka, ok, "tok-b.keyauth")
	}

	// Presenting the same token again overwrites the key authorization.
	s.Present("tok-a", "tok-a.rotated")
	if ka, _ := s.Lookup("tok-a"); ka != "tok-a.rotated" {
		t.Errorf("Lookup(tok-a) after re-Present = %q, want %q", ka, "tok-a.rotated")
	}

	s.Cleanup("tok-a")
	if _, ok := s.Lookup("tok-a"); ok {
		t.Errorf("Lookup(tok-a) after Cleanup returned ok")
	}
	if _, ok := s.Lookup("tok-b"); !ok {
		t.Errorf("Cleanup(tok-a) removed tok-b too")
	}

	// Cleaning up an unknown token is a no-op.
	s.Cleanup("never-presented")
}
