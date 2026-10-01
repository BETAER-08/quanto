package store

import (
	"context"
	"sync"
	"testing"
)

func TestMigrateIdempotent(t *testing.T) {
	dsn := testDSN(t)
	s := openSchema(t, dsn, testSchema(t, dsn))
	ctx := context.Background()
	first, err := s.Migrate(ctx)
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if len(first) != 1 || first[0] != "0001_init.sql" {
		t.Fatalf("first migrate applied %v", first)
	}
	second, err := s.Migrate(ctx)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second migrate applied %v", second)
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("schema_migrations rows = %d", n)
	}
}

func TestMigrateConcurrent(t *testing.T) {
	dsn := testDSN(t)
	schema := testSchema(t, dsn)
	stores := []*Store{openSchema(t, dsn, schema), openSchema(t, dsn, schema)}
	results := make([][]string, len(stores))
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			results[i], errs[i] = s.Migrate(context.Background())
		}(i, s)
	}
	wg.Wait()
	total := 0
	for i := range stores {
		if errs[i] != nil {
			t.Fatalf("migrate %d: %v", i, errs[i])
		}
		total += len(results[i])
	}
	if total != 1 {
		t.Fatalf("applied %d migrations in total, results %v", total, results)
	}
}

func TestMigrationSchemaTables(t *testing.T) {
	s := testStore(t)
	want := []string{"analyses", "installations", "job_runs", "job_stats", "pr_comments", "queue_jobs", "repositories", "schema_migrations", "webhook_deliveries"}
	rows, err := s.pool.Query(context.Background(), "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() ORDER BY table_name")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tables = %v, want %v", got, want)
		}
	}
}
