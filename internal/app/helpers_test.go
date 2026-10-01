package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BETAER-08/quanto/internal/github"
	"github.com/BETAER-08/quanto/internal/metrics"
	"github.com/BETAER-08/quanto/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testSecret = "test-webhook-secret"
	testOwner  = "octo"
	testRepo   = "demo"
	testHead   = "1111111111111111111111111111111111111111"
	testBase   = "2222222222222222222222222222222222222222"
	botLogin   = "quanto-test[bot]"
)

var (
	keyOnce sync.Once
	keyPEM  []byte
	keyErr  error
)

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	keyOnce.Do(func() {
		var key *rsa.PrivateKey
		key, keyErr = rsa.GenerateKey(rand.Reader, 2048)
		if keyErr == nil {
			keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		}
	})
	if keyErr != nil {
		t.Fatalf("generate key: %v", keyErr)
	}
	return keyPEM
}

func sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type loggedRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

func (r loggedRequest) String() string {
	return r.Method + " " + r.Path
}

type fakeGitHub struct {
	t         *testing.T
	server    *httptest.Server
	mu        sync.Mutex
	log       []loggedRequest
	files     []github.PullRequestFile
	contents  map[string]string
	headSHA   string
	comments  []github.IssueComment
	nextID    int64
	runs      []github.WorkflowRun
	jobs      []map[string]any
	limited   map[string]bool
	broken    map[string]bool
	slow      map[string]bool
	checks    []*fakeCheckRun
	mergeBase string
}

type fakeCheckRun struct {
	ID          int64
	HeadSHA     string
	AppID       int64
	Annotations int
}

func (f *fakeGitHub) checkRunLocked(id string) *fakeCheckRun {
	for _, c := range f.checks {
		if strconv.FormatInt(c.ID, 10) == id {
			return c
		}
	}
	return nil
}

func decodeCheckBody(t *testing.T, r *http.Request) (string, int) {
	var body struct {
		HeadSHA string `json:"head_sha"`
		Output  struct {
			Annotations []json.RawMessage `json:"annotations"`
		} `json:"output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode check run: %v", err)
	}
	return body.HeadSHA, len(body.Output.Annotations)
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, contents: map[string]string{}, headSHA: testHead, mergeBase: testBase, nextID: 5000, limited: map[string]bool{}, broken: map[string]bool{}, slow: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		f.json(w, http.StatusCreated, map[string]any{"token": "ghs_test", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	})
	mux.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) {
		f.json(w, http.StatusOK, map[string]any{"id": 1, "slug": "quanto-test", "name": "quanto test"})
	})
	mux.HandleFunc("GET /repos/{o}/{r}/pulls/{n}/files", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.jsonLocked(w, http.StatusOK, f.files)
	})
	mux.HandleFunc("GET /repos/{o}/{r}/pulls/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil {
			f.json(w, http.StatusBadRequest, map[string]string{"message": "bad number"})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.jsonLocked(w, http.StatusOK, map[string]any{"number": n, "state": "open", "head": map[string]string{"sha": f.headSHA}, "base": map[string]string{"sha": testBase}})
	})
	mux.HandleFunc("GET /repos/{o}/{r}/compare/{basehead}", func(w http.ResponseWriter, r *http.Request) {
		base, head, ok := strings.Cut(r.PathValue("basehead"), "...")
		if !ok || base != testBase || head == "" {
			f.json(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.jsonLocked(w, http.StatusOK, map[string]any{"status": "diverged", "merge_base_commit": map[string]string{"sha": f.mergeBase}, "base_commit": map[string]string{"sha": base}})
	})
	mux.HandleFunc("GET /repos/{o}/{r}/contents/{path...}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		content, ok := f.contents[r.URL.Query().Get("ref")+":"+r.PathValue("path")]
		f.mu.Unlock()
		if !ok {
			f.json(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(w, content); err != nil {
			t.Errorf("write content: %v", err)
		}
	})
	mux.HandleFunc("POST /repos/{o}/{r}/check-runs", func(w http.ResponseWriter, r *http.Request) {
		head, n := decodeCheckBody(t, r)
		f.mu.Lock()
		defer f.mu.Unlock()
		c := &fakeCheckRun{ID: int64(900 + len(f.checks)), HeadSHA: head, AppID: 1, Annotations: n}
		f.checks = append(f.checks, c)
		f.jsonLocked(w, http.StatusCreated, map[string]any{"id": c.ID})
	})
	mux.HandleFunc("PATCH /repos/{o}/{r}/check-runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, n := decodeCheckBody(t, r)
		f.mu.Lock()
		defer f.mu.Unlock()
		c := f.checkRunLocked(r.PathValue("id"))
		if c == nil {
			f.jsonLocked(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		c.Annotations += n
		f.jsonLocked(w, http.StatusOK, map[string]any{"id": c.ID})
	})
	mux.HandleFunc("GET /repos/{o}/{r}/check-runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		c := f.checkRunLocked(r.PathValue("id"))
		if c == nil {
			f.jsonLocked(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		f.jsonLocked(w, http.StatusOK, map[string]any{"id": c.ID, "app": map[string]any{"id": c.AppID}, "output": map[string]any{"annotations_count": c.Annotations}})
	})
	mux.HandleFunc("GET /repos/{o}/{r}/commits/{ref}/check-runs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("check_name") != "quanto" {
			t.Errorf("check_name = %q", r.URL.Query().Get("check_name"))
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		runs := []map[string]any{}
		for i := len(f.checks) - 1; i >= 0; i-- {
			c := f.checks[i]
			if c.HeadSHA == r.PathValue("ref") {
				runs = append(runs, map[string]any{"id": c.ID, "app": map[string]any{"id": c.AppID}, "output": map[string]any{"annotations_count": c.Annotations}})
			}
		}
		f.jsonLocked(w, http.StatusOK, map[string]any{"total_count": len(runs), "check_runs": runs})
	})
	mux.HandleFunc("GET /repos/{o}/{r}/issues/{n}/comments", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.jsonLocked(w, http.StatusOK, f.comments)
	})
	mux.HandleFunc("POST /repos/{o}/{r}/issues/{n}/comments", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode comment: %v", err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.nextID++
		f.comments = append(f.comments, github.IssueComment{ID: f.nextID, Body: body.Body, User: github.User{Login: botLogin}})
		f.jsonLocked(w, http.StatusCreated, map[string]any{"id": f.nextID})
	})
	mux.HandleFunc("PATCH /repos/{o}/{r}/issues/comments/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			f.json(w, http.StatusBadRequest, map[string]string{"message": "bad id"})
			return
		}
		var body struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode comment: %v", err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := range f.comments {
			if f.comments[i].ID == id {
				f.comments[i].Body = body.Body
				f.jsonLocked(w, http.StatusOK, f.comments[i])
				return
			}
		}
		f.jsonLocked(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	mux.HandleFunc("GET /repos/{o}/{r}/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.jsonLocked(w, http.StatusOK, map[string]any{"total_count": len(f.runs), "workflow_runs": f.runs})
	})
	mux.HandleFunc("GET /repos/{o}/{r}/actions/runs/{id}/jobs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.jsonLocked(w, http.StatusOK, map[string]any{"total_count": len(f.jobs), "jobs": f.jobs})
	})
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		key := r.Method + " " + r.URL.Path
		f.mu.Lock()
		f.log = append(f.log, loggedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(data)})
		limited, broken, slow := f.limited[key], f.broken[key], f.slow[key]
		f.mu.Unlock()
		if slow {
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			return
		}
		if limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			f.json(w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded"})
			return
		}
		if broken {
			f.json(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) json(w http.ResponseWriter, status int, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jsonLocked(w, status, v)
}

func (f *fakeGitHub) jsonLocked(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		f.t.Errorf("encode response: %v", err)
	}
}

func (f *fakeGitHub) requests() []loggedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []loggedRequest
	for _, r := range f.log {
		if strings.HasSuffix(r.Path, "/access_tokens") {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (f *fakeGitHub) endpoints() []string {
	var out []string
	for _, r := range f.requests() {
		out = append(out, r.String())
	}
	return out
}

func (f *fakeGitHub) find(method, suffix string) []loggedRequest {
	var out []loggedRequest
	for _, r := range f.requests() {
		if r.Method == method && strings.HasSuffix(r.Path, suffix) {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeGitHub) setContent(ref, path, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contents[ref+":"+path] = content
}

func (f *fakeGitHub) resetLog() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = nil
}

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("QUANTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUANTO_TEST_DATABASE_URL is not set")
	}
	return dsn
}

type harness struct {
	t       *testing.T
	app     *App
	store   *store.Store
	db      *pgxpool.Pool
	gh      *fakeGitHub
	metrics *metrics.Metrics
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()
	dsn := testDSN(t)
	ctx := context.Background()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	schema := "quanto_app_test_" + hex.EncodeToString(buf)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		if err := admin.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	st, err := store.OpenConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(db.Close)
	gh := newFakeGitHub(t)
	m, err := metrics.New()
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	client, err := github.NewAppClient(github.Options{BaseURL: gh.server.URL, AppID: 1, PrivateKey: testKeyPEM(t), Version: "test", OnResponse: m.ObserveGitHubResponse})
	if err != nil {
		t.Fatalf("app client: %v", err)
	}
	opts := Options{Store: st, GitHub: client, Metrics: m, WebhookSecret: []byte(testSecret), MaxWorkflowFiles: 50, Concurrency: 2}
	if mutate != nil {
		mutate(&opts)
	}
	a, err := New(opts)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return &harness{t: t, app: a, store: st, db: db, gh: gh, metrics: m}
}

func (h *harness) seedRepo() {
	h.t.Helper()
	ctx := context.Background()
	if err := h.store.UpsertInstallation(ctx, store.Installation{ID: 7, AccountLogin: testOwner, AccountType: "Organization"}); err != nil {
		h.t.Fatalf("upsert installation: %v", err)
	}
	if err := h.store.UpsertRepositories(ctx, 7, []store.Repository{{ID: 42, Owner: testOwner, Name: testRepo}}); err != nil {
		h.t.Fatalf("upsert repo: %v", err)
	}
}

func (h *harness) enqueue(kind string, payload any) {
	h.t.Helper()
	if _, err := h.store.Enqueue(context.Background(), kind, payload, ""); err != nil {
		h.t.Fatalf("enqueue: %v", err)
	}
}

func (h *harness) runOne() {
	h.t.Helper()
	processed, err := h.app.ProcessNext(context.Background())
	if err != nil {
		h.t.Fatalf("process: %v", err)
	}
	if !processed {
		h.t.Fatal("no job processed")
	}
}

type queueRow struct {
	Kind      string
	Payload   string
	DedupeKey string
	Status    string
	Attempts  int
	LastError string
	Delay     float64
}

func (h *harness) queue() []queueRow {
	h.t.Helper()
	rows, err := h.db.Query(context.Background(), `SELECT kind, payload::text, coalesce(dedupe_key, ''), status, attempts, coalesce(last_error, ''),
EXTRACT(EPOCH FROM run_after - now())::float8 FROM queue_jobs ORDER BY id`)
	if err != nil {
		h.t.Fatalf("query queue: %v", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (queueRow, error) {
		var q queueRow
		err := row.Scan(&q.Kind, &q.Payload, &q.DedupeKey, &q.Status, &q.Attempts, &q.LastError, &q.Delay)
		return q, err
	})
	if err != nil {
		h.t.Fatalf("collect queue: %v", err)
	}
	return out
}

func (h *harness) count(sql string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		h.t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

func analyzePayload() AnalyzePayload {
	return AnalyzePayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo, Number: 3, HeadSHA: testHead, BaseSHA: testBase}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func webhookRequest(event, delivery string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign(body))
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	return req
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, wantBody string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, want, rec.Body.String())
	}
	if wantBody != "" && strings.TrimSpace(rec.Body.String()) != wantBody {
		t.Fatalf("body = %q, want %q", rec.Body.String(), wantBody)
	}
}
