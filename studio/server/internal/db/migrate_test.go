package db_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"crewstudio/internal/db"
)

// recordedNames returns the migrations goose records as applied, oldest first, as file names.
func recordedNames(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	states, err := db.MigrationStatus(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range states {
		if s.Applied {
			names = append(names, s.Name)
		}
	}
	return names
}

func reversed(in []string) []string {
	out := slices.Clone(in)
	slices.Reverse(out)
	return out
}

// tablesInSchema counts the tables in the pool's schema (its search_path).
func tablesInSchema(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_tables WHERE schemaname = current_schema()`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// When makeLegacy records every migration as applied
var legacyAppliedAt = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

// makeLegacy turns a goose-migrated schema back into what the hand-written migrator left: a schema_migrations
// table naming the applied files, and no goose table.
func makeLegacy(t *testing.T, pool *pgxpool.Pool, names []string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE `+db.VersionTable+`;
		CREATE TABLE schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)`, n, legacyAppliedAt); err != nil {
			t.Fatal(err)
		}
	}
}

func legacyTableExists(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestEveryMigrationHasUpAndDown(t *testing.T) {
	for _, name := range migrationNames(t) {
		raw, err := os.ReadFile("migrations/" + name + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		sql := string(raw)
		for _, want := range []string{"-- +goose Up", "-- +goose Down"} {
			if !strings.Contains(sql, want) {
				t.Errorf("%s has no %q section", name, want)
			}
		}
		// Each section is one block: goose cannot split $s$ dollar-quoted text into statements itself
		if begins, ends := strings.Count(sql, "-- +goose StatementBegin"), strings.Count(sql, "-- +goose StatementEnd"); begins != 2 || ends != 2 {
			t.Errorf("%s has %d StatementBegin and %d StatementEnd, want 2 of each (one per section)", name, begins, ends)
		}
	}
}

func TestMigrateAppliesEveryFileInOrderOnce(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	want := migrationNames(t)
	if !slices.Equal(applied, want) {
		t.Errorf("applied %v, want %v", applied, want)
	}
	if got := recordedNames(t, pool); !slices.Equal(got, want) {
		t.Errorf("recorded %v, want %v", got, want)
	}
	again, err := db.Migrate(ctx, pool)
	if err != nil || len(again) != 0 {
		t.Errorf("second run applied %v, err %v", again, err)
	}
}

func TestDownToZeroThenUpRebuildsEverything(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var agentsBefore int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM agents`).Scan(&agentsBefore)

	rolledBack, err := db.MigrateDownTo(ctx, pool, 0)
	if err != nil {
		t.Fatalf("roll back everything: %v", err)
	}
	if want := reversed(migrationNames(t)); !slices.Equal(rolledBack, want) {
		t.Errorf("rolled back %v, want newest first %v", rolledBack, want)
	}
	// Every Down section together must leave nothing but goose's own table
	if n := tablesInSchema(t, pool); n != 1 {
		t.Errorf("%d tables left after rolling back everything, want only %s", n, db.VersionTable)
	}

	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("re-apply after full rollback: %v", err)
	}
	var agentsAfter int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM agents`).Scan(&agentsAfter)
	if agentsAfter != agentsBefore || agentsAfter == 0 {
		t.Errorf("agents %d after rebuild, %d before", agentsAfter, agentsBefore)
	}
}

func TestEachMigrationRollsBackAndReapplies(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	names := migrationNames(t)
	// From a fully migrated database, roll back to each version in turn and re-apply, so every Down runs on a
	// database its Up (and everything after it) produced, and every Up runs again after its Down
	for v := len(names) - 1; v >= 0; v-- {
		rolledBack, err := db.MigrateDownTo(ctx, pool, int64(v))
		if err != nil {
			t.Fatalf("roll back to version %d: %v", v, err)
		}
		if want := reversed(names[v:]); !slices.Equal(rolledBack, want) {
			t.Fatalf("rolled back %v, want %v", rolledBack, want)
		}
		applied, err := db.Migrate(ctx, pool)
		if err != nil || !slices.Equal(applied, names[v:]) {
			t.Fatalf("re-apply from version %d: applied %v, err %v", v, applied, err)
		}
	}
}

func TestDownRefusesToLoseDataInUse(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// A task uses a library agent: rolling back the library must fail and change nothing
	if _, err := pool.Exec(ctx, `INSERT INTO tasks (name, description, expected_output, agent_id)
		VALUES ('Mine', 'd', 'e', '22222222-0000-4000-8000-000000000110')`); err != nil {
		t.Fatal(err)
	}
	// Migrations after 0007 roll back first; 0007 itself must then fail
	rolledBack, err := db.MigrateDownTo(ctx, pool, 6)
	if err == nil || !strings.Contains(err.Error(), "roll back migration 0007_agent_library") {
		t.Fatalf("rolled back %v, err %v; want a failure naming 0007", rolledBack, err)
	}
	if slices.Contains(rolledBack, "0007_agent_library") {
		t.Errorf("rolled back %v, but 0007 failed", rolledBack)
	}
	if !slices.Contains(recordedNames(t, pool), "0007_agent_library") {
		t.Error("0007 no longer recorded as applied")
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM agents WHERE id::text LIKE '22222222-0000-4000-8000-0000000001%'`).Scan(&n)
	if n == 0 {
		t.Error("failed rollback deleted the library agents")
	}
}

func TestMigrateStopsAtFirstFailureAndKeepsEarlierOnes(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	// A table left over from a manual change, named like one 0004 creates: 0004 must fail loudly
	if _, err := pool.Exec(ctx, `CREATE TABLE permissions (key text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	applied, err := db.Migrate(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "apply migration 0004_access_control") {
		t.Fatalf("err = %v, want a failure naming 0004", err)
	}
	want := []string{"0001_schema", "0002_seed_existing_crews", "0003_reasoning_models_and_gemini3"}
	if !slices.Equal(applied, want) {
		t.Errorf("applied before failure = %v, want %v", applied, want)
	}
	if got := recordedNames(t, pool); !slices.Equal(got, want) {
		t.Errorf("recorded = %v, want %v", got, want)
	}
	// 0004 ran in a transaction, so none of its other tables exist
	var users bool
	_ = pool.QueryRow(ctx, `SELECT to_regclass('users') IS NOT NULL`).Scan(&users)
	if users {
		t.Error("failed migration left tables behind")
	}
	// Fix the clash and carry on from 0004
	if _, err := pool.Exec(ctx, `DROP TABLE permissions`); err != nil {
		t.Fatal(err)
	}
	applied, err = db.Migrate(ctx, pool)
	if err != nil || len(applied) == 0 || applied[0] != "0004_access_control" {
		t.Errorf("resume applied %v, err %v", applied, err)
	}
}

func TestMigrateAdoptsLegacyHistory(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	names := migrationNames(t)
	makeLegacy(t, pool, names)

	applied, err := db.Migrate(ctx, pool)
	if err != nil || len(applied) != 0 {
		t.Fatalf("legacy database fully migrated: applied %v, err %v; want nothing re-applied", applied, err)
	}
	if got := recordedNames(t, pool); !slices.Equal(got, names) {
		t.Errorf("goose recorded %v, want %v", got, names)
	}
	if legacyTableExists(t, pool) {
		t.Error("schema_migrations should be dropped once adopted")
	}
	// The real apply times carry over (goose stores them without a time zone, in the session's)
	states, err := db.MigrationStatus(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if d := st.AppliedAt.Sub(legacyAppliedAt); d < -14*time.Hour || d > 14*time.Hour {
			t.Errorf("%s applied at %v, want the legacy time %v", st.Name, st.AppliedAt, legacyAppliedAt)
		}
	}
}

func TestImportedCrewsWithRunsBlockRollingBackTheirSeed(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (crew_id, crew_name, status, budget_usd, spec)
		VALUES ('44444444-0000-4000-8000-000000000001', 'PE Due Diligence Crew', 'succeeded', 2, '{}')`); err != nil {
		t.Fatal(err)
	}
	_, err := db.MigrateDownTo(ctx, pool, 1)
	if err == nil || !strings.Contains(err.Error(), "imported crews have runs") {
		t.Fatalf("err = %v, want the 0002 rollback refused because of runs", err)
	}
	var crewID *string
	if err := pool.QueryRow(ctx, `SELECT crew_id::text FROM runs`).Scan(&crewID); err != nil || crewID == nil {
		t.Errorf("run lost its crew: %v, %v", crewID, err)
	}
}

func TestMigrateAdoptsPartialLegacyHistoryAndAppliesTheRest(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	names := migrationNames(t)
	// The old migrator had applied everything but the newest migration
	if _, err := db.MigrateDownTo(ctx, pool, int64(len(names)-1)); err != nil {
		t.Fatal(err)
	}
	makeLegacy(t, pool, names[:len(names)-1])

	applied, err := db.Migrate(ctx, pool)
	if err != nil || !slices.Equal(applied, names[len(names)-1:]) {
		t.Errorf("applied %v, err %v; want only %s", applied, err, names[len(names)-1])
	}
}

func TestMigrateRefusesUnknownLegacyMigration(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	makeLegacy(t, pool, append(migrationNames(t), "9999_from_a_newer_build"))

	if _, err := db.Migrate(ctx, pool); err == nil || !strings.Contains(err.Error(), "9999_from_a_newer_build") {
		t.Fatalf("err = %v, want a refusal naming the unknown migration", err)
	}
	if !legacyTableExists(t, pool) {
		t.Error("a refused adoption must leave schema_migrations in place")
	}
}

func TestMigrateDownToRejectsNegativeVersion(t *testing.T) {
	if _, err := db.MigrateDownTo(context.Background(), nil, -1); err == nil {
		t.Error("negative version accepted")
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"6", 6, false},
		{"0", 0, false},
		{"0006_llm_vision", 6, false},
		{"0007", 7, false},
		{"", 0, true},
		{"abc", 0, true},
		{"-1", 0, true},
		{"six_llm_vision", 0, true},
	}
	for _, tt := range tests {
		got, err := db.ParseVersion(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseVersion(%q) = %d, %v; want %d, error %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}
