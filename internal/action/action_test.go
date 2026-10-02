package action

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/report"
	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/internal/github"
)

var update = flag.Bool("update", false, "update golden files")

const (
	goldenDir  = "../../testdata/golden/action"
	fixtureDir = "../../testdata/golden/semdiff/"
	ciPath     = ".github/workflows/ci.yml"
	baseSHA    = "2222222222222222222222222222222222222222"
	headSHA    = "1111111111111111111111111111111111111111"
	mergeSHA   = "3333333333333333333333333333333333333333"
	testToken  = "ghs_actiontokenvalue0123456789"
)

type recorded struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Body   string
}

type fakeGitHub struct {
	t        *testing.T
	mu       sync.Mutex
	requests []recorded
	handlers map[string]http.HandlerFunc
	server   *httptest.Server
}

func newFake(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, handlers: map[string]http.HandlerFunc{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) handle(pattern string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[pattern] = h
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("read body: %v", err)
	}
	f.mu.Lock()
	f.requests = append(f.requests, recorded{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), Body: string(body)})
	h, ok := f.handlers[r.Method+" "+r.URL.EscapedPath()]
	if !ok && strings.HasPrefix(r.URL.Path, "/repos/o/r/contents/") {
		h, ok = f.handlers[r.Method+" /repos/o/r/contents/*"]
	}
	f.mu.Unlock()
	if !ok {
		writeJSON(f.t, w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	h(w, r)
}

func (f *fakeGitHub) find(method, path string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.requests {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeGitHub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		t.Errorf("encode: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		t.Errorf("write: %v", err)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureDir + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func (f *fakeGitHub) setPullRequest(files []map[string]string, contents map[string][]byte) {
	f.handle("GET /repos/o/r/pulls/3/files", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(f.t, w, http.StatusOK, files)
	})
	f.handle("GET /repos/o/r/compare/"+baseSHA+"..."+headSHA, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(f.t, w, http.StatusOK, map[string]any{"merge_base_commit": map[string]string{"sha": mergeSHA}})
	})
	for key, data := range contents {
		ref, path, _ := strings.Cut(key, ":")
		body := data
		f.handle("GET /repos/o/r/contents/"+path, f.contentHandler(ref, body, f.handlers["GET /repos/o/r/contents/"+path]))
	}
}

func (f *fakeGitHub) contentHandler(ref string, body []byte, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != ref {
			if next != nil {
				next(w, r)
				return
			}
			writeJSON(f.t, w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		if _, err := w.Write(body); err != nil {
			f.t.Errorf("write: %v", err)
		}
	}
}

func (f *fakeGitHub) setModified(t *testing.T, fixture string) {
	f.setPullRequest(
		[]map[string]string{{"filename": ciPath, "status": "modified"}, {"filename": "src/main.go", "status": "modified"}},
		map[string][]byte{
			mergeSHA + ":" + ciPath: readFixture(t, fixture+"/before.yml"),
			headSHA + ":" + ciPath:  readFixture(t, fixture+"/after.yml"),
		},
	)
}

func (f *fakeGitHub) setHistory(runs int) {
	f.handle("GET /repos/o/r/actions/workflows/ci.yml/runs", func(w http.ResponseWriter, r *http.Request) {
		list := []map[string]any{}
		for i := 1; i <= runs; i++ {
			list = append(list, map[string]any{"id": i, "path": ciPath, "status": "completed", "conclusion": "success"})
		}
		writeJSON(f.t, w, http.StatusOK, map[string]any{"workflow_runs": list})
	})
	for i := 1; i <= runs; i++ {
		id := i
		f.handle("GET /repos/o/r/actions/runs/"+strconv.Itoa(id)+"/jobs", func(w http.ResponseWriter, r *http.Request) {
			start := time.Date(2026, 1, id, 0, 0, 0, 0, time.UTC)
			jobs := []map[string]any{
				{"id": id * 100, "name": "lint", "conclusion": "success", "started_at": start, "completed_at": start.Add(time.Minute)},
				{"id": id*100 + 99, "name": "lint", "conclusion": "failure", "started_at": start, "completed_at": start.Add(time.Hour)},
				{"id": id*100 + 98, "name": "call / inner", "conclusion": "success", "started_at": start, "completed_at": start.Add(time.Hour)},
			}
			n := 0
			for _, osName := range []string{"ubuntu-latest", "windows-latest", "macos-latest"} {
				for _, node := range []string{"18", "20"} {
					n++
					jobs = append(jobs, map[string]any{"id": id*100 + n, "name": "test (" + osName + ", " + node + ")", "conclusion": "success", "started_at": start, "completed_at": start.Add(2 * time.Minute)})
				}
			}
			writeJSON(f.t, w, http.StatusOK, map[string]any{"jobs": jobs})
		})
	}
}

func (f *fakeGitHub) setComments(comments []map[string]any) {
	f.handle("GET /repos/o/r/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(f.t, w, http.StatusOK, comments)
	})
	f.handle("POST /repos/o/r/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(f.t, w, http.StatusCreated, map[string]any{"id": 900})
	})
	for _, c := range comments {
		f.handle(fmt.Sprintf("PATCH /repos/o/r/issues/comments/%v", c["id"]), func(w http.ResponseWriter, r *http.Request) {
			writeJSON(f.t, w, http.StatusOK, map[string]any{})
		})
	}
}

type env struct {
	vars    map[string]string
	summary string
}

func newEnv(t *testing.T, f *fakeGitHub) *env {
	t.Helper()
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := map[string]any{"action": "synchronize", "pull_request": map[string]any{
		"number": 3,
		"head":   map[string]string{"sha": headSHA, "ref": "feature"},
		"base":   map[string]string{"sha": baseSHA, "ref": "main"},
	}}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	e := &env{summary: filepath.Join(dir, "summary.md")}
	e.vars = map[string]string{
		"GITHUB_EVENT_NAME":   "pull_request",
		"GITHUB_EVENT_PATH":   eventPath,
		"GITHUB_TOKEN":        testToken,
		"GITHUB_REPOSITORY":   "o/r",
		"GITHUB_STEP_SUMMARY": e.summary,
		"INPUT_COMMENT":       "true",
		"INPUT_ESTIMATE":      "true",
		"INPUT_MAX_FILES":     "50",
	}
	if f != nil {
		e.vars["GITHUB_API_URL"] = f.server.URL
	}
	return e
}

func (e *env) getenv(k string) string {
	return e.vars[k]
}

func (e *env) run(t *testing.T) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), e.getenv, &stdout, &stderr, "1.2.3")
	return code, stdout.String(), stderr.String()
}

func (e *env) readSummary(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(e.summary)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	return string(data)
}

func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func decodeBody(t *testing.T, raw string) string {
	t.Helper()
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return payload.Body
}

func TestEndToEndGolden(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	f.setHistory(5)
	f.setComments([]map[string]any{})
	e := newEnv(t, f)
	code, stdout, stderr := e.run(t)
	if code != 0 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	compareGolden(t, "commands.txt", []byte(stdout))
	compareGolden(t, "summary.md", []byte(e.readSummary(t)))
	posts := f.find("POST", "/repos/o/r/issues/3/comments")
	if len(posts) != 1 {
		t.Fatalf("comment posts = %d", len(posts))
	}
	compareGolden(t, "comment.md", []byte(decodeBody(t, posts[0].Body)))
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if r.Auth != "Bearer "+testToken {
			t.Errorf("%s %s auth = %q", r.Method, r.Path, r.Auth)
		}
		if r.Method == "GET" && strings.HasPrefix(r.Path, "/repos/o/r/contents/") && !strings.Contains(r.Query, mergeSHA) && !strings.Contains(r.Query, headSHA) {
			t.Errorf("content read at %q", r.Query)
		}
	}
}

func TestEstimateRecordsCallsAndFindings(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	f.setHistory(5)
	e := newEnv(t, f)
	e.vars["INPUT_COMMENT"] = "false"
	if code, _, _ := e.run(t); code != 0 {
		t.Fatalf("code = %d", code)
	}
	summary := e.readSummary(t)
	for _, want := range []string{"| " + report.EstimateLabel + " | 13 | 49 |", "Runner-minute estimate: 6 GitHub API calls for 1 workflow file."} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q\n%s", want, summary)
		}
	}
	if n := len(f.find("GET", "/repos/o/r/issues/3/comments")); n != 0 {
		t.Errorf("comment requests with comment=false: %d", n)
	}
}

func TestEstimateFailureFallsBack(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	f.handle("GET /repos/o/r/actions/workflows/ci.yml/runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"message": "boom"})
	})
	f.setComments([]map[string]any{})
	e := newEnv(t, f)
	code, stdout, _ := e.run(t)
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	summary := e.readSummary(t)
	if !strings.Contains(summary, "\n"+estimateSkipped+"\n") || strings.Contains(summary, report.EstimateLabel) {
		t.Errorf("summary = %s", summary)
	}
	if !strings.Contains(summary, "Job `test` matrix: 6 → 24 jobs") {
		t.Errorf("analysis missing from summary: %s", summary)
	}
	if !strings.Contains(stdout, "::warning title=quanto::runner-minute estimate skipped: ") {
		t.Errorf("stdout = %s", stdout)
	}
	if n := len(f.find("POST", "/repos/o/r/issues/3/comments")); n != 1 {
		t.Errorf("comment posts = %d", n)
	}
}

func TestEstimateWithoutHistory(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	e := newEnv(t, f)
	e.vars["INPUT_COMMENT"] = "false"
	code, stdout, _ := e.run(t)
	if code != 0 || strings.Contains(stdout, "::warning") {
		t.Fatalf("code = %d, stdout = %s", code, stdout)
	}
	if summary := e.readSummary(t); !strings.Contains(summary, "Runner-minute estimate: 1 GitHub API call for 1 workflow file.") {
		t.Errorf("summary = %s", summary)
	}
}

func TestEstimateDisabled(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	f.setHistory(5)
	e := newEnv(t, f)
	e.vars["INPUT_ESTIMATE"] = "FALSE"
	e.vars["INPUT_COMMENT"] = "false"
	if code, _, _ := e.run(t); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if n := len(f.find("GET", "/repos/o/r/actions/workflows/ci.yml/runs")); n != 0 {
		t.Errorf("runs requests = %d", n)
	}
	if summary := e.readSummary(t); strings.Contains(summary, "Runner-minute estimate") {
		t.Errorf("summary = %s", summary)
	}
}

func TestEstimatePaths(t *testing.T) {
	w := &model.Workflow{}
	var inputs []semdiff.Input
	for i := 11; i >= 0; i-- {
		inputs = append(inputs, semdiff.Input{Path: fmt.Sprintf(".github/workflows/w%02d.yml", i), After: w})
	}
	inputs = append(inputs,
		semdiff.Input{Path: ".github/workflows/a-new.yml", OldPath: ".github/workflows/a-old.yml", Before: w, After: w},
		semdiff.Input{Path: ".github/workflows/a-gone.yml", Before: w},
		semdiff.Input{Path: ".github/workflows/a-broken.yml"},
	)
	got := estimatePaths(inputs)
	want := []string{".github/workflows/a-gone.yml", ".github/workflows/a-new.yml", ".github/workflows/a-old.yml", ".github/workflows/w00.yml", ".github/workflows/w01.yml", ".github/workflows/w02.yml", ".github/workflows/w03.yml", ".github/workflows/w04.yml", ".github/workflows/w05.yml", ".github/workflows/w06.yml"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v", got)
	}
	if len(got) != EstimateMaxFiles {
		t.Errorf("len = %d", len(got))
	}
}

func TestEstimateWindowAndRounding(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/o/r/actions/workflows/ci.yml/runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"workflow_runs": []map[string]any{{"id": 1}}})
	})
	f.handle("GET /repos/o/r/actions/runs/1/jobs", func(w http.ResponseWriter, r *http.Request) {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		var jobs []map[string]any
		for i := 0; i < 40; i++ {
			secs := 10
			if i >= 10 {
				secs = 21
			}
			if i == 39 {
				secs = 20
			}
			jobs = append(jobs, map[string]any{"id": i + 1, "name": fmt.Sprintf("build (%d)", i), "conclusion": "success", "started_at": start.Add(time.Duration(i) * time.Minute), "completed_at": start.Add(time.Duration(i)*time.Minute + time.Duration(secs)*time.Second)})
		}
		jobs = append(jobs, map[string]any{"id": 99, "name": "skipped", "conclusion": "skipped", "started_at": nil, "completed_at": nil})
		writeJSON(t, w, http.StatusOK, map[string]any{"jobs": jobs})
	})
	e := newEnv(t, f)
	cfg, err := loadConfig(e.getenv)
	if err != nil {
		t.Fatal(err)
	}
	client, err := github.NewTokenClient(github.Options{BaseURL: f.server.URL}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{cfg: cfg, client: client, stdout: io.Discard}
	table := durationTable{}
	if err := r.collect(context.Background(), ciPath, table); err != nil {
		t.Fatal(err)
	}
	avg, n, ok := table.JobAverage(ciPath, "build")
	if !ok || n != 30 || avg != 21*time.Second {
		t.Errorf("build = %v %d %v", avg, n, ok)
	}
	if _, _, ok := table.JobAverage(ciPath, "skipped"); ok {
		t.Error("skipped job recorded")
	}
}

func TestForkReadOnlyToken(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	f.setComments([]map[string]any{})
	f.handle("POST /repos/o/r/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
	})
	e := newEnv(t, f)
	e.vars["INPUT_ESTIMATE"] = "false"
	code, stdout, stderr := e.run(t)
	if code != 0 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "::warning") {
		t.Errorf("stdout = %s", stdout)
	}
	summary := e.readSummary(t)
	if !strings.HasSuffix(summary, "\ncomment skipped: token is read-only\n") {
		t.Errorf("summary = %s", summary)
	}
}

func TestCommentRateLimitIsNotReadOnly(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	f.setComments([]map[string]any{})
	f.handle("POST /repos/o/r/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded"})
	})
	e := newEnv(t, f)
	e.vars["INPUT_ESTIMATE"] = "false"
	code, stdout, _ := e.run(t)
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout, "::warning title=quanto::comment failed: ") {
		t.Errorf("stdout = %s", stdout)
	}
	if summary := e.readSummary(t); !strings.HasSuffix(summary, "\n"+commentFailed+"\n") {
		t.Errorf("summary = %s", summary)
	}
}

func TestCommentUpdatesBotCommentOnly(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	f.setComments([]map[string]any{
		{"id": 11, "body": report.CommentMarker + "\nhuman copy", "user": map[string]string{"login": "octocat"}},
		{"id": 12, "body": "unrelated", "user": map[string]string{"login": BotLogin}},
		{"id": 13, "body": report.CommentMarker + "\nold", "user": map[string]string{"login": BotLogin}},
	})
	e := newEnv(t, f)
	e.vars["INPUT_ESTIMATE"] = "false"
	if code, _, _ := e.run(t); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if n := len(f.find("POST", "/repos/o/r/issues/3/comments")); n != 0 {
		t.Errorf("posts = %d", n)
	}
	for _, id := range []string{"11", "12"} {
		if n := len(f.find("PATCH", "/repos/o/r/issues/comments/"+id)); n != 0 {
			t.Errorf("comment %s patched", id)
		}
	}
	patches := f.find("PATCH", "/repos/o/r/issues/comments/13")
	if len(patches) != 1 {
		t.Fatalf("patches = %d", len(patches))
	}
	if body := decodeBody(t, patches[0].Body); !strings.HasPrefix(body, report.CommentMarker) || !strings.Contains(body, "6 → 24 jobs") {
		t.Errorf("body = %s", body)
	}
}

func TestHumanMarkerCommentDoesNotBlockCreate(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "matrix-axis-added")
	f.setComments([]map[string]any{
		{"id": 11, "body": report.CommentMarker + "\nhuman copy", "user": map[string]string{"login": "octocat"}},
	})
	e := newEnv(t, f)
	e.vars["INPUT_ESTIMATE"] = "false"
	if code, _, _ := e.run(t); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if n := len(f.find("POST", "/repos/o/r/issues/3/comments")); n != 1 {
		t.Errorf("posts = %d", n)
	}
	if n := len(f.find("PATCH", "/repos/o/r/issues/comments/11")); n != 0 {
		t.Errorf("human comment patched")
	}
}

func TestBelowThresholdComment(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		existing bool
		want     string
	}{
		{"below threshold without comment", "action-major-bump", false, ""},
		{"below threshold with comment", "action-major-bump", true, report.BelowThreshold(headSHA)},
		{"no changes with comment", "identical", true, report.NoChanges(headSHA)},
		{"no changes without comment", "identical", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			f.setModified(t, tt.fixture)
			comments := []map[string]any{}
			if tt.existing {
				comments = append(comments, map[string]any{"id": 21, "body": report.CommentMarker + "\nold", "user": map[string]string{"login": BotLogin}})
			}
			f.setComments(comments)
			e := newEnv(t, f)
			e.vars["INPUT_ESTIMATE"] = "false"
			if code, _, _ := e.run(t); code != 0 {
				t.Fatalf("code = %d", code)
			}
			if n := len(f.find("POST", "/repos/o/r/issues/3/comments")); n != 0 {
				t.Errorf("posts = %d", n)
			}
			patches := f.find("PATCH", "/repos/o/r/issues/comments/21")
			if tt.want == "" {
				if len(patches) != 0 {
					t.Errorf("patches = %d", len(patches))
				}
				return
			}
			if len(patches) != 1 || decodeBody(t, patches[0].Body) != tt.want {
				t.Errorf("patches = %+v", patches)
			}
		})
	}
}

func TestEventMismatch(t *testing.T) {
	for _, name := range []string{"push", "", "workflow_dispatch", "pull_request_review"} {
		f := newFake(t)
		e := newEnv(t, f)
		e.vars["GITHUB_EVENT_NAME"] = name
		delete(e.vars, "GITHUB_TOKEN")
		code, stdout, stderr := e.run(t)
		if code != 0 || stderr != "" || stdout != notPullRequest+"\n" {
			t.Errorf("%q: code = %d, stdout = %q, stderr = %q", name, code, stdout, stderr)
		}
		if f.count() != 0 {
			t.Errorf("%q: requests = %d", name, f.count())
		}
		if _, err := os.Stat(e.summary); !os.IsNotExist(err) {
			t.Errorf("%q: summary written", name)
		}
	}
}

func TestPullRequestTarget(t *testing.T) {
	f := newFake(t)
	f.setModified(t, "identical")
	e := newEnv(t, f)
	e.vars["GITHUB_EVENT_NAME"] = "pull_request_target"
	e.vars["INPUT_ESTIMATE"] = "false"
	e.vars["INPUT_COMMENT"] = "false"
	if code, _, _ := e.run(t); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if summary := e.readSummary(t); !strings.HasPrefix(summary, "## quanto\n\nNo execution changes in 1 workflow file.") {
		t.Errorf("summary = %s", summary)
	}
}

func TestConfigurationErrors(t *testing.T) {
	tests := []struct {
		name string
		edit func(e *env)
		want string
	}{
		{"missing token", func(e *env) { delete(e.vars, "GITHUB_TOKEN") }, "GITHUB_TOKEN is not set"},
		{"missing event path", func(e *env) { delete(e.vars, "GITHUB_EVENT_PATH") }, "GITHUB_EVENT_PATH is not set"},
		{"unreadable event", func(e *env) { e.vars["GITHUB_EVENT_PATH"] = filepath.Join(filepath.Dir(e.summary), "missing.json") }, "read event file"},
		{"bad repository", func(e *env) { e.vars["GITHUB_REPOSITORY"] = "only-owner" }, "GITHUB_REPOSITORY must be owner/repo"},
		{"bad comment", func(e *env) { e.vars["INPUT_COMMENT"] = "yes" }, "INPUT_COMMENT must be true or false"},
		{"bad estimate", func(e *env) { e.vars["INPUT_ESTIMATE"] = "1" }, "INPUT_ESTIMATE must be true or false"},
		{"bad max files", func(e *env) { e.vars["INPUT_MAX_FILES"] = "201" }, "INPUT_MAX_FILES must be an integer from 1 to 200"},
		{"bad api url", func(e *env) { e.vars["GITHUB_API_URL"] = "ftp://example.com" }, "base URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			e := newEnv(t, f)
			tt.edit(e)
			code, stdout, stderr := e.run(t)
			if code != 1 || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
			}
			if strings.Contains(stderr, testToken) {
				t.Errorf("stderr leaks token")
			}
			if f.count() != 0 {
				t.Errorf("requests = %d", f.count())
			}
		})
	}
}

func TestEventWithoutPullRequest(t *testing.T) {
	f := newFake(t)
	e := newEnv(t, f)
	path := filepath.Join(filepath.Dir(e.summary), "push.json")
	if err := os.WriteFile(path, []byte(`{"ref":"refs/heads/main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e.vars["GITHUB_EVENT_PATH"] = path
	code, _, stderr := e.run(t)
	if code != 1 || !strings.Contains(stderr, "event file has no pull_request") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestMultipleConfigurationErrorsReported(t *testing.T) {
	e := newEnv(t, nil)
	delete(e.vars, "GITHUB_TOKEN")
	e.vars["GITHUB_REPOSITORY"] = ""
	code, _, stderr := e.run(t)
	if code != 1 || !strings.Contains(stderr, "quanto: GITHUB_TOKEN is not set\n") || !strings.Contains(stderr, "quanto: GITHUB_REPOSITORY must be owner/repo\n") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestAnalysisFailure(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/o/r/pulls/3/files", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"message": "boom"})
	})
	e := newEnv(t, f)
	code, stdout, _ := e.run(t)
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.HasPrefix(stdout, "::warning title=quanto::analysis failed: ") || strings.Count(stdout, "\n") != 1 {
		t.Errorf("stdout = %q", stdout)
	}
	if summary := e.readSummary(t); summary != analysisFailed {
		t.Errorf("summary = %q", summary)
	}
}

func TestNoWorkflowFiles(t *testing.T) {
	f := newFake(t)
	f.setPullRequest([]map[string]string{{"filename": "README.md", "status": "modified"}}, nil)
	e := newEnv(t, f)
	code, stdout, _ := e.run(t)
	if code != 0 || stdout != "" {
		t.Fatalf("code = %d, stdout = %q", code, stdout)
	}
	if summary := e.readSummary(t); summary != noWorkflowFiles {
		t.Errorf("summary = %q", summary)
	}
	if f.count() != 1 {
		t.Errorf("requests = %d", f.count())
	}
}

func TestSummaryAppends(t *testing.T) {
	f := newFake(t)
	f.setPullRequest([]map[string]string{}, nil)
	e := newEnv(t, f)
	if err := os.WriteFile(e.summary, []byte("previous\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := e.run(t); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if summary := e.readSummary(t); summary != "previous\n"+noWorkflowFiles {
		t.Errorf("summary = %q", summary)
	}
}

func TestNoSummaryPath(t *testing.T) {
	f := newFake(t)
	f.setPullRequest([]map[string]string{}, nil)
	e := newEnv(t, f)
	delete(e.vars, "GITHUB_STEP_SUMMARY")
	if code, stdout, _ := e.run(t); code != 0 || stdout != "" {
		t.Fatalf("code = %d, stdout = %q", code, stdout)
	}
}

func TestUnwritableSummary(t *testing.T) {
	f := newFake(t)
	f.setPullRequest([]map[string]string{}, nil)
	e := newEnv(t, f)
	e.vars["GITHUB_STEP_SUMMARY"] = filepath.Join(filepath.Dir(e.summary), "missing", "summary.md")
	code, stdout, _ := e.run(t)
	if code != 0 || !strings.HasPrefix(stdout, "::warning title=quanto::write job summary: ") {
		t.Fatalf("code = %d, stdout = %q", code, stdout)
	}
}

func TestInjectedPathStaysInCommand(t *testing.T) {
	f := newFake(t)
	evil := ".github/workflows/x\n::add-mask::secret,title=y:z%0A\r::stop-commands::tok.yml"
	f.setPullRequest([]map[string]string{{"filename": evil, "status": "modified"}}, nil)
	before, after := readFixture(t, "matrix-axis-added/before.yml"), readFixture(t, "matrix-axis-added/after.yml")
	f.handle("GET /repos/o/r/contents/*", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/contents/"+evil {
			t.Errorf("path = %q", r.URL.Path)
		}
		body := before
		if r.URL.Query().Get("ref") == headSHA {
			body = after
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("write: %v", err)
		}
	})
	e := newEnv(t, f)
	e.vars["INPUT_ESTIMATE"] = "false"
	e.vars["INPUT_COMMENT"] = "false"
	code, stdout, _ := e.run(t)
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	want := "::notice file=.github/workflows/x%0A%3A%3Aadd-mask%3A%3Asecret%2Ctitle=y%3Az%250A%0D%3A%3Astop-commands%3A%3Atok.yml,line=13,endLine=15,title=matrix.count_changed::Job `test` matrix: 6 → 24 jobs\n"
	if stdout != want {
		t.Errorf("stdout = %q", stdout)
	}
}
