package admin

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"time"

	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/delivery"
	"github.com/xiqi/wispbox/internal/security"
)

// relayView never exposes the encrypted password.
type relayView struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Provider  string     `json:"provider"`
	Host      string     `json:"host"`
	Port      int        `json:"port"`
	Username  string     `json:"username"`
	TLSMode   db.TLSMode `json:"tls_mode"`
	Enabled   bool       `json:"enabled"`
	HasSecret bool       `json:"has_secret"`
	CreatedAt string     `json:"created_at"`
}

func toRelayView(r *db.OutboundRelay) relayView {
	return relayView{
		ID: r.ID, Name: r.Name, Provider: r.Provider, Host: r.Host, Port: r.Port,
		Username: r.Username, TLSMode: r.TLSMode, Enabled: r.Enabled,
		HasSecret: r.EncryptedPassword != "", CreatedAt: r.CreatedAt,
	}
}

func (h *Handlers) listRelays(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	relays, err := h.Core.Store.ListRelays(r.Context())
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	views := []relayView{}
	for i := range relays {
		views = append(views, toRelayView(&relays[i]))
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"relays": views})
}

func (h *Handlers) relayPresets(w http.ResponseWriter, _ *http.Request, _ *adminCtx) {
	httpjson.Write(w, http.StatusOK, map[string]any{"presets": delivery.Presets()})
}

type relayRequest struct {
	Name     string     `json:"name"`
	Provider string     `json:"provider"`
	Host     string     `json:"host"`
	Port     int        `json:"port"`
	Username string     `json:"username"`
	Password string     `json:"password"` // plaintext in transit, encrypted at rest
	TLSMode  db.TLSMode `json:"tls_mode"`
	Enabled  *bool      `json:"enabled"`
}

func (h *Handlers) createRelay(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req relayRequest
	if err := httpjson.Decode(w, r, &req, 64<<10); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	relay, warning, err := h.Core.SaveRelay(r.Context(), 0, req.Name, req.Provider, req.Host, req.Port, req.Username, req.Password, req.TLSMode, enabled)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "relay_create", "relay", relay.Name)
	httpjson.Write(w, http.StatusCreated, map[string]any{"relay": toRelayView(relay), "warning": warning})
}

func (h *Handlers) updateRelay(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	existing, err := h.Core.Store.GetRelay(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	req := relayRequest{
		Name: existing.Name, Provider: existing.Provider, Host: existing.Host,
		Port: existing.Port, Username: existing.Username, TLSMode: existing.TLSMode,
	}
	if err := httpjson.Decode(w, r, &req, 64<<10); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	relay, warning, err := h.Core.SaveRelay(r.Context(), id, req.Name, req.Provider, req.Host, req.Port, req.Username, req.Password, req.TLSMode, enabled)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "relay_update", "relay", relay.Name)
	httpjson.Write(w, http.StatusOK, map[string]any{"relay": toRelayView(relay), "warning": warning})
}

func (h *Handlers) deleteRelay(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	relay, warning, err := h.Core.DeleteRelay(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "relay_delete", "relay", relay.Name)
	httpjson.Write(w, http.StatusOK, map[string]any{"warning": warning})
}

// testRelay opens a real SMTP connection to the relay, negotiates TLS, and
// authenticates — without sending any mail.
func (h *Handlers) testRelay(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	relay, err := h.Core.Store.GetRelay(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	password := ""
	if relay.EncryptedPassword != "" {
		var derr error
		password, derr = security.Decrypt(h.Core.Secret, relay.EncryptedPassword)
		if derr != nil {
			httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("stored relay password cannot be decrypted; re-enter it"))
			return
		}
	}
	h.auditLog(r, ac, "relay_test", "relay", relay.Name)
	if err := h.TestMailer.TestRelay(r.Context(), relay, password); err != nil {
		httpjson.Write(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- delivery policies ----

func (h *Handlers) listPolicies(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	policies, err := h.Core.Store.ListPolicies(r.Context())
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	global, err := h.Core.Engine.ResolveGlobal(r.Context())
	resolvedGlobal := map[string]any{}
	if err == nil {
		resolvedGlobal["mode"] = global.Mode
		if global.Relay != nil {
			resolvedGlobal["relay_name"] = global.Relay.Name
		}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"policies":              policies,
		"effective_global":      resolvedGlobal,
		"outbound_smtp_25_open": h.Core.OutboundSMTP25Status(r.Context()),
	})
}

func (h *Handlers) upsertPolicy(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req struct {
		ScopeType db.PolicyScope  `json:"scope_type"`
		ScopeID   int64           `json:"scope_id"`
		Mode      db.DeliveryMode `json:"mode"`
		RelayID   *int64          `json:"relay_id"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	switch req.ScopeType {
	case db.ScopeGlobal:
		req.ScopeID = 0
	case db.ScopeDomain:
		if _, err := h.Core.Store.GetDomain(r.Context(), req.ScopeID); err != nil {
			httpjson.Fail(w, err)
			return
		}
	default:
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("scope must be global or domain"))
		return
	}
	policy, warning, err := h.Core.UpsertPolicy(r.Context(), req.ScopeType, req.ScopeID, req.Mode, req.RelayID)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "policy_set", "policy", fmt.Sprintf("%s/%d", req.ScopeType, req.ScopeID))
	httpjson.Write(w, http.StatusOK, map[string]any{"policy": policy, "warning": warning})
}

func (h *Handlers) deletePolicy(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	warning, err := h.Core.DeletePolicy(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "policy_delete", "policy", strconv.FormatInt(id, 10))
	httpjson.Write(w, http.StatusOK, map[string]any{"warning": warning})
}

// testEmail sends a test message from the server to an external address.
func (h *Handlers) testEmail(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	body := "This is a wispbox test message.\n\nIf you are reading this, outbound delivery works.\nSent at " + time.Now().UTC().Format(time.RFC1123) + "."
	if to, ok := SendTestEmail(w, r, h.Core.Store, h.TestMailer, "wispbox test message", body); ok {
		h.auditLog(r, ac, "test_email", "address", to)
	}
}

// ---- TestMailer adapters ----

// TestMailer performs outbound checks that need host access in production.
type TestMailer interface {
	// TestRelay connects and authenticates to a relay without sending mail.
	TestRelay(ctx context.Context, relay *db.OutboundRelay, password string) error
	// SendTest submits a plain test message using local injection.
	SendTest(ctx context.Context, from, to, subject, body string) error
}

// SMTPTestMailer is the production adapter: real sockets, real sendmail.
type SMTPTestMailer struct {
	SendmailPath string // usually /usr/sbin/sendmail
}

func (t *SMTPTestMailer) TestRelay(ctx context.Context, relay *db.OutboundRelay, password string) error {
	addr := net.JoinHostPort(relay.Host, strconv.Itoa(relay.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("could not reach %s: %v", addr, err)
	}
	defer conn.Close()

	if relay.TLSMode == db.TLSModeTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: relay.Host})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("TLS handshake with %s failed: %v", relay.Host, err)
		}
		conn = tlsConn
	}
	c, err := smtp.NewClient(conn, relay.Host)
	if err != nil {
		return fmt.Errorf("SMTP greeting failed: %v", err)
	}
	defer c.Close()
	if relay.TLSMode == db.TLSModeStartTLS {
		if err := c.StartTLS(&tls.Config{ServerName: relay.Host}); err != nil {
			return fmt.Errorf("STARTTLS failed: %v", err)
		}
	}
	if relay.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", relay.Username, password, relay.Host)); err != nil {
			return fmt.Errorf("authentication failed: %v", err)
		}
	}
	return c.Quit()
}

func (t *SMTPTestMailer) SendTest(ctx context.Context, from, to, subject, body string) error {
	return sendmailInject(ctx, t.SendmailPath, from, to, subject, body)
}

// MockTestMailer is the development adapter: always succeeds and records the
// last test message for API tests.
type MockTestMailer struct {
	Fail        bool
	LastFrom    string
	LastTo      string
	LastSubject string
	LastBody    string
}

func (t *MockTestMailer) TestRelay(context.Context, *db.OutboundRelay, string) error {
	if t.Fail {
		return fmt.Errorf("simulated relay failure (dev mode)")
	}
	return nil
}

func (t *MockTestMailer) SendTest(_ context.Context, from, to, subject, body string) error {
	if t.Fail {
		return fmt.Errorf("simulated send failure (dev mode)")
	}
	t.LastFrom = from
	t.LastTo = to
	t.LastSubject = subject
	t.LastBody = body
	return nil
}
