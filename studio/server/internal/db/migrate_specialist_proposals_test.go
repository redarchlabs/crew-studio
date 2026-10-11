package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
)

const specialistProposalsVersion = 30

// 0030 adds an agent's "Specialists may propose changes" setting, off by default. Rolling it back removes it; applying
// it again restores the default.
func TestSpecialistProposalsMigration(t *testing.T) {
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
		AND table_name = 'agent_chat' AND column_name = 'specialists_may_propose'`
	const index = `SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'conversation_messages_handoff_seq_idx'`
	const oldIndex = `SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'conversation_messages_handoff_idx'`
	if count(columns) != 1 || count(index) != 1 || count(oldIndex) != 0 {
		t.Fatal("specialist proposal schema missing after up")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents (name, role, goal, backstory) VALUES ('E2E proposing lead', 'Lead', 'g', 'b')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent_chat (agent_id) SELECT id FROM agents WHERE name = 'E2E proposing lead'`); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT count(*) FROM agent_chat WHERE NOT specialists_may_propose`) != 1 {
		t.Error("the setting is not off by default")
	}
	if _, err := db.MigrateDownTo(ctx, pool, specialistProposalsVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if count(columns) != 0 || count(index) != 0 || count(oldIndex) != 1 {
		t.Error("specialist proposal schema left after down")
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if count(columns) != 1 || count(`SELECT count(*) FROM agent_chat WHERE NOT specialists_may_propose`) != 1 {
		t.Error("specialist proposal schema not restored after up again")
	}
}
