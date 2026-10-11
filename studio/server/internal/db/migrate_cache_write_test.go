package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
)

const cacheWriteVersion = 23

// 0023 adds cache-write prices (Claude models get 1.25 times input) and cache-write tokens on LLM calls; rolling it
// back removes both columns.
func TestCacheWriteMigration(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var claudeOK, othersNull bool
	if err := pool.QueryRow(ctx, `SELECT
		bool_and(cache_write_per_mtok = round((input_per_mtok * 1.25)::numeric, 6)) FILTER (WHERE model ILIKE 'claude-%'),
		bool_and(cache_write_per_mtok IS NULL) FILTER (WHERE model NOT ILIKE 'claude-%')
		FROM model_prices`).Scan(&claudeOK, &othersNull); err != nil {
		t.Fatal(err)
	}
	if !claudeOK || !othersNull {
		t.Errorf("claude priced = %v, others null = %v", claudeOK, othersNull)
	}
	columns := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
			AND ((table_name = 'model_prices' AND column_name = 'cache_write_per_mtok')
			  OR (table_name = 'llm_calls' AND column_name = 'cache_write_tokens'))`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := columns(); n != 2 {
		t.Fatalf("columns after up = %d", n)
	}
	if _, err := db.MigrateDownTo(ctx, pool, cacheWriteVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if n := columns(); n != 0 {
		t.Errorf("columns after down = %d", n)
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
