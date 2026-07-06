// Package api_test exercises the assembled HTTP surface end to end: the real
// router, real SQLite store, and the development-mode mock adapters. No
// network access and no root are required.
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/xiqi/wispbox/internal/admin"
	"github.com/xiqi/wispbox/internal/api"
	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/buildinfo"
	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/smtpclient"
)

const (
	seedAdminUsername = "admin"
	seedAdminPassword = "wispbox-admin"
	seedMailEmail     = "demo@example.com"
	seedMailPassword  = "wispbox-demo"
)

// newTestServer builds a development-mode app rooted in a temp dir and serves
// its full handler via httptest. seed populates the demo admin/domain/mailbox
// and marks setup complete.
func newTestServer(t *testing.T, seed bool) (*api.App, *httptest.Server) {
	t.Helper()
	ctx := context.Background()
	cfg := config.DevelopmentDefaults(t.TempDir())
	app, err := api.NewApp(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(func() { app.Close() })
	if seed {
		if err := app.SeedDev(ctx); err != nil {
			t.Fatalf("SeedDev: %v", err)
		}
	}
	handler, err := app.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return app, srv
}

// testClient is an http.Client with a cookie jar, a base URL, and the CSRF
// token captured at login. Redirects are never followed so tests can assert
// on them.
type testClient struct {
	t    *testing.T
	base string
	hc   *http.Client
	csrf string
}

func newClient(t *testing.T, srv *httptest.Server) *testClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &testClient{
		t:    t,
		base: srv.URL,
		hc: &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// do sends one request. body (if non-nil) is JSON-encoded; csrf (if non-empty)
// is sent as the X-wispbox-CSRF header. The response body is decoded as JSON
// when possible; non-JSON bodies leave the map nil.
func (c *testClient) do(method, path string, body any, csrf string) (*http.Response, map[string]any) {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal request body: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		c.t.Fatalf("NewRequest %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set(auth.CSRFHeader, csrf)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

// get/post/patch/del send authenticated-style requests: mutations carry the
// client's CSRF token automatically.
func (c *testClient) get(path string) (int, map[string]any) {
	resp, m := c.do(http.MethodGet, path, nil, "")
	return resp.StatusCode, m
}

func (c *testClient) post(path string, body any) (int, map[string]any) {
	resp, m := c.do(http.MethodPost, path, body, c.csrf)
	return resp.StatusCode, m
}

func (c *testClient) patch(path string, body any) (int, map[string]any) {
	resp, m := c.do(http.MethodPatch, path, body, c.csrf)
	return resp.StatusCode, m
}

func (c *testClient) del(path string) (int, map[string]any) {
	resp, m := c.do(http.MethodDelete, path, nil, c.csrf)
	return resp.StatusCode, m
}

func (c *testClient) loginAdmin(username, password string) {
	c.t.Helper()
	status, body := c.post("/api/admin/login", map[string]any{"username": username, "password": password})
	if status != http.StatusOK {
		c.t.Fatalf("admin login: status = %d, body %v", status, body)
	}
	c.csrf = str(c.t, body, "csrf")
}

func (c *testClient) loginMail(email, password string) {
	c.t.Helper()
	status, body := c.post("/api/mail/login", map[string]any{"email": email, "password": password})
	if status != http.StatusOK {
		c.t.Fatalf("mail login: status = %d, body %v", status, body)
	}
	c.csrf = str(c.t, body, "csrf")
}

// ---- JSON field helpers ----

func field(t *testing.T, m map[string]any, key string) any {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("response is missing %q: %v", key, m)
	}
	return v
}

func str(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	s, ok := field(t, m, key).(string)
	if !ok {
		t.Fatalf("field %q is %T, want string: %v", key, m[key], m)
	}
	return s
}

func boolean(t *testing.T, m map[string]any, key string) bool {
	t.Helper()
	b, ok := field(t, m, key).(bool)
	if !ok {
		t.Fatalf("field %q is %T, want bool: %v", key, m[key], m)
	}
	return b
}

func num(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	f, ok := field(t, m, key).(float64)
	if !ok {
		t.Fatalf("field %q is %T, want number: %v", key, m[key], m)
	}
	return int64(f)
}

func obj(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	o, ok := field(t, m, key).(map[string]any)
	if !ok {
		t.Fatalf("field %q is %T, want object: %v", key, m[key], m)
	}
	return o
}

func list(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	l, ok := field(t, m, key).([]any)
	if !ok {
		t.Fatalf("field %q is %T, want array: %v", key, m[key], m)
	}
	return l
}

// findMessage lists a folder and returns the id and header of the first
// message with the given subject, or ok=false.
func findMessage(t *testing.T, c *testClient, folder, subject string) (id string, msg map[string]any, ok bool) {
	t.Helper()
	status, body := c.get("/api/mail/messages?folder=" + folder)
	if status != http.StatusOK {
		t.Fatalf("list %s: status = %d, body %v", folder, status, body)
	}
	for _, v := range list(t, body, "messages") {
		m, isMap := v.(map[string]any)
		if !isMap {
			t.Fatalf("message entry is %T, want object", v)
		}
		if str(t, m, "subject") == subject {
			return str(t, m, "id"), m, true
		}
	}
	return "", nil, false
}

// ---- (1) route authorization and session separation ----

func TestRouteAuthorization(t *testing.T) {
	_, srv := newTestServer(t, true)

	anon := newClient(t, srv)
	for _, tc := range []struct {
		name string
		path string
	}{
		{"admin endpoint without session", "/api/admin/overview"},
		{"mail endpoint without session", "/api/mail/folders"},
	} {
		if status, body := anon.get(tc.path); status != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401 (body %v)", tc.name, status, body)
		}
	}

	// An admin session must not open the mail API.
	adminC := newClient(t, srv)
	adminC.loginAdmin(seedAdminUsername, seedAdminPassword)
	if status, _ := adminC.get("/api/admin/overview"); status != http.StatusOK {
		t.Fatalf("admin session on /api/admin/overview: status = %d, want 200", status)
	}
	if status, _ := adminC.get("/api/mail/folders"); status != http.StatusUnauthorized {
		t.Errorf("admin session on /api/mail/folders: status = %d, want 401", status)
	}

	// A mailbox session must not open the admin API.
	mailC := newClient(t, srv)
	mailC.loginMail(seedMailEmail, seedMailPassword)
	if status, _ := mailC.get("/api/mail/folders"); status != http.StatusOK {
		t.Fatalf("mail session on /api/mail/folders: status = %d, want 200", status)
	}
	if status, _ := mailC.get("/api/admin/overview"); status != http.StatusUnauthorized {
		t.Errorf("mail session on /api/admin/overview: status = %d, want 401", status)
	}
}

func TestAdminOverviewShowsQueuedDeliveryErrors(t *testing.T) {
	_, srv := newTestServer(t, true)
	c := newClient(t, srv)
	c.loginAdmin(seedAdminUsername, seedAdminPassword)

	status, body := c.get("/api/admin/overview")
	if status != http.StatusOK {
		t.Fatalf("overview: status = %d, body %v", status, body)
	}
	raw, ok := field(t, body, "recent_errors").([]any)
	if !ok {
		t.Fatalf("recent_errors is %T, want array: %v", body["recent_errors"], body)
	}
	for _, item := range raw {
		ev, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("recent_errors item is %T, want object: %v", item, item)
		}
		if str(t, ev, "service") == "postfix" &&
			str(t, ev, "event_type") == "delivery" &&
			strings.Contains(str(t, ev, "message"), "Connection timed out") {
			return
		}
	}
	t.Fatalf("recent_errors = %v, want queued delivery timeout", raw)
}

// ---- (2) CSRF ----

func TestCSRFRequiredOnAdminMutations(t *testing.T) {
	_, srv := newTestServer(t, true)
	c := newClient(t, srv)
	c.loginAdmin(seedAdminUsername, seedAdminPassword)

	// Authenticated POST without the CSRF header is rejected.
	resp, body := c.do(http.MethodPost, "/api/admin/domains",
		map[string]any{"name": "csrf.example"}, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without CSRF header: status = %d, want 403 (body %v)", resp.StatusCode, body)
	}

	// The same request with the header succeeds.
	status, body := c.post("/api/admin/domains", map[string]any{"name": "csrf.example"})
	if status != http.StatusCreated {
		t.Fatalf("POST with CSRF header: status = %d, want 201 (body %v)", status, body)
	}
	dom := obj(t, body, "domain")
	if got := str(t, dom, "mail_hostname"); got != "mail.csrf.example" {
		t.Errorf("mail_hostname = %q, want mail.csrf.example", got)
	}
}

func TestDomainAppearanceOverridesBrandByHost(t *testing.T) {
	_, srv := newTestServer(t, true)
	c := newClient(t, srv)
	c.loginAdmin(seedAdminUsername, seedAdminPassword)

	status, body := c.post("/api/admin/domains", map[string]any{"name": "brand.example"})
	if status != http.StatusCreated {
		t.Fatalf("create domain: status = %d, want 201 (body %v)", status, body)
	}

	key := "brand_domain:brand.example:brand_name"
	status, body = c.patch("/api/admin/settings", map[string]any{key: "Brand Mail"})
	if status != http.StatusOK {
		t.Fatalf("patch domain brand: status = %d, want 200 (body %v)", status, body)
	}
	settings := obj(t, body, "settings")
	if got := str(t, settings, key); got != "Brand Mail" {
		t.Fatalf("settings[%q] = %q, want Brand Mail", key, got)
	}

	req, err := http.NewRequest(http.MethodGet, c.base+"/api/brand", nil)
	if err != nil {
		t.Fatalf("NewRequest /api/brand: %v", err)
	}
	req.Host = "mail.brand.example"
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("GET /api/brand: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/brand: %v", err)
	}
	var brandBody map[string]any
	if err := json.Unmarshal(raw, &brandBody); err != nil {
		t.Fatalf("decode /api/brand %q: %v", raw, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/brand status = %d, want 200 (body %v)", resp.StatusCode, brandBody)
	}
	brand := obj(t, brandBody, "brand")
	if got := str(t, brand, "name"); got != "Brand Mail" {
		t.Fatalf("brand.name = %q, want Brand Mail", got)
	}

	status, body = c.patch("/api/admin/settings", map[string]any{
		"brand_domain:missing.example:brand_name": "Missing",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("patch missing domain brand: status = %d, want 400 (body %v)", status, body)
	}
}

func TestDirectDeliveryRequiresOutbound25(t *testing.T) {
	app, srv := newTestServer(t, true)
	app.Cfg.Mode = config.ModeProduction
	app.AdminH.Core.OutboundSMTP25Open = func(context.Context) bool { return false }
	c := newClient(t, srv)
	c.loginAdmin(seedAdminUsername, seedAdminPassword)

	status, body := c.post("/api/admin/delivery-policies", map[string]any{
		"scope_type": "global", "mode": "direct",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("set direct with blocked outbound 25: status = %d, want 400 (body %v)", status, body)
	}
	if msg := str(t, body, "error"); !strings.Contains(msg, "outbound port 25") || !strings.Contains(msg, "relay") {
		t.Fatalf("blocked direct error = %q, want port 25 relay guidance", msg)
	}
}

func TestUpgradeDoesNotStartWhenAlreadyLatest(t *testing.T) {
	app, srv := newTestServer(t, true)
	app.Cfg.Mode = config.ModeProduction
	app.AdminH.LatestVersion = func(context.Context) (string, error) {
		return "v" + buildinfo.Version, nil
	}
	c := newClient(t, srv)
	c.loginAdmin(seedAdminUsername, seedAdminPassword)

	status, body := c.get("/api/admin/upgrade")
	if status != http.StatusOK {
		t.Fatalf("upgrade status: status = %d, body %v", status, body)
	}
	if got := str(t, body, "latest_version"); got != buildinfo.Version {
		t.Fatalf("latest_version = %q, want %q", got, buildinfo.Version)
	}
	if boolean(t, body, "update_available") {
		t.Fatalf("update_available = true, want false")
	}

	status, body = c.post("/api/admin/upgrade", nil)
	if status != http.StatusConflict {
		t.Fatalf("start already-latest upgrade: status = %d, want 409 (body %v)", status, body)
	}
	if msg := str(t, body, "error"); !strings.Contains(msg, "up to date") {
		t.Fatalf("already-latest error = %q, want up to date guidance", msg)
	}
}

func TestAdminCertificateIssue(t *testing.T) {
	_, srv := newTestServer(t, true)
	c := newClient(t, srv)
	c.loginAdmin(seedAdminUsername, seedAdminPassword)

	status, body := c.get("/api/admin/certificates")
	if status != http.StatusOK {
		t.Fatalf("list certificates: status = %d, body %v", status, body)
	}
	if got := str(t, body, "primary_hostname"); got != "mail.example.com" {
		t.Errorf("primary_hostname = %q, want mail.example.com", got)
	}

	status, body = c.post("/api/admin/certificates/admin/issue", nil)
	if status != http.StatusAccepted {
		t.Fatalf("issue admin certificate: status = %d, body %v", status, body)
	}
	cert := obj(t, body, "certificate")
	if got := str(t, cert, "hostname"); got != "mail.example.com" {
		t.Errorf("certificate hostname = %q, want mail.example.com", got)
	}
}

// ---- (3) account security ----

func TestAccountPasswordChanges(t *testing.T) {
	_, srv := newTestServer(t, true)

	adminC := newClient(t, srv)
	adminC.loginAdmin(seedAdminUsername, seedAdminPassword)
	status, body := adminC.post("/api/admin/account/password", map[string]any{
		"current_password": seedAdminPassword,
		"new_password":     "new-admin-pass-123",
	})
	if status != http.StatusOK {
		t.Fatalf("admin password change: status = %d, body %v", status, body)
	}

	oldAdmin := newClient(t, srv)
	status, _ = oldAdmin.post("/api/admin/login", map[string]any{"username": seedAdminUsername, "password": seedAdminPassword})
	if status != http.StatusUnauthorized {
		t.Fatalf("old admin password login status = %d, want 401", status)
	}
	newAdmin := newClient(t, srv)
	newAdmin.loginAdmin(seedAdminUsername, "new-admin-pass-123")

	mailC := newClient(t, srv)
	mailC.loginMail(seedMailEmail, seedMailPassword)
	status, body = mailC.post("/api/mail/account/password", map[string]any{
		"current_password": seedMailPassword,
		"new_password":     "new-mail-pass-123",
	})
	if status != http.StatusOK {
		t.Fatalf("mail password change: status = %d, body %v", status, body)
	}
	if status, body = mailC.get("/api/mail/folders"); status != http.StatusOK {
		t.Fatalf("mail session after password change: status = %d, body %v", status, body)
	}

	oldMail := newClient(t, srv)
	status, _ = oldMail.post("/api/mail/login", map[string]any{"email": seedMailEmail, "password": seedMailPassword})
	if status != http.StatusUnauthorized {
		t.Fatalf("old mailbox password login status = %d, want 401", status)
	}
	newMail := newClient(t, srv)
	newMail.loginMail(seedMailEmail, "new-mail-pass-123")
}

func TestAccountTOTPAndPasskeyOptions(t *testing.T) {
	_, srv := newTestServer(t, true)

	adminC := newClient(t, srv)
	adminC.loginAdmin(seedAdminUsername, seedAdminPassword)
	status, body := adminC.post("/api/admin/account/2fa/setup", nil)
	if status != http.StatusOK {
		t.Fatalf("admin totp setup: status = %d, body %v", status, body)
	}
	adminChallengeID := str(t, body, "challenge_id")
	adminCode, err := totp.GenerateCode(str(t, body, "secret"), time.Now())
	if err != nil {
		t.Fatalf("generate admin totp: %v", err)
	}
	status, body = adminC.post("/api/admin/account/2fa/enable", map[string]any{
		"challenge_id": adminChallengeID,
		"code":         adminCode,
	})
	if status != http.StatusOK {
		t.Fatalf("admin totp enable: status = %d, body %v", status, body)
	}

	needsCode := newClient(t, srv)
	status, body = needsCode.post("/api/admin/login", map[string]any{"username": seedAdminUsername, "password": seedAdminPassword})
	if status != http.StatusAccepted || !boolean(t, body, "two_factor_required") {
		t.Fatalf("admin login without totp: status = %d, body %v", status, body)
	}
	withCode := newClient(t, srv)
	status, body = withCode.post("/api/admin/login", map[string]any{
		"username":  seedAdminUsername,
		"password":  seedAdminPassword,
		"totp_code": adminCode,
	})
	if status != http.StatusOK {
		t.Fatalf("admin login with totp: status = %d, body %v", status, body)
	}

	status, body = adminC.post("/api/admin/account/2fa/disable", map[string]any{"code": adminCode})
	if status != http.StatusOK {
		t.Fatalf("admin totp disable: status = %d, body %v", status, body)
	}

	status, body = adminC.post("/api/admin/account/passkeys/register/options", nil)
	if status != http.StatusOK || str(t, body, "challenge_id") == "" || obj(t, body, "options") == nil {
		t.Fatalf("admin passkey register options: status = %d, body %v", status, body)
	}
	status, body = newClient(t, srv).post("/api/admin/passkeys/login/options", nil)
	if status != http.StatusOK || str(t, body, "challenge_id") == "" || obj(t, body, "options") == nil {
		t.Fatalf("admin passkey login options: status = %d, body %v", status, body)
	}

	mailC := newClient(t, srv)
	mailC.loginMail(seedMailEmail, seedMailPassword)
	status, body = mailC.post("/api/mail/account/2fa/setup", nil)
	if status != http.StatusOK {
		t.Fatalf("mail totp setup: status = %d, body %v", status, body)
	}
	mailChallengeID := str(t, body, "challenge_id")
	mailCode, err := totp.GenerateCode(str(t, body, "secret"), time.Now())
	if err != nil {
		t.Fatalf("generate mail totp: %v", err)
	}
	status, body = mailC.post("/api/mail/account/2fa/enable", map[string]any{
		"challenge_id": mailChallengeID,
		"code":         mailCode,
	})
	if status != http.StatusOK {
		t.Fatalf("mail totp enable: status = %d, body %v", status, body)
	}
	status, body = mailC.post("/api/mail/account/2fa/disable", map[string]any{"code": mailCode})
	if status != http.StatusOK {
		t.Fatalf("mail totp disable: status = %d, body %v", status, body)
	}
	status, body = mailC.post("/api/mail/account/passkeys/register/options", nil)
	if status != http.StatusOK || str(t, body, "challenge_id") == "" || obj(t, body, "options") == nil {
		t.Fatalf("mail passkey register options: status = %d, body %v", status, body)
	}
	status, body = newClient(t, srv).post("/api/mail/passkeys/login/options", nil)
	if status != http.StatusOK || str(t, body, "challenge_id") == "" || obj(t, body, "options") == nil {
		t.Fatalf("mail passkey login options: status = %d, body %v", status, body)
	}
}

// ---- (4) admin CRUD ----

func TestAdminCRUD(t *testing.T) {
	_, srv := newTestServer(t, true)
	c := newClient(t, srv)
	c.loginAdmin(seedAdminUsername, seedAdminPassword)

	// Create domain: default mail hostname is mail.<domain>.
	status, body := c.post("/api/admin/domains", map[string]any{"name": "shop.example"})
	if status != http.StatusCreated {
		t.Fatalf("create domain: status = %d, body %v", status, body)
	}
	dom := obj(t, body, "domain")
	domainID := num(t, dom, "id")
	if got := str(t, dom, "mail_hostname"); got != "mail.shop.example" {
		t.Errorf("default mail_hostname = %q, want mail.shop.example", got)
	}

	// Duplicate domain: 4xx with a human-readable message.
	status, body = c.post("/api/admin/domains", map[string]any{"name": "shop.example"})
	if status < 400 || status > 499 {
		t.Fatalf("duplicate domain: status = %d, want 4xx (body %v)", status, body)
	}
	if msg := str(t, body, "error"); !strings.Contains(msg, "already exists") {
		t.Errorf("duplicate domain error = %q, want mention of already exists", msg)
	}

	// Create mailbox.
	status, body = c.post("/api/admin/mailboxes", map[string]any{
		"domain_id": domainID, "local_part": "sales", "password": "orange-crumble-42", "quota_mb": 512,
	})
	if status != http.StatusCreated {
		t.Fatalf("create mailbox: status = %d, body %v", status, body)
	}
	mb := obj(t, body, "mailbox")
	mailboxID := num(t, mb, "id")
	if got := str(t, mb, "email"); got != "sales@shop.example" {
		t.Errorf("mailbox email = %q, want sales@shop.example", got)
	}

	// Disable via PATCH.
	status, body = c.patch("/api/admin/mailboxes/"+itoa(mailboxID), map[string]any{"enabled": false})
	if status != http.StatusOK {
		t.Fatalf("disable mailbox: status = %d, body %v", status, body)
	}
	if boolean(t, obj(t, body, "mailbox"), "enabled") {
		t.Errorf("mailbox still enabled after PATCH enabled=false")
	}

	// Reset password with generate=true returns a fresh password.
	status, body = c.post("/api/admin/mailboxes/"+itoa(mailboxID)+"/reset-password",
		map[string]any{"generate": true})
	if status != http.StatusOK {
		t.Fatalf("reset password: status = %d, body %v", status, body)
	}
	if pw := str(t, body, "generated_password"); len(pw) < 10 {
		t.Errorf("generated_password = %q, want at least 10 characters", pw)
	}

	// Alias and catch-all.
	status, body = c.post("/api/admin/aliases", map[string]any{
		"domain_id": domainID, "source": "info", "destination": "sales@shop.example",
	})
	if status != http.StatusCreated {
		t.Fatalf("create alias: status = %d, body %v", status, body)
	}
	if got := str(t, obj(t, body, "alias"), "source"); got != "info@shop.example" {
		t.Errorf("alias source = %q, want info@shop.example", got)
	}
	status, body = c.post("/api/admin/aliases", map[string]any{
		"domain_id": domainID, "is_catch_all": true, "destination": "sales@shop.example",
	})
	if status != http.StatusCreated {
		t.Fatalf("create catch-all alias: status = %d, body %v", status, body)
	}
	catchAll := obj(t, body, "alias")
	if got := str(t, catchAll, "source"); got != "@shop.example" {
		t.Errorf("catch-all source = %q, want @shop.example", got)
	}
	if !boolean(t, catchAll, "is_catch_all") {
		t.Errorf("catch-all alias has is_catch_all = false")
	}

	// Create relay: the password must never be echoed back.
	status, body = c.post("/api/admin/relays", map[string]any{
		"name": "outbound", "provider": "postmark", "host": "smtp.postmarkapp.com",
		"port": 587, "username": "server-token", "password": "relay-secret-123",
		"tls_mode": "starttls",
	})
	if status != http.StatusCreated {
		t.Fatalf("create relay: status = %d, body %v", status, body)
	}
	relay := obj(t, body, "relay")
	relayID := num(t, relay, "id")
	if _, exists := relay["password"]; exists {
		t.Errorf("relay response echoes a password field: %v", relay)
	}
	if _, exists := relay["encrypted_password"]; exists {
		t.Errorf("relay response leaks encrypted_password: %v", relay)
	}
	if !boolean(t, relay, "has_secret") {
		t.Errorf("relay has_secret = false, want true after setting a password")
	}

	// Global relay policy, then a per-domain direct policy.
	status, body = c.post("/api/admin/delivery-policies", map[string]any{
		"scope_type": "global", "mode": "relay", "relay_id": relayID,
	})
	if status != http.StatusOK {
		t.Fatalf("set global relay policy: status = %d, body %v", status, body)
	}
	if got := str(t, obj(t, body, "policy"), "mode"); got != "relay" {
		t.Errorf("global policy mode = %q, want relay", got)
	}
	status, body = c.post("/api/admin/delivery-policies", map[string]any{
		"scope_type": "domain", "scope_id": domainID, "mode": "direct",
	})
	if status != http.StatusOK {
		t.Fatalf("set domain policy: status = %d, body %v", status, body)
	}
	policy := obj(t, body, "policy")
	if got := str(t, policy, "scope_type"); got != "domain" {
		t.Errorf("policy scope_type = %q, want domain", got)
	}
	if got := num(t, policy, "scope_id"); got != domainID {
		t.Errorf("policy scope_id = %d, want %d", got, domainID)
	}

	// Deleting a relay that a policy still uses must be rejected.
	status, body = c.del("/api/admin/relays/" + itoa(relayID))
	if status < 400 || status > 499 {
		t.Fatalf("delete relay in use: status = %d, want 4xx (body %v)", status, body)
	}
	if msg := str(t, body, "error"); !strings.Contains(msg, "used by") {
		t.Errorf("delete relay in use error = %q, want mention of policies using it", msg)
	}
}

// ---- (4) mail API against the mock adapters ----

func TestMailAPI(t *testing.T) {
	app, srv := newTestServer(t, true)
	c := newClient(t, srv)
	mockSMTP, ok := app.SMTP.(*smtpclient.MockSender)
	if !ok {
		t.Fatalf("SMTP = %T, want *smtpclient.MockSender", app.SMTP)
	}

	// Wrong password is a 401.
	status, body := c.post("/api/mail/login",
		map[string]any{"email": seedMailEmail, "password": "definitely-wrong"})
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong-password login: status = %d, want 401 (body %v)", status, body)
	}
	c.loginMail(seedMailEmail, seedMailPassword)

	// Folders include the standard set.
	status, body = c.get("/api/mail/folders")
	if status != http.StatusOK {
		t.Fatalf("folders: status = %d, body %v", status, body)
	}
	have := map[string]bool{}
	for _, v := range list(t, body, "folders") {
		have[str(t, v.(map[string]any), "name")] = true
	}
	for _, want := range []string{"INBOX", "Sent", "Drafts", "Junk", "Trash"} {
		if !have[want] {
			t.Errorf("folders missing %q (got %v)", want, have)
		}
	}

	// INBOX listing returns seeded messages with opaque ids.
	status, body = c.get("/api/mail/messages?folder=INBOX")
	if status != http.StatusOK {
		t.Fatalf("list INBOX: status = %d, body %v", status, body)
	}
	msgs := list(t, body, "messages")
	if len(msgs) == 0 {
		t.Fatalf("INBOX is empty, want seeded messages")
	}
	for i, v := range msgs {
		if str(t, v.(map[string]any), "id") == "" {
			t.Errorf("message %d has an empty id", i)
		}
	}

	// The newsletter's HTML is sanitized: no scripts, remote tracker images
	// stripped, and has_blocked_images offered to the client.
	newsletterID, _, found := findMessage(t, c, "INBOX", "Issue 118: Small servers are back")
	if !found {
		t.Fatalf("seeded newsletter not found in INBOX")
	}
	status, body = c.get("/api/mail/messages/" + newsletterID)
	if status != http.StatusOK {
		t.Fatalf("get newsletter: status = %d, body %v", status, body)
	}
	html := str(t, body, "html_body")
	if html == "" {
		t.Fatalf("newsletter html_body is empty")
	}
	if strings.Contains(strings.ToLower(html), "<script") {
		t.Errorf("sanitized html still contains <script: %q", html)
	}
	if strings.Contains(html, "tracker.sidecar.news") {
		t.Errorf("sanitized html still references the tracking pixel: %q", html)
	}
	if strings.Contains(html, "https://") {
		t.Errorf("sanitized html still loads remote content: %q", html)
	}
	if !boolean(t, body, "has_blocked_images") {
		t.Errorf("has_blocked_images = false, want true for the newsletter")
	}

	// Reply recipients are editable: the API must honor explicit To/Cc/Bcc
	// instead of silently re-deriving them from the original message.
	replyBefore := mockSMTP.SentCount()
	const customReplySubject = "Custom reply subject"
	status, body = c.post("/api/mail/reply", map[string]any{
		"id": newsletterID, "to": "custom-to@example.net", "cc": "custom-cc@example.net",
		"bcc": "custom-bcc@example.net", "subject": customReplySubject, "body": "edited reply",
	})
	if status != http.StatusOK {
		t.Fatalf("custom reply: status = %d, body %v", status, body)
	}
	if got := mockSMTP.SentCount(); got != replyBefore+1 {
		t.Fatalf("mock sent count = %d, want %d", got, replyBefore+1)
	}
	replyRaw := string(mockSMTP.Sent[replyBefore].Raw)
	if got := strings.Join(mockSMTP.Sent[replyBefore].To, ","); got != "custom-to@example.net,custom-cc@example.net,custom-bcc@example.net" {
		t.Errorf("custom reply envelope recipients = %q", got)
	}
	if !strings.Contains(replyRaw, "Subject: "+customReplySubject) {
		t.Errorf("custom reply subject not found in raw message:\n%s", replyRaw)
	}
	if strings.Contains(strings.ToLower(replyRaw), "\nbcc:") {
		t.Errorf("custom reply leaked Bcc header:\n%s", replyRaw)
	}

	// Sending JSON (no attachments) succeeds and lands in Sent.
	const sentSubject = "API test: hello from api_test"
	status, body = c.post("/api/mail/send", map[string]any{
		"to": "mira@example.org", "subject": sentSubject, "body": "hello there",
	})
	if status != http.StatusOK {
		t.Fatalf("send: status = %d, body %v", status, body)
	}
	if _, _, found := findMessage(t, c, "Sent", sentSubject); !found {
		t.Errorf("sent message %q not found in Sent", sentSubject)
	}

	// A forged From is rejected before anything is sent.
	status, body = c.post("/api/mail/send", map[string]any{
		"from": "admin@example.com", "to": "mira@example.org",
		"subject": "forged", "body": "nope",
	})
	if status != http.StatusForbidden {
		t.Fatalf("forged From: status = %d, want 403 (body %v)", status, body)
	}

	// Mark the unread welcome message read.
	const welcomeSubject = "Welcome to wispbox 🎉"
	welcomeID, welcome, found := findMessage(t, c, "INBOX", welcomeSubject)
	if !found {
		t.Fatalf("seeded welcome message not found in INBOX")
	}
	if boolean(t, welcome, "seen") {
		t.Fatalf("welcome message is already seen; expected unread seed")
	}
	if status, body = c.post("/api/mail/messages/"+welcomeID+"/mark-read", map[string]any{"read": true}); status != http.StatusOK {
		t.Fatalf("mark-read: status = %d, body %v", status, body)
	}
	if _, after, ok := findMessage(t, c, "INBOX", welcomeSubject); !ok || !boolean(t, after, "seen") {
		t.Errorf("welcome message not marked seen after mark-read")
	}

	// Move it to Trash, then delete it permanently from Trash.
	if status, body = c.post("/api/mail/messages/"+welcomeID+"/move", map[string]any{"folder": "Trash"}); status != http.StatusOK {
		t.Fatalf("move to Trash: status = %d, body %v", status, body)
	}
	if _, _, ok := findMessage(t, c, "INBOX", welcomeSubject); ok {
		t.Errorf("welcome message still in INBOX after move")
	}
	trashID, _, found := findMessage(t, c, "Trash", welcomeSubject)
	if !found {
		t.Fatalf("welcome message not found in Trash after move")
	}
	if status, body = c.post("/api/mail/messages/"+trashID+"/delete", nil); status != http.StatusOK {
		t.Fatalf("delete from Trash: status = %d, body %v", status, body)
	}
	if _, _, ok := findMessage(t, c, "Trash", welcomeSubject); ok {
		t.Errorf("welcome message still in Trash after permanent delete")
	}
}

// ---- (5) setup gating on a fresh instance ----

func TestSetupGating(t *testing.T) {
	app, srv := newTestServer(t, false) // fresh: no seed
	c := newClient(t, srv)

	// Before setup, / redirects to /setup.
	resp, _ := c.do(http.MethodGet, "/", nil, "")
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("GET /: status = %d, want 307", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/setup" {
		t.Fatalf("GET / redirects to %q, want /setup", loc)
	}

	status, body := c.get("/api/setup/status")
	if status != http.StatusOK {
		t.Fatalf("setup status: status = %d, body %v", status, body)
	}
	if boolean(t, body, "has_admin") {
		t.Fatalf("fresh instance reports has_admin = true")
	}
	if boolean(t, body, "initialized") {
		t.Fatalf("fresh instance reports initialized = true")
	}

	// Full wizard: admin, host, domain, delivery, mailbox, complete.
	status, body = c.post("/api/setup/admin",
		map[string]any{"username": "owner", "password": "sunny-meadow-99"})
	if status != http.StatusCreated {
		t.Fatalf("setup admin: status = %d, body %v", status, body)
	}
	c.csrf = str(t, body, "csrf")

	status, body = c.post("/api/setup/host", map[string]any{
		"hostname": "mail.fresh.example", "server_ipv4": "203.0.113.9",
		"acme_email": "owner@fresh.example",
	})
	if status != http.StatusOK {
		t.Fatalf("setup host: status = %d, body %v", status, body)
	}

	status, body = c.post("/api/setup/domain", map[string]any{"name": "fresh.example"})
	if status != http.StatusCreated {
		t.Fatalf("setup domain: status = %d, body %v", status, body)
	}
	domainID := num(t, obj(t, body, "domain"), "id")

	if status, body = c.post("/api/setup/delivery", map[string]any{"mode": "direct"}); status != http.StatusOK {
		t.Fatalf("setup delivery: status = %d, body %v", status, body)
	}

	status, body = c.post("/api/setup/mailbox", map[string]any{
		"domain_id": domainID, "local_part": "me", "password": "quiet-harbor-7",
	})
	if status != http.StatusCreated {
		t.Fatalf("setup mailbox: status = %d, body %v", status, body)
	}
	if got := num(t, obj(t, body, "mailbox"), "quota_mb"); got != 0 {
		t.Errorf("setup mailbox quota_mb = %v, want 0", got)
	}

	status, body = c.post("/api/setup/test-email", map[string]any{"to": "owner@example.net"})
	if status != http.StatusOK || !boolean(t, body, "ok") {
		t.Fatalf("setup test email: status = %d, body %v", status, body)
	}
	mockMailer, ok := app.TestMailer.(*admin.MockTestMailer)
	if !ok {
		t.Fatalf("TestMailer = %T, want *admin.MockTestMailer", app.TestMailer)
	}
	if mockMailer.LastFrom != "me@fresh.example" {
		t.Errorf("setup test email sender = %q, want me@fresh.example", mockMailer.LastFrom)
	}

	if status, body = c.post("/api/setup/complete", nil); status != http.StatusOK {
		t.Fatalf("setup complete: status = %d, body %v", status, body)
	}

	// The wizard surface disappears.
	if status, _ = c.get("/api/setup/status"); status != http.StatusNotFound {
		t.Errorf("setup status after complete: status = %d, want 404", status)
	}
	resp, _ = c.do(http.MethodGet, "/setup", nil, "")
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("GET /setup after complete: status = %d, want 307", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/admin" {
		t.Errorf("GET /setup after complete redirects to %q, want /admin", loc)
	}

	// The webmail shell is now served at /.
	resp, _ = c.do(http.MethodGet, "/", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / after complete: status = %d, want 200", resp.StatusCode)
	}
}

func TestSetupDirectDeliveryRequiresOutbound25(t *testing.T) {
	app, srv := newTestServer(t, false)
	c := newClient(t, srv)

	status, body := c.post("/api/setup/admin", map[string]any{
		"username": "owner", "password": "sunny-meadow-99",
	})
	if status != http.StatusCreated {
		t.Fatalf("setup admin: status = %d, body %v", status, body)
	}
	c.csrf = str(t, body, "csrf")

	app.Cfg.Mode = config.ModeProduction
	app.AdminH.Core.OutboundSMTP25Open = func(context.Context) bool { return false }
	status, body = c.post("/api/setup/delivery", map[string]any{"mode": "direct"})
	if status != http.StatusBadRequest {
		t.Fatalf("setup direct with blocked outbound 25: status = %d, want 400 (body %v)", status, body)
	}
	if msg := str(t, body, "error"); !strings.Contains(msg, "outbound port 25") || !strings.Contains(msg, "relay") {
		t.Fatalf("blocked setup direct error = %q, want port 25 relay guidance", msg)
	}
}

// ---- (6) unknown API path ----

func TestUnknownAPIPathReturnsJSON404(t *testing.T) {
	_, srv := newTestServer(t, false)
	c := newClient(t, srv)

	resp, body := c.do(http.MethodGet, "/api/does-not-exist", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown API path: status = %d, want 404", resp.StatusCode)
	}
	if body == nil {
		t.Fatalf("unknown API path returned a non-JSON body")
	}
	if msg := str(t, body, "error"); msg == "" {
		t.Errorf("unknown API path error message is empty")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
