package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mustCreateDomain is a test helper for setting up a domain fixture.
func mustCreateDomain(t *testing.T, st *Store, name string) *Domain {
	t.Helper()
	dom, err := st.CreateDomain(context.Background(), name, "")
	if err != nil {
		t.Fatalf("CreateDomain(%q): %v", name, err)
	}
	return dom
}

func TestCreateDomain(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	dom, err := st.CreateDomain(ctx, "Example.COM", "")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if dom.Name != "example.com" {
		t.Errorf("Name = %q, want %q", dom.Name, "example.com")
	}
	if dom.MailHostname != "mail.example.com" {
		t.Errorf("MailHostname = %q, want %q (default)", dom.MailHostname, "mail.example.com")
	}
	if dom.Status != DomainPending {
		t.Errorf("Status = %q, want %q", dom.Status, DomainPending)
	}

	// Explicit mail hostname is honored.
	dom2, err := st.CreateDomain(ctx, "example.org", "mx.example.org")
	if err != nil {
		t.Fatalf("CreateDomain with hostname: %v", err)
	}
	if dom2.MailHostname != "mx.example.org" {
		t.Errorf("MailHostname = %q, want %q", dom2.MailHostname, "mx.example.org")
	}

	// Invalid names are rejected.
	if _, err := st.CreateDomain(ctx, "https://example.net", ""); err == nil {
		t.Error("CreateDomain accepted a URL, want error")
	}
	if _, err := st.CreateDomain(ctx, "mail.example.net", ""); err == nil {
		t.Error("CreateDomain accepted a mail. prefixed name, want error")
	}
}

func TestCreateDomainDuplicate(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	mustCreateDomain(t, st, "example.com")

	if _, err := st.CreateDomain(ctx, "example.com", ""); err == nil {
		t.Fatal("duplicate CreateDomain succeeded, want error")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate error = %q, want mention of already exists", err)
	}
	// Case variants normalize to the same domain.
	if _, err := st.CreateDomain(ctx, "EXAMPLE.COM", ""); err == nil {
		t.Error("duplicate CreateDomain with different case succeeded, want error")
	}
}

func TestDomainLookupUpdateDelete(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")
	mustCreateDomain(t, st, "aaa.example.net")

	got, err := st.GetDomainByName(ctx, "  Example.com ")
	if err != nil {
		t.Fatalf("GetDomainByName: %v", err)
	}
	if got.ID != dom.ID {
		t.Errorf("GetDomainByName ID = %d, want %d", got.ID, dom.ID)
	}

	list, err := st.ListDomains(ctx)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(list) != 2 || list[0].Name != "aaa.example.net" || list[1].Name != "example.com" {
		t.Errorf("ListDomains = %+v, want 2 domains ordered by name", list)
	}

	if err := st.UpdateDomainStatus(ctx, dom.ID, DomainActive); err != nil {
		t.Fatalf("UpdateDomainStatus: %v", err)
	}
	got, err = st.GetDomain(ctx, dom.ID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if got.Status != DomainActive {
		t.Errorf("Status = %q after update, want %q", got.Status, DomainActive)
	}

	if err := st.DeleteDomain(ctx, dom.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	if _, err := st.GetDomain(ctx, dom.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDomain after delete = %v, want ErrNotFound", err)
	}
	if err := st.DeleteDomain(ctx, dom.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteDomain = %v, want ErrNotFound", err)
	}
}

func TestDeleteDomainCascadesMailboxes(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")
	mb, err := st.CreateMailbox(ctx, dom.ID, "user", "hash", 0)
	if err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}

	if err := st.DeleteDomain(ctx, dom.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	if _, err := st.GetMailbox(ctx, mb.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMailbox after domain delete = %v, want ErrNotFound (cascade)", err)
	}
}

func TestCreateMailbox(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")

	mb, err := st.CreateMailbox(ctx, dom.ID, "  User.Name  ", "hash1", 0)
	if err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	if mb.Email != "user.name@example.com" {
		t.Errorf("Email = %q, want %q", mb.Email, "user.name@example.com")
	}
	if mb.LocalPart != "user.name" {
		t.Errorf("LocalPart = %q, want %q", mb.LocalPart, "user.name")
	}
	if mb.QuotaMB != 0 {
		t.Errorf("QuotaMB = %d, want default 0", mb.QuotaMB)
	}
	if !mb.Enabled {
		t.Error("Enabled = false, want true")
	}

	// Explicit quota is kept.
	mb2, err := st.CreateMailbox(ctx, dom.ID, "big", "hash2", 4096)
	if err != nil {
		t.Fatalf("CreateMailbox with quota: %v", err)
	}
	if mb2.QuotaMB != 4096 {
		t.Errorf("QuotaMB = %d, want 4096", mb2.QuotaMB)
	}

	// Invalid local part and missing domain are rejected.
	if _, err := st.CreateMailbox(ctx, dom.ID, "bad..dots", "h", 0); err == nil {
		t.Error("CreateMailbox accepted invalid local part, want error")
	}
	if _, err := st.CreateMailbox(ctx, 9999, "user", "h", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateMailbox on missing domain = %v, want ErrNotFound", err)
	}
}

func TestCreateMailboxDuplicate(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")
	other := mustCreateDomain(t, st, "example.org")

	if _, err := st.CreateMailbox(ctx, dom.ID, "user", "h", 0); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	if _, err := st.CreateMailbox(ctx, dom.ID, "user", "h", 0); err == nil {
		t.Fatal("duplicate CreateMailbox succeeded, want error")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate error = %q, want mention of already exists", err)
	}
	// Same local part on a different domain is fine.
	if _, err := st.CreateMailbox(ctx, other.ID, "user", "h", 0); err != nil {
		t.Errorf("CreateMailbox on other domain: %v", err)
	}
}

func TestMailboxLookupUpdateDelete(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")
	other := mustCreateDomain(t, st, "example.org")
	mb, err := st.CreateMailbox(ctx, dom.ID, "user", "h", 0)
	if err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	if _, err := st.CreateMailbox(ctx, other.ID, "elsewhere", "h", 0); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}

	got, err := st.GetMailboxByEmail(ctx, " User@Example.com ")
	if err != nil {
		t.Fatalf("GetMailboxByEmail: %v", err)
	}
	if got.ID != mb.ID {
		t.Errorf("GetMailboxByEmail ID = %d, want %d", got.ID, mb.ID)
	}

	all, err := st.ListMailboxes(ctx, 0)
	if err != nil {
		t.Fatalf("ListMailboxes(0): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListMailboxes(0) returned %d, want 2", len(all))
	}
	scoped, err := st.ListMailboxes(ctx, dom.ID)
	if err != nil {
		t.Fatalf("ListMailboxes(domain): %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != mb.ID {
		t.Errorf("ListMailboxes(domain) = %+v, want only %d", scoped, mb.ID)
	}

	n, err := st.CountMailboxes(ctx, dom.ID)
	if err != nil {
		t.Fatalf("CountMailboxes: %v", err)
	}
	if n != 1 {
		t.Errorf("CountMailboxes = %d, want 1", n)
	}

	if err := st.UpdateMailbox(ctx, mb.ID, 2048, false); err != nil {
		t.Fatalf("UpdateMailbox: %v", err)
	}
	if err := st.UpdateMailboxPassword(ctx, mb.ID, "newhash"); err != nil {
		t.Fatalf("UpdateMailboxPassword: %v", err)
	}
	got, err = st.GetMailbox(ctx, mb.ID)
	if err != nil {
		t.Fatalf("GetMailbox: %v", err)
	}
	if got.QuotaMB != 2048 || got.Enabled || got.PasswordHash != "newhash" {
		t.Errorf("after update got quota=%d enabled=%v hash=%q, want 2048 false newhash",
			got.QuotaMB, got.Enabled, got.PasswordHash)
	}
	if err := st.UpdateMailbox(ctx, mb.ID, 0, true); err != nil {
		t.Fatalf("UpdateMailbox clear quota: %v", err)
	}
	got, err = st.GetMailbox(ctx, mb.ID)
	if err != nil {
		t.Fatalf("GetMailbox after clear quota: %v", err)
	}
	if got.QuotaMB != 0 || !got.Enabled {
		t.Errorf("after clear quota got quota=%d enabled=%v, want 0 true", got.QuotaMB, got.Enabled)
	}

	if err := st.DeleteMailbox(ctx, mb.ID); err != nil {
		t.Fatalf("DeleteMailbox: %v", err)
	}
	if err := st.DeleteMailbox(ctx, mb.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteMailbox = %v, want ErrNotFound", err)
	}
}

func TestCreateAliasSourceRewriting(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")

	tests := []struct {
		name       string
		source     string
		dest       string
		catchAll   bool
		wantSource string
		wantErr    string
	}{
		{"bare local part qualified", "info", "team@external.com", false, "info@example.com", ""},
		{"full address normalized", " Sales@Example.com ", "team@external.com", false, "sales@example.com", ""},
		{"catch-all rewrites source", "whatever", "team@external.com", true, "@example.com", ""},
		{"wrong domain rejected", "info@other.com", "team@external.com", false, "", "must be on example.com"},
		{"invalid local part rejected", "bad..dots", "team@external.com", false, "", "source"},
		{"invalid destination rejected", "ok", "not-an-email", false, "", "destination"},
		{"self-pointing rejected", "self", "self@example.com", false, "", "cannot point to itself"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := st.CreateAlias(ctx, dom.ID, tt.source, tt.dest, tt.catchAll)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("CreateAlias(%q) succeeded, want error containing %q", tt.source, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateAlias(%q): %v", tt.source, err)
			}
			if a.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", a.Source, tt.wantSource)
			}
			if a.IsCatchAll != tt.catchAll {
				t.Errorf("IsCatchAll = %v, want %v", a.IsCatchAll, tt.catchAll)
			}
			if !a.Enabled {
				t.Error("Enabled = false, want true")
			}
		})
	}
}

func TestCreateAliasDuplicate(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")

	if _, err := st.CreateAlias(ctx, dom.ID, "info", "a@external.com", false); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	if _, err := st.CreateAlias(ctx, dom.ID, "info", "a@external.com", false); err == nil {
		t.Fatal("duplicate CreateAlias succeeded, want error")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate error = %q, want mention of already exists", err)
	}
	// Same source with a different destination is a distinct alias.
	if _, err := st.CreateAlias(ctx, dom.ID, "info", "b@external.com", false); err != nil {
		t.Errorf("CreateAlias same source new destination: %v", err)
	}
}

func TestListAliasSourcesForDestination(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")

	if _, err := st.CreateAlias(ctx, dom.ID, "info", "user@example.net", false); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	sales, err := st.CreateAlias(ctx, dom.ID, "sales", "user@example.net", false)
	if err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	if _, err := st.CreateAlias(ctx, dom.ID, "", "user@example.net", true); err != nil {
		t.Fatalf("CreateAlias catch-all: %v", err)
	}
	if _, err := st.CreateAlias(ctx, dom.ID, "other", "elsewhere@example.net", false); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	// Disabled aliases must not be returned.
	if err := st.UpdateAliasEnabled(ctx, sales.ID, false); err != nil {
		t.Fatalf("UpdateAliasEnabled: %v", err)
	}

	got, err := st.ListAliasSourcesForDestination(ctx, " User@Example.NET ")
	if err != nil {
		t.Fatalf("ListAliasSourcesForDestination: %v", err)
	}
	want := map[string]bool{"info@example.com": true, "@example.com": true}
	if len(got) != len(want) {
		t.Fatalf("sources = %v, want %v", got, want)
	}
	for _, src := range got {
		if !want[src] {
			t.Errorf("unexpected source %q in %v", src, got)
		}
	}
}

func TestAliasListAndDelete(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")
	other := mustCreateDomain(t, st, "example.org")

	a, err := st.CreateAlias(ctx, dom.ID, "info", "x@external.com", false)
	if err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	if _, err := st.CreateAlias(ctx, other.ID, "info", "x@external.com", false); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}

	all, err := st.ListAliases(ctx, 0)
	if err != nil {
		t.Fatalf("ListAliases(0): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListAliases(0) returned %d, want 2", len(all))
	}
	scoped, err := st.ListAliases(ctx, dom.ID)
	if err != nil {
		t.Fatalf("ListAliases(domain): %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != a.ID {
		t.Errorf("ListAliases(domain) = %+v, want only %d", scoped, a.ID)
	}

	got, err := st.GetAlias(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if got.Source != "info@example.com" {
		t.Errorf("GetAlias Source = %q, want %q", got.Source, "info@example.com")
	}

	if err := st.DeleteAlias(ctx, a.ID); err != nil {
		t.Fatalf("DeleteAlias: %v", err)
	}
	if err := st.DeleteAlias(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteAlias = %v, want ErrNotFound", err)
	}
}
