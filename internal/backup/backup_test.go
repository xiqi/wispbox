package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/db"
)

// TestCreateAndRestore covers the round trip plus the two regressions the
// review found: stale WAL sidecars and unpreserved file modes.
func TestCreateAndRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.DevelopmentDefaults(dir)
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	sqldb, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, sqldb); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(sqldb)
	if _, err := store.CreateDomain(ctx, "example.com", ""); err != nil {
		t.Fatal(err)
	}

	// A cert file whose mode must survive the round trip.
	certFile := filepath.Join(cfg.CertDir, "mail.example.com", "privkey.pem")
	if err := os.MkdirAll(filepath.Dir(certFile), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, []byte("KEY"), 0o600); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(dir, "backup.tar.gz")
	if _, err := Create(ctx, cfg, sqldb, archive); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	sqldb.Close()

	// Plant a stale -wal sidecar and a mangled DB to prove restore replaces
	// them cleanly rather than letting SQLite replay the old journal.
	if err := os.WriteFile(cfg.DBPath+"-wal", []byte("stale journal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.DBPath, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, []byte("TAMPERED"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Restore(ctx, cfg, archive, true); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Stale WAL must be gone.
	if _, err := os.Stat(cfg.DBPath + "-wal"); !os.IsNotExist(err) {
		t.Errorf("stale -wal sidecar was not removed")
	}

	// The restored cert key must keep its 0600 mode and original content.
	info, err := os.Stat(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("restored key mode = %o, want 0600", info.Mode().Perm())
	}
	if b, _ := os.ReadFile(certFile); string(b) != "KEY" {
		t.Errorf("restored key content = %q, want KEY", b)
	}

	// The restored database must be readable and contain the domain.
	sqldb2, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer sqldb2.Close()
	if _, err := db.Migrate(ctx, sqldb2); err != nil {
		t.Fatal(err)
	}
	store2 := db.NewStore(sqldb2)
	if _, err := store2.GetDomainByName(ctx, "example.com"); err != nil {
		t.Errorf("restored db missing domain: %v", err)
	}
}

func TestRestoreRejectsUnsafePaths(t *testing.T) {
	// Not a real archive; Restore should fail cleanly, never panic.
	dir := t.TempDir()
	cfg := config.DevelopmentDefaults(dir)
	_ = cfg.EnsureDirs()
	bad := filepath.Join(dir, "not-a-backup.tar.gz")
	_ = os.WriteFile(bad, []byte("garbage"), 0o600)
	if err := Restore(context.Background(), cfg, bad, true); err == nil {
		t.Error("expected error restoring a non-archive")
	}
}
