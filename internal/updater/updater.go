// Package updater reads the persisted one-click upgrade status.
package updater

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	StatusFile = "upgrade.json"
	LogFile    = "upgrade.log"
)

// Status is the admin-facing view of the latest upgrade attempt.
type Status struct {
	Available       bool     `json:"available"`
	State           string   `json:"state"`
	CurrentVersion  string   `json:"current_version"`
	CurrentCommit   string   `json:"current_commit"`
	CurrentDate     string   `json:"current_date"`
	LatestVersion   string   `json:"latest_version,omitempty"`
	UpdateAvailable bool     `json:"update_available"`
	TargetVersion   string   `json:"target_version,omitempty"`
	StartedAt       string   `json:"started_at,omitempty"`
	FinishedAt      string   `json:"finished_at,omitempty"`
	Message         string   `json:"message,omitempty"`
	LogTail         []string `json:"log_tail,omitempty"`
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

func LatestReleaseVersion(ctx context.Context, repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = "xiqi/wispbox"
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("latest release lookup returned %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if strings.TrimSpace(body.TagName) == "" {
		return "", fmt.Errorf("latest release has no tag")
	}
	return NormalizeVersion(body.TagName), nil
}

func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "wispbox ")
	v = strings.TrimPrefix(v, "v")
	return v
}

func SameVersion(a, b string) bool {
	return NormalizeVersion(a) != "" && NormalizeVersion(a) == NormalizeVersion(b)
}
