package app

import (
	"strings"

	"github.com/BETAER-08/quanto/internal/store"
)

const (
	KindAnalyzePR          = "analyze_pr"
	KindIngestWorkflowRun  = "ingest_workflow_run"
	KindBackfillRepository = "backfill_repo"
)

type AnalyzePayload struct {
	InstallationID int64  `json:"installation_id"`
	RepositoryID   int64  `json:"repository_id"`
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	Number         int    `json:"number"`
	HeadSHA        string `json:"head_sha"`
	BaseSHA        string `json:"base_sha"`
}

type IngestPayload struct {
	InstallationID int64  `json:"installation_id"`
	RepositoryID   int64  `json:"repository_id"`
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	RunID          int64  `json:"run_id"`
	WorkflowPath   string `json:"workflow_path"`
}

type BackfillPayload struct {
	InstallationID int64  `json:"installation_id"`
	RepositoryID   int64  `json:"repository_id"`
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
}

type accountPayload struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type repositoryPayload struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	FullName string         `json:"full_name"`
	Private  bool           `json:"private"`
	Owner    accountPayload `json:"owner"`
}

type installationPayload struct {
	ID      int64          `json:"id"`
	Account accountPayload `json:"account"`
}

type shaPayload struct {
	SHA string `json:"sha"`
}

type pullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Number int        `json:"number"`
		Head   shaPayload `json:"head"`
		Base   shaPayload `json:"base"`
	} `json:"pull_request"`
	Repository   repositoryPayload   `json:"repository"`
	Installation installationPayload `json:"installation"`
}

type workflowRunEvent struct {
	Action      string `json:"action"`
	WorkflowRun struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	} `json:"workflow_run"`
	Repository   repositoryPayload   `json:"repository"`
	Installation installationPayload `json:"installation"`
}

type installationEvent struct {
	Action       string              `json:"action"`
	Installation installationPayload `json:"installation"`
	Repositories []repositoryPayload `json:"repositories"`
}

type installationRepositoriesEvent struct {
	Action              string              `json:"action"`
	Installation        installationPayload `json:"installation"`
	RepositoriesAdded   []repositoryPayload `json:"repositories_added"`
	RepositoriesRemoved []repositoryPayload `json:"repositories_removed"`
}

func (r repositoryPayload) model(fallbackOwner string) store.Repository {
	owner := r.Owner.Login
	if owner == "" {
		if i := strings.Index(r.FullName, "/"); i > 0 {
			owner = r.FullName[:i]
		} else {
			owner = fallbackOwner
		}
	}
	return store.Repository{ID: r.ID, Owner: owner, Name: r.Name, Private: r.Private}
}

func workflowPathFromRun(path string) string {
	if i := strings.Index(path, "@"); i >= 0 {
		return path[:i]
	}
	return path
}
