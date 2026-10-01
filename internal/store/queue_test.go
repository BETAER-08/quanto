package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEnqueueDedupe(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ok, err := s.Enqueue(ctx, "analyze_pr", map[string]int{"n": 1}, "pr:1:2:abc")
	if err != nil || !ok {
		t.Fatalf("first enqueue = %v, %v", ok, err)
	}
	ok, err = s.Enqueue(ctx, "analyze_pr", map[string]int{"n": 2}, "pr:1:2:abc")
	if err != nil || ok {
		t.Fatalf("duplicate enqueue = %v, %v", ok, err)
	}
	for i := 0; i < 2; i++ {
		ok, err = s.Enqueue(ctx, "backfill_repo", map[string]int{"n": i}, "")
		if err != nil || !ok {
			t.Fatalf("enqueue without key = %v, %v", ok, err)
		}
	}
	job, err := s.Dequeue(ctx)
	if err != nil || job == nil {
		t.Fatalf("dequeue = %v, %v", job, err)
	}
	if job.Kind != "analyze_pr" || job.Attempts != 1 || string(job.Payload) != `{"n": 1}` {
		t.Fatalf("dequeued %+v payload %s", job, job.Payload)
	}
	ok, err = s.Enqueue(ctx, "analyze_pr", nil, "pr:1:2:abc")
	if err != nil || ok {
		t.Fatalf("enqueue while running = %v, %v", ok, err)
	}
	if err := s.Complete(ctx, job.ID, job.Attempts); err != nil {
		t.Fatalf("complete: %v", err)
	}
	ok, err = s.Enqueue(ctx, "analyze_pr", nil, "pr:1:2:abc")
	if err != nil || !ok {
		t.Fatalf("enqueue after done = %v, %v", ok, err)
	}
	n, err := s.PendingCount(ctx)
	if err != nil || n != 3 {
		t.Fatalf("pending = %d, %v", n, err)
	}
}

func TestDequeueEmptyAndFuture(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	job, err := s.Dequeue(ctx)
	if err != nil || job != nil {
		t.Fatalf("empty dequeue = %v, %v", job, err)
	}
	if _, err := s.Enqueue(ctx, "k", 1, ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	mustExec(t, s, "UPDATE queue_jobs SET run_after = now() + interval '1 hour'")
	job, err = s.Dequeue(ctx)
	if err != nil || job != nil {
		t.Fatalf("future dequeue = %v, %v", job, err)
	}
}

func TestDequeueOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.Enqueue(ctx, "k", i, ""); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	mustExec(t, s, "UPDATE queue_jobs SET run_after = now() - interval '1 minute' WHERE payload = '2'::jsonb")
	var got []string
	for i := 0; i < 3; i++ {
		job, err := s.Dequeue(ctx)
		if err != nil || job == nil {
			t.Fatalf("dequeue: %v, %v", job, err)
		}
		got = append(got, string(job.Payload))
	}
	if strings.Join(got, ",") != "2,0,1" {
		t.Fatalf("order = %v", got)
	}
}

func TestDequeueConcurrentExactlyOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const jobs = 100
	const workers = 8
	for i := 0; i < jobs; i++ {
		if _, err := s.Enqueue(ctx, "k", i, ""); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				job, err := s.Dequeue(ctx)
				if err != nil {
					errs <- err
					return
				}
				if job == nil {
					return
				}
				mu.Lock()
				seen[job.ID]++
				mu.Unlock()
				if err := s.Complete(ctx, job.ID, job.Attempts); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("worker: %v", err)
	}
	if len(seen) != jobs {
		t.Fatalf("dequeued %d distinct jobs, want %d", len(seen), jobs)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("job %d dequeued %d times", id, n)
		}
	}
	var done int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM queue_jobs WHERE status = 'done' AND attempts = 1").Scan(&done); err != nil {
		t.Fatalf("count: %v", err)
	}
	if done != jobs {
		t.Fatalf("done jobs = %d", done)
	}
}

func TestFailBackoffAndDead(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Enqueue(ctx, "k", 1, "key"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	wantDelay := []float64{30, 60, 120, 240}
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		mustExec(t, s, "UPDATE queue_jobs SET run_after = now()")
		job, err := s.Dequeue(ctx)
		if err != nil || job == nil {
			t.Fatalf("dequeue attempt %d: %v, %v", attempt, job, err)
		}
		if job.Attempts != attempt {
			t.Fatalf("attempts = %d, want %d", job.Attempts, attempt)
		}
		if err := s.Fail(ctx, job.ID, job.Attempts, errors.New(strings.Repeat("é", 600))); err != nil {
			t.Fatalf("fail: %v", err)
		}
		st := readJob(t, s, job.ID)
		if len([]rune(st.lastError)) != 500 {
			t.Fatalf("last_error runes = %d", len([]rune(st.lastError)))
		}
		if attempt < MaxAttempts {
			if st.status != "pending" {
				t.Fatalf("attempt %d status = %s", attempt, st.status)
			}
			if st.delay < wantDelay[attempt-1]-5 || st.delay > wantDelay[attempt-1]+1 {
				t.Fatalf("attempt %d delay = %.1f, want %.0f", attempt, st.delay, wantDelay[attempt-1])
			}
		} else if st.status != "dead" {
			t.Fatalf("final status = %s", st.status)
		}
	}
	ok, err := s.Enqueue(ctx, "k", 1, "key")
	if err != nil || !ok {
		t.Fatalf("enqueue after dead = %v, %v", ok, err)
	}
}

func TestFailBackoffCap(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Enqueue(ctx, "k", 1, ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := s.Dequeue(ctx)
	if err != nil || job == nil {
		t.Fatalf("dequeue: %v, %v", job, err)
	}
	mustExec(t, s, "UPDATE queue_jobs SET attempts = 4, status = 'running'")
	if err := s.Fail(ctx, job.ID, 4, errors.New("boom")); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if st := readJob(t, s, job.ID); st.delay < 235 || st.delay > 241 {
		t.Fatalf("delay = %.1f", st.delay)
	}
	mustExec(t, s, "UPDATE queue_jobs SET attempts = 3, status = 'running'")
	if err := s.Fail(ctx, job.ID, 3, errors.New("boom")); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if st := readJob(t, s, job.ID); st.delay < 115 || st.delay > 121 {
		t.Fatalf("delay = %.1f", st.delay)
	}
	mustExec(t, s, "UPDATE queue_jobs SET attempts = 0, status = 'running'")
	if err := s.Fail(ctx, job.ID, 0, nil); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if st := readJob(t, s, job.ID); st.delay < 25 || st.delay > 31 || st.lastError != "" {
		t.Fatalf("state = %+v", st)
	}
}

func TestKill(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Enqueue(ctx, "k", 1, "kill"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := s.Dequeue(ctx)
	if err != nil || job == nil {
		t.Fatalf("dequeue: %v, %v", job, err)
	}
	if err := s.Kill(ctx, job.ID, job.Attempts, errors.New("decode payload: bad json")); err != nil {
		t.Fatalf("kill: %v", err)
	}
	st := readJob(t, s, job.ID)
	if st.status != "dead" || st.attempts != 1 || st.lastError != "decode payload: bad json" {
		t.Fatalf("state = %+v", st)
	}
	if err := s.Kill(ctx, 999999, 1, errors.New("x")); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("kill missing = %v", err)
	}
}

func TestDeferRestoresAttempts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Enqueue(ctx, "k", 1, ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := s.Dequeue(ctx)
	if err != nil || job == nil {
		t.Fatalf("dequeue: %v, %v", job, err)
	}
	if err := s.Defer(ctx, job.ID, job.Attempts, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("defer: %v", err)
	}
	st := readJob(t, s, job.ID)
	if st.status != "pending" || st.attempts != 0 || st.delay < 3590 || st.delay > 3601 {
		t.Fatalf("state = %+v", st)
	}
	again, err := s.Dequeue(ctx)
	if err != nil || again != nil {
		t.Fatalf("dequeue before until = %v, %v", again, err)
	}
	if err := s.Complete(ctx, 424242, 1); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("complete missing = %v", err)
	}
}

func TestReapStale(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := s.Enqueue(ctx, "k", i, ""); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	a, err := s.Dequeue(ctx)
	if err != nil || a == nil {
		t.Fatalf("dequeue: %v, %v", a, err)
	}
	b, err := s.Dequeue(ctx)
	if err != nil || b == nil {
		t.Fatalf("dequeue: %v, %v", b, err)
	}
	mustExec(t, s, "UPDATE queue_jobs SET locked_at = now() - interval '11 minutes' WHERE id = $1", a.ID)
	n, err := s.ReapStale(ctx, 10*time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("reap = %d, %v", n, err)
	}
	if st := readJob(t, s, a.ID); st.status != "pending" {
		t.Fatalf("reaped status = %s", st.status)
	}
	if st := readJob(t, s, b.ID); st.status != "running" {
		t.Fatalf("fresh status = %s", st.status)
	}
}

func TestReapedJobFencesFormerOwner(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Enqueue(ctx, "k", 1, "fence"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	old, err := s.Dequeue(ctx)
	if err != nil || old == nil {
		t.Fatalf("dequeue: %v, %v", old, err)
	}
	mustExec(t, s, "UPDATE queue_jobs SET locked_at = now() - interval '11 minutes'")
	if n, err := s.ReapStale(ctx, 10*time.Minute); err != nil || n != 1 {
		t.Fatalf("reap = %d, %v", n, err)
	}
	current, err := s.Dequeue(ctx)
	if err != nil || current == nil || current.ID != old.ID || current.Attempts != old.Attempts+1 {
		t.Fatalf("redequeue = %+v, %v", current, err)
	}
	stale := []struct {
		name string
		call func() error
	}{
		{"complete", func() error { return s.Complete(ctx, old.ID, old.Attempts) }},
		{"fail", func() error { return s.Fail(ctx, old.ID, old.Attempts, errors.New("late")) }},
		{"defer", func() error { return s.Defer(ctx, old.ID, old.Attempts, time.Now().Add(time.Hour)) }},
		{"kill", func() error { return s.Kill(ctx, old.ID, old.Attempts, errors.New("late")) }},
	}
	for _, c := range stale {
		if err := c.call(); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("%s by former owner = %v, want ErrLeaseLost", c.name, err)
		}
		st := readJob(t, s, old.ID)
		if st.status != "running" || st.attempts != current.Attempts || st.lastError != "" {
			t.Fatalf("%s by former owner changed state: %+v", c.name, st)
		}
	}
	ok, err := s.Enqueue(ctx, "k", 2, "fence")
	if err != nil || ok {
		t.Fatalf("dedupe while running = %v, %v", ok, err)
	}
	if err := s.Complete(ctx, current.ID, current.Attempts); err != nil {
		t.Fatalf("complete by current owner: %v", err)
	}
	if st := readJob(t, s, old.ID); st.status != "done" {
		t.Fatalf("status = %s", st.status)
	}
	if err := s.Complete(ctx, current.ID, current.Attempts); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("second complete = %v", err)
	}
}

func TestReapStaleKillsAfterMaxAttempts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Enqueue(ctx, "k", 1, "poison"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var id int64
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		job, err := s.Dequeue(ctx)
		if err != nil || job == nil || job.Attempts != attempt {
			t.Fatalf("dequeue attempt %d = %+v, %v", attempt, job, err)
		}
		id = job.ID
		mustExec(t, s, "UPDATE queue_jobs SET locked_at = now() - interval '11 minutes'")
		if n, err := s.ReapStale(ctx, 10*time.Minute); err != nil || n != 1 {
			t.Fatalf("reap %d = %d, %v", attempt, n, err)
		}
		st := readJob(t, s, id)
		if attempt < MaxAttempts && st.status != "pending" {
			t.Fatalf("attempt %d status = %s", attempt, st.status)
		}
		if attempt == MaxAttempts && (st.status != "dead" || st.lastError != "job lease expired") {
			t.Fatalf("final state = %+v", st)
		}
	}
	job, err := s.Dequeue(ctx)
	if err != nil || job != nil {
		t.Fatalf("dead job dequeued: %+v, %v", job, err)
	}
	ok, err := s.Enqueue(ctx, "k", 1, "poison")
	if err != nil || !ok {
		t.Fatalf("enqueue after dead = %v, %v", ok, err)
	}
}

func TestPruneQueue(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	rows := []struct {
		status string
		age    string
		kept   bool
	}{
		{"done", "8 days", false},
		{"done", "6 days", true},
		{"dead", "31 days", false},
		{"dead", "8 days", true},
		{"pending", "60 days", true},
		{"running", "60 days", true},
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		if err := s.pool.QueryRow(ctx, `INSERT INTO queue_jobs (kind, payload, status, updated_at)
VALUES ('k', '{}'::jsonb, $1, now() - $2::interval) RETURNING id`, r.status, r.age).Scan(&ids[i]); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	n, err := s.PruneQueue(ctx, 7*24*time.Hour, 30*24*time.Hour)
	if err != nil || n != 2 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	for i, r := range rows {
		var exists bool
		if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM queue_jobs WHERE id = $1)", ids[i]).Scan(&exists); err != nil {
			t.Fatalf("exists: %v", err)
		}
		if exists != r.kept {
			t.Fatalf("row %s %s kept = %v, want %v", r.status, r.age, exists, r.kept)
		}
	}
}
