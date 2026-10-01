package action

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/BETAER-08/quanto/internal/github"
)

const (
	defaultMaxFiles = 50
	maxMaxFiles     = 200
	eventLimit      = 25 << 20
)

type config struct {
	owner       string
	repo        string
	number      int
	headSHA     string
	baseSHA     string
	token       string
	apiURL      string
	summaryPath string
	comment     bool
	estimate    bool
	maxFiles    int
}

func isPullRequestEvent(name string) bool {
	return name == "pull_request" || name == "pull_request_target"
}

func parseBool(name, raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%s must be true or false", name)
}

func loadConfig(getenv func(string) string) (config, error) {
	var errs []error
	cfg := config{
		token:       getenv("GITHUB_TOKEN"),
		apiURL:      strings.TrimSpace(getenv("GITHUB_API_URL")),
		summaryPath: getenv("GITHUB_STEP_SUMMARY"),
		maxFiles:    defaultMaxFiles,
	}
	if cfg.token == "" {
		errs = append(errs, errors.New("GITHUB_TOKEN is not set"))
	}
	if cfg.apiURL == "" {
		cfg.apiURL = github.DefaultBaseURL
	}
	owner, repo, ok := strings.Cut(getenv("GITHUB_REPOSITORY"), "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		errs = append(errs, errors.New("GITHUB_REPOSITORY must be owner/repo"))
	}
	cfg.owner, cfg.repo = owner, repo
	var err error
	if cfg.comment, err = parseBool("INPUT_COMMENT", getenv("INPUT_COMMENT")); err != nil {
		errs = append(errs, err)
	}
	if cfg.estimate, err = parseBool("INPUT_ESTIMATE", getenv("INPUT_ESTIMATE")); err != nil {
		errs = append(errs, err)
	}
	if raw := strings.TrimSpace(getenv("INPUT_MAX_FILES")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxMaxFiles {
			errs = append(errs, fmt.Errorf("INPUT_MAX_FILES must be an integer from 1 to %d", maxMaxFiles))
		} else {
			cfg.maxFiles = n
		}
	}
	if err := cfg.readEvent(getenv("GITHUB_EVENT_PATH")); err != nil {
		errs = append(errs, err)
	}
	return cfg, errors.Join(errs...)
}

func (cfg *config) readEvent(path string) error {
	if path == "" {
		return errors.New("GITHUB_EVENT_PATH is not set")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read event file: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(f, eventLimit+1))
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("read event file: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("read event file: %w", closeErr)
	}
	if len(data) > eventLimit {
		return errors.New("read event file: file exceeds 25 MiB")
	}
	var event struct {
		PullRequest *struct {
			Number int           `json:"number"`
			Head   github.GitRef `json:"head"`
			Base   github.GitRef `json:"base"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("decode event file: %w", err)
	}
	pr := event.PullRequest
	if pr == nil || pr.Number <= 0 || pr.Head.SHA == "" || pr.Base.SHA == "" {
		return errors.New("event file has no pull_request number, head sha and base sha")
	}
	cfg.number, cfg.headSHA, cfg.baseSHA = pr.Number, pr.Head.SHA, pr.Base.SHA
	return nil
}
