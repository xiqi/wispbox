package db

import (
	"context"
	"fmt"
	"strings"
)

// ---- domains ----

func (s *Store) CreateDomain(ctx context.Context, name, mailHostname string) (*Domain, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if mailHostname == "" {
		mailHostname = DefaultMailHostname(name)
	}
	mailHostname = strings.ToLower(strings.TrimSpace(mailHostname))
	if err := ValidateDomainName(name); err != nil {
		return nil, err
	}
	if err := ValidateHostname(mailHostname); err != nil {
		return nil, err
	}
	now := NowString()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO domains (name, mail_hostname, status, created_at, updated_at) VALUES (?, ?, 'pending', ?, ?)`,
		name, mailHostname, now, now)
	if err != nil {
		if isUnique(err) {
			return nil, fmt.Errorf("domain %s already exists", name)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetDomain(ctx, id)
}

func (s *Store) GetDomain(ctx context.Context, id int64) (*Domain, error) {
	d := &Domain{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, mail_hostname, status, created_at, updated_at FROM domains WHERE id = ?`, id).
		Scan(&d.ID, &d.Name, &d.MailHostname, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, scanErr("domain", err)
	}
	return d, nil
}

func (s *Store) GetDomainByName(ctx context.Context, name string) (*Domain, error) {
	d := &Domain{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, mail_hostname, status, created_at, updated_at FROM domains WHERE name = ?`,
		strings.ToLower(strings.TrimSpace(name))).
		Scan(&d.ID, &d.Name, &d.MailHostname, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, scanErr("domain", err)
	}
	return d, nil
}

func (s *Store) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, mail_hostname, status, created_at, updated_at FROM domains ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Domain
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.Name, &d.MailHostname, &d.Status, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) UpdateDomainStatus(ctx context.Context, id int64, status DomainStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE domains SET status = ?, updated_at = ? WHERE id = ?`, string(status), NowString(), id)
	return err
}

func (s *Store) DeleteDomain(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("domain")
	}
	return nil
}

func (s *Store) CountMailboxes(ctx context.Context, domainID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mailboxes WHERE domain_id = ?`, domainID).Scan(&n)
	return n, err
}

// ---- mailboxes ----

func (s *Store) CreateMailbox(ctx context.Context, domainID int64, localPart, passwordHash string, quotaMB int64) (*Mailbox, error) {
	localPart = strings.ToLower(strings.TrimSpace(localPart))
	if err := ValidateLocalPart(localPart); err != nil {
		return nil, err
	}
	dom, err := s.GetDomain(ctx, domainID)
	if err != nil {
		return nil, err
	}
	if quotaMB <= 0 {
		quotaMB = 1024
	}
	email := localPart + "@" + dom.Name
	now := NowString()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO mailboxes (domain_id, local_part, email, password_hash, quota_mb, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		domainID, localPart, email, passwordHash, quotaMB, now, now)
	if err != nil {
		if isUnique(err) {
			return nil, fmt.Errorf("mailbox %s already exists", email)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetMailbox(ctx, id)
}

const mailboxCols = `id, domain_id, local_part, email, password_hash, webauthn_handle, two_factor_enabled, encrypted_totp_secret, encrypted_passkey_password, quota_mb, enabled, created_at, updated_at`

func scanMailbox(scan func(dest ...any) error) (*Mailbox, error) {
	m := &Mailbox{}
	err := scan(&m.ID, &m.DomainID, &m.LocalPart, &m.Email, &m.PasswordHash, &m.WebAuthnHandle, &m.TwoFactorEnabled, &m.EncryptedTOTPSecret, &m.EncryptedPasskeyPassword, &m.QuotaMB, &m.Enabled, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, scanErr("mailbox", err)
	}
	return m, nil
}

func (s *Store) GetMailbox(ctx context.Context, id int64) (*Mailbox, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+mailboxCols+` FROM mailboxes WHERE id = ?`, id)
	return scanMailbox(row.Scan)
}

func (s *Store) GetMailboxByEmail(ctx context.Context, email string) (*Mailbox, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+mailboxCols+` FROM mailboxes WHERE email = ?`,
		strings.ToLower(strings.TrimSpace(email)))
	return scanMailbox(row.Scan)
}

func (s *Store) ListMailboxes(ctx context.Context, domainID int64) ([]Mailbox, error) {
	q := `SELECT ` + mailboxCols + ` FROM mailboxes ORDER BY email`
	args := []any{}
	if domainID > 0 {
		q = `SELECT ` + mailboxCols + ` FROM mailboxes WHERE domain_id = ? ORDER BY email`
		args = append(args, domainID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mailbox
	for rows.Next() {
		m, err := scanMailbox(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *Store) UpdateMailbox(ctx context.Context, id int64, quotaMB int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE mailboxes SET quota_mb = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		quotaMB, enabled, NowString(), id)
	return err
}

func (s *Store) UpdateMailboxPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE mailboxes SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, NowString(), id)
	return err
}

func (s *Store) UpdateMailboxPasswordAndPasskeySecret(ctx context.Context, id int64, passwordHash, encryptedPasskeyPassword string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE mailboxes SET password_hash = ?, encrypted_passkey_password = ?, updated_at = ? WHERE id = ?`,
		passwordHash, encryptedPasskeyPassword, NowString(), id)
	return err
}

func (s *Store) UpdateMailboxTOTP(ctx context.Context, id int64, enabled bool, encryptedSecret string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE mailboxes SET two_factor_enabled = ?, encrypted_totp_secret = ?, updated_at = ? WHERE id = ?`,
		enabled, encryptedSecret, NowString(), id)
	return err
}

func (s *Store) UpdateMailboxWebAuthnHandle(ctx context.Context, id int64, handle string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE mailboxes SET webauthn_handle = ?, updated_at = ? WHERE id = ?`, handle, NowString(), id)
	return err
}

func (s *Store) DeleteMailbox(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mailboxes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("mailbox")
	}
	return nil
}

// ---- aliases ----

func (s *Store) CreateAlias(ctx context.Context, domainID int64, source, destination string, isCatchAll bool) (*Alias, error) {
	dom, err := s.GetDomain(ctx, domainID)
	if err != nil {
		return nil, err
	}
	destination = strings.ToLower(strings.TrimSpace(destination))
	if err := ValidateEmail(destination); err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}
	if isCatchAll {
		source = "@" + dom.Name
	} else {
		source = strings.ToLower(strings.TrimSpace(source))
		lp, sdom, ok := strings.Cut(source, "@")
		if !ok {
			// Accept a bare local part and qualify it with the domain.
			lp, sdom = source, dom.Name
		}
		if sdom != dom.Name {
			return nil, fmt.Errorf("alias source must be on %s", dom.Name)
		}
		if err := ValidateLocalPart(lp); err != nil {
			return nil, fmt.Errorf("source: %w", err)
		}
		source = lp + "@" + dom.Name
	}
	if source == destination {
		return nil, fmt.Errorf("alias cannot point to itself")
	}
	now := NowString()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO aliases (domain_id, source, destination, is_catch_all, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, ?, ?)`,
		domainID, source, destination, isCatchAll, now, now)
	if err != nil {
		if isUnique(err) {
			return nil, fmt.Errorf("alias %s → %s already exists", source, destination)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetAlias(ctx, id)
}

const aliasCols = `id, domain_id, source, destination, is_catch_all, enabled, created_at, updated_at`

func scanAlias(scan func(dest ...any) error) (*Alias, error) {
	a := &Alias{}
	err := scan(&a.ID, &a.DomainID, &a.Source, &a.Destination, &a.IsCatchAll, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, scanErr("alias", err)
	}
	return a, nil
}

func (s *Store) GetAlias(ctx context.Context, id int64) (*Alias, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+aliasCols+` FROM aliases WHERE id = ?`, id)
	return scanAlias(row.Scan)
}

func (s *Store) ListAliases(ctx context.Context, domainID int64) ([]Alias, error) {
	q := `SELECT ` + aliasCols + ` FROM aliases ORDER BY source`
	args := []any{}
	if domainID > 0 {
		q = `SELECT ` + aliasCols + ` FROM aliases WHERE domain_id = ? ORDER BY source`
		args = append(args, domainID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alias
	for rows.Next() {
		a, err := scanAlias(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ListAliasSourcesForDestination returns enabled alias sources that deliver
// to the given mailbox address (used for sender identity authorization).
func (s *Store) ListAliasSourcesForDestination(ctx context.Context, destination string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source FROM aliases WHERE destination = ? AND enabled = 1`,
		strings.ToLower(strings.TrimSpace(destination)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) UpdateAliasEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE aliases SET enabled = ?, updated_at = ? WHERE id = ?`, enabled, NowString(), id)
	return err
}

func (s *Store) DeleteAlias(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM aliases WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("alias")
	}
	return nil
}
