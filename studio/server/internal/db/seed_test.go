package db_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"crewstudio/internal/db"
	"crewstudio/internal/testdb"
)

const libraryAgentIDs = `id::text LIKE '22222222-0000-4000-8000-0000000001%'`

func TestAgentLibrarySeed(t *testing.T) {
	pool := testdb.New(t, "db_seed_test")
	ctx := context.Background()

	var count, delegating, withoutArea, withoutLLM int
	err := pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE allow_delegation),
		count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM agent_areas x WHERE x.agent_id = agents.id)),
		count(*) FILTER (WHERE llm_id IS NULL)
		FROM agents WHERE `+libraryAgentIDs).Scan(&count, &delegating, &withoutArea, &withoutLLM)
	if err != nil {
		t.Fatal(err)
	}
	if count != 27 || delegating != 0 || withoutArea != 0 || withoutLLM != 0 {
		t.Errorf("library agents = %d, delegating %d, without area %d, without LLM %d", count, delegating, withoutArea, withoutLLM)
	}

	// Every tool an agent is given must exist, or crews using it cannot run
	raw, err := os.ReadFile("../../../runner/tools_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog []struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, tool := range catalog {
		known[tool.Key] = true
	}
	rows, err := pool.Query(ctx, `SELECT DISTINCT unnest(tools) FROM agents WHERE `+libraryAgentIDs)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		if !known[key] {
			t.Errorf("library agent uses unknown tool %q", key)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentLibrarySkipsTakenNames(t *testing.T) {
	pool := testdb.New(t, "db_seed_rerun_test")
	ctx := context.Background()
	if _, err := db.MigrateDownTo(ctx, pool, 6); err != nil {
		t.Fatal(err)
	}
	// Someone already has an agent called "QA Engineer": the seed must skip it, not fail
	if _, err := pool.Exec(ctx, `INSERT INTO agents (name, role, goal, backstory) VALUES ('QA Engineer', 'Mine', 'g', 'b')`); err != nil {
		t.Fatal(err)
	}
	if applied, err := db.Migrate(ctx, pool); err != nil || !slices.Contains(applied, "0007_agent_library") {
		t.Fatalf("applying the library seed: applied %v, err %v", applied, err)
	}
	var role string
	if err := pool.QueryRow(ctx, `SELECT role FROM agents WHERE name = 'QA Engineer'`).Scan(&role); err != nil || role != "Mine" {
		t.Errorf("existing QA Engineer was changed: role %q, %v", role, err)
	}
}

const videoProducerID = "22222222-0000-4000-8000-000000000126"

func TestVideoGenerationSeed(t *testing.T) {
	pool := testdb.New(t, "db_seed_video_test")
	ctx := context.Background()

	var tools []string
	var maxIter int
	var role string
	if err := pool.QueryRow(ctx, `SELECT tools, max_iter, role FROM agents WHERE id = $1`, videoProducerID).Scan(&tools, &maxIter, &role); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tools, "generate_video") || maxIter != 20 || role != "Video Producer (Gemini Omni Flash)" {
		t.Errorf("video producer = tools %v, max_iter %d, role %q", tools, maxIter, role)
	}
	var output float64
	if err := pool.QueryRow(ctx, `SELECT output_per_mtok FROM model_prices WHERE model = 'gemini-omni-1.1-flash'`).Scan(&output); err != nil || output != 17.50 {
		t.Errorf("omni price = %v, %v", output, err)
	}
}

func TestVideoGenerationKeepsYourEdits(t *testing.T) {
	pool := testdb.New(t, "db_seed_video_edits_test")
	ctx := context.Background()
	if _, err := db.MigrateDownTo(ctx, pool, 10); err != nil {
		t.Fatal(err)
	}
	// Someone rewrote the Video Producer's goal after 0007: 0011 must not overwrite it, and must not add the tool twice
	if _, err := pool.Exec(ctx, `UPDATE agents SET goal = 'My own goal', tools = array_append(tools, 'generate_video')
		WHERE id = $1`, videoProducerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var goal string
	var tools []string
	if err := pool.QueryRow(ctx, `SELECT goal, tools FROM agents WHERE id = $1`, videoProducerID).Scan(&goal, &tools); err != nil {
		t.Fatal(err)
	}
	if goal != "My own goal" {
		t.Errorf("goal overwritten: %q", goal)
	}
	if n := len(slices.DeleteFunc(slices.Clone(tools), func(s string) bool { return s != "generate_video" })); n != 1 {
		t.Errorf("generate_video appears %d times in %v", n, tools)
	}
}
