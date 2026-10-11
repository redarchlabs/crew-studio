package db_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"crewstudio/internal/model"
	"crewstudio/internal/testdb"
)

func TestPMOTemplateSeed(t *testing.T) {
	pool := testdb.New(t, "db_seed_pmo_test")
	ctx := context.Background()
	var agents, tasks int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM crew_agents WHERE crew_id = '44444444-0000-4000-8000-000000000201'),
		(SELECT count(*) FROM crew_tasks WHERE crew_id = '44444444-0000-4000-8000-000000000201')`).Scan(&agents, &tasks); err != nil {
		t.Fatal(err)
	}
	if agents != 2 || tasks != 2 {
		t.Errorf("template crew has %d agents and %d tasks", agents, tasks)
	}

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
	rows, err := pool.Query(ctx, `SELECT name, tools, llm_id IS NOT NULL FROM agents WHERE id::text LIKE '22222222-0000-4000-8000-0000000002%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name string
		var tools []string
		var hasLLM bool
		if err := rows.Scan(&name, &tools, &hasLLM); err != nil {
			t.Fatal(err)
		}
		n++
		if !hasLLM {
			t.Errorf("%s has no LLM", name)
		}
		for _, key := range tools {
			if !known[key] {
				t.Errorf("%s uses unknown tool %q", name, key)
			}
			if key == "tracker_create_issue" {
				t.Errorf("%s may create issues; the PMO only proposes them", name)
			}
		}
	}
	if n != 2 {
		t.Errorf("template agents = %d", n)
	}

	// Task text must not hold {placeholders}: the crew declares no inputs
	trows, err := pool.Query(ctx, `SELECT description || expected_output FROM tasks WHERE id::text LIKE '33333333-0000-4000-8000-0000000002%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer trows.Close()
	for trows.Next() {
		var text string
		if err := trows.Scan(&text); err != nil {
			t.Fatal(err)
		}
		if p := model.Placeholders(text); len(p) > 0 {
			t.Errorf("template task uses placeholders %v", p)
		}
	}
}
