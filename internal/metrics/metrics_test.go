package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newMetrics(t *testing.T) *Metrics {
	t.Helper()
	m, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestRegisteredFamilies(t *testing.T) {
	m := newMetrics(t)
	m.WebhookReceived.WithLabelValues("ping").Inc()
	m.QueueJobs.WithLabelValues("analyze_pr", ResultDone).Inc()
	out := scrape(t, m)
	for _, want := range []string{
		"# TYPE quanto_webhook_received_total counter",
		"# TYPE quanto_webhook_rejected_total counter",
		"# TYPE quanto_queue_jobs_total counter",
		"# TYPE quanto_queue_depth gauge",
		"# TYPE quanto_analysis_duration_seconds histogram",
		"# TYPE quanto_findings_total counter",
		"# TYPE quanto_github_requests_total counter",
		"# TYPE quanto_github_rate_limit_remaining gauge",
		"# TYPE go_goroutines gauge",
		"# TYPE process_cpu_seconds_total counter",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestLabels(t *testing.T) {
	m := newMetrics(t)
	m.WebhookReceived.WithLabelValues("pull_request").Inc()
	m.QueueJobs.WithLabelValues("backfill_repo", ResultDead).Inc()
	m.Findings.WithLabelValues("high").Add(2)
	out := scrape(t, m)
	for _, want := range []string{
		`quanto_webhook_received_total{event="pull_request"} 1`,
		`quanto_webhook_rejected_total{reason="signature"} 0`,
		`quanto_webhook_rejected_total{reason="headers"} 0`,
		`quanto_webhook_rejected_total{reason="size"} 0`,
		`quanto_queue_jobs_total{kind="backfill_repo",result="dead"} 1`,
		`quanto_findings_total{significance="high"} 2`,
		`quanto_findings_total{significance="low"} 0`,
		`quanto_findings_total{significance="normal"} 0`,
		`quanto_github_requests_total{status_class="2xx"} 0`,
		`quanto_github_requests_total{status_class="4xx"} 0`,
		`quanto_github_requests_total{status_class="5xx"} 0`,
		"quanto_queue_depth 0",
		"quanto_analysis_duration_seconds_count 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestObserveGitHubResponse(t *testing.T) {
	m := newMetrics(t)
	m.ObserveGitHubResponse(200, 4999)
	m.ObserveGitHubResponse(201, -1)
	m.ObserveGitHubResponse(404, 4998)
	m.ObserveGitHubResponse(502, -1)
	m.ObserveGitHubResponse(304, -1)
	tests := []struct {
		class string
		want  float64
	}{
		{"2xx", 2},
		{"4xx", 1},
		{"5xx", 1},
	}
	for _, tt := range tests {
		if got := testutil.ToFloat64(m.GitHubRequests.WithLabelValues(tt.class)); got != tt.want {
			t.Errorf("%s = %v, want %v", tt.class, got, tt.want)
		}
	}
	if got := testutil.CollectAndCount(m.GitHubRequests); got != 3 {
		t.Errorf("status classes = %d, want 3", got)
	}
	if got := testutil.ToFloat64(m.GitHubRateLimitRemaining); got != 4998 {
		t.Errorf("rate limit remaining = %v", got)
	}
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		status int
		want   string
		ok     bool
	}{
		{200, "2xx", true},
		{299, "2xx", true},
		{301, "", false},
		{403, "4xx", true},
		{429, "4xx", true},
		{500, "5xx", true},
		{0, "", false},
	}
	for _, tt := range tests {
		got, ok := StatusClass(tt.status)
		if got != tt.want || ok != tt.ok {
			t.Errorf("StatusClass(%d) = %q, %v", tt.status, got, ok)
		}
	}
}

func TestIndependentRegistries(t *testing.T) {
	a := newMetrics(t)
	b := newMetrics(t)
	a.QueueDepth.Set(7)
	if got := testutil.ToFloat64(b.QueueDepth); got != 0 {
		t.Errorf("registries share state: %v", got)
	}
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if strings.HasPrefix(f.GetName(), "quanto_") {
			t.Errorf("global registry contains %s", f.GetName())
		}
	}
}

func TestAnalysisDuration(t *testing.T) {
	m := newMetrics(t)
	m.AnalysisDuration.Observe(0.2)
	m.AnalysisDuration.Observe(3)
	out := scrape(t, m)
	if !strings.Contains(out, "quanto_analysis_duration_seconds_count 2") {
		t.Errorf("histogram count missing:\n%s", out)
	}
}
