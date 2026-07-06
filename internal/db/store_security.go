package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

func randomToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func randomHandle() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Store) EnsureWebAuthnHandle(ctx context.Context, userType UserType, userID int64) (string, error) {
	switch userType {
	case UserAdmin:
		a, err := s.GetAdmin(ctx, userID)
		if err != nil {
			return "", err
		}
		if a.WebAuthnHandle != "" {
			return a.WebAuthnHandle, nil
		}
		handle, err := randomHandle()
		if err != nil {
			return "", err
		}
		return handle, s.UpdateAdminWebAuthnHandle(ctx, userID, handle)
	case UserMailbox:
		m, err := s.GetMailbox(ctx, userID)
		if err != nil {
			return "", err
		}
		if m.WebAuthnHandle != "" {
			return m.WebAuthnHandle, nil
		}
		handle, err := randomHandle()
		if err != nil {
			return "", err
		}
		return handle, s.UpdateMailboxWebAuthnHandle(ctx, userID, handle)
	default:
		return "", fmt.Errorf("unknown user type %q", userType)
	}
}

func (s *Store) GetUserByWebAuthnHandle(ctx context.Context, userType UserType, handle string) (userID int64, err error) {
	switch userType {
	case UserAdmin:
		err = s.db.QueryRowContext(ctx, `SELECT id FROM admins WHERE webauthn_handle = ?`, handle).Scan(&userID)
	case UserMailbox:
		err = s.db.QueryRowContext(ctx, `SELECT id FROM mailboxes WHERE webauthn_handle = ?`, handle).Scan(&userID)
	default:
		return 0, fmt.Errorf("unknown user type %q", userType)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return userID, err
}

func (s *Store) SavePasskey(ctx context.Context, p Passkey) (*Passkey, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		p.Name = "Passkey"
	}
	if len(p.Name) > 80 {
		p.Name = p.Name[:80]
	}
	now := NowString()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO passkeys (user_type, user_id, rp_id, credential_id, name, encrypted_credential, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(p.UserType), p.UserID, p.RPID, p.CredentialID, p.Name, p.EncryptedCredential, now, now)
	if err != nil {
		if isUnique(err) {
			return nil, fmt.Errorf("this passkey is already registered")
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetPasskey(ctx, id)
}

func scanPasskey(scan func(dest ...any) error) (*Passkey, error) {
	p := &Passkey{}
	var userType string
	err := scan(&p.ID, &userType, &p.UserID, &p.RPID, &p.CredentialID, &p.Name, &p.EncryptedCredential, &p.CreatedAt, &p.UpdatedAt, &p.LastUsedAt)
	if err != nil {
		return nil, scanErr("passkey", err)
	}
	p.UserType = UserType(userType)
	return p, nil
}

const passkeyCols = `id, user_type, user_id, rp_id, credential_id, name, encrypted_credential, created_at, updated_at, last_used_at`

func (s *Store) GetPasskey(ctx context.Context, id int64) (*Passkey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+passkeyCols+` FROM passkeys WHERE id = ?`, id)
	return scanPasskey(row.Scan)
}

func (s *Store) GetPasskeyByCredential(ctx context.Context, rpID, credentialID string) (*Passkey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+passkeyCols+` FROM passkeys WHERE rp_id = ? AND credential_id = ?`, rpID, credentialID)
	return scanPasskey(row.Scan)
}

func (s *Store) ListPasskeys(ctx context.Context, userType UserType, userID int64, rpID string) ([]Passkey, error) {
	q := `SELECT ` + passkeyCols + ` FROM passkeys WHERE user_type = ? AND user_id = ?`
	args := []any{string(userType), userID}
	if rpID != "" {
		q += ` AND rp_id = ?`
		args = append(args, rpID)
	}
	q += ` ORDER BY created_at DESC, id DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		p, err := scanPasskey(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) CountPasskeys(ctx context.Context, userType UserType, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM passkeys WHERE user_type = ? AND user_id = ?`, string(userType), userID).Scan(&n)
	return n, err
}

func (s *Store) UpdatePasskeyCredential(ctx context.Context, id int64, encryptedCredential string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE passkeys SET encrypted_credential = ?, last_used_at = ?, updated_at = ? WHERE id = ?`,
		encryptedCredential, NowString(), NowString(), id)
	return err
}

func (s *Store) DeletePasskey(ctx context.Context, userType UserType, userID, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM passkeys WHERE id = ? AND user_type = ? AND user_id = ?`, id, string(userType), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("passkey")
	}
	return nil
}

func (s *Store) CreateAuthChallenge(ctx context.Context, userType UserType, userID int64, kind string, sessionData string, ttl time.Duration) (string, error) {
	id, err := randomToken(32)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO auth_challenges (id, user_type, user_id, kind, session_data, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, string(userType), userID, kind, sessionData, NowString(), FormatTime(time.Now().Add(ttl)))
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) GetAuthChallenge(ctx context.Context, id, kind string) (*AuthChallenge, error) {
	c := &AuthChallenge{}
	var userType string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_type, user_id, kind, session_data, created_at, expires_at
		 FROM auth_challenges WHERE id = ? AND kind = ?`, id, kind).
		Scan(&c.ID, &userType, &c.UserID, &c.Kind, &c.SessionData, &c.CreatedAt, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.UserType = UserType(userType)
	if exp, ok := ParseTime(c.ExpiresAt); !ok || time.Now().UTC().After(exp) {
		_ = s.DeleteAuthChallenge(ctx, id)
		return nil, ErrNotFound
	}
	return c, nil
}

func (s *Store) DeleteAuthChallenge(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_challenges WHERE id = ?`, id)
	return err
}

func (s *Store) PruneAuthChallenges(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_challenges WHERE expires_at <= ?`, NowString())
	return err
}
