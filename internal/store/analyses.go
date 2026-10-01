package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Analysis struct {
	RepositoryID int64
	PRNumber     int
	HeadSHA      string
	BaseSHA      string
	Result       []byte
	FindingCount int
	CheckRunID   int64
	DurationMS   int
}

func (s *Store) SaveAnalysis(ctx context.Context, a Analysis) error {
	var checkRun *int64
	if a.CheckRunID != 0 {
		checkRun = &a.CheckRunID
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO analyses (repository_id, pr_number, head_sha, base_sha, result, finding_count, check_run_id, duration_ms)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (repository_id, pr_number, head_sha) DO UPDATE SET
    base_sha = EXCLUDED.base_sha,
    result = EXCLUDED.result,
    finding_count = EXCLUDED.finding_count,
    check_run_id = EXCLUDED.check_run_id,
    duration_ms = EXCLUDED.duration_ms,
    created_at = now()`,
		a.RepositoryID, a.PRNumber, a.HeadSHA, a.BaseSHA, a.Result, a.FindingCount, checkRun, a.DurationMS); err != nil {
		return fmt.Errorf("store: save analysis: %w", err)
	}
	return nil
}

func (s *Store) CommentID(ctx context.Context, repositoryID int64, prNumber int) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, "SELECT comment_id FROM pr_comments WHERE repository_id = $1 AND pr_number = $2", repositoryID, prNumber).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: read comment ID: %w", err)
	}
	return id, true, nil
}

func (s *Store) SetCommentID(ctx context.Context, repositoryID int64, prNumber int, commentID int64) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO pr_comments (repository_id, pr_number, comment_id)
VALUES ($1, $2, $3)
ON CONFLICT (repository_id, pr_number) DO UPDATE SET comment_id = EXCLUDED.comment_id, updated_at = now()`,
		repositoryID, prNumber, commentID); err != nil {
		return fmt.Errorf("store: set comment ID: %w", err)
	}
	return nil
}
