// Package store is the Postgres repository for every Crew Studio entity.
// Each mutation runs in a transaction and records a revision, so the full edit history is kept.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the requested row does not exist.
var ErrNotFound = errors.New("not found")

// ConflictError means the change clashes with existing data (a taken name, or a row still in use).
type ConflictError struct {
	Message string
	// Code is a stable machine-readable reason for clients; empty means the generic "conflict"
	Code string
	// Entity and Name are the kind and name that are already taken, for a unique name
	Entity, Name string
}

func (e *ConflictError) Error() string { return e.Message }

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Pool exposes the pool for health checks.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// queryer is satisfied by both the pool and a transaction.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// mapErr turns driver errors into ErrNotFound or a ConflictError the API can show to people.
func mapErr(err error, entity, name string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgUniqueViolation:
			return &ConflictError{Message: fmt.Sprintf("A %s named %q already exists.", entity, name), Entity: entity, Name: name}
		case pgForeignKeyViolation:
			return &ConflictError{Message: fmt.Sprintf("This %s refers to something that does not exist, or is still in use.", entity)}
		}
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

func recordRevision(ctx context.Context, q queryer, entityType, entityID, action string, snapshot any) error {
	return recordAreaRevision(ctx, q, entityType, entityID, action, snapshot, nil)
}

// usedBy returns the names of rows that reference id, for a friendly "still in use" message.
func usedBy(ctx context.Context, q queryer, sql, id string) ([]string, error) {
	rows, err := q.Query(ctx, sql, id)
	if err != nil {
		return nil, fmt.Errorf("check references: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read references: %w", err)
	}
	return names, nil
}

func inUseError(entity string, users []string) error {
	return &ConflictError{Message: fmt.Sprintf("This %s is still used by %s. Remove it from those first.", entity, strings.Join(users, ", "))}
}

func orEmpty[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// Revision filters and listing.

type RevisionFilter struct {
	EntityType string
	EntityID   string
	// Types the reader may see; revisions of other types are left out
	Types  []string
	Scope  Scope
	Limit  int
	Offset int
}
