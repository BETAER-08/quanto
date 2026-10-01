package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BETAER-08/quanto/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func unitApp(t *testing.T) (*App, *metrics.Metrics) {
	t.Helper()
	m, err := metrics.New()
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	a, err := New(Options{Metrics: m, WebhookSecret: []byte(testSecret)})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return a, m
}

func TestNewRequiresDependencies(t *testing.T) {
	m, err := metrics.New()
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if _, err := New(Options{WebhookSecret: []byte("x")}); err == nil {
		t.Error("missing metrics accepted")
	}
	if _, err := New(Options{Metrics: m}); err == nil {
		t.Error("missing secret accepted")
	}
}

func TestValidSignature(t *testing.T) {
	body := []byte(`{"zen":"ok"}`)
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{"valid", sign(body), true},
		{"empty", "", false},
		{"sha1 prefix", strings.Replace(sign(body), "sha256=", "sha1=", 1), false},
		{"not hex", "sha256=zz", false},
		{"wrong digest", sign([]byte("other")), false},
		{"uppercase hex", "sha256=" + strings.ToUpper(strings.TrimPrefix(sign(body), "sha256=")), true},
		{"truncated", sign(body)[:20], false},
	}
	for _, tt := range tests {
		if got := validSignature([]byte(testSecret), tt.header, body); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestWebhookRejections(t *testing.T) {
	body := []byte(`{"zen":"ok"}`)
	tests := []struct {
		name   string
		build  func() *http.Request
		status int
		reason string
	}{
		{"missing signature", func() *http.Request {
			req := webhookRequest("ping", "d1", body)
			req.Header.Del("X-Hub-Signature-256")
			return req
		}, http.StatusUnauthorized, metrics.RejectSignature},
		{"wrong signature", func() *http.Request {
			req := webhookRequest("ping", "d1", body)
			req.Header.Set("X-Hub-Signature-256", sign([]byte("tampered")))
			return req
		}, http.StatusUnauthorized, metrics.RejectSignature},
		{"signature checked before headers", func() *http.Request {
			req := webhookRequest("", "", body)
			req.Header.Set("X-Hub-Signature-256", "sha256=00")
			return req
		}, http.StatusUnauthorized, metrics.RejectSignature},
		{"missing event", func() *http.Request { return webhookRequest("", "d1", body) }, http.StatusBadRequest, metrics.RejectHeaders},
		{"missing delivery", func() *http.Request { return webhookRequest("ping", "", body) }, http.StatusBadRequest, metrics.RejectHeaders},
		{"too large", func() *http.Request {
			big := bytes.Repeat([]byte("a"), maxWebhookBody+1)
			return webhookRequest("ping", "d1", big)
		}, http.StatusRequestEntityTooLarge, metrics.RejectSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, m := unitApp(t)
			rec := serve(a.Handler(), tt.build())
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := testutil.ToFloat64(m.WebhookRejected.WithLabelValues(tt.reason)); got != 1 {
				t.Fatalf("rejected{%s} = %v", tt.reason, got)
			}
			if got := testutil.CollectAndCount(m.WebhookReceived); got != 0 {
				t.Fatalf("received series = %d", got)
			}
		})
	}
}

func TestWebhookAcceptsExactLimit(t *testing.T) {
	a, _ := unitApp(t)
	body := bytes.Repeat([]byte(" "), maxWebhookBody)
	req := webhookRequest("ping", "", body)
	rec := serve(a.Handler(), req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want header rejection after reading full body", rec.Code)
	}
}

func TestHealthAndMetrics(t *testing.T) {
	a, _ := unitApp(t)
	h := a.Handler()
	expectStatus(t, serve(h, httptest.NewRequest(http.MethodGet, "/healthz", nil)), http.StatusOK, "ok")
	expectStatus(t, serve(h, httptest.NewRequest(http.MethodGet, "/readyz", nil)), http.StatusServiceUnavailable, "unavailable")
	serve(h, webhookRequest("ping", "", []byte("{}")))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	expectStatus(t, rec, http.StatusOK, "")
	data, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{`quanto_webhook_rejected_total{reason="headers"} 1`, "quanto_queue_depth 0", "go_goroutines"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("metrics lack %q", want)
		}
	}
	if rec := serve(h, httptest.NewRequest(http.MethodGet, "/webhook", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /webhook = %d", rec.Code)
	}
}

func TestIsWorkflowPath(t *testing.T) {
	tests := map[string]bool{
		".github/workflows/ci.yml":       true,
		".github/workflows/ci.yaml":      true,
		".github/workflows/.yml":         false,
		".github/workflows/a/ci.yml":     false,
		".github/workflows/ci.yml.bak":   false,
		"github/workflows/ci.yml":        false,
		".github/workflows/README.md":    false,
		"x/.github/workflows/ci.yml":     false,
		".github/workflows/a.b.yml":      true,
		".github/workflows/ci.YML":       false,
		".github/workflows/한글.yml":       true,
		".github/workflows/with space.y": false,
	}
	for in, want := range tests {
		if got := isWorkflowPath(in); got != want {
			t.Errorf("isWorkflowPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWorkflowPathFromRun(t *testing.T) {
	tests := map[string]string{
		".github/workflows/ci.yml":                 ".github/workflows/ci.yml",
		".github/workflows/ci.yml@refs/heads/main": ".github/workflows/ci.yml",
		"dynamic/github-code-scanning/codeql":      "dynamic/github-code-scanning/codeql",
	}
	for in, want := range tests {
		if got := workflowPathFromRun(in); got != want {
			t.Errorf("workflowPathFromRun(%q) = %q, want %q", in, got, want)
		}
	}
}
