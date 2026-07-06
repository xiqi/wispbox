package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateCertificate(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")

	c, err := st.CreateCertificate(ctx, dom.ID, "Mail.Example.COM")
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	if c.Hostname != "mail.example.com" {
		t.Errorf("Hostname = %q, want lowercased %q", c.Hostname, "mail.example.com")
	}
	if c.Status != CertPending {
		t.Errorf("Status = %q, want %q", c.Status, CertPending)
	}
	if c.ChallengeType != "http-01" {
		t.Errorf("ChallengeType = %q, want %q", c.ChallengeType, "http-01")
	}
	if c.CertPath != "" || c.KeyPath != "" || c.LastError != "" {
		t.Errorf("new certificate has unexpected paths/error: %+v", c)
	}
	if _, ok := c.NotAfterTime(); ok {
		t.Error("NotAfterTime ok on a never-issued certificate, want false")
	}
	if _, ok := c.RenewAfterTime(); ok {
		t.Error("RenewAfterTime ok on a never-issued certificate, want false")
	}

	if _, err := st.CreateCertificate(ctx, dom.ID, "mail.example.com"); err == nil {
		t.Fatal("duplicate CreateCertificate succeeded, want error")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate error = %q, want mention of already exists", err)
	}

	got, err := st.GetCertificateByHostname(ctx, "MAIL.EXAMPLE.COM")
	if err != nil {
		t.Fatalf("GetCertificateByHostname: %v", err)
	}
	if got.ID != c.ID {
		t.Errorf("GetCertificateByHostname ID = %d, want %d", got.ID, c.ID)
	}
}

func TestCreateStandaloneCertificate(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	c, err := st.CreateCertificate(ctx, 0, "Admin.Example.COM")
	if err != nil {
		t.Fatalf("CreateCertificate without domain: %v", err)
	}
	if c.DomainID != 0 {
		t.Errorf("DomainID = %d, want 0 for standalone certificate", c.DomainID)
	}
	if c.Hostname != "admin.example.com" {
		t.Errorf("Hostname = %q, want admin.example.com", c.Hostname)
	}
}

func TestCertificateStateTransitions(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")
	c, err := st.CreateCertificate(ctx, dom.ID, "mail.example.com")
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	if err := st.UpdateCertificateStatus(ctx, c.ID, CertError, "acme: boom"); err != nil {
		t.Fatalf("UpdateCertificateStatus: %v", err)
	}
	got, err := st.GetCertificate(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if got.Status != CertError || got.LastError != "acme: boom" {
		t.Errorf("after error: status=%q lastError=%q, want error/acme: boom", got.Status, got.LastError)
	}

	now := time.Now().UTC()
	notBefore := now.Add(-time.Hour)
	notAfter := now.Add(90 * 24 * time.Hour)
	renewAfter := now.Add(60 * 24 * time.Hour)
	if err := st.MarkCertificateIssued(ctx, c.ID, "http-01", "/etc/wispbox/tls/cert.pem", "/etc/wispbox/tls/key.pem",
		notBefore, notAfter, renewAfter); err != nil {
		t.Fatalf("MarkCertificateIssued: %v", err)
	}
	got, err = st.GetCertificate(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if got.Status != CertActive {
		t.Errorf("Status = %q after issuance, want %q", got.Status, CertActive)
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q after issuance, want empty", got.LastError)
	}
	if got.CertPath != "/etc/wispbox/tls/cert.pem" || got.KeyPath != "/etc/wispbox/tls/key.pem" {
		t.Errorf("paths = %q / %q, want issued paths", got.CertPath, got.KeyPath)
	}
	if got.LastRenewedAt == "" {
		t.Error("LastRenewedAt empty after issuance")
	}
	na, ok := got.NotAfterTime()
	if !ok {
		t.Fatalf("NotAfterTime not parseable: %q", got.NotAfter)
	}
	if want := notAfter.Truncate(time.Millisecond); !na.Equal(want) {
		t.Errorf("NotAfterTime = %v, want %v", na, want)
	}
	ra, ok := got.RenewAfterTime()
	if !ok {
		t.Fatalf("RenewAfterTime not parseable: %q", got.RenewAfter)
	}
	if want := renewAfter.Truncate(time.Millisecond); !ra.Equal(want) {
		t.Errorf("RenewAfterTime = %v, want %v", ra, want)
	}
}

func TestCertificatesDueForRenewal(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")
	c, err := st.CreateCertificate(ctx, dom.ID, "mail.example.com")
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	due, err := st.CertificatesDueForRenewal(ctx)
	if err != nil {
		t.Fatalf("CertificatesDueForRenewal: %v", err)
	}
	if len(due) != 1 || due[0].ID != c.ID {
		t.Fatalf("never-issued certificate not due: %+v", due)
	}

	// After issuance with a future renew_after, it is no longer due.
	now := time.Now().UTC()
	if err := st.MarkCertificateIssued(ctx, c.ID, "http-01", "/c.pem", "/k.pem",
		now.Add(-time.Hour), now.Add(90*24*time.Hour), now.Add(60*24*time.Hour)); err != nil {
		t.Fatalf("MarkCertificateIssued: %v", err)
	}
	due, err = st.CertificatesDueForRenewal(ctx)
	if err != nil {
		t.Fatalf("CertificatesDueForRenewal: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("freshly issued certificate reported due: %+v", due)
	}

	// Forcing renew_after into the past makes it due again.
	if err := st.SetCertificateRenewAfter(ctx, c.ID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("SetCertificateRenewAfter: %v", err)
	}
	due, err = st.CertificatesDueForRenewal(ctx)
	if err != nil {
		t.Fatalf("CertificatesDueForRenewal: %v", err)
	}
	if len(due) != 1 || due[0].ID != c.ID {
		t.Fatalf("certificate with past renew_after not due: %+v", due)
	}

	if _, err := st.GetCertificate(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetCertificate(missing) = %v, want ErrNotFound", err)
	}
}

func TestListCertificates(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")
	if _, err := st.CreateCertificate(ctx, dom.ID, "mail.example.com"); err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	if _, err := st.CreateCertificate(ctx, dom.ID, "autoconfig.example.com"); err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	list, err := st.ListCertificates(ctx)
	if err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if len(list) != 2 || list[0].Hostname != "autoconfig.example.com" || list[1].Hostname != "mail.example.com" {
		t.Errorf("ListCertificates = %+v, want 2 certs ordered by hostname", list)
	}
}
