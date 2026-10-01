package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/BETAER-08/quanto/core/report"
	"github.com/BETAER-08/quanto/internal/github"
	"github.com/BETAER-08/quanto/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	fixtureDir = "../../testdata/golden/semdiff/"
	ciPath     = ".github/workflows/ci.yml"
)

func TestPlanFiles(t *testing.T) {
	files := []github.PullRequestFile{
		{Filename: "README.md", Status: "modified"},
		{Filename: ".github/workflows/z.yml", Status: "modified"},
		{Filename: ".github/workflows/new.yml", Status: "added"},
		{Filename: ".github/workflows/old.yml", Status: "removed"},
		{Filename: ".github/workflows/b.yml", PreviousFilename: ".github/workflows/a.yml", Status: "renamed"},
		{Filename: ".github/workflows/in.yml", PreviousFilename: "ci/in.yml", Status: "renamed"},
		{Filename: "ci/out.yml", PreviousFilename: ".github/workflows/out.yml", Status: "renamed"},
		{Filename: ".github/workflows/copy.yaml", PreviousFilename: ".github/workflows/z.yml", Status: "copied"},
		{Filename: ".github/workflows/nested/x.yml", Status: "added"},
	}
	want := []plannedFile{
		{path: ".github/workflows/b.yml", oldPath: ".github/workflows/a.yml", beforePath: ".github/workflows/a.yml", afterPath: ".github/workflows/b.yml"},
		{path: ".github/workflows/copy.yaml", afterPath: ".github/workflows/copy.yaml"},
		{path: ".github/workflows/in.yml", afterPath: ".github/workflows/in.yml"},
		{path: ".github/workflows/new.yml", afterPath: ".github/workflows/new.yml"},
		{path: ".github/workflows/old.yml", beforePath: ".github/workflows/old.yml"},
		{path: ".github/workflows/out.yml", beforePath: ".github/workflows/out.yml"},
		{path: ".github/workflows/z.yml", beforePath: ".github/workflows/z.yml", afterPath: ".github/workflows/z.yml"},
	}
	got := planFiles(files)
	if len(got) != len(want) {
		t.Fatalf("planFiles = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("planFiles[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestNoChangeBody(t *testing.T) {
	want := "<!-- quanto:summary -->\n## quanto\n\nNo workflow execution changes as of commit `1111111`.\n"
	if got := report.NoChanges(testHead); got != want {
		t.Fatalf("NoChanges = %q", got)
	}
}

func setupModified(h *harness) {
	before := readFixture(h.t, fixtureDir+"matrix-axis-added/before.yml")
	after := readFixture(h.t, fixtureDir+"matrix-axis-added/after.yml")
	h.gh.files = []github.PullRequestFile{{Filename: ciPath, Status: "modified"}}
	h.gh.setContent(testBase, ciPath, before)
	h.gh.setContent(testHead, ciPath, after)
}

func setupIdentical(h *harness) {
	same := readFixture(h.t, fixtureDir+"identical/before.yml")
	h.gh.files = []github.PullRequestFile{{Filename: ciPath, Status: "modified"}}
	h.gh.setContent(testBase, ciPath, same)
	h.gh.setContent(testHead, ciPath, same)
}

func expectEndpoints(t *testing.T, h *harness, want []string) {
	t.Helper()
	if got := h.gh.endpoints(); !equalStrings(got, want) {
		t.Fatalf("endpoints:\n got  %q\n want %q", got, want)
	}
}

func expectJob(t *testing.T, h *harness, status string, attempts int) queueRow {
	t.Helper()
	q := h.queue()
	if len(q) == 0 {
		t.Fatal("queue is empty")
	}
	last := q[len(q)-1]
	if last.Status != status || last.Attempts != attempts {
		t.Fatalf("job = %+v, want status %s attempts %d", last, status, attempts)
	}
	return last
}

const (
	filesEndpoint     = "GET /repos/octo/demo/pulls/3/files"
	prEndpoint        = "GET /repos/octo/demo/pulls/3"
	checkEndpoint     = "POST /repos/octo/demo/check-runs"
	findCheckEndpoint = "GET /repos/octo/demo/commits/" + testHead + "/check-runs"
	appEndpoint       = "GET /app"
	listComments      = "GET /repos/octo/demo/issues/3/comments"
	createComment     = "POST /repos/octo/demo/issues/3/comments"
	contentsEndpoint  = "GET /repos/octo/demo/contents/"
)

func updateComment(id int64) string {
	return fmt.Sprintf("PATCH /repos/octo/demo/issues/comments/%d", id)
}

func TestAnalyzeNoWorkflowChanges(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	h.gh.files = []github.PullRequestFile{{Filename: "README.md", Status: "modified"}, {Filename: ".github/dependabot.yml", Status: "modified"}}
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint})
	expectJob(t, h, "done", 1)
	if n := h.count("SELECT count(*) FROM analyses"); n != 0 {
		t.Fatalf("analyses = %d", n)
	}
}

func TestAnalyzeFileStatuses(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	before := readFixture(t, fixtureDir+"matrix-axis-added/before.yml")
	after := readFixture(t, fixtureDir+"matrix-axis-added/after.yml")
	h.gh.files = []github.PullRequestFile{
		{Filename: "README.md", Status: "modified"},
		{Filename: ciPath, Status: "modified"},
		{Filename: ".github/workflows/new.yml", Status: "added"},
		{Filename: ".github/workflows/old.yml", Status: "removed"},
		{Filename: ".github/workflows/b.yml", PreviousFilename: ".github/workflows/a.yml", Status: "renamed"},
	}
	h.gh.setContent(testBase, ciPath, before)
	h.gh.setContent(testHead, ciPath, after)
	h.gh.setContent(testHead, ".github/workflows/new.yml", before)
	h.gh.setContent(testBase, ".github/workflows/old.yml", before)
	h.gh.setContent(testBase, ".github/workflows/a.yml", before)
	h.gh.setContent(testHead, ".github/workflows/b.yml", before)
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{
		filesEndpoint,
		contentsEndpoint + ".github/workflows/a.yml",
		contentsEndpoint + ".github/workflows/b.yml",
		contentsEndpoint + ciPath,
		contentsEndpoint + ciPath,
		contentsEndpoint + ".github/workflows/new.yml",
		contentsEndpoint + ".github/workflows/old.yml",
		findCheckEndpoint,
		checkEndpoint,
		prEndpoint,
		appEndpoint,
		listComments,
		createComment,
	})
	refs := []string{testBase, testHead, testBase, testHead, testHead, testBase}
	for i, r := range h.gh.find("GET", ".yml") {
		if r.Query != "ref="+refs[i] {
			t.Errorf("content request %d %s query = %q, want ref %s", i, r.Path, r.Query, refs[i])
		}
	}
	expectJob(t, h, "done", 1)
	checks := h.gh.find("POST", "/check-runs")
	var check map[string]any
	if err := json.Unmarshal([]byte(checks[0].Body), &check); err != nil {
		t.Fatalf("decode check run: %v", err)
	}
	output, ok := check["output"].(map[string]any)
	if !ok {
		t.Fatalf("check run output = %v", check["output"])
	}
	if check["name"] != "quanto" || check["head_sha"] != testHead || check["conclusion"] != "neutral" || check["status"] != "completed" {
		t.Fatalf("check run = %v", check)
	}
	if output["title"] != "5 execution changes" {
		t.Fatalf("title = %v", output["title"])
	}
	summary, ok := output["summary"].(string)
	if !ok {
		t.Fatalf("summary = %v", output["summary"])
	}
	for _, want := range []string{"Workflow added", "Workflow removed", "Workflow renamed from `.github/workflows/a.yml`", "Job `test` matrix: 6 → 24 jobs"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q", want)
		}
	}
	comments := h.gh.find("POST", "/issues/3/comments")
	var comment struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(comments[0].Body), &comment); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if !strings.HasPrefix(comment.Body, "<!-- quanto:summary -->\n## quanto\n\nExecution changes in 3 workflow files.") {
		t.Fatalf("comment = %q", comment.Body)
	}
	id, ok, err := h.store.CommentID(context.Background(), 42, 3)
	if err != nil || !ok || id != 5001 {
		t.Fatalf("cached comment = %d %v %v", id, ok, err)
	}
	var result string
	var findings int
	var checkRun int64
	if err := h.db.QueryRow(context.Background(), "SELECT result::text, finding_count, check_run_id FROM analyses WHERE repository_id = 42 AND pr_number = 3 AND head_sha = $1 AND base_sha = $2", testHead, testBase).Scan(&result, &findings, &checkRun); err != nil {
		t.Fatalf("analysis: %v", err)
	}
	if findings != 5 || checkRun != 900 || !strings.Contains(result, `"quanto.diff/v1"`) {
		t.Fatalf("analysis = %d %d %s", findings, checkRun, result)
	}
	for _, raw := range []string{"npm test", "make lint", "setup-node@v4", "runs-on"} {
		if strings.Contains(result, raw) {
			t.Errorf("analysis result stores workflow source text %q", raw)
		}
	}
	if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(KindAnalyzePR, metrics.ResultDone)); got != 1 {
		t.Fatalf("done metric = %v", got)
	}
	if got := testutil.ToFloat64(h.metrics.Findings.WithLabelValues("normal")) + testutil.ToFloat64(h.metrics.Findings.WithLabelValues("low")) + testutil.ToFloat64(h.metrics.Findings.WithLabelValues("high")); got != 5 {
		t.Fatalf("findings metric = %v", got)
	}
	if got := testutil.ToFloat64(h.metrics.GitHubRequests.WithLabelValues("2xx")); got < 12 {
		t.Fatalf("github 2xx metric = %v", got)
	}
}

func TestAnalyzeHeadMovedSkipsComment(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	h.gh.headSHA = "9999999999999999999999999999999999999999"
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint, contentsEndpoint + ciPath, contentsEndpoint + ciPath, findCheckEndpoint, checkEndpoint, prEndpoint})
	expectJob(t, h, "done", 1)
	if n := h.count("SELECT count(*) FROM analyses"); n != 1 {
		t.Fatalf("analyses = %d", n)
	}
	if n := h.count("SELECT count(*) FROM pr_comments"); n != 0 {
		t.Fatalf("pr_comments = %d", n)
	}
}

func TestAnalyzeUpdatesBotCommentOnly(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	human := report0("human copy")
	h.gh.comments = []github.IssueComment{
		{ID: 10, Body: human, User: github.User{Login: "alice"}},
		{ID: 11, Body: "plain bot note", User: github.User{Login: botLogin}},
		{ID: 12, Body: report0("old"), User: github.User{Login: botLogin}},
	}
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint, contentsEndpoint + ciPath, contentsEndpoint + ciPath, findCheckEndpoint, checkEndpoint, prEndpoint, appEndpoint, listComments, updateComment(12)})
	if h.gh.comments[0].Body != human {
		t.Fatal("human comment modified")
	}
	if !strings.Contains(h.gh.comments[2].Body, "Job `test` matrix: 6 → 24 jobs") {
		t.Fatalf("bot comment = %q", h.gh.comments[2].Body)
	}
	id, ok, err := h.store.CommentID(context.Background(), 42, 3)
	if err != nil || !ok || id != 12 {
		t.Fatalf("cached = %d %v %v", id, ok, err)
	}
	h.gh.resetLog()
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint, contentsEndpoint + ciPath, contentsEndpoint + ciPath, findCheckEndpoint, "GET /repos/octo/demo/check-runs/900", "PATCH /repos/octo/demo/check-runs/900", prEndpoint, updateComment(12)})
}

func report0(text string) string {
	return "<!-- quanto:summary -->\n" + text
}

func TestAnalyzeIgnoresHumanCommentWithMarker(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	human := report0("pasted by a person")
	h.gh.comments = []github.IssueComment{{ID: 10, Body: human, User: github.User{Login: "alice"}}}
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint, contentsEndpoint + ciPath, contentsEndpoint + ciPath, findCheckEndpoint, checkEndpoint, prEndpoint, appEndpoint, listComments, createComment})
	if h.gh.comments[0].Body != human || len(h.gh.comments) != 2 {
		t.Fatalf("comments = %+v", h.gh.comments)
	}
}

func TestAnalyzeNoChangeUpdatesExistingComment(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupIdentical(h)
	h.gh.comments = []github.IssueComment{{ID: 12, Body: report0("old findings"), User: github.User{Login: botLogin}}}
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint, contentsEndpoint + ciPath, contentsEndpoint + ciPath, findCheckEndpoint, checkEndpoint, prEndpoint, appEndpoint, listComments, updateComment(12)})
	if got := h.gh.comments[0].Body; got != report.NoChanges(testHead) {
		t.Fatalf("comment = %q", got)
	}
	checks := h.gh.find("POST", "/check-runs")
	if !strings.Contains(checks[0].Body, `"title":"No execution changes"`) {
		t.Fatalf("check run = %s", checks[0].Body)
	}
}

func TestAnalyzeNoChangeWithoutCommentDoesNothing(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupIdentical(h)
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint, contentsEndpoint + ciPath, contentsEndpoint + ciPath, findCheckEndpoint, checkEndpoint, prEndpoint, appEndpoint, listComments})
	if n := h.count("SELECT count(*) FROM pr_comments"); n != 0 {
		t.Fatalf("pr_comments = %d", n)
	}
	if n := h.count("SELECT count(*) FROM analyses WHERE finding_count = 0"); n != 1 {
		t.Fatalf("analyses = %d", n)
	}
}

func TestAnalyzeCachedCommentDeleted(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	if err := h.store.SetCommentID(context.Background(), 42, 3, 999); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	h.gh.comments = []github.IssueComment{{ID: 12, Body: report0("old"), User: github.User{Login: botLogin}}}
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint, contentsEndpoint + ciPath, contentsEndpoint + ciPath, findCheckEndpoint, checkEndpoint, prEndpoint, updateComment(999), appEndpoint, listComments, updateComment(12)})
	expectJob(t, h, "done", 1)
	id, ok, err := h.store.CommentID(context.Background(), 42, 3)
	if err != nil || !ok || id != 12 {
		t.Fatalf("cached = %d %v %v", id, ok, err)
	}
}

func TestAnalyzeCachedCommentDeletedRecreates(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	if err := h.store.SetCommentID(context.Background(), 42, 3, 999); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectEndpoints(t, h, []string{filesEndpoint, contentsEndpoint + ciPath, contentsEndpoint + ciPath, findCheckEndpoint, checkEndpoint, prEndpoint, updateComment(999), appEndpoint, listComments, createComment})
	id, ok, err := h.store.CommentID(context.Background(), 42, 3)
	if err != nil || !ok || id != 5001 {
		t.Fatalf("cached = %d %v %v", id, ok, err)
	}
}

func manyJobsWorkflow(n int) string {
	var b strings.Builder
	b.WriteString("on: push\njobs:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  job%02d:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo %d\n", i, i)
	}
	return b.String()
}

func TestAnalyzeAnnotationBatches(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	h.gh.files = []github.PullRequestFile{{Filename: ciPath, Status: "modified"}}
	h.gh.setContent(testBase, ciPath, manyJobsWorkflow(1))
	h.gh.setContent(testHead, ciPath, manyJobsWorkflow(61))
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectJob(t, h, "done", 1)
	count := func(body string) int {
		var payload struct {
			Output struct {
				Annotations []json.RawMessage `json:"annotations"`
			} `json:"output"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return len(payload.Output.Annotations)
	}
	posts := h.gh.find("POST", "/check-runs")
	patches := h.gh.find("PATCH", "/check-runs/900")
	if len(posts) != 1 || len(patches) != 1 {
		t.Fatalf("posts %d patches %d", len(posts), len(patches))
	}
	if n := count(posts[0].Body); n != 50 {
		t.Fatalf("first batch = %d", n)
	}
	if n := count(patches[0].Body); n != 11 {
		t.Fatalf("second batch = %d", n)
	}
}

func TestAnalyzeRetryReusesCheckRun(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	h.gh.files = []github.PullRequestFile{{Filename: ciPath, Status: "modified"}}
	h.gh.setContent(testBase, ciPath, manyJobsWorkflow(1))
	h.gh.setContent(testHead, ciPath, manyJobsWorkflow(121))
	h.gh.broken["PATCH /repos/octo/demo/check-runs/900"] = true
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectJob(t, h, "pending", 1)
	h.gh.mu.Lock()
	partial := h.gh.checks[0].Annotations
	h.gh.mu.Unlock()
	if partial != 50 {
		t.Fatalf("annotations after failed attempt = %d", partial)
	}
	h.gh.broken["PATCH /repos/octo/demo/check-runs/900"] = false
	if _, err := h.db.Exec(context.Background(), "UPDATE queue_jobs SET run_after = now()"); err != nil {
		t.Fatal(err)
	}
	h.gh.resetLog()
	h.runOne()
	expectJob(t, h, "done", 2)
	if n := len(h.gh.find("POST", "/check-runs")); n != 0 {
		t.Fatalf("retry created %d new check runs", n)
	}
	h.gh.mu.Lock()
	defer h.gh.mu.Unlock()
	if len(h.gh.checks) != 1 || h.gh.checks[0].Annotations != 121 {
		t.Fatalf("check runs = %d, annotations = %d, want 1 run with 121", len(h.gh.checks), h.gh.checks[0].Annotations)
	}
}

func TestAnalyzeIgnoresOtherAppsCheckRuns(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	h.gh.checks = append(h.gh.checks, &fakeCheckRun{ID: 800, HeadSHA: testHead, AppID: 99, Annotations: 3})
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectJob(t, h, "done", 1)
	if n := len(h.gh.find("POST", "/check-runs")); n != 1 {
		t.Fatalf("creates = %d", n)
	}
	if n := len(h.gh.find("PATCH", "/check-runs/800")); n != 0 {
		t.Fatalf("other app's check run patched %d times", n)
	}
}

func TestAnalyzeRateLimitDefers(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	h.gh.limited[filesEndpoint] = true
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	job := expectJob(t, h, "pending", 0)
	if job.Delay < 3590 || job.Delay > 3632 {
		t.Fatalf("delay = %.1f", job.Delay)
	}
	if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(KindAnalyzePR, metrics.ResultDeferred)); got != 1 {
		t.Fatalf("deferred metric = %v", got)
	}
	if got := testutil.ToFloat64(h.metrics.GitHubRateLimitRemaining); got != 0 {
		t.Fatalf("rate limit gauge = %v", got)
	}
}

func TestAnalyzeTransientFailure(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	h.gh.broken["POST /repos/octo/demo/check-runs"] = true
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	job := expectJob(t, h, "pending", 1)
	if !strings.Contains(job.LastError, "status 500") || job.Delay < 25 || job.Delay > 31 {
		t.Fatalf("job = %+v", job)
	}
	if n := h.count("SELECT count(*) FROM analyses"); n != 0 {
		t.Fatalf("analyses = %d", n)
	}
	if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(KindAnalyzePR, metrics.ResultFailed)); got != 1 {
		t.Fatalf("failed metric = %v", got)
	}
}

func TestBadPayloadsAreKilled(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		payload any
		want    string
	}{
		{"not an object", KindAnalyzePR, "nope", "decode payload"},
		{"incomplete analyze", KindAnalyzePR, map[string]any{"installation_id": 7}, "analyze_pr payload is incomplete"},
		{"incomplete ingest", KindIngestWorkflowRun, map[string]any{}, "ingest_workflow_run payload is incomplete"},
		{"incomplete backfill", KindBackfillRepository, []int{1}, "decode payload"},
		{"unknown kind", "mystery", map[string]any{}, `unknown job kind "mystery"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.enqueue(tt.kind, tt.payload)
			h.runOne()
			job := expectJob(t, h, "dead", 1)
			if !strings.Contains(job.LastError, tt.want) {
				t.Fatalf("last_error = %q", job.LastError)
			}
			if len(h.gh.endpoints()) != 0 {
				t.Fatalf("github called: %v", h.gh.endpoints())
			}
			if got := testutil.ToFloat64(h.metrics.QueueJobs.WithLabelValues(tt.kind, metrics.ResultDead)); got != 1 {
				t.Fatalf("dead metric = %v", got)
			}
		})
	}
}

func TestAnalyzeSuspendedInstallation(t *testing.T) {
	h := newHarness(t, nil)
	h.seedRepo()
	setupModified(h)
	if err := h.store.SetInstallationSuspended(context.Background(), 7, true); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectJob(t, h, "done", 1)
	if len(h.gh.log) != 0 {
		t.Fatalf("github called: %v", h.gh.log)
	}
}

func TestAnalyzeFileLimitAndOversize(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.MaxWorkflowFiles = 1 })
	h.seedRepo()
	h.gh.files = []github.PullRequestFile{
		{Filename: ".github/workflows/z.yml", Status: "modified"},
		{Filename: ".github/workflows/big.yml", Status: "modified"},
	}
	small := readFixture(t, fixtureDir+"identical/before.yml")
	h.gh.setContent(testBase, ".github/workflows/big.yml", small)
	h.gh.setContent(testHead, ".github/workflows/big.yml", small+"#"+strings.Repeat("x", 1<<20))
	h.enqueue(KindAnalyzePR, analyzePayload())
	h.runOne()
	expectJob(t, h, "done", 1)
	for _, r := range h.gh.requests() {
		if strings.Contains(r.Path, "z.yml") {
			t.Fatalf("skipped file fetched: %s", r.Path)
		}
	}
	var result string
	if err := h.db.QueryRow(context.Background(), "SELECT result::text FROM analyses").Scan(&result); err != nil {
		t.Fatalf("analysis: %v", err)
	}
	for _, want := range []string{`"skipped_files": 1`, `"status": "unanalyzable"`, `"error": "file exceeds 1 MiB"`} {
		if !strings.Contains(result, want) {
			t.Errorf("result lacks %s: %s", want, result)
		}
	}
	comments := h.gh.find("POST", "/issues/3/comments")
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "1 additional workflow file was not analyzed.") {
		t.Fatalf("comments = %+v", comments)
	}
}
