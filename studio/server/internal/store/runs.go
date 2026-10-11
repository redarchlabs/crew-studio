package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const runColumns = `id, crew_id, crew_name, status, inputs, budget_usd, output, error, cost_usd, usage,
	area_ids::text[], created_by, created_by_name, trigger_type, kind, work_order_id, created_at, started_at, finished_at`

func scanRun(row pgx.Row) (model.Run, error) {
	var r model.Run
	err := row.Scan(&r.ID, &r.CrewID, &r.CrewName, &r.Status, &r.Inputs, &r.BudgetUSD, &r.Output, &r.Error,
		&r.CostUSD, &r.Usage, &r.AreaIDs, &r.CreatedBy, &r.CreatedByName, &r.TriggerType, &r.Kind, &r.WorkOrderID,
		&r.CreatedAt, &r.StartedAt, &r.FinishedAt)
	if r.Inputs == nil {
		r.Inputs = map[string]string{}
	}
	r.AreaIDs = orEmpty(r.AreaIDs)
	return r, err
}

func collectRuns(rows pgx.Rows) ([]model.Run, error) {
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Run, error) { return scanRun(r) })
	if err != nil {
		return nil, fmt.Errorf("read runs: %w", err)
	}
	return orEmpty(items), nil
}

// RunMeta is who started a run, from where, and what it may be found under.
type RunMeta struct {
	AreaIDs       []string
	CreatedBy     string
	CreatedByName string
	TriggerType   string
	WorkOrderID   string
	// Kind is model.RunKindCrew (the default), model.RunKindChat or model.RunKindHandoff
	Kind string
	// ChatAgentID is the agent an agent-chat turn talks to (docs/agent-chat-design.md)
	ChatAgentID string
	// A repeated (scope, key) pair returns the first run instead of starting another
	IdempotencyScope string
	IdempotencyKey   string
	Callback         *Callback
}

// Callback asks for a signed POST to URL when the run finishes.
type Callback struct {
	URL          string
	SecretSource string
	SecretOwner  string
}

const (
	CallbackFromTrigger        = "trigger"
	CallbackFromServiceAccount = "service_account"
)

// insertRun adds a queued run with its frozen spec inside tx.
func insertRun(ctx context.Context, tx pgx.Tx, spec model.RunSpec, meta RunMeta, scope, key *string) (model.Run, error) {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return model.Run{}, fmt.Errorf("encode run spec: %w", err)
	}
	inputsJSON, err := json.Marshal(spec.Inputs)
	if err != nil {
		return model.Run{}, fmt.Errorf("encode run inputs: %w", err)
	}
	if meta.TriggerType == "" {
		meta.TriggerType = "manual"
	}
	if meta.Kind == "" {
		meta.Kind = model.RunKindCrew
	}
	run, err := scanRun(tx.QueryRow(ctx,
		`INSERT INTO runs (crew_id, crew_name, status, inputs, budget_usd, spec, area_ids, created_by, created_by_name,
		 trigger_type, kind, work_order_id, idempotency_scope, idempotency_key, chat_agent_id)
		 VALUES ($1, $2, 'queued', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING `+runColumns,
		nullableUUID(spec.Crew.ID), spec.Crew.Name, inputsJSON, spec.BudgetUSD, specJSON, orEmpty(meta.AreaIDs),
		nullableUUID(meta.CreatedBy), meta.CreatedByName, meta.TriggerType, meta.Kind, nullableUUID(meta.WorkOrderID), scope, key,
		nullableUUID(meta.ChatAgentID)))
	if err != nil {
		return model.Run{}, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

// CreateRun stores a queued run with its frozen spec. When an idempotency key repeats, the earlier run is
// returned with existing=true and nothing new is created.
func (s *Store) CreateRun(ctx context.Context, spec model.RunSpec, meta RunMeta) (run model.Run, existing bool, err error) {
	var scope, key *string
	if meta.IdempotencyKey != "" {
		scope, key = &meta.IdempotencyScope, &meta.IdempotencyKey
		if prior, err := scanRun(s.pool.QueryRow(ctx,
			`SELECT `+runColumns+` FROM runs WHERE idempotency_scope = $1 AND idempotency_key = $2`, scope, key)); err == nil {
			return prior, true, nil
		}
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if meta.WorkOrderID != "" {
			// Two submits of one work order must not both start a run
			if err := lockIdleWorkOrder(ctx, tx, meta.WorkOrderID); err != nil {
				return err
			}
		}
		var err error
		if run, err = insertRun(ctx, tx, spec, meta, scope, key); err != nil {
			return err
		}
		if meta.WorkOrderID != "" {
			if _, err := tx.Exec(ctx,
				`UPDATE work_orders SET current_run_id = $2, submitted_at = COALESCE(submitted_at, now()), updated_at = now()
				 WHERE id = $1`, meta.WorkOrderID, run.ID); err != nil {
				return fmt.Errorf("link run to work order: %w", err)
			}
		}
		if cb := meta.Callback; cb != nil {
			if _, err := tx.Exec(ctx,
				`INSERT INTO callback_deliveries (run_id, url, secret_source, secret_owner_id) VALUES ($1, $2, $3, $4)`,
				run.ID, cb.URL, cb.SecretSource, cb.SecretOwner); err != nil {
				return fmt.Errorf("queue callback: %w", err)
			}
		}
		return nil
	})
	if err != nil && meta.IdempotencyKey != "" && isUniqueViolation(err) {
		// Lost a race with an identical request
		prior, perr := scanRun(s.pool.QueryRow(ctx,
			`SELECT `+runColumns+` FROM runs WHERE idempotency_scope = $1 AND idempotency_key = $2`, scope, key))
		if perr == nil {
			return prior, true, nil
		}
	}
	return run, false, err
}

type RunFilter struct {
	CrewID      string
	WorkOrderID string
	Status      string
	TriggerType string
	// Kind is "crew", "chat", "handoff" or "" for all
	Kind   string
	Scope  Scope
	Limit  int
	Offset int
	// Viewer, when set, limits chat turns and hand-offs to that person's own (they hold private conversations)
	Viewer string
}

// ListRuns returns runs visible in the filter's scope, newest first.
func (s *Store) ListRuns(ctx context.Context, f RunFilter) ([]model.Run, int, error) {
	all, areas := f.Scope.areaArgs()
	const where = `WHERE ($1::boolean OR area_ids && $2::uuid[])
		AND ($3::uuid IS NULL OR crew_id = $3) AND ($4::uuid IS NULL OR work_order_id = $4)
		AND ($5::text IS NULL OR status = $5) AND ($6::text IS NULL OR trigger_type = $6)
		AND ($7::text IS NULL OR kind = $7) AND ($8::uuid IS NULL OR kind NOT IN ('chat', 'handoff') OR created_by = $8)`
	args := []any{all, areas, nullableUUID(f.CrewID), nullableUUID(f.WorkOrderID), nullableText(f.Status), nullableText(f.TriggerType),
		nullableText(f.Kind), nullableUUID(f.Viewer)}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count runs: %w", err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+runColumns+` FROM runs `+where+` ORDER BY created_at DESC, id DESC LIMIT $9 OFFSET $10`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list runs: %w", err)
	}
	runs, err := collectRuns(rows)
	return runs, total, err
}

func nullableText(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func (s *Store) GetRun(ctx context.Context, id string) (model.Run, error) {
	r, err := scanRun(s.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id = $1`, id))
	return r, mapErr(err, "run", "")
}

// GetRunDetail returns the run with its task outputs and frozen spec.
func (s *Store) GetRunDetail(ctx context.Context, id string) (model.Run, error) {
	r, err := s.GetRun(ctx, id)
	if err != nil {
		return r, err
	}
	spec, err := s.GetRunSpec(ctx, id)
	if err != nil {
		return r, err
	}
	r.Spec = &spec
	rows, err := s.pool.Query(ctx,
		`SELECT position, task_name, agent_role, raw FROM run_task_outputs WHERE run_id = $1 ORDER BY position`, id)
	if err != nil {
		return r, fmt.Errorf("list task outputs: %w", err)
	}
	outputs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.TaskOutput, error) {
		var o model.TaskOutput
		err := row.Scan(&o.Position, &o.TaskName, &o.AgentRole, &o.Raw)
		return o, err
	})
	if err != nil {
		return r, fmt.Errorf("read task outputs: %w", err)
	}
	outputs = orEmpty(outputs)
	r.TaskOutputs = &outputs
	return r, nil
}

func (s *Store) GetRunSpec(ctx context.Context, id string) (model.RunSpec, error) {
	var spec model.RunSpec
	err := s.pool.QueryRow(ctx, `SELECT spec FROM runs WHERE id = $1`, id).Scan(&spec)
	return spec, mapErr(err, "run", "")
}

// StartRun moves a queued run to running. It returns false if the run was no longer queued. A hand-off's time limit
// starts counting now.
func (s *Store) StartRun(ctx context.Context, id string) (bool, error) {
	var started bool
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE runs SET status = 'running', started_at = now() WHERE id = $1 AND status = 'queued'`, id)
		if err != nil {
			return fmt.Errorf("start run: %w", err)
		}
		if started = tag.RowsAffected() == 1; !started {
			return nil
		}
		return startHandoffClock(ctx, tx, id)
	})
	return started, err
}

// RunResult is what a finished run reports back.
type RunResult struct {
	Status      model.RunStatus
	Output      *string
	Error       *string
	CostUSD     *float64
	Usage       *model.TokenUsage
	TaskOutputs []model.TaskOutput
}

// FinishRun records the outcome of a run that has not already finished.
func (s *Store) FinishRun(ctx context.Context, id string, res RunResult) (model.Run, error) {
	var out model.Run
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanRun(tx.QueryRow(ctx,
			`UPDATE runs SET status = $2, output = $3, error = $4, cost_usd = $5, usage = $6, finished_at = now()
			 WHERE id = $1 AND status IN ('queued', 'running') RETURNING `+runColumns,
			id, res.Status, res.Output, res.Error, res.CostUSD, res.Usage))
		if err != nil {
			return mapErr(err, "run", "")
		}
		for _, o := range res.TaskOutputs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO run_task_outputs (run_id, position, task_name, agent_role, raw) VALUES ($1, $2, $3, $4, $5)
				 ON CONFLICT (run_id, position) DO UPDATE SET task_name = EXCLUDED.task_name,
				 agent_role = EXCLUDED.agent_role, raw = EXCLUDED.raw`,
				id, o.Position, o.TaskName, o.AgentRole, o.Raw); err != nil {
				return fmt.Errorf("save task output: %w", err)
			}
		}
		return nil
	})
	return out, err
}

// CancelQueuedRun cancels a run only if it has not started. It returns false when the run was not queued.
func (s *Store) CancelQueuedRun(ctx context.Context, id, reason string) (model.Run, bool, error) {
	r, err := scanRun(s.pool.QueryRow(ctx,
		`UPDATE runs SET status = 'cancelled', error = $2, finished_at = now()
		 WHERE id = $1 AND status = 'queued' RETURNING `+runColumns, id, reason))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Run{}, false, nil
	}
	if err != nil {
		return model.Run{}, false, fmt.Errorf("cancel queued run: %w", err)
	}
	return r, true, nil
}

// ForceFinishRun records only a terminal status and error, for when saving the full result failed.
func (s *Store) ForceFinishRun(ctx context.Context, id string, status model.RunStatus, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE runs SET status = $2, error = $3, finished_at = now() WHERE id = $1 AND status IN ('queued', 'running')`,
		id, status, reason)
	if err != nil {
		return fmt.Errorf("force finish run: %w", err)
	}
	return nil
}

// QueuedRunIDs returns queued runs, oldest first.
func (s *Store) QueuedRunIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text FROM runs WHERE status = 'queued' ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list queued runs: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read queued runs: %w", err)
	}
	return ids, nil
}

// FailOrphanedRuns marks runs left "running" by a previous service process as failed.
func (s *Store) FailOrphanedRuns(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE runs SET status = 'failed', finished_at = now(),
		 error = 'The Crew Studio service restarted while this run was in progress.'
		 WHERE status = 'running' RETURNING id::text`)
	if err != nil {
		return nil, fmt.Errorf("fail orphaned runs: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read orphaned runs: %w", err)
	}
	return ids, nil
}

// DeleteRun removes a finished run with its events, outputs and files. A work order then points at its
// newest remaining run.
func (s *Store) DeleteRun(ctx context.Context, id string) error {
	run, err := s.GetRun(ctx, id)
	if err != nil {
		return err
	}
	if !run.Status.Terminal() {
		return &ConflictError{Message: "Cancel the run before deleting it."}
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM runs WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete run: %w", err)
		}
		if run.WorkOrderID != nil {
			if _, err := tx.Exec(ctx,
				`UPDATE work_orders SET current_run_id = (SELECT id FROM runs WHERE work_order_id = $1 ORDER BY created_at DESC LIMIT 1)
				 WHERE id = $1`, *run.WorkOrderID); err != nil {
				return fmt.Errorf("relink work order: %w", err)
			}
		}
		return nil
	})
}

// FindRunByIdempotency returns the run an earlier request with the same key created.
func (s *Store) FindRunByIdempotency(ctx context.Context, scope, key string) (model.Run, error) {
	r, err := scanRun(s.pool.QueryRow(ctx,
		`SELECT `+runColumns+` FROM runs WHERE idempotency_scope = $1 AND idempotency_key = $2`, scope, key))
	return r, mapErr(err, "run", "")
}
