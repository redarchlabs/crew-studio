package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// A tracker connection's people map (design section 10): the tracker accounts it has seen, linked to Studio users
// by email where the tracker shows one, or by hand.

const personSelect = `SELECT p.connection_id, p.account_id, p.display_name, p.email, p.user_id, COALESCE(u.name, ''), p.link,
	p.seen_at, p.updated_at FROM tracker_people p LEFT JOIN users u ON u.id = p.user_id`

func scanPerson(row pgx.Row) (model.TrackerPerson, error) {
	var p model.TrackerPerson
	err := row.Scan(&p.ConnectionID, &p.AccountID, &p.DisplayName, &p.Email, &p.UserID, &p.UserName, &p.Link, &p.SeenAt, &p.UpdatedAt)
	return p, err
}

// ListTrackerPeople returns a connection's people map, by name.
func (s *Store) ListTrackerPeople(ctx context.Context, connectionID string) ([]model.TrackerPerson, error) {
	rows, err := s.pool.Query(ctx, personSelect+` WHERE p.connection_id = $1 ORDER BY lower(p.display_name), p.account_id`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("list tracker people: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.TrackerPerson, error) { return scanPerson(r) })
	if err != nil {
		return nil, fmt.Errorf("read tracker people: %w", err)
	}
	return orEmpty(items), nil
}

// TrackerPerson returns one account of a connection's people map, or ErrNotFound.
func (s *Store) TrackerPerson(ctx context.Context, connectionID, accountID string) (model.TrackerPerson, error) {
	p, err := scanPerson(s.pool.QueryRow(ctx, personSelect+` WHERE p.connection_id = $1 AND p.account_id = $2`, connectionID, accountID))
	return p, mapErr(err, "tracker person", "")
}

// SeeTrackerPeople records accounts read from the tracker (names and, when shown, emails) and links each one not
// linked by hand to the active Studio user with the same email. It returns how many accounts are now linked.
func (s *Store) SeeTrackerPeople(ctx context.Context, connectionID string, people []model.TrackerPerson, at time.Time) (int, error) {
	ids := make([]string, 0, len(people))
	names := make([]string, 0, len(people))
	emails := make([]string, 0, len(people))
	for _, p := range people {
		ids, names, emails = append(ids, p.AccountID), append(names, p.DisplayName), append(emails, p.Email)
	}
	var linked int
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tracker_people (connection_id, account_id, display_name, email, seen_at, updated_at)
			SELECT $1, a, n, e, $5, $5 FROM unnest($2::text[], $3::text[], $4::text[]) AS x (a, n, e)
			ON CONFLICT (connection_id, account_id) DO UPDATE SET display_name = EXCLUDED.display_name,
			  email = CASE WHEN EXCLUDED.email <> '' THEN EXCLUDED.email ELSE tracker_people.email END, seen_at = EXCLUDED.seen_at`,
			connectionID, ids, names, emails, at); err != nil {
			return mapErr(err, "tracker person", "")
		}
		if _, err := tx.Exec(ctx, `UPDATE tracker_people p SET user_id = u.id, link = 'email', updated_at = $2
			FROM users u WHERE p.connection_id = $1 AND p.link <> 'manual' AND p.email <> '' AND u.kind = 'human'
			  AND u.status = 'active' AND lower(u.email) = lower(p.email) AND p.user_id IS DISTINCT FROM u.id`, connectionID, at); err != nil {
			return fmt.Errorf("link tracker people: %w", err)
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM tracker_people WHERE connection_id = $1 AND user_id IS NOT NULL`, connectionID).Scan(&linked)
	})
	return linked, err
}

// LinkTrackerPerson links an account to a Studio user by hand (userID nil clears the link and keeps it cleared).
func (s *Store) LinkTrackerPerson(ctx context.Context, connectionID, accountID string, userID *string) (model.TrackerPerson, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE tracker_people SET user_id = $3, link = 'manual', updated_at = now()
		WHERE connection_id = $1 AND account_id = $2`, connectionID, accountID, userID)
	if err != nil {
		return model.TrackerPerson{}, mapErr(err, "tracker person", "")
	}
	if tag.RowsAffected() == 0 {
		return model.TrackerPerson{}, ErrNotFound
	}
	return s.TrackerPerson(ctx, connectionID, accountID)
}

// MentionsSince counts the crew's tracker posts since a time that mentioned an account (the per-person daily cap).
// Dry-run posts count only for a dry-run caller, as with follow-ups.
func (s *Store) MentionsSince(ctx context.Context, crewID, accountID string, since time.Time, includeDryRun bool) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM action_log WHERE crew_id = $1 AND at >= $2
		AND details->'mentions' ? $3 AND (NOT dry_run OR $4)`, crewID, since, accountID, includeDryRun).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count mentions: %w", err)
	}
	return n, nil
}

// TrackerPostTools are the tools whose posts (or dry-run would-posts) count against a run's comment cap.
var TrackerPostTools = []string{"tracker_comment", "tracker_suggest_change", "tracker_create_issue"}

// RunTrackerPosts counts what a run has posted (or would have, in a dry run) through the tracker tools.
func (s *Store) RunTrackerPosts(ctx context.Context, runID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM action_log WHERE run_id = $1 AND details->>'tool' = ANY($2)`,
		runID, TrackerPostTools).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count tracker posts: %w", err)
	}
	return n, nil
}

// LinkableUsers lists the active people (not service accounts) in any of the areas, for linking tracker accounts.
func (s *Store) LinkableUsers(ctx context.Context, areaIDs []string) ([]model.LinkableUser, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.name, COALESCE(u.email, '') FROM users u
		WHERE u.kind = 'human' AND u.status = 'active'
		  AND (u.all_areas OR EXISTS (SELECT 1 FROM user_areas ua WHERE ua.user_id = u.id AND ua.area_id = ANY($1::uuid[])))
		ORDER BY lower(u.name) LIMIT 2000`, orEmpty(areaIDs))
	if err != nil {
		return nil, fmt.Errorf("list linkable users: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.LinkableUser, error) {
		var u model.LinkableUser
		err := r.Scan(&u.ID, &u.Name, &u.Email)
		return u, err
	})
	if err != nil {
		return nil, fmt.Errorf("read linkable users: %w", err)
	}
	return orEmpty(items), nil
}

// ActionByKey returns the crew's action log entry with an idempotency key, or nil.
func (s *Store) ActionByKey(ctx context.Context, crewID, key string) (*model.ActionEntry, error) {
	e, err := scanAction(s.pool.QueryRow(ctx, `SELECT `+actionColumns+actionFrom+` WHERE a.crew_id = $1 AND a.idempotency_key = $2`, crewID, key))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read action log entry: %w", err)
	}
	return &e, nil
}

// WithCrewPostLock runs fn while holding a database advisory lock for the crew's tracker posts. Posts check the
// per-run comment cap, the per-person mention cap and follow-up repeats against the action log and only log after
// posting, so without the lock parallel tool calls could all pass the checks before any of them is logged.
func (s *Store) WithCrewPostLock(ctx context.Context, crewID string, fn func() error) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("lock tracker posts: %w", err)
	}
	defer conn.Release()
	const key = `hashtextextended('tracker-post:' || $1, 0)`
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(`+key+`)`, crewID); err != nil {
		return fmt.Errorf("lock tracker posts: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(`+key+`)`, crewID); err != nil {
			// A connection that could not unlock must not go back to the pool holding the lock
			_ = conn.Conn().Close(unlockCtx)
		}
	}()
	return fn()
}
