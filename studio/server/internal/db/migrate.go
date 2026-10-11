package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
)

// Migrations are goose SQL files: NNNN_name.sql, version NNNN, with "-- +goose Up" and "-- +goose Down"
// sections. Each section is one StatementBegin/StatementEnd block, because goose's statement splitter does not
// understand the $s$ dollar quoting the seed data uses.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

const (
	// VersionTable is where goose records applied migrations.
	VersionTable = "goose_db_version"
	// legacyTable is where the hand-written migrator recorded them before goose (by file name, without .sql).
	legacyTable = "schema_migrations"
	// Advisory lock held while migrating, so only one process migrates at a time. It is the id the hand-written
	// migrator used, so an old and a new build cannot migrate the same database at once either.
	migrationLockID = 727_001
)

// MigrationState is one migration and whether it has been applied.
type MigrationState struct {
	Name      string
	Version   int64
	Applied   bool
	AppliedAt time.Time
}

// Migrate applies every pending migration in order, each in its own transaction, and returns the names of
// those it applied. It stops at the first failure; migrations before it stay applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	provider, sqlDB, err := newProvider(pool)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	if err := adoptLegacyHistory(ctx, sqlDB, provider); err != nil {
		return nil, err
	}
	results, err := provider.Up(ctx)
	return resultNames(results, err, "apply")
}

// MigrateDownTo rolls back applied migrations newer than version, newest first, running each one's Down
// section, and returns the names it rolled back. Version 0 rolls back everything.
func MigrateDownTo(ctx context.Context, pool *pgxpool.Pool, version int64) ([]string, error) {
	if version < 0 {
		return nil, fmt.Errorf("roll back to version %d: the version must be 0 or more", version)
	}
	provider, sqlDB, err := newProvider(pool)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	if err := adoptLegacyHistory(ctx, sqlDB, provider); err != nil {
		return nil, err
	}
	results, err := provider.DownTo(ctx, version)
	return resultNames(results, err, "roll back")
}

// MigrationStatus lists every migration in this build, oldest first, with whether it has been applied. Like every
// entry point it first adopts a pre-goose history and creates goose's table if missing; it never runs a migration.
func MigrationStatus(ctx context.Context, pool *pgxpool.Pool) ([]MigrationState, error) {
	provider, sqlDB, err := newProvider(pool)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	if err := adoptLegacyHistory(ctx, sqlDB, provider); err != nil {
		return nil, err
	}
	statuses, err := provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration status: %w", err)
	}
	states := make([]MigrationState, 0, len(statuses))
	for _, s := range statuses {
		states = append(states, MigrationState{
			Name:      sourceName(s.Source),
			Version:   s.Source.Version,
			Applied:   s.State == goose.StateApplied,
			AppliedAt: s.AppliedAt,
		})
	}
	return states, nil
}

func newProvider(pool *pgxpool.Pool) (*goose.Provider, *sql.DB, error) {
	fsys, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, nil, fmt.Errorf("open embedded migrations: %w", err)
	}
	store, err := database.NewStore(database.DialectPostgres, VersionTable)
	if err != nil {
		return nil, nil, fmt.Errorf("create migration store: %w", err)
	}
	// Poll for the lock every second (goose waits 5s between tries by default), for up to 5 minutes
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(migrationLockID), lock.WithLockTimeout(1, 300))
	if err != nil {
		return nil, nil, fmt.Errorf("create migration lock: %w", err)
	}
	// Closing this *sql.DB does not close the pool
	sqlDB := stdlib.OpenDBFromPool(pool)
	provider, err := goose.NewProvider(goose.DialectCustom, sqlDB, fsys,
		goose.WithStore(store), goose.WithSessionLocker(locker))
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("load migrations: %w", err)
	}
	return provider, sqlDB, nil
}

// resultNames returns the names of the migrations that ran and, on failure, an error naming the one that failed.
func resultNames(results []*goose.MigrationResult, err error, verb string) ([]string, error) {
	var partial *goose.PartialError
	if errors.As(err, &partial) {
		results = partial.Applied
	}
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, sourceName(r.Source))
	}
	switch {
	case partial != nil:
		return names, fmt.Errorf("%s migration %s: %w", verb, sourceName(partial.Failed.Source), partial.Err)
	case err != nil:
		return names, fmt.Errorf("%s migrations: %w", verb, err)
	}
	return names, nil
}

func sourceName(src *goose.Source) string {
	if src == nil {
		return ""
	}
	return strings.TrimSuffix(path.Base(src.Path), ".sql")
}

// adoptLegacyHistory moves a database migrated by the hand-written migrator over to goose: it records the same
// migrations, with the times they were applied, in goose's table and drops the old one, in one transaction, so
// nothing is applied twice. It does nothing once goose's table exists or when the database is new. Both tables are
// looked up in the current schema only, as goose does.
func adoptLegacyHistory(ctx context.Context, sqlDB *sql.DB, provider *goose.Provider) error {
	known := map[string]int64{}
	for _, src := range provider.ListSources() {
		known[sourceName(src)] = src.Version
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("adopt migration history: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit

	// Same lock as goose's, so this cannot run while another process migrates; give up as goose does
	if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout = '5min'"); err != nil {
		return fmt.Errorf("set lock timeout: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	var schema string
	if err := tx.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		return fmt.Errorf("read current schema: %w", err)
	}
	legacy := pgx.Identifier{schema, legacyTable}.Sanitize()
	var hasLegacy, hasGoose bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL, to_regclass($2) IS NOT NULL`,
		legacy, pgx.Identifier{schema, VersionTable}.Sanitize()).Scan(&hasLegacy, &hasGoose); err != nil {
		return fmt.Errorf("check migration tables: %w", err)
	}
	if !hasLegacy || hasGoose {
		return nil
	}

	records, err := legacyRecords(ctx, tx, legacy, known)
	if err != nil {
		return err
	}
	store, err := database.NewStore(database.DialectPostgres, VersionTable)
	if err != nil {
		return fmt.Errorf("create migration store: %w", err)
	}
	if err := store.CreateVersionTable(ctx, tx); err != nil {
		return fmt.Errorf("create %s: %w", VersionTable, err)
	}
	if err := store.Insert(ctx, tx, database.InsertRequest{Version: 0}); err != nil {
		return fmt.Errorf("record version 0 in %s: %w", VersionTable, err)
	}
	for _, r := range records {
		if err := store.Insert(ctx, tx, database.InsertRequest{Version: r.version}); err != nil {
			return fmt.Errorf("record migration %d in %s: %w", r.version, VersionTable, err)
		}
		// Keep when it was really applied, not when it was adopted
		if _, err := tx.ExecContext(ctx, "UPDATE "+VersionTable+" SET tstamp = $1 WHERE version_id = $2",
			r.appliedAt, r.version); err != nil {
			return fmt.Errorf("record when migration %d was applied: %w", r.version, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DROP TABLE "+legacy); err != nil {
		return fmt.Errorf("drop %s: %w", legacyTable, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("adopt migration history: %w", err)
	}
	return nil
}

type legacyRecord struct {
	version   int64
	appliedAt time.Time
}

// legacyRecords reads the old migrator's records, oldest first, as goose versions. A record this build has no
// file for stops the adoption: the database was migrated by a newer build, and guessing would lose that.
func legacyRecords(ctx context.Context, tx *sql.Tx, table string, known map[string]int64) ([]legacyRecord, error) {
	rows, err := tx.QueryContext(ctx, "SELECT version, applied_at FROM "+table+" ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", legacyTable, err)
	}
	defer rows.Close()
	var records []legacyRecord
	for rows.Next() {
		var name string
		var appliedAt time.Time
		if err := rows.Scan(&name, &appliedAt); err != nil {
			return nil, fmt.Errorf("read %s: %w", legacyTable, err)
		}
		v, ok := known[name]
		if !ok {
			return nil, fmt.Errorf("%s records migration %q, which this build does not have; use the build that applied it",
				legacyTable, name)
		}
		records = append(records, legacyRecord{version: v, appliedAt: appliedAt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", legacyTable, err)
	}
	return records, nil
}

// ParseVersion reads a migration version given as a number ("6") or a file name ("0006_llm_vision").
func ParseVersion(s string) (int64, error) {
	digits := s
	if i := strings.IndexByte(s, '_'); i >= 0 {
		digits = s[:i]
	}
	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%q is not a migration version (use a number such as 6, or a file name such as 0006_llm_vision)", s)
	}
	return v, nil
}
