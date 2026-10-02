package action

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/BETAER-08/quanto/core/report"
	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/internal/analysis"
	"github.com/BETAER-08/quanto/internal/github"
)

const (
	BotLogin          = "github-actions[bot]"
	notPullRequest    = "quanto: nothing to analyze; quanto runs on pull_request and pull_request_target events."
	analysisFailed    = "## quanto\n\nAnalysis failed: a GitHub API request failed. See the step log.\n"
	noWorkflowFiles   = "## quanto\n\nNo workflow files changed in this pull request.\n"
	commentReadOnly   = "comment skipped: token is read-only"
	commentFailed     = "comment skipped: a GitHub API request failed."
	exitOK            = 0
	exitConfiguration = 1
)

type runner struct {
	cfg    config
	client *github.Client
	stdout io.Writer
	stderr io.Writer
	calls  atomic.Int64
	outErr error
}

func Run(ctx context.Context, getenv func(string) string, stdout, stderr io.Writer, version string) int {
	if !isPullRequestEvent(getenv("GITHUB_EVENT_NAME")) {
		if _, err := fmt.Fprintln(stdout, notPullRequest); err != nil {
			fmt.Fprintf(stderr, "quanto: write output: %v\n", err)
		}
		return exitOK
	}
	cfg, err := loadConfig(getenv)
	r := &runner{cfg: cfg, stdout: stdout, stderr: stderr}
	if err == nil {
		r.client, err = github.NewTokenClient(github.Options{
			BaseURL:    cfg.apiURL,
			Version:    version,
			OnResponse: func(int, int) { r.calls.Add(1) },
		}, cfg.token)
	}
	if err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			fmt.Fprintf(stderr, "quanto: %s\n", line)
		}
		return exitConfiguration
	}
	r.run(ctx)
	if r.outErr != nil {
		fmt.Fprintf(stderr, "quanto: write output: %v\n", r.outErr)
	}
	return exitOK
}

func (r *runner) line(s string) {
	if r.outErr != nil {
		return
	}
	if _, err := io.WriteString(r.stdout, s+"\n"); err != nil {
		r.outErr = err
	}
}

func (r *runner) warn(what string, err error) {
	r.line(command("warning", []prop{{name: "title", value: "quanto"}}, what+": "+err.Error()))
}

func (r *runner) run(ctx context.Context) {
	res, err := analysis.Load(ctx, r.client, analysis.Request{
		Owner:    r.cfg.owner,
		Repo:     r.cfg.repo,
		Number:   r.cfg.number,
		BaseSHA:  r.cfg.baseSHA,
		HeadSHA:  r.cfg.headSHA,
		MaxFiles: r.cfg.maxFiles,
	})
	if err != nil {
		r.warn("analysis failed", err)
		r.writeSummary(analysisFailed)
		return
	}
	if len(res.Inputs) == 0 {
		r.writeSummary(noWorkflowFiles)
		return
	}
	var notes []string
	opts := semdiff.Options{}
	if r.cfg.estimate {
		durations, note := r.estimate(ctx, res.Inputs)
		opts.Durations = durations
		notes = append(notes, note)
	}
	diffs := res.Compare(opts)
	for _, a := range report.Annotations(diffs) {
		r.line(command("notice", annotationProps(a), a.Message))
	}
	if r.cfg.comment {
		if note := r.comment(ctx, diffs, res.Meta); note != "" {
			notes = append(notes, note)
		}
	}
	_, summary := report.CheckSummary(diffs, res.Meta)
	var b strings.Builder
	b.WriteString(summary)
	for _, n := range notes {
		b.WriteString("\n" + n + "\n")
	}
	r.writeSummary(b.String())
}

func annotationProps(a report.Annotation) []prop {
	props := []prop{
		{name: "file", value: a.Path},
		{name: "line", value: strconv.Itoa(a.StartLine)},
		{name: "endLine", value: strconv.Itoa(a.EndLine)},
	}
	if a.StartColumn > 0 && a.EndColumn > 0 {
		props = append(props, prop{name: "col", value: strconv.Itoa(a.StartColumn)}, prop{name: "endColumn", value: strconv.Itoa(a.EndColumn)})
	}
	return append(props, prop{name: "title", value: a.Title})
}

func (r *runner) comment(ctx context.Context, diffs []*semdiff.FileDiff, meta report.Meta) string {
	body, publishable := analysis.CommentBody(diffs, meta, report.DetailsJobSummary)
	comments, err := r.client.IssueComments(ctx, r.cfg.owner, r.cfg.repo, r.cfg.number)
	if err != nil {
		return r.commentFailure(err)
	}
	for _, c := range comments {
		if c.User.Login == BotLogin && strings.HasPrefix(c.Body, report.CommentMarker) {
			if err := r.client.UpdateIssueComment(ctx, r.cfg.owner, r.cfg.repo, c.ID, body); err != nil {
				return r.commentFailure(err)
			}
			return ""
		}
	}
	if !publishable {
		return ""
	}
	if _, err := r.client.CreateIssueComment(ctx, r.cfg.owner, r.cfg.repo, r.cfg.number, body); err != nil {
		return r.commentFailure(err)
	}
	return ""
}

func (r *runner) commentFailure(err error) string {
	var apiErr *github.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
		return commentReadOnly
	}
	r.warn("comment failed", err)
	return commentFailed
}

func (r *runner) writeSummary(body string) {
	if r.cfg.summaryPath == "" {
		return
	}
	f, err := os.OpenFile(r.cfg.summaryPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		r.warn("write job summary", err)
		return
	}
	_, err = io.WriteString(f, body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		r.warn("write job summary", err)
	}
}
