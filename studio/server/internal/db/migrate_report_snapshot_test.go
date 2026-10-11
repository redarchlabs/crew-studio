package db_test

import (
	"context"
	"strings"
	"testing"

	"crewstudio/internal/db"
)

const reportSnapshotVersion = 20

// 0020 adds the report snapshot (size-checked) and the narrative guidance to the PMO template's weekly task; rolling
// it back removes both, leaving the task as it was.
func TestReportSnapshotMigration(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	task := func() string {
		var d string
		if err := pool.QueryRow(ctx, `SELECT description FROM tasks WHERE id = '33333333-0000-4000-8000-000000000202'`).Scan(&d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	after := task()
	if !strings.Contains(after, "Crew Studio adds the data to the report itself") || strings.Count(after, "Crew Studio adds the data") != 1 {
		t.Errorf("template task = %q", after)
	}
	insert := `INSERT INTO reports (crew_id, project, project_slug, week, period, title, markdown, content_sha256, dry_run, snapshot)
		SELECT id, 'Atlas', 'atlas', '2026-W41', '2026-W41', 't', 'x', 'h', false, $1::jsonb FROM crews WHERE name = 'PE Due Diligence Crew'`
	if _, err := pool.Exec(ctx, insert, `{"version": 1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insert, `{"items": "`+strings.Repeat("x", 600001)+`"}`); err == nil {
		t.Error("an oversized snapshot was stored")
	}
	if _, err := db.MigrateDownTo(ctx, pool, reportSnapshotVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if before := task(); strings.Contains(before, "Crew Studio adds the data") || !strings.HasSuffix(before, "says what would have been sent.") {
		t.Errorf("template task after down = %q", before)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'reports' AND column_name = 'snapshot'
		AND table_schema = current_schema()`).Scan(&n); err != nil || n != 0 {
		t.Errorf("snapshot column after down: %d %v", n, err)
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if strings.Count(task(), "Crew Studio adds the data") != 1 {
		t.Error("the guidance was added twice")
	}
}
