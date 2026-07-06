// Package certs owns the certificate lifecycle: per-hostname issuance,
// storage, renewal with backoff, SNI selection, and the fallback default
// certificate. State lives in the certificates table; PEM files live under
// the certificate directory:
//
//	/var/lib/wispbox/certs/<hostname>/fullchain.pem
//	/var/lib/wispbox/certs/<hostname>/privkey.pem
package certs

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiqi/wispbox/internal/acme"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/dnscheck"
	"github.com/xiqi/wispbox/internal/services"
)

// renewBefore is how long before expiry a certificate becomes due.
const renewBefore = 30 * 24 * time.Hour

// maxBackoff caps the exponential retry delay after failed issuance.
const maxBackoff = 24 * time.Hour

// Manager drives the certificate state machine.
type Manager struct {
	CertDir  string
	Store    *db.Store
	Issuer   acme.Issuer
	Checker  *dnscheck.Checker
	Services services.Manager
	Log      *slog.Logger

	// ServerIPs returns the public IPs this server answers on, for the DNS
	// preflight. Wired to the settings table.
	ServerIPs func(ctx context.Context) []string

	// OnIssued, if set, runs after a certificate becomes active. It is used
	// to regenerate Postfix/Dovecot config so the new cert appears in the
	// SNI maps and local_name blocks (a bare service reload is not enough —
	// the generated maps only reference certs that are active on disk). The
	// generator reloads the services itself.
	OnIssued func(ctx context.Context) error

	// issuing serializes issuance per hostname so two concurrent IssueNow
	// calls can never write a mismatched cert/key pair.
	issuing sync.Map // hostname -> *sync.Mutex

	mu    sync.Mutex
	cache map[string]*tls.Certificate // hostname -> loaded cert
	// failures counts consecutive issuance failures per hostname to grow the
	// retry backoff. It is in-memory and best-effort per process: a restart
	// resets it to zero, so backoff starts over (the persisted renew_after
	// still gates the next attempt). That is fine for a solo-dev box; we
	// deliberately don't add a DB column just to preserve the escalation.
	failures map[string]int
	fallback *tls.Certificate
}

func NewManager(certDir string, store *db.Store, issuer acme.Issuer, checker *dnscheck.Checker, svc services.Manager, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		CertDir:   certDir,
		Store:     store,
		Issuer:    issuer,
		Checker:   checker,
		Services:  svc,
		Log:       log,
		ServerIPs: func(context.Context) []string { return nil },
		cache:     map[string]*tls.Certificate{},
		failures:  map[string]int{},
	}
}

// Paths returns where a hostname's PEM files live.
func (m *Manager) Paths(hostname string) (certPath, keyPath string) {
	dir := filepath.Join(m.CertDir, strings.ToLower(hostname))
	return filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
}

// EnsureTracked makes sure a certificate row exists for the hostname.
func (m *Manager) EnsureTracked(ctx context.Context, domainID int64, hostname string) (*db.Certificate, error) {
	if c, err := m.Store.GetCertificateByHostname(ctx, hostname); err == nil {
		return c, nil
	}
	return m.Store.CreateCertificate(ctx, domainID, hostname)
}

// EnsureFallback creates (or loads) the default self-signed certificate
// served for unknown SNI and before any real certificate exists.
func (m *Manager) EnsureFallback(ctx context.Context, hostname string) error {
	if hostname == "" {
		hostname = "wispbox.invalid"
	}
	certPath := filepath.Join(m.CertDir, "_default", "fullchain.pem")
	keyPath := filepath.Join(m.CertDir, "_default", "privkey.pem")
	if pair, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		m.mu.Lock()
		m.fallback = &pair
		m.mu.Unlock()
		return nil
	}
	issued, err := acme.NewSelfSignedIssuer().Issue(ctx, hostname)
	if err != nil {
		return fmt.Errorf("generate fallback certificate: %w", err)
	}
	if err := writePEMPair(certPath, keyPath, issued.CertPEM, issued.KeyPEM); err != nil {
		return err
	}
	pair, err := tls.X509KeyPair(issued.CertPEM, issued.KeyPEM)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.fallback = &pair
	m.mu.Unlock()
	return nil
}

// GetCertificate implements tls.Config.GetCertificate with SNI selection.
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := strings.ToLower(strings.TrimSuffix(hello.ServerName, "."))
	m.mu.Lock()
	if c, ok := m.cache[host]; ok {
		m.mu.Unlock()
		return c, nil
	}
	fallback := m.fallback
	m.mu.Unlock()

	if host != "" {
		certPath, keyPath := m.Paths(host)
		if pair, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
			m.mu.Lock()
			m.cache[host] = &pair
			m.mu.Unlock()
			return &pair, nil
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("no certificate available for %q", host)
}

// TLSConfig returns the HTTPS server TLS configuration.
func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: m.GetCertificate,
	}
}

// RenewDue walks certificates whose renew_after has passed and tries to
// issue or renew each one. Failures never remove a working certificate.
func (m *Manager) RenewDue(ctx context.Context) {
	due, err := m.Store.CertificatesDueForRenewal(ctx)
	if err != nil {
		m.Log.Error("certificate renewal scan failed", "error", err)
		return
	}
	for _, cert := range due {
		if err := m.IssueNow(ctx, cert.ID); err != nil {
			m.Log.Warn("certificate issuance failed", "hostname", cert.Hostname, "error", err)
		}
	}
}

// IssueNow runs the full state machine for one certificate:
// preflight DNS -> issuing -> write files -> active -> reload services.
// On failure the status becomes error, renew_after is pushed back with
// exponential backoff, and any previously issued files stay in place.
func (m *Manager) IssueNow(ctx context.Context, certID int64) error {
	cert, err := m.Store.GetCertificate(ctx, certID)
	if err != nil {
		return err
	}

	// Serialize issuance per hostname: two concurrent issuances (e.g. the
	// renewal loop racing an admin "renew" click) must not interleave their
	// cert/key writes.
	lockAny, _ := m.issuing.LoadOrStore(cert.Hostname, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	fail := func(status db.CertStatus, cause error) error {
		m.mu.Lock()
		m.failures[cert.Hostname]++
		n := m.failures[cert.Hostname]
		m.mu.Unlock()
		backoff := time.Duration(1<<min(n-1, 5)) * time.Hour // 1h,2h,4h,8h,16h,32h capped
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
		_ = m.Store.UpdateCertificateStatus(ctx, cert.ID, status, humanCertError(cause))
		_ = m.Store.SetCertificateRenewAfter(ctx, cert.ID, time.Now().Add(backoff))
		_ = m.Store.AppendServiceEvent(ctx, db.ServiceEvent{
			Service: "wispboxd", EventType: "cert_renew", Status: "error",
			Message: cert.Hostname + ": " + humanCertError(cause),
		})
		return cause
	}

	// Preflight: never contact the CA while DNS points elsewhere.
	if m.Issuer.Kind() == "http-01" {
		if err := m.Checker.PreflightHostname(ctx, cert.Hostname, m.ServerIPs(ctx)); err != nil {
			return fail(db.CertDNSWait, err)
		}
	}

	if err := m.Store.UpdateCertificateStatus(ctx, cert.ID, db.CertIssuing, ""); err != nil {
		return err
	}

	issued, err := m.Issuer.Issue(ctx, cert.Hostname)
	if err != nil {
		return fail(db.CertError, err)
	}

	certPath, keyPath := m.Paths(cert.Hostname)
	if err := writePEMPair(certPath, keyPath, issued.CertPEM, issued.KeyPEM); err != nil {
		return fail(db.CertError, err)
	}

	renewAfter := issued.NotAfter.Add(-renewBefore)
	if renewAfter.Before(time.Now()) {
		// Short-lived (e.g. self-signed dev) certs: renew at 2/3 lifetime.
		renewAfter = issued.NotBefore.Add(issued.NotAfter.Sub(issued.NotBefore) * 2 / 3)
	}
	if err := m.Store.MarkCertificateIssued(ctx, cert.ID, m.Issuer.Kind(), certPath, keyPath,
		issued.NotBefore, issued.NotAfter, renewAfter); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.failures, cert.Hostname)
	delete(m.cache, cert.Hostname) // next handshake reloads from disk
	m.mu.Unlock()

	_ = m.Store.AppendServiceEvent(ctx, db.ServiceEvent{
		Service: "wispboxd", EventType: "cert_renew", Status: "ok",
		Message: cert.Hostname + " issued, valid until " + issued.NotAfter.UTC().Format("2006-01-02"),
	})

	// The certificate is now active on disk, so it must appear in the Postfix
	// SNI map and Dovecot local_name blocks. Regenerating the config picks it
	// up and reloads both services; if no regenerator is wired (e.g. unit
	// tests), fall back to a plain reload so the fullchain change still lands.
	if m.OnIssued != nil {
		if err := m.OnIssued(ctx); err != nil {
			m.Log.Warn("config regeneration after cert issuance failed", "hostname", cert.Hostname, "error", err)
		}
	} else {
		for _, svc := range []string{"postfix", "dovecot"} {
			if err := m.Services.Reload(ctx, svc); err != nil {
				m.Log.Warn("service reload after cert renewal failed", "service", svc, "error", err)
			}
		}
	}
	m.Log.Info("certificate issued", "hostname", cert.Hostname, "not_after", issued.NotAfter)
	return nil
}

// RunRenewalLoop periodically renews due certificates until ctx is done.
func (m *Manager) RunRenewalLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	m.RenewDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.RenewDue(ctx)
		}
	}
}

// humanCertError converts ACME/DNS errors into messages an admin can act on.
func humanCertError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "rateLimited"):
		return "Let's Encrypt rate limit reached. wispbox will retry automatically; no action needed unless this persists for a day."
	case strings.Contains(s, "urn:ietf:params:acme:error:unauthorized"):
		return "The certificate authority could not reach this server on port 80. Check that port 80 is open and not used by another service. (" + s + ")"
	case strings.Contains(s, "does not resolve"), strings.Contains(s, "fix the A/AAAA record"):
		return s
	}
	if len(s) > 500 {
		s = s[:500] + "…"
	}
	return s
}

// writePEMPair writes cert+key atomically with restrictive permissions.
func writePEMPair(certPath, keyPath string, certPEM, keyPEM []byte) error {
	dir := filepath.Dir(certPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := atomicWrite(certPath, certPEM, 0o640); err != nil {
		return err
	}
	return atomicWrite(keyPath, keyPEM, 0o640)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
