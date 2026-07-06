// Package smtpclient submits outgoing mail. The production adapter speaks
// SMTP to the local Postfix submission port with the user's own credentials,
// so Postfix enforces sender identity exactly as it would for any mail
// client. The mock adapter records sends and delivers locally in dev mode.
package smtpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/imapclient"
)

// Sender submits one raw RFC 5322 message.
type Sender interface {
	Send(ctx context.Context, creds auth.Credentials, from string, to []string, raw []byte) error
}

// ReaderSender submits a message without requiring the caller to hold the
// whole RFC 5322 payload in memory.
type ReaderSender interface {
	SendReader(ctx context.Context, creds auth.Credentials, from string, to []string, raw io.Reader) error
}

// ---- production: Postfix submission on loopback ----

// Submission authenticates against Postfix on 127.0.0.1:587.
type Submission struct {
	Addr string // 127.0.0.1:587
	// HeloName is used for EHLO; the primary mail hostname.
	HeloName string
	Timeout  time.Duration
}

func NewSubmission(addr, heloName string) *Submission {
	return &Submission{Addr: addr, HeloName: heloName, Timeout: 45 * time.Second}
}

func (s *Submission) Send(ctx context.Context, creds auth.Credentials, from string, to []string, raw []byte) error {
	return s.SendReader(ctx, creds, from, to, bytes.NewReader(raw))
}

func (s *Submission) SendReader(ctx context.Context, creds auth.Credentials, from string, to []string, raw io.Reader) error {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	d := net.Dialer{Timeout: minDuration(timeout, 15*time.Second)}
	conn, err := d.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("connect to mail submission service: %w", err)
	}
	// Bound the whole SMTP exchange so a stuck local Postfix submission
	// service returns a JSON error before the HTTP request times out.
	_ = conn.SetDeadline(time.Now().Add(timeout))
	host, _, _ := net.SplitHostPort(s.Addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("submission handshake: %w", err)
	}
	defer c.Close()

	if s.HeloName != "" {
		if err := c.Hello(s.HeloName); err != nil {
			return humanSubmissionError("submission EHLO", err)
		}
	}
	// Postfix requires TLS before AUTH on 587. The connection never leaves
	// loopback and the local certificate may be the self-signed fallback,
	// so certificate verification is intentionally skipped here.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, InsecureSkipVerify: true}); err != nil {
			return humanSubmissionError("submission STARTTLS", err)
		}
	}
	authn := smtp.PlainAuth("", creds.Email, creds.Password, host)
	if err := c.Auth(authn); err != nil {
		if isTimeout(err) {
			return humanSubmissionError("submission AUTH", err)
		}
		return fmt.Errorf("the mail server rejected your credentials; please sign in again")
	}
	if err := c.Mail(from); err != nil {
		return humanSMTPError(err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("recipient %s was rejected: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return humanSMTPError(err)
	}
	if _, err := io.Copy(w, raw); err != nil {
		return humanSubmissionError("send message body", err)
	}
	if err := w.Close(); err != nil {
		return humanSMTPError(err)
	}
	if err := c.Quit(); err != nil {
		return humanSubmissionError("submission QUIT", err)
	}
	return nil
}

func humanSMTPError(err error) error {
	if isTimeout(err) {
		return humanSubmissionError("mail submission", err)
	}
	s := err.Error()
	if strings.Contains(s, "Sender address rejected: not owned by user") {
		return fmt.Errorf("you can only send from your own address or an alias that forwards to you")
	}
	return fmt.Errorf("the mail server refused the message: %s", s)
}

func humanSubmissionError(op string, err error) error {
	if isTimeout(err) {
		return fmt.Errorf("%s timed out; check Postfix and relay connectivity", op)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// ---- mock (development and tests) ----

// SentRecord is one message captured by the mock sender.
type SentRecord struct {
	From string
	To   []string
	Raw  []byte
	At   time.Time
}

// MockSender records messages and, when a recipient is a seeded local user,
// delivers into their mock INBOX so dev flows feel real.
type MockSender struct {
	mu   sync.Mutex
	Sent []SentRecord
	// Mock IMAP store for local delivery; optional.
	IMAP *imapclient.Mock
	// FailNext forces the next send to fail (tests, error-path demos).
	FailNext error
}

func NewMockSender(imapMock *imapclient.Mock) *MockSender {
	return &MockSender{IMAP: imapMock}
}

func (m *MockSender) Send(_ context.Context, _ auth.Credentials, from string, to []string, raw []byte) error {
	m.mu.Lock()
	if m.FailNext != nil {
		err := m.FailNext
		m.FailNext = nil
		m.mu.Unlock()
		return err
	}
	m.Sent = append(m.Sent, SentRecord{From: from, To: append([]string(nil), to...), Raw: append([]byte(nil), raw...), At: time.Now()})
	imapMock := m.IMAP
	m.mu.Unlock()

	if imapMock != nil {
		for _, rcpt := range to {
			imapMock.DeliverLocal(rcpt, raw)
		}
	}
	return nil
}

func (m *MockSender) SendReader(ctx context.Context, creds auth.Credentials, from string, to []string, raw io.Reader) error {
	data, err := io.ReadAll(raw)
	if err != nil {
		return err
	}
	return m.Send(ctx, creds, from, to, data)
}

// SentCount reports how many messages were captured (tests).
func (m *MockSender) SentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Sent)
}
