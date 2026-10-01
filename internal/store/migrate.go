package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
)

const migrationLockID = 7310254

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: read migrations: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := migrationFiles.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("store: read migration %s: %w", e.Name(), err)
		}
		out = append(out, migration{version: e.Name(), sql: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func (s *Store) Migrate(ctx context.Context) (applied []string, err error) {
	migrations, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: acquire migration connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(migrationLockID)); err != nil {
		return nil, fmt.Errorf("store: acquire migration lock: %w", err)
	}
	defer func() {
		bg := context.WithoutCancel(ctx)
		if _, uerr := conn.Exec(bg, "SELECT pg_advisory_unlock($1)", int64(migrationLockID)); uerr != nil {
			err = errors.Join(err, fmt.Errorf("store: release migration lock: %w", uerr))
			if cerr := conn.Conn().Close(bg); cerr != nil {
				err = errors.Join(err, fmt.Errorf("store: close migration connection: %w", cerr))
			}
		}
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`); err != nil {
		return nil, fmt.Errorf("store: create schema_migrations: %w", err)
	}
	done := map[string]bool{}
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("store: list applied migrations: %w", err)
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("store: list applied migrations: %w", err)
	}
	for _, v := range versions {
		done[v] = true
	}
	applied = []string{}
	for _, m := range migrations {
		if done[m.version] {
			continue
		}
		if err := applyMigration(ctx, conn.Conn(), m); err != nil {
			return applied, err
		}
		applied = append(applied, m.version)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, conn *pgx.Conn, m migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin migration %s: %w", m.version, err)
	}
	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return rollback(ctx, tx, fmt.Errorf("store: apply migration %s: %w", m.version, err))
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", m.version); err != nil {
		return rollback(ctx, tx, fmt.Errorf("store: record migration %s: %w", m.version, err))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit migration %s: %w", m.version, err)
	}
	return nil
}

func rollback(ctx context.Context, tx pgx.Tx, cause error) error {
	if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil {
		return errors.Join(cause, fmt.Errorf("store: rollback: %w", err))
	}
	return cause
}
