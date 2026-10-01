package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BETAER-08/quanto/core/report"
)

const (
	testAppID      = 4242
	testToken      = "ghs_installationtokenvalue0123456789"
	testInstallID  = 77
	testOwner      = "octo"
	testRepo       = "hello"
	testVersion    = "1.2.3"
	tokenEndpoint  = "/app/installations/77/access_tokens"
	defaultExpires = time.Hour
)

var (
	keyOnce sync.Once
	rsaKey  *rsa.PrivateKey
	keyErr  error
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	keyOnce.Do(func() {
		rsaKey, keyErr = rsa.GenerateKey(rand.Reader, 2048)
	})
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	return rsaKey
}

func pkcs1PEM(t *testing.T) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey(t))})
}

func pkcs8PEM(t *testing.T) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

type recorded struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   string
}

type fakeGitHub struct {
	t          *testing.T
	mu         sync.Mutex
	requests   []recorded
	tokenCalls atomic.Int64
	clock      *fakeClock
	expiresIn  time.Duration
	handlers   map[string]http.HandlerFunc
	server     *httptest.Server
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newFake(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{
		t:         t,
		clock:     &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		expiresIn: defaultExpires,
		handlers:  map[string]http.HandlerFunc{},
	}
	f.handle("POST "+tokenEndpoint, func(w http.ResponseWriter, r *http.Request) {
		n := f.tokenCalls.Add(1)
		time.Sleep(5 * time.Millisecond)
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"token":      testToken + strconv.FormatInt(n, 10),
			"expires_at": f.clock.Now().Add(f.expiresIn).Format(time.RFC3339),
		})
	})
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
	f.requests = append(f.requests, recorded{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: string(body)})
	h, ok := f.handlers[r.Method+" "+r.URL.EscapedPath()]
	f.mu.Unlock()
	if !ok {
		writeJSON(f.t, w, http.StatusNotFound, map[string]string{"message": "Not Found", "documentation_url": "https://docs.github.com/rest"})
		return
	}
	h(w, r)
}

func (f *fakeGitHub) recorded(method, path string) []recorded {
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

func (f *fakeGitHub) appClient(t *testing.T, onResponse func(int, int)) *AppClient {
	t.Helper()
	c, err := NewAppClient(Options{
		BaseURL:    f.server.URL,
		AppID:      testAppID,
		PrivateKey: pkcs1PEM(t),
		Version:    testVersion,
		Now:        f.clock.Now,
		OnResponse: onResponse,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *fakeGitHub) client(t *testing.T) *Client {
	t.Helper()
	c, err := f.appClient(t, nil).Installation(context.Background(), testInstallID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		t.Errorf("encode response: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeRaw(t, w, string(data))
}

func writeRaw(t *testing.T, w http.ResponseWriter, body string) {
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func verifyJWT(t *testing.T, token string, pub *rsa.PublicKey) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts", len(parts))
	}
	for _, p := range parts {
		if strings.ContainsAny(p, "=+/") {
			t.Fatalf("jwt part is not unpadded base64url: %q", p)
		}
	}
	enc := base64.RawURLEncoding
	headerJSON, err := enc.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(headerJSON) != `{"alg":"RS256","typ":"JWT"}` {
		t.Errorf("header = %s", headerJSON)
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
	claimsJSON, err := enc.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestParsePrivateKey(t *testing.T) {
	tests := []struct {
		name string
		pem  []byte
		ok   bool
	}{
		{"pkcs1", pkcs1PEM(t), true},
		{"pkcs8", pkcs8PEM(t), true},
		{"garbage", []byte("not a key"), false},
		{"unknown type", pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte{1, 2, 3}}), false},
		{"broken pkcs1", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte{1, 2, 3}}), false},
		{"broken pkcs8", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1, 2, 3}}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ParsePrivateKey(tt.pem)
			if tt.ok {
				if err != nil {
					t.Fatal(err)
				}
				if !key.Equal(testKey(t)) {
					t.Error("parsed key differs")
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestJWTSignature(t *testing.T) {
	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	for name, pemData := range map[string][]byte{"pkcs1": pkcs1PEM(t), "pkcs8": pkcs8PEM(t)} {
		t.Run(name, func(t *testing.T) {
			key, err := ParsePrivateKey(pemData)
			if err != nil {
				t.Fatal(err)
			}
			token, err := signJWT(key, testAppID, now)
			if err != nil {
				t.Fatal(err)
			}
			claims := verifyJWT(t, token, &testKey(t).PublicKey)
			if claims["iss"] != "4242" {
				t.Errorf("iss = %v", claims["iss"])
			}
			if int64(claims["iat"].(float64)) != now.Unix()-60 {
				t.Errorf("iat = %v", claims["iat"])
			}
			if int64(claims["exp"].(float64)) != now.Unix()+540 {
				t.Errorf("exp = %v", claims["exp"])
			}
		})
	}
}

func TestJWTTamperDetected(t *testing.T) {
	token, err := signJWT(testKey(t), testAppID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"iat":1,"exp":2,"iss":"9"}`))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + forged))
	if rsa.VerifyPKCS1v15(&testKey(t).PublicKey, crypto.SHA256, digest[:], sig) == nil {
		t.Fatal("forged claims verified")
	}
}

func TestAppAndHeaders(t *testing.T) {
	f := newFake(t)
	f.handle("GET /app", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"id": testAppID, "slug": "quanto-test", "name": "Quanto Test"})
	})
	app, err := f.appClient(t, nil).App(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != testAppID || app.Slug != "quanto-test" || app.Name != "Quanto Test" {
		t.Errorf("app = %+v", app)
	}
	req := f.recorded("GET", "/app")[0]
	if got := req.Header.Get("Accept"); got != "application/vnd.github+json" {
		t.Errorf("Accept = %q", got)
	}
	if got := req.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "quanto/1.2.3" {
		t.Errorf("User-Agent = %q", got)
	}
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Fatalf("Authorization = %q", auth)
	}
	claims := verifyJWT(t, strings.TrimPrefix(auth, "Bearer "), &testKey(t).PublicKey)
	if int64(claims["iat"].(float64)) != f.clock.Now().Unix()-60 {
		t.Errorf("jwt does not use injected clock: %v", claims["iat"])
	}
}

func TestInstallationTokenUsesJWTAndToken(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/5", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"number": 5, "state": "open", "head": map[string]string{"sha": "h", "ref": "feature"}, "base": map[string]string{"sha": "b", "ref": "main"}})
	})
	c := f.client(t)
	pr, err := c.PullRequest(context.Background(), testOwner, testRepo, 5)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 5 || pr.Head.SHA != "h" || pr.Base.SHA != "b" || pr.Head.Ref != "feature" || pr.State != "open" {
		t.Errorf("pr = %+v", pr)
	}
	tokenReq := f.recorded("POST", tokenEndpoint)
	if len(tokenReq) != 1 {
		t.Fatalf("token requests = %d", len(tokenReq))
	}
	verifyJWT(t, strings.TrimPrefix(tokenReq[0].Header.Get("Authorization"), "Bearer "), &testKey(t).PublicKey)
	prReq := f.recorded("GET", "/repos/octo/hello/pulls/5")[0]
	if got := prReq.Header.Get("Authorization"); got != "token "+testToken+"1" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestTokenCacheReuseAndRefresh(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"number": 1})
	})
	c := f.client(t)
	ctx := context.Background()
	call := func() string {
		t.Helper()
		if _, err := c.PullRequest(ctx, testOwner, testRepo, 1); err != nil {
			t.Fatal(err)
		}
		reqs := f.recorded("GET", "/repos/octo/hello/pulls/1")
		return reqs[len(reqs)-1].Header.Get("Authorization")
	}
	if got := call(); got != "token "+testToken+"1" {
		t.Fatalf("first auth = %q", got)
	}
	f.clock.Advance(54*time.Minute + 59*time.Second)
	if got := call(); got != "token "+testToken+"1" {
		t.Fatalf("token refreshed too early: %q", got)
	}
	if n := f.tokenCalls.Load(); n != 1 {
		t.Fatalf("token calls = %d, want 1", n)
	}
	f.clock.Advance(time.Second)
	if got := call(); got != "token "+testToken+"2" {
		t.Fatalf("token not refreshed 5 minutes before expiry: %q", got)
	}
	if n := f.tokenCalls.Load(); n != 2 {
		t.Fatalf("token calls = %d, want 2", n)
	}
}

func TestTokenCacheConcurrent(t *testing.T) {
	f := newFake(t)
	app := f.appClient(t, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	tokens := make(chan string, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := app.installationToken(context.Background(), testInstallID)
			if err != nil {
				errs <- err
				return
			}
			tokens <- tok
		}()
	}
	wg.Wait()
	close(errs)
	close(tokens)
	for err := range errs {
		t.Fatal(err)
	}
	for tok := range tokens {
		if tok != testToken+"1" {
			t.Errorf("token = %q", tok)
		}
	}
	if n := f.tokenCalls.Load(); n != 1 {
		t.Fatalf("token calls = %d, want 1", n)
	}
}

func TestTokenCachePerInstallation(t *testing.T) {
	f := newFake(t)
	var other atomic.Int64
	f.handle("POST /app/installations/88/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		other.Add(1)
		writeJSON(t, w, http.StatusCreated, map[string]any{"token": "other", "expires_at": f.clock.Now().Add(time.Hour).Format(time.RFC3339)})
	})
	app := f.appClient(t, nil)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := app.installationToken(ctx, testInstallID); err != nil {
			t.Fatal(err)
		}
		tok, err := app.installationToken(ctx, 88)
		if err != nil {
			t.Fatal(err)
		}
		if tok != "other" {
			t.Errorf("token = %q", tok)
		}
	}
	if f.tokenCalls.Load() != 1 || other.Load() != 1 {
		t.Errorf("token calls = %d/%d", f.tokenCalls.Load(), other.Load())
	}
}

func cachedInstallations(c *AppClient) []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var ids []int64
	for id := range c.tokens {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func TestTokenCacheEvictsExpiredOnRefresh(t *testing.T) {
	f := newFake(t)
	f.handle("POST /app/installations/88/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, map[string]any{"token": "other", "expires_at": f.clock.Now().Add(time.Hour).Format(time.RFC3339)})
	})
	app := f.appClient(t, nil)
	ctx := context.Background()
	if _, err := app.installationToken(ctx, testInstallID); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(cachedInstallations(app)); got != "[77]" {
		t.Fatalf("cache = %s", got)
	}
	f.clock.Advance(30 * time.Minute)
	if _, err := app.installationToken(ctx, 88); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(cachedInstallations(app)); got != "[77 88]" {
		t.Fatalf("live token evicted: %s", got)
	}
	f.clock.Advance(31 * time.Minute)
	f.handle("POST /app/installations/99/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, map[string]any{"token": "third", "expires_at": f.clock.Now().Add(time.Hour).Format(time.RFC3339)})
	})
	if _, err := app.installationToken(ctx, 99); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(cachedInstallations(app)); got != "[88 99]" {
		t.Fatalf("expired token kept: %s", got)
	}
	tok, err := app.installationToken(ctx, testInstallID)
	if err != nil || tok != testToken+"2" {
		t.Fatalf("token after eviction = %q, %v", tok, err)
	}
}

func TestPruneTokens(t *testing.T) {
	f := newFake(t)
	app := f.appClient(t, nil)
	ctx := context.Background()
	if _, err := app.installationToken(ctx, testInstallID); err != nil {
		t.Fatal(err)
	}
	f.handle("POST /app/installations/55/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]any{"message": "boom"})
	})
	if _, err := app.installationToken(ctx, 55); err == nil {
		t.Fatal("expected token error")
	}
	if n := app.PruneTokens(); n != 1 {
		t.Fatalf("pruned = %d, want failed entry only", n)
	}
	if got := fmt.Sprint(cachedInstallations(app)); got != "[77]" {
		t.Fatalf("cache = %s", got)
	}
	f.clock.Advance(time.Hour)
	if n := app.PruneTokens(); n != 1 {
		t.Fatalf("pruned = %d, want expired entry", n)
	}
	if got := len(cachedInstallations(app)); got != 0 {
		t.Fatalf("cache size = %d", got)
	}
	locked := app.entry(testInstallID)
	locked.mu.Lock()
	if n := app.PruneTokens(); n != 0 {
		t.Fatalf("pruned locked entry")
	}
	locked.mu.Unlock()
}

func TestTokenCacheConcurrentWithPrune(t *testing.T) {
	f := newFake(t)
	app := f.appClient(t, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := app.installationToken(context.Background(), testInstallID); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			app.PruneTokens()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n := f.tokenCalls.Load(); n < 1 || n > 100 {
		t.Fatalf("token calls = %d", n)
	}
}

func TestPullRequestFilesPagination(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/9/files", func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			page = 1
		}
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %q", r.URL.Query().Get("per_page"))
		}
		var files []map[string]string
		for i := 0; i < 100; i++ {
			files = append(files, map[string]string{"filename": fmt.Sprintf("f%d-%d.yml", page, i), "status": "modified"})
		}
		if page < 40 {
			next := fmt.Sprintf("%s/repos/octo/hello/pulls/9/files?per_page=100&page=%d", f.server.URL, page+1)
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next", <%s>; rel="last"`, next, next))
		}
		writeJSON(t, w, http.StatusOK, files)
	})
	files, err := f.client(t).PullRequestFiles(context.Background(), testOwner, testRepo, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3000 {
		t.Fatalf("files = %d, want 3000", len(files))
	}
	if files[0].Filename != "f1-0.yml" || files[2999].Filename != "f30-99.yml" {
		t.Errorf("unexpected boundary files %q %q", files[0].Filename, files[2999].Filename)
	}
	if n := len(f.recorded("GET", "/repos/octo/hello/pulls/9/files")); n != 30 {
		t.Errorf("page requests = %d, want 30", n)
	}
}

func TestPullRequestFilesFields(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/2/files", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, []map[string]string{
			{"filename": ".github/workflows/new.yml", "previous_filename": ".github/workflows/old.yml", "status": "renamed"},
		})
	})
	files, err := f.client(t).PullRequestFiles(context.Background(), testOwner, testRepo, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := PullRequestFile{Filename: ".github/workflows/new.yml", PreviousFilename: ".github/workflows/old.yml", Status: "renamed"}
	if len(files) != 1 || files[0] != want {
		t.Errorf("files = %+v", files)
	}
}

func TestPaginationRejectsForeignHost(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://evil.example/steal?page=2>; rel="next"`)
		writeJSON(t, w, http.StatusOK, []map[string]any{{"id": 1, "body": "x"}})
	})
	_, err := f.client(t).IssueComments(context.Background(), testOwner, testRepo, 3)
	if err == nil || !strings.Contains(err.Error(), "different host") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseNextLink(t *testing.T) {
	tests := []struct {
		header string
		want   string
		ok     bool
	}{
		{`<https://a/x?page=2>; rel="next", <https://a/x?page=5>; rel="last"`, "https://a/x?page=2", true},
		{`<https://a/x?page=1>; rel="prev", <https://a/x?page=3>; rel="next"`, "https://a/x?page=3", true},
		{`<https://a/x?page=5>; rel="last"`, "", false},
		{`<https://a/x?page=2>; rel=next`, "https://a/x?page=2", true},
		{`garbage`, "", false},
		{``, "", false},
	}
	for _, tt := range tests {
		got, ok := parseNextLink(tt.header)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseNextLink(%q) = %q, %v", tt.header, got, ok)
		}
	}
}

func TestFileContent(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/contents/.github/workflows/ci.yml", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "abc123" {
			t.Errorf("ref = %q", r.URL.Query().Get("ref"))
		}
		if r.Header.Get("Accept") != "application/vnd.github.raw+json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		writeRaw(t, w, "name: ci\n")
	})
	data, ok, err := f.client(t).FileContent(context.Background(), testOwner, testRepo, ".github/workflows/ci.yml", "abc123")
	if err != nil || !ok || string(data) != "name: ci\n" {
		t.Fatalf("FileContent = %q, %v, %v", data, ok, err)
	}
}

func TestFileContentEscaping(t *testing.T) {
	f := newFake(t)
	path := ".github/workflows/my flow #1 한글.yml"
	escaped := "/repos/octo/hello/contents/.github/workflows/my%20flow%20%231%20%ED%95%9C%EA%B8%80.yml"
	f.handle("GET "+escaped, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octo/hello/contents/"+path {
			t.Errorf("decoded path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("ref") != "feature/a b#c" {
			t.Errorf("ref = %q", r.URL.Query().Get("ref"))
		}
		writeRaw(t, w, "on: push\n")
	})
	data, ok, err := f.client(t).FileContent(context.Background(), testOwner, testRepo, path, "feature/a b#c")
	if err != nil || !ok || string(data) != "on: push\n" {
		t.Fatalf("FileContent = %q, %v, %v", data, ok, err)
	}
	if n := len(f.recorded("GET", escaped)); n != 1 {
		t.Errorf("escaped requests = %d", n)
	}
}

func TestFileContentSizeLimit(t *testing.T) {
	tests := []struct {
		name string
		size int
		ok   bool
	}{
		{"exactly 256 KiB", MaxFileSize, true},
		{"one byte over", MaxFileSize + 1, false},
		{"far over", 16 * MaxFileSize, false},
	}
	for _, tt := range tests {
		f := newFake(t)
		f.handle("GET /repos/octo/hello/contents/.github/workflows/big.yml", func(w http.ResponseWriter, r *http.Request) {
			chunk := []byte(strings.Repeat("a", 64<<10))
			for left := tt.size; left > 0; {
				n := min(left, len(chunk))
				if _, err := w.Write(chunk[:n]); err != nil {
					return
				}
				left -= n
			}
		})
		data, ok, err := f.client(t).FileContent(context.Background(), testOwner, testRepo, ".github/workflows/big.yml", "abc")
		if tt.ok {
			if err != nil || !ok || len(data) != tt.size {
				t.Errorf("%s: FileContent = %d bytes, %v, %v", tt.name, len(data), ok, err)
			}
			continue
		}
		if !errors.Is(err, ErrFileTooLarge) || data != nil {
			t.Errorf("%s: FileContent = %d bytes, %v, %v", tt.name, len(data), ok, err)
		}
		if err != nil && err.Error() != "file exceeds 256 KiB" {
			t.Errorf("%s: error = %q", tt.name, err)
		}
	}
}

func TestFileContentNotFound(t *testing.T) {
	f := newFake(t)
	data, ok, err := f.client(t).FileContent(context.Background(), testOwner, testRepo, ".github/workflows/gone.yml", "abc")
	if err != nil || ok || data != nil {
		t.Fatalf("FileContent = %q, %v, %v", data, ok, err)
	}
}

func TestAPIError(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity, map[string]string{"message": "Validation Failed", "documentation_url": "https://docs.github.com/x"})
	})
	f.handle("GET /repos/octo/hello/pulls/2", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeRaw(t, w, "<html>oops</html>")
	})
	f.handle("GET /repos/octo/hello/pulls/3", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
	})
	c := f.client(t)
	tests := []struct {
		number  int
		status  int
		message string
		doc     string
	}{
		{1, 422, "Validation Failed", "https://docs.github.com/x"},
		{2, 500, "Internal Server Error", ""},
		{3, 403, "Resource not accessible by integration", ""},
	}
	for _, tt := range tests {
		_, err := c.PullRequest(context.Background(), testOwner, testRepo, tt.number)
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("%d: err = %v", tt.number, err)
		}
		if apiErr.Status != tt.status || apiErr.Message != tt.message || apiErr.DocumentationURL != tt.doc {
			t.Errorf("%d: %+v", tt.number, apiErr)
		}
	}
}

func TestRateLimitRetryAfter(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.Header().Set("X-RateLimit-Reset", "1")
		writeJSON(t, w, http.StatusTooManyRequests, map[string]string{"message": "slow down"})
	})
	f.handle("GET /repos/octo/hello/pulls/2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "secondary rate limit"})
	})
	c := f.client(t)
	for number, wantDelay := range map[int]time.Duration{1: 120 * time.Second, 2: 30 * time.Second} {
		_, err := c.PullRequest(context.Background(), testOwner, testRepo, number)
		var rl *RateLimitError
		if !errors.As(err, &rl) {
			t.Fatalf("%d: err = %v", number, err)
		}
		if want := f.clock.Now().Add(wantDelay); !rl.Reset.Equal(want) {
			t.Errorf("%d: reset = %v, want %v", number, rl.Reset, want)
		}
	}
}

func TestRateLimitReset(t *testing.T) {
	f := newFake(t)
	reset := f.clock.Now().Add(17 * time.Minute).Unix()
	f.handle("GET /repos/octo/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded"})
	})
	_, err := f.client(t).PullRequest(context.Background(), testOwner, testRepo, 1)
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v", err)
	}
	if rl.Reset.Unix() != reset {
		t.Errorf("reset = %v, want %v", rl.Reset.Unix(), reset)
	}
}

func TestForbiddenWithRemainingIsNotRateLimit(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "10")
		writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	})
	_, err := f.client(t).PullRequest(context.Background(), testOwner, testRepo, 1)
	var rl *RateLimitError
	if errors.As(err, &rl) {
		t.Fatalf("unexpected rate limit error: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("err = %v", err)
	}
}

func TestOnResponse(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4321")
		writeJSON(t, w, http.StatusOK, map[string]any{"number": 1})
	})
	var mu sync.Mutex
	var seen [][2]int
	app := f.appClient(t, func(status, remaining int) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, [2]int{status, remaining})
	})
	c, err := app.Installation(context.Background(), testInstallID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PullRequest(context.Background(), testOwner, testRepo, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PullRequest(context.Background(), testOwner, testRepo, 404); err == nil {
		t.Fatal("expected error")
	}
	want := [][2]int{{201, -1}, {200, 4321}, {404, -1}}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Errorf("seen = %v, want %v", seen, want)
	}
}

func makeAnnotations(n int) []report.Annotation {
	out := make([]report.Annotation, n)
	for i := range out {
		out[i] = report.Annotation{Path: ".github/workflows/ci.yml", StartLine: i + 1, EndLine: i + 1, StartColumn: 3, EndColumn: 9, Level: "notice", Title: "job.added", Message: fmt.Sprintf("m%d", i)}
	}
	return out
}

type sentCheckRun struct {
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Output     struct {
		Title       string           `json:"title"`
		Summary     string           `json:"summary"`
		Annotations []map[string]any `json:"annotations"`
	} `json:"output"`
}

func decodeCheckRun(t *testing.T, body string) sentCheckRun {
	t.Helper()
	var s sentCheckRun
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCheckRunAnnotationBatches(t *testing.T) {
	f := newFake(t)
	f.handle("POST /repos/octo/hello/check-runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, map[string]any{"id": 555})
	})
	f.handle("PATCH /repos/octo/hello/check-runs/555", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 555})
	})
	run := CheckRun{HeadSHA: "deadbeef", Title: "3 execution changes", Summary: "summary", Annotations: makeAnnotations(120)}
	id, err := f.client(t).CreateCheckRun(context.Background(), testOwner, testRepo, run)
	if err != nil {
		t.Fatal(err)
	}
	if id != 555 {
		t.Errorf("id = %d", id)
	}
	creates := f.recorded("POST", "/repos/octo/hello/check-runs")
	updates := f.recorded("PATCH", "/repos/octo/hello/check-runs/555")
	if len(creates) != 1 || len(updates) != 2 {
		t.Fatalf("creates = %d, updates = %d", len(creates), len(updates))
	}
	created := decodeCheckRun(t, creates[0].Body)
	if created.Name != "quanto" || created.HeadSHA != "deadbeef" || created.Status != "completed" || created.Conclusion != "neutral" {
		t.Errorf("create body = %+v", created)
	}
	if created.Output.Title != run.Title || created.Output.Summary != run.Summary {
		t.Errorf("create output = %+v", created.Output)
	}
	sizes := []int{len(created.Output.Annotations)}
	next := 0
	for _, a := range created.Output.Annotations {
		if a["message"] != fmt.Sprintf("m%d", next) {
			t.Errorf("annotation order broken at %d: %v", next, a["message"])
		}
		next++
	}
	for _, u := range updates {
		body := decodeCheckRun(t, u.Body)
		if body.Output.Title != run.Title || body.Output.Summary != run.Summary || body.Conclusion != "neutral" {
			t.Errorf("update body = %+v", body)
		}
		sizes = append(sizes, len(body.Output.Annotations))
		for _, a := range body.Output.Annotations {
			if a["message"] != fmt.Sprintf("m%d", next) {
				t.Errorf("annotation order broken at %d: %v", next, a["message"])
			}
			next++
		}
	}
	if fmt.Sprint(sizes) != "[50 50 20]" {
		t.Errorf("batch sizes = %v", sizes)
	}
	first := created.Output.Annotations[0]
	if first["annotation_level"] != "notice" || first["path"] != ".github/workflows/ci.yml" || first["start_column"] != float64(3) || first["title"] != "job.added" {
		t.Errorf("annotation = %v", first)
	}
}

func TestCheckRunMultiLineOmitsColumns(t *testing.T) {
	f := newFake(t)
	f.handle("POST /repos/octo/hello/check-runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, map[string]any{"id": 1})
	})
	run := CheckRun{HeadSHA: "x", Title: "t", Summary: "s", Annotations: []report.Annotation{
		{Path: "a.yml", StartLine: 2, EndLine: 5, StartColumn: 4, EndColumn: 1, Level: "notice", Title: "k", Message: "m"},
	}}
	if _, err := f.client(t).CreateCheckRun(context.Background(), testOwner, testRepo, run); err != nil {
		t.Fatal(err)
	}
	a := decodeCheckRun(t, f.recorded("POST", "/repos/octo/hello/check-runs")[0].Body).Output.Annotations[0]
	if _, ok := a["start_column"]; ok {
		t.Errorf("multi-line annotation has columns: %v", a)
	}
}

func TestCheckRunWithoutAnnotations(t *testing.T) {
	f := newFake(t)
	f.handle("POST /repos/octo/hello/check-runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, map[string]any{"id": 9})
	})
	if _, err := f.client(t).CreateCheckRun(context.Background(), testOwner, testRepo, CheckRun{HeadSHA: "x", Title: "No execution changes", Summary: "s"}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.recorded("POST", "/repos/octo/hello/check-runs")); n != 1 {
		t.Errorf("creates = %d", n)
	}
	if n := len(f.recorded("PATCH", "/repos/octo/hello/check-runs/9")); n != 0 {
		t.Errorf("updates = %d", n)
	}
}

func TestUpdateCheckRunBatches(t *testing.T) {
	tests := []struct {
		name    string
		present int
		sizes   string
		first   string
	}{
		{"fresh", 0, "[50 1]", "m0"},
		{"resume after first batch", 50, "[1]", "m50"},
		{"resume mid batch", 20, "[31]", "m20"},
		{"complete", 51, "[0]", ""},
		{"more than expected", 70, "[0]", ""},
	}
	for _, tt := range tests {
		f := newFake(t)
		f.handle("GET /repos/octo/hello/check-runs/7", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 7, "app": map[string]any{"id": testAppID}, "output": map[string]any{"annotations_count": tt.present}})
		})
		f.handle("PATCH /repos/octo/hello/check-runs/7", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 7})
		})
		run := CheckRun{Title: "t", Summary: "s", Annotations: makeAnnotations(51)}
		if err := f.client(t).UpdateCheckRun(context.Background(), testOwner, testRepo, 7, run); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		var sizes []int
		first := ""
		for i, u := range f.recorded("PATCH", "/repos/octo/hello/check-runs/7") {
			body := decodeCheckRun(t, u.Body)
			if body.Output.Title != "t" || body.Output.Summary != "s" || body.Conclusion != "neutral" || body.Status != "completed" {
				t.Errorf("%s: update body = %+v", tt.name, body)
			}
			sizes = append(sizes, len(body.Output.Annotations))
			if i == 0 && len(body.Output.Annotations) > 0 {
				first, _ = body.Output.Annotations[0]["message"].(string)
			}
		}
		if fmt.Sprint(sizes) != tt.sizes || first != tt.first {
			t.Errorf("%s: sizes = %v first = %q, want %s %q", tt.name, sizes, first, tt.sizes, tt.first)
		}
	}
}

func TestMergeBase(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/compare/aaa...bbb", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "1" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"status": "diverged", "base_commit": map[string]any{"sha": "aaa"}, "merge_base_commit": map[string]any{"sha": "mmm"}})
	})
	got, err := f.client(t).MergeBase(context.Background(), testOwner, testRepo, "aaa", "bbb")
	if err != nil || got != "mmm" {
		t.Fatalf("MergeBase = %q, %v", got, err)
	}
	f.handle("GET /repos/octo/hello/compare/aaa...empty", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"merge_base_commit": map[string]any{}})
	})
	if got, err := f.client(t).MergeBase(context.Background(), testOwner, testRepo, "aaa", "empty"); err == nil || got != "" {
		t.Fatalf("MergeBase(empty) = %q, %v", got, err)
	}
	var apiErr *APIError
	if _, err := f.client(t).MergeBase(context.Background(), testOwner, testRepo, "aaa", "missing"); !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("MergeBase(missing) = %v", err)
	}
}

func TestFindCheckRun(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/commits/abc123/check-runs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("check_name") != "quanto" || q.Get("per_page") != "100" {
			t.Errorf("query = %v", q)
		}
		if q.Get("page") == "2" {
			writeJSON(t, w, http.StatusOK, map[string]any{"total_count": 3, "check_runs": []map[string]any{
				{"id": 31, "app": map[string]any{"id": testAppID}},
				{"id": 32, "app": map[string]any{"id": testAppID}},
			}})
			return
		}
		w.Header().Set("Link", "<"+f.server.URL+"/repos/octo/hello/commits/abc123/check-runs?check_name=quanto&per_page=100&page=2>; rel=\"next\"")
		writeJSON(t, w, http.StatusOK, map[string]any{"total_count": 3, "check_runs": []map[string]any{
			{"id": 30, "app": map[string]any{"id": 9999}},
		}})
	})
	id, ok, err := f.client(t).FindCheckRun(context.Background(), testOwner, testRepo, "abc123", CheckRunName)
	if err != nil || !ok || id != 31 {
		t.Fatalf("FindCheckRun = %d, %v, %v", id, ok, err)
	}
	f.handle("GET /repos/octo/hello/commits/none/check-runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"total_count": 1, "check_runs": []map[string]any{{"id": 40, "app": map[string]any{"id": 9999}}}})
	})
	id, ok, err = f.client(t).FindCheckRun(context.Background(), testOwner, testRepo, "none", CheckRunName)
	if err != nil || ok || id != 0 {
		t.Fatalf("FindCheckRun(other app) = %d, %v, %v", id, ok, err)
	}
	f.handle("GET /repos/octo/hello/commits/bad/check-runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]any{"message": "boom"})
	})
	var apiErr *APIError
	if _, _, err := f.client(t).FindCheckRun(context.Background(), testOwner, testRepo, "bad", CheckRunName); !errors.As(err, &apiErr) {
		t.Fatalf("FindCheckRun error = %v", err)
	}
}

func TestIssueComments(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/issues/4/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(t, w, http.StatusOK, []map[string]any{{"id": 3, "body": "third", "user": map[string]string{"login": "quanto[bot]"}}})
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/octo/hello/issues/4/comments?per_page=100&page=2>; rel="next"`, f.server.URL))
		writeJSON(t, w, http.StatusOK, []map[string]any{
			{"id": 1, "body": "first", "user": map[string]string{"login": "alice"}},
			{"id": 2, "body": "second", "user": map[string]string{"login": "bob"}},
		})
	})
	comments, err := f.client(t).IssueComments(context.Background(), testOwner, testRepo, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 3 || comments[2].ID != 3 || comments[2].User.Login != "quanto[bot]" || comments[0].Body != "first" {
		t.Errorf("comments = %+v", comments)
	}
}

func TestCreateAndUpdateIssueComment(t *testing.T) {
	f := newFake(t)
	f.handle("POST /repos/octo/hello/issues/4/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, map[string]any{"id": 99})
	})
	f.handle("PATCH /repos/octo/hello/issues/comments/99", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 99})
	})
	c := f.client(t)
	id, err := c.CreateIssueComment(context.Background(), testOwner, testRepo, 4, "hello")
	if err != nil || id != 99 {
		t.Fatalf("create = %d, %v", id, err)
	}
	if err := c.UpdateIssueComment(context.Background(), testOwner, testRepo, 99, "updated"); err != nil {
		t.Fatal(err)
	}
	if body := f.recorded("POST", "/repos/octo/hello/issues/4/comments")[0].Body; strings.TrimSpace(body) != `{"body":"hello"}` {
		t.Errorf("create body = %s", body)
	}
	if body := f.recorded("PATCH", "/repos/octo/hello/issues/comments/99")[0].Body; strings.TrimSpace(body) != `{"body":"updated"}` {
		t.Errorf("update body = %s", body)
	}
}

func TestWorkflowRuns(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("status") != "completed" || q.Get("per_page") != "100" {
			t.Errorf("query = %v", q)
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/octo/hello/actions/runs?page=2>; rel="next"`, f.server.URL))
		writeJSON(t, w, http.StatusOK, map[string]any{"total_count": 2, "workflow_runs": []map[string]any{
			{"id": 10, "name": "ci", "path": ".github/workflows/ci.yml", "head_sha": "a", "status": "completed", "conclusion": "success"},
			{"id": 9, "name": "ci", "path": ".github/workflows/ci.yml", "head_sha": "b", "status": "completed", "conclusion": "failure"},
		}})
	})
	runs, err := f.client(t).WorkflowRuns(context.Background(), testOwner, testRepo, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != 10 || runs[1].Conclusion != "failure" || runs[0].Path != ".github/workflows/ci.yml" {
		t.Errorf("runs = %+v", runs)
	}
	if n := len(f.recorded("GET", "/repos/octo/hello/actions/runs")); n != 1 {
		t.Errorf("requests = %d, want one page only", n)
	}
}

func TestRunJobs(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/actions/runs/10/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != "latest" {
			t.Errorf("filter = %q", r.URL.Query().Get("filter"))
		}
		if r.URL.Query().Get("page") == "2" {
			writeJSON(t, w, http.StatusOK, map[string]any{"jobs": []map[string]any{
				{"id": 3, "name": "pending", "conclusion": nil, "started_at": nil, "completed_at": nil, "labels": []string{}},
			}})
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/octo/hello/actions/runs/10/jobs?filter=latest&per_page=100&page=2>; rel="next"`, f.server.URL))
		writeJSON(t, w, http.StatusOK, map[string]any{"jobs": []map[string]any{
			{"id": 1, "name": "test (ubuntu-latest, 18)", "conclusion": "success", "started_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:05:30Z", "labels": []string{"ubuntu-latest"}},
			{"id": 2, "name": "lint", "conclusion": "failure", "started_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:01:00Z", "labels": []string{"self-hosted", "linux"}},
		}})
	})
	jobs, err := f.client(t).RunJobs(context.Background(), testOwner, testRepo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 {
		t.Fatalf("jobs = %d", len(jobs))
	}
	if d := jobs[0].CompletedAt.Sub(jobs[0].StartedAt); d != 330*time.Second {
		t.Errorf("duration = %v", d)
	}
	if jobs[1].Labels[1] != "linux" || jobs[0].Name != "test (ubuntu-latest, 18)" {
		t.Errorf("jobs = %+v", jobs)
	}
	if !jobs[2].StartedAt.IsZero() || !jobs[2].CompletedAt.IsZero() || jobs[2].Conclusion != "" {
		t.Errorf("null fields not zero: %+v", jobs[2])
	}
}

func TestContextCancellation(t *testing.T) {
	f := newFake(t)
	c := f.client(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.PullRequest(ctx, testOwner, testRepo, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientTimeout(t *testing.T) {
	f := newFake(t)
	app := f.appClient(t, nil)
	if app.t.http.Timeout != 30*time.Second {
		t.Errorf("timeout = %v", app.t.http.Timeout)
	}
}

func TestSecretsNotInErrors(t *testing.T) {
	f := newFake(t)
	f.handle("GET /repos/octo/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
	})
	f.handle("GET /repos/octo/hello/pulls/2", func(w http.ResponseWriter, r *http.Request) {
		writeRaw(t, w, "{not json")
	})
	f.handle("GET /repos/octo/hello/pulls/3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	})
	app := f.appClient(t, nil)
	c, err := app.Installation(context.Background(), testInstallID)
	if err != nil {
		t.Fatal(err)
	}
	var errs []error
	for _, n := range []int{1, 2, 3, 404} {
		_, err := c.PullRequest(context.Background(), testOwner, testRepo, n)
		if err == nil {
			t.Fatalf("%d: expected error", n)
		}
		errs = append(errs, err)
	}
	f.handle("POST "+tokenEndpoint, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
	})
	f.clock.Advance(2 * time.Hour)
	_, err = c.PullRequest(context.Background(), testOwner, testRepo, 1)
	if err == nil {
		t.Fatal("expected token error")
	}
	errs = append(errs, err)
	_, err = NewAppClient(Options{AppID: 1, PrivateKey: []byte("-----BEGIN RSA PRIVATE KEY-----\nU0VDUkVUS0VZTUFURVJJQUw=\n-----END RSA PRIVATE KEY-----\n")})
	if err == nil {
		t.Fatal("expected key error")
	}
	errs = append(errs, err)

	jwt, jwtErr := signJWT(testKey(t), testAppID, f.clock.Now())
	if jwtErr != nil {
		t.Fatal(jwtErr)
	}
	jwtHeader := strings.Split(jwt, ".")[0]
	keyPEM := string(pkcs1PEM(t))
	keyBody := strings.Split(keyPEM, "\n")[1]
	secrets := []string{testToken, jwtHeader, keyBody, "U0VDUkVUS0VZTUFURVJJQUw", "SECRETKEYMATERIAL"}
	f.mu.Lock()
	reqs := append([]recorded(nil), f.requests...)
	f.mu.Unlock()
	for _, r := range reqs {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			secrets = append(secrets, strings.TrimPrefix(auth, "Bearer "))
		}
	}
	for _, err := range errs {
		for _, s := range secrets {
			if strings.Contains(err.Error(), s) {
				t.Errorf("error %q leaks secret material", err)
			}
		}
	}
}

func TestNewAppClientValidation(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"no app id", Options{PrivateKey: pkcs1PEM(t)}},
		{"bad key", Options{AppID: 1, PrivateKey: []byte("x")}},
		{"bad base url", Options{AppID: 1, PrivateKey: pkcs1PEM(t), BaseURL: "ftp://x"}},
	}
	for _, tt := range tests {
		if _, err := NewAppClient(tt.opts); err == nil {
			t.Errorf("%s: expected error", tt.name)
		}
	}
	c, err := NewAppClient(Options{AppID: 1, PrivateKey: pkcs8PEM(t)})
	if err != nil {
		t.Fatal(err)
	}
	if c.t.base.String() != "https://api.github.com" || c.t.userAgent != "quanto/dev" {
		t.Errorf("defaults: %s %s", c.t.base, c.t.userAgent)
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	f := newFake(t)
	var hits atomic.Int64
	f.handle("POST /api/v3"+tokenEndpoint, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, map[string]any{"token": "t", "expires_at": f.clock.Now().Add(time.Hour).Format(time.RFC3339)})
	})
	f.handle("GET /api/v3/repos/octo/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeJSON(t, w, http.StatusOK, map[string]any{"number": 1})
	})
	app, err := NewAppClient(Options{BaseURL: f.server.URL + "/api/v3/", AppID: testAppID, PrivateKey: pkcs1PEM(t), Now: f.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	c, err := app.Installation(context.Background(), testInstallID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PullRequest(context.Background(), testOwner, testRepo, 1); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d", hits.Load())
	}
}
