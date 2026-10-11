package db_test

import (
	"context"
	"fmt"
	"testing"

	"crewstudio/internal/db"
)

const handoffsVersion = 28

// 0028 adds hand-offs: the run kind, the chat_handoffs table, the result message's handoff_id and the agent's hand-off
// settings, and replaces the messages' role check with one that also covers hand-offs (summaries may still carry a
// run). Rolling it back removes them, turns hand-off runs into chat runs and puts the old check back.
func TestHandoffsMigration(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	const columns = `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
		AND ((table_name = 'conversation_messages' AND column_name = 'handoff_id')
		  OR (table_name = 'agent_chat' AND column_name IN ('handoffs_enabled', 'handoff_budget_usd', 'handoff_timeout_minutes', 'max_active_handoffs')))`
	const table = `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'chat_handoffs'`
	const constraint = `SELECT count(*) FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = current_schema() AND t.relname = 'conversation_messages' AND c.conname = '%s'`
	if count(columns) != 5 || count(table) != 1 || count(fmt.Sprintf(constraint, "conversation_messages_assistant_only_check")) != 1 ||
		count(fmt.Sprintf(constraint, "conversation_messages_check1")) != 0 {
		t.Fatal("hand-off schema missing after up")
	}
	// A run of kind handoff is allowed; an unknown kind is not
	if _, err := pool.Exec(ctx, `INSERT INTO runs (crew_name, status, inputs, budget_usd, spec, kind) VALUES ('x', 'succeeded', '{}', 1, '{}', 'handoff')`); err != nil {
		t.Fatalf("hand-off run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (crew_name, status, inputs, budget_usd, spec, kind) VALUES ('x', 'succeeded', '{}', 1, '{}', 'other')`); err == nil {
		t.Error("an unknown kind was accepted")
	}
	if _, err := db.MigrateDownTo(ctx, pool, handoffsVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if count(columns) != 0 || count(table) != 0 || count(fmt.Sprintf(constraint, "conversation_messages_check1")) != 1 {
		t.Error("hand-off schema left after down")
	}
	if count(`SELECT count(*) FROM runs WHERE kind = 'chat'`) != 1 {
		t.Error("the hand-off run was not kept as a chat run")
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
