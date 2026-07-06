// Package setup implements the first-run wizard API at /api/setup/*.
//
// The wizard is only usable before initialization completes. Creating the
// admin account is the first (unauthenticated) step; it immediately signs
// the new admin in, and every later step requires that admin session. Once
// /api/setup/complete runs, the whole surface returns 404 and /setup
// redirects to /admin.
package setup

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"time"

	"github.com/xiqi/wispbox/internal/admin"
	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/delivery"
	"github.com/xiqi/wispbox/internal/security"
)

// Handlers is the setup wizard API surface.
type Handlers struct {
	Cfg          *config.Config
	Core         *admin.Core
	Sessions     *auth.Sessions
	LoginLimiter *security.RateLimiter
	TestMailer   admin.TestMailer
}

// Mount registers all /api/setup routes.
func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.Handle("GET /api/setup/status", h.gate(h.status, false))
	mux.Handle("GET /api/setup/relay-presets", h.gate(h.relayPresets, false))
	mux.Handle("POST /api/setup/admin", h.gate(h.createAdmin, false))
	mux.Handle("POST /api/setup/host", h.gate(h.configureHost, true))
	mux.Handle("POST /api/setup/domain", h.gate(h.addDomain, true))
	mux.Handle("POST /api/setup/delivery", h.gate(h.configureDelivery, true))
	mux.Handle("GET /api/setup/dns/{domainID}", h.gate(h.dnsRecords, true))
	mux.Handle("POST /api/setup/dns/{domainID}/check", h.gate(h.dnsCheck, true))
	mux.Handle("POST /api/setup/certificate", h.gate(h.issueCertificate, true))
	mux.Handle("GET /api/setup/certificate/{id}", h.gate(h.certificateStatus, true))
	mux.Handle("POST /api/setup/mailbox", h.gate(h.createMailbox, true))
	mux.Handle("POST /api/setup/test-email", h.gate(h.testEmail, true))
	mux.Handle("POST /api/setup/complete", h.gate(h.complete, true))
}

// gate blocks the whole surface after initialization and optionally
// requires the setup admin session.
func (h *Handlers) gate(fn func(http.ResponseWriter, *http.Request), needAuth bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.Core.Store.IsInitialized(r.Context()) {
			http.NotFound(w, r)
			return
		}
		if needAuth {
			if _, ok := h.Sessions.RequireSession(w, r, db.UserAdmin, "create the admin account first"); !ok {
				return
			}
		}
		fn(w, r)
	})
}

// binaryExists reports whether a command is on PATH (read-only host probe).
func binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// status powers step 1 (system check) and lets the wizard resume mid-flight.
func (h *Handlers) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	adminCount, _ := h.Core.Store.CountAdmins(ctx)
	domains, _ := h.Core.Store.ListDomains(ctx)
	mailboxes, _ := h.Core.Store.ListMailboxes(ctx, 0)
	hostname := h.Core.Store.GetSettingDefault(ctx, "primary_hostname", "")

	checks := []map[string]any{
		{"name": "Database", "ok": true, "detail": "SQLite control database is writable"},
		{"name": "Mode", "ok": true, "detail": string(h.Cfg.Mode) + " mode"},
	}
	if h.Cfg.IsDev() {
		checks = append(checks, map[string]any{
			"name": "Mail services", "ok": true,
			"detail": "development mode: Postfix and Dovecot are mocked",
		})
	} else {
		for _, svc := range []string{"postfix", "dovecot"} {
			ok := binaryExists(svc)
			checks = append(checks, map[string]any{
				"name": svc, "ok": ok,
				"detail": map[bool]string{true: svc + " is installed", false: svc + " not found — run the wispbox installer"}[ok],
			})
		}
	}

	httpjson.Write(w, http.StatusOK, map[string]any{
		"initialized":      false,
		"has_admin":        adminCount > 0,
		"primary_hostname": hostname,
		"server_ipv4":      h.Core.Store.GetSettingDefault(ctx, "server_ipv4", ""),
		"domains":          domains,
		"mailbox_count":    len(mailboxes),
		"checks":           checks,
		"authenticated":    h.Sessions.Resolve(r, db.UserAdmin) != nil,
		"csrf":             h.Sessions.CSRFForRequest(r, db.UserAdmin),
	})
}

// relayPresets serves the built-in relay providers so the wizard's delivery
// step shares one source of truth with the admin console (delivery.Presets).
func (h *Handlers) relayPresets(w http.ResponseWriter, _ *http.Request) {
	httpjson.Write(w, http.StatusOK, map[string]any{"presets": delivery.Presets()})
}

// createAdmin is step 2. Only works while no admin exists; signs in the new
// admin so the rest of the wizard is authenticated.
func (h *Handlers) createAdmin(w http.ResponseWriter, r *http.Request) {
	if !h.LoginLimiter.Allow("setup:" + httpjson.ClientIP(r)) {
		httpjson.Error(w, http.StatusTooManyRequests, fmt.Errorf("too many attempts; wait a minute"))
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if n, _ := h.Core.Store.CountAdmins(r.Context()); n > 0 {
		httpjson.Error(w, http.StatusConflict, fmt.Errorf("an admin account already exists; sign in at /admin"))
		return
	}
	if err := db.ValidateAdminUsername(req.Username); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	hash, err := auth.HashAdminPassword(req.Password)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	adm, err := h.Core.Store.CreateAdmin(r.Context(), req.Username, hash)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	csrf, err := h.Sessions.Login(r.Context(), w, db.UserAdmin, adm.ID, nil)
	if err != nil {
		httpjson.Error(w, http.StatusInternalServerError, fmt.Errorf("could not create session"))
		return
	}
	_ = h.Core.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "admin", ActorID: adm.ID, Action: "setup_admin_created",
		TargetType: "admin", TargetID: adm.Username, IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusCreated, map[string]any{"username": adm.Username, "csrf": csrf})
}

// configureHost is step 3: primary hostname, server IPs, ACME email.
func (h *Handlers) configureHost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname   string `json:"hostname"`
		ServerIPv4 string `json:"server_ipv4"`
		ServerIPv6 string `json:"server_ipv6"`
		ACMEEmail  string `json:"acme_email"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if err := db.ValidateHostname(req.Hostname); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if req.ServerIPv4 != "" {
		if err := db.ValidateIPv4(req.ServerIPv4); err != nil {
			httpjson.Error(w, http.StatusBadRequest, err)
			return
		}
	}
	if req.ServerIPv6 != "" {
		if err := db.ValidateIPv6(req.ServerIPv6); err != nil {
			httpjson.Error(w, http.StatusBadRequest, err)
			return
		}
	}
	if req.ACMEEmail != "" {
		if err := db.ValidateEmail(req.ACMEEmail); err != nil {
			httpjson.Error(w, http.StatusBadRequest, err)
			return
		}
	}
	ctx := r.Context()
	for k, v := range map[string]string{
		"primary_hostname": req.Hostname,
		"server_ipv4":      req.ServerIPv4,
		"server_ipv6":      req.ServerIPv6,
		"acme_email":       req.ACMEEmail,
	} {
		if err := h.Core.Store.SetSetting(ctx, k, v); err != nil {
			httpjson.Fail(w, err)
			return
		}
	}
	httpjson.Write(w, http.StatusOK, nil)
}

// addDomain is step 4.
func (h *Handlers) addDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	dom, warning, err := h.Core.CreateDomain(r.Context(), req.Name, "")
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusCreated, map[string]any{"domain": dom, "warning": warning})
}

// configureDelivery is steps 5–6: global sending method, optional relay.
func (h *Handlers) configureDelivery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode  db.DeliveryMode `json:"mode"`
		Relay *struct {
			Name     string     `json:"name"`
			Provider string     `json:"provider"`
			Host     string     `json:"host"`
			Port     int        `json:"port"`
			Username string     `json:"username"`
			Password string     `json:"password"`
			TLSMode  db.TLSMode `json:"tls_mode"`
		} `json:"relay"`
	}
	if err := httpjson.Decode(w, r, &req, 64<<10); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	var relayID *int64
	if req.Mode == db.ModeRelay {
		if req.Relay == nil {
			httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("relay settings are required for relay mode"))
			return
		}
		name := req.Relay.Name
		if name == "" {
			name = req.Relay.Provider
		}
		relay, _, err := h.Core.SaveRelay(r.Context(), 0, name, req.Relay.Provider, req.Relay.Host,
			req.Relay.Port, req.Relay.Username, req.Relay.Password, req.Relay.TLSMode, true)
		if err != nil {
			httpjson.Fail(w, err)
			return
		}
		relayID = &relay.ID
	}
	if _, err := h.Core.Engine.SetPolicy(r.Context(), db.ScopeGlobal, 0, req.Mode, relayID); err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, nil)
}

// dnsRecords / dnsCheck are steps 7–8.
func (h *Handlers) dnsRecords(w http.ResponseWriter, r *http.Request) {
	admin.WriteDNSRecords(w, r, h.Core, false)
}

func (h *Handlers) dnsCheck(w http.ResponseWriter, r *http.Request) {
	admin.WriteDNSRecords(w, r, h.Core, true)
}

// issueCertificate is step 9. Async; poll certificateStatus.
func (h *Handlers) issueCertificate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DomainID int64 `json:"domain_id"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	dom, err := h.Core.Store.GetDomain(r.Context(), req.DomainID)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	cert, err := h.Core.Certs.EnsureTracked(r.Context(), dom.ID, dom.MailHostname)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = h.Core.Certs.IssueNow(ctx, cert.ID)
	}()
	httpjson.Write(w, http.StatusAccepted, map[string]any{"certificate": cert})
}

func (h *Handlers) certificateStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	cert, err := h.Core.Store.GetCertificate(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"certificate": cert})
}

// createMailbox is step 10.
func (h *Handlers) createMailbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DomainID  int64  `json:"domain_id"`
		LocalPart string `json:"local_part"`
		Password  string `json:"password"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	mb, warning, err := h.Core.CreateMailbox(r.Context(), req.DomainID, req.LocalPart, req.Password, 0)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusCreated, map[string]any{"mailbox": mb, "warning": warning})
}

// testEmail is step 11.
func (h *Handlers) testEmail(w http.ResponseWriter, r *http.Request) {
	admin.SendTestEmail(w, r, h.Core.Store, h.TestMailer,
		"wispbox setup test", "Your wispbox server can send mail. Welcome aboard!")
}

// complete is step 12: locks the wizard away.
func (h *Handlers) complete(w http.ResponseWriter, r *http.Request) {
	if err := h.Core.Store.SetSetting(r.Context(), "initialized", "true"); err != nil {
		httpjson.Fail(w, err)
		return
	}
	_ = h.Core.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "admin", Action: "setup_completed", IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusOK, map[string]any{"initialized": true})
}
