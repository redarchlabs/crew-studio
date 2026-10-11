package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// The tools of MCP connections (connection_tools, 0031; docs/agent-chat-handoffs-design.md, slice 3e): what Discover
// tools last found on each server, and the effect an administrator gave each tool.

const connectionToolSelect = `SELECT t.connection_id, t.name, t.description, t.input_schema, t.effect, t.enabled, t.discovered_at,
	COALESCE(u.name, ''), t.updated_at FROM connection_tools t LEFT JOIN users u ON u.id = t.updated_by`

func collectConnectionTools(rows pgx.Rows, err error) ([]model.ConnectionTool, error) {
	if err != nil {
		return nil, fmt.Errorf("list connection tools: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ConnectionTool, error) {
		var t model.ConnectionTool
		var schema []byte
		err := r.Scan(&t.ConnectionID, &t.Name, &t.Description, &schema, &t.Effect, &t.Enabled, &t.DiscoveredAt, &t.UpdatedByName, &t.UpdatedAt)
		t.InputSchema = json.RawMessage(schema)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("read connection tools: %w", err)
	}
	return orEmpty(items), nil
}

// ConnectionTools lists a connection's tools by name.
func (s *Store) ConnectionTools(ctx context.Context, connectionID string) ([]model.ConnectionTool, error) {
	return collectConnectionTools(s.pool.Query(ctx, connectionToolSelect+` WHERE t.connection_id = $1 ORDER BY t.name`, connectionID))
}

// SaveDiscoveredTools replaces a connection's tools with what discovery found (already normalized) and returns them
// with the names of the tools to review. The server's read-only hint is its own claim, so it is trusted only while an
// administrator is setting the connection up: on the first discovery a tool that says it only reads starts as Reads.
// Afterwards a new tool, or one whose description or input schema changed (it may do something else now), starts as
// Changes and is listed for review; an unchanged tool keeps its effect and enabled flag. Tools the server no longer
// offers are removed.
func (s *Store) SaveDiscoveredTools(ctx context.Context, connectionID string, tools []model.DiscoveredTool) ([]model.ConnectionTool, []string, error) {
	review := []string{}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// One discovery at a time per connection: the connection row is the lock
		if _, err := tx.Exec(ctx, `SELECT 1 FROM connections WHERE id = $1 FOR UPDATE`, connectionID); err != nil {
			return fmt.Errorf("lock connection: %w", err)
		}
		var known int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM connection_tools WHERE connection_id = $1`, connectionID).Scan(&known); err != nil {
			return fmt.Errorf("count connection tools: %w", err)
		}
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			names = append(names, t.Name)
			isNew, changed, err := discoveredChange(ctx, tx, connectionID, t)
			if err != nil {
				return err
			}
			effect := model.EffectChanges
			if known == 0 && t.ReadOnly {
				effect = model.EffectReads
			}
			if (isNew && known > 0) || changed {
				review = append(review, t.Name)
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO connection_tools (connection_id, name, description, input_schema, effect)
				 VALUES ($1, $2, $3, $4, $5)
				 ON CONFLICT (connection_id, name) DO UPDATE SET
				   effect = CASE WHEN connection_tools.description = EXCLUDED.description
				                  AND connection_tools.input_schema = EXCLUDED.input_schema
				             THEN connection_tools.effect ELSE 'changes' END,
				   description = EXCLUDED.description, input_schema = EXCLUDED.input_schema, discovered_at = now()`,
				connectionID, t.Name, t.Description, []byte(t.InputSchema), effect); err != nil {
				return fmt.Errorf("save connection tool: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM connection_tools WHERE connection_id = $1 AND NOT (name = ANY($2::text[]))`,
			connectionID, names); err != nil {
			return fmt.Errorf("remove connection tools: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	out, err := s.ConnectionTools(ctx, connectionID)
	return out, review, err
}

// discoveredChange says whether a discovered tool is new to the connection, or known with another description or
// input schema.
func discoveredChange(ctx context.Context, tx pgx.Tx, connectionID string, t model.DiscoveredTool) (bool, bool, error) {
	var same bool
	err := tx.QueryRow(ctx, `SELECT description = $3 AND input_schema = $4::jsonb FROM connection_tools
		WHERE connection_id = $1 AND name = $2`, connectionID, t.Name, t.Description, []byte(t.InputSchema)).Scan(&same)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("compare connection tool: %w", err)
	}
	return false, !same, nil
}

// UpdateConnectionTools sets the effect and enabled flag of the named tools (normalized), all or none: a name the
// connection does not have is refused.
func (s *Store) UpdateConnectionTools(ctx context.Context, connectionID string, changes []model.ConnectionToolChange) ([]model.ConnectionTool, error) {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var missing []string
		for _, c := range changes {
			tag, err := tx.Exec(ctx,
				`UPDATE connection_tools SET effect = $3, enabled = $4, updated_by = $5, updated_at = now()
				 WHERE connection_id = $1 AND name = $2`,
				connectionID, c.Name, c.Effect, c.Enabled, nullableUUID(actorFrom(ctx).ID))
			if err != nil {
				return fmt.Errorf("update connection tool: %w", err)
			}
			if tag.RowsAffected() == 0 {
				missing = append(missing, "tools: the connection has no tool named "+c.Name+"; discover its tools again")
			}
		}
		if len(missing) > 0 {
			return &model.ValidationError{Details: missing}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ConnectionTools(ctx, connectionID)
}

// DeleteConnectionTools forgets a connection's tools (its server address changed, so they describe another server).
func (s *Store) DeleteConnectionTools(ctx context.Context, connectionID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM connection_tools WHERE connection_id = $1`, connectionID); err != nil {
		return fmt.Errorf("delete connection tools: %w", err)
	}
	return nil
}

// ChatMCPTools are the tools agent chat may use on these MCP connections: enabled, effect reads, on an enabled MCP
// connection; in the order of the connections, then by name.
func (s *Store) ChatMCPTools(ctx context.Context, connections []model.RunConnection) ([]model.ChatMCPTool, error) {
	ids := make([]string, 0, len(connections))
	for _, c := range connections {
		if c.Kind == model.KindMCP {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return []model.ChatMCPTool{}, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT t.connection_id, x.name, t.name, t.description, t.input_schema FROM connection_tools t
		 JOIN connections x ON x.id = t.connection_id
		 WHERE t.connection_id::text = ANY($1::text[]) AND t.enabled AND t.effect = 'reads' AND x.enabled AND x.kind = 'mcp'
		 ORDER BY array_position($1::text[], t.connection_id::text), t.name`, ids)
	if err != nil {
		return nil, fmt.Errorf("list chat mcp tools: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ChatMCPTool, error) {
		var t model.ChatMCPTool
		var schema []byte
		err := r.Scan(&t.ConnectionID, &t.ConnectionName, &t.Name, &t.Description, &schema)
		t.InputSchema = json.RawMessage(schema)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("read chat mcp tools: %w", err)
	}
	return orEmpty(out), nil
}

// MCPToolReads reports whether a connection's tool may be called from agent chat now: it exists, is enabled and reads.
func (s *Store) MCPToolReads(ctx context.Context, connectionID, name string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM connection_tools WHERE connection_id = $1 AND name = $2 AND enabled AND effect = 'reads')`,
		connectionID, name).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check mcp tool: %w", err)
	}
	return ok, nil
}
