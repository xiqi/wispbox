package updater

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadMissingFiles(t *testing.T) {
	st := Read(context.Background(), t.TempDir(), t.TempDir(), 20)
	if st.State != "idle" {
		t.Fatalf("State = %q, want idle", st.State)
	}
	if len(st.LogTail) != 0 {
		t.Fatalf("LogTail = %v, want empty", st.LogTail)
	}
}

func TestReadStatusAndTail(t *testing.T) {
	dataDir := t.TempDir()
	logDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, StatusFile), []byte(`{
		"state":"running",
		"target_version":"1.2.3",
		"message":"Installing"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, LogFile), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	st := Read(context.Background(), dataDir, logDir, 2)
	if st.State != "running" || st.TargetVersion != "1.2.3" || st.Message != "Installing" {
		t.Fatalf("Read() = %+v", st)
	}
	if got := st.LogTail; len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("LogTail = %v, want [two three]", got)
	}
}
