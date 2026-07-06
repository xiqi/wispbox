// Package updater reads the persisted one-click upgrade status.
package updater

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	StatusFile = "upgrade.json"
	LogFile    = "upgrade.log"
)

// Status is the admin-facing view of the latest upgrade attempt.
type Status struct {
	Available      bool     `json:"available"`
	State          string   `json:"state"`
	CurrentVersion string   `json:"current_version"`
	CurrentCommit  string   `json:"current_commit"`
	CurrentDate    string   `json:"current_date"`
	TargetVersion  string   `json:"target_version,omitempty"`
	StartedAt      string   `json:"started_at,omitempty"`
	FinishedAt     string   `json:"finished_at,omitempty"`
	Message        string   `json:"message,omitempty"`
	LogTail        []string `json:"log_tail,omitempty"`
}

// Read returns a best-effort status. Missing files mean no upgrade has run.
func Read(ctx context.Context, dataDir, logDir string, tailLines int) Status {
	st := Status{State: "idle", LogTail: []string{}}
	if b, err := os.ReadFile(filepath.Join(dataDir, StatusFile)); err == nil {
		_ = json.Unmarshal(b, &st)
		if st.State == "" {
			st.State = "idle"
		}
	}
	st.LogTail = Tail(filepath.Join(logDir, LogFile), tailLines)
	select {
	case <-ctx.Done():
		return st
	default:
		return st
	}
}

// Tail returns the last n lines of path.
func Tail(path string, n int) []string {
	if n <= 0 {
		return []string{}
	}
	f, err := os.Open(path)
	if err != nil {
		return []string{}
	}
	defer f.Close()

	lines := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(lines) == n {
			copy(lines, lines[1:])
			lines[n-1] = sc.Text()
			continue
		}
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return []string{}
	}
	return lines
}
