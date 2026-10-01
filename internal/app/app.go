package app

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/BETAER-08/quanto/internal/github"
	"github.com/BETAER-08/quanto/internal/metrics"
	"github.com/BETAER-08/quanto/internal/store"
)

type Options struct {
	Store             *store.Store
	GitHub            *github.AppClient
	Metrics           *metrics.Metrics
	Logger            *slog.Logger
	WebhookSecret     []byte
	AllowPrivateRepos bool
	MaxWorkflowFiles  int
	Concurrency       int
}

type App struct {
	store             *store.Store
	github            *github.AppClient
	metrics           *metrics.Metrics
	logger            *slog.Logger
	webhookSecret     []byte
	allowPrivateRepos bool
	maxWorkflowFiles  int
	concurrency       int

	slugMu sync.Mutex
	slug   string
}

func New(opts Options) (*App, error) {
	if opts.Metrics == nil {
		return nil, errors.New("app: metrics are required")
	}
	if len(opts.WebhookSecret) == 0 {
		return nil, errors.New("app: webhook secret is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	maxFiles := opts.MaxWorkflowFiles
	if maxFiles <= 0 {
		maxFiles = 50
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	return &App{
		store:             opts.Store,
		github:            opts.GitHub,
		metrics:           opts.Metrics,
		logger:            logger,
		webhookSecret:     opts.WebhookSecret,
		allowPrivateRepos: opts.AllowPrivateRepos,
		maxWorkflowFiles:  maxFiles,
		concurrency:       concurrency,
	}, nil
}

func (a *App) repoAllowed(private bool) bool {
	return !private || a.allowPrivateRepos
}

type permanentError struct {
	err error
}

func (e *permanentError) Error() string {
	return e.err.Error()
}

func (e *permanentError) Unwrap() error {
	return e.err
}

func permanent(err error) error {
	return &permanentError{err: err}
}
