package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// Memories (docs/crew-state-and-pmo-design.md, section 19). Each row belongs to one crew; scopes are paths relative
// to it. Vectors are real[] scanned per crew in Go (internal/memory), like help_embeddings.

// ErrMemoryNotOwned means a run tried to change a memory another run or a person wrote.
var ErrMemoryNotOwned = errors.New("memory not written by this run")

// ErrMemoryLocked means a run tried to change a memory a person pinned, or approved while the run's saves are
// reviewed: once a person has vouched for a memory, runs cannot change what it says.
var ErrMemoryLocked = errors.New("memory approved or pinned by a person")

// Why a write was skipped (MemorySaved.Skipped).
const (
	// SkipSaveCap: the run already saved its most memories
	SkipSaveCap = "save_cap"
	// SkipCrewFull: the crew holds its most memories
	SkipCrewFull = "crew_full"
	// SkipPendingFull: the crew has its most memories waiting for review
	SkipPendingFull = "pending_full"
	// SkipNotSaved: not saved, with no detail (it would reveal a memory the run cannot see)
	SkipNotSaved = "not_saved"
)

// memoryExists is the conflict when an edit would make a memory say exactly what another in its scope says.
var memoryExists = &ConflictError{Code: "memory_exists", Message: "Another memory in this scope already says the same thing."}

const memoryColumns = `m.id, m.content, m.scope, m.categories, m.metadata, m.importance, m.source, m.pinned, m.status, m.run_id,
	m.agent_role, m.task_name, m.written_by, m.created_at, m.last_accessed, m.updated_at, m.km2_document_id, m.embed_model`

// maxMemoryScan bounds the rows read for one search; a crew holds at most model.MaxMemoriesPerCrew (plus a batch)
const maxMemoryScan = model.MaxMemoriesPerCrew + 100

func scanMemory(row pgx.Row, withVector bool) (model.MemoryRecord, error) {
	var m model.MemoryRecord
	var importance float32
	dest := []any{&m.ID, &m.Content, &m.Scope, &m.Categories, &m.Metadata, &importance, &m.Source, &m.Pinned, &m.Status, &m.RunID,
		&m.AgentRole, &m.TaskName, &m.WrittenBy, &m.CreatedAt, &m.LastAccessed, &m.UpdatedAt, &m.KM2DocumentID, &m.EmbedModel}
	if withVector {
		dest = append(dest, &m.Vector)
	}
	if err := row.Scan(dest...); err != nil {
		return m, err
	}
	m.Importance = round3(importance)
	m.Categories = orEmpty(m.Categories)
	if m.Metadata == nil {
		m.Metadata = map[string]any{}
	}
	return m, nil
}

// MemoryFilter picks memories of one crew. Zero fields do not filter.
type MemoryFilter struct {
	CrewID string
	// VisibleToRun limits to what that run may read: active memories, and the ones it wrote itself
	VisibleToRun string
	// WrittenByRun limits to the memories that run wrote
	WrittenByRun string
	EmbedModel   string
	// ScopePrefix matches the scope and everything below it, by whole parts ("" and "/" are everything)
	ScopePrefix string
	// Categories matches memories with any of them
	Categories []string
	// Metadata matches memories whose metadata contains these fields
	Metadata map[string]any
	Status   string
	Pinned   *bool
	RunID    string
	// Contains matches content anywhere, ignoring case
	Contains  string
	IDs       []string
	OlderThan *time.Time
	Unpinned  bool
	// OtherEmbedModel matches memories embedded with another model than this one (waiting to be re-embedded)
	OtherEmbedModel string
}

func (f MemoryFilter) where() (string, []any, error) {
	args := []any{f.CrewID}
	conds := []string{"m.crew_id = $1"}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.VisibleToRun != "" {
		conds = append(conds, "(m.status = 'active' OR m.run_id = "+arg(f.VisibleToRun)+")")
	}
	if f.WrittenByRun != "" {
		conds = append(conds, "m.run_id = "+arg(f.WrittenByRun))
	}
	if f.EmbedModel != "" {
		conds = append(conds, "m.embed_model = "+arg(f.EmbedModel))
	}
	if f.OtherEmbedModel != "" {
		conds = append(conds, "m.embed_model <> "+arg(f.OtherEmbedModel))
	}
	if f.ScopePrefix != "" && f.ScopePrefix != "/" {
		conds = append(conds, fmt.Sprintf(`(m.scope = %s OR m.scope LIKE %s ESCAPE '\')`, arg(f.ScopePrefix), arg(escapeLike(f.ScopePrefix)+"/%")))
	}
	if len(f.Categories) > 0 {
		conds = append(conds, "m.categories && "+arg(f.Categories)+"::text[]")
	}
	if len(f.Metadata) > 0 {
		raw, err := json.Marshal(f.Metadata)
		if err != nil {
			return "", nil, fmt.Errorf("encode metadata filter: %w", err)
		}
		conds = append(conds, "m.metadata @> "+arg(string(raw))+"::jsonb")
	}
	if f.Status != "" {
		conds = append(conds, "m.status = "+arg(f.Status))
	}
	if f.Pinned != nil {
		conds = append(conds, "m.pinned = "+arg(*f.Pinned))
	}
	if f.Unpinned {
		conds = append(conds, "NOT m.pinned")
	}
	if f.RunID != "" {
		conds = append(conds, "m.run_id = "+arg(f.RunID))
	}
	if f.Contains != "" {
		conds = append(conds, `m.content ILIKE '%' || `+arg(escapeLike(f.Contains))+` || '%' ESCAPE '\'`)
	}
	if f.IDs != nil {
		conds = append(conds, "m.id = ANY("+arg(f.IDs)+"::uuid[])")
	}
	if f.OlderThan != nil {
		conds = append(conds, "m.created_at < "+arg(*f.OlderThan))
	}
	return " WHERE " + strings.Join(conds, " AND "), args, nil
}

func (s *Store) queryMemories(ctx context.Context, f MemoryFilter, withVectors bool, tail string, extra ...any) ([]model.MemoryRecord, error) {
	where, args, err := f.where()
	if err != nil {
		return nil, err
	}
	cols := memoryColumns
	if withVectors {
		cols += ", m.vector"
	}
	for _, v := range extra {
		args = append(args, v)
		tail = strings.Replace(tail, "?", fmt.Sprintf("$%d", len(args)), 1)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM memories m`+where+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.MemoryRecord, error) { return scanMemory(r, withVectors) })
	if err != nil {
		return nil, fmt.Errorf("read memories: %w", err)
	}
	return orEmpty(items), nil
}

// MemoryRows returns every memory matching f with its vector, newest first, for ranking in Go.
func (s *Store) MemoryRows(ctx context.Context, f MemoryFilter) ([]model.MemoryRecord, error) {
	return s.queryMemories(ctx, f, true, " ORDER BY m.created_at DESC, m.id LIMIT ?", maxMemoryScan)
}

// ListMemories returns one page of memories matching f, newest first, and how many match in all.
func (s *Store) ListMemories(ctx context.Context, f MemoryFilter, limit, offset int) ([]model.MemoryRecord, int, error) {
	total, err := s.CountMemories(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.queryMemories(ctx, f, false, " ORDER BY m.created_at DESC, m.id LIMIT ? OFFSET ?", limit, offset)
	return items, total, err
}

// GetMemory reads one of a crew's memories.
func (s *Store) GetMemory(ctx context.Context, crewID, id string) (model.MemoryRecord, error) {
	m, err := scanMemory(s.pool.QueryRow(ctx, `SELECT `+memoryColumns+` FROM memories m WHERE m.crew_id = $1 AND m.id = $2`, crewID, id), false)
	return m, mapErr(err, "memory", "")
}

// CountMemories counts the memories matching f.
func (s *Store) CountMemories(ctx context.Context, f MemoryFilter) (int, error) {
	where, args, err := f.where()
	if err != nil {
		return 0, err
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memories m`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count memories: %w", err)
	}
	return n, nil
}

// MemoryScopes lists the scopes holding memories matching f, with how many each holds.
func (s *Store) MemoryScopes(ctx context.Context, f MemoryFilter) ([]model.MemoryScopeCount, error) {
	where, args, err := f.where()
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT m.scope, count(*) FROM memories m`+where+` GROUP BY m.scope ORDER BY m.scope`, args...)
	if err != nil {
		return nil, fmt.Errorf("list memory scopes: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.MemoryScopeCount, error) {
		var c model.MemoryScopeCount
		err := r.Scan(&c.Scope, &c.Count)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("read memory scopes: %w", err)
	}
	return orEmpty(items), nil
}

// MemoryCategories counts the categories of the memories matching f.
func (s *Store) MemoryCategories(ctx context.Context, f MemoryFilter) (map[string]int, error) {
	where, args, err := f.where()
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT c, count(*) FROM memories m CROSS JOIN LATERAL unnest(m.categories) AS c`+where+` GROUP BY c`, args...)
	if err != nil {
		return nil, fmt.Errorf("list memory categories: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, fmt.Errorf("read memory categories: %w", err)
		}
		out[name] = n
	}
	return out, rows.Err()
}

// MemoryAge returns the oldest and newest creation times of the memories matching f (nil when there are none).
func (s *Store) MemoryAge(ctx context.Context, f MemoryFilter) (*time.Time, *time.Time, error) {
	where, args, err := f.where()
	if err != nil {
		return nil, nil, err
	}
	var oldest, newest *time.Time
	if err := s.pool.QueryRow(ctx, `SELECT min(m.created_at), max(m.created_at) FROM memories m`+where, args...).Scan(&oldest, &newest); err != nil {
		return nil, nil, fmt.Errorf("read memory dates: %w", err)
	}
	return oldest, newest, nil
}

// MemoryWrite is one memory to save, already checked and embedded.
type MemoryWrite struct {
	// ID is the caller's UUID, kept when free; "" lets the database pick one
	ID          string
	CrewID      string
	Scope       string
	Content     string
	ContentHash string
	Categories  []string
	Metadata    map[string]any
	Importance  float64
	Status      string
	Pinned      bool
	AgentRole   string
	TaskName    string
	Source      string
	WrittenBy   string
	RunID       *string
	EmbedModel  string
	Vector      []float32
	// DuplicateOf is a near-duplicate the caller found; it is touched instead of inserting this one
	DuplicateOf string
}

// MemorySaved is the outcome of one write: saved (a new row, or an existing one it duplicates), or skipped (no id
// and no status).
type MemorySaved struct {
	ID        string `json:"id"`
	Duplicate bool   `json:"duplicate"`
	Status    string `json:"status"`
	// Skipped says why the write was not saved (Skip*); "" when it was
	Skipped string `json:"skipped,omitempty"`
}

// MemoryCaps bound saves: per run (when RunID is set), per crew, and pending ones per crew (when PendingPerCrew is
// set). With a RunID, duplicates are looked for only among the memories that run may see.
type MemoryCaps struct {
	RunID          string
	PerRun         int
	PerCrew        int
	PendingPerCrew int
}

// MemorySaveResult is what SaveMemories did. Saved has one entry per write, in order.
type MemorySaveResult struct {
	Saved []MemorySaved
	// Skipped counts the entries of Saved that were skipped
	Skipped int
	// Remaining is how many more memories the run may save (0 for people)
	Remaining int
	// CrewFull is true when a write was skipped because the crew holds its most memories
	CrewFull bool
	// PendingFull is true when a write was skipped because the crew has its most memories waiting for review
	PendingFull bool
	// Inserted are the ids of new rows
	Inserted []string
}

// saveCounts is what SaveMemories counts while it goes: the run's saves before it started, and the crew's rows.
type saveCounts struct {
	used, rows, pending int
}

// SaveMemories saves writes for one crew under a short per-crew lock: an exact duplicate (same scope and content
// hash) or a near-duplicate is touched instead of inserted; new rows past the run's cap, the crew's cap or the
// crew's pending cap are skipped. For a run, only memories it may see count as duplicates; an exact duplicate it
// cannot see is skipped as SkipNotSaved. The run's saves are counted in run_memory_usage, so deleting rows does not
// give a run more.
func (s *Store) SaveMemories(ctx context.Context, crewID string, writes []MemoryWrite, caps MemoryCaps) (MemorySaveResult, error) {
	var out MemorySaveResult
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('memories:' || $1, 0))`, crewID); err != nil {
			return fmt.Errorf("lock memories: %w", err)
		}
		var n saveCounts
		if caps.RunID != "" {
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT saves FROM run_memory_usage WHERE run_id = $1), 0)`, caps.RunID).Scan(&n.used); err != nil {
				return fmt.Errorf("read memory saves: %w", err)
			}
		}
		if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status = 'pending') FROM memories WHERE crew_id = $1`, crewID).
			Scan(&n.rows, &n.pending); err != nil {
			return fmt.Errorf("count memories: %w", err)
		}
		for _, w := range writes {
			saved, err := saveMemory(ctx, tx, crewID, w, caps, &n, &out)
			if err != nil {
				return err
			}
			if saved.Skipped != "" {
				out.Skipped++
			}
			out.Saved = append(out.Saved, saved)
		}
		added := len(out.Inserted)
		if caps.RunID != "" {
			out.Remaining = max(caps.PerRun-n.used-added, 0)
			if added > 0 {
				if _, err := tx.Exec(ctx, `INSERT INTO run_memory_usage (run_id, saves) VALUES ($1, $2)
					ON CONFLICT (run_id) DO UPDATE SET saves = run_memory_usage.saves + EXCLUDED.saves`, caps.RunID, added); err != nil {
					return fmt.Errorf("count memory saves: %w", err)
				}
			}
		}
		return nil
	})
	return out, err
}

// saveMemory handles one write: touches a duplicate, skips it (MemorySaved.Skipped), or inserts it.
func saveMemory(ctx context.Context, tx pgx.Tx, crewID string, w MemoryWrite, caps MemoryCaps, n *saveCounts,
	out *MemorySaveResult) (MemorySaved, error) {
	// A run's duplicates are only the memories it may see: active ones and its own
	visible, args := "", []any{crewID}
	if caps.RunID != "" {
		visible, args = ` AND (status = 'active' OR run_id = $2)`, append(args, caps.RunID)
	}
	at := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if w.DuplicateOf != "" {
		if saved, ok, err := touchMemory(ctx, tx, `id = `+at(w.DuplicateOf)+visible, args...); err != nil || ok {
			return saved, err
		}
		args = args[:len(args)-1]
	}
	if saved, ok, err := touchMemory(ctx, tx, `scope = `+at(w.Scope)+` AND content_hash = `+at(w.ContentHash)+visible, args...); err != nil || ok {
		return saved, err
	}
	if caps.RunID != "" {
		var hidden bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM memories WHERE crew_id = $1 AND scope = $2 AND content_hash = $3)`,
			crewID, w.Scope, w.ContentHash).Scan(&hidden); err != nil {
			return MemorySaved{}, fmt.Errorf("find memory: %w", err)
		}
		if hidden {
			return MemorySaved{Skipped: SkipNotSaved}, nil
		}
	}
	switch {
	case caps.RunID != "" && n.used+len(out.Inserted) >= caps.PerRun:
		return MemorySaved{Skipped: SkipSaveCap}, nil
	case n.rows >= caps.PerCrew:
		out.CrewFull = true
		return MemorySaved{Skipped: SkipCrewFull}, nil
	case w.Status == model.MemoryPending && caps.PendingPerCrew > 0 && n.pending >= caps.PendingPerCrew:
		out.PendingFull = true
		return MemorySaved{Skipped: SkipPendingFull}, nil
	}
	id, err := insertMemory(ctx, tx, crewID, w)
	if err != nil {
		return MemorySaved{}, err
	}
	n.rows++
	if w.Status == model.MemoryPending {
		n.pending++
	}
	out.Inserted = append(out.Inserted, id)
	return MemorySaved{ID: id, Status: w.Status}, nil
}

func touchMemory(ctx context.Context, tx pgx.Tx, cond string, args ...any) (MemorySaved, bool, error) {
	var saved MemorySaved
	err := tx.QueryRow(ctx, `UPDATE memories SET last_accessed = now() WHERE crew_id = $1 AND `+cond+` RETURNING id, status`, args...).
		Scan(&saved.ID, &saved.Status)
	if isNoRows(err) {
		return saved, false, nil
	}
	if err != nil {
		return saved, false, fmt.Errorf("touch memory: %w", err)
	}
	saved.Duplicate = true
	return saved, true, nil
}

// insertMemory inserts a new row, keeping the caller's id unless another memory already has it.
func insertMemory(ctx context.Context, tx pgx.Tx, crewID string, w MemoryWrite) (string, error) {
	meta, err := json.Marshal(orEmptyAnyMap(w.Metadata))
	if err != nil {
		return "", fmt.Errorf("encode memory metadata: %w", err)
	}
	ids := []any{nullableUUID(w.ID)}
	if w.ID != "" {
		ids = append(ids, nil)
	}
	for _, id := range ids {
		var out string
		err := tx.QueryRow(ctx, `INSERT INTO memories (id, crew_id, scope, content, content_hash, categories, importance, metadata, source,
			  status, pinned, agent_role, task_name, embed_model, vector, run_id, written_by)
			VALUES (COALESCE($1::uuid, gen_random_uuid()), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
			ON CONFLICT (id) DO NOTHING RETURNING id`,
			id, crewID, w.Scope, w.Content, w.ContentHash, orEmpty(w.Categories), w.Importance, meta, w.Source, w.Status, w.Pinned,
			w.AgentRole, w.TaskName, w.EmbedModel, w.Vector, w.RunID, w.WrittenBy).Scan(&out)
		if isNoRows(err) {
			continue
		}
		if err != nil {
			return "", mapErr(err, "memory", "")
		}
		return out, nil
	}
	return "", fmt.Errorf("insert memory: no id")
}

func orEmptyAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// MemoryUpdate changes some fields of a memory; nil fields stay as they are. A new Content needs its hash, vector
// and embed model.
type MemoryUpdate struct {
	Content     *string
	ContentHash string
	Vector      []float32
	EmbedModel  string
	Scope       *string
	Categories  *[]string
	Metadata    map[string]any
	Importance  *float64
	Pinned      *bool
	Status      *string
	// OnlyRunID refuses the change (ErrMemoryNotOwned) unless that run wrote the memory, and (ErrMemoryLocked) when a
	// person pinned it
	OnlyRunID string
	// RunReview (with OnlyRunID) says the run's saves are reviewed: an active memory was approved by a person, so the
	// change is refused (ErrMemoryLocked)
	RunReview bool
}

// UpdateMemory changes one of a crew's memories and returns it.
func (s *Store) UpdateMemory(ctx context.Context, crewID, id string, u MemoryUpdate) (model.MemoryRecord, error) {
	var out model.MemoryRecord
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var runID *string
		var pinned bool
		var status string
		err := tx.QueryRow(ctx, `SELECT run_id, pinned, status FROM memories WHERE crew_id = $1 AND id = $2 FOR UPDATE`, crewID, id).
			Scan(&runID, &pinned, &status)
		if err != nil {
			return mapErr(err, "memory", "")
		}
		if u.OnlyRunID != "" {
			switch {
			case runID == nil || *runID != u.OnlyRunID:
				return ErrMemoryNotOwned
			case pinned || (u.RunReview && status == model.MemoryActive):
				return ErrMemoryLocked
			}
		}
		var content, hash, embedModel, meta any
		var vector any
		if u.Content != nil {
			content, hash, embedModel, vector = *u.Content, u.ContentHash, u.EmbedModel, u.Vector
		}
		if u.Metadata != nil {
			raw, err := json.Marshal(u.Metadata)
			if err != nil {
				return fmt.Errorf("encode memory metadata: %w", err)
			}
			meta = string(raw)
		}
		var categories any
		if u.Categories != nil {
			categories = orEmpty(*u.Categories)
		}
		_, err = tx.Exec(ctx, `UPDATE memories SET content = COALESCE($3, content), content_hash = COALESCE($4, content_hash),
			embed_model = COALESCE($5, embed_model), vector = COALESCE($6::real[], vector), scope = COALESCE($7, scope),
			categories = COALESCE($8::text[], categories), metadata = COALESCE($9::jsonb, metadata),
			importance = COALESCE($10, importance), pinned = COALESCE($11, pinned), status = COALESCE($12, status), updated_at = now()
			WHERE crew_id = $1 AND id = $2`,
			crewID, id, content, hash, embedModel, vector, u.Scope, categories, meta, u.Importance, u.Pinned, u.Status)
		if isUniqueViolation(err) {
			return memoryExists
		}
		if err != nil {
			return fmt.Errorf("update memory: %w", err)
		}
		out, err = scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM memories m WHERE m.id = $1`, id), false)
		return mapErr(err, "memory", "")
	})
	return out, err
}

// DeleteMemory deletes one of a crew's memories and returns what it was.
func (s *Store) DeleteMemory(ctx context.Context, crewID, id string) (model.MemoryRecord, error) {
	m, err := scanMemory(s.pool.QueryRow(ctx, `DELETE FROM memories m WHERE m.crew_id = $1 AND m.id = $2 RETURNING `+memoryColumns, crewID, id), false)
	return m, mapErr(err, "memory", "")
}

// DeleteMemories deletes the memories matching f (or, with dryRun, only counts them) and returns how many matched.
func (s *Store) DeleteMemories(ctx context.Context, f MemoryFilter, dryRun bool) (int, error) {
	if dryRun {
		return s.CountMemories(ctx, f)
	}
	where, args, err := f.where()
	if err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM memories m`+where, args...)
	if err != nil {
		return 0, fmt.Errorf("delete memories: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// TouchMemories marks the memories matching f as just used.
func (s *Store) TouchMemories(ctx context.Context, f MemoryFilter) (int, error) {
	where, args, err := f.where()
	if err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE memories m SET last_accessed = now()`+where, args...)
	if err != nil {
		return 0, fmt.Errorf("touch memories: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// AddMemoryEmbeds counts n more embedded texts against a run's limit. ok is false (and nothing is counted) when
// they would take the run past limit.
func (s *Store) AddMemoryEmbeds(ctx context.Context, runID string, n, limit int) (bool, error) {
	if n <= 0 {
		return true, nil
	}
	if n > limit {
		return false, nil
	}
	var total int
	err := s.pool.QueryRow(ctx, `INSERT INTO run_memory_usage (run_id, embed_texts) VALUES ($1, $2)
		ON CONFLICT (run_id) DO UPDATE SET embed_texts = run_memory_usage.embed_texts + EXCLUDED.embed_texts
		WHERE run_memory_usage.embed_texts + EXCLUDED.embed_texts <= $3
		RETURNING embed_texts`, runID, n, limit).Scan(&total)
	if isNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("count memory embeddings: %w", err)
	}
	return true, nil
}

// RefundMemoryEmbeds gives back n texts counted by AddMemoryEmbeds that the provider never embedded (it failed or
// was busy).
func (s *Store) RefundMemoryEmbeds(ctx context.Context, runID string, n int) error {
	if n <= 0 {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE run_memory_usage SET embed_texts = GREATEST(embed_texts - $2, 0) WHERE run_id = $1`, runID, n); err != nil {
		return fmt.Errorf("refund memory embeddings: %w", err)
	}
	return nil
}

// RunMemorySaves is how many memories a run has saved.
func (s *Store) RunMemorySaves(ctx context.Context, runID string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT saves FROM run_memory_usage WHERE run_id = $1), 0)`, runID).Scan(&n); err != nil {
		return 0, fmt.Errorf("read memory saves: %w", err)
	}
	return n, nil
}

// PruneMemories deletes unpinned memories past their crew's retention (model.DefaultMemoryRetention days when the
// crew has no settings; never when its retention is null) and pending memories nobody approved within
// model.PendingMemoryExpiryDays. It returns how many it deleted.
func (s *Store) PruneMemories(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM memories m WHERE NOT m.pinned AND (
		  (m.status = 'pending' AND m.created_at < now() - make_interval(days => $1))
		  OR (CASE WHEN EXISTS (SELECT 1 FROM crew_memory cm WHERE cm.crew_id = m.crew_id)
		        THEN m.created_at < now() - make_interval(days => (SELECT cm.retention_days FROM crew_memory cm WHERE cm.crew_id = m.crew_id))
		        ELSE m.created_at < now() - make_interval(days => $2) END))`,
		model.PendingMemoryExpiryDays, model.DefaultMemoryRetention)
	if err != nil {
		return 0, fmt.Errorf("prune memories: %w", err)
	}
	return tag.RowsAffected(), nil
}

// MemoriesToReembed returns up to limit memories embedded with another model than embedModel, oldest first.
func (s *Store) MemoriesToReembed(ctx context.Context, embedModel string, limit int) ([]model.MemoryRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+memoryColumns+` FROM memories m WHERE m.embed_model <> $1 ORDER BY m.created_at LIMIT $2`, embedModel, limit)
	if err != nil {
		return nil, fmt.Errorf("list memories to re-embed: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.MemoryRecord, error) { return scanMemory(r, false) })
	if err != nil {
		return nil, fmt.Errorf("read memories to re-embed: %w", err)
	}
	return orEmpty(items), nil
}

// SetMemoryVector stores a memory's new vector, unless its content changed since it was read.
func (s *Store) SetMemoryVector(ctx context.Context, id, content, embedModel string, vector []float32) error {
	if _, err := s.pool.Exec(ctx, `UPDATE memories SET vector = $3, embed_model = $4 WHERE id = $1 AND content = $2`, id, content, vector, embedModel); err != nil {
		return fmt.Errorf("store memory vector: %w", err)
	}
	return nil
}

// SetMemoriesKM2Document records the KM2 document some of a crew's memories were promoted to.
func (s *Store) SetMemoriesKM2Document(ctx context.Context, crewID string, ids []string, documentID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE memories SET km2_document_id = $3, updated_at = now() WHERE crew_id = $1 AND id = ANY($2::uuid[])`,
		crewID, ids, documentID); err != nil {
		return fmt.Errorf("record KM2 document of memories: %w", err)
	}
	return nil
}
