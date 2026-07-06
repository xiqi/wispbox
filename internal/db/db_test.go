package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// newTestDB opens a fresh SQLite database in a temp dir and applies all
// migrations. The handle is closed automatically when the test ends.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.db")
	sqldb, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() { sqldb.Close() })
	if _, err := Migrate(context.Background(), sqldb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqldb
}

// newTestStore returns a Store backed by a fresh migrated database.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(newTestDB(t))
}

func TestMigrateFreshAndIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	sqldb, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sqldb.Close()

	applied, err := Migrate(ctx, sqldb)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("first Migrate applied %d migrations, want 1: %v", len(applied), applied)
	}

	again, err := Migrate(ctx, sqldb)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second Migrate applied %d migrations, want 0: %v", len(again), again)
	}

	var recorded int
	if err := sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&recorded); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if recorded != 1 {
		t.Fatalf("schema_migrations has %d rows, want 1", recorded)
	}
}

func TestMigrateCreatesAllTables(t *testing.T) {
	ctx := context.Background()
	sqldb := newTestDB(t)

	rows, err := sqldb.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	want := []string{
		"admins", "domains", "mailboxes", "aliases",
		"outbound_relays", "outbound_policies", "certificates",
		"sessions", "passkeys", "auth_challenges", "audit_logs",
		"service_events", "settings",
	}
	for _, tbl := range want {
		if !have[tbl] {
			t.Errorf("table %q missing after migration", tbl)
		}
	}

	var usernameColumns, emailColumns int
	if err := sqldb.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('admins') WHERE name = 'username'`).Scan(&usernameColumns); err != nil {
		t.Fatalf("query admin username column: %v", err)
	}
	if err := sqldb.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('admins') WHERE name = 'email'`).Scan(&emailColumns); err != nil {
		t.Fatalf("query admin email column: %v", err)
	}
	if usernameColumns != 1 || emailColumns != 0 {
		t.Fatalf("admin columns username=%d email=%d, want username=1 email=0", usernameColumns, emailColumns)
	}
}

func TestMigrateSeedsGlobalPolicyAndSettings(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	pol, err := st.GetPolicyForScope(ctx, ScopeGlobal, 0)
	if err != nil {
		t.Fatalf("GetPolicyForScope(global, 0): %v", err)
	}
	if pol.Mode != ModeDirect {
		t.Errorf("seeded global policy mode = %q, want %q", pol.Mode, ModeDirect)
	}
	if pol.RelayID != nil {
		t.Errorf("seeded global policy relay_id = %v, want nil", *pol.RelayID)
	}
	if !pol.Enabled {
		t.Error("seeded global policy is disabled, want enabled")
	}

	v, err := st.GetSetting(ctx, "initialized")
	if err != nil {
		t.Fatalf("GetSetting(initialized): %v", err)
	}
	if v != "false" {
		t.Errorf("seeded setting initialized = %q, want %q", v, "false")
	}
	if st.IsInitialized(ctx) {
		t.Error("IsInitialized = true on a fresh database, want false")
	}
}

func TestNowStringRoundTrip(t *testing.T) {
	s := NowString()
	got, ok := ParseTime(s)
	if !ok {
		t.Fatalf("ParseTime(NowString() = %q) failed", s)
	}
	if d := time.Since(got); d < -time.Minute || d > time.Minute {
		t.Errorf("ParseTime(NowString()) = %v, not close to now", got)
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"2026-07-05T12:34:56.789Z", true},
		{"2026-07-05T12:34:56Z", true},
		{"2026-07-05 12:34:56", true},
		{"", false},
		{"not-a-time", false},
	}
	for _, tt := range tests {
		if _, ok := ParseTime(tt.in); ok != tt.ok {
			t.Errorf("ParseTime(%q) ok = %v, want %v", tt.in, ok, tt.ok)
		}
	}
}
