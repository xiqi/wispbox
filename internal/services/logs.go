package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// LogLine is one normalized log entry for the Admin UI.
type LogLine struct {
	Time    string `json:"time"`
	Service string `json:"service"`
	Message string `json:"message"`
}

// LogReader tails service logs.
type LogReader interface {
	Tail(ctx context.Context, services []string, n int) ([]LogLine, error)
}

// ---- journald (production) ----

type JournalLogReader struct{}

func NewJournalLogReader() *JournalLogReader { return &JournalLogReader{} }

func (j *JournalLogReader) Tail(ctx context.Context, svcs []string, n int) ([]LogLine, error) {
	if n <= 0 || n > 1000 {
		n = 200
	}
	args := []string{"--no-pager", "--output", "json", "-n", strconv.Itoa(n)}
	for _, s := range svcs {
		if !allowedServices[s] {
			return nil, fmt.Errorf("unknown service %q", s)
		}
		// Debian/Ubuntu run Postfix as the templated instance postfix@-.service
		// (and the top-level postfix.service is just a wrapper), so match the
		// whole unit family with a glob; journalctl expands "-u name*".
		args = append(args, "-u", s+"*")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "journalctl", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}
	var lines []LogLine
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var raw struct {
			Message   any    `json:"MESSAGE"`
			Unit      string `json:"_SYSTEMD_UNIT"`
			Timestamp string `json:"__REALTIME_TIMESTAMP"`
		}
		if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
			continue
		}
		msg := ""
		switch v := raw.Message.(type) {
		case string:
			msg = v
		case []any: // journald encodes binary messages as byte arrays
			var b []byte
			for _, x := range v {
				if f, ok := x.(float64); ok {
					b = append(b, byte(f))
				}
			}
			msg = string(b)
		}
		ts := ""
		if us, err := strconv.ParseInt(raw.Timestamp, 10, 64); err == nil {
			ts = time.UnixMicro(us).UTC().Format(time.RFC3339)
		}
		lines = append(lines, LogLine{
			Time:    ts,
			Service: strings.TrimSuffix(raw.Unit, ".service"),
			Message: msg,
		})
	}
	return lines, sc.Err()
}

// ---- mock ----

type MockLogReader struct{}

func NewMockLogReader() *MockLogReader { return &MockLogReader{} }

func (m *MockLogReader) Tail(_ context.Context, svcs []string, n int) ([]LogLine, error) {
	base := time.Now().Add(-10 * time.Minute)
	samples := []LogLine{
		{Service: "postfix", Message: "connect from mail-ej1-f54.google.com[209.85.218.54]"},
		{Service: "postfix", Message: "1A2B3C4D5E: from=<friend@elsewhere.net>, size=5120, nrcpt=1 (queue active)"},
		{Service: "dovecot", Message: "lmtp(hello@example.com)<1234><abcd>: saved mail to INBOX"},
		{Service: "postfix", Message: "1A2B3C4D5E: removed"},
		{Service: "dovecot", Message: "imap-login: Login: user=<hello@example.com>, method=PLAIN, rip=127.0.0.1, secured"},
		{Service: "wispboxd", Message: "certificate mail.example.com renewed; postfix and dovecot reloaded"},
	}
	want := map[string]bool{}
	for _, s := range svcs {
		want[s] = true
	}
	var out []LogLine
	for i, l := range samples {
		if len(svcs) > 0 && !want[l.Service] {
			continue
		}
		l.Time = base.Add(time.Duration(i) * 90 * time.Second).UTC().Format(time.RFC3339)
		out = append(out, l)
		if len(out) >= n && n > 0 {
			break
		}
	}
	return out, nil
}
