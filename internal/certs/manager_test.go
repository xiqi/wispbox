package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xiqi/wispbox/internal/acme"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/dnscheck"
	"github.com/xiqi/wispbox/internal/services"
)

// stubIssuer lets tests force a challenge kind and issuance failures.
type stubIssuer struct {
	kind  string
	err   error
	calls int
}

func (s *stubIssuer) Issue(context.Context, string) (*acme.Issued, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return nil, errors.New("stubIssuer: no result configured")
}

func (s *stubIssuer) Kind() string { return s.kind }

type testEnv struct {
	mgr      *Manager
	store    *db.Store
	mock     *services.MockManager
	resolver *dnscheck.MockResolver
	domainID int64
}

func newTestEnv(t *testing.T, issuer acme.Issuer) *testEnv {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	sqldb, err := db.Open(filepath.Join(dir, "control.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqldb.Close() })
	if _, err := db.Migrate(ctx, sqldb); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	store := db.NewStore(sqldb)

	domain, err := store.CreateDomain(ctx, "example.com", "mail.example.com")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}

	resolver := dnscheck.NewMockResolver()
	mock := services.NewMockManager(store)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewManager(filepath.Join(dir, "certs"), store, issuer, dnscheck.NewChecker(resolver), mock, log)
	return &testEnv{mgr: mgr, store: store, mock: mock, resolver: resolver, domainID: domain.ID}
}

func TestEnsureTracked(t *testing.T) {
	env := newTestEnv(t, acme.NewSelfSignedIssuer())
	ctx := context.Background()

	first, err := env.mgr.EnsureTracked(ctx, env.domainID, "Mail.Example.com")
	if err != nil {
		t.Fatalf("EnsureTracked: %v", err)
	}
	if first.Hostname != "mail.example.com" {
		t.Errorf("Hostname = %q, want %q", first.Hostname, "mail.example.com")
	}
	if first.Status != db.CertPending {
		t.Errorf("Status = %q, want %q", first.Status, db.CertPending)
	}

	second, err := env.mgr.EnsureTracked(ctx, env.domainID, "mail.example.com")
	if err != nil {
		t.Fatalf("EnsureTracked (second call): %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("second call created a new row: id %d != %d", second.ID, first.ID)
	}

	certs, err := env.store.ListCertificates(ctx)
	if err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if len(certs) != 1 {
		t.Errorf("ListCertificates returned %d rows, want 1", len(certs))
	}
}

func TestIssueNowSelfSigned(t *testing.T) {
	env := newTestEnv(t, acme.NewSelfSignedIssuer())
	ctx := context.Background()

	cert, err := env.mgr.EnsureTracked(ctx, env.domainID, "mail.example.com")
	if err != nil {
		t.Fatalf("EnsureTracked: %v", err)
	}
	if err := env.mgr.IssueNow(ctx, cert.ID); err != nil {
		t.Fatalf("IssueNow: %v", err)
	}

	got, err := env.store.GetCertificate(ctx, cert.ID)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if got.Status != db.CertActive {
		t.Errorf("Status = %q, want %q", got.Status, db.CertActive)
	}
	if got.ChallengeType != "self-signed" {
		t.Errorf("ChallengeType = %q, want %q", got.ChallengeType, "self-signed")
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want empty", got.LastError)
	}

	certPath, keyPath := env.mgr.Paths("mail.example.com")
	if got.CertPath != certPath || got.KeyPath != keyPath {
		t.Errorf("paths in row = (%q, %q), want (%q, %q)", got.CertPath, got.KeyPath, certPath, keyPath)
	}
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		t.Errorf("LoadX509KeyPair on written files: %v", err)
	}

	notAfter, ok := got.NotAfterTime()
	if !ok {
		t.Fatalf("not_after not set: %q", got.NotAfter)
	}
	if !notAfter.After(time.Now()) {
		t.Errorf("not_after %v is not in the future", notAfter)
	}
	renewAfter, ok := got.RenewAfterTime()
	if !ok {
		t.Fatalf("renew_after not set: %q", got.RenewAfter)
	}
	if !renewAfter.After(time.Now()) {
		t.Errorf("renew_after %v is not in the future", renewAfter)
	}
	if !renewAfter.Before(notAfter) {
		t.Errorf("renew_after %v is not before not_after %v", renewAfter, notAfter)
	}

	for _, want := range []string{"reload postfix", "reload dovecot"} {
		if !slices.Contains(env.mock.Actions, want) {
			t.Errorf("mock actions %v missing %q", env.mock.Actions, want)
		}
	}
}

func TestIssueNowDNSPreflightFailure(t *testing.T) {
	env := newTestEnv(t, acme.NewSelfSignedIssuer())
	ctx := context.Background()

	cert, err := env.mgr.EnsureTracked(ctx, env.domainID, "mail.example.com")
	if err != nil {
		t.Fatalf("EnsureTracked: %v", err)
	}
	// First issuance succeeds (self-signed skips the preflight) and leaves
	// PEM files on disk.
	if err := env.mgr.IssueNow(ctx, cert.ID); err != nil {
		t.Fatalf("IssueNow (self-signed): %v", err)
	}
	certPath, keyPath := env.mgr.Paths("mail.example.com")
	certBefore, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert file: %v", err)
	}
	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}

	// Switch to an http-01 issuer: the DNS preflight now runs, and the mock
	// resolver has no A record for the hostname.
	stub := &stubIssuer{kind: "http-01"}
	env.mgr.Issuer = stub
	env.mgr.ServerIPs = func(context.Context) []string { return []string{"192.0.2.10"} }

	start := time.Now()
	if err := env.mgr.IssueNow(ctx, cert.ID); err == nil {
		t.Fatalf("IssueNow succeeded despite unresolvable hostname")
	}
	if stub.calls != 0 {
		t.Errorf("issuer was called %d times; preflight should run first", stub.calls)
	}

	got, err := env.store.GetCertificate(ctx, cert.ID)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if got.Status != db.CertDNSWait {
		t.Errorf("Status = %q, want %q", got.Status, db.CertDNSWait)
	}
	if !strings.Contains(got.LastError, "does not resolve") {
		t.Errorf("LastError = %q, want a does-not-resolve message", got.LastError)
	}
	renewAfter, ok := got.RenewAfterTime()
	if !ok {
		t.Fatalf("renew_after not set: %q", got.RenewAfter)
	}
	if !renewAfter.After(start) {
		t.Errorf("renew_after %v not pushed into the future", renewAfter)
	}
	if renewAfter.After(start.Add(2 * time.Hour)) {
		t.Errorf("renew_after %v further out than the first backoff step", renewAfter)
	}

	// The previously issued files must be untouched.
	certAfter, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert file after failure: %v", err)
	}
	keyAfter, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key file after failure: %v", err)
	}
	if string(certAfter) != string(certBefore) {
		t.Errorf("cert file changed after failed issuance")
	}
	if string(keyAfter) != string(keyBefore) {
		t.Errorf("key file changed after failed issuance")
	}
}

func TestIssueNowIssuerErrorBackoff(t *testing.T) {
	stub := &stubIssuer{kind: "self-signed", err: errors.New("simulated CA outage")}
	env := newTestEnv(t, stub)
	ctx := context.Background()

	cert, err := env.mgr.EnsureTracked(ctx, env.domainID, "mail.example.com")
	if err != nil {
		t.Fatalf("EnsureTracked: %v", err)
	}

	start := time.Now()
	if err := env.mgr.IssueNow(ctx, cert.ID); err == nil {
		t.Fatalf("IssueNow succeeded with failing issuer")
	}
	got, err := env.store.GetCertificate(ctx, cert.ID)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if got.Status != db.CertError {
		t.Errorf("Status = %q, want %q", got.Status, db.CertError)
	}
	if !strings.Contains(got.LastError, "simulated CA outage") {
		t.Errorf("LastError = %q, want issuer error", got.LastError)
	}
	firstRenew, ok := got.RenewAfterTime()
	if !ok {
		t.Fatalf("renew_after not set after first failure: %q", got.RenewAfter)
	}
	// First failure backs off by 1h.
	if firstRenew.Before(start.Add(50*time.Minute)) || firstRenew.After(start.Add(70*time.Minute)) {
		t.Errorf("first backoff renew_after = %v, want ~1h after %v", firstRenew, start)
	}

	// A consecutive failure doubles the backoff.
	if err := env.mgr.IssueNow(ctx, cert.ID); err == nil {
		t.Fatalf("second IssueNow succeeded with failing issuer")
	}
	got, err = env.store.GetCertificate(ctx, cert.ID)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	secondRenew, ok := got.RenewAfterTime()
	if !ok {
		t.Fatalf("renew_after not set after second failure: %q", got.RenewAfter)
	}
	if !secondRenew.After(firstRenew) {
		t.Errorf("backoff did not grow: first %v, second %v", firstRenew, secondRenew)
	}
	if diff := secondRenew.Sub(firstRenew); diff < 45*time.Minute || diff > 75*time.Minute {
		t.Errorf("backoff growth = %v, want ~1h (1h -> 2h)", diff)
	}
	if stub.calls != 2 {
		t.Errorf("issuer called %d times, want 2", stub.calls)
	}
}

func TestFallbackAndSNISelection(t *testing.T) {
	env := newTestEnv(t, acme.NewSelfSignedIssuer())
	ctx := context.Background()

	if err := env.mgr.EnsureFallback(ctx, "box.example.com"); err != nil {
		t.Fatalf("EnsureFallback: %v", err)
	}

	leafFor := func(hello *tls.ClientHelloInfo) *x509.Certificate {
		t.Helper()
		pair, err := env.mgr.GetCertificate(hello)
		if err != nil {
			t.Fatalf("GetCertificate(%q): %v", hello.ServerName, err)
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			t.Fatalf("ParseCertificate: %v", err)
		}
		return leaf
	}

	// Unknown SNI and empty SNI both get the fallback certificate.
	for _, sni := range []string{"unknown.example.org", ""} {
		leaf := leafFor(&tls.ClientHelloInfo{ServerName: sni})
		if !slices.Contains(leaf.DNSNames, "box.example.com") {
			t.Errorf("SNI %q: served cert for %v, want fallback box.example.com", sni, leaf.DNSNames)
		}
	}

	// After issuance, the per-hostname certificate wins for its SNI name.
	cert, err := env.mgr.EnsureTracked(ctx, env.domainID, "mail.example.com")
	if err != nil {
		t.Fatalf("EnsureTracked: %v", err)
	}
	if err := env.mgr.IssueNow(ctx, cert.ID); err != nil {
		t.Fatalf("IssueNow: %v", err)
	}
	leaf := leafFor(&tls.ClientHelloInfo{ServerName: "mail.example.com"})
	if !slices.Contains(leaf.DNSNames, "mail.example.com") {
		t.Errorf("SNI mail.example.com: served cert for %v", leaf.DNSNames)
	}
	// Trailing-dot and case variations select the same certificate.
	leaf = leafFor(&tls.ClientHelloInfo{ServerName: "MAIL.example.com."})
	if !slices.Contains(leaf.DNSNames, "mail.example.com") {
		t.Errorf("SNI MAIL.example.com.: served cert for %v", leaf.DNSNames)
	}
	// The fallback still serves unrelated names.
	leaf = leafFor(&tls.ClientHelloInfo{ServerName: "other.example.org"})
	if !slices.Contains(leaf.DNSNames, "box.example.com") {
		t.Errorf("unknown SNI after issuance: served cert for %v, want fallback", leaf.DNSNames)
	}
}

// TestIssueNowRunsOnIssued covers the review fix: after a certificate becomes
// active, the manager must regenerate the mail config (so the cert lands in
// the Postfix SNI map and Dovecot local_name blocks) via the OnIssued hook,
// rather than only issuing bare service reloads.
func TestIssueNowRunsOnIssued(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, acme.NewSelfSignedIssuer())

	called := 0
	env.mgr.OnIssued = func(context.Context) error {
		called++
		return nil
	}

	cert, err := env.mgr.EnsureTracked(ctx, env.domainID, "mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.mgr.IssueNow(ctx, cert.ID); err != nil {
		t.Fatalf("IssueNow: %v", err)
	}
	if called != 1 {
		t.Fatalf("OnIssued called %d times, want 1", called)
	}
	// With OnIssued wired, the manager must NOT also fire its own bare
	// reloads (the generator handles reloading), so no reload actions here.
	for _, a := range env.mock.Actions {
		if a == "reload postfix" || a == "reload dovecot" {
			t.Errorf("unexpected bare reload %q when OnIssued is set", a)
		}
	}
}
