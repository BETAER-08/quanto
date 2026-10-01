package action

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path"
	"sort"
	"time"

	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/internal/github"
)

const (
	EstimateMaxFiles = 10
	estimateRuns     = 10
	statsWindow      = 30
	estimateSkipped  = "Runner-minute estimate skipped: a GitHub API request failed."
)

type statKey struct {
	path string
	job  string
}

type statValue struct {
	avg     time.Duration
	samples int
}

type durationTable map[statKey]statValue

func (t durationTable) JobAverage(workflowPath, jobKey string) (time.Duration, int, bool) {
	v, ok := t[statKey{path: workflowPath, job: jobKey}]
	if !ok {
		return 0, 0, false
	}
	return v.avg, v.samples, true
}

type sample struct {
	jobID     int64
	completed time.Time
	seconds   int
}

func estimatePaths(inputs []semdiff.Input) []string {
	seen := map[string]bool{}
	for _, in := range inputs {
		if in.Before != nil {
			p := in.Path
			if in.OldPath != "" {
				p = in.OldPath
			}
			seen[p] = true
		}
		if in.After != nil {
			seen[in.Path] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	if len(out) > EstimateMaxFiles {
		out = out[:EstimateMaxFiles]
	}
	return out
}

func isNotFound(err error) bool {
	var apiErr *github.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (r *runner) estimate(ctx context.Context, inputs []semdiff.Input) (semdiff.DurationSource, string) {
	paths := estimatePaths(inputs)
	start := r.calls.Load()
	table := durationTable{}
	for _, p := range paths {
		if err := r.collect(ctx, p, table); err != nil {
			r.warn("runner-minute estimate skipped", err)
			return nil, estimateSkipped
		}
	}
	calls := r.calls.Load() - start
	return table, "Runner-minute estimate: " + plural(calls, "GitHub API call", "GitHub API calls") + " for " + plural(int64(len(paths)), "workflow file", "workflow files") + "."
}

func (r *runner) collect(ctx context.Context, workflowPath string, table durationTable) error {
	runs, err := r.client.WorkflowFileRuns(ctx, r.cfg.owner, r.cfg.repo, path.Base(workflowPath), estimateRuns)
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	byKey := map[string][]sample{}
	for _, run := range runs {
		jobs, err := r.client.RunJobs(ctx, r.cfg.owner, r.cfg.repo, run.ID)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if j.Conclusion != "success" || j.StartedAt.IsZero() || j.CompletedAt.IsZero() || j.CompletedAt.Before(j.StartedAt) {
				continue
			}
			key, ok := semdiff.NormalizeRunJobName(j.Name)
			if !ok {
				continue
			}
			byKey[key] = append(byKey[key], sample{jobID: j.ID, completed: j.CompletedAt, seconds: int(j.CompletedAt.Sub(j.StartedAt).Seconds())})
		}
	}
	for key, samples := range byKey {
		sort.Slice(samples, func(i, j int) bool {
			if !samples[i].completed.Equal(samples[j].completed) {
				return samples[i].completed.After(samples[j].completed)
			}
			return samples[i].jobID > samples[j].jobID
		})
		if len(samples) > statsWindow {
			samples = samples[:statsWindow]
		}
		total := 0
		for _, s := range samples {
			total += s.seconds
		}
		avg := math.Round(float64(total) / float64(len(samples)))
		table[statKey{path: workflowPath, job: key}] = statValue{avg: time.Duration(avg) * time.Second, samples: len(samples)}
	}
	return nil
}
