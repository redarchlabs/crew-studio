package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"crewstudio/internal/model"
)

// ErrResumeActive answers a second resume of a run while the first is still queued or running.
var ErrResumeActive = &ConflictError{Code: "resume_active", Message: "This run is already being resumed. Wait for that run to finish."}

// activeResumeIndex allows one queued or running resume per run (migration 0033)
const activeResumeIndex = "runs_one_active_resume"

// violates reports whether err is a unique violation of the named constraint or index.
func violates(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == constraint
}

// RunResume is the run a new run resumes, the first task it runs, and the outputs it reuses.
type RunResume struct {
	FromRunID     string
	StartPosition int
	Outputs       []model.TaskOutput
}

// linkResume records which run the new one resumes and copies the reused outputs into it, inside CreateRun's
// transaction. Each copy keeps the run that produced it and what it cost then.
func linkResume(ctx context.Context, tx pgx.Tx, run *model.Run, res RunResume) error {
	if _, err := tx.Exec(ctx, `UPDATE runs SET resumed_from_run_id = $2, resume_position = $3 WHERE id = $1`,
		run.ID, res.FromRunID, res.StartPosition); err != nil {
		return fmt.Errorf("link resumed run: %w", err)
	}
	for _, o := range res.Outputs {
		from := res.FromRunID
		if o.ReusedFromRunID != nil {
			from = *o.ReusedFromRunID
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO run_task_outputs (run_id, position, task_name, agent_role, raw, cost_usd, reused_from_run_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			run.ID, o.Position, o.TaskName, o.AgentRole, o.Raw, o.CostUSD, nullableUUID(from)); err != nil {
			return fmt.Errorf("copy reused task output: %w", err)
		}
	}
	run.ResumedFromRunID = &res.FromRunID
	run.ResumePosition = &res.StartPosition
	return nil
}

// upsertTaskOutput saves one task's output. A reused output is never replaced.
func upsertTaskOutput(ctx context.Context, q queryer, runID string, o model.TaskOutput) error {
	if _, err := q.Exec(ctx,
		`INSERT INTO run_task_outputs (run_id, position, task_name, agent_role, raw) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (run_id, position) DO UPDATE SET task_name = EXCLUDED.task_name, agent_role = EXCLUDED.agent_role,
		 raw = EXCLUDED.raw
		 WHERE run_task_outputs.reused_from_run_id IS NULL`,
		runID, o.Position, o.TaskName, o.AgentRole, o.Raw); err != nil {
		return fmt.Errorf("save task output: %w", err)
	}
	return nil
}

// SaveTaskOutput stores a task's output as soon as the task finishes, so a run that is killed, times out or stops with
// the service still keeps (and can resume from) the work it finished. Only runs still in progress take outputs; the
// row lock keeps FinishRun from finishing the run in between.
func (s *Store) SaveTaskOutput(ctx context.Context, runID string, o model.TaskOutput) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT status IN ('queued', 'running') FROM runs WHERE id = $1 FOR SHARE`, runID).Scan(&active); err != nil {
			return mapErr(err, "run", "")
		}
		if !active {
			return nil
		}
		return upsertTaskOutput(ctx, tx, runID, o)
	})
}

// RunByIdempotencyKey returns the run an earlier request with the same (scope, key) created, if any.
func (s *Store) RunByIdempotencyKey(ctx context.Context, scope, key string) (model.Run, bool, error) {
	run, err := scanRun(s.pool.QueryRow(ctx,
		`SELECT `+runColumns+` FROM runs WHERE idempotency_scope = $1 AND idempotency_key = $2`, scope, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Run{}, false, nil
	}
	if err != nil {
		return model.Run{}, false, fmt.Errorf("find run by idempotency key: %w", err)
	}
	return run, true, nil
}

// LastEventOfType returns a run's newest event of one type, or nil when it has none.
func (s *Store) LastEventOfType(ctx context.Context, runID, eventType string) (*model.RunEvent, error) {
	var e model.RunEvent
	err := s.pool.QueryRow(ctx,
		`SELECT seq, type, payload, created_at FROM run_events WHERE run_id = $1 AND type = $2 ORDER BY seq DESC LIMIT 1`,
		runID, eventType).Scan(&e.Seq, &e.Type, &e.Payload, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last %s event: %w", eventType, err)
	}
	return &e, nil
}

// readResumeLinks fills in which run this one resumed and which runs resumed it (newest first), and what each task the
// run did itself cost. Which of those runs the reader may open (or still exist) is the API's to say.
func (s *Store) readResumeLinks(ctx context.Context, r *model.Run) error {
	if err := s.pool.QueryRow(ctx, `SELECT resumed_from_run_id::text, resume_position FROM runs WHERE id = $1`, r.ID).
		Scan(&r.ResumedFromRunID, &r.ResumePosition); err != nil {
		return mapErr(err, "run", "")
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text FROM runs WHERE resumed_from_run_id = $1 ORDER BY created_at DESC, id DESC`, r.ID)
	if err != nil {
		return fmt.Errorf("list resumed runs: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("read resumed runs: %w", err)
	}
	ids = orEmpty(ids)
	r.ResumedBy = &ids
	return s.fillTaskCosts(ctx, r)
}

// fillTaskCosts sets what each task the run did itself cost: its LLM calls (a compaction summary is made for the task
// whose context it compacts) plus its searches and videos, which the runner attributes to the task each agent was
// working on. Costs are keyed by task name, which is unique within a run: task names are unique, and a crew lists a
// task once. Reused outputs keep the cost recorded when they ran. TaskCosts also covers tasks that spent money and
// did not finish, such as the one that failed.
func (s *Store) fillTaskCosts(ctx context.Context, r *model.Run) error {
	costs := map[string]float64{}
	rows, err := s.pool.Query(ctx,
		`SELECT task_name, sum(cost) FROM (
		   SELECT task_name, cost_usd AS cost FROM llm_calls WHERE run_id = $1
		   UNION ALL
		   SELECT payload->>'task_name', (payload->>'cost_usd')::double precision FROM run_events
		    WHERE run_id = $1 AND type IN ('search', 'video') AND jsonb_typeof(payload->'cost_usd') = 'number'
		 ) spend WHERE task_name IS NOT NULL GROUP BY task_name`, r.ID)
	if err != nil {
		return fmt.Errorf("sum task costs: %w", err)
	}
	var name string
	var cost float64
	if _, err := pgx.ForEachRow(rows, []any{&name, &cost}, func() error {
		costs[name] = cost
		return nil
	}); err != nil {
		return fmt.Errorf("read task costs: %w", err)
	}
	if len(costs) > 0 {
		r.TaskCosts = costs
	}
	if r.TaskOutputs == nil {
		return nil
	}
	for i, o := range *r.TaskOutputs {
		if o.ReusedFromRunID == nil {
			c := costs[o.TaskName]
			(*r.TaskOutputs)[i].CostUSD = &c
		}
	}
	return nil
}

// RunsByIDs returns the runs with these ids that still exist, in no particular order.
func (s *Store) RunsByIDs(ctx context.Context, ids []string) ([]model.Run, error) {
	if len(ids) == 0 {
		return []model.Run{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("list runs by id: %w", err)
	}
	return collectRuns(rows)
}
