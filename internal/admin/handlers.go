package admin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/security"
	"github.com/xiqi/wispbox/internal/services"
)

// Handlers is the full Admin API surface.
type Handlers struct {
	Core         *Core
	Sessions     *auth.Sessions
	LoginLimiter *security.RateLimiter
	Services     services.Manager
	Queue        services.QueueInspector
	Logs         services.LogReader
	TestMailer   TestMailer
	StartedAt    time.Time

	LatestVersion func(ctx context.Context) (string, error)
}

// Mount registers all /api/admin routes.
func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/login", h.login)
	mux.HandleFunc("POST /api/admin/passkeys/login/options", h.passkeyLoginOptions)
	mux.HandleFunc("POST /api/admin/passkeys/login/finish", h.passkeyLoginFinish)
	mux.HandleFunc("POST /api/admin/logout", h.logout)
	mux.Handle("GET /api/admin/me", h.authed(h.me))
	mux.Handle("GET /api/admin/account/security", h.authed(h.accountSecurity))
	mux.Handle("POST /api/admin/account/password", h.authed(h.changePassword))
	mux.Handle("POST /api/admin/account/2fa/setup", h.authed(h.totpSetup))
	mux.Handle("POST /api/admin/account/2fa/enable", h.authed(h.totpEnable))
	mux.Handle("POST /api/admin/account/2fa/disable", h.authed(h.totpDisable))
	mux.Handle("POST /api/admin/account/passkeys/register/options", h.authed(h.passkeyRegisterOptions))
	mux.Handle("POST /api/admin/account/passkeys/register/finish", h.authed(h.passkeyRegisterFinish))
	mux.Handle("DELETE /api/admin/account/passkeys/{id}", h.authed(h.passkeyDelete))

	mux.Handle("GET /api/admin/overview", h.authed(h.overview))

	mux.Handle("GET /api/admin/domains", h.authed(h.listDomains))
	mux.Handle("POST /api/admin/domains", h.authed(h.createDomain))
	mux.Handle("GET /api/admin/domains/{id}", h.authed(h.getDomain))
	mux.Handle("DELETE /api/admin/domains/{id}", h.authed(h.deleteDomain))

	mux.Handle("GET /api/admin/mailboxes", h.authed(h.listMailboxes))
	mux.Handle("POST /api/admin/mailboxes", h.authed(h.createMailbox))
	mux.Handle("PATCH /api/admin/mailboxes/{id}", h.authed(h.updateMailbox))
	mux.Handle("POST /api/admin/mailboxes/{id}/reset-password", h.authed(h.resetMailboxPassword))
	mux.Handle("DELETE /api/admin/mailboxes/{id}", h.authed(h.deleteMailbox))

	mux.Handle("GET /api/admin/aliases", h.authed(h.listAliases))
	mux.Handle("POST /api/admin/aliases", h.authed(h.createAlias))
	mux.Handle("PATCH /api/admin/aliases/{id}", h.authed(h.updateAlias))
	mux.Handle("DELETE /api/admin/aliases/{id}", h.authed(h.deleteAlias))

	mux.Handle("GET /api/admin/relays", h.authed(h.listRelays))
	mux.Handle("GET /api/admin/relay-presets", h.authed(h.relayPresets))
	mux.Handle("POST /api/admin/relays", h.authed(h.createRelay))
	mux.Handle("PATCH /api/admin/relays/{id}", h.authed(h.updateRelay))
	mux.Handle("DELETE /api/admin/relays/{id}", h.authed(h.deleteRelay))
	mux.Handle("POST /api/admin/relays/{id}/test", h.authed(h.testRelay))

	mux.Handle("GET /api/admin/delivery-policies", h.authed(h.listPolicies))
	mux.Handle("POST /api/admin/delivery-policies", h.authed(h.upsertPolicy))
	mux.Handle("DELETE /api/admin/delivery-policies/{id}", h.authed(h.deletePolicy))
	mux.Handle("POST /api/admin/test-email", h.authed(h.testEmail))

	mux.Handle("GET /api/admin/dns/{domainID}", h.authed(h.dnsRecords))
	mux.Handle("POST /api/admin/dns/{domainID}/check", h.authed(h.dnsCheck))

	mux.Handle("GET /api/admin/certificates", h.authed(h.listCertificates))
	mux.Handle("POST /api/admin/certificates/admin/issue", h.authed(h.issueAdminCertificate))
	mux.Handle("POST /api/admin/certificates/{id}/renew", h.authed(h.renewCertificate))

	mux.Handle("GET /api/admin/queue", h.authed(h.queue))
	mux.Handle("POST /api/admin/queue/flush", h.authed(h.queueFlush))
	mux.Handle("POST /api/admin/queue/{id}/retry", h.authed(h.queueRetry))
	mux.Handle("DELETE /api/admin/queue/{id}", h.authed(h.queueDelete))

	mux.Handle("GET /api/admin/logs", h.authed(h.logs))
	mux.Handle("GET /api/admin/audit", h.authed(h.audit))

	mux.Handle("GET /api/admin/settings", h.authed(h.getSettings))
	mux.Handle("PATCH /api/admin/settings", h.authed(h.patchSettings))
	mux.Handle("POST /api/admin/settings/logo", h.authed(h.uploadLogo))
	mux.Handle("DELETE /api/admin/settings/logo", h.authed(h.deleteLogo))

	mux.Handle("GET /api/admin/upgrade", h.authed(h.upgradeStatus))
	mux.Handle("POST /api/admin/upgrade", h.authed(h.startUpgrade))
}

// ---- auth middleware ----

type adminCtx struct {
	Session *db.Session
	Admin   *db.Admin
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, ac *adminCtx)

// authed requires a live admin session. Mailbox sessions are a different
// user type and can never pass this gate.
func (h *Handlers) authed(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := h.Sessions.RequireSession(w, r, db.UserAdmin, "admin sign-in required")
		if !ok {
			return
		}
		adm, err := h.Core.Store.GetAdmin(r.Context(), sess.UserID)
		if err != nil {
			httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("admin sign-in required"))
			return
		}
		fn(w, r, &adminCtx{Session: sess, Admin: adm})
	})
}

// audit records an admin action in the audit log.
func (h *Handlers) auditLog(r *http.Request, ac *adminCtx, action, targetType, targetID string) {
	_ = h.Core.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "admin", ActorID: ac.Admin.ID, Action: action,
		TargetType: targetType, TargetID: targetID, IP: httpjson.ClientIP(r),
	})
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	ip := httpjson.ClientIP(r)
	if !h.LoginLimiter.Allow("admin:" + ip) {
		httpjson.Error(w, http.StatusTooManyRequests, fmt.Errorf("too many sign-in attempts; wait a minute and try again"))
		return
	}
	adm, err := h.Core.Store.GetAdminByUsername(r.Context(), req.Username)
	if err != nil || !auth.VerifyAdminPassword(req.Password, adm.PasswordHash) {
		_ = h.Core.Store.AppendAudit(r.Context(), db.AuditLog{
			ActorType: "admin", Action: "login_failed", TargetType: "admin", TargetID: req.Username, IP: ip,
		})
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("wrong username or password"))
		return
	}
	if adm.TwoFactorEnabled {
		if strings.TrimSpace(req.TOTPCode) == "" {
			httpjson.Write(w, http.StatusAccepted, map[string]any{"two_factor_required": true})
			return
		}
		secret, err := security.Decrypt(h.Core.Secret, adm.EncryptedTOTPSecret)
		if err != nil || !auth.ValidateTOTP(secret, req.TOTPCode) {
			_ = h.Core.Store.AppendAudit(r.Context(), db.AuditLog{
				ActorType: "admin", ActorID: adm.ID, Action: "login_failed", TargetType: "admin", TargetID: adm.Username, IP: ip,
			})
			httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("wrong username, password, or verification code"))
			return
		}
	}
	csrf, err := h.Sessions.Login(r.Context(), w, db.UserAdmin, adm.ID, nil)
	if err != nil {
		httpjson.Error(w, http.StatusInternalServerError, fmt.Errorf("could not create session"))
		return
	}
	_ = h.Core.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "admin", ActorID: adm.ID, Action: "login", TargetType: "admin", TargetID: adm.Username, IP: ip,
	})
	httpjson.Write(w, http.StatusOK, map[string]any{"username": adm.Username, "csrf": csrf})
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	h.Sessions.Logout(r.Context(), w, r, db.UserAdmin)
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) me(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	passkeyCount, _ := h.Core.Store.CountPasskeys(r.Context(), db.UserAdmin, ac.Admin.ID)
	httpjson.Write(w, http.StatusOK, map[string]any{
		"username":           ac.Admin.Username,
		"id":                 ac.Admin.ID,
		"two_factor_enabled": ac.Admin.TwoFactorEnabled,
		"passkey_count":      passkeyCount,
		"csrf":               h.Sessions.CSRFForRequest(r, db.UserAdmin),
	})
}

// ---- domains ----

type domainView struct {
	db.Domain
	MailboxCount int    `json:"mailbox_count"`
	CertStatus   string `json:"cert_status"`
	DeliveryMode string `json:"delivery_mode"`
	DeliverySrc  string `json:"delivery_source"`
}

func (h *Handlers) domainView(r *http.Request, dom db.Domain) domainView {
	v := domainView{Domain: dom, CertStatus: "none", DeliveryMode: "direct"}
	if n, err := h.Core.Store.CountMailboxes(r.Context(), dom.ID); err == nil {
		v.MailboxCount = n
	}
	if cert, err := h.Core.Store.GetCertificateByHostname(r.Context(), dom.MailHostname); err == nil {
		v.CertStatus = string(cert.Status)
	}
	if resolved, err := h.Core.Engine.ResolveForDomain(r.Context(), dom.ID); err == nil {
		v.DeliveryMode = string(resolved.Mode)
		v.DeliverySrc = resolved.Source
		if resolved.Relay != nil {
			v.DeliveryMode = "relay:" + resolved.Relay.Name
		}
	}
	return v
}

func (h *Handlers) listDomains(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	domains, err := h.Core.Store.ListDomains(r.Context())
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	views := []domainView{}
	for _, d := range domains {
		views = append(views, h.domainView(r, d))
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"domains": views})
}

func (h *Handlers) createDomain(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req struct {
		Name         string `json:"name"`
		MailHostname string `json:"mail_hostname"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	dom, warning, err := h.Core.CreateDomain(r.Context(), req.Name, req.MailHostname)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "domain_create", "domain", dom.Name)
	httpjson.Write(w, http.StatusCreated, map[string]any{"domain": h.domainView(r, *dom), "warning": warning})
}

func (h *Handlers) getDomain(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	dom, err := h.Core.Store.GetDomain(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"domain": h.domainView(r, *dom)})
}

func (h *Handlers) deleteDomain(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	dom, warning, err := h.Core.DeleteDomain(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "domain_delete", "domain", dom.Name)
	httpjson.Write(w, http.StatusOK, map[string]any{"warning": warning})
}

// ---- mailboxes ----

func (h *Handlers) listMailboxes(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	domainID, _ := strconv.ParseInt(r.URL.Query().Get("domain_id"), 10, 64)
	boxes, err := h.Core.Store.ListMailboxes(r.Context(), domainID)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	if boxes == nil {
		boxes = []db.Mailbox{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"mailboxes": boxes})
}

func (h *Handlers) createMailbox(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req struct {
		DomainID  int64  `json:"domain_id"`
		LocalPart string `json:"local_part"`
		Password  string `json:"password"`
		QuotaMB   int64  `json:"quota_mb"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	mb, warning, err := h.Core.CreateMailbox(r.Context(), req.DomainID, req.LocalPart, req.Password, req.QuotaMB)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "mailbox_create", "mailbox", mb.Email)
	httpjson.Write(w, http.StatusCreated, map[string]any{"mailbox": mb, "warning": warning})
}

func (h *Handlers) updateMailbox(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	req := struct {
		QuotaMB *int64 `json:"quota_mb"`
		Enabled *bool  `json:"enabled"`
	}{}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	mb, warning, err := h.Core.UpdateMailbox(r.Context(), id, req.QuotaMB, req.Enabled)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "mailbox_update", "mailbox", mb.Email)
	httpjson.Write(w, http.StatusOK, map[string]any{"mailbox": mb, "warning": warning})
}

func (h *Handlers) resetMailboxPassword(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Password string `json:"password"`
		Generate bool   `json:"generate"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	generated := ""
	if req.Generate {
		generated = auth.GeneratePassword()
		req.Password = generated
	}
	mb, err := h.Core.ResetMailboxPassword(r.Context(), id, req.Password)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "mailbox_reset_password", "mailbox", mb.Email)
	httpjson.Write(w, http.StatusOK, map[string]any{"generated_password": generated})
}

func (h *Handlers) deleteMailbox(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	mb, warning, err := h.Core.DeleteMailbox(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "mailbox_delete", "mailbox", mb.Email)
	httpjson.Write(w, http.StatusOK, map[string]any{"warning": warning})
}

// ---- aliases ----

func (h *Handlers) listAliases(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	domainID, _ := strconv.ParseInt(r.URL.Query().Get("domain_id"), 10, 64)
	aliases, err := h.Core.Store.ListAliases(r.Context(), domainID)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	if aliases == nil {
		aliases = []db.Alias{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"aliases": aliases})
}

func (h *Handlers) createAlias(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req struct {
		DomainID    int64  `json:"domain_id"`
		Source      string `json:"source"`
		Destination string `json:"destination"`
		IsCatchAll  bool   `json:"is_catch_all"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	alias, warning, err := h.Core.CreateAlias(r.Context(), req.DomainID, req.Source, req.Destination, req.IsCatchAll)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "alias_create", "alias", alias.Source)
	httpjson.Write(w, http.StatusCreated, map[string]any{"alias": alias, "warning": warning})
}

func (h *Handlers) updateAlias(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if req.Enabled == nil {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("enabled is required"))
		return
	}
	alias, warning, err := h.Core.UpdateAliasEnabled(r.Context(), id, *req.Enabled)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "alias_update", "alias", alias.Source)
	httpjson.Write(w, http.StatusOK, map[string]any{"alias": alias, "warning": warning})
}

func (h *Handlers) deleteAlias(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	alias, warning, err := h.Core.DeleteAlias(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "alias_delete", "alias", alias.Source)
	httpjson.Write(w, http.StatusOK, map[string]any{"warning": warning})
}
