package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Installation struct {
	ID           int64
	AccountLogin string
	AccountType  string
}

type Repository struct {
	ID      int64
	Owner   string
	Name    string
	Private bool
}

func (s *Store) UpsertInstallation(ctx context.Context, inst Installation) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO installations (id, account_login, account_type)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET account_login = EXCLUDED.account_login, account_type = EXCLUDED.account_type, updated_at = now()`,
		inst.ID, inst.AccountLogin, inst.AccountType); err != nil {
		return fmt.Errorf("store: upsert installation: %w", err)
	}
	return nil
}

func (s *Store) DeleteInstallation(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, "DELETE FROM installations WHERE id = $1", id); err != nil {
		return fmt.Errorf("store: delete installation: %w", err)
	}
	return nil
}

func (s *Store) SetInstallationSuspended(ctx context.Context, id int64, suspended bool) error {
	if _, err := s.pool.Exec(ctx, "UPDATE installations SET suspended = $2, updated_at = now() WHERE id = $1", id, suspended); err != nil {
		return fmt.Errorf("store: set installation suspended: %w", err)
	}
	return nil
}

func (s *Store) InstallationSuspended(ctx context.Context, id int64) (bool, error) {
	var suspended bool
	err := s.pool.QueryRow(ctx, "SELECT suspended FROM installations WHERE id = $1", id).Scan(&suspended)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: read installation: %w", err)
	}
	return suspended, nil
}

func (s *Store) UpsertRepositories(ctx context.Context, installationID int64, repos []Repository) error {
	if len(repos) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range repos {
		batch.Queue(`INSERT INTO repositories (id, installation_id, owner, name, private)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET installation_id = EXCLUDED.installation_id, owner = EXCLUDED.owner, name = EXCLUDED.name, private = EXCLUDED.private, updated_at = now()`,
			r.ID, installationID, r.Owner, r.Name, r.Private)
	}
	if err := s.pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("store: upsert repositories: %w", err)
	}
	return nil
}

func (s *Store) RemoveRepositories(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM repositories WHERE id = ANY($1)", ids); err != nil {
		return fmt.Errorf("store: remove repositories: %w", err)
	}
	return nil
}
