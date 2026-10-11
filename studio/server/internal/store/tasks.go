package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const taskSelect = `SELECT t.id, t.name, t.description, t.expected_output, t.agent_id,
	COALESCE((SELECT array_agg(tc.context_task_id::text ORDER BY tc.position) FROM task_context tc WHERE tc.task_id = t.id), '{}'),
	t.markdown,
	COALESCE((SELECT array_agg(x.area_id::text ORDER BY x.area_id) FROM task_areas x WHERE x.task_id = t.id), '{}'),
	t.created_at, t.updated_at FROM tasks t`

func scanTask(row pgx.Row) (model.Task, error) {
	var t model.Task
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.ExpectedOutput, &t.AgentID, &t.ContextTaskIDs,
		&t.Markdown, &t.AreaIDs, &t.CreatedAt, &t.UpdatedAt)
	t.ContextTaskIDs = orEmpty(t.ContextTaskIDs)
	t.AreaIDs = orEmpty(t.AreaIDs)
	return t, err
}

// ListTasks returns the tasks visible in scope.
func (s *Store) ListTasks(ctx context.Context, scope Scope) ([]model.Task, error) {
	all, areas := scope.areaArgs()
	rows, err := s.pool.Query(ctx, taskSelect+` WHERE `+areaFilter("task_areas", "task_id", "t.id")+` ORDER BY t.name`, all, areas)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Task, error) { return scanTask(r) })
	if err != nil {
		return nil, fmt.Errorf("read tasks: %w", err)
	}
	return orEmpty(items), nil
}

func getTask(ctx context.Context, q queryer, id string) (model.Task, error) {
	t, err := scanTask(q.QueryRow(ctx, taskSelect+` WHERE t.id = $1`, id))
	return t, mapErr(err, "task", "")
}

// GetTask returns one task regardless of scope; callers check area access.
func (s *Store) GetTask(ctx context.Context, id string) (model.Task, error) {
	return getTask(ctx, s.pool, id)
}

func replaceTaskContext(ctx context.Context, tx pgx.Tx, taskID string, contextIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM task_context WHERE task_id = $1`, taskID); err != nil {
		return fmt.Errorf("clear task context: %w", err)
	}
	if len(contextIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO task_context (task_id, context_task_id, position)
		 SELECT $1, id, ord - 1 FROM unnest($2::uuid[]) WITH ORDINALITY AS c(id, ord)`,
		taskID, contextIDs)
	if err != nil {
		return mapErr(err, "task", "")
	}
	return nil
}

func (s *Store) CreateTask(ctx context.Context, in model.Task) (model.Task, error) {
	var out model.Task
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx,
			`INSERT INTO tasks (name, description, expected_output, agent_id, markdown) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			in.Name, in.Description, in.ExpectedOutput, in.AgentID, in.Markdown).Scan(&id)
		if err != nil {
			return mapErr(err, "task", in.Name)
		}
		if err := replaceTaskContext(ctx, tx, id, in.ContextTaskIDs); err != nil {
			return err
		}
		if err := replaceAreas(ctx, tx, "task_areas", "task_id", id, in.AreaIDs); err != nil {
			return err
		}
		if out, err = getTask(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "task", out.ID, "create", out, out.AreaIDs)
	})
	return out, err
}

func (s *Store) UpdateTask(ctx context.Context, id string, in model.Task) (model.Task, error) {
	var out model.Task
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE tasks SET name = $2, description = $3, expected_output = $4, agent_id = $5, markdown = $6, updated_at = now()
			 WHERE id = $1`,
			id, in.Name, in.Description, in.ExpectedOutput, in.AgentID, in.Markdown)
		if err != nil {
			return mapErr(err, "task", in.Name)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := replaceTaskContext(ctx, tx, id, in.ContextTaskIDs); err != nil {
			return err
		}
		if err := replaceAreas(ctx, tx, "task_areas", "task_id", id, in.AreaIDs); err != nil {
			return err
		}
		if out, err = getTask(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "task", out.ID, "update", out, out.AreaIDs)
	})
	return out, err
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		users, err := usedBy(ctx, tx, `
			SELECT 'crew "' || c.name || '"' FROM crews c JOIN crew_tasks ct ON ct.crew_id = c.id WHERE ct.task_id = $1
			UNION ALL
			SELECT 'task "' || t.name || '" (as context)' FROM tasks t JOIN task_context tc ON tc.task_id = t.id
			WHERE tc.context_task_id = $1`, id)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return inUseError("task", users)
		}
		old, err := getTask(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id); err != nil {
			return mapErr(err, "task", "")
		}
		return recordAreaRevision(ctx, tx, "task", id, "delete", old, old.AreaIDs)
	})
}
