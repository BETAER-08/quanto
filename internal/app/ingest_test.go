package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/BETAER-08/quanto/internal/github"
)

func runJob(id int64, name, conclusion, started, completed string) map[string]any {
	j := map[string]any{"id": id, "name": name, "conclusion": conclusion, "labels": []string{"ubuntu-latest"}}
	if started != "" {
		j["started_at"] = started
	}
	if completed != "" {
		j["completed_at"] = completed
	} else {
		j["completed_at"] = nil
	}
	return j
}

func TestIngestFiltersAndRecomputes(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	h.gh.jobs = []map[string]any{
		runJob(1, "test (ubuntu-latest, 18)", "success", "2026-01-01T00:00:00Z", "2026-01-01T00:01:40Z"),
		runJob(2, "test (ubuntu-latest, 20)", "success", "2026-01-01T00:00:00Z", "2026-01-01T00:03:20Z"),
		runJob(3, "lint", "failure", "2026-01-01T00:00:00Z", "2026-01-01T00:00:50Z"),
		runJob(4, "build", "cancelled", "2026-01-01T00:00:00Z", "2026-01-01T00:00:50Z"),
		runJob(5, "deploy", "success", "2026-01-01T00:00:00Z", ""),
		runJob(6, "call / inner", "success", "2026-01-01T00:00:00Z", "2026-01-01T00:00:50Z"),
		runJob(7, "docs", "skipped", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"),
		runJob(8, "late", "success", "2026-01-01T00:01:00Z", "2026-01-01T00:00:00Z"),
	}
	h.enqueue(KindIngestWorkflowRun, IngestPayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo, RunID: 555, WorkflowPath: ciPath})
	h.runOne()
	expectEndpoints(t, h, []string{"GET /repos/octo/demo/actions/runs/555/jobs"})
	if q := h.gh.requests()[0].Query; q != "filter=latest&per_page=100" {
		t.Fatalf("query = %q", q)
	}
	expectJob(t, h, "done", 1)
	if n := h.count("SELECT count(*) FROM job_runs WHERE run_id = 555 AND workflow_path = $1", ciPath); n != 3 {
		t.Fatalf("job_runs = %d", n)
	}
	if n := h.count("SELECT count(*) FROM job_runs WHERE job_id = 1 AND job_key = 'test' AND duration_seconds = 100 AND runner_labels = ARRAY['ubuntu-latest']"); n != 1 {
		t.Fatal("job 1 not stored as expected")
	}
	src, err := h.store.Durations(context.Background(), 42)
	if err != nil {
		t.Fatalf("durations: %v", err)
	}
	if d, n, ok := src.JobAverage(ciPath, "test"); !ok || d.Seconds() != 150 || n != 2 {
		t.Fatalf("test average = %v %d %v", d, n, ok)
	}
	if _, _, ok := src.JobAverage(ciPath, "lint"); ok {
		t.Fatal("failure-only job has stats")
	}
}

func TestIngestIgnoresNonWorkflowPath(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	h.enqueue(KindIngestWorkflowRun, IngestPayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo, RunID: 1, WorkflowPath: "dynamic/github-code-scanning/codeql"})
	h.runOne()
	expectJob(t, h, "done", 1)
	if len(h.gh.log) != 0 {
		t.Fatalf("github called: %v", h.gh.log)
	}
}

func TestBackfillEnqueuesRuns(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	h.gh.runs = []github.WorkflowRun{
		{ID: 1, Path: ciPath, Status: "completed"},
		{ID: 2, Path: ".github/workflows/x.yml@refs/heads/main", Status: "completed"},
	}
	if _, err := h.store.Enqueue(context.Background(), KindIngestWorkflowRun, IngestPayload{RunID: 1}, "run:1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h.enqueue(KindBackfillRepository, BackfillPayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo})
	if _, err := h.db.Exec(context.Background(), "UPDATE queue_jobs SET run_after = now() + interval '1 hour' WHERE kind = $1", KindIngestWorkflowRun); err != nil {
		t.Fatalf("delay seed: %v", err)
	}
	h.runOne()
	expectEndpoints(t, h, []string{"GET /repos/octo/demo/actions/runs"})
	if q := h.gh.requests()[0].Query; q != "per_page=100&status=completed" {
		t.Fatalf("query = %q", q)
	}
	q := h.queue()
	if len(q) != 3 || q[1].Status != "done" || q[2].DedupeKey != "run:2" {
		t.Fatalf("queue = %+v", q)
	}
	var p IngestPayload
	if err := json.Unmarshal([]byte(q[2].Payload), &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	want := IngestPayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo, RunID: 2, WorkflowPath: ".github/workflows/x.yml"}
	if p != want {
		t.Fatalf("payload = %+v", p)
	}
}
