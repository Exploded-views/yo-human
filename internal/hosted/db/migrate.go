package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads migrations/NNNN_name.sql, in version order. Versions must run 1, 2, 3...
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations: %w", err)
	}
	var ms []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		v, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil {
			return nil, fmt.Errorf("db: migration %q is not named NNNN_name.sql", e.Name())
		}
		body, err := fs.ReadFile(migrationFS, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("db: read migration %s: %w", e.Name(), err)
		}
		ms = append(ms, migration{version: v, name: e.Name(), sql: string(body)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("db: migration %s: versions must run 1, 2, 3 without a gap", m.name)
		}
	}
	return ms, nil
}

// migrate brings the database to the latest user_version, each migration in its own
// transaction. A database newer than this binary is refused.
func migrate(ctx context.Context, d *sql.DB) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	var have int
	if err := d.QueryRowContext(ctx, "PRAGMA user_version").Scan(&have); err != nil {
		return fmt.Errorf("db: read user_version: %w", err)
	}
	if have > len(ms) {
		return fmt.Errorf("db: schema version %d is newer than this binary's %d", have, len(ms))
	}
	for _, m := range ms[have:] {
		if err := apply(ctx, d, m); err != nil {
			return err
		}
	}
	return nil
}

func apply(ctx context.Context, d *sql.DB, m migration) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: migration %s: %w", m.name, err)
	}
	// Re-read under the write lock: another process may have migrated since the check above.
	var have int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&have); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("db: migration %s: %w", m.name, err)
	}
	if have >= m.version {
		return tx.Rollback()
	}
	// user_version is part of the transaction, so a failed migration leaves the old version.
	if _, err := tx.ExecContext(ctx, m.sql+fmt.Sprintf("\nPRAGMA user_version = %d;", m.version)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("db: migration %s: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: migration %s: %w", m.name, err)
	}
	return nil
}
