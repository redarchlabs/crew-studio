package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
)

const mcpConnectionsVersion = 31

// 0031 adds the mcp connection kind and the tools of each MCP connection. Rolling it back removes both (and the MCP
// connections); applying it again restores the schema.
func TestMCPConnectionsMigration(t *testing.T) {
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
	const table = `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'connection_tools'`
	if count(table) != 1 {
		t.Fatal("connection_tools missing after up")
	}
	exec := func(q string) error {
		_, err := pool.Exec(ctx, q)
		return err
	}
	if err := exec(`INSERT INTO connections (id, name, kind, config) VALUES ('99999999-0000-4000-8000-000000000001', 'm', 'mcp', '{}'),
		('99999999-0000-4000-8000-000000000002', 'k', 'km2_tracker', '{}')`); err != nil {
		t.Fatalf("mcp and km2_tracker kinds: %v", err)
	}
	if err := exec(`INSERT INTO connection_tools (connection_id, name) VALUES ('99999999-0000-4000-8000-000000000001', 'search_notes')`); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT count(*) FROM connection_tools WHERE effect = 'changes' AND enabled AND input_schema = '{}'`) != 1 {
		t.Error("tool defaults not applied")
	}
	for _, bad := range []string{
		`INSERT INTO connection_tools (connection_id, name) VALUES ('99999999-0000-4000-8000-000000000001', 'a b')`,
		`INSERT INTO connection_tools (connection_id, name, effect) VALUES ('99999999-0000-4000-8000-000000000001', 'x', 'proposes')`,
		`INSERT INTO connection_tools (connection_id, name, input_schema) VALUES ('99999999-0000-4000-8000-000000000001', 'y', '[1]')`,
		`INSERT INTO connections (name, kind, config) VALUES ('bad', 'ftp', '{}')`,
	} {
		if exec(bad) == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
	if _, err := db.MigrateDownTo(ctx, pool, mcpConnectionsVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if count(table) != 0 || count(`SELECT count(*) FROM connections WHERE kind = 'mcp'`) != 0 ||
		count(`SELECT count(*) FROM connections WHERE kind = 'km2_tracker'`) != 1 {
		t.Error("down left MCP connections or tools, or removed others")
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if count(table) != 1 {
		t.Error("connection_tools not restored after up again")
	}
}
