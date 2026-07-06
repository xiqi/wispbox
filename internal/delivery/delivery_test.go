package delivery

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiqi/wispbox/internal/db"
)

func validRelay() db.OutboundRelay {
	return db.OutboundRelay{
		Name:              "Postmark",
		Provider:          "postmark",
		Host:              "smtp.postmarkapp.com",
		Port:              587,
		Username:          "token",
		EncryptedPassword: "sealed",
		TLSMode:           db.TLSModeStartTLS,
		Enabled:           true,
	}
}

func TestValidateRelay(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*db.OutboundRelay)
		wantErr string
	}{
		{"valid", func(r *db.OutboundRelay) {}, ""},
		{"valid max port", func(r *db.OutboundRelay) { r.Port = 65535 }, ""},
		{"valid implicit tls", func(r *db.OutboundRelay) { r.TLSMode = db.TLSModeTLS }, ""},
		{"missing name", func(r *db.OutboundRelay) { r.Name = "  " }, "name is required"},
		{"unknown provider", func(r *db.OutboundRelay) { r.Provider = "sendgrid" }, "unknown relay provider"},
		{"empty host", func(r *db.OutboundRelay) { r.Host = " " }, "host is required"},
		{"host with scheme", func(r *db.OutboundRelay) { r.Host = "smtp://smtp.example.com" }, "bare hostname"},
		{"host with port", func(r *db.OutboundRelay) { r.Host = "smtp.example.com:587" }, "bare hostname"},
		{"host with space", func(r *db.OutboundRelay) { r.Host = "smtp example.com" }, "bare hostname"},
		{"port zero", func(r *db.OutboundRelay) { r.Port = 0 }, "between 1 and 65535"},
		{"port negative", func(r *db.OutboundRelay) { r.Port = -25 }, "between 1 and 65535"},
		{"port too high", func(r *db.OutboundRelay) { r.Port = 65536 }, "between 1 and 65535"},
		{"tls none rejected", func(r *db.OutboundRelay) { r.TLSMode = db.TLSModeNone }, "unencrypted relays are not supported"},
		{"tls invalid", func(r *db.OutboundRelay) { r.TLSMode = db.TLSMode("ssl") }, "invalid TLS mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validRelay()
			tt.mutate(&r)
			err := ValidateRelay(r)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateRelay() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateRelay() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateRelay() = %q, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestPresets(t *testing.T) {
	presets := Presets()
	if len(presets) == 0 {
		t.Fatal("Presets() returned no presets")
	}
	seen := map[string]bool{}
	for _, p := range presets {
		if seen[p.Provider] {
			t.Errorf("duplicate provider %q", p.Provider)
		}
		seen[p.Provider] = true
		if p.TLSMode == db.TLSModeNone {
			t.Errorf("preset %q uses unencrypted TLS mode", p.Provider)
		}
		if p.Provider == "custom" {
			continue // custom has no host until the admin fills one in
		}
		if err := ValidateRelay(db.OutboundRelay{
			Name: p.Label, Provider: p.Provider, Host: p.Host, Port: p.Port, TLSMode: p.TLSMode,
		}); err != nil {
			t.Errorf("preset %q does not validate: %v", p.Provider, err)
		}
	}
	for _, want := range []string{"ses", "postmark", "mailgun", "smtp2go", "resend", "custom"} {
		if !seen[want] {
			t.Errorf("missing preset %q", want)
		}
	}
}

func TestPresetFor(t *testing.T) {
	p, ok := PresetFor("postmark")
	if !ok {
		t.Fatal("PresetFor(postmark) not found")
	}
	if p.Provider != "postmark" || p.Host != "smtp.postmarkapp.com" || p.Port != 587 {
		t.Fatalf("PresetFor(postmark) = %+v, unexpected fields", p)
	}
	if _, ok := PresetFor("sendgrid"); ok {
		t.Fatal("PresetFor(sendgrid) = ok, want not found")
	}
	if _, ok := PresetFor(""); ok {
		t.Fatal("PresetFor(\"\") = ok, want not found")
	}
}

// newTestEngine opens a real SQLite store in a temp dir with all migrations
// applied, so engine tests exercise the actual policy queries.
func newTestEngine(t *testing.T) (*Engine, *db.Store) {
	t.Helper()
	sqldb, err := db.Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqldb.Close() })
	if _, err := db.Migrate(context.Background(), sqldb); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	store := db.NewStore(sqldb)
	return NewEngine(store), store
}

func mustCreateRelay(t *testing.T, store *db.Store, name string, enabled bool) *db.OutboundRelay {
	t.Helper()
	r := validRelay()
	r.Name = name
	r.Enabled = enabled
	created, err := store.CreateRelay(context.Background(), r)
	if err != nil {
		t.Fatalf("CreateRelay(%s): %v", name, err)
	}
	return created
}

func TestResolveForDomainGlobalDirectDefault(t *testing.T) {
	engine, _ := newTestEngine(t)
	ctx := context.Background()

	res, err := engine.ResolveForDomain(ctx, 42)
	if err != nil {
		t.Fatalf("ResolveForDomain: %v", err)
	}
	if res.Mode != db.ModeDirect {
		t.Errorf("Mode = %q, want %q", res.Mode, db.ModeDirect)
	}
	if res.Source != "global" {
		t.Errorf("Source = %q, want %q", res.Source, "global")
	}
	if res.Relay != nil {
		t.Errorf("Relay = %+v, want nil", res.Relay)
	}
}

func TestResolveForDomainRelayOverride(t *testing.T) {
	engine, store := newTestEngine(t)
	ctx := context.Background()
	relay := mustCreateRelay(t, store, "Postmark", true)

	if _, err := engine.SetPolicy(ctx, db.ScopeDomain, 42, db.ModeRelay, &relay.ID); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}

	res, err := engine.ResolveForDomain(ctx, 42)
	if err != nil {
		t.Fatalf("ResolveForDomain: %v", err)
	}
	if res.Mode != db.ModeRelay {
		t.Errorf("Mode = %q, want %q", res.Mode, db.ModeRelay)
	}
	if res.Source != "domain" {
		t.Errorf("Source = %q, want %q", res.Source, "domain")
	}
	if res.Relay == nil || res.Relay.ID != relay.ID {
		t.Errorf("Relay = %+v, want relay id %d", res.Relay, relay.ID)
	}

	// Other domains are unaffected by the override.
	other, err := engine.ResolveForDomain(ctx, 43)
	if err != nil {
		t.Fatalf("ResolveForDomain(other): %v", err)
	}
	if other.Mode != db.ModeDirect || other.Source != "global" {
		t.Errorf("other domain = %+v, want global direct", other)
	}
}

func TestResolveForDomainInheritFallsBack(t *testing.T) {
	engine, store := newTestEngine(t)
	ctx := context.Background()

	if _, err := engine.SetPolicy(ctx, db.ScopeDomain, 42, db.ModeInherit, nil); err != nil {
		t.Fatalf("SetPolicy(inherit): %v", err)
	}

	res, err := engine.ResolveForDomain(ctx, 42)
	if err != nil {
		t.Fatalf("ResolveForDomain: %v", err)
	}
	if res.Mode != db.ModeDirect || res.Source != "global" {
		t.Errorf("inherit resolved to %+v, want global direct", res)
	}

	// If the global default is a relay, inherit should pick that up too.
	relay := mustCreateRelay(t, store, "Postmark", true)
	if _, err := engine.SetPolicy(ctx, db.ScopeGlobal, 0, db.ModeRelay, &relay.ID); err != nil {
		t.Fatalf("SetPolicy(global relay): %v", err)
	}
	res, err = engine.ResolveForDomain(ctx, 42)
	if err != nil {
		t.Fatalf("ResolveForDomain after global relay: %v", err)
	}
	if res.Mode != db.ModeRelay || res.Source != "global" {
		t.Errorf("inherit resolved to %+v, want global relay", res)
	}
	if res.Relay == nil || res.Relay.ID != relay.ID {
		t.Errorf("Relay = %+v, want relay id %d", res.Relay, relay.ID)
	}
}

func TestResolveForDomainDisabledRelayErrors(t *testing.T) {
	engine, store := newTestEngine(t)
	ctx := context.Background()
	relay := mustCreateRelay(t, store, "Postmark", true)

	if _, err := engine.SetPolicy(ctx, db.ScopeDomain, 42, db.ModeRelay, &relay.ID); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
	relay.Enabled = false
	if err := store.UpdateRelay(ctx, *relay); err != nil {
		t.Fatalf("UpdateRelay: %v", err)
	}

	// No silent fallback to direct: a broken relay must surface as an error.
	if _, err := engine.ResolveForDomain(ctx, 42); err == nil {
		t.Fatal("ResolveForDomain = nil error, want disabled-relay error")
	} else if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("ResolveForDomain error = %q, want mention of disabled relay", err)
	}
}

func TestResolveForDomainRelayPolicyWithoutRelayErrors(t *testing.T) {
	engine, store := newTestEngine(t)
	ctx := context.Background()

	// SetPolicy refuses this shape, so write it directly to simulate a
	// half-configured row.
	if _, err := store.UpsertPolicy(ctx, db.ScopeDomain, 42, db.ModeRelay, nil); err != nil {
		t.Fatalf("UpsertPolicy: %v", err)
	}

	if _, err := engine.ResolveForDomain(ctx, 42); err == nil {
		t.Fatal("ResolveForDomain = nil error, want missing-relay error")
	} else if !strings.Contains(err.Error(), "no relay is selected") {
		t.Fatalf("ResolveForDomain error = %q, want mention of missing relay", err)
	}
}

func TestSetPolicyValidation(t *testing.T) {
	engine, store := newTestEngine(t)
	ctx := context.Background()
	enabled := mustCreateRelay(t, store, "Enabled", true)
	disabled := mustCreateRelay(t, store, "Disabled", false)
	missing := int64(9999)

	tests := []struct {
		name    string
		scope   db.PolicyScope
		scopeID int64
		mode    db.DeliveryMode
		relayID *int64
		wantErr string
	}{
		{"global cannot inherit", db.ScopeGlobal, 0, db.ModeInherit, nil, "cannot be 'inherit'"},
		{"relay mode requires relay", db.ScopeDomain, 1, db.ModeRelay, nil, "requires selecting a relay"},
		{"relay mode missing relay", db.ScopeDomain, 1, db.ModeRelay, &missing, "not found"},
		{"relay mode disabled relay", db.ScopeDomain, 1, db.ModeRelay, &disabled.ID, "disabled"},
		{"invalid mode", db.ScopeDomain, 1, db.DeliveryMode("bogus"), nil, "invalid delivery mode"},
		{"domain inherit ok", db.ScopeDomain, 1, db.ModeInherit, nil, ""},
		{"relay mode ok", db.ScopeDomain, 2, db.ModeRelay, &enabled.ID, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := engine.SetPolicy(ctx, tt.scope, tt.scopeID, tt.mode, tt.relayID)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("SetPolicy() = %v, want nil", err)
				}
				if p.Mode != tt.mode {
					t.Errorf("stored mode = %q, want %q", p.Mode, tt.mode)
				}
				return
			}
			if err == nil {
				t.Fatalf("SetPolicy() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("SetPolicy() = %q, want error containing %q", err, tt.wantErr)
			}
		})
	}

	// A stray relay id on a direct policy is dropped, not stored.
	p, err := engine.SetPolicy(ctx, db.ScopeDomain, 3, db.ModeDirect, &enabled.ID)
	if err != nil {
		t.Fatalf("SetPolicy(direct with relay id): %v", err)
	}
	if p.RelayID != nil {
		t.Errorf("direct policy stored relay id %d, want nil", *p.RelayID)
	}
}
