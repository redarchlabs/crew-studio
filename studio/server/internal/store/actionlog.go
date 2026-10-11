package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const actionColumns = `a.id, a.crew_id, a.schedule_id, COALESCE(sc.name, ''), a.subject, a.action, a.run_id, a.dry_run,
	a.details, a.idempotency_key, a.at`

const actionFrom = ` FROM action_log a LEFT JOIN schedules sc ON sc.id = a.schedule_id`

func scanAction(row pgx.Row) (model.ActionEntry, error) {
	var e model.ActionEntry
	err := row.Scan(&e.ID, &e.CrewID, &e.ScheduleID, &e.ScheduleName, &e.Subject, &e.Action, &e.RunID, &e.DryRun,
		&e.Details, &e.IdempotencyKey, &e.At)
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	return e, err
}

// RecordAction adds an entry to a crew's action log. If the idempotency key was already recorded for the crew,
// nothing is added and the earlier entry is returned with duplicate true. The exception is a dry-run entry followed
// by a real one with the same key (dry run was turned off): the real one replaces it, so it counts as sent.
func (s *Store) RecordAction(ctx context.Context, scope StateScope, runID *string, in model.LogRecord) (model.ActionEntry, bool, error) {
	details, err := json.Marshal(in.Details)
	if err != nil {
		return model.ActionEntry{}, false, fmt.Errorf("encode action details: %w", err)
	}
	var key *string
	if in.IdempotencyKey != "" {
		key = &in.IdempotencyKey
	}
	var id string
	err = s.pool.QueryRow(ctx,
		`INSERT INTO action_log (crew_id, schedule_id, subject, action, run_id, details, idempotency_key, dry_run)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (crew_id, idempotency_key) DO UPDATE SET schedule_id = EXCLUDED.schedule_id, subject = EXCLUDED.subject,
		   action = EXCLUDED.action, run_id = EXCLUDED.run_id, details = EXCLUDED.details, dry_run = false, at = now()
		 WHERE action_log.dry_run AND NOT EXCLUDED.dry_run
		 RETURNING id`,
		scope.CrewID, scope.ScheduleID, in.Subject, in.Action, runID, details, key, scope.DryRun).Scan(&id)
	duplicate := false
	switch {
	case err == nil:
	case key != nil && isNoRows(err):
		duplicate = true
		err = s.pool.QueryRow(ctx, `SELECT a.id FROM action_log a WHERE a.crew_id = $1 AND a.idempotency_key = $2`,
			scope.CrewID, *key).Scan(&id)
		if err != nil {
			return model.ActionEntry{}, false, mapErr(err, "action log entry", "")
		}
	default:
		return model.ActionEntry{}, false, mapErr(err, "action log entry", "")
	}
	entry, err := scanAction(s.pool.QueryRow(ctx, `SELECT `+actionColumns+actionFrom+` WHERE a.id = $1`, id))
	return entry, duplicate, mapErr(err, "action log entry", "")
}

// CheckAction returns the latest entry for subject and action in the scope (at or after since, when set), or
// nil when there is none. Dry-run entries count only for a dry-run scope.
func (s *Store) CheckAction(ctx context.Context, scope StateScope, subject, action string, since *time.Time) (*model.ActionEntry, error) {
	entry, err := scanAction(s.pool.QueryRow(ctx, `SELECT `+actionColumns+actionFrom+`
		WHERE a.crew_id = $1 AND a.schedule_id IS NOT DISTINCT FROM $2::uuid AND a.subject = $3 AND a.action = $4
		  AND ($5::timestamptz IS NULL OR a.at >= $5) AND (NOT a.dry_run OR $6)
		ORDER BY a.at DESC LIMIT 1`, scope.CrewID, scope.ScheduleID, subject, action, since, scope.DryRun))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("check action log: %w", err)
	}
	return &entry, nil
}

// ActionFilter pages through one crew's action log. Subject and Action match anywhere, ignoring case.
type ActionFilter struct {
	Scope        StateScope
	AllSchedules bool
	Subject      string
	Action       string
	Limit        int
	Offset       int
}

// ListActions returns a page of the log, newest first, and the total that match.
func (s *Store) ListActions(ctx context.Context, f ActionFilter) ([]model.ActionEntry, int, error) {
	const where = ` WHERE a.crew_id = $1 AND ($2 OR a.schedule_id IS NOT DISTINCT FROM $3::uuid)
		AND ($4 = '' OR strpos(lower(a.subject), lower($4)) > 0) AND ($5 = '' OR strpos(lower(a.action), lower($5)) > 0)`
	args := []any{f.Scope.CrewID, f.AllSchedules, f.Scope.ScheduleID, f.Subject, f.Action}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM action_log a`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count action log: %w", err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+actionColumns+actionFrom+where+` ORDER BY a.at DESC, a.id LIMIT $6 OFFSET $7`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list action log: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ActionEntry, error) { return scanAction(r) })
	if err != nil {
		return nil, 0, fmt.Errorf("read action log: %w", err)
	}
	return orEmpty(items), total, nil
}

// GetAction returns one entry of a crew's log, or ErrNotFound.
func (s *Store) GetAction(ctx context.Context, crewID, id string) (model.ActionEntry, error) {
	e, err := scanAction(s.pool.QueryRow(ctx, `SELECT `+actionColumns+actionFrom+` WHERE a.crew_id = $1 AND a.id = $2`, crewID, id))
	return e, mapErr(err, "action log entry", "")
}

// DeleteAction removes one entry of a crew's log.
func (s *Store) DeleteAction(ctx context.Context, crewID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM action_log WHERE crew_id = $1 AND id = $2`, crewID, id)
	if err != nil {
		return fmt.Errorf("delete action log entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
