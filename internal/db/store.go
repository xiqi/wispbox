package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// Store wraps the SQLite handle with typed accessors.
type Store struct {
	db *sql.DB
}

func NewStore(sqldb *sql.DB) *Store { return &Store{db: sqldb} }

// DB exposes the raw handle for backup and diagnostics.
func (s *Store) DB() *sql.DB { return s.db }

// ---- settings ----

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) GetSettingDefault(ctx context.Context, key, def string) string {
	v, err := s.GetSetting(ctx, key)
	if err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, NowString())
	return err
}

func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// IsInitialized reports whether first-run setup has completed.
func (s *Store) IsInitialized(ctx context.Context) bool {
	return s.GetSettingDefault(ctx, "initialized", "false") == "true"
}

// ---- sessions ----

// CreateSession stores a new session and returns the opaque token the
// client should hold. Only a SHA-256 of the token is persisted.
func (s *Store) CreateSession(ctx context.Context, userType UserType, userID int64, ttl time.Duration) (token string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token = hex.EncodeToString(raw)
	id := HashSessionToken(token)
	expires := FormatTime(time.Now().Add(ttl))
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_type, user_id, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, string(userType), userID, expires, NowString())
	if err != nil {
		return "", err
	}
	return token, nil
}

// HashSessionToken derives the stored session id from a client token.
func HashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// LookupSession resolves a client token to a live session, enforcing expiry
// and the expected user type. Session separation is enforced here: a mailbox
// session can never resolve as an admin session and vice versa.
func (s *Store) LookupSession(ctx context.Context, token string, want UserType) (*Session, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	var sess Session
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_type, user_id, expires_at, created_at FROM sessions WHERE id = ?`,
		HashSessionToken(token)).
		Scan(&sess.ID, &sess.UserType, &sess.UserID, &sess.ExpiresAt, &sess.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if sess.UserType != want {
		return nil, ErrNotFound
	}
	if exp, ok := ParseTime(sess.ExpiresAt); !ok || time.Now().UTC().After(exp) {
		_ = s.DeleteSessionByID(ctx, sess.ID)
		return nil, ErrNotFound
	}
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	return s.DeleteSessionByID(ctx, HashSessionToken(token))
}

func (s *Store) DeleteSessionByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// PruneSessions removes expired sessions; called periodically by the daemon.
func (s *Store) PruneSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, NowString())
	return err
}

// ---- audit log ----

func (s *Store) AppendAudit(ctx context.Context, entry AuditLog) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (actor_type, actor_id, action, target_type, target_id, ip, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.ActorType, entry.ActorID, entry.Action, entry.TargetType, entry.TargetID, entry.IP, NowString())
	return err
}

func (s *Store) RecentAudit(ctx context.Context, limit int) ([]AuditLog, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, actor_type, actor_id, action, target_type, target_id, ip, created_at
		 FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var a AuditLog
		if err := rows.Scan(&a.ID, &a.ActorType, &a.ActorID, &a.Action, &a.TargetType, &a.TargetID, &a.IP, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- service events ----

func (s *Store) AppendServiceEvent(ctx context.Context, ev ServiceEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO service_events (service, event_type, status, message, created_at) VALUES (?, ?, ?, ?, ?)`,
		ev.Service, ev.EventType, ev.Status, ev.Message, NowString())
	return err
}

func (s *Store) RecentServiceEvents(ctx context.Context, limit int) ([]ServiceEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, service, event_type, status, message, created_at
		 FROM service_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceEvent
	for rows.Next() {
		var e ServiceEvent
		if err := rows.Scan(&e.ID, &e.Service, &e.EventType, &e.Status, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) RecentServiceErrors(ctx context.Context, limit int) ([]ServiceEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, service, event_type, status, message, created_at
		 FROM service_events WHERE status = 'error' ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceEvent
	for rows.Next() {
		var e ServiceEvent
		if err := rows.Scan(&e.ID, &e.Service, &e.EventType, &e.Status, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanErr(entity string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notFound(entity)
	}
	return err
}

// notFound builds a consistent "<entity>: not found" error for lookups and
// for mutations that affected no rows.
func notFound(entity string) error {
	return fmt.Errorf("%s: %w", entity, ErrNotFound)
}

// isUnique reports whether err is a SQLite UNIQUE-constraint violation. The
// detection is by error text (the driver exposes no typed constraint error),
// so it lives in one place to fix if that ever changes.
func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE")
}
