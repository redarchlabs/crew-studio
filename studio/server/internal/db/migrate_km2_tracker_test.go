package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
)

const km2TrackerVersion = 18

// Rolling 0018 back deletes km2_tracker connections with what points at them, keeps every other connection, and
// running it again allows the kind once more.
func TestKM2TrackerKindRollsBack(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO connections (name, kind, secret_cipher, secret_keys) VALUES ('KM2 issues', 'km2_tracker', '\x01', '{api_key}'), ('Jira', 'jira', NULL, '{}')`)
	exec(`INSERT INTO crew_connections (crew_id, connection_id) SELECT c.id, k.id FROM crews c, connections k WHERE c.name = 'PE Due Diligence Crew'`)
	exec(`INSERT INTO tracking_items (crew_id, connection_id, project, item_ref)
		SELECT c.id, k.id, 'ERP', 'ERP-1' FROM crews c, connections k WHERE c.name = 'PE Due Diligence Crew' AND k.kind = 'km2_tracker'`)
	if _, err := db.MigrateDownTo(ctx, pool, km2TrackerVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	var kinds, links, items int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM connections WHERE kind = 'jira'), (SELECT count(*) FROM crew_connections),
		(SELECT count(*) FROM tracking_items)`).Scan(&kinds, &links, &items); err != nil {
		t.Fatal(err)
	}
	if kinds != 1 || links != 1 || items != 0 {
		t.Errorf("after down: jira %d, crew links %d, tracking items %d", kinds, links, items)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO connections (name, kind) VALUES ('Again', 'km2_tracker')`); err == nil {
		t.Error("km2_tracker allowed without 0018")
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO connections (name, kind) VALUES ('Again', 'km2_tracker')`)
}
