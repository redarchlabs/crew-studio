package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

// RunTokenPrefix starts every run token, so a leaked one is recognisable.
const RunTokenPrefix = "csr_"

// expiredTokenRetention is how long expired run tokens linger before they are swept.
const expiredTokenRetention = 24 * time.Hour

// RunConnection is a connection a run may use (never its secrets).
type RunConnection = model.RunConnection

// RunScope is everything a run token allows: one run, its crew and schedule, its areas, and the connections
// its crew may use that share one of those areas.
type RunScope struct {
	RunID string `json:"run_id"`
	// CrewID is "" when the crew was deleted after the run started
	CrewID       string          `json:"crew_id"`
	CrewName     string          `json:"crew_name"`
	ScheduleID   *string         `json:"schedule_id"`
	ScheduleName string          `json:"schedule_name"`
	AreaIDs      []string        `json:"area_ids"`
	Connections  []RunConnection `json:"connections"`
	ExpiresAt    time.Time       `json:"expires_at"`
	// FromSchedule is true when a schedule started the run; with ScheduleID nil, that schedule has been deleted
	FromSchedule bool `json:"-"`
	// DryRun is true when a schedule's work order has the dry_run input "true" (its watch is in dry run): its action
	// log entries are recorded as dry-run entries. Other runs cannot set it.
	DryRun bool `json:"dry_run"`
	// Kind is the run's kind; a chat turn ("chat") may only read state and propose changes, never write
	Kind string `json:"kind"`
	// ChatAgentID is set for an agent-chat turn, which has no crew: it may only propose and read through the
	// connections frozen into its spec (docs/agent-chat-handoffs-design.md, section 4)
	ChatAgentID string `json:"-"`
	// Memory is the memory frozen into the run's spec; nil when memory is off for the run
	Memory *model.MemorySpec `json:"-"`
	// Escalation is the escalation frozen into the run's spec; nil when the run may not ask a person
	Escalation *model.EscalationSpec `json:"-"`
}

// CreateRunToken makes the run's token for the run-state API (replacing any earlier one) and returns it.
// Only its hash is stored. Tokens long past their expiry are swept at the same time.
func (s *Store) CreateRunToken(ctx context.Context, runID string, expiresAt time.Time) (string, error) {
	token := RunTokenPrefix + auth.RandomToken(32)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO run_tokens (run_id, token_hash, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (run_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at`,
		runID, auth.HashToken(token), expiresAt); err != nil {
		return "", mapErr(err, "run token", "")
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM run_tokens WHERE expires_at < $1`, time.Now().Add(-expiredTokenRetention)); err != nil {
		return "", fmt.Errorf("sweep run tokens: %w", err)
	}
	return token, nil
}

// ExpireRunToken brings a run's token expiry forward to at (it never extends it).
func (s *Store) ExpireRunToken(ctx context.Context, runID string, at time.Time) error {
	if _, err := s.pool.Exec(ctx, `UPDATE run_tokens SET expires_at = LEAST(expires_at, $2) WHERE run_id = $1`, runID, at); err != nil {
		return fmt.Errorf("expire run token: %w", err)
	}
	return nil
}

// RunTokenScope resolves a run token. Unknown and expired tokens are ErrNotFound.
func (s *Store) RunTokenScope(ctx context.Context, token string) (RunScope, error) {
	var sc RunScope
	var crewID *string
	var memory, escalation, chatConns []byte
	err := s.pool.QueryRow(ctx,
		`SELECT r.id, r.crew_id, COALESCE(c.name, r.crew_name), sch.id, COALESCE(sch.name, ''), r.area_ids::text[], t.expires_at,
		   COALESCE(wo.source = 'schedule', false), COALESCE(wo.source = 'schedule' AND r.inputs->>'dry_run' = 'true', false), r.kind,
		   r.spec->'memory', r.spec->'escalation', COALESCE(r.chat_agent_id::text, ''), r.spec->'chat'->'connections'
		 FROM run_tokens t
		 JOIN runs r ON r.id = t.run_id
		 LEFT JOIN crews c ON c.id = r.crew_id
		 LEFT JOIN work_orders wo ON wo.id = r.work_order_id
		 LEFT JOIN schedules sch ON wo.source = 'schedule' AND sch.id = wo.source_id
		 WHERE t.token_hash = $1 AND t.expires_at > now()`, auth.HashToken(token)).
		Scan(&sc.RunID, &crewID, &sc.CrewName, &sc.ScheduleID, &sc.ScheduleName, &sc.AreaIDs, &sc.ExpiresAt, &sc.FromSchedule, &sc.DryRun, &sc.Kind, &memory, &escalation, &sc.ChatAgentID, &chatConns)
	if err != nil {
		return RunScope{}, mapErr(err, "run token", "")
	}
	if len(memory) > 0 && string(memory) != "null" {
		var m model.MemorySpec
		if err := json.Unmarshal(memory, &m); err != nil {
			return RunScope{}, fmt.Errorf("read run memory: %w", err)
		}
		sc.Memory = &m
	}
	if len(escalation) > 0 && string(escalation) != "null" {
		var e model.EscalationSpec
		if err := json.Unmarshal(escalation, &e); err != nil {
			return RunScope{}, fmt.Errorf("read run escalation: %w", err)
		}
		sc.Escalation = &e
	}
	sc.AreaIDs = orEmpty(sc.AreaIDs)
	sc.Connections = []RunConnection{}
	switch {
	case crewID != nil:
		sc.CrewID = *crewID
		sc.Connections, err = s.runConnections(ctx,
			`SELECT x.id, x.name, x.kind FROM crew_connections cc JOIN connections x ON x.id = cc.connection_id
			 WHERE cc.crew_id = $1 AND x.enabled AND x.area_ids && $2::uuid[] ORDER BY x.name`, sc.CrewID, sc.AreaIDs)
	case sc.ChatAgentID != "":
		sc.Connections, err = s.frozenChatConnections(ctx, chatConns)
	}
	if err != nil {
		return RunScope{}, err
	}
	return sc, nil
}

// frozenChatConnections are the connections an agent-chat turn froze into its spec (the agent's, as the person could
// see them when it started), kept only while they are still enabled.
func (s *Store) frozenChatConnections(ctx context.Context, raw []byte) ([]RunConnection, error) {
	var frozen []RunConnection
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &frozen); err != nil {
			return nil, fmt.Errorf("read run chat connections: %w", err)
		}
	}
	ids := make([]string, 0, len(frozen))
	for _, c := range frozen {
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		return []RunConnection{}, nil
	}
	return s.runConnections(ctx, `SELECT x.id, x.name, x.kind FROM connections x
		WHERE x.id::text = ANY($1::text[]) AND x.enabled ORDER BY array_position($1::text[], x.id::text)`, ids)
}

// runConnections reads the connections a query lists.
func (s *Store) runConnections(ctx context.Context, sql string, args ...any) ([]RunConnection, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list run connections: %w", err)
	}
	defer rows.Close()
	out := []RunConnection{}
	for rows.Next() {
		var c RunConnection
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind); err != nil {
			return nil, fmt.Errorf("read run connections: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
