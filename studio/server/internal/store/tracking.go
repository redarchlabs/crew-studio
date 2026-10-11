package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// A crew's tracking list: tracker items it follows with their child tickets (design section 12). Only people
// change it (on the State tab or by confirming a chat proposal); runs can only read it. Removed items are kept
// for history.

const trackingSelect = `SELECT t.id, t.crew_id, t.schedule_id, COALESCE(sc.name, ''), t.connection_id, cn.name, cn.kind,
	t.project, t.item_ref, t.include_children, t.note, t.lead_account_id, COALESCE(lp.display_name, ''), t.added_by,
	COALESCE(ua.name, ''), t.added_at, t.removed_at, COALESCE(ur.name, '')
	FROM tracking_items t
	JOIN connections cn ON cn.id = t.connection_id
	LEFT JOIN schedules sc ON sc.id = t.schedule_id
	LEFT JOIN tracker_people lp ON lp.connection_id = t.connection_id AND lp.account_id = t.lead_account_id AND t.lead_account_id <> ''
	LEFT JOIN users ua ON ua.id = t.added_by
	LEFT JOIN users ur ON ur.id = t.removed_by`

func scanTrackingItem(row pgx.Row) (model.TrackingItem, error) {
	var t model.TrackingItem
	err := row.Scan(&t.ID, &t.CrewID, &t.ScheduleID, &t.ScheduleName, &t.ConnectionID, &t.ConnectionName, &t.ConnectionKind,
		&t.Project, &t.ItemRef, &t.IncludeChildren, &t.Note, &t.LeadAccountID, &t.LeadName, &t.AddedBy, &t.AddedByName, &t.AddedAt,
		&t.RemovedAt, &t.RemovedByName)
	return t, err
}

func collectTrackingItems(rows pgx.Rows, err error) ([]model.TrackingItem, error) {
	if err != nil {
		return nil, fmt.Errorf("list tracking items: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.TrackingItem, error) { return scanTrackingItem(r) })
	if err != nil {
		return nil, fmt.Errorf("read tracking items: %w", err)
	}
	return orEmpty(items), nil
}

// TrackingFilter selects a crew's tracking list.
type TrackingFilter struct {
	CrewID string
	// ScheduleID, when set, keeps the crew-wide items and that schedule's (what its tracking_list watch reads)
	ScheduleID *string
	// IncludeRemoved also returns removed items, newest first after the tracked ones
	IncludeRemoved bool
	Limit          int
}

// ListTrackingItems returns a crew's tracking list, by project and item.
func (s *Store) ListTrackingItems(ctx context.Context, f TrackingFilter) ([]model.TrackingItem, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 1000
	}
	return collectTrackingItems(s.pool.Query(ctx, trackingSelect+`
		WHERE t.crew_id = $1 AND ($2::uuid IS NULL OR t.schedule_id IS NULL OR t.schedule_id = $2) AND ($3 OR t.removed_at IS NULL)
		ORDER BY t.removed_at DESC NULLS FIRST, lower(t.project), t.item_ref LIMIT $4`, f.CrewID, f.ScheduleID, f.IncludeRemoved, limit))
}

// GetTrackingItem returns one item of a crew's list (tracked or removed), or ErrNotFound.
func (s *Store) GetTrackingItem(ctx context.Context, crewID, id string) (model.TrackingItem, error) {
	t, err := scanTrackingItem(s.pool.QueryRow(ctx, trackingSelect+` WHERE t.crew_id = $1 AND t.id = $2`, crewID, id))
	return t, mapErr(err, "tracking item", "")
}

// ErrAlreadyTracked means the item is already on the list (for the same schedule and connection).
var ErrAlreadyTracked = &ConflictError{Code: "already_tracked", Message: "This item is already on the tracking list."}

// AddTrackingItem puts a normalized item on a crew's list, added by userID (nil for none).
func (s *Store) AddTrackingItem(ctx context.Context, crewID string, in model.TrackingItemInput, userID *string) (model.TrackingItem, error) {
	var id string
	err := s.pool.QueryRow(ctx, `INSERT INTO tracking_items (crew_id, schedule_id, connection_id, project, item_ref,
		include_children, note, lead_account_id, added_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		crewID, in.ScheduleID, in.ConnectionID, in.Project, in.ItemRef, in.IncludeChildren, in.Note, in.LeadAccountID,
		nullableUUID(derefString(userID))).Scan(&id)
	if isUniqueViolation(err) {
		return model.TrackingItem{}, ErrAlreadyTracked
	}
	if err != nil {
		return model.TrackingItem{}, mapErr(err, "tracking item", "")
	}
	return s.GetTrackingItem(ctx, crewID, id)
}

// RemoveTrackingItem takes an item off the list (it stays as history) and returns it as it was.
func (s *Store) RemoveTrackingItem(ctx context.Context, crewID, id string, userID *string, at time.Time) (model.TrackingItem, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE tracking_items SET removed_at = $3, removed_by = $4
		WHERE crew_id = $1 AND id = $2 AND removed_at IS NULL`, crewID, id, at, nullableUUID(derefString(userID)))
	if err != nil {
		return model.TrackingItem{}, mapErr(err, "tracking item", "")
	}
	if tag.RowsAffected() == 0 {
		return model.TrackingItem{}, ErrNotFound
	}
	return s.GetTrackingItem(ctx, crewID, id)
}

// RestoreTrackingItem puts a removed item back on the list (undo); ErrAlreadyTracked when it was added again since.
func (s *Store) RestoreTrackingItem(ctx context.Context, crewID, id string) (model.TrackingItem, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE tracking_items SET removed_at = NULL, removed_by = NULL
		WHERE crew_id = $1 AND id = $2 AND removed_at IS NOT NULL`, crewID, id)
	if isUniqueViolation(err) {
		return model.TrackingItem{}, ErrAlreadyTracked
	}
	if err != nil {
		return model.TrackingItem{}, mapErr(err, "tracking item", "")
	}
	if tag.RowsAffected() == 0 {
		return model.TrackingItem{}, ErrNotFound
	}
	return s.GetTrackingItem(ctx, crewID, id)
}

// TrackingLeads returns the lead overrides set on a connection's tracked items (people a follow-up may mention).
func (s *Store) TrackingLeads(ctx context.Context, crewID, connectionID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT lead_account_id FROM tracking_items
		WHERE crew_id = $1 AND connection_id = $2 AND removed_at IS NULL AND lead_account_id <> ''`, crewID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("list tracking leads: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read tracking leads: %w", err)
	}
	return orEmpty(ids), nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
