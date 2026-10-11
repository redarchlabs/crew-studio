package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
)

const questionsVersion = 29

// 0029 adds an agent's question settings (when the lead may ask the person during a hand-off) and the agent a question
// was asked for. Rolling it back removes them; applying it again restores the defaults.
func TestQuestionsMigration(t *testing.T) {
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
		AND ((table_name = 'human_questions' AND column_name = 'asked_for')
		  OR (table_name = 'agent_chat' AND column_name IN ('questions_enabled', 'question_rules', 'question_timeout_minutes',
		      'question_on_timeout', 'max_questions')))`
	if count(columns) != 6 {
		t.Fatal("question schema missing after up")
	}
	// The defaults: off, 30 minutes, carry on, 3 questions; out-of-range values are refused
	if _, err := pool.Exec(ctx, `INSERT INTO agents (name, role, goal, backstory) VALUES ('E2E question lead', 'Lead', 'g', 'b')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent_chat (agent_id) SELECT id FROM agents WHERE name = 'E2E question lead'`); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT count(*) FROM agent_chat WHERE NOT questions_enabled AND question_rules = '' AND question_timeout_minutes = 30
		AND question_on_timeout = 'proceed' AND max_questions = 3`) != 1 {
		t.Error("question defaults not applied")
	}
	for _, bad := range []string{`question_on_timeout = 'wait'`, `max_questions = 21`, `question_timeout_minutes = 0`} {
		if _, err := pool.Exec(ctx, `UPDATE agent_chat SET `+bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	if _, err := db.MigrateDownTo(ctx, pool, questionsVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if count(columns) != 0 {
		t.Error("question schema left after down")
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if count(columns) != 6 || count(`SELECT count(*) FROM agent_chat WHERE max_questions = 3`) != 1 {
		t.Error("question schema not restored after up again")
	}
}
