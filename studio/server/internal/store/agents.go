package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const agentSelect = `SELECT a.id, a.name, a.role, a.goal, a.backstory, a.llm_id, a.tools, a.max_iter, a.allow_delegation,
	a.verbose_output,
	COALESCE((SELECT array_agg(x.area_id::text ORDER BY x.area_id) FROM agent_areas x WHERE x.agent_id = a.id), '{}'),
	a.created_at, a.updated_at FROM agents a`

func scanAgent(row pgx.Row) (model.Agent, error) {
	var a model.Agent
	err := row.Scan(&a.ID, &a.Name, &a.Role, &a.Goal, &a.Backstory, &a.LLMID, &a.Tools, &a.MaxIter,
		&a.AllowDelegation, &a.Verbose, &a.AreaIDs, &a.CreatedAt, &a.UpdatedAt)
	a.Tools = orEmpty(a.Tools)
	a.AreaIDs = orEmpty(a.AreaIDs)
	return a, err
}

// ListAgents returns the agents visible in scope.
func (s *Store) ListAgents(ctx context.Context, scope Scope) ([]model.Agent, error) {
	all, areas := scope.areaArgs()
	rows, err := s.pool.Query(ctx, agentSelect+` WHERE `+areaFilter("agent_areas", "agent_id", "a.id")+` ORDER BY a.name`, all, areas)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Agent, error) { return scanAgent(r) })
	if err != nil {
		return nil, fmt.Errorf("read agents: %w", err)
	}
	return orEmpty(items), nil
}

func getAgent(ctx context.Context, q queryer, id string) (model.Agent, error) {
	a, err := scanAgent(q.QueryRow(ctx, agentSelect+` WHERE a.id = $1`, id))
	return a, mapErr(err, "agent", "")
}

// GetAgent returns one agent regardless of scope; callers check area access.
func (s *Store) GetAgent(ctx context.Context, id string) (model.Agent, error) {
	return getAgent(ctx, s.pool, id)
}

func (s *Store) CreateAgent(ctx context.Context, in model.Agent) (model.Agent, error) {
	var out model.Agent
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx,
			`INSERT INTO agents (name, role, goal, backstory, llm_id, tools, max_iter, allow_delegation, verbose_output)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			in.Name, in.Role, in.Goal, in.Backstory, in.LLMID, in.Tools, in.MaxIter, in.AllowDelegation, in.Verbose).Scan(&id)
		if err != nil {
			return mapErr(err, "agent", in.Name)
		}
		if err := replaceAreas(ctx, tx, "agent_areas", "agent_id", id, in.AreaIDs); err != nil {
			return err
		}
		if out, err = getAgent(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "agent", out.ID, "create", out, out.AreaIDs)
	})
	return out, err
}

func (s *Store) UpdateAgent(ctx context.Context, id string, in model.Agent) (model.Agent, error) {
	var out model.Agent
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE agents SET name = $2, role = $3, goal = $4, backstory = $5, llm_id = $6, tools = $7, max_iter = $8,
			 allow_delegation = $9, verbose_output = $10, updated_at = now() WHERE id = $1`,
			id, in.Name, in.Role, in.Goal, in.Backstory, in.LLMID, in.Tools, in.MaxIter, in.AllowDelegation, in.Verbose)
		if err != nil {
			return mapErr(err, "agent", in.Name)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := replaceAreas(ctx, tx, "agent_areas", "agent_id", id, in.AreaIDs); err != nil {
			return err
		}
		if out, err = getAgent(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "agent", out.ID, "update", out, out.AreaIDs)
	})
	return out, err
}

func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		users, err := usedBy(ctx, tx, `
			SELECT 'task "' || name || '"' FROM tasks WHERE agent_id = $1
			UNION ALL
			SELECT 'crew "' || c.name || '"' FROM crews c JOIN crew_agents ca ON ca.crew_id = c.id WHERE ca.agent_id = $1
			UNION ALL
			SELECT 'agent "' || a.name || '" (chat specialist)' FROM agent_chat_specialists sp JOIN agents a ON a.id = sp.agent_id
			WHERE sp.specialist_id = $1
			UNION ALL
			-- A running hand-off holds its agent (the lead) and its worker; deleting either would cut it off mid-run
			SELECT 'hand-off "' || h.title || '" (running; cancel it or wait for it)' FROM chat_handoffs h JOIN runs r ON r.id = h.run_id
			WHERE r.status IN ('queued', 'running') AND (r.chat_agent_id = $1 OR h.worker_agent_id = $1)`, id)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return inUseError("agent", users)
		}
		old, err := getAgent(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM agents WHERE id = $1`, id); err != nil {
			return mapErr(err, "agent", "")
		}
		return recordAreaRevision(ctx, tx, "agent", id, "delete", old, old.AreaIDs)
	})
}
