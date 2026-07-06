package db

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const certCols = `id, COALESCE(domain_id, 0), hostname, status, challenge_type, cert_path, key_path,
	COALESCE(not_before,''), COALESCE(not_after,''), COALESCE(last_renewed_at,''), COALESCE(renew_after,''),
	last_error, created_at, updated_at`

func scanCert(scan func(dest ...any) error) (*Certificate, error) {
	c := &Certificate{}
	err := scan(&c.ID, &c.DomainID, &c.Hostname, &c.Status, &c.ChallengeType, &c.CertPath, &c.KeyPath,
		&c.NotBefore, &c.NotAfter, &c.LastRenewedAt, &c.RenewAfter, &c.LastError, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, scanErr("certificate", err)
	}
	return c, nil
}

func (s *Store) CreateCertificate(ctx context.Context, domainID int64, hostname string) (*Certificate, error) {
	now := NowString()
	var domain any
	if domainID > 0 {
		domain = domainID
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO certificates (domain_id, hostname, status, challenge_type, created_at, updated_at)
		 VALUES (?, ?, 'pending', 'http-01', ?, ?)`,
		domain, strings.ToLower(hostname), now, now)
	if err != nil {
		if isUnique(err) {
			return nil, fmt.Errorf("a certificate for %s already exists", hostname)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetCertificate(ctx, id)
}

func (s *Store) GetCertificate(ctx context.Context, id int64) (*Certificate, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+certCols+` FROM certificates WHERE id = ?`, id)
	return scanCert(row.Scan)
}

func (s *Store) GetCertificateByHostname(ctx context.Context, hostname string) (*Certificate, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+certCols+` FROM certificates WHERE hostname = ?`,
		strings.ToLower(hostname))
	return scanCert(row.Scan)
}

func (s *Store) ListCertificates(ctx context.Context) ([]Certificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+certCols+` FROM certificates ORDER BY hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Certificate
	for rows.Next() {
		c, err := scanCert(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) UpdateCertificateStatus(ctx context.Context, id int64, status CertStatus, lastError string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE certificates SET status = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		string(status), lastError, NowString(), id)
	return err
}

// MarkCertificateIssued records a successful issuance/renewal.
func (s *Store) MarkCertificateIssued(ctx context.Context, id int64, challengeType, certPath, keyPath string, notBefore, notAfter, renewAfter time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE certificates SET status = 'active', challenge_type = ?, cert_path = ?, key_path = ?,
		 not_before = ?, not_after = ?, last_renewed_at = ?, renew_after = ?, last_error = '', updated_at = ?
		 WHERE id = ?`,
		challengeType, certPath, keyPath,
		FormatTime(notBefore), FormatTime(notAfter),
		NowString(), FormatTime(renewAfter), NowString(), id)
	return err
}

// SetCertificateRenewAfter pushes the next renewal attempt time (backoff or
// forced renewal via `wispboxctl cert renew`).
func (s *Store) SetCertificateRenewAfter(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE certificates SET renew_after = ?, updated_at = ? WHERE id = ?`,
		FormatTime(at), NowString(), id)
	return err
}

// CertificatesDueForRenewal returns certificates whose renew_after has passed
// or that have never been issued.
func (s *Store) CertificatesDueForRenewal(ctx context.Context) ([]Certificate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+certCols+` FROM certificates
		 WHERE renew_after IS NULL OR renew_after = '' OR renew_after <= ?
		 ORDER BY hostname`, NowString())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Certificate
	for rows.Next() {
		c, err := scanCert(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
