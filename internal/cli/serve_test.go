package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

var quantoEnv = []string{
	"QUANTO_DATABASE_URL", "QUANTO_LISTEN_ADDR", "QUANTO_APP_ID", "QUANTO_PRIVATE_KEY_FILE",
	"QUANTO_WEBHOOK_SECRET_FILE", "QUANTO_GITHUB_API_URL", "QUANTO_WORKER_CONCURRENCY",
	"QUANTO_ALLOW_PRIVATE_REPOS", "QUANTO_MAX_WORKFLOW_FILES", "QUANTO_LOG_LEVEL",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range quantoEnv {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

func TestServeUsage(t *testing.T) {
	clearEnv(t)
	tests := []struct {
		name string
		args []string
	}{
		{"missing role", []string{"serve"}},
		{"unknown role", []string{"serve", "--role", "api"}},
		{"positional", []string{"serve", "--role", "web", "extra"}},
		{"unknown flag", []string{"serve", "--port", "1"}},
		{"migrate positional", []string{"migrate", "now"}},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != exitUsage {
			t.Errorf("%s: exit = %d", tt.name, code)
		}
		if stdout != "" || !strings.Contains(stderr, "usage:") {
			t.Errorf("%s: stdout %q stderr %q", tt.name, stdout, stderr)
		}
	}
}

func TestUsageListsServeAndMigrate(t *testing.T) {
	_, _, stderr := run()
	for _, want := range []string{"quanto serve --role web|worker|all", "quanto migrate"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("usage lacks %q", want)
		}
	}
}

func TestServeConfigErrors(t *testing.T) {
	clearEnv(t)
	code, stdout, stderr := run("serve", "--role", "all")
	if code != exitError || stdout != "" {
		t.Fatalf("exit = %d stdout %q", code, stdout)
	}
	for _, want := range []string{"QUANTO_DATABASE_URL is required", "QUANTO_APP_ID is required", "QUANTO_PRIVATE_KEY_FILE is required", "QUANTO_WEBHOOK_SECRET_FILE is required"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q: %s", want, stderr)
		}
	}
}

func TestMigrateConfigErrors(t *testing.T) {
	clearEnv(t)
	code, _, stderr := run("migrate")
	if code != exitError || !strings.Contains(stderr, "QUANTO_DATABASE_URL is required") {
		t.Fatalf("exit = %d stderr %q", code, stderr)
	}
	t.Setenv("QUANTO_DATABASE_URL", "postgres://quanto:hunter2@127.0.0.1:1/quanto?connect_timeout=1")
	code, _, stderr = run("migrate")
	if code != exitError {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(stderr, "hunter2") {
		t.Fatalf("stderr leaks password: %s", stderr)
	}
}

func TestMigrateAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("QUANTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUANTO_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	schema := "quanto_cli_test_" + hex.EncodeToString(buf)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		if err := admin.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	clearEnv(t)
	t.Setenv("QUANTO_DATABASE_URL", u.String())
	code, stdout, stderr := run("migrate")
	if code != exitOK || stdout != "applied 0001_init.sql\n" {
		t.Fatalf("first migrate = %d %q %q", code, stdout, stderr)
	}
	code, stdout, stderr = run("migrate")
	if code != exitOK || stdout != "no migrations to apply\n" {
		t.Fatalf("second migrate = %d %q %q", code, stdout, stderr)
	}
}
