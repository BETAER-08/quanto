package metrics

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	RejectSignature = "signature"
	RejectHeaders   = "headers"
	RejectSize      = "size"

	ResultDone     = "done"
	ResultFailed   = "failed"
	ResultDeferred = "deferred"
	ResultDead     = "dead"
)

type Metrics struct {
	Registry                 *prometheus.Registry
	WebhookReceived          *prometheus.CounterVec
	WebhookRejected          *prometheus.CounterVec
	QueueJobs                *prometheus.CounterVec
	QueueDepth               prometheus.Gauge
	AnalysisDuration         prometheus.Histogram
	Findings                 *prometheus.CounterVec
	GitHubRequests           *prometheus.CounterVec
	GitHubRateLimitRemaining prometheus.Gauge
}

func New() (*Metrics, error) {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		WebhookReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quanto_webhook_received_total",
			Help: "Webhook deliveries received, by event.",
		}, []string{"event"}),
		WebhookRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quanto_webhook_rejected_total",
			Help: "Webhook deliveries rejected, by reason.",
		}, []string{"reason"}),
		QueueJobs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quanto_queue_jobs_total",
			Help: "Queue jobs processed, by kind and result.",
		}, []string{"kind", "result"}),
		QueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quanto_queue_depth",
			Help: "Pending queue jobs.",
		}),
		AnalysisDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "quanto_analysis_duration_seconds",
			Help:    "Duration of pull request analyses.",
			Buckets: prometheus.DefBuckets,
		}),
		Findings: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quanto_findings_total",
			Help: "Findings reported, by significance.",
		}, []string{"significance"}),
		GitHubRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quanto_github_requests_total",
			Help: "GitHub API responses, by status class.",
		}, []string{"status_class"}),
		GitHubRateLimitRemaining: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quanto_github_rate_limit_remaining",
			Help: "Remaining GitHub API rate limit reported by the latest response.",
		}),
	}
	for _, c := range []prometheus.Collector{
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.WebhookReceived,
		m.WebhookRejected,
		m.QueueJobs,
		m.QueueDepth,
		m.AnalysisDuration,
		m.Findings,
		m.GitHubRequests,
		m.GitHubRateLimitRemaining,
	} {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("register collector: %w", err)
		}
	}
	for _, r := range []string{RejectSignature, RejectHeaders, RejectSize} {
		m.WebhookRejected.WithLabelValues(r)
	}
	for _, s := range []string{"low", "normal", "high"} {
		m.Findings.WithLabelValues(s)
	}
	for _, c := range []string{"2xx", "4xx", "5xx"} {
		m.GitHubRequests.WithLabelValues(c)
	}
	return m, nil
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

func StatusClass(status int) (string, bool) {
	switch {
	case status >= 200 && status < 300:
		return "2xx", true
	case status >= 400 && status < 500:
		return "4xx", true
	case status >= 500 && status < 600:
		return "5xx", true
	}
	return "", false
}

func (m *Metrics) ObserveGitHubResponse(status int, rateRemaining int) {
	if class, ok := StatusClass(status); ok {
		m.GitHubRequests.WithLabelValues(class).Inc()
	}
	if rateRemaining >= 0 {
		m.GitHubRateLimitRemaining.Set(float64(rateRemaining))
	}
}
