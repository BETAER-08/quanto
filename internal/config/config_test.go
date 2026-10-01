package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
)

const (
	testKey     = "-----BEGIN RSA PRIVATE KEY-----\nSECRETKEYMATERIAL\n-----END RSA PRIVATE KEY-----"
	testSecret  = "hook-secret-value"
	testDBPass  = "dbpassword123"
	testDBURL   = "postgres://quanto:" + testDBPass + "@db:5432/quanto"
	keyPath     = "/run/secrets/key.pem"
	secretPath  = "/run/secrets/webhook"
	missingPath = "/run/secrets/missing"
)

func files() map[string][]byte {
	return map[string][]byte{
		keyPath:              []byte(testKey + "\n"),
		secretPath:           []byte(testSecret + "\r\n"),
		"/run/secrets/empty": []byte("\n"),
	}
}

func reader(m map[string][]byte) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if v, ok := m[p]; ok {
			return v, nil
		}
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
}

func lookup(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func fullEnv() map[string]string {
	return map[string]string{
		EnvDatabaseURL:       testDBURL,
		EnvAppID:             "12345",
		EnvPrivateKeyFile:    keyPath,
		EnvWebhookSecretFile: secretPath,
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(lookup(fullEnv()), reader(files()), PurposeServe)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.GitHubAPIURL != "https://api.github.com" {
		t.Errorf("GitHubAPIURL = %q", cfg.GitHubAPIURL)
	}
	if cfg.WorkerConcurrency != 4 {
		t.Errorf("WorkerConcurrency = %d", cfg.WorkerConcurrency)
	}
	if cfg.AllowPrivateRepos {
		t.Error("AllowPrivateRepos = true")
	}
	if cfg.MaxWorkflowFiles != 50 {
		t.Errorf("MaxWorkflowFiles = %d", cfg.MaxWorkflowFiles)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.AppID != 12345 {
		t.Errorf("AppID = %d", cfg.AppID)
	}
	if string(cfg.PrivateKey) != testKey {
		t.Errorf("PrivateKey not trimmed: %q", cfg.PrivateKey)
	}
	if string(cfg.WebhookSecret) != testSecret {
		t.Errorf("WebhookSecret not trimmed: %q", cfg.WebhookSecret)
	}
	if cfg.DatabaseURL != testDBURL {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestOverrides(t *testing.T) {
	env := fullEnv()
	env[EnvListenAddr] = "127.0.0.1:9000"
	env[EnvGitHubAPIURL] = "http://127.0.0.1:3000/api/v3/"
	env[EnvWorkerConcurrency] = "64"
	env[EnvAllowPrivateRepos] = "true"
	env[EnvMaxWorkflowFiles] = "200"
	env[EnvLogLevel] = "DEBUG"
	cfg, err := Load(lookup(env), reader(files()), PurposeServe)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" || cfg.GitHubAPIURL != "http://127.0.0.1:3000/api/v3" ||
		cfg.WorkerConcurrency != 64 || !cfg.AllowPrivateRepos || cfg.MaxWorkflowFiles != 200 || cfg.LogLevel != slog.LevelDebug {
		t.Errorf("unexpected config: %s", cfg)
	}
}

func TestLogLevels(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"Error", slog.LevelError},
	}
	for _, tt := range tests {
		env := fullEnv()
		env[EnvLogLevel] = tt.in
		cfg, err := Load(lookup(env), reader(files()), PurposeServe)
		if err != nil {
			t.Fatalf("%s: %v", tt.in, err)
		}
		if cfg.LogLevel != tt.want {
			t.Errorf("%s: got %v", tt.in, cfg.LogLevel)
		}
	}
}

func TestPurposeRequirements(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		purpose Purpose
		wantErr []string
	}{
		{"serve empty", map[string]string{}, PurposeServe, []string{EnvDatabaseURL, EnvAppID, EnvPrivateKeyFile, EnvWebhookSecretFile}},
		{"migrate empty", map[string]string{}, PurposeMigrate, []string{EnvDatabaseURL}},
		{"migrate db only", map[string]string{EnvDatabaseURL: testDBURL}, PurposeMigrate, nil},
		{"serve full", fullEnv(), PurposeServe, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(lookup(tt.env), reader(files()), tt.purpose)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			for _, w := range tt.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not mention %s", err, w)
				}
			}
			if tt.purpose == PurposeMigrate && strings.Contains(err.Error(), EnvAppID) {
				t.Errorf("migrate should not require %s: %v", EnvAppID, err)
			}
		})
	}
}

func TestAllErrorsReportedAtOnce(t *testing.T) {
	env := map[string]string{
		EnvDatabaseURL:       testDBURL,
		EnvAppID:             "abc",
		EnvPrivateKeyFile:    missingPath,
		EnvWebhookSecretFile: "/run/secrets/empty",
		EnvGitHubAPIURL:      "ftp://example.com",
		EnvWorkerConcurrency: "65",
		EnvAllowPrivateRepos: "maybe",
		EnvMaxWorkflowFiles:  "0",
		EnvLogLevel:          "verbose",
	}
	_, err := Load(lookup(env), reader(files()), PurposeServe)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, name := range []string{EnvAppID, EnvPrivateKeyFile, EnvWebhookSecretFile, EnvGitHubAPIURL, EnvWorkerConcurrency, EnvAllowPrivateRepos, EnvMaxWorkflowFiles, EnvLogLevel} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not mention %s: %v", name, err)
		}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file error not wrapped: %v", err)
	}
}

func TestInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"app id zero", EnvAppID, "0"},
		{"app id negative", EnvAppID, "-3"},
		{"concurrency zero", EnvWorkerConcurrency, "0"},
		{"concurrency text", EnvWorkerConcurrency, "four"},
		{"max files over", EnvMaxWorkflowFiles, "201"},
		{"api url relative", EnvGitHubAPIURL, "/api"},
		{"api url credentials", EnvGitHubAPIURL, "https://user:pass@example.com"},
		{"api url query", EnvGitHubAPIURL, "https://example.com/?a=b"},
		{"bool", EnvAllowPrivateRepos, "yes please"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := fullEnv()
			env[tt.key] = tt.value
			_, err := Load(lookup(env), reader(files()), PurposeServe)
			if err == nil {
				t.Fatalf("expected error for %s=%q", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Errorf("error does not mention %s: %v", tt.key, err)
			}
			if strings.Contains(err.Error(), "pass@") {
				t.Errorf("error leaks credentials: %v", err)
			}
		})
	}
}

func TestBoundaries(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{EnvWorkerConcurrency, "1"},
		{EnvWorkerConcurrency, "64"},
		{EnvMaxWorkflowFiles, "1"},
		{EnvMaxWorkflowFiles, "200"},
		{EnvAllowPrivateRepos, "false"},
	}
	for _, tt := range tests {
		env := fullEnv()
		env[tt.key] = tt.value
		if _, err := Load(lookup(env), reader(files()), PurposeServe); err != nil {
			t.Errorf("%s=%s: %v", tt.key, tt.value, err)
		}
	}
}

func TestStringRedactsSecrets(t *testing.T) {
	cfg, err := Load(lookup(fullEnv()), reader(files()), PurposeServe)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{
		"String":   cfg.String(),
		"%v ptr":   fmt.Sprintf("%v", cfg),
		"%v value": fmt.Sprintf("%v", *cfg),
		"%+v":      fmt.Sprintf("%+v", *cfg),
		"%#v":      fmt.Sprintf("%#v", *cfg),
		"%s":       fmt.Sprintf("%s", cfg),
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "config", cfg)
	outputs["slog"] = buf.String()
	for name, out := range outputs {
		for _, secret := range []string{testDBPass, "SECRETKEYMATERIAL", testSecret} {
			if strings.Contains(out, secret) {
				t.Errorf("%s leaks secret %q: %s", name, secret, out)
			}
		}
		if !strings.Contains(out, "[redacted]") {
			t.Errorf("%s does not show [redacted]: %s", name, out)
		}
		if !strings.Contains(out, keyPath) {
			t.Errorf("%s should show key file path: %s", name, out)
		}
	}
}

func TestErrorsDoNotLeakSecrets(t *testing.T) {
	env := fullEnv()
	env[EnvAppID] = "x"
	_, err := Load(lookup(env), reader(files()), PurposeServe)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, secret := range []string{testDBPass, "SECRETKEYMATERIAL", testSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks secret %q: %v", secret, err)
		}
	}
}

func TestPoolSize(t *testing.T) {
	tests := []struct {
		concurrency int
		want        int32
	}{
		{1, 5},
		{DefaultWorkerConcurrency, 8},
		{64, 68},
	}
	for _, tt := range tests {
		cfg := Config{WorkerConcurrency: tt.concurrency}
		if got := cfg.PoolSize(); got != tt.want {
			t.Errorf("PoolSize(%d) = %d, want %d", tt.concurrency, got, tt.want)
		}
	}
}
