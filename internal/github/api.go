package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/BETAER-08/quanto/core/report"
)

const MaxFileSize = 256 << 10

var ErrFileTooLarge = errors.New("file exceeds 256 KiB")

const (
	maxPullRequestFiles = 3000
	annotationBatch     = 50
)

const CheckRunName = "quanto"

type GitRef struct {
	SHA string `json:"sha"`
	Ref string `json:"ref"`
}

type PullRequest struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Head   GitRef `json:"head"`
	Base   GitRef `json:"base"`
}

type PullRequestFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
}

type User struct {
	Login string `json:"login"`
}

type IssueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User User   `json:"user"`
}

type WorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type RunJob struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Labels      []string  `json:"labels"`
}

type CheckRun struct {
	HeadSHA     string
	Title       string
	Summary     string
	Annotations []report.Annotation
}

type checkAnnotation struct {
	Path            string `json:"path"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	StartColumn     int    `json:"start_column,omitempty"`
	EndColumn       int    `json:"end_column,omitempty"`
	AnnotationLevel string `json:"annotation_level"`
	Title           string `json:"title,omitempty"`
	Message         string `json:"message"`
}

type checkOutput struct {
	Title       string            `json:"title"`
	Summary     string            `json:"summary"`
	Annotations []checkAnnotation `json:"annotations,omitempty"`
}

type checkRunBody struct {
	Name       string      `json:"name,omitempty"`
	HeadSHA    string      `json:"head_sha,omitempty"`
	Status     string      `json:"status"`
	Conclusion string      `json:"conclusion"`
	Output     checkOutput `json:"output"`
}

func repoSegments(owner, repo string, rest ...string) []string {
	return append([]string{"repos", owner, repo}, rest...)
}

func (c *Client) PullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return nil, err
	}
	var pr PullRequest
	endpoint := c.t.endpoint(nil, repoSegments(owner, repo, "pulls", strconv.Itoa(number))...)
	if _, err := c.t.getJSON(ctx, endpoint, auth, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func (c *Client) MergeBase(ctx context.Context, owner, repo, base, head string) (string, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return "", err
	}
	endpoint := c.t.endpoint(url.Values{"per_page": {"1"}}, repoSegments(owner, repo, "compare", base+"..."+head)...)
	var payload struct {
		MergeBaseCommit struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	if _, err := c.t.getJSON(ctx, endpoint, auth, &payload); err != nil {
		return "", fmt.Errorf("github: compare commits: %w", err)
	}
	if payload.MergeBaseCommit.SHA == "" {
		return "", errors.New("github: compare commits: empty merge base")
	}
	return payload.MergeBaseCommit.SHA, nil
}

func (c *Client) PullRequestFiles(ctx context.Context, owner, repo string, number int) ([]PullRequestFile, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"per_page": {strconv.Itoa(perPage)}}
	endpoint := c.t.endpoint(query, repoSegments(owner, repo, "pulls", strconv.Itoa(number), "files")...)
	return paginate(ctx, c.t, endpoint, auth, maxPullRequestFiles, decodeList[PullRequestFile])
}

func (c *Client) FileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, bool, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return nil, false, err
	}
	segments := repoSegments(owner, repo, "contents")
	segments = append(segments, strings.Split(strings.TrimPrefix(path, "/"), "/")...)
	endpoint := c.t.endpoint(url.Values{"ref": {ref}}, segments...)
	resp, err := c.t.do(ctx, request{method: http.MethodGet, url: endpoint, accept: acceptRaw, auth: auth})
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFileSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("github: read file content: %w", err)
	}
	if len(data) > MaxFileSize {
		return nil, true, ErrFileTooLarge
	}
	return data, true, nil
}

func toCheckAnnotations(in []report.Annotation) []checkAnnotation {
	out := make([]checkAnnotation, len(in))
	for i, a := range in {
		out[i] = checkAnnotation{
			Path:            a.Path,
			StartLine:       a.StartLine,
			EndLine:         a.EndLine,
			AnnotationLevel: a.Level,
			Title:           a.Title,
			Message:         a.Message,
		}
		if a.StartLine == a.EndLine {
			out[i].StartColumn = a.StartColumn
			out[i].EndColumn = a.EndColumn
		}
	}
	return out
}

func batches(in []checkAnnotation) [][]checkAnnotation {
	var out [][]checkAnnotation
	for len(in) > annotationBatch {
		out = append(out, in[:annotationBatch])
		in = in[annotationBatch:]
	}
	return append(out, in)
}

func (c *Client) CreateCheckRun(ctx context.Context, owner, repo string, run CheckRun) (int64, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return 0, err
	}
	chunks := batches(toCheckAnnotations(run.Annotations))
	body := checkRunBody{
		Name:       CheckRunName,
		HeadSHA:    run.HeadSHA,
		Status:     "completed",
		Conclusion: "neutral",
		Output:     checkOutput{Title: run.Title, Summary: run.Summary, Annotations: chunks[0]},
	}
	var created struct {
		ID int64 `json:"id"`
	}
	endpoint := c.t.endpoint(nil, repoSegments(owner, repo, "check-runs")...)
	if err := c.t.sendJSON(ctx, http.MethodPost, endpoint, auth, body, &created); err != nil {
		return 0, fmt.Errorf("github: create check run: %w", err)
	}
	for _, chunk := range chunks[1:] {
		if err := c.patchCheckRun(ctx, owner, repo, created.ID, run, chunk); err != nil {
			return created.ID, err
		}
	}
	return created.ID, nil
}

type existingCheckRun struct {
	ID  int64 `json:"id"`
	App struct {
		ID int64 `json:"id"`
	} `json:"app"`
	Output struct {
		AnnotationsCount int `json:"annotations_count"`
	} `json:"output"`
}

func (c *Client) FindCheckRun(ctx context.Context, owner, repo, headSHA, name string) (int64, bool, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return 0, false, err
	}
	query := url.Values{"check_name": {name}, "per_page": {strconv.Itoa(perPage)}}
	endpoint := c.t.endpoint(query, repoSegments(owner, repo, "commits", headSHA, "check-runs")...)
	runs, err := paginate(ctx, c.t, endpoint, auth, 0, func(resp *http.Response) ([]existingCheckRun, error) {
		var payload struct {
			CheckRuns []existingCheckRun `json:"check_runs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return nil, err
		}
		return payload.CheckRuns, nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("github: find check run: %w", err)
	}
	for _, r := range runs {
		if r.App.ID == c.app.appID && r.ID > 0 {
			return r.ID, true, nil
		}
	}
	return 0, false, nil
}

func (c *Client) checkRunAnnotations(ctx context.Context, owner, repo string, id int64) (int, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return 0, err
	}
	var existing existingCheckRun
	endpoint := c.t.endpoint(nil, repoSegments(owner, repo, "check-runs", strconv.FormatInt(id, 10))...)
	if _, err := c.t.getJSON(ctx, endpoint, auth, &existing); err != nil {
		return 0, fmt.Errorf("github: read check run: %w", err)
	}
	return existing.Output.AnnotationsCount, nil
}

func (c *Client) UpdateCheckRun(ctx context.Context, owner, repo string, id int64, run CheckRun) error {
	present, err := c.checkRunAnnotations(ctx, owner, repo, id)
	if err != nil {
		return err
	}
	all := toCheckAnnotations(run.Annotations)
	present = min(max(present, 0), len(all))
	for _, chunk := range batches(all[present:]) {
		if err := c.patchCheckRun(ctx, owner, repo, id, run, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) patchCheckRun(ctx context.Context, owner, repo string, id int64, run CheckRun, chunk []checkAnnotation) error {
	auth, err := c.auth(ctx)
	if err != nil {
		return err
	}
	body := checkRunBody{
		Status:     "completed",
		Conclusion: "neutral",
		Output:     checkOutput{Title: run.Title, Summary: run.Summary, Annotations: chunk},
	}
	endpoint := c.t.endpoint(nil, repoSegments(owner, repo, "check-runs", strconv.FormatInt(id, 10))...)
	if err := c.t.sendJSON(ctx, http.MethodPatch, endpoint, auth, body, nil); err != nil {
		return fmt.Errorf("github: update check run: %w", err)
	}
	return nil
}

func (c *Client) IssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"per_page": {strconv.Itoa(perPage)}}
	endpoint := c.t.endpoint(query, repoSegments(owner, repo, "issues", strconv.Itoa(number), "comments")...)
	return paginate(ctx, c.t, endpoint, auth, 0, decodeList[IssueComment])
}

func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) (int64, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return 0, err
	}
	var created struct {
		ID int64 `json:"id"`
	}
	endpoint := c.t.endpoint(nil, repoSegments(owner, repo, "issues", strconv.Itoa(number), "comments")...)
	if err := c.t.sendJSON(ctx, http.MethodPost, endpoint, auth, map[string]string{"body": body}, &created); err != nil {
		return 0, fmt.Errorf("github: create issue comment: %w", err)
	}
	return created.ID, nil
}

func (c *Client) UpdateIssueComment(ctx context.Context, owner, repo string, id int64, body string) error {
	auth, err := c.auth(ctx)
	if err != nil {
		return err
	}
	endpoint := c.t.endpoint(nil, repoSegments(owner, repo, "issues", "comments", strconv.FormatInt(id, 10))...)
	if err := c.t.sendJSON(ctx, http.MethodPatch, endpoint, auth, map[string]string{"body": body}, nil); err != nil {
		return fmt.Errorf("github: update issue comment: %w", err)
	}
	return nil
}

func (c *Client) WorkflowRuns(ctx context.Context, owner, repo string, limit int) ([]WorkflowRun, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > perPage {
		limit = perPage
	}
	query := url.Values{"status": {"completed"}, "per_page": {strconv.Itoa(limit)}}
	endpoint := c.t.endpoint(query, repoSegments(owner, repo, "actions", "runs")...)
	var payload struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	if _, err := c.t.getJSON(ctx, endpoint, auth, &payload); err != nil {
		return nil, err
	}
	if len(payload.WorkflowRuns) > limit {
		payload.WorkflowRuns = payload.WorkflowRuns[:limit]
	}
	return payload.WorkflowRuns, nil
}

func (c *Client) RunJobs(ctx context.Context, owner, repo string, runID int64) ([]RunJob, error) {
	auth, err := c.auth(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"filter": {"latest"}, "per_page": {strconv.Itoa(perPage)}}
	endpoint := c.t.endpoint(query, repoSegments(owner, repo, "actions", "runs", strconv.FormatInt(runID, 10), "jobs")...)
	return paginate(ctx, c.t, endpoint, auth, 0, func(resp *http.Response) ([]RunJob, error) {
		var payload struct {
			Jobs []RunJob `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return nil, err
		}
		return payload.Jobs, nil
	})
}
