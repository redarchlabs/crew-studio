package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
)

const humanInputVersion = 21

// 0021 adds crew escalation settings and the questions agents ask; rolling it back drops both tables.
func TestHumanInputMigration(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tables := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema()
			AND table_name IN ('crew_escalation', 'human_questions')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := tables(); n != 2 {
		t.Fatalf("tables after up = %d", n)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crew_escalation (crew_id, timeout_minutes) SELECT id, 0 FROM crews LIMIT 1`); err == nil {
		t.Error("a zero timeout was stored")
	}
	if _, err := db.MigrateDownTo(ctx, pool, humanInputVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if n := tables(); n != 0 {
		t.Errorf("tables after down = %d", n)
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
