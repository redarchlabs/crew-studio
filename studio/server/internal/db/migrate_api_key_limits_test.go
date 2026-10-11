package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"crewstudio/internal/db"
)

const apiKeyLimitsVersion = 32

// 0032 adds per-key limits. Keys made before it keep everything on both channels; a key must work somewhere.
// Rolling it back drops the limits and keeps the keys.
func TestAPIKeyLimitsMigration(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MigrateDownTo(ctx, pool, apiKeyLimitsVersion-1); err != nil {
		t.Fatal(err)
	}
	exec := func(q string) error {
		_, err := pool.Exec(ctx, q)
		return err
	}
	if err := exec(`INSERT INTO users (id, kind, name) VALUES ('99999999-0000-4000-8000-000000000001', 'service', 'ERP');
		INSERT INTO api_keys (user_id, name, prefix, secret_hash) VALUES ('99999999-0000-4000-8000-000000000001', 'old', 'p1', 'h')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var api, mcp, noPerms, noAreas bool
	if err := pool.QueryRow(ctx, `SELECT allow_api, allow_mcp, permissions IS NULL, area_ids IS NULL FROM api_keys WHERE prefix = 'p1'`).
		Scan(&api, &mcp, &noPerms, &noAreas); err != nil {
		t.Fatal(err)
	}
	if !api || !mcp || !noPerms || !noAreas {
		t.Errorf("existing key: api %v mcp %v unlimited permissions %v unlimited areas %v", api, mcp, noPerms, noAreas)
	}
	for _, bad := range []string{
		`UPDATE api_keys SET allow_api = false, allow_mcp = false`,
		`UPDATE api_keys SET permissions = '{}'`,
		`UPDATE api_keys SET area_ids = '{}'`,
	} {
		if exec(bad) == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
	for _, col := range []string{"runs.api_key_id", "schedules.run_as_key", "triggers.run_as_key"} {
		if !hasColumn(t, pool, col) {
			t.Errorf("%s missing after up", col)
		}
	}
	if _, err := db.MigrateDownTo(ctx, pool, apiKeyLimitsVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_keys`).Scan(&n); err != nil || n != 1 {
		t.Errorf("keys after down = %d, %v", n, err)
	}
	if hasColumn(t, pool, "runs.api_key_id") {
		t.Error("runs.api_key_id left after down")
	}
}

func hasColumn(t *testing.T, pool *pgxpool.Pool, tableColumn string) bool {
	t.Helper()
	table, column, _ := strings.Cut(tableColumn, ".")
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`, table, column).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}
