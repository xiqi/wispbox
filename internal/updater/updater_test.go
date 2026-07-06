package updater

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("Marshal status: %v", err)
	}
	if strings.Contains(string(raw), "log_tail") || strings.Contains(string(raw), "three") {
		t.Fatalf("marshaled status leaks log tail: %s", raw)
	}
}

func TestReadFailedStatusDerivesErrorSummary(t *testing.T) {
	dataDir := t.TempDir()
	logDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, StatusFile), []byte(`{
		"state":"failed",
		"target_version":"1.2.3",
		"message":"Upgrade failed. See the log for details."
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	log := "==> installing\n\x1b[1;31merror:\x1b[0m could not install packages\n"
	if err := os.WriteFile(filepath.Join(logDir, LogFile), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}

	st := Read(context.Background(), dataDir, logDir, 20)
	if st.Error != "error: could not install packages" {
		t.Fatalf("Error = %q, want cleaned log error", st.Error)
	}
}

func TestSameVersionNormalizesReleaseTags(t *testing.T) {
	if !SameVersion("0.1.0", "v0.1.0") {
		t.Fatal("SameVersion(0.1.0, v0.1.0) = false, want true")
	}
	if SameVersion("0.1.0-dev", "v0.1.0") {
		t.Fatal("SameVersion(0.1.0-dev, v0.1.0) = true, want false")
	}
}
