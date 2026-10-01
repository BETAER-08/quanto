package app

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/internal/analysis"
	"github.com/BETAER-08/quanto/internal/store"
)

const backfillRuns = 100

func (p IngestPayload) validate() error {
	if p.InstallationID <= 0 || p.RepositoryID <= 0 || p.Owner == "" || p.Repo == "" || p.RunID <= 0 {
		return permanent(errors.New("decode payload: ingest_workflow_run payload is incomplete"))
	}
	return nil
}

func (p BackfillPayload) validate() error {
	if p.InstallationID <= 0 || p.RepositoryID <= 0 || p.Owner == "" || p.Repo == "" {
		return permanent(errors.New("decode payload: backfill_repo payload is incomplete"))
	}
	return nil
}

func (a *App) ingestWorkflowRun(ctx context.Context, raw []byte) error {
	var p IngestPayload
	if err := decodePayload(raw, &p); err != nil {
		return err
	}
	if err := p.validate(); err != nil {
		return err
	}
	if !strings.HasPrefix(p.WorkflowPath, analysis.WorkflowDir) {
		return nil
	}
	client, err := a.github.Installation(ctx, p.InstallationID)
	if err != nil {
		return err
	}
	jobs, err := client.RunJobs(ctx, p.Owner, p.Repo, p.RunID)
	if err != nil {
		return err
	}
	var runs []store.JobRun
	keys := map[string]bool{}
	for _, j := range jobs {
		if j.Conclusion != "success" && j.Conclusion != "failure" {
			continue
		}
		if j.StartedAt.IsZero() || j.CompletedAt.IsZero() || j.CompletedAt.Before(j.StartedAt) {
			continue
		}
		key, ok := semdiff.NormalizeRunJobName(j.Name)
		if !ok {
			continue
		}
		runs = append(runs, store.JobRun{
			JobID:           j.ID,
			RepositoryID:    p.RepositoryID,
			RunID:           p.RunID,
			WorkflowPath:    p.WorkflowPath,
			JobKey:          key,
			RunnerLabels:    j.Labels,
			Conclusion:      j.Conclusion,
			StartedAt:       j.StartedAt,
			CompletedAt:     j.CompletedAt,
			DurationSeconds: int(j.CompletedAt.Sub(j.StartedAt).Seconds()),
		})
		keys[key] = true
	}
	if len(runs) == 0 {
		return nil
	}
	if err := a.store.InsertJobRuns(ctx, runs); err != nil {
		return err
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		if err := a.store.RecomputeJobStats(ctx, p.RepositoryID, p.WorkflowPath, k); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) backfillRepo(ctx context.Context, raw []byte) error {
	var p BackfillPayload
	if err := decodePayload(raw, &p); err != nil {
		return err
	}
	if err := p.validate(); err != nil {
		return err
	}
	client, err := a.github.Installation(ctx, p.InstallationID)
	if err != nil {
		return err
	}
	runs, err := client.WorkflowRuns(ctx, p.Owner, p.Repo, backfillRuns)
	if err != nil {
		return err
	}
	repo := store.Repository{ID: p.RepositoryID, Owner: p.Owner, Name: p.Repo}
	for _, r := range runs {
		if _, err := a.enqueueIngest(ctx, p.InstallationID, repo, r.ID, r.Path); err != nil {
			return err
		}
	}
	return nil
}
