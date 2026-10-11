package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const crewSelect = `SELECT c.id, c.name, c.description, c.process, c.manager_llm_id,
	COALESCE((SELECT array_agg(ca.agent_id::text ORDER BY ca.position) FROM crew_agents ca WHERE ca.crew_id = c.id), '{}'),
	COALESCE((SELECT array_agg(ct.task_id::text ORDER BY ct.position) FROM crew_tasks ct WHERE ct.crew_id = c.id), '{}'),
	c.inputs, c.budget_usd,
	COALESCE((SELECT array_agg(x.area_id::text ORDER BY x.area_id) FROM crew_areas x WHERE x.crew_id = c.id), '{}'),
	c.created_at, c.updated_at FROM crews c`

func scanCrew(row pgx.Row) (model.Crew, error) {
	var c model.Crew
	err := row.Scan(&c.ID, &c.Name, &c.Description, &c.Process, &c.ManagerLLMID, &c.AgentIDs, &c.TaskIDs,
		&c.Inputs, &c.BudgetUSD, &c.AreaIDs, &c.CreatedAt, &c.UpdatedAt)
	c.AreaIDs = orEmpty(c.AreaIDs)
	c.AgentIDs = orEmpty(c.AgentIDs)
	c.TaskIDs = orEmpty(c.TaskIDs)
	c.Inputs = orEmpty(c.Inputs)
	return c, err
}

// ListCrews returns the crews visible in scope.
func (s *Store) ListCrews(ctx context.Context, scope Scope) ([]model.Crew, error) {
	all, areas := scope.areaArgs()
	rows, err := s.pool.Query(ctx, crewSelect+` WHERE `+areaFilter("crew_areas", "crew_id", "c.id")+` ORDER BY c.name`, all, areas)
	if err != nil {
		return nil, fmt.Errorf("list crews: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Crew, error) { return scanCrew(r) })
	if err != nil {
		return nil, fmt.Errorf("read crews: %w", err)
	}
	return orEmpty(items), nil
}

func getCrew(ctx context.Context, q queryer, id string) (model.Crew, error) {
	c, err := scanCrew(q.QueryRow(ctx, crewSelect+` WHERE c.id = $1`, id))
	return c, mapErr(err, "crew", "")
}

// GetCrew returns one crew regardless of scope; callers check area access.
func (s *Store) GetCrew(ctx context.Context, id string) (model.Crew, error) {
	return getCrew(ctx, s.pool, id)
}

func replaceCrewMembers(ctx context.Context, tx pgx.Tx, crewID string, agentIDs, taskIDs []string) error {
	for _, table := range []string{"crew_agents", "crew_tasks"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE crew_id = $1`, crewID); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO crew_agents (crew_id, agent_id, position)
		 SELECT $1, id, ord - 1 FROM unnest($2::uuid[]) WITH ORDINALITY AS a(id, ord)`, crewID, agentIDs); err != nil {
		return mapErr(err, "crew", "")
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO crew_tasks (crew_id, task_id, position)
		 SELECT $1, id, ord - 1 FROM unnest($2::uuid[]) WITH ORDINALITY AS t(id, ord)`, crewID, taskIDs); err != nil {
		return mapErr(err, "crew", "")
	}
	return nil
}

func encodeInputs(inputs []model.CrewInput) ([]byte, error) {
	data, err := json.Marshal(orEmpty(inputs))
	if err != nil {
		return nil, fmt.Errorf("encode crew inputs: %w", err)
	}
	return data, nil
}

func (s *Store) CreateCrew(ctx context.Context, in model.Crew) (model.Crew, error) {
	inputs, err := encodeInputs(in.Inputs)
	if err != nil {
		return model.Crew{}, err
	}
	var out model.Crew
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx,
			`INSERT INTO crews (name, description, process, manager_llm_id, inputs, budget_usd)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			in.Name, in.Description, in.Process, in.ManagerLLMID, inputs, in.BudgetUSD).Scan(&id)
		if err != nil {
			return mapErr(err, "crew", in.Name)
		}
		if err := replaceCrewMembers(ctx, tx, id, in.AgentIDs, in.TaskIDs); err != nil {
			return err
		}
		if err := replaceAreas(ctx, tx, "crew_areas", "crew_id", id, in.AreaIDs); err != nil {
			return err
		}
		if out, err = getCrew(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "crew", out.ID, "create", out, out.AreaIDs)
	})
	return out, err
}

func (s *Store) UpdateCrew(ctx context.Context, id string, in model.Crew) (model.Crew, error) {
	inputs, err := encodeInputs(in.Inputs)
	if err != nil {
		return model.Crew{}, err
	}
	var out model.Crew
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE crews SET name = $2, description = $3, process = $4, manager_llm_id = $5, inputs = $6, budget_usd = $7,
			 updated_at = now() WHERE id = $1`,
			id, in.Name, in.Description, in.Process, in.ManagerLLMID, inputs, in.BudgetUSD)
		if err != nil {
			return mapErr(err, "crew", in.Name)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := replaceCrewMembers(ctx, tx, id, in.AgentIDs, in.TaskIDs); err != nil {
			return err
		}
		if err := replaceAreas(ctx, tx, "crew_areas", "crew_id", id, in.AreaIDs); err != nil {
			return err
		}
		if out, err = getCrew(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "crew", out.ID, "update", out, out.AreaIDs)
	})
	return out, err
}

// DeleteCrew removes the crew. Its past runs and work orders are kept, detached from the crew.
func (s *Store) DeleteCrew(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		users, err := usedBy(ctx, tx, `
			SELECT 'schedule "' || name || '"' FROM schedules WHERE crew_id = $1
			UNION ALL
			SELECT 'trigger "' || name || '"' FROM triggers WHERE crew_id = $1
			UNION ALL
			SELECT 'agent "' || a.name || '" (chat)' FROM agent_chat_crews k JOIN agents a ON a.id = k.agent_id WHERE k.crew_id = $1`, id)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return inUseError("crew", users)
		}
		old, err := getCrew(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crews WHERE id = $1`, id); err != nil {
			return mapErr(err, "crew", "")
		}
		return recordAreaRevision(ctx, tx, "crew", id, "delete", old, old.AreaIDs)
	})
}
