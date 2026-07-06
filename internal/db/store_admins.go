package db

import (
	"context"
	"strings"
)

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string) (*Admin, error) {
	now := NowString()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO admins (username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		strings.ToLower(strings.TrimSpace(username)), passwordHash, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetAdmin(ctx, id)
}

func (s *Store) GetAdmin(ctx context.Context, id int64) (*Admin, error) {
	a := &Admin{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, webauthn_handle, two_factor_enabled, encrypted_totp_secret, created_at, updated_at FROM admins WHERE id = ?`, id).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.WebAuthnHandle, &a.TwoFactorEnabled, &a.EncryptedTOTPSecret, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, scanErr("admin", err)
	}
	return a, nil
}

func (s *Store) GetAdminByUsername(ctx context.Context, username string) (*Admin, error) {
	a := &Admin{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, webauthn_handle, two_factor_enabled, encrypted_totp_secret, created_at, updated_at FROM admins WHERE username = ?`,
		strings.ToLower(strings.TrimSpace(username))).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.WebAuthnHandle, &a.TwoFactorEnabled, &a.EncryptedTOTPSecret, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, scanErr("admin", err)
	}
	return a, nil
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

func (s *Store) UpdateAdminPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE admins SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, NowString(), id)
	return err
}

func (s *Store) UpdateAdminTOTP(ctx context.Context, id int64, enabled bool, encryptedSecret string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE admins SET two_factor_enabled = ?, encrypted_totp_secret = ?, updated_at = ? WHERE id = ?`,
		enabled, encryptedSecret, NowString(), id)
	return err
}

func (s *Store) UpdateAdminWebAuthnHandle(ctx context.Context, id int64, handle string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE admins SET webauthn_handle = ?, updated_at = ? WHERE id = ?`, handle, NowString(), id)
	return err
}
