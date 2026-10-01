package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/BETAER-08/quanto/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRunWorkerProcessesAndStops(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	for i := int64(1); i <= 5; i++ {
		h.enqueue(KindIngestWorkflowRun, IngestPayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo, RunID: i, WorkflowPath: "other/path.yml"})
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- h.app.RunWorker(ctx)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for h.count("SELECT count(*) FROM queue_jobs WHERE status = 'done'") != 5 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("jobs not processed: %+v", h.queue())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWorker: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunWorker did not stop")
	}
	if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(KindIngestWorkflowRun, metrics.ResultDone)); got != 5 {
		t.Fatalf("done metric = %v", got)
	}
}

func TestPanicIsRecordedAsFailure(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.GitHub = nil })
	h.enqueue(KindBackfillRepository, BackfillPayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo})
	h.runOne()
	job := expectJob(t, h, "pending", 1)
	if !strings.HasPrefix(job.LastError, "panic in backfill_repo handler") {
		t.Fatalf("last_error = %q", job.LastError)
	}
}

func TestFailureAfterMaxAttemptsIsDead(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	h.gh.broken[filesEndpoint] = true
	h.enqueue(KindAnalyzePR, analyzePayload())
	if _, err := h.db.Exec(context.Background(), "UPDATE queue_jobs SET attempts = 4"); err != nil {
		t.Fatalf("seed attempts: %v", err)
	}
	h.runOne()
	expectJob(t, h, "dead", 5)
	if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(KindAnalyzePR, metrics.ResultDead)); got != 1 {
		t.Fatalf("dead metric = %v", got)
	}
}

func TestShutdownDefersInFlightJob(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	h.enqueue(KindAnalyzePR, analyzePayload())
	job, err := h.store.Dequeue(context.Background())
	if err != nil || job == nil {
		t.Fatalf("dequeue: %v, %v", job, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.app.process(ctx, job)
	expectJob(t, h, "pending", 0)
	if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(KindAnalyzePR, metrics.ResultDeferred)); got != 1 {
		t.Fatalf("deferred metric = %v", got)
	}
}

func TestHandlerTimeoutFails(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	h.app.handlerTimeout = 200 * time.Millisecond
	h.gh.slow[filesEndpoint] = true
	h.enqueue(KindAnalyzePR, analyzePayload())
	started := time.Now()
	h.runOne()
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("handler ran %v despite timeout", elapsed)
	}
	job := expectJob(t, h, "pending", 1)
	if !strings.Contains(job.LastError, "deadline exceeded") {
		t.Fatalf("last_error = %q", job.LastError)
	}
	if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(KindAnalyzePR, metrics.ResultFailed)); got != 1 {
		t.Fatalf("failed metric = %v", got)
	}
}

func TestDefaultHandlerTimeout(t *testing.T) {
	h := newHarness(t, nil)
	if h.app.handlerTimeout != 5*time.Minute {
		t.Fatalf("handler timeout = %v", h.app.handlerTimeout)
	}
}

type leaseCase struct {
	name   string
	kind   string
	setup  func(h *harness) any
	result string
	status string
	hit    string
}

func leaseCases() []leaseCase {
	return []leaseCase{
		{"complete", KindIngestWorkflowRun, func(h *harness) any {
			return IngestPayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo, RunID: 1, WorkflowPath: "other/path.yml"}
		}, metrics.ResultDone, "done", ""},
		{"fail", KindAnalyzePR, func(h *harness) any {
			setupModified(h)
			h.gh.broken[filesEndpoint] = true
			return analyzePayload()
		}, metrics.ResultFailed, "pending", filesEndpoint},
		{"defer", KindAnalyzePR, func(h *harness) any {
			setupModified(h)
			h.gh.limited[filesEndpoint] = true
			return analyzePayload()
		}, metrics.ResultDeferred, "pending", filesEndpoint},
		{"kill", "mystery", func(h *harness) any {
			return map[string]any{}
		}, metrics.ResultDead, "dead", ""},
	}
}

func (h *harness) hits(endpoint string) int {
	n := 0
	for _, e := range h.gh.endpoints() {
		if e == endpoint {
			n++
		}
	}
	return n
}

func TestLeaseCasesTakeIntendedPath(t *testing.T) {
	for _, tc := range leaseCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.seedRepo()
			h.enqueue(tc.kind, tc.setup(h))
			h.runOne()
			if got := h.queue()[0].Status; got != tc.status {
				t.Fatalf("status = %s, want %s", got, tc.status)
			}
			if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(tc.kind, tc.result)); got != 1 {
				t.Fatalf("%s metric = %v", tc.result, got)
			}
			if tc.hit != "" && h.hits(tc.hit) == 0 {
				t.Fatalf("handler did not call %s", tc.hit)
			}
		})
	}
}

func TestLostLeaseDiscardsResult(t *testing.T) {
	for _, tc := range leaseCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.seedRepo()
			h.enqueue(tc.kind, tc.setup(h))
			job, err := h.store.Dequeue(context.Background())
			if err != nil || job == nil {
				t.Fatalf("dequeue: %v, %v", job, err)
			}
			if _, err := h.db.Exec(context.Background(), "UPDATE queue_jobs SET attempts = attempts + 1, locked_at = now(), run_after = now() - interval '1 minute'"); err != nil {
				t.Fatalf("simulate takeover: %v", err)
			}
			before := expectJob(t, h, "running", 2)
			h.app.process(context.Background(), job)
			if tc.hit != "" && h.hits(tc.hit) == 0 {
				t.Fatalf("handler did not call %s", tc.hit)
			}
			after := expectJob(t, h, "running", 2)
			if after.LastError != "" || after.Delay > before.Delay+1 {
				t.Fatalf("former owner changed the job: before %+v after %+v", before, after)
			}
			var locked bool
			if err := h.db.QueryRow(context.Background(), "SELECT locked_at IS NOT NULL FROM queue_jobs").Scan(&locked); err != nil || !locked {
				t.Fatalf("lease cleared: %v, %v", locked, err)
			}
			for _, result := range []string{metrics.ResultDone, metrics.ResultFailed, metrics.ResultDeferred, metrics.ResultDead} {
				if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(tc.kind, result)); got != 0 {
					t.Fatalf("%s metric = %v", result, got)
				}
			}
		})
	}
}

func TestLostLeaseAtMaxAttemptsDoesNotKill(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	h.gh.broken[filesEndpoint] = true
	h.enqueue(KindAnalyzePR, analyzePayload())
	if _, err := h.db.Exec(context.Background(), "UPDATE queue_jobs SET attempts = 4"); err != nil {
		t.Fatalf("seed attempts: %v", err)
	}
	job, err := h.store.Dequeue(context.Background())
	if err != nil || job == nil || job.Attempts != 5 {
		t.Fatalf("dequeue: %+v, %v", job, err)
	}
	if _, err := h.db.Exec(context.Background(), "UPDATE queue_jobs SET attempts = attempts + 1, locked_at = now()"); err != nil {
		t.Fatalf("simulate takeover: %v", err)
	}
	h.app.process(context.Background(), job)
	if job := expectJob(t, h, "running", 6); job.LastError != "" {
		t.Fatalf("last_error = %q", job.LastError)
	}
	if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(KindAnalyzePR, metrics.ResultDead)); got != 0 {
		t.Fatalf("dead metric = %v", got)
	}
}
