package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// KM2 saves are reserved in the action log before Studio sends anything to KM2, and finished when KM2 answers. The
// reservation is made under a short advisory lock per owner (a run, or a person promoting memories) in one
// transaction, so parallel calls cannot all pass the owner's cap or both send the same document, and no lock (or
// pooled connection) is held while KM2 works.

// KM2 save states kept in an entry's details.outcome while it is not finished.
const (
	KM2Sending = "sending"
	// KM2Unknown is a save whose answer never came (KM2 failed or timed out after it was sent); it counts against the
	// cap, and sending it again in the same run is allowed
	KM2Unknown = "unknown"
)

// KM2Limits bound saves: per owner (see KM2Owner), and per connection over the last 24 hours (real saves only, not
// dry runs).
type KM2Limits struct {
	PerOwner         int
	PerConnectionDay int
	ConnectionID     string
}

// KM2Owner is who a save counts against: a run (km_save_document, report delivery) or a person promoting memories
// from a crew's Memory tab.
type KM2Owner struct {
	// Key names the owner for the reservation lock: run:<id> or user:<id>
	Key string
	// RunID is stored on the action log entry; nil for a person's save
	RunID *string
	// Count counts the owner's saves against KM2Limits.PerOwner
	Count func(ctx context.Context, q QueryRower) (int, error)
}

// RunKM2Owner counts every save a run made, tried or (in a dry run) would have made.
func RunKM2Owner(runID string) KM2Owner {
	return KM2Owner{Key: "run:" + runID, RunID: &runID,
		Count: func(ctx context.Context, q QueryRower) (int, error) { return countKM2Saves(ctx, q, runID) }}
}

// PersonKM2Owner counts the saves a person made from the Memory tab in the last 24 hours (details.promoted_by).
func PersonKM2Owner(userID string) KM2Owner {
	return KM2Owner{Key: "user:" + userID, Count: func(ctx context.Context, q QueryRower) (int, error) {
		var n int
		err := q.QueryRow(ctx, `SELECT count(*) FROM action_log WHERE action = $1 AND run_id IS NULL
			AND details->>'promoted_by' = $2 AND at > now() - interval '24 hours'`, model.ActionKM2Save, userID).Scan(&n)
		if err != nil {
			return 0, fmt.Errorf("count KM2 promotions: %w", err)
		}
		return n, nil
	}}
}

// KM2Reservation is the result of ReserveKM2Save: a new (or retried) entry to send, a prior entry for the same
// document, or a limit reached.
type KM2Reservation struct {
	Entry *model.ActionEntry
	Prior *model.ActionEntry
	// Saves is how many saves the owner has (when its cap was reached)
	Saves         int
	CapReached    bool
	DayCapReached bool
}

// RunKM2Saves counts the KM2 saves a run made, tried or (in a dry run) would have made.
func (s *Store) RunKM2Saves(ctx context.Context, runID string) (int, error) {
	return countKM2Saves(ctx, s.pool, runID)
}

// QueryRower runs a query returning one row: a pool or a transaction.
type QueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func countKM2Saves(ctx context.Context, q QueryRower, runID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM action_log WHERE run_id = $1 AND action = $2`, runID, model.ActionKM2Save).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count KM2 saves: %w", err)
	}
	return n, nil
}

// ReserveKM2Save records a km2:save entry for rec (whose IdempotencyKey identifies the document for its owner) unless
// the crew already has one for that key or the owner has reached its cap. Dry-run entries are final when reserved;
// others are stored with outcome "sending" and finished with FinishKM2Save or dropped with DropKM2Save.
func (s *Store) ReserveKM2Save(ctx context.Context, scope StateScope, owner KM2Owner, rec model.LogRecord, limits KM2Limits) (KM2Reservation, error) {
	var out KM2Reservation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('km2-save:' || $1, 0))`, owner.Key); err != nil {
			return fmt.Errorf("lock KM2 saves: %w", err)
		}
		prior, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+actionFrom+` WHERE a.crew_id = $1 AND a.idempotency_key = $2`,
			scope.CrewID, rec.IdempotencyKey))
		switch {
		case err == nil && prior.Details["outcome"] == KM2Unknown:
			// Sent before without an answer: send again, under the same entry
			out.Entry, err = setKM2Outcome(ctx, tx, prior.ID, mergeDetails(prior.Details, map[string]any{"outcome": KM2Sending}))
			return err
		case err == nil:
			out.Prior = &prior
			return nil
		case !isNoRows(err):
			return fmt.Errorf("read KM2 save: %w", err)
		}
		if out.Saves, err = owner.Count(ctx, tx); err != nil {
			return err
		}
		if out.Saves >= limits.PerOwner {
			out.CapReached = true
			return nil
		}
		if !scope.DryRun {
			var day int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM action_log WHERE action = $1 AND NOT dry_run
				AND details->>'connection_id' = $2 AND at > now() - interval '24 hours'`, model.ActionKM2Save, limits.ConnectionID).Scan(&day); err != nil {
				return fmt.Errorf("count KM2 saves of the connection: %w", err)
			}
			if day >= limits.PerConnectionDay {
				out.DayCapReached = true
				return nil
			}
		}
		details := rec.Details
		if !scope.DryRun {
			details = mergeDetails(details, map[string]any{"outcome": KM2Sending})
		}
		out.Entry, err = insertKM2Save(ctx, tx, scope, owner.RunID, rec, details)
		return err
	})
	return out, err
}

func insertKM2Save(ctx context.Context, tx pgx.Tx, scope StateScope, runID *string, rec model.LogRecord, details map[string]any) (*model.ActionEntry, error) {
	raw, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("encode KM2 save: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO action_log (crew_id, schedule_id, subject, action, run_id, details, idempotency_key, dry_run)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		scope.CrewID, scope.ScheduleID, rec.Subject, model.ActionKM2Save, runID, raw, rec.IdempotencyKey, scope.DryRun).Scan(&id)
	if err != nil {
		return nil, mapErr(err, "action log entry", "")
	}
	e, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+actionFrom+` WHERE a.id = $1`, id))
	return &e, mapErr(err, "action log entry", "")
}

func setKM2Outcome(ctx context.Context, tx pgx.Tx, id string, details map[string]any) (*model.ActionEntry, error) {
	raw, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("encode KM2 save: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE action_log SET details = $2, at = now() WHERE id = $1`, id, raw); err != nil {
		return nil, fmt.Errorf("update KM2 save: %w", err)
	}
	e, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+actionFrom+` WHERE a.id = $1`, id))
	return &e, mapErr(err, "action log entry", "")
}

// FinishKM2Save replaces a reserved save's details (with KM2's outcome, or KM2Unknown) and returns the entry.
func (s *Store) FinishKM2Save(ctx context.Context, id string, details map[string]any) (model.ActionEntry, error) {
	var out *model.ActionEntry
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		out, err = setKM2Outcome(ctx, tx, id, details)
		return err
	})
	if err != nil {
		return model.ActionEntry{}, err
	}
	return *out, nil
}

// DropKM2Save removes a reserved save that KM2 refused, so it does not count against the cap.
func (s *Store) DropKM2Save(ctx context.Context, id string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM action_log WHERE id = $1 AND action = $2`, id, model.ActionKM2Save); err != nil {
		return fmt.Errorf("drop KM2 save: %w", err)
	}
	return nil
}

// mergeDetails returns a copy of base with extra's fields set.
func mergeDetails(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
