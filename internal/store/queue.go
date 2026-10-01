package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	MaxAttempts    = 5
	lastErrorRunes = 500
)

type QueueJob struct {
	ID       int64
	Kind     string
	Payload  []byte
	Attempts int
}

func (s *Store) Enqueue(ctx context.Context, kind string, payload any, dedupeKey string) (bool, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("store: encode %s payload: %w", kind, err)
	}
	var key *string
	if dedupeKey != "" {
		key = &dedupeKey
	}
	var id int64
	err = s.pool.QueryRow(ctx, `INSERT INTO queue_jobs (kind, payload, dedupe_key)
VALUES ($1, $2, $3)
ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('pending', 'running') DO NOTHING
RETURNING id`, kind, data, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: enqueue %s: %w", kind, err)
	}
	return true, nil
}

func (s *Store) Dequeue(ctx context.Context) (*QueueJob, error) {
	var j QueueJob
	err := s.pool.QueryRow(ctx, `UPDATE queue_jobs
SET status = 'running', locked_at = now(), attempts = attempts + 1, updated_at = now()
WHERE id = (
    SELECT id FROM queue_jobs
    WHERE status = 'pending' AND run_after <= now()
    ORDER BY run_after, id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING id, kind, payload, attempts`).Scan(&j.ID, &j.Kind, &j.Payload, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: dequeue: %w", err)
	}
	return &j, nil
}

var ErrLeaseLost = errors.New("store: job lease lost")

func (s *Store) fenced(ctx context.Context, op string, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("store: %s: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: %s: %w", op, ErrLeaseLost)
	}
	return nil
}

func errorText(cause error) string {
	if cause == nil {
		return ""
	}
	return truncateRunes(cause.Error(), lastErrorRunes)
}

func (s *Store) Complete(ctx context.Context, id int64, attempts int) error {
	return s.fenced(ctx, "complete job", `UPDATE queue_jobs
SET status = 'done', locked_at = NULL, updated_at = now()
WHERE id = $1 AND status = 'running' AND attempts = $2`, id, attempts)
}

func (s *Store) Fail(ctx context.Context, id int64, attempts int, cause error) error {
	return s.fenced(ctx, "fail job", `UPDATE queue_jobs
SET status = CASE WHEN attempts >= $4 THEN 'dead' ELSE 'pending' END,
    run_after = CASE WHEN attempts >= $4 THEN run_after
        ELSE now() + LEAST(interval '30 seconds' * power(2, GREATEST(attempts - 1, 0)), interval '30 minutes') END,
    locked_at = NULL,
    last_error = $3,
    updated_at = now()
WHERE id = $1 AND status = 'running' AND attempts = $2`, id, attempts, errorText(cause), MaxAttempts)
}

func (s *Store) Kill(ctx context.Context, id int64, attempts int, cause error) error {
	return s.fenced(ctx, "kill job", `UPDATE queue_jobs
SET status = 'dead', locked_at = NULL, last_error = $3, updated_at = now()
WHERE id = $1 AND status = 'running' AND attempts = $2`, id, attempts, errorText(cause))
}

func (s *Store) Defer(ctx context.Context, id int64, attempts int, until time.Time) error {
	return s.fenced(ctx, "defer job", `UPDATE queue_jobs
SET status = 'pending', run_after = $3, attempts = GREATEST(attempts - 1, 0), locked_at = NULL, updated_at = now()
WHERE id = $1 AND status = 'running' AND attempts = $2`, id, attempts, until)
}

func (s *Store) ReapStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE queue_jobs
SET status = CASE WHEN attempts >= $2 THEN 'dead' ELSE 'pending' END,
    last_error = CASE WHEN attempts >= $2 THEN 'job lease expired' ELSE last_error END,
    locked_at = NULL,
    updated_at = now()
WHERE status = 'running' AND locked_at < now() - make_interval(secs => $1)`, olderThan.Seconds(), MaxAttempts)
	if err != nil {
		return 0, fmt.Errorf("store: reap stale jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) PruneQueue(ctx context.Context, doneOlderThan, deadOlderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM queue_jobs
WHERE (status = 'done' AND updated_at < now() - make_interval(secs => $1))
   OR (status = 'dead' AND updated_at < now() - make_interval(secs => $2))`, doneOlderThan.Seconds(), deadOlderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("store: prune queue: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) PendingCount(ctx context.Context) (int64, error) {
	var n int64
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM queue_jobs WHERE status = 'pending'").Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count pending jobs: %w", err)
	}
	return n, nil
}
