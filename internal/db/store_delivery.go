package db

import (
	"context"
	"fmt"
)

// ---- outbound relays ----

const relayCols = `id, name, provider, host, port, username, encrypted_password, tls_mode, enabled, created_at, updated_at`

func scanRelay(scan func(dest ...any) error) (*OutboundRelay, error) {
	r := &OutboundRelay{}
	err := scan(&r.ID, &r.Name, &r.Provider, &r.Host, &r.Port, &r.Username, &r.EncryptedPassword, &r.TLSMode, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, scanErr("relay", err)
	}
	return r, nil
}

func (s *Store) CreateRelay(ctx context.Context, r OutboundRelay) (*OutboundRelay, error) {
	now := NowString()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO outbound_relays (name, provider, host, port, username, encrypted_password, tls_mode, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, r.Provider, r.Host, r.Port, r.Username, r.EncryptedPassword, string(r.TLSMode), r.Enabled, now, now)
	if err != nil {
		if isUnique(err) {
			return nil, fmt.Errorf("a relay named %q already exists", r.Name)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetRelay(ctx, id)
}

func (s *Store) GetRelay(ctx context.Context, id int64) (*OutboundRelay, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+relayCols+` FROM outbound_relays WHERE id = ?`, id)
	return scanRelay(row.Scan)
}

func (s *Store) ListRelays(ctx context.Context) ([]OutboundRelay, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+relayCols+` FROM outbound_relays ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboundRelay
	for rows.Next() {
		r, err := scanRelay(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateRelay(ctx context.Context, r OutboundRelay) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE outbound_relays SET name = ?, provider = ?, host = ?, port = ?, username = ?,
		 encrypted_password = ?, tls_mode = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		r.Name, r.Provider, r.Host, r.Port, r.Username, r.EncryptedPassword, string(r.TLSMode), r.Enabled, NowString(), r.ID)
	return err
}

func (s *Store) DeleteRelay(ctx context.Context, id int64) error {
	var inUse int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbound_policies WHERE relay_id = ? AND enabled = 1`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse > 0 {
		return fmt.Errorf("relay is used by %d delivery polic(ies); change those first", inUse)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM outbound_relays WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("relay")
	}
	return nil
}

// ---- outbound policies ----

const policyCols = `id, scope_type, scope_id, mode, relay_id, enabled, created_at, updated_at`

func scanPolicy(scan func(dest ...any) error) (*OutboundPolicy, error) {
	p := &OutboundPolicy{}
	err := scan(&p.ID, &p.ScopeType, &p.ScopeID, &p.Mode, &p.RelayID, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, scanErr("delivery policy", err)
	}
	return p, nil
}

// UpsertPolicy creates or replaces the policy for a scope.
func (s *Store) UpsertPolicy(ctx context.Context, scope PolicyScope, scopeID int64, mode DeliveryMode, relayID *int64) (*OutboundPolicy, error) {
	now := NowString()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO outbound_policies (scope_type, scope_id, mode, relay_id, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, ?, ?)
		 ON CONFLICT(scope_type, scope_id) DO UPDATE SET
		   mode = excluded.mode, relay_id = excluded.relay_id, enabled = 1, updated_at = excluded.updated_at`,
		string(scope), scopeID, string(mode), relayID, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetPolicyForScope(ctx, scope, scopeID)
}

func (s *Store) GetPolicyForScope(ctx context.Context, scope PolicyScope, scopeID int64) (*OutboundPolicy, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+policyCols+` FROM outbound_policies WHERE scope_type = ? AND scope_id = ?`,
		string(scope), scopeID)
	return scanPolicy(row.Scan)
}

func (s *Store) ListPolicies(ctx context.Context) ([]OutboundPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+policyCols+` FROM outbound_policies ORDER BY scope_type, scope_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboundPolicy
	for rows.Next() {
		p, err := scanPolicy(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) DeletePolicy(ctx context.Context, id int64) error {
	// The global default row is load-bearing; never delete it.
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM outbound_policies WHERE id = ? AND scope_type != 'global'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("delivery policy")
	}
	return nil
}
