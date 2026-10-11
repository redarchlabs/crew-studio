package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// StateScope is where a crew's state lives: the crew, and optionally one of its schedules (nil is crew-wide).
type StateScope struct {
	CrewID     string
	ScheduleID *string
	// DryRun marks a dry-run caller (a watch's dry run): the log entries it records are dry-run entries, and its
	// log checks also see dry-run entries. Everyone else ignores dry-run entries when checking.
	DryRun bool
}

// NoteKey identifies one note within a scope.
type NoteKey struct {
	StateScope
	Subject string
	Name    string
}

// NoteWriter says who wrote a version: a run (RunID set, Name "run") or a person (their name).
type NoteWriter struct {
	RunID *string
	Name  string
}

// maxNoteVersions is how many versions of a note are kept (and listed); older ones are pruned on write
const maxNoteVersions = 100

// The key columns in notes, matched against $1 crew, $2 schedule, $3 subject and $4 name
const noteKeyWhere = `crew_id = $1 AND schedule_id IS NOT DISTINCT FROM $2::uuid AND subject = $3 AND name = $4`

const noteVersionColumns = `id, crew_id, schedule_id, subject, name, version, content, run_id, written_by, created_at`

func scanNoteVersion(row pgx.Row) (model.NoteVersion, error) {
	var n model.NoteVersion
	err := row.Scan(&n.ID, &n.CrewID, &n.ScheduleID, &n.Subject, &n.Name, &n.Version, &n.Content, &n.RunID, &n.WrittenBy, &n.CreatedAt)
	return n, err
}

// LatestNote returns the newest version of a note, or ErrNotFound.
func (s *Store) LatestNote(ctx context.Context, key NoteKey) (model.NoteVersion, error) {
	n, err := scanNoteVersion(s.pool.QueryRow(ctx, `SELECT `+noteVersionColumns+` FROM notes WHERE `+noteKeyWhere+
		` ORDER BY version DESC LIMIT 1`, key.CrewID, key.ScheduleID, key.Subject, key.Name))
	return n, mapErr(err, "note", key.Name)
}

// NoteVersion returns one version of a note, or ErrNotFound.
func (s *Store) NoteVersion(ctx context.Context, key NoteKey, version int) (model.NoteVersion, error) {
	n, err := scanNoteVersion(s.pool.QueryRow(ctx, `SELECT `+noteVersionColumns+` FROM notes WHERE `+noteKeyWhere+
		` AND version = $5`, key.CrewID, key.ScheduleID, key.Subject, key.Name, version))
	return n, mapErr(err, "note", key.Name)
}

// NoteVersions returns a note's versions, newest first.
func (s *Store) NoteVersions(ctx context.Context, key NoteKey) ([]model.NoteVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+noteVersionColumns+` FROM notes WHERE `+noteKeyWhere+
		` ORDER BY version DESC LIMIT $5`, key.CrewID, key.ScheduleID, key.Subject, key.Name, maxNoteVersions)
	if err != nil {
		return nil, fmt.Errorf("list note versions: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.NoteVersion, error) { return scanNoteVersion(r) })
	if err != nil {
		return nil, fmt.Errorf("read note versions: %w", err)
	}
	return orEmpty(items), nil
}

// PutNote saves content as the note's next version. When expected is set it must equal the current version
// (0: the note must not exist yet); otherwise the write is refused with a version_conflict ConflictError.
// Writers of the same note take turns (a transaction-scoped advisory lock), so versions never collide. Only
// the newest maxNoteVersions versions are kept.
func (s *Store) PutNote(ctx context.Context, key NoteKey, content string, expected *int, by NoteWriter) (model.NoteVersion, error) {
	var out model.NoteVersion
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || '/' || COALESCE($2::text, '') || '/' || $3 || '/' || $4, 0))`,
			key.CrewID, key.ScheduleID, key.Subject, key.Name); err != nil {
			return fmt.Errorf("lock note: %w", err)
		}
		var current int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version), 0) FROM notes WHERE `+noteKeyWhere,
			key.CrewID, key.ScheduleID, key.Subject, key.Name).Scan(&current); err != nil {
			return fmt.Errorf("read note version: %w", err)
		}
		if expected != nil && *expected != current {
			return &ConflictError{Code: "version_conflict", Message: fmt.Sprintf(
				"The note %q is at version %d, not %d. Read it again, merge your change and save with expected_version %d.",
				key.Name, current, *expected, current)}
		}
		var err error
		out, err = scanNoteVersion(tx.QueryRow(ctx,
			`INSERT INTO notes (crew_id, schedule_id, subject, name, version, content, run_id, written_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+noteVersionColumns,
			key.CrewID, key.ScheduleID, key.Subject, key.Name, current+1, content, by.RunID, by.Name))
		if err != nil {
			return mapErr(err, "note", key.Name)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM notes WHERE `+noteKeyWhere+` AND version <= $5`,
			key.CrewID, key.ScheduleID, key.Subject, key.Name, out.Version-maxNoteVersions); err != nil {
			return fmt.Errorf("prune note versions: %w", err)
		}
		return nil
	})
	return out, err
}

// NoteFilter picks notes of one crew. With AllSchedules false only Scope's schedule (or crew-wide) is listed.
type NoteFilter struct {
	Scope        StateScope
	AllSchedules bool
	// Subject, when set, keeps only notes about that subject
	Subject *string
}

// ListNotes returns the latest version of each note, without content, ordered by subject and name.
func (s *Store) ListNotes(ctx context.Context, f NoteFilter) ([]model.NoteInfo, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (n.schedule_id, n.subject, n.name)
		   n.schedule_id, COALESCE(sc.name, ''), n.subject, n.name, n.version, octet_length(n.content), n.written_by, n.run_id, n.created_at
		 FROM notes n LEFT JOIN schedules sc ON sc.id = n.schedule_id
		 WHERE n.crew_id = $1 AND ($2 OR n.schedule_id IS NOT DISTINCT FROM $3::uuid) AND ($4::text IS NULL OR n.subject = $4)
		 ORDER BY n.schedule_id NULLS FIRST, n.subject, n.name, n.version DESC`,
		f.Scope.CrewID, f.AllSchedules, f.Scope.ScheduleID, f.Subject)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.NoteInfo, error) {
		var n model.NoteInfo
		err := r.Scan(&n.ScheduleID, &n.ScheduleName, &n.Subject, &n.Name, &n.Version, &n.Bytes, &n.WrittenBy, &n.RunID, &n.UpdatedAt)
		return n, err
	})
	if err != nil {
		return nil, fmt.Errorf("read notes: %w", err)
	}
	return orEmpty(items), nil
}

// DeleteNote removes every version of a note. It returns ErrNotFound when there was none.
func (s *Store) DeleteNote(ctx context.Context, key NoteKey) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM notes WHERE `+noteKeyWhere, key.CrewID, key.ScheduleID, key.Subject, key.Name)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ScheduleOfCrew reports whether the schedule exists and runs the crew.
func (s *Store) ScheduleOfCrew(ctx context.Context, scheduleID, crewID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schedules WHERE id = $1 AND crew_id = $2)`, scheduleID, crewID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check schedule: %w", err)
	}
	return ok, nil
}

// RunStateWrites counts the note versions and action log entries a run has written (pruned versions excluded).
func (s *Store) RunStateWrites(ctx context.Context, runID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notes WHERE run_id = $1) + (SELECT count(*) FROM action_log WHERE run_id = $1)`,
		runID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count run state writes: %w", err)
	}
	return n, nil
}
