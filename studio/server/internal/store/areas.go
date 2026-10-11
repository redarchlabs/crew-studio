package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const areaSelect = `SELECT a.id, a.name, a.description,
	(SELECT count(*) FROM crew_areas x WHERE x.area_id = a.id),
	(SELECT count(*) FROM agent_areas x WHERE x.area_id = a.id),
	(SELECT count(*) FROM task_areas x WHERE x.area_id = a.id),
	(SELECT count(*) FROM user_areas x WHERE x.area_id = a.id),
	a.created_at, a.updated_at FROM functional_areas a`

func scanArea(row pgx.Row) (model.Area, error) {
	var a model.Area
	var u model.AreaUsage
	err := row.Scan(&a.ID, &a.Name, &a.Description, &u.Crews, &u.Agents, &u.Tasks, &u.Users, &a.CreatedAt, &a.UpdatedAt)
	a.Usage = &u
	return a, err
}

// ListAreas returns every functional area. Area names are not secret: everyone signed in may list them.
func (s *Store) ListAreas(ctx context.Context) ([]model.Area, error) {
	rows, err := s.pool.Query(ctx, areaSelect+` ORDER BY a.name`)
	if err != nil {
		return nil, fmt.Errorf("list areas: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Area, error) { return scanArea(r) })
	if err != nil {
		return nil, fmt.Errorf("read areas: %w", err)
	}
	return orEmpty(items), nil
}

func getArea(ctx context.Context, q queryer, id string) (model.Area, error) {
	a, err := scanArea(q.QueryRow(ctx, areaSelect+` WHERE a.id = $1`, id))
	return a, mapErr(err, "functional area", "")
}

func (s *Store) GetArea(ctx context.Context, id string) (model.Area, error) {
	return getArea(ctx, s.pool, id)
}

func (s *Store) CreateArea(ctx context.Context, in model.Area) (model.Area, error) {
	var out model.Area
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO functional_areas (name, description) VALUES ($1, $2) RETURNING id`,
			in.Name, in.Description).Scan(&id); err != nil {
			return mapErr(err, "functional area", in.Name)
		}
		var err error
		if out, err = getArea(ctx, tx, id); err != nil {
			return err
		}
		return recordRevision(ctx, tx, "area", id, "create", out)
	})
	return out, err
}

func (s *Store) UpdateArea(ctx context.Context, id string, in model.Area) (model.Area, error) {
	var out model.Area
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE functional_areas SET name = $2, description = $3, updated_at = now() WHERE id = $1`,
			id, in.Name, in.Description)
		if err != nil {
			return mapErr(err, "functional area", in.Name)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if out, err = getArea(ctx, tx, id); err != nil {
			return err
		}
		return recordRevision(ctx, tx, "area", id, "update", out)
	})
	return out, err
}

// DeleteArea removes an empty area. People lose membership of it; crews, agents and tasks must be moved first.
func (s *Store) DeleteArea(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := getArea(ctx, tx, id)
		if err != nil {
			return err
		}
		if n := old.Usage.Crews + old.Usage.Agents + old.Usage.Tasks; n > 0 {
			return &ConflictError{Message: fmt.Sprintf(
				"%s still holds %d crews, %d agents and %d tasks. Move them to another area first.",
				old.Name, old.Usage.Crews, old.Usage.Agents, old.Usage.Tasks)}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM functional_areas WHERE id = $1`, id); err != nil {
			return mapErr(err, "functional area", "")
		}
		return recordRevision(ctx, tx, "area", id, "delete", old)
	})
}

// ExistingAreaIDs returns which of ids are real areas.
func (s *Store) ExistingAreaIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM functional_areas WHERE id = ANY($1::uuid[])`, orEmpty(ids))
	if err != nil {
		return nil, fmt.Errorf("check areas: %w", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read areas: %w", err)
	}
	out := make(map[string]bool, len(found))
	for _, id := range found {
		out[id] = true
	}
	return out, nil
}
