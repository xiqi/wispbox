// Package mailapi implements the Webmail REST API at /api/mail/*.
//
// Authentication is a mailbox session (separate cookie and user type from
// admin sessions). All mailbox access goes through the imapclient.Client
// interface; all sending goes through smtpclient.Sender.
package mailapi

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/imapclient"
	"github.com/xiqi/wispbox/internal/security"
	"github.com/xiqi/wispbox/internal/smtpclient"
)

// Handlers carries the mail API dependencies.
type Handlers struct {
	Store        *db.Store
	Sessions     *auth.Sessions
	Secret       []byte
	IMAP         imapclient.Client
	SMTP         smtpclient.Sender
	LoginLimiter *security.RateLimiter
	Log          *slog.Logger
}

// Mount registers all /api/mail routes on mux.
func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/mail/login", h.login)
	mux.HandleFunc("POST /api/mail/passkeys/login/options", h.passkeyLoginOptions)
	mux.HandleFunc("POST /api/mail/passkeys/login/finish", h.passkeyLoginFinish)
	mux.HandleFunc("POST /api/mail/logout", h.logout)
	mux.Handle("GET /api/mail/me", h.authed(h.me))
	mux.Handle("GET /api/mail/account/security", h.authed(h.accountSecurity))
	mux.Handle("POST /api/mail/account/password", h.authed(h.changePassword))
	mux.Handle("POST /api/mail/account/2fa/setup", h.authed(h.totpSetup))
	mux.Handle("POST /api/mail/account/2fa/enable", h.authed(h.totpEnable))
	mux.Handle("POST /api/mail/account/2fa/disable", h.authed(h.totpDisable))
	mux.Handle("POST /api/mail/account/passkeys/register/options", h.authed(h.passkeyRegisterOptions))
	mux.Handle("POST /api/mail/account/passkeys/register/finish", h.authed(h.passkeyRegisterFinish))
	mux.Handle("DELETE /api/mail/account/passkeys/{id}", h.authed(h.passkeyDelete))
	mux.Handle("GET /api/mail/folders", h.authed(h.folders))
	mux.Handle("GET /api/mail/messages", h.authed(h.listMessages))
	mux.Handle("GET /api/mail/messages/{id}", h.authed(h.getMessage))
	mux.Handle("POST /api/mail/send", h.authed(h.send))
	mux.Handle("POST /api/mail/reply", h.authed(h.reply))
	mux.Handle("POST /api/mail/forward", h.authed(h.forward))
	mux.Handle("POST /api/mail/messages/{id}/move", h.authed(h.move))
	mux.Handle("POST /api/mail/messages/{id}/delete", h.authed(h.delete))
	mux.Handle("POST /api/mail/messages/{id}/mark-read", h.authed(h.markRead))
	mux.Handle("GET /api/mail/attachments/{id}", h.authed(h.attachment))
}

// session context plumbing --------------------------------------------------

type mailCtx struct {
	Session *db.Session
	Creds   auth.Credentials
	Mailbox *db.Mailbox
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, mc *mailCtx)

// authed resolves the mailbox session, enforces CSRF on mutating methods,
// and loads IMAP credentials from the in-memory vault.
func (h *Handlers) authed(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := h.Sessions.RequireSession(w, r, db.UserMailbox, "please sign in")
		if !ok {
			return
		}
		creds, ok := h.Sessions.CredentialsFor(sess.ID)
		if !ok {
			// Daemon restarted since login; credentials are memory-only.
			httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("please sign in again"))
			return
		}
		mb, err := h.Store.GetMailboxByEmail(r.Context(), creds.Email)
		if err != nil || !mb.Enabled {
			httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("this mailbox is disabled"))
			return
		}
		fn(w, r, &mailCtx{Session: sess, Creds: creds, Mailbox: mb})
	})
}

// auth ------------------------------------------------------------------

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	ip := httpjson.ClientIP(r)
	if !h.LoginLimiter.Allow("mail:" + ip) {
		httpjson.Error(w, http.StatusTooManyRequests, fmt.Errorf("too many sign-in attempts; wait a minute and try again"))
		return
	}
	mb, err := h.Store.GetMailboxByEmail(r.Context(), req.Email)
	if err != nil || !mb.Enabled || !auth.VerifyMailboxPassword(req.Password, mb.PasswordHash) {
		_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
			ActorType: "mailbox", Action: "login_failed", TargetType: "mailbox", TargetID: req.Email, IP: ip,
		})
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("wrong email or password"))
		return
	}
	if mb.TwoFactorEnabled {
		if strings.TrimSpace(req.TOTPCode) == "" {
			httpjson.Write(w, http.StatusAccepted, map[string]any{"two_factor_required": true})
			return
		}
		secret, err := security.Decrypt(h.Secret, mb.EncryptedTOTPSecret)
		if err != nil || !auth.ValidateTOTP(secret, req.TOTPCode) {
			_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
				ActorType: "mailbox", ActorID: mb.ID, Action: "login_failed", TargetType: "mailbox", TargetID: req.Email, IP: ip,
			})
			httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("wrong email, password, or verification code"))
			return
		}
	}
	csrf, err := h.Sessions.Login(r.Context(), w, db.UserMailbox, mb.ID, &auth.Credentials{Email: mb.Email, Password: req.Password})
	if err != nil {
		httpjson.Error(w, http.StatusInternalServerError, fmt.Errorf("could not create session"))
		return
	}
	_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "mailbox", ActorID: mb.ID, Action: "login", TargetType: "mailbox", TargetID: mb.Email, IP: ip,
	})
	httpjson.Write(w, http.StatusOK, map[string]any{"email": mb.Email, "csrf": csrf})
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	h.Sessions.Logout(r.Context(), w, r, db.UserMailbox)
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) me(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	passkeyCount, _ := h.Store.CountPasskeys(r.Context(), db.UserMailbox, mc.Mailbox.ID)
	httpjson.Write(w, http.StatusOK, map[string]any{
		"email":              mc.Mailbox.Email,
		"quota_mb":           mc.Mailbox.QuotaMB,
		"two_factor_enabled": mc.Mailbox.TwoFactorEnabled,
		"passkey_count":      passkeyCount,
		"csrf":               h.Sessions.CSRFForRequest(r, db.UserMailbox),
	})
}

// message ids -------------------------------------------------------------

// Message IDs are opaque to clients: base64url("folder\x1fuid").
func encodeMessageID(folder string, uid uint32) string {
	return base64.RawURLEncoding.EncodeToString([]byte(folder + "\x1f" + strconv.FormatUint(uint64(uid), 10)))
}

func decodeMessageID(id string) (folder string, uid uint32, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", 0, fmt.Errorf("bad message id")
	}
	folder, uidStr, ok := strings.Cut(string(raw), "\x1f")
	if !ok {
		return "", 0, fmt.Errorf("bad message id")
	}
	u, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("bad message id")
	}
	return folder, uint32(u), nil
}

// Attachment IDs: base64url("folder\x1fuid\x1findex").
func encodeAttachmentID(folder string, uid uint32, index int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s\x1f%d\x1f%d", folder, uid, index)))
}

func decodeAttachmentID(id string) (folder string, uid uint32, index int, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", 0, 0, fmt.Errorf("bad attachment id")
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != 3 {
		return "", 0, 0, fmt.Errorf("bad attachment id")
	}
	u, err1 := strconv.ParseUint(parts[1], 10, 32)
	idx, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil {
		return "", 0, 0, fmt.Errorf("bad attachment id")
	}
	return parts[0], uint32(u), idx, nil
}

// reading -----------------------------------------------------------------

func (h *Handlers) folders(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	folders, err := h.IMAP.Folders(r.Context(), mc.Creds)
	if err != nil {
		httpjson.Error(w, http.StatusBadGateway, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"folders": folders})
}

type apiHeader struct {
	ID string `json:"id"`
	imapclient.Header
}

func (h *Handlers) listMessages(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	cursor := r.URL.Query().Get("cursor")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	var page *imapclient.Page
	var err error
	if query != "" {
		page, err = h.IMAP.Search(r.Context(), mc.Creds, folder, query, limit)
	} else {
		page, err = h.IMAP.List(r.Context(), mc.Creds, folder, cursor, limit)
	}
	if err != nil {
		httpjson.Error(w, http.StatusBadGateway, err)
		return
	}
	out := struct {
		Messages   []apiHeader `json:"messages"`
		Total      uint32      `json:"total"`
		NextCursor string      `json:"next_cursor,omitempty"`
	}{Total: page.Total, NextCursor: page.NextCursor, Messages: []apiHeader{}}
	for _, m := range page.Messages {
		out.Messages = append(out.Messages, apiHeader{ID: encodeMessageID(m.Folder, m.UID), Header: m})
	}
	httpjson.Write(w, http.StatusOK, out)
}

func (h *Handlers) getMessage(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	folder, uid, err := decodeMessageID(r.PathValue("id"))
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	msg, err := h.IMAP.Get(r.Context(), mc.Creds, folder, uid)
	if err != nil {
		httpjson.Error(w, http.StatusNotFound, err)
		return
	}
	allowRemote := r.URL.Query().Get("remote") == "1"

	htmlBody := msg.HTMLBody
	hadRemote := false
	if htmlBody != "" {
		// Resolve cid: references to attachment URLs before sanitizing so
		// inline images survive the sanitizer's src allowlist.
		for _, att := range msg.Attachments {
			if att.ContentID != "" {
				url := "/api/mail/attachments/" + encodeAttachmentID(folder, uid, att.Index)
				htmlBody = strings.ReplaceAll(htmlBody, "cid:"+att.ContentID, url)
			}
		}
		htmlBody, hadRemote = security.SanitizeEmailHTML(htmlBody, allowRemote)
	}

	type apiAttachment struct {
		ID string `json:"id"`
		imapclient.AttachmentMeta
	}
	atts := []apiAttachment{}
	for _, a := range msg.Attachments {
		atts = append(atts, apiAttachment{ID: encodeAttachmentID(folder, uid, a.Index), AttachmentMeta: a})
	}

	httpjson.Write(w, http.StatusOK, map[string]any{
		"id":         encodeMessageID(folder, uid),
		"header":     msg.Header,
		"cc":         msg.CC,
		"reply_to":   msg.ReplyTo,
		"message_id": msg.MessageID,
		"text_body":  msg.TextBody,
		"html_body":  htmlBody,
		// hadRemote reports whether remote references existed; the flag reports
		// whether they were actually stripped (only when allowRemote is false).
		"has_blocked_images": hadRemote && !allowRemote,
		"attachments":        atts,
	})
}

func (h *Handlers) attachment(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	folder, uid, index, err := decodeAttachmentID(r.PathValue("id"))
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	att, err := h.IMAP.Attachment(r.Context(), mc.Creds, folder, uid, index)
	if err != nil {
		httpjson.Error(w, http.StatusNotFound, err)
		return
	}
	head := att.Data
	if len(head) > 512 {
		head = head[:512]
	}
	ct := security.SafeDownloadType(att.MIMEType, head)
	disposition := "attachment"
	if strings.HasPrefix(ct, "image/") {
		disposition = "inline" // safe image types may render inline (cid images)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, att.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(att.Data)))
	_, _ = w.Write(att.Data)
}

// flags / folders ----------------------------------------------------------

func (h *Handlers) move(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	folder, uid, err := decodeMessageID(r.PathValue("id"))
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Folder string `json:"folder"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil || req.Folder == "" {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("destination folder is required"))
		return
	}
	if err := h.IMAP.Move(r.Context(), mc.Creds, folder, uid, req.Folder); err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, nil)
}

// delete moves to Trash, or permanently deletes when already in Trash.
func (h *Handlers) delete(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	folder, uid, err := decodeMessageID(r.PathValue("id"))
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if imapclient.RoleForFolderName(folder) == imapclient.RoleTrash {
		err = h.IMAP.Delete(r.Context(), mc.Creds, folder, uid)
	} else {
		err = h.IMAP.Move(r.Context(), mc.Creds, folder, uid, "Trash")
	}
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) markRead(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	folder, uid, err := decodeMessageID(r.PathValue("id"))
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Read bool `json:"read"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if err := h.IMAP.SetSeen(r.Context(), mc.Creds, folder, uid, req.Read); err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, nil)
}

// helpers -------------------------------------------------------------------

func parseAddressField(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	list, err := mail.ParseAddressList(s)
	if err != nil {
		return nil, fmt.Errorf("%q is not a valid address list", s)
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}
	return out, nil
}
