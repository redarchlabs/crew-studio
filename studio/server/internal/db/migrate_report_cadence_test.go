package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
)

const reportCadenceVersion = 19

// 0019 gives existing reports the weekly cadence and their week as period; rolling it back drops the daily reports,
// which the old one-report-per-run-and-project rule cannot hold, and keeps the weekly ones.
func TestReportCadenceMigration(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MigrateDownTo(ctx, pool, reportCadenceVersion-1); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO reports (crew_id, project, project_slug, week, title, markdown, content_sha256, dry_run)
		SELECT id, 'Atlas', 'atlas', '2026-W41', 'Atlas status, 2026-W41', 'x', 'h', false FROM crews WHERE name = 'PE Due Diligence Crew'`
	if _, err := pool.Exec(ctx, insert); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var cadence, period string
	if err := pool.QueryRow(ctx, `SELECT cadence, period FROM reports`).Scan(&cadence, &period); err != nil || cadence != "weekly" || period != "2026-W41" {
		t.Fatalf("existing report = %q %q %v", cadence, period, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO reports (crew_id, project, project_slug, week, cadence, period, title, markdown, content_sha256, dry_run)
		SELECT crew_id, project, project_slug, week, 'daily', '2026-10-06', 'Atlas daily report, 2026-10-06', 'y', 'h2', false FROM reports`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO reports (crew_id, project, project_slug, week, cadence, period, title, markdown, content_sha256, dry_run)
		SELECT crew_id, project, project_slug, week, 'monthly', '2026-10', 't', 'z', 'h3', false FROM reports LIMIT 1`); err == nil {
		t.Error("a monthly cadence was accepted")
	}
	if _, err := db.MigrateDownTo(ctx, pool, reportCadenceVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reports WHERE week = '2026-W41'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("reports after down = %d %v", n, err)
	}
}
