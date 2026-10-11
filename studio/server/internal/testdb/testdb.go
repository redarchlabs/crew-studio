// Package testdb gives integration tests a freshly migrated Postgres schema.
package testdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"crewstudio/internal/db"
)

// EnvVar names the database the integration tests may wipe.
const EnvVar = "STUDIO_TEST_DATABASE_URL"

// New drops and recreates schema, applies migrations into it, and returns a pool bound to it.
// Each test package should use its own schema so packages can run in parallel.
// The test is skipped when STUDIO_TEST_DATABASE_URL is not set.
func New(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv(EnvVar)
	if url == "" {
		t.Skipf("%s not set; skipping Postgres integration test", EnvVar)
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE; CREATE SCHEMA "+quoted); err != nil {
		t.Fatalf("reset schema %s: %v", schema, err)
	}
	_ = admin.Close(ctx)

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
	return pool
}
