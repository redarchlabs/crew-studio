package db_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"crewstudio/internal/db"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

const crewTargetsVersion = 10

func taskID(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT id::text FROM tasks WHERE name = $1`, name).Scan(&id); err != nil {
		t.Fatalf("task %q: %v", name, err)
	}
	return id
}

// libraryAt loads what runspec.TaskCrew needs, as the server did before work orders stopped targeting tasks.
func libraryAt(t *testing.T, pool *pgxpool.Pool) runspec.Library {
	t.Helper()
	ctx := context.Background()
	st := store.New(pool)
	lib := runspec.Library{Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{}}
	tasks, err := st.ListTasks(ctx, store.AllAreas)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range tasks {
		lib.Tasks[x.ID] = x
	}
	agents, err := st.ListAgents(ctx, store.AllAreas)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range agents {
		lib.Agents[x.ID] = x
	}
	if lib.Crews, err = st.ListCrews(ctx, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	// Read only what TaskCrew uses: the store reads columns later migrations add
	if err := pool.QueryRow(ctx, `SELECT default_budget_usd, search_model FROM settings`).
		Scan(&lib.Settings.DefaultBudgetUSD, &lib.Settings.SearchModel); err != nil {
		t.Fatal(err)
	}
	return lib
}

func sortedInputs(in []model.CrewInput) []model.CrewInput {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b model.CrewInput) int { return strings.Compare(a.Key, b.Key) })
	return out
}

func TestTaskTargetsBecomeCrews(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MigrateDownTo(ctx, pool, crewTargetsVersion-1); err != nil {
		t.Fatal(err)
	}
	memo, profile := taskID(t, pool, "Investment Memo"), taskID(t, pool, "Corporate Profile")
	// A crew already has the profile task's name, so its new crew needs another
	if _, err := pool.Exec(ctx, `INSERT INTO crews (name) VALUES ('Corporate Profile')`); err != nil {
		t.Fatal(err)
	}
	lib := libraryAt(t, pool)
	wantMemo, problems := runspec.TaskCrew(memo, lib)
	if len(problems) > 0 {
		t.Fatalf("TaskCrew: %v", problems)
	}
	for _, x := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO work_orders (title, target_type, task_id, target_name) VALUES
			('memo', 'task', $1, 'Investment Memo'), ('memo again', 'task', $1, 'Investment Memo'),
			('profile', 'task', $2, 'Corporate Profile'), ('gone', 'task', NULL, 'Deleted task')`, []any{memo, profile}},
		{`INSERT INTO work_orders (title, target_type, crew_id, target_name)
			SELECT 'crew', 'crew', id, name FROM crews WHERE name = 'PE Due Diligence Crew'`, nil},
		{`INSERT INTO schedules (name, target_type, task_id, cron) VALUES ('nightly memo', 'task', $1, '0 2 * * *')`, []any{memo}},
		{`INSERT INTO triggers (name, target_type, task_id, token_prefix, token_hash, signing_secret)
			VALUES ('profile hook', 'task', $1, 'p', 'h', 's')`, []any{profile}},
	} {
		if _, err := pool.Exec(ctx, x.sql, x.args...); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	st := store.New(pool)
	crewOf := func(table, name string) model.Crew {
		t.Helper()
		var id *string
		if err := pool.QueryRow(ctx, `SELECT crew_id::text FROM `+table+` WHERE `+map[string]string{
			"work_orders": "title", "schedules": "name", "triggers": "name"}[table]+` = $1`, name).Scan(&id); err != nil {
			t.Fatalf("%s %q: %v", table, name, err)
		}
		if id == nil {
			t.Fatalf("%s %q has no crew", table, name)
		}
		c, err := st.GetCrew(ctx, *id)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	got := crewOf("work_orders", "memo")
	if got.Name != "Investment Memo" || got.Process != model.ProcessSequential || got.BudgetUSD != lib.Settings.DefaultBudgetUSD {
		t.Errorf("memo crew = %q %s $%v", got.Name, got.Process, got.BudgetUSD)
	}
	// Same tasks, agents and inputs as the crew the server built on the fly, in an order that can run
	if !slices.Equal(slices.Sorted(slices.Values(got.TaskIDs)), slices.Sorted(slices.Values(wantMemo.TaskIDs))) ||
		got.TaskIDs[len(got.TaskIDs)-1] != memo {
		t.Errorf("tasks = %v, want %v ending with the memo", got.TaskIDs, wantMemo.TaskIDs)
	}
	if !slices.Equal(slices.Sorted(slices.Values(got.AgentIDs)), slices.Sorted(slices.Values(wantMemo.AgentIDs))) {
		t.Errorf("agents = %v, want %v", got.AgentIDs, wantMemo.AgentIDs)
	}
	if !slices.Equal(sortedInputs(got.Inputs), sortedInputs(wantMemo.Inputs)) {
		t.Errorf("inputs = %+v, want %+v", got.Inputs, wantMemo.Inputs)
	}
	if !slices.Equal(got.AreaIDs, lib.Tasks[memo].AreaIDs) {
		t.Errorf("areas = %v, want the task's %v", got.AreaIDs, lib.Tasks[memo].AreaIDs)
	}
	after := libraryAt(t, pool)
	for _, p := range runspec.Check(got, after) {
		if strings.Contains(p, "must come earlier") || strings.Contains(p, "not in the crew") || strings.Contains(p, "no input") {
			t.Errorf("crew cannot run: %s", p)
		}
	}

	// One crew per task, shared by everything that targeted it
	if again, sched := crewOf("work_orders", "memo again"), crewOf("schedules", "nightly memo"); again.ID != got.ID || sched.ID != got.ID {
		t.Errorf("memo targets got crews %s, %s and %s", got.ID, again.ID, sched.ID)
	}
	profileCrew := crewOf("triggers", "profile hook")
	if profileCrew.Name != "Corporate Profile (task)" || !slices.Equal(profileCrew.TaskIDs, []string{profile}) {
		t.Errorf("profile crew = %q %v", profileCrew.Name, profileCrew.TaskIDs)
	}
	if wo := crewOf("work_orders", "profile"); wo.ID != profileCrew.ID {
		t.Errorf("profile work order crew %s, trigger crew %s", wo.ID, profileCrew.ID)
	}
	if crewOf("work_orders", "crew").Name != "PE Due Diligence Crew" {
		t.Error("crew work order changed crew")
	}
	var goneCrew *string
	if err := pool.QueryRow(ctx, `SELECT crew_id::text FROM work_orders WHERE title = 'gone'`).Scan(&goneCrew); err != nil || goneCrew != nil {
		t.Errorf("work order of a deleted task: crew %v, err %v", goneCrew, err)
	}
	var revisions int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM revisions WHERE entity_type = 'crew' AND action = 'create'
		AND entity_id IN ($1, $2)`, got.ID, profileCrew.ID).Scan(&revisions)
	if revisions != 2 {
		t.Errorf("%d create revisions for the new crews, want 2", revisions)
	}
	var columns int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
		AND table_name IN ('work_orders', 'schedules', 'triggers') AND column_name IN ('target_type', 'task_id')`).Scan(&columns)
	if columns != 0 {
		t.Errorf("%d task target columns left", columns)
	}
}
