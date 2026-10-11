package db_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"crewstudio/internal/db"
	"crewstudio/internal/testdb"
)

var schemaSeq atomic.Int64

// newSchema returns a pool on an empty (unmigrated) schema of its own and the schema name.
func newEmptySchemaPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv(testdb.EnvVar)
	if url == "" {
		t.Skipf("%s not set", testdb.EnvVar)
	}
	// testdb.New migrates; build the empty schema by migrating then wiping it
	schema := fmt.Sprintf("db_unit_%d_%d", os.Getpid(), schemaSeq.Add(1))
	pool := testdb.New(t, schema)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
	return pool
}

func migrationNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".sql"))
	}
	slices.Sort(names)
	return names
}

func TestMigrationFilesAreNumberedContiguously(t *testing.T) {
	names := migrationNames(t)
	if len(names) == 0 {
		t.Fatal("no migrations")
	}
	re := regexp.MustCompile(`^(\d{4})_[a-z0-9_]+$`)
	for i, n := range names {
		m := re.FindStringSubmatch(n)
		if m == nil {
			t.Fatalf("migration %q is not named NNNN_snake_case", n)
		}
		if want := fmt.Sprintf("%04d", i+1); m[1] != want {
			t.Errorf("migration %d is numbered %s, want %s (gaps or duplicates break ordering)", i, m[1], want)
		}
	}
}

func TestMigrateConcurrentRunnersApplyEachOnce(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx := context.Background()
	type res struct {
		applied []string
		err     error
	}
	out := make(chan res, 4)
	for i := 0; i < 4; i++ {
		go func() {
			a, err := db.Migrate(ctx, pool)
			out <- res{a, err}
		}()
	}
	var all []string
	for i := 0; i < 4; i++ {
		r := <-out
		if r.err != nil {
			t.Fatalf("runner failed: %v", r.err)
		}
		all = append(all, r.applied...)
	}
	slices.Sort(all)
	if want := migrationNames(t); !slices.Equal(all, want) {
		t.Errorf("combined applied %v, want each of %v exactly once", all, want)
	}
}

func TestMigrateWithCanceledContext(t *testing.T) {
	pool := newEmptySchemaPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := db.Migrate(ctx, pool); err == nil {
		t.Error("canceled context must fail")
	}
}

func TestOpen(t *testing.T) {
	url := os.Getenv(testdb.EnvVar)
	if url == "" {
		t.Skipf("%s not set", testdb.EnvVar)
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	if cfg := pool.Config(); cfg.MaxConns != 10 || cfg.MaxConnIdleTime != 5*time.Minute {
		t.Errorf("pool config = max %d idle %v", cfg.MaxConns, cfg.MaxConnIdleTime)
	}
	var one int
	if err := pool.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Errorf("query = %d %v", one, err)
	}
}

func TestOpenErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := db.Open(ctx, "postgres://%zz"); err == nil || !strings.Contains(err.Error(), "parse database url") {
		t.Errorf("malformed url: %v", err)
	}
	// nothing listens on port 1: the ping must fail promptly with a clear message
	start := time.Now()
	_, err := db.Open(ctx, "postgres://nobody:nothing@127.0.0.1:1/none?connect_timeout=2&sslmode=disable")
	if err == nil || !strings.Contains(err.Error(), "connect to postgres") {
		t.Errorf("unreachable: %v", err)
	}
	if time.Since(start) > 8*time.Second {
		t.Errorf("Open hung for %v", time.Since(start))
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.Open(cctx, "postgres://nobody:nothing@127.0.0.1:1/none?sslmode=disable"); err == nil {
		t.Error("canceled context must fail")
	}
	// the error never echoes the password
	_, err = db.Open(ctx, "postgres://nobody:sekret-pw@127.0.0.1:1/none?connect_timeout=2&sslmode=disable")
	if err != nil && strings.Contains(err.Error(), "sekret-pw") {
		t.Error("password leaked in error text")
	}
}
