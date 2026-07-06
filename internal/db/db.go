// Package db provides the SQLite control database: connection handling,
// migrations, and typed accessors for every wispbox entity.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/xiqi/wispbox/internal/migrations"

	_ "modernc.org/sqlite"
)

// Open opens (creating if necessary) the control database at path.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	// SQLite handles one writer at a time; a small pool avoids lock churn
	// and keeps memory flat on 512MB hosts.
	sqldb.SetMaxOpenConns(4)
	sqldb.SetMaxIdleConns(4)
	sqldb.SetConnMaxLifetime(0)
	if err := sqldb.Ping(); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("ping sqlite db: %w", err)
	}
	// The database holds mailbox password hashes and encrypted relay
	// credentials. Keep it owner+group readable (Dovecot's SQL auth runs as
	// the wispbox user) but never world-readable.
	if err := os.Chmod(path, 0o640); err != nil && !os.IsNotExist(err) {
		sqldb.Close()
		return nil, fmt.Errorf("secure db file: %w", err)
	}
	return sqldb, nil
}

// Migrate applies all pending schema migrations in lexical order.
// It is safe to call on every startup.
func Migrate(ctx context.Context, sqldb *sql.DB) (applied []string, err error) {
	if _, err := sqldb.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		var exists int
		if err := sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := sqldb.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, NowString()); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit migration %s: %w", name, err)
		}
		applied = append(applied, name)
	}
	return applied, nil
}

func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// TimeLayout is the canonical timestamp representation stored in SQLite.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// NowString returns the current time in the canonical stored format.
func NowString() string { return FormatTime(time.Now()) }

// FormatTime renders t in the canonical stored format (always UTC).
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseTime parses timestamps written either by Go or by SQLite defaults.
func ParseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		TimeLayout,
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
