package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// Watches belong to schedules (at most one each). Their snapshots are a cache; their checks are history.

const (
	// watchCheckRetention is how long check history is kept
	watchCheckRetention = 90 * 24 * time.Hour
	// statsRunSample is how many recent finished runs give a crew's average tokens per run
	statsRunSample = 20
)

const watchSelect = `SELECT w.id, w.schedule_id, sc.name, sc.crew_id, w.connection_id, COALESCE(cn.name, ''), w.source,
	w.id_field, w.tracked_fields, w.run_when, w.followups, w.dry_run, w.max_items, w.mention_cap_per_day, w.comment_cap_per_run,
	w.last_checked_at, w.last_changed_at,
	w.last_error, w.snapshot_at, (SELECT count(*) FROM watch_snapshots s WHERE s.watch_id = w.id)::int,
	w.created_at, w.updated_at
	FROM watches w JOIN schedules sc ON sc.id = w.schedule_id LEFT JOIN connections cn ON cn.id = w.connection_id`

func scanWatch(row pgx.Row) (model.Watch, error) {
	var w model.Watch
	var source, followups []byte
	err := row.Scan(&w.ID, &w.ScheduleID, &w.ScheduleName, &w.CrewID, &w.ConnectionID, &w.ConnectionName, &source,
		&w.IDField, &w.TrackedFields, &w.RunWhen, &followups, &w.DryRun, &w.MaxItems, &w.MentionCapPerDay, &w.CommentCapPerRun,
		&w.LastCheckedAt, &w.LastChangedAt,
		&w.LastError, &w.SnapshotAt, &w.SnapshotItems, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return w, err
	}
	if err := json.Unmarshal(source, &w.Source); err != nil {
		return w, fmt.Errorf("read watch source: %w", err)
	}
	if len(followups) > 0 && string(followups) != "null" {
		var l model.FollowupLadder
		if err := json.Unmarshal(followups, &l); err != nil {
			return w, fmt.Errorf("read watch followups: %w", err)
		}
		w.Followups = &l
	}
	w.TrackedFields = orEmpty(w.TrackedFields)
	return w, nil
}

// WatchOfSchedule returns the schedule's watch, or ErrNotFound when it has none.
func (s *Store) WatchOfSchedule(ctx context.Context, scheduleID string) (model.Watch, error) {
	w, err := scanWatch(s.pool.QueryRow(ctx, watchSelect+` WHERE w.schedule_id = $1`, scheduleID))
	return w, mapErr(err, "watch", "")
}

// CrewWatches returns the watches of a crew's schedules, by schedule name.
func (s *Store) CrewWatches(ctx context.Context, crewID string) ([]model.Watch, error) {
	rows, err := s.pool.Query(ctx, watchSelect+` WHERE sc.crew_id = $1 ORDER BY sc.name`, crewID)
	if err != nil {
		return nil, fmt.Errorf("list watches: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Watch, error) { return scanWatch(r) })
	if err != nil {
		return nil, fmt.Errorf("read watches: %w", err)
	}
	return orEmpty(items), nil
}

// SaveWatch creates or replaces a schedule's watch from a normalized input. When the new definition would not
// produce comparable snapshots (another source, id or fields), the snapshot is dropped and the next check is the
// first.
func (s *Store) SaveWatch(ctx context.Context, scheduleID string, in model.WatchInput) (model.Watch, error) {
	source, err := json.Marshal(in.Source)
	if err != nil {
		return model.Watch{}, fmt.Errorf("encode watch source: %w", err)
	}
	var followups []byte
	if in.Followups != nil {
		if followups, err = json.Marshal(in.Followups); err != nil {
			return model.Watch{}, fmt.Errorf("encode watch followups: %w", err)
		}
	}
	dryRun := in.DryRun == nil || *in.DryRun
	var out model.Watch
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := scanWatch(tx.QueryRow(ctx, watchSelect+` WHERE w.schedule_id = $1 FOR UPDATE OF w`, scheduleID))
		exists := err == nil
		if err != nil && !isNoRows(err) {
			return fmt.Errorf("read watch: %w", err)
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO watches (schedule_id, connection_id, source, id_field, tracked_fields, run_when, followups, dry_run,
			  max_items, created_by, mention_cap_per_day, comment_cap_per_run)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 ON CONFLICT (schedule_id) DO UPDATE SET connection_id = EXCLUDED.connection_id, source = EXCLUDED.source,
			  id_field = EXCLUDED.id_field, tracked_fields = EXCLUDED.tracked_fields, run_when = EXCLUDED.run_when,
			  followups = EXCLUDED.followups, dry_run = EXCLUDED.dry_run, max_items = EXCLUDED.max_items,
			  mention_cap_per_day = EXCLUDED.mention_cap_per_day, comment_cap_per_run = EXCLUDED.comment_cap_per_run,
			  last_error = NULL, updated_at = now()`,
			scheduleID, in.ConnectionID, source, in.IDField, orEmpty(in.TrackedFields), in.RunWhen, followups, dryRun,
			in.MaxItems, nullableUUID(actorFrom(ctx).ID), capOr(in.MentionCapPerDay, model.DefaultMentionCapPerDay),
			capOr(in.CommentCapPerRun, model.DefaultCommentCapPerRun))
		if err != nil {
			return mapErr(err, "watch", "")
		}
		if exists && !model.SameSnapshotShape(old, in) {
			if err := clearSnapshot(ctx, tx, old.ID); err != nil {
				return err
			}
		}
		out, err = scanWatch(tx.QueryRow(ctx, watchSelect+` WHERE w.schedule_id = $1`, scheduleID))
		return mapErr(err, "watch", "")
	})
	return out, err
}

// DeleteWatch removes a schedule's watch with its snapshot and check history.
func (s *Store) DeleteWatch(ctx context.Context, scheduleID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM watches WHERE schedule_id = $1`, scheduleID)
	if err != nil {
		return fmt.Errorf("delete watch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LoadSnapshot returns a watch's stored snapshot (item id to tracked fields), numbers kept exact.
func (s *Store) LoadSnapshot(ctx context.Context, watchID string) (map[string]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT item_id, fields FROM watch_snapshots WHERE watch_id = $1`, watchID)
	if err != nil {
		return nil, fmt.Errorf("load snapshot: %w", err)
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("read snapshot: %w", err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		fields := map[string]any{}
		if err := dec.Decode(&fields); err != nil {
			return nil, fmt.Errorf("decode snapshot item: %w", err)
		}
		out[id] = fields
	}
	return out, rows.Err()
}

// ErrWatchChanged means the watch was edited (or its snapshot cleared) after a check read it, so that check's
// snapshot is not saved: it would not match the new definition.
var ErrWatchChanged = &ConflictError{Code: "watch_changed", Message: "The watch changed while it was being checked."}

// watchUnchanged locks the watch row and reports whether it is still as the check read it (by updated_at).
func watchUnchanged(ctx context.Context, tx pgx.Tx, watchID string, readAt time.Time) error {
	var updated time.Time
	if err := tx.QueryRow(ctx, `SELECT updated_at FROM watches WHERE id = $1 FOR UPDATE`, watchID).Scan(&updated); err != nil {
		return mapErr(err, "watch", "")
	}
	if !updated.Equal(readAt) {
		return ErrWatchChanged
	}
	return nil
}

// SaveSnapshot replaces a watch's snapshot with items and marks it confirmed at at. readAt is the watch's
// updated_at when the check read it; if the watch changed since, nothing is saved and ErrWatchChanged is returned.
func (s *Store) SaveSnapshot(ctx context.Context, watchID string, readAt time.Time, items map[string]map[string]any, at time.Time) error {
	ids := make([]string, 0, len(items))
	fields := make([]string, 0, len(items))
	for id, f := range items {
		data, err := json.Marshal(f)
		if err != nil {
			return fmt.Errorf("encode snapshot item: %w", err)
		}
		ids = append(ids, id)
		fields = append(fields, string(data))
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := watchUnchanged(ctx, tx, watchID, readAt); err != nil {
			return err
		}
		if err := clearSnapshot(ctx, tx, watchID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO watch_snapshots (watch_id, item_id, fields, seen_at)
			SELECT $1, i, f::jsonb, $4 FROM unnest($2::text[], $3::text[]) AS x (i, f)`, watchID, ids, fields, at); err != nil {
			return fmt.Errorf("save snapshot: %w", err)
		}
		return confirmSnapshot(ctx, tx, watchID, at)
	})
}

// ConfirmSnapshot records that a check found the snapshot still current (nothing changed). Like SaveSnapshot it
// does nothing (ErrWatchChanged) when the watch changed after the check read it.
func (s *Store) ConfirmSnapshot(ctx context.Context, watchID string, readAt, at time.Time) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := watchUnchanged(ctx, tx, watchID, readAt); err != nil {
			return err
		}
		return confirmSnapshot(ctx, tx, watchID, at)
	})
}

func confirmSnapshot(ctx context.Context, q queryer, watchID string, at time.Time) error {
	if _, err := q.Exec(ctx, `UPDATE watches SET snapshot_at = $2 WHERE id = $1`, watchID, at); err != nil {
		return fmt.Errorf("confirm snapshot: %w", err)
	}
	return nil
}

// ClearSnapshot deletes a watch's snapshot; the next check is the first again. It counts as a change to the watch,
// so a check already in progress does not save its snapshot over the cleared one.
func (s *Store) ClearSnapshot(ctx context.Context, watchID string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := clearSnapshot(ctx, tx, watchID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE watches SET updated_at = now() WHERE id = $1`, watchID); err != nil {
			return fmt.Errorf("clear snapshot: %w", err)
		}
		return nil
	})
}

func clearSnapshot(ctx context.Context, q queryer, watchID string) error {
	if _, err := q.Exec(ctx, `DELETE FROM watch_snapshots WHERE watch_id = $1`, watchID); err != nil {
		return fmt.Errorf("clear snapshot: %w", err)
	}
	if _, err := q.Exec(ctx, `UPDATE watches SET snapshot_at = NULL WHERE id = $1`, watchID); err != nil {
		return fmt.Errorf("clear snapshot: %w", err)
	}
	return nil
}

// RecordWatchCheck stores one check, updates the watch's status and drops history past its retention.
func (s *Store) RecordWatchCheck(ctx context.Context, watchID string, c model.WatchCheck) error {
	var lastError *string
	if c.Outcome == model.WatchCheckError {
		lastError = &c.Message
	}
	changed := c.Added+c.Changed+c.Removed > 0 && c.Outcome == model.WatchCheckStarted
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO watch_checks (watch_id, checked_at, outcome, added, changed, removed, unchanged,
			followups_due, work_order_id, message) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			watchID, c.CheckedAt, c.Outcome, c.Added, c.Changed, c.Removed, c.Unchanged, c.FollowupsDue, c.WorkOrderID, c.Message); err != nil {
			return mapErr(err, "watch check", "")
		}
		if _, err := tx.Exec(ctx, `UPDATE watches SET last_checked_at = $2, last_error = $3,
			last_changed_at = CASE WHEN $4 THEN $2 ELSE last_changed_at END WHERE id = $1`,
			watchID, c.CheckedAt, lastError, changed); err != nil {
			return fmt.Errorf("update watch status: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM watch_checks WHERE watch_id = $1 AND checked_at < $2`,
			watchID, c.CheckedAt.Add(-watchCheckRetention)); err != nil {
			return fmt.Errorf("sweep watch checks: %w", err)
		}
		return nil
	})
}

// ListWatchChecks returns a watch's most recent checks, newest first.
func (s *Store) ListWatchChecks(ctx context.Context, watchID string, limit int) ([]model.WatchCheck, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, checked_at, outcome, added, changed, removed, unchanged, followups_due,
		work_order_id, message FROM watch_checks WHERE watch_id = $1 ORDER BY checked_at DESC, id LIMIT $2`, watchID, limit)
	if err != nil {
		return nil, fmt.Errorf("list watch checks: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.WatchCheck, error) {
		var c model.WatchCheck
		err := r.Scan(&c.ID, &c.CheckedAt, &c.Outcome, &c.Added, &c.Changed, &c.Removed, &c.Unchanged, &c.FollowupsDue,
			&c.WorkOrderID, &c.Message)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("read watch checks: %w", err)
	}
	return orEmpty(items), nil
}

// WatchStats counts a watch's checks over the last days and estimates what skipped runs saved, from the crew's
// average tokens and cost over its recent finished runs.
func (s *Store) WatchStats(ctx context.Context, watchID string, crewID *string, days int, now time.Time) (model.WatchStats, error) {
	st := model.WatchStats{Days: days}
	since := now.Add(-time.Duration(days) * 24 * time.Hour)
	err := s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE outcome = 'started'),
		count(*) FILTER (WHERE outcome = 'no_changes') FROM watch_checks WHERE watch_id = $1 AND checked_at >= $2`,
		watchID, since).Scan(&st.Checks, &st.RunsStarted, &st.RunsSkipped)
	if err != nil {
		return st, fmt.Errorf("count watch checks: %w", err)
	}
	if crewID == nil {
		return st, nil
	}
	var tokens, cost float64
	err = s.pool.QueryRow(ctx, `SELECT COALESCE(avg(t.tokens), 0), COALESCE(avg(t.cost), 0) FROM (
		SELECT COALESCE((r.usage->>'prompt_tokens')::bigint, 0) + COALESCE((r.usage->>'completion_tokens')::bigint, 0) AS tokens,
		       COALESCE(r.cost_usd, 0) AS cost
		FROM runs r WHERE r.crew_id = $1 AND r.status = 'succeeded' AND r.usage IS NOT NULL
		ORDER BY r.finished_at DESC NULLS LAST LIMIT $2) t`, *crewID, statsRunSample).Scan(&tokens, &cost)
	if err != nil {
		return st, fmt.Errorf("average run tokens: %w", err)
	}
	st.AvgRunTokens = int(tokens)
	st.EstTokensSaved = st.RunsSkipped * st.AvgRunTokens
	st.EstCostSavedUSD = float64(st.RunsSkipped) * cost
	return st, nil
}

// FollowupSent is the latest follow-up log entry for one subject and action.
type FollowupSent struct {
	Subject string
	Action  string
	At      time.Time
}

// LatestFollowups returns when each follow-up action (followup:...) was last recorded for a crew's schedule.
// Dry-run entries count only when includeDryRun is set, so turning dry run off does not count them as sent.
func (s *Store) LatestFollowups(ctx context.Context, crewID, scheduleID string, includeDryRun bool) ([]FollowupSent, error) {
	rows, err := s.pool.Query(ctx, `SELECT subject, action, max(at) FROM action_log
		WHERE crew_id = $1 AND schedule_id = $2 AND action LIKE 'followup:%' AND (NOT dry_run OR $3)
		GROUP BY subject, action`, crewID, scheduleID, includeDryRun)
	if err != nil {
		return nil, fmt.Errorf("list follow-ups sent: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (FollowupSent, error) {
		var f FollowupSent
		err := r.Scan(&f.Subject, &f.Action, &f.At)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("read follow-ups sent: %w", err)
	}
	return orEmpty(items), nil
}

// CrewConnectionUse says whether a crew may use a connection: it is linked to the crew and shares an area with it.
type CrewConnectionUse struct {
	Connection model.Connection
	Linked     bool
	SharesArea bool
}

// ConnectionForCrew loads a connection and how the crew relates to it (ErrNotFound when it does not exist).
func (s *Store) ConnectionForCrew(ctx context.Context, connectionID, crewID string) (CrewConnectionUse, error) {
	c, err := s.GetConnection(ctx, connectionID)
	if err != nil {
		return CrewConnectionUse{}, err
	}
	use := CrewConnectionUse{Connection: c}
	err = s.pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM crew_connections WHERE crew_id = $1 AND connection_id = $2),
		EXISTS (SELECT 1 FROM crew_areas ca JOIN connections x ON x.id = $2 WHERE ca.crew_id = $1 AND ca.area_id = ANY(x.area_ids))`,
		crewID, connectionID).Scan(&use.Linked, &use.SharesArea)
	if err != nil {
		return CrewConnectionUse{}, fmt.Errorf("check crew connection: %w", err)
	}
	return use, nil
}

// capOr is a cap, or its default when unset (inputs that skipped normalization).
func capOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
