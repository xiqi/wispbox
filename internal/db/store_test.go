package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSettings(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if _, err := st.GetSetting(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSetting(missing) = %v, want ErrNotFound", err)
	}
	if got := st.GetSettingDefault(ctx, "missing", "fallback"); got != "fallback" {
		t.Errorf("GetSettingDefault(missing) = %q, want %q", got, "fallback")
	}

	if err := st.SetSetting(ctx, "primary_hostname", "mail.example.com"); err != nil {
		t.Fatalf("SetSetting insert: %v", err)
	}
	if got, err := st.GetSetting(ctx, "primary_hostname"); err != nil || got != "mail.example.com" {
		t.Errorf("GetSetting = %q, %v, want mail.example.com", got, err)
	}

	// Setting the same key again updates in place.
	if err := st.SetSetting(ctx, "primary_hostname", "mx.example.com"); err != nil {
		t.Fatalf("SetSetting update: %v", err)
	}
	if got, err := st.GetSetting(ctx, "primary_hostname"); err != nil || got != "mx.example.com" {
		t.Errorf("GetSetting after update = %q, %v, want mx.example.com", got, err)
	}

	all, err := st.AllSettings(ctx)
	if err != nil {
		t.Fatalf("AllSettings: %v", err)
	}
	// "initialized" is seeded by migrations.
	if all["initialized"] != "false" || all["primary_hostname"] != "mx.example.com" {
		t.Errorf("AllSettings = %v, want initialized=false and primary_hostname=mx.example.com", all)
	}

	if st.IsInitialized(ctx) {
		t.Error("IsInitialized = true before setup completes, want false")
	}
	if err := st.SetSetting(ctx, "initialized", "true"); err != nil {
		t.Fatalf("SetSetting initialized: %v", err)
	}
	if !st.IsInitialized(ctx) {
		t.Error("IsInitialized = false after marking initialized, want true")
	}
}

func TestSessionCreateAndLookup(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	token, err := st.CreateSession(ctx, UserAdmin, 42, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64 hex chars", len(token))
	}

	sess, err := st.LookupSession(ctx, token, UserAdmin)
	if err != nil {
		t.Fatalf("LookupSession: %v", err)
	}
	if sess.UserID != 42 || sess.UserType != UserAdmin {
		t.Errorf("session = %+v, want user_id 42 type admin", sess)
	}
	if sess.ID != HashSessionToken(token) {
		t.Errorf("session id = %q, want sha256 of token", sess.ID)
	}

	if _, err := st.LookupSession(ctx, "", UserAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupSession(empty token) = %v, want ErrNotFound", err)
	}
	if _, err := st.LookupSession(ctx, "deadbeef", UserAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupSession(unknown token) = %v, want ErrNotFound", err)
	}
}

// TestSessionUserTypeSeparation is the session separation test: a mailbox
// session must never resolve as an admin session and vice versa.
func TestSessionUserTypeSeparation(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	adminTok, err := st.CreateSession(ctx, UserAdmin, 1, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession(admin): %v", err)
	}
	mboxTok, err := st.CreateSession(ctx, UserMailbox, 1, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession(mailbox): %v", err)
	}

	if _, err := st.LookupSession(ctx, mboxTok, UserAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("mailbox token resolved as admin: %v, want ErrNotFound", err)
	}
	if _, err := st.LookupSession(ctx, adminTok, UserMailbox); !errors.Is(err, ErrNotFound) {
		t.Errorf("admin token resolved as mailbox: %v, want ErrNotFound", err)
	}

	// The correct type still resolves after the cross-type rejections.
	if _, err := st.LookupSession(ctx, adminTok, UserAdmin); err != nil {
		t.Errorf("admin token as admin: %v", err)
	}
	if _, err := st.LookupSession(ctx, mboxTok, UserMailbox); err != nil {
		t.Errorf("mailbox token as mailbox: %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	token, err := st.CreateSession(ctx, UserAdmin, 7, -time.Minute)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := st.LookupSession(ctx, token, UserAdmin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LookupSession(expired) = %v, want ErrNotFound", err)
	}
	// The expired row is removed on lookup.
	var n int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`,
		HashSessionToken(token)).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("expired session row still present after lookup, want deleted")
	}
}

func TestSessionDeleteAndPrune(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	token, err := st.CreateSession(ctx, UserAdmin, 1, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := st.DeleteSession(ctx, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := st.LookupSession(ctx, token, UserAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupSession after delete = %v, want ErrNotFound", err)
	}

	expired, err := st.CreateSession(ctx, UserMailbox, 2, -time.Minute)
	if err != nil {
		t.Fatalf("CreateSession(expired): %v", err)
	}
	live, err := st.CreateSession(ctx, UserMailbox, 3, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession(live): %v", err)
	}
	if err := st.PruneSessions(ctx); err != nil {
		t.Fatalf("PruneSessions: %v", err)
	}
	var n int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`,
		HashSessionToken(expired)).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Error("expired session survived PruneSessions")
	}
	if _, err := st.LookupSession(ctx, live, UserMailbox); err != nil {
		t.Errorf("live session pruned: %v", err)
	}
}
