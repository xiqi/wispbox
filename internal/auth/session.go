package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/db"
)

// Cookie names. Webmail and Admin sessions are entirely separate: different
// cookies, different session rows, different user types.
const (
	AdminCookie = "wispbox_admin"
	MailCookie  = "wispbox_mail"

	AdminSessionTTL = 12 * time.Hour
	MailSessionTTL  = 7 * 24 * time.Hour

	CSRFHeader = "X-wispbox-CSRF"
)

// Sessions issues and resolves cookie-backed sessions.
type Sessions struct {
	store  *db.Store
	secret []byte
	secure bool // set Secure on cookies (false only in plain-HTTP dev)

	// creds holds mailbox credentials in memory only, keyed by session id
	// hash, so the IMAP/SMTP adapters can authenticate as the user. Never
	// persisted; webmail sessions require re-login after a daemon restart.
	mu    sync.Mutex
	creds map[string]Credentials
}

// Credentials are what the production IMAP/SMTP adapters log in with.
type Credentials struct {
	Email    string
	Password string
}

func NewSessions(store *db.Store, secret []byte, secureCookies bool) *Sessions {
	return &Sessions{store: store, secret: secret, secure: secureCookies, creds: map[string]Credentials{}}
}

// cookieName maps a user type to its session cookie name.
func cookieName(userType db.UserType) string {
	if userType == db.UserMailbox {
		return MailCookie
	}
	return AdminCookie
}

// Login creates a session of the given type and sets the cookie.
func (s *Sessions) Login(ctx context.Context, w http.ResponseWriter, userType db.UserType, userID int64, creds *Credentials) (csrf string, err error) {
	ttl := AdminSessionTTL
	if userType == db.UserMailbox {
		ttl = MailSessionTTL
	}
	cookie := cookieName(userType)
	token, err := s.store.CreateSession(ctx, userType, userID, ttl)
	if err != nil {
		return "", err
	}
	if creds != nil {
		s.mu.Lock()
		s.creds[db.HashSessionToken(token)] = *creds
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return s.CSRFToken(token), nil
}

// Logout deletes the session and clears the cookie.
func (s *Sessions) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request, userType db.UserType) {
	cookie := cookieName(userType)
	if c, err := r.Cookie(cookie); err == nil {
		s.mu.Lock()
		delete(s.creds, db.HashSessionToken(c.Value))
		s.mu.Unlock()
		_ = s.store.DeleteSession(ctx, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
}

// Resolve returns the live session for a request, or nil.
func (s *Sessions) Resolve(r *http.Request, userType db.UserType) *db.Session {
	c, err := r.Cookie(cookieName(userType))
	if err != nil {
		return nil
	}
	sess, err := s.store.LookupSession(r.Context(), c.Value, userType)
	if err != nil {
		return nil
	}
	return sess
}

// CredentialsFor returns the in-memory mail credentials for a session id.
func (s *Sessions) CredentialsFor(sessionID string) (Credentials, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.creds[sessionID]
	return c, ok
}

// SetCredentials replaces the in-memory IMAP/SMTP credentials for a live
// mailbox session. It is used after mailbox password changes and passkey login.
func (s *Sessions) SetCredentials(sessionID string, creds Credentials) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds[sessionID] = creds
}

// CSRFToken derives a stateless CSRF token bound to a session token.
func (s *Sessions) CSRFToken(sessionToken string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("csrf:" + db.HashSessionToken(sessionToken)))
	return hex.EncodeToString(mac.Sum(nil))
}

// CSRFForRequest recomputes the CSRF token for the session cookie on a
// request, so the `me`/status probes can hand it back to a client that lost
// it (browser restart, second tab). Returns "" if the cookie is absent.
func (s *Sessions) CSRFForRequest(r *http.Request, userType db.UserType) string {
	c, err := r.Cookie(cookieName(userType))
	if err != nil {
		return ""
	}
	return s.CSRFToken(c.Value)
}

// VerifyCSRF checks the CSRF header on a mutating request against the
// session cookie it claims to protect.
func (s *Sessions) VerifyCSRF(r *http.Request, userType db.UserType) bool {
	c, err := r.Cookie(cookieName(userType))
	if err != nil {
		return false
	}
	got := r.Header.Get(CSRFHeader)
	want := s.CSRFToken(c.Value)
	return got != "" && hmac.Equal([]byte(got), []byte(want))
}

// RequireSession resolves the session cookie for userType and enforces CSRF on
// mutating (non-GET/HEAD) requests. On failure it writes the 401/403 response
// itself and returns ok=false; signInMsg is the 401 body when no live session
// is present. This is the shared shell behind the admin, mailbox, and setup
// auth middlewares — the per-surface context loading stays with each caller.
func (s *Sessions) RequireSession(w http.ResponseWriter, r *http.Request, userType db.UserType, signInMsg string) (*db.Session, bool) {
	sess := s.Resolve(r, userType)
	if sess == nil {
		httpjson.Error(w, http.StatusUnauthorized, errors.New(signInMsg))
		return nil, false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if !s.VerifyCSRF(r, userType) {
			httpjson.Error(w, http.StatusForbidden, errors.New("session expired; please reload the page"))
			return nil, false
		}
	}
	return sess, true
}

// Sweep removes expired sessions and their cached credentials.
func (s *Sessions) Sweep(ctx context.Context) {
	_ = s.store.PruneSessions(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Credential entries are removed on logout; entries whose sessions
	// expired server-side are pruned by checking which session ids survive.
	rows, err := s.store.DB().QueryContext(ctx, `SELECT id FROM sessions WHERE user_type = 'mailbox'`)
	if err != nil {
		return
	}
	defer rows.Close()
	live := map[string]bool{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			live[id] = true
		}
	}
	for id := range s.creds {
		if !live[id] {
			delete(s.creds, id)
		}
	}
}
