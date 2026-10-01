package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Purpose int

const (
	PurposeServe Purpose = iota
	PurposeMigrate
)

const (
	EnvDatabaseURL       = "QUANTO_DATABASE_URL"
	EnvListenAddr        = "QUANTO_LISTEN_ADDR"
	EnvAppID             = "QUANTO_APP_ID"
	EnvPrivateKeyFile    = "QUANTO_PRIVATE_KEY_FILE"
	EnvWebhookSecretFile = "QUANTO_WEBHOOK_SECRET_FILE"
	EnvGitHubAPIURL      = "QUANTO_GITHUB_API_URL"
	EnvWorkerConcurrency = "QUANTO_WORKER_CONCURRENCY"
	EnvAllowPrivateRepos = "QUANTO_ALLOW_PRIVATE_REPOS"
	EnvMaxWorkflowFiles  = "QUANTO_MAX_WORKFLOW_FILES"
	EnvLogLevel          = "QUANTO_LOG_LEVEL"
)

const (
	DefaultListenAddr        = ":8080"
	DefaultGitHubAPIURL      = "https://api.github.com"
	DefaultWorkerConcurrency = 4
	DefaultMaxWorkflowFiles  = 50
)

const redacted = "[redacted]"

type Config struct {
	DatabaseURL       string
	ListenAddr        string
	AppID             int64
	PrivateKeyFile    string
	PrivateKey        []byte
	WebhookSecretFile string
	WebhookSecret     []byte
	GitHubAPIURL      string
	WorkerConcurrency int
	AllowPrivateRepos bool
	MaxWorkflowFiles  int
	LogLevel          slog.Level
}

func FromEnv(purpose Purpose) (*Config, error) {
	return Load(os.LookupEnv, os.ReadFile, purpose)
}

func Load(lookup func(string) (string, bool), readFile func(string) ([]byte, error), purpose Purpose) (*Config, error) {
	get := func(name string) string {
		v, ok := lookup(name)
		if !ok {
			return ""
		}
		return strings.TrimSpace(v)
	}
	var errs []error
	cfg := &Config{
		ListenAddr:        DefaultListenAddr,
		GitHubAPIURL:      DefaultGitHubAPIURL,
		WorkerConcurrency: DefaultWorkerConcurrency,
		MaxWorkflowFiles:  DefaultMaxWorkflowFiles,
		LogLevel:          slog.LevelInfo,
	}

	cfg.DatabaseURL = get(EnvDatabaseURL)
	if cfg.DatabaseURL == "" {
		errs = append(errs, fmt.Errorf("%s is required", EnvDatabaseURL))
	}

	if v := get(EnvListenAddr); v != "" {
		cfg.ListenAddr = v
	}

	if v := get(EnvAppID); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive integer", EnvAppID))
		} else {
			cfg.AppID = id
		}
	} else if purpose == PurposeServe {
		errs = append(errs, fmt.Errorf("%s is required", EnvAppID))
	}

	cfg.PrivateKeyFile = get(EnvPrivateKeyFile)
	if cfg.PrivateKeyFile != "" {
		data, err := readSecretFile(readFile, cfg.PrivateKeyFile)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", EnvPrivateKeyFile, err))
		} else {
			cfg.PrivateKey = data
		}
	} else if purpose == PurposeServe {
		errs = append(errs, fmt.Errorf("%s is required", EnvPrivateKeyFile))
	}

	cfg.WebhookSecretFile = get(EnvWebhookSecretFile)
	if cfg.WebhookSecretFile != "" {
		data, err := readSecretFile(readFile, cfg.WebhookSecretFile)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", EnvWebhookSecretFile, err))
		} else {
			cfg.WebhookSecret = data
		}
	} else if purpose == PurposeServe {
		errs = append(errs, fmt.Errorf("%s is required", EnvWebhookSecretFile))
	}

	if v := get(EnvGitHubAPIURL); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			errs = append(errs, fmt.Errorf("%s must be an absolute http or https URL without credentials, query or fragment", EnvGitHubAPIURL))
		} else {
			cfg.GitHubAPIURL = strings.TrimRight(v, "/")
		}
	}

	if v := get(EnvWorkerConcurrency); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 64 {
			errs = append(errs, fmt.Errorf("%s must be an integer between 1 and 64", EnvWorkerConcurrency))
		} else {
			cfg.WorkerConcurrency = n
		}
	}

	if v := get(EnvAllowPrivateRepos); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be true or false", EnvAllowPrivateRepos))
		} else {
			cfg.AllowPrivateRepos = b
		}
	}

	if v := get(EnvMaxWorkflowFiles); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			errs = append(errs, fmt.Errorf("%s must be an integer between 1 and 200", EnvMaxWorkflowFiles))
		} else {
			cfg.MaxWorkflowFiles = n
		}
	}

	if v := get(EnvLogLevel); v != "" {
		level, ok := parseLevel(v)
		if !ok {
			errs = append(errs, fmt.Errorf("%s must be one of debug, info, warn, error", EnvLogLevel))
		} else {
			cfg.LogLevel = level
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func readSecretFile(readFile func(string) ([]byte, error), path string) ([]byte, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secret file: %w", err)
	}
	trimmed := strings.TrimRight(string(data), "\r\n")
	if trimmed == "" {
		return nil, fmt.Errorf("secret file %q is empty", path)
	}
	return []byte(trimmed), nil
}

func parseLevel(v string) (slog.Level, bool) {
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return 0, false
}

func levelName(l slog.Level) string {
	switch l {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	}
	return "info"
}

func secretState(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	return redacted
}

func (c Config) String() string {
	db := ""
	if c.DatabaseURL != "" {
		db = redacted
	}
	return fmt.Sprintf(
		"Config{DatabaseURL:%s ListenAddr:%s AppID:%d PrivateKeyFile:%s PrivateKey:%s WebhookSecretFile:%s WebhookSecret:%s GitHubAPIURL:%s WorkerConcurrency:%d AllowPrivateRepos:%t MaxWorkflowFiles:%d LogLevel:%s}",
		db, c.ListenAddr, c.AppID, c.PrivateKeyFile, secretState(c.PrivateKey), c.WebhookSecretFile, secretState(c.WebhookSecret),
		c.GitHubAPIURL, c.WorkerConcurrency, c.AllowPrivateRepos, c.MaxWorkflowFiles, levelName(c.LogLevel),
	)
}

func (c Config) GoString() string {
	return c.String()
}

func (c Config) LogValue() slog.Value {
	db := ""
	if c.DatabaseURL != "" {
		db = redacted
	}
	return slog.GroupValue(
		slog.String("database_url", db),
		slog.String("listen_addr", c.ListenAddr),
		slog.Int64("app_id", c.AppID),
		slog.String("private_key_file", c.PrivateKeyFile),
		slog.String("private_key", secretState(c.PrivateKey)),
		slog.String("webhook_secret_file", c.WebhookSecretFile),
		slog.String("webhook_secret", secretState(c.WebhookSecret)),
		slog.String("github_api_url", c.GitHubAPIURL),
		slog.Int("worker_concurrency", c.WorkerConcurrency),
		slog.Bool("allow_private_repos", c.AllowPrivateRepos),
		slog.Int("max_workflow_files", c.MaxWorkflowFiles),
		slog.String("log_level", levelName(c.LogLevel)),
	)
}
