package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupRestoreAcceptsForceBeforePath(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "missing.tar.gz")

	err := run([]string{
		"backup", "restore",
		"--dev", "--dev-dir", tmp,
		"--force", missing,
	})
	if err == nil {
		t.Fatal("expected missing archive error")
	}
	if strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("path after --force should be accepted, got %v", err)
	}
}
