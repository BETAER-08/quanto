package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("QUANTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUANTO_TEST_DATABASE_URL is not set")
	}
	return dsn
}

func testSchema(t *testing.T, dsn string) string {
	t.Helper()
	ctx := context.Background()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random schema name: %v", err)
	}
	schema := "quanto_test_" + hex.EncodeToString(buf)
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
			t.Errorf("close admin connection: %v", err)
		}
	})
	return schema
}

func openSchema(t *testing.T, dsn, schema string) *Store {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	s, err := OpenConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := testDSN(t)
	s := openSchema(t, dsn, testSchema(t, dsn))
	if _, err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

func mustExec(t *testing.T, s *Store, sql string, args ...any) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func seedRepo(t *testing.T, s *Store, installationID, repoID int64) {
	t.Helper()
	ctx := context.Background()
	if err := s.UpsertInstallation(ctx, Installation{ID: installationID, AccountLogin: "octo", AccountType: "Organization"}); err != nil {
		t.Fatalf("upsert installation: %v", err)
	}
	if err := s.UpsertRepositories(ctx, installationID, []Repository{{ID: repoID, Owner: "octo", Name: "repo"}}); err != nil {
		t.Fatalf("upsert repositories: %v", err)
	}
}

type jobState struct {
	status    string
	attempts  int
	lastError string
	delay     float64
}

func readJob(t *testing.T, s *Store, id int64) jobState {
	t.Helper()
	var st jobState
	var lastError *string
	err := s.pool.QueryRow(context.Background(), `SELECT status, attempts, last_error, EXTRACT(EPOCH FROM run_after - now())::float8
FROM queue_jobs WHERE id = $1`, id).Scan(&st.status, &st.attempts, &lastError, &st.delay)
	if err != nil {
		t.Fatalf("read job %d: %v", id, err)
	}
	if lastError != nil {
		st.lastError = *lastError
	}
	return st
}
