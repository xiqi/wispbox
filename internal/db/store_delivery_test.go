package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func testRelay(name string) OutboundRelay {
	return OutboundRelay{
		Name:              name,
		Provider:          "custom",
		Host:              "smtp.relay.test",
		Port:              587,
		Username:          "apikey",
		EncryptedPassword: "enc:secret",
		TLSMode:           TLSModeStartTLS,
		Enabled:           true,
	}
}

func TestRelayCRUD(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	r, err := st.CreateRelay(ctx, testRelay("primary"))
	if err != nil {
		t.Fatalf("CreateRelay: %v", err)
	}
	if r.Host != "smtp.relay.test" || r.Port != 587 || r.TLSMode != TLSModeStartTLS || !r.Enabled {
		t.Errorf("CreateRelay round-trip mismatch: %+v", r)
	}
	if r.EncryptedPassword != "enc:secret" {
		t.Errorf("EncryptedPassword = %q, want %q", r.EncryptedPassword, "enc:secret")
	}

	// Duplicate name is rejected.
	if _, err := st.CreateRelay(ctx, testRelay("primary")); err == nil {
		t.Fatal("duplicate CreateRelay succeeded, want error")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate error = %q, want mention of already exists", err)
	}

	if _, err := st.CreateRelay(ctx, testRelay("backup")); err != nil {
		t.Fatalf("CreateRelay(backup): %v", err)
	}
	list, err := st.ListRelays(ctx)
	if err != nil {
		t.Fatalf("ListRelays: %v", err)
	}
	if len(list) != 2 || list[0].Name != "backup" || list[1].Name != "primary" {
		t.Errorf("ListRelays = %+v, want backup, primary ordered by name", list)
	}

	upd := *r
	upd.Host = "smtp2.relay.test"
	upd.Port = 2525
	upd.TLSMode = TLSModeTLS
	upd.Enabled = false
	if err := st.UpdateRelay(ctx, upd); err != nil {
		t.Fatalf("UpdateRelay: %v", err)
	}
	got, err := st.GetRelay(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRelay: %v", err)
	}
	if got.Host != "smtp2.relay.test" || got.Port != 2525 || got.TLSMode != TLSModeTLS || got.Enabled {
		t.Errorf("after update got %+v, want host smtp2.relay.test port 2525 tls disabled", got)
	}

	if err := st.DeleteRelay(ctx, r.ID); err != nil {
		t.Fatalf("DeleteRelay: %v", err)
	}
	if _, err := st.GetRelay(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetRelay after delete = %v, want ErrNotFound", err)
	}
	if err := st.DeleteRelay(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteRelay = %v, want ErrNotFound", err)
	}
}

func TestDeleteRelayInUse(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")

	r, err := st.CreateRelay(ctx, testRelay("primary"))
	if err != nil {
		t.Fatalf("CreateRelay: %v", err)
	}
	pol, err := st.UpsertPolicy(ctx, ScopeDomain, dom.ID, ModeRelay, &r.ID)
	if err != nil {
		t.Fatalf("UpsertPolicy: %v", err)
	}

	if err := st.DeleteRelay(ctx, r.ID); err == nil {
		t.Fatal("DeleteRelay succeeded while referenced by an enabled policy, want error")
	} else if !strings.Contains(err.Error(), "used by") {
		t.Errorf("in-use error = %q, want mention of used by", err)
	}

	if err := st.DeletePolicy(ctx, pol.ID); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
	if err := st.DeleteRelay(ctx, r.ID); err != nil {
		t.Errorf("DeleteRelay after removing policy: %v", err)
	}
}

func TestUpsertPolicySemantics(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")

	r, err := st.CreateRelay(ctx, testRelay("primary"))
	if err != nil {
		t.Fatalf("CreateRelay: %v", err)
	}

	// First upsert for a scope inserts.
	p1, err := st.UpsertPolicy(ctx, ScopeDomain, dom.ID, ModeRelay, &r.ID)
	if err != nil {
		t.Fatalf("UpsertPolicy insert: %v", err)
	}
	if p1.Mode != ModeRelay || p1.RelayID == nil || *p1.RelayID != r.ID {
		t.Errorf("inserted policy = %+v, want mode relay relay_id %d", p1, r.ID)
	}

	// Second upsert for the same scope updates in place.
	p2, err := st.UpsertPolicy(ctx, ScopeDomain, dom.ID, ModeDirect, nil)
	if err != nil {
		t.Fatalf("UpsertPolicy update: %v", err)
	}
	if p2.ID != p1.ID {
		t.Errorf("upsert created a new row: id %d then %d, want same id", p1.ID, p2.ID)
	}
	if p2.Mode != ModeDirect || p2.RelayID != nil {
		t.Errorf("updated policy = %+v, want mode direct relay_id nil", p2)
	}

	// Upserting the global scope updates the seeded row rather than adding one.
	seeded, err := st.GetPolicyForScope(ctx, ScopeGlobal, 0)
	if err != nil {
		t.Fatalf("GetPolicyForScope(global): %v", err)
	}
	g, err := st.UpsertPolicy(ctx, ScopeGlobal, 0, ModeRelay, &r.ID)
	if err != nil {
		t.Fatalf("UpsertPolicy global: %v", err)
	}
	if g.ID != seeded.ID {
		t.Errorf("global upsert created a new row: id %d then %d, want same id", seeded.ID, g.ID)
	}
	if g.Mode != ModeRelay || g.RelayID == nil || *g.RelayID != r.ID {
		t.Errorf("global policy = %+v, want mode relay relay_id %d", g, r.ID)
	}

	list, err := st.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListPolicies returned %d policies, want 2 (global + domain): %+v", len(list), list)
	}
}

func TestDeletePolicyProtectsGlobal(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dom := mustCreateDomain(t, st, "example.com")

	global, err := st.GetPolicyForScope(ctx, ScopeGlobal, 0)
	if err != nil {
		t.Fatalf("GetPolicyForScope(global): %v", err)
	}
	if err := st.DeletePolicy(ctx, global.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeletePolicy(global) = %v, want ErrNotFound (protected)", err)
	}
	if _, err := st.GetPolicyForScope(ctx, ScopeGlobal, 0); err != nil {
		t.Errorf("global policy missing after protected delete: %v", err)
	}

	// Non-global policies delete normally.
	p, err := st.UpsertPolicy(ctx, ScopeDomain, dom.ID, ModeDirect, nil)
	if err != nil {
		t.Fatalf("UpsertPolicy: %v", err)
	}
	if err := st.DeletePolicy(ctx, p.ID); err != nil {
		t.Fatalf("DeletePolicy(domain): %v", err)
	}
	if _, err := st.GetPolicyForScope(ctx, ScopeDomain, dom.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPolicyForScope after delete = %v, want ErrNotFound", err)
	}
}
