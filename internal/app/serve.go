package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/BETAER-08/quanto/internal/config"
	"github.com/BETAER-08/quanto/internal/github"
	"github.com/BETAER-08/quanto/internal/metrics"
	"github.com/BETAER-08/quanto/internal/store"
)

type Role string

const (
	RoleWeb    Role = "web"
	RoleWorker Role = "worker"
	RoleAll    Role = "all"
)

func ParseRole(s string) (Role, bool) {
	switch Role(s) {
	case RoleWeb, RoleWorker, RoleAll:
		return Role(s), true
	}
	return "", false
}

func Serve(ctx context.Context, cfg *config.Config, role Role, version string, logger *slog.Logger) error {
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	applied, err := st.Migrate(ctx)
	if err != nil {
		return err
	}
	logger.Info("migrations applied", "count", len(applied), "versions", applied)
	m, err := metrics.New()
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	gh, err := github.NewAppClient(github.Options{
		BaseURL:    cfg.GitHubAPIURL,
		AppID:      cfg.AppID,
		PrivateKey: cfg.PrivateKey,
		Version:    version,
		OnResponse: m.ObserveGitHubResponse,
	})
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	a, err := New(Options{
		Store:             st,
		GitHub:            gh,
		Metrics:           m,
		Logger:            logger,
		WebhookSecret:     cfg.WebhookSecret,
		AllowPrivateRepos: cfg.AllowPrivateRepos,
		MaxWorkflowFiles:  cfg.MaxWorkflowFiles,
		Concurrency:       cfg.WorkerConcurrency,
	})
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var tasks []func(context.Context) error
	if role == RoleWeb || role == RoleAll {
		tasks = append(tasks, func(c context.Context) error { return a.ServeHTTP(c, cfg.ListenAddr) })
	}
	if role == RoleWorker || role == RoleAll {
		tasks = append(tasks, a.RunWorker)
	}
	errc := make(chan error, len(tasks))
	for _, task := range tasks {
		go func(task func(context.Context) error) {
			errc <- task(runCtx)
		}(task)
	}
	var first error
	for range tasks {
		if err := <-errc; err != nil && first == nil {
			first = err
			cancel()
		}
	}
	return first
}
