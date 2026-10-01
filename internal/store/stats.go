package store

import (
	"context"
	"fmt"
	"time"

	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/jackc/pgx/v5"
)

const statsWindow = 30

type JobRun struct {
	JobID           int64
	RepositoryID    int64
	RunID           int64
	WorkflowPath    string
	JobKey          string
	RunnerLabels    []string
	Conclusion      string
	StartedAt       time.Time
	CompletedAt     time.Time
	DurationSeconds int
}

func (s *Store) InsertJobRuns(ctx context.Context, runs []JobRun) error {
	if len(runs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range runs {
		labels := r.RunnerLabels
		if labels == nil {
			labels = []string{}
		}
		batch.Queue(`INSERT INTO job_runs (job_id, repository_id, run_id, workflow_path, job_key, runner_labels, conclusion, started_at, completed_at, duration_seconds)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT DO NOTHING`,
			r.JobID, r.RepositoryID, r.RunID, r.WorkflowPath, r.JobKey, labels, r.Conclusion, r.StartedAt, r.CompletedAt, r.DurationSeconds)
	}
	if err := s.pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("store: insert job runs: %w", err)
	}
	return nil
}

func (s *Store) RecomputeJobStats(ctx context.Context, repositoryID int64, workflowPath, jobKey string) error {
	tag, err := s.pool.Exec(ctx, `WITH recent AS (
    SELECT duration_seconds FROM job_runs
    WHERE repository_id = $1 AND workflow_path = $2 AND job_key = $3 AND conclusion = 'success'
    ORDER BY completed_at DESC, job_id DESC
    LIMIT $4
)
INSERT INTO job_stats (repository_id, workflow_path, job_key, avg_seconds, p50_seconds, sample_count)
SELECT $1, $2, $3,
    round(avg(duration_seconds))::int,
    round(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_seconds))::int,
    count(*)
FROM recent
HAVING count(*) > 0
ON CONFLICT (repository_id, workflow_path, job_key) DO UPDATE SET
    avg_seconds = EXCLUDED.avg_seconds,
    p50_seconds = EXCLUDED.p50_seconds,
    sample_count = EXCLUDED.sample_count,
    updated_at = now()`, repositoryID, workflowPath, jobKey, statsWindow)
	if err != nil {
		return fmt.Errorf("store: recompute job stats: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM job_stats WHERE repository_id = $1 AND workflow_path = $2 AND job_key = $3", repositoryID, workflowPath, jobKey); err != nil {
		return fmt.Errorf("store: clear job stats: %w", err)
	}
	return nil
}

type statKey struct {
	path string
	job  string
}

type statValue struct {
	avg     time.Duration
	samples int
}

type durationTable map[statKey]statValue

func (t durationTable) JobAverage(workflowPath, jobKey string) (time.Duration, int, bool) {
	v, ok := t[statKey{path: workflowPath, job: jobKey}]
	if !ok {
		return 0, 0, false
	}
	return v.avg, v.samples, true
}

func (s *Store) Durations(ctx context.Context, repositoryID int64) (semdiff.DurationSource, error) {
	rows, err := s.pool.Query(ctx, "SELECT workflow_path, job_key, avg_seconds, sample_count FROM job_stats WHERE repository_id = $1", repositoryID)
	if err != nil {
		return nil, fmt.Errorf("store: read job stats: %w", err)
	}
	defer rows.Close()
	table := durationTable{}
	for rows.Next() {
		var k statKey
		var avg, samples int
		if err := rows.Scan(&k.path, &k.job, &avg, &samples); err != nil {
			return nil, fmt.Errorf("store: scan job stats: %w", err)
		}
		table[k] = statValue{avg: time.Duration(avg) * time.Second, samples: samples}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read job stats: %w", err)
	}
	return table, nil
}
