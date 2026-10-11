package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const workOrderSelect = `SELECT w.id, w.number, w.title, w.brief, w.crew_id, w.target_name,
	w.inputs, w.budget_usd, w.area_ids::text[], w.source, w.source_id, w.created_by, w.created_by_name,
	COALESCE(r.status, 'draft'), w.current_run_id, r.cost_usd,
	(SELECT count(*) FROM runs x WHERE x.work_order_id = w.id), w.submitted_at, r.finished_at, w.created_at, w.updated_at
	FROM work_orders w LEFT JOIN runs r ON r.id = w.current_run_id`

func scanWorkOrder(row pgx.Row) (model.WorkOrder, error) {
	var w model.WorkOrder
	err := row.Scan(&w.ID, &w.Number, &w.Title, &w.Brief, &w.CrewID, &w.TargetName,
		&w.Inputs, &w.BudgetUSD, &w.AreaIDs, &w.Source, &w.SourceID, &w.CreatedBy, &w.CreatedByName,
		&w.Status, &w.CurrentRunID, &w.CostUSD, &w.RunCount, &w.SubmittedAt, &w.FinishedAt, &w.CreatedAt, &w.UpdatedAt)
	if w.Inputs == nil {
		w.Inputs = map[string]string{}
	}
	w.AreaIDs = orEmpty(w.AreaIDs)
	return w, err
}

// WorkOrderMeta is set by the server, not the caller.
type WorkOrderMeta struct {
	TargetName    string
	AreaIDs       []string
	Source        string
	SourceID      string
	CreatedBy     string
	CreatedByName string
}

func (s *Store) CreateWorkOrder(ctx context.Context, in model.WorkOrderInput, meta WorkOrderMeta) (model.WorkOrder, error) {
	inputs, err := json.Marshal(orEmptyMap(in.Inputs))
	if err != nil {
		return model.WorkOrder{}, fmt.Errorf("encode inputs: %w", err)
	}
	if meta.Source == "" {
		meta.Source = model.SourceManual
	}
	var out model.WorkOrder
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx,
			`INSERT INTO work_orders (title, brief, crew_id, target_name, inputs, budget_usd, area_ids,
			 source, source_id, created_by, created_by_name) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			 RETURNING id`,
			in.Title, in.Brief, in.CrewID, meta.TargetName, inputs, in.BudgetUSD,
			orEmpty(meta.AreaIDs), meta.Source, nullableUUID(meta.SourceID), nullableUUID(meta.CreatedBy), meta.CreatedByName).
			Scan(&id); err != nil {
			return mapErr(err, "work order", in.Title)
		}
		var err error
		if out, err = getWorkOrder(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "work_order", id, "create", out, out.AreaIDs)
	})
	return out, err
}

// ErrWorkOrderActive stops changes while a work order is queued or running.
var ErrWorkOrderActive = &ConflictError{Message: "This work order is queued or running. Cancel it first."}

func (s *Store) UpdateWorkOrder(ctx context.Context, id string, in model.WorkOrderInput, meta WorkOrderMeta) (model.WorkOrder, error) {
	inputs, err := json.Marshal(orEmptyMap(in.Inputs))
	if err != nil {
		return model.WorkOrder{}, fmt.Errorf("encode inputs: %w", err)
	}
	var out model.WorkOrder
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if err := lockIdleWorkOrder(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE work_orders SET title = $2, brief = $3, crew_id = $4, target_name = $5,
			 inputs = $6, budget_usd = $7, area_ids = $8, updated_at = now() WHERE id = $1`,
			id, in.Title, in.Brief, in.CrewID, meta.TargetName, inputs, in.BudgetUSD,
			orEmpty(meta.AreaIDs)); err != nil {
			return mapErr(err, "work order", in.Title)
		}
		if out, err = getWorkOrder(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "work_order", id, "update", out, out.AreaIDs)
	})
	return out, err
}

// lockIdleWorkOrder locks a work order row for the rest of the transaction and fails if its current run is
// queued or running. Submitting and editing both take the lock, so they cannot interleave.
func lockIdleWorkOrder(ctx context.Context, tx pgx.Tx, id string) error {
	// Lock first, then read the run in a separate statement: under READ COMMITTED a statement that waited for the
	// lock keeps its old snapshot of joined rows, so a run started while we waited would look idle.
	var runID *string
	err := tx.QueryRow(ctx, `SELECT current_run_id FROM work_orders WHERE id = $1 FOR UPDATE`, id).Scan(&runID)
	if err != nil {
		return mapErr(err, "work order", "")
	}
	if runID == nil {
		return nil
	}
	var status *string
	if err := tx.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, *runID).Scan(&status); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read work order run: %w", err)
	}
	if status != nil && isActive(*status) {
		return ErrWorkOrderActive
	}
	return nil
}

// EnsureWorkOrderIdle fails with ErrWorkOrderActive while the work order is queued or running.
func (s *Store) EnsureWorkOrderIdle(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error { return lockIdleWorkOrder(ctx, tx, id) })
}

func isActive(status string) bool {
	return status == string(model.RunQueued) || status == string(model.RunRunning)
}

func getWorkOrder(ctx context.Context, q queryer, id string) (model.WorkOrder, error) {
	w, err := scanWorkOrder(q.QueryRow(ctx, workOrderSelect+` WHERE w.id = $1`, id))
	return w, mapErr(err, "work order", "")
}

// GetWorkOrder returns a work order regardless of scope; callers check area access.
func (s *Store) GetWorkOrder(ctx context.Context, id string) (model.WorkOrder, error) {
	return getWorkOrder(ctx, s.pool, id)
}

// GetWorkOrderDetail adds the input files and every run, newest first.
func (s *Store) GetWorkOrderDetail(ctx context.Context, id string) (model.WorkOrder, error) {
	w, err := s.GetWorkOrder(ctx, id)
	if err != nil {
		return w, err
	}
	attachments, err := s.ListAttachments(ctx, AttachmentFilter{WorkOrderID: id, Kind: model.AttachmentInput})
	if err != nil {
		return w, err
	}
	w.Attachments = &attachments
	rows, err := s.pool.Query(ctx, `SELECT `+runColumns+` FROM runs WHERE work_order_id = $1 ORDER BY created_at DESC`, id)
	if err != nil {
		return w, fmt.Errorf("list work order runs: %w", err)
	}
	runs, err := collectRuns(rows)
	if err != nil {
		return w, err
	}
	w.Runs = &runs
	return w, nil
}

type WorkOrderFilter struct {
	Scope    Scope
	Status   string
	Source   string
	SourceID string
	CrewID   string
	// Query matches the title or the number
	Query  string
	Limit  int
	Offset int
}

func (s *Store) ListWorkOrders(ctx context.Context, f WorkOrderFilter) ([]model.WorkOrder, int, error) {
	all, areas := f.Scope.areaArgs()
	var number *int64
	q := strings.TrimSpace(f.Query)
	var n int64
	if _, err := fmt.Sscanf(strings.TrimPrefix(strings.ToUpper(q), "WO-"), "%d", &n); err == nil {
		number = &n
	}
	const where = ` WHERE ($1::boolean OR w.area_ids && $2::uuid[])
		AND ($3::text IS NULL OR COALESCE(r.status, 'draft') = $3) AND ($4::text IS NULL OR w.source = $4)
		AND ($5::uuid IS NULL OR w.source_id = $5) AND ($6::uuid IS NULL OR w.crew_id = $6)
		AND ($7::text IS NULL OR w.title ILIKE '%' || $7 || '%' OR w.number = $8)`
	args := []any{all, areas, nullableText(f.Status), nullableText(f.Source), nullableUUID(f.SourceID),
		nullableUUID(f.CrewID), nullableText(escapeLike(q)), number}
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM work_orders w LEFT JOIN runs r ON r.id = w.current_run_id`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count work orders: %w", err)
	}
	rows, err := s.pool.Query(ctx, workOrderSelect+where+` ORDER BY w.created_at DESC, w.id DESC LIMIT $9 OFFSET $10`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list work orders: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.WorkOrder, error) { return scanWorkOrder(r) })
	if err != nil {
		return nil, 0, fmt.Errorf("read work orders: %w", err)
	}
	return orEmpty(items), total, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// DeleteWorkOrder removes a work order that is not running, with its runs and files.
func (s *Store) DeleteWorkOrder(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := getWorkOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		if isActive(old.Status) {
			return ErrWorkOrderActive
		}
		// Runs reference the work order with ON DELETE CASCADE; their events, outputs and files go too
		if _, err := tx.Exec(ctx, `DELETE FROM work_orders WHERE id = $1`, id); err != nil {
			return mapErr(err, "work order", "")
		}
		return recordAreaRevision(ctx, tx, "work_order", id, "delete", old, old.AreaIDs)
	})
}

// SourceHasActiveWorkOrder reports whether a schedule or trigger has a work order still queued or running.
func (s *Store) SourceHasActiveWorkOrder(ctx context.Context, source, sourceID string) (bool, error) {
	var active bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM work_orders w JOIN runs r ON r.id = w.current_run_id
		 WHERE w.source = $1 AND w.source_id = $2 AND r.status IN ('queued', 'running'))`, source, sourceID).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("check active work orders: %w", err)
	}
	return active, nil
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// Attachments

const attachmentColumns = `id, work_order_id, run_id, message_id, kind, filename, content_type, size_bytes, sha256, storage_key,
	created_by_name, created_at`

func scanAttachment(row pgx.Row) (model.Attachment, error) {
	var a model.Attachment
	err := row.Scan(&a.ID, &a.WorkOrderID, &a.RunID, &a.MessageID, &a.Kind, &a.Filename, &a.ContentType, &a.SizeBytes, &a.SHA256,
		&a.StorageKey, &a.CreatedByName, &a.CreatedAt)
	return a, err
}

// AddAttachment records a file already written to object storage.
func (s *Store) AddAttachment(ctx context.Context, a model.Attachment) (model.Attachment, error) {
	actor := actorFrom(ctx)
	out, err := scanAttachment(s.pool.QueryRow(ctx,
		`INSERT INTO attachments (id, work_order_id, run_id, kind, filename, content_type, size_bytes, sha256, storage_key,
		 created_by, created_by_name) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING `+attachmentColumns,
		a.ID, a.WorkOrderID, a.RunID, a.Kind, a.Filename, a.ContentType, a.SizeBytes, a.SHA256, a.StorageKey,
		nullableUUID(actor.ID), actor.Name))
	if err != nil {
		return out, mapErr(err, "attachment", a.Filename)
	}
	return out, nil
}

type AttachmentFilter struct {
	WorkOrderID string
	RunID       string
	Kind        string
}

func (s *Store) ListAttachments(ctx context.Context, f AttachmentFilter) ([]model.Attachment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+attachmentColumns+` FROM attachments WHERE ($1::uuid IS NULL OR work_order_id = $1)
		 AND ($2::uuid IS NULL OR run_id = $2) AND ($3::text IS NULL OR kind = $3) ORDER BY created_at, filename`,
		nullableUUID(f.WorkOrderID), nullableUUID(f.RunID), nullableText(f.Kind))
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Attachment, error) { return scanAttachment(r) })
	if err != nil {
		return nil, fmt.Errorf("read attachments: %w", err)
	}
	return orEmpty(items), nil
}

func (s *Store) GetAttachment(ctx context.Context, id string) (model.Attachment, error) {
	a, err := scanAttachment(s.pool.QueryRow(ctx, `SELECT `+attachmentColumns+` FROM attachments WHERE id = $1`, id))
	return a, mapErr(err, "attachment", "")
}

// DeleteAttachment removes a row; a database trigger queues its object for deletion.
func (s *Store) DeleteAttachment(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// InputAttachmentTotals returns how many input files a work order has and their total size.
func (s *Store) InputAttachmentTotals(ctx context.Context, workOrderID string) (count int, bytes int64, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT count(*), COALESCE(sum(size_bytes), 0) FROM attachments WHERE work_order_id = $1 AND kind = 'input'`,
		workOrderID).Scan(&count, &bytes)
	if err != nil {
		return 0, 0, fmt.Errorf("count attachments: %w", err)
	}
	return count, bytes, nil
}

// PendingStorageDeletions returns object keys whose rows are gone.
func (s *Store) PendingStorageDeletions(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT storage_key FROM storage_deletions WHERE attempts < 10 ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list storage deletions: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read storage deletions: %w", err)
	}
	return keys, nil
}

// FinishStorageDeletion clears a key after its object is deleted, or records the failure.
func (s *Store) FinishStorageDeletion(ctx context.Context, key string, deleteErr error) error {
	var err error
	if deleteErr == nil {
		_, err = s.pool.Exec(ctx, `DELETE FROM storage_deletions WHERE storage_key = $1`, key)
	} else {
		_, err = s.pool.Exec(ctx, `UPDATE storage_deletions SET attempts = attempts + 1, last_error = $2 WHERE storage_key = $1`,
			key, deleteErr.Error())
	}
	if err != nil {
		return fmt.Errorf("update storage deletion: %w", err)
	}
	return nil
}

// ActiveWorkOrders counts a schedule's or trigger's work orders that are still queued or running.
func (s *Store) ActiveWorkOrders(ctx context.Context, source, sourceID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM work_orders w JOIN runs r ON r.id = w.current_run_id
		 WHERE w.source = $1 AND w.source_id = $2 AND r.status IN ('queued', 'running')`, source, sourceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count active work orders: %w", err)
	}
	return n, nil
}

// ErrInputQuota means another file would exceed the work order's limits.
var ErrInputQuota = errors.New("work order file limit reached")

// AddInputAttachment records an input file only while the work order is idle and within its limits. The
// work order row is locked, so parallel uploads cannot overshoot the limits together.
func (s *Store) AddInputAttachment(ctx context.Context, a model.Attachment, maxFiles int, maxBytes int64) (model.Attachment, error) {
	actor := actorFrom(ctx)
	var out model.Attachment
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := lockIdleWorkOrder(ctx, tx, *a.WorkOrderID); err != nil {
			return err
		}
		var count int
		var total int64
		if err := tx.QueryRow(ctx,
			`SELECT count(*), COALESCE(sum(size_bytes), 0) FROM attachments WHERE work_order_id = $1 AND kind = 'input'`,
			*a.WorkOrderID).Scan(&count, &total); err != nil {
			return fmt.Errorf("count attachments: %w", err)
		}
		if count+1 > maxFiles || total+a.SizeBytes > maxBytes {
			return ErrInputQuota
		}
		var err error
		out, err = scanAttachment(tx.QueryRow(ctx,
			`INSERT INTO attachments (id, work_order_id, run_id, kind, filename, content_type, size_bytes, sha256, storage_key,
			 created_by, created_by_name) VALUES ($1, $2, NULL, 'input', $3, $4, $5, $6, $7, $8, $9) RETURNING `+attachmentColumns,
			a.ID, a.WorkOrderID, a.Filename, a.ContentType, a.SizeBytes, a.SHA256, a.StorageKey, nullableUUID(actor.ID), actor.Name))
		return mapErr(err, "attachment", a.Filename)
	})
	return out, err
}
