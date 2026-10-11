package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Scope limits area-scoped reads to what one principal may see.
type Scope struct {
	All     bool
	AreaIDs []string
}

// AllAreas is the scope of the service itself (run manager, scheduler).
var AllAreas = Scope{All: true}

func (s Scope) areaArgs() (bool, []string) {
	ids := s.AreaIDs
	if ids == nil {
		ids = []string{}
	}
	return s.All, ids
}

// Actor is who made a change; it is recorded on every revision.
type Actor struct {
	ID   string
	Name string
}

type actorKey struct{}

// WithActor records who is acting for the rest of the request.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

func actorFrom(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey{}).(Actor)
	return a
}

func nullableUUID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// recordAreaRevision is recordRevision for items that belong to functional areas.
func recordAreaRevision(ctx context.Context, q queryer, entityType, entityID, action string, snapshot any, areaIDs []string) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode %s revision: %w", entityType, err)
	}
	actor := actorFrom(ctx)
	_, err = q.Exec(ctx,
		`INSERT INTO revisions (entity_type, entity_id, action, snapshot, actor_id, actor_name, area_ids)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entityType, entityID, action, data, nullableUUID(actor.ID), actor.Name, orEmpty(areaIDs))
	if err != nil {
		return fmt.Errorf("record %s revision: %w", entityType, err)
	}
	return nil
}

// replaceAreas sets the areas of one item in a join table (agent_areas, task_areas or crew_areas).
func replaceAreas(ctx context.Context, q queryer, table, column, id string, areaIDs []string) error {
	if _, err := q.Exec(ctx, `DELETE FROM `+table+` WHERE `+column+` = $1`, id); err != nil {
		return fmt.Errorf("clear %s: %w", table, err)
	}
	if _, err := q.Exec(ctx,
		`INSERT INTO `+table+` (`+column+`, area_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`,
		id, orEmpty(areaIDs)); err != nil {
		return mapErr(err, "functional area", "")
	}
	return nil
}

// areaFilter is the SQL condition limiting rows of a join table to a scope; $1 is "all", $2 the area ids.
func areaFilter(table, column, outer string) string {
	return `($1::boolean OR EXISTS (SELECT 1 FROM ` + table + ` x WHERE x.` + column + ` = ` + outer +
		` AND x.area_id = ANY($2::uuid[])))`
}

// VisibleIDs returns which of ids (agents, tasks, crews or connections) exist and are visible in scope.
func (s *Store) VisibleIDs(ctx context.Context, kind string, ids []string, scope Scope) (map[string]bool, error) {
	var table, joinTable, column string
	filter := ""
	switch kind {
	case "connection":
		// Connections keep their areas on the row, not in a join table
		table, filter = "connections", "($1::boolean OR t.area_ids && $2::uuid[])"
	case "agent":
		table, joinTable, column = "agents", "agent_areas", "agent_id"
	case "task":
		table, joinTable, column = "tasks", "task_areas", "task_id"
	case "crew":
		table, joinTable, column = "crews", "crew_areas", "crew_id"
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	if filter == "" {
		filter = areaFilter(joinTable, column, "t.id")
	}
	all, areas := scope.areaArgs()
	rows, err := s.pool.Query(ctx, `SELECT t.id::text FROM `+table+` t WHERE t.id = ANY($3::uuid[]) AND `+filter, all, areas, orEmpty(ids))
	if err != nil {
		return nil, fmt.Errorf("check visible %ss: %w", kind, err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read visible %ss: %w", kind, err)
	}
	out := make(map[string]bool, len(found))
	for _, id := range found {
		out[id] = true
	}
	return out, nil
}
