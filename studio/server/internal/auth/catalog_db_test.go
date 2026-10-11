package auth

import (
	"context"
	"slices"
	"testing"

	"crewstudio/internal/testdb"
)

// The catalog in Go and the permissions seeded by migration 0004 must list the same keys.
func TestCatalogMatchesDatabase(t *testing.T) {
	pool := testdb.New(t, "auth_unit")
	ctx := context.Background()
	rows, err := pool.Query(ctx, `SELECT key FROM permissions ORDER BY position`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var inDB []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		inDB = append(inDB, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(inDB, Catalog) {
		t.Errorf("database permissions %v\nGo catalog %v", inDB, Catalog)
	}

	var granted int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions WHERE role_id = $1`, AdministratorRoleID).Scan(&granted); err != nil {
		t.Fatal(err)
	}
	if granted != len(Catalog) {
		t.Errorf("Administrator role holds %d permissions, catalog has %d", granted, len(Catalog))
	}
}
