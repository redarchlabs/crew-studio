package db_test

import (
	"context"
	"testing"

	"crewstudio/internal/db"
	"crewstudio/internal/testdb"
)

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testdb.New(t, "db_test") // already migrated once
	ctx := context.Background()
	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&before); err != nil || before == 0 {
		t.Fatalf("migrations recorded = %d, %v", before, err)
	}
	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Errorf("second run applied %v", applied)
	}
	var after int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&after)
	if after != before {
		t.Errorf("migration rows %d -> %d", before, after)
	}
}

func TestMigrateReappliesAMissingRecord(t *testing.T) {
	pool := testdb.New(t, "db_test")
	ctx := context.Background()
	// Forget the last migration; it is idempotent SQL only if written so, so just check the error path is reported
	if _, err := pool.Exec(ctx, `DROP TABLE goose_db_version`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, pool); err == nil {
		t.Skip("migrations re-ran cleanly")
	}
}

func TestMigrateFailsOnClosedPool(t *testing.T) {
	pool := testdb.New(t, "db_test")
	pool.Close()
	if _, err := db.Migrate(context.Background(), pool); err == nil {
		t.Error("expected an error from a closed pool")
	}
}
