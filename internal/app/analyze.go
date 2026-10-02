package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/BETAER-08/quanto/core/report"
	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/internal/analysis"
	"github.com/BETAER-08/quanto/internal/github"
	"github.com/BETAER-08/quanto/internal/store"
)

func decodePayload(raw []byte, out any) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return permanent(fmt.Errorf("decode payload: %w", err))
	}
	return nil
}

func (p AnalyzePayload) validate() error {
	if p.InstallationID <= 0 || p.RepositoryID <= 0 || p.Owner == "" || p.Repo == "" || p.Number <= 0 || p.HeadSHA == "" || p.BaseSHA == "" {
		return permanent(errors.New("decode payload: analyze_pr payload is incomplete"))
	}
	return nil
}

func (a *App) analyzePR(ctx context.Context, raw []byte) error {
	started := time.Now()
	var p AnalyzePayload
	if err := decodePayload(raw, &p); err != nil {
		return err
	}
	if err := p.validate(); err != nil {
		return err
	}
	suspended, err := a.store.InstallationSuspended(ctx, p.InstallationID)
	if err != nil {
		return err
	}
	if suspended {
		return nil
	}
	client, err := a.github.Installation(ctx, p.InstallationID)
	if err != nil {
		return err
	}
	res, err := analysis.Load(ctx, client, analysis.Request{
		Owner:    p.Owner,
		Repo:     p.Repo,
		Number:   p.Number,
		BaseSHA:  p.BaseSHA,
		HeadSHA:  p.HeadSHA,
		MaxFiles: a.maxWorkflowFiles,
	})
	if err != nil {
		return err
	}
	if len(res.Inputs) == 0 {
		return nil
	}
	mergeBase, meta := res.MergeBase, res.Meta
	durations, err := a.store.Durations(ctx, p.RepositoryID)
	if err != nil {
		return err
	}
	diffs := res.Compare(semdiff.Options{Durations: durations})
	title, summary := report.CheckSummary(diffs, meta)
	checkRunID, err := a.publishCheckRun(ctx, client, p, github.CheckRun{
		HeadSHA:     p.HeadSHA,
		Title:       title,
		Summary:     summary,
		Annotations: report.Annotations(diffs),
	})
	if err != nil {
		return err
	}
	pr, err := client.PullRequest(ctx, p.Owner, p.Repo, p.Number)
	if err != nil {
		return err
	}
	if pr.Head.SHA == p.HeadSHA {
		if err := a.publishComment(ctx, client, p, diffs, meta); err != nil {
			return err
		}
	} else {
		a.logger.Info("head moved; skipping comment", "repository_id", p.RepositoryID, "number", p.Number)
	}
	result, err := report.JSON(diffs, meta)
	if err != nil {
		return fmt.Errorf("render analysis json: %w", err)
	}
	counts := map[semdiff.Significance]int{}
	total := 0
	for _, d := range diffs {
		for _, f := range d.Findings {
			counts[f.Significance]++
			total++
		}
	}
	elapsed := time.Since(started)
	if err := a.store.SaveAnalysis(ctx, store.Analysis{
		RepositoryID: p.RepositoryID,
		PRNumber:     p.Number,
		HeadSHA:      p.HeadSHA,
		BaseSHA:      mergeBase,
		Result:       result,
		FindingCount: total,
		CheckRunID:   checkRunID,
		DurationMS:   int(elapsed.Milliseconds()),
	}); err != nil {
		return err
	}
	a.metrics.AnalysisDuration.Observe(elapsed.Seconds())
	for _, sig := range []semdiff.Significance{semdiff.Low, semdiff.Normal, semdiff.High} {
		if counts[sig] > 0 {
			a.metrics.Findings.WithLabelValues(sig.String()).Add(float64(counts[sig]))
		}
	}
	return nil
}

func (a *App) publishCheckRun(ctx context.Context, client *github.Client, p AnalyzePayload, run github.CheckRun) (int64, error) {
	id, found, err := client.FindCheckRun(ctx, p.Owner, p.Repo, p.HeadSHA, github.CheckRunName)
	if err != nil {
		return 0, err
	}
	if found {
		return id, client.UpdateCheckRun(ctx, p.Owner, p.Repo, id, run)
	}
	return client.CreateCheckRun(ctx, p.Owner, p.Repo, run)
}

func (a *App) botLogin(ctx context.Context) (string, error) {
	a.slugMu.Lock()
	defer a.slugMu.Unlock()
	if a.slug == "" {
		app, err := a.github.App(ctx)
		if err != nil {
			return "", err
		}
		if app.Slug == "" {
			return "", errors.New("github app slug is empty")
		}
		a.slug = app.Slug
	}
	return a.slug + "[bot]", nil
}

func (a *App) findComment(ctx context.Context, client *github.Client, p AnalyzePayload) (int64, bool, error) {
	login, err := a.botLogin(ctx)
	if err != nil {
		return 0, false, err
	}
	comments, err := client.IssueComments(ctx, p.Owner, p.Repo, p.Number)
	if err != nil {
		return 0, false, err
	}
	for _, c := range comments {
		if c.User.Login == login && strings.HasPrefix(c.Body, report.CommentMarker) {
			return c.ID, true, nil
		}
	}
	return 0, false, nil
}

func isNotFound(err error) bool {
	var apiErr *github.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

func (a *App) publishComment(ctx context.Context, client *github.Client, p AnalyzePayload, diffs []*semdiff.FileDiff, meta report.Meta) error {
	body, publishable := analysis.CommentBody(diffs, meta, report.DetailsCheckRun)
	id, ok, err := a.store.CommentID(ctx, p.RepositoryID, p.Number)
	if err != nil {
		return err
	}
	if ok {
		err := client.UpdateIssueComment(ctx, p.Owner, p.Repo, id, body)
		if err == nil {
			return nil
		}
		if !isNotFound(err) {
			return err
		}
		a.logger.Info("cached comment not found; searching", "repository_id", p.RepositoryID, "number", p.Number)
	}
	id, ok, err = a.findComment(ctx, client, p)
	if err != nil {
		return err
	}
	switch {
	case ok:
		if err := client.UpdateIssueComment(ctx, p.Owner, p.Repo, id, body); err != nil {
			return err
		}
	case publishable:
		id, err = client.CreateIssueComment(ctx, p.Owner, p.Repo, p.Number, body)
		if err != nil {
			return err
		}
	default:
		return nil
	}
	return a.store.SetCommentID(ctx, p.RepositoryID, p.Number, id)
}
