package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
)

const followUpVersion = 22

// 0022 links conversations to work orders and adds work order feedback; rolling it back removes both.
func TestWorkOrderFollowUpMigration(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	const column = `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
		AND table_name = 'conversations' AND column_name = 'work_order_id'`
	const table = `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'work_order_feedback'`
	if count(column) != 1 || count(table) != 1 {
		t.Fatal("follow-up schema missing after up")
	}
	if _, err := db.MigrateDownTo(ctx, pool, followUpVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if count(column) != 0 || count(table) != 0 {
		t.Error("follow-up schema left after down")
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
