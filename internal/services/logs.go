package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

const maxLogMessageBytes = 4096

func (j *JournalLogReader) Tail(ctx context.Context, svcs []string, n int) ([]LogLine, error) {
	if n <= 0 {
		n = 100
	} else if n > 500 {
		n = 500
	}
	if len(svcs) == 0 {
		svcs = []string{"postfix", "dovecot", "opendkim", "wispboxd"}
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
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}

	var lines []LogLine
	br := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := readJournalLine(br, 256*1024)
		if len(line) > 0 {
			if parsed, ok := parseJournalLine(line); ok {
				lines = append(lines, parsed)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = cmd.Wait()
			return nil, err
		}
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return nil, fmt.Errorf("journalctl: %s", msg)
	}
	return lines, nil
}

func parseJournalLine(line []byte) (LogLine, bool) {
	var raw struct {
		Message   any    `json:"MESSAGE"`
		Unit      string `json:"_SYSTEMD_UNIT"`
		Timestamp string `json:"__REALTIME_TIMESTAMP"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return LogLine{}, false
	}
	msg := ""
	switch v := raw.Message.(type) {
	case string:
		msg = clipLogMessage(v)
	case []any: // journald encodes binary messages as byte arrays
		var b []byte
		for _, x := range v {
			if f, ok := x.(float64); ok {
				if len(b) >= maxLogMessageBytes {
					break
				}
				b = append(b, byte(f))
			}
		}
		msg = clipLogMessage(string(b))
	}
	ts := ""
	if us, err := strconv.ParseInt(raw.Timestamp, 10, 64); err == nil {
		ts = time.UnixMicro(us).UTC().Format(time.RFC3339)
	}
	return LogLine{
		Time:    ts,
		Service: normalizeJournalService(raw.Unit),
		Message: msg,
	}, true
}

func readJournalLine(r *bufio.Reader, limit int) ([]byte, error) {
	var out []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(out) < limit {
			keep := len(part)
			if len(out)+keep > limit {
				keep = limit - len(out)
			}
			out = append(out, part[:keep]...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return out, err
		}
		return out, nil
	}
}

func normalizeJournalService(unit string) string {
	name := strings.TrimSuffix(unit, ".service")
	switch {
	case strings.HasPrefix(name, "postfix@") || name == "postfix":
		return "postfix"
	case strings.HasPrefix(name, "dovecot") || name == "dovecot":
		return "dovecot"
	case strings.HasPrefix(name, "wispboxd") || name == "wispboxd":
		return "wispboxd"
	case strings.HasPrefix(name, "opendkim") || name == "opendkim":
		return "opendkim"
	default:
		return name
	}
}

func clipLogMessage(s string) string {
	if len(s) <= maxLogMessageBytes {
		return s
	}
	return s[:maxLogMessageBytes] + "..."
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
