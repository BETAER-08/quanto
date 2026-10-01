package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDeliveries(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	seen, err := s.DeliverySeen(ctx, "d1")
	if err != nil || seen {
		t.Fatalf("seen before record = %v, %v", seen, err)
	}
	for i := 0; i < 2; i++ {
		if err := s.RecordDelivery(ctx, "d1", "ping"); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	seen, err = s.DeliverySeen(ctx, "d1")
	if err != nil || !seen {
		t.Fatalf("seen after record = %v, %v", seen, err)
	}
	if err := s.RecordDelivery(ctx, "d2", "ping"); err != nil {
		t.Fatalf("record: %v", err)
	}
	mustExec(t, s, "UPDATE webhook_deliveries SET received_at = now() - interval '8 days' WHERE delivery_id = 'd1'")
	n, err := s.PruneDeliveries(ctx, 7*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	seen, err = s.DeliverySeen(ctx, "d2")
	if err != nil || !seen {
		t.Fatalf("d2 seen = %v, %v", seen, err)
	}
}

func TestInstallations(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	suspended, err := s.InstallationSuspended(ctx, 1)
	if err != nil || suspended {
		t.Fatalf("missing installation = %v, %v", suspended, err)
	}
	seedRepo(t, s, 1, 10)
	if err := s.SetInstallationSuspended(ctx, 1, true); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	suspended, err = s.InstallationSuspended(ctx, 1)
	if err != nil || !suspended {
		t.Fatalf("suspended = %v, %v", suspended, err)
	}
	if err := s.UpsertInstallation(ctx, Installation{ID: 1, AccountLogin: "renamed", AccountType: "User"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	suspended, err = s.InstallationSuspended(ctx, 1)
	if err != nil || !suspended {
		t.Fatalf("upsert reset suspension: %v, %v", suspended, err)
	}
	var login string
	if err := s.pool.QueryRow(ctx, "SELECT account_login FROM installations WHERE id = 1").Scan(&login); err != nil || login != "renamed" {
		t.Fatalf("login = %q, %v", login, err)
	}
	if err := s.UpsertRepositories(ctx, 1, []Repository{{ID: 10, Owner: "octo", Name: "renamed", Private: true}, {ID: 11, Owner: "octo", Name: "two"}}); err != nil {
		t.Fatalf("upsert repos: %v", err)
	}
	var name string
	var private bool
	if err := s.pool.QueryRow(ctx, "SELECT name, private FROM repositories WHERE id = 10").Scan(&name, &private); err != nil || name != "renamed" || !private {
		t.Fatalf("repo = %q %v, %v", name, private, err)
	}
	if err := s.RemoveRepositories(ctx, []int64{11}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM repositories").Scan(&count); err != nil || count != 1 {
		t.Fatalf("repos = %d, %v", count, err)
	}
}

func TestCascadeDelete(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	seedRepo(t, s, 1, 10)
	if err := s.SaveAnalysis(ctx, Analysis{RepositoryID: 10, PRNumber: 1, HeadSHA: "h", BaseSHA: "b", Result: []byte(`{}`), DurationMS: 5}); err != nil {
		t.Fatalf("save analysis: %v", err)
	}
	if err := s.SetCommentID(ctx, 10, 1, 77); err != nil {
		t.Fatalf("set comment: %v", err)
	}
	now := time.Now()
	if err := s.InsertJobRuns(ctx, []JobRun{{JobID: 1, RepositoryID: 10, RunID: 1, WorkflowPath: "p", JobKey: "k", Conclusion: "success", StartedAt: now, CompletedAt: now, DurationSeconds: 1}}); err != nil {
		t.Fatalf("insert runs: %v", err)
	}
	if err := s.RecomputeJobStats(ctx, 10, "p", "k"); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if err := s.DeleteInstallation(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, table := range []string{"repositories", "analyses", "pr_comments", "job_runs", "job_stats"} {
		var n int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Fatalf("%s has %d rows after cascade", table, n)
		}
	}
}

func TestAnalysesAndComments(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	seedRepo(t, s, 1, 10)
	a := Analysis{RepositoryID: 10, PRNumber: 3, HeadSHA: "h", BaseSHA: "b", Result: []byte(`{"files": []}`), FindingCount: 1, DurationMS: 9}
	if err := s.SaveAnalysis(ctx, a); err != nil {
		t.Fatalf("save: %v", err)
	}
	a.FindingCount = 4
	a.CheckRunID = 55
	if err := s.SaveAnalysis(ctx, a); err != nil {
		t.Fatalf("save again: %v", err)
	}
	var count, findings int
	var checkRun *int64
	if err := s.pool.QueryRow(ctx, "SELECT count(*), max(finding_count), max(check_run_id) FROM analyses").Scan(&count, &findings, &checkRun); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 || findings != 4 || checkRun == nil || *checkRun != 55 {
		t.Fatalf("analyses = %d %d %v", count, findings, checkRun)
	}
	_, ok, err := s.CommentID(ctx, 10, 3)
	if err != nil || ok {
		t.Fatalf("comment before set = %v, %v", ok, err)
	}
	for _, id := range []int64{1, 2} {
		if err := s.SetCommentID(ctx, 10, 3, id); err != nil {
			t.Fatalf("set: %v", err)
		}
	}
	id, ok, err := s.CommentID(ctx, 10, 3)
	if err != nil || !ok || id != 2 {
		t.Fatalf("comment = %d %v %v", id, ok, err)
	}
	if err := s.SaveAnalysis(ctx, Analysis{RepositoryID: 999, PRNumber: 1, Result: []byte(`{}`)}); err == nil {
		t.Fatal("save for unknown repository succeeded")
	}
}

func TestRecomputeJobStats(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	seedRepo(t, s, 1, 10)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var runs []JobRun
	for i := 0; i < 35; i++ {
		d := 100
		if i >= 5 {
			d = 10 * (i - 4)
		}
		runs = append(runs, JobRun{
			JobID: int64(i + 1), RepositoryID: 10, RunID: int64(i/2 + 1), WorkflowPath: ".github/workflows/ci.yml", JobKey: "test",
			RunnerLabels: []string{"ubuntu-latest"}, Conclusion: "success",
			StartedAt: base.Add(time.Duration(i) * time.Hour), CompletedAt: base.Add(time.Duration(i)*time.Hour + time.Duration(d)*time.Second), DurationSeconds: d,
		})
	}
	runs = append(runs, JobRun{JobID: 100, RepositoryID: 10, RunID: 100, WorkflowPath: ".github/workflows/ci.yml", JobKey: "test", Conclusion: "failure",
		StartedAt: base.Add(100 * time.Hour), CompletedAt: base.Add(101 * time.Hour), DurationSeconds: 3600})
	if err := s.InsertJobRuns(ctx, runs); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.InsertJobRuns(ctx, runs[:3]); err != nil {
		t.Fatalf("insert duplicate: %v", err)
	}
	if err := s.RecomputeJobStats(ctx, 10, ".github/workflows/ci.yml", "test"); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	var avg, p50, samples int
	if err := s.pool.QueryRow(ctx, "SELECT avg_seconds, p50_seconds, sample_count FROM job_stats").Scan(&avg, &p50, &samples); err != nil {
		t.Fatalf("query: %v", err)
	}
	if avg != 155 || p50 != 155 || samples != 30 {
		t.Fatalf("stats = avg %d p50 %d samples %d", avg, p50, samples)
	}
	src, err := s.Durations(ctx, 10)
	if err != nil {
		t.Fatalf("durations: %v", err)
	}
	d, n, ok := src.JobAverage(".github/workflows/ci.yml", "test")
	if !ok || d != 155*time.Second || n != 30 {
		t.Fatalf("JobAverage = %v %d %v", d, n, ok)
	}
	if _, _, ok := src.JobAverage(".github/workflows/ci.yml", "lint"); ok {
		t.Fatal("unknown job found")
	}
	other, err := s.Durations(ctx, 11)
	if err != nil {
		t.Fatalf("durations: %v", err)
	}
	if _, _, ok := other.JobAverage(".github/workflows/ci.yml", "test"); ok {
		t.Fatal("other repository sees stats")
	}
	mustExec(t, s, "UPDATE job_runs SET conclusion = 'failure'")
	if err := s.RecomputeJobStats(ctx, 10, ".github/workflows/ci.yml", "test"); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	var rows int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM job_stats").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("stats rows = %d, %v", rows, err)
	}
}

func TestOpenInvalidURL(t *testing.T) {
	_, err := Open(context.Background(), "postgres://user:hunter2@[::1")
	if err == nil {
		t.Fatal("expected error")
	}
	if contains := errors.Unwrap(err); contains != nil {
		t.Fatalf("error wraps %v", contains)
	}
	if got := err.Error(); got != "store: invalid database URL" {
		t.Fatalf("error = %q", got)
	}
}

func TestPruneJobRuns(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	seedRepo(t, s, 1, 10)
	now := time.Now().UTC()
	ages := []time.Duration{91 * 24 * time.Hour, 89 * 24 * time.Hour, time.Hour}
	var runs []JobRun
	for i, age := range ages {
		done := now.Add(-age)
		runs = append(runs, JobRun{JobID: int64(i + 1), RepositoryID: 10, RunID: 1, WorkflowPath: "p", JobKey: "k", Conclusion: "success", StartedAt: done.Add(-time.Minute), CompletedAt: done, DurationSeconds: 60})
	}
	if err := s.InsertJobRuns(ctx, runs); err != nil {
		t.Fatalf("insert: %v", err)
	}
	n, err := s.PruneJobRuns(ctx, 90*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	var ids []int64
	rows, err := s.pool.Query(ctx, "SELECT job_id FROM job_runs ORDER BY job_id")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(ids) != 2 || ids[0] != 2 || ids[1] != 3 {
		t.Fatalf("remaining = %v", ids)
	}
	var indexed bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'job_runs_completed_at' AND schemaname = current_schema())").Scan(&indexed); err != nil || !indexed {
		t.Fatalf("completed_at index = %v, %v", indexed, err)
	}
}
