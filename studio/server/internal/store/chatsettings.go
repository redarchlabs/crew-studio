package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const chatSettingsColumns = `cs.crew_id, cs.enabled, cs.front_desk_agent_id, cs.llm_id, cs.daily_budget_usd,
	cs.person_daily_budget_usd, COALESCE(u.name, ''), cs.updated_at`

func scanChatSettings(row pgx.Row) (model.ChatSettings, error) {
	var c model.ChatSettings
	err := row.Scan(&c.CrewID, &c.Enabled, &c.FrontDeskAgentID, &c.LLMID, &c.DailyBudgetUSD, &c.PersonDailyBudgetUSD, &c.UpdatedByName, &c.UpdatedAt)
	return c, err
}

// ChatSettings returns a crew's chat settings; a crew nobody set up has chat off with the default budget.
func (s *Store) ChatSettings(ctx context.Context, crewID string) (model.ChatSettings, error) {
	c, err := scanChatSettings(s.pool.QueryRow(ctx,
		`SELECT `+chatSettingsColumns+` FROM crew_chat cs LEFT JOIN users u ON u.id = cs.updated_by WHERE cs.crew_id = $1`, crewID))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DefaultChatSettings(crewID), nil
	}
	if err != nil {
		return c, fmt.Errorf("read chat settings: %w", err)
	}
	return c, nil
}

// PutChatSettings saves a crew's chat settings (already validated) as changed by userID, who is shown with them.
func (s *Store) PutChatSettings(ctx context.Context, in model.ChatSettings, userID string) (model.ChatSettings, error) {
	var out model.ChatSettings
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO crew_chat (crew_id, enabled, front_desk_agent_id, llm_id, daily_budget_usd, person_daily_budget_usd, updated_by, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
			 ON CONFLICT (crew_id) DO UPDATE SET enabled = EXCLUDED.enabled, front_desk_agent_id = EXCLUDED.front_desk_agent_id,
			   llm_id = EXCLUDED.llm_id, daily_budget_usd = EXCLUDED.daily_budget_usd,
			   person_daily_budget_usd = EXCLUDED.person_daily_budget_usd, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			in.CrewID, in.Enabled, in.FrontDeskAgentID, in.LLMID, in.DailyBudgetUSD, personBudget(in.PersonDailyBudgetUSD), nullableUUID(userID)); err != nil {
			return mapErr(err, "chat setting", "")
		}
		var err error
		out, err = scanChatSettings(tx.QueryRow(ctx,
			`SELECT `+chatSettingsColumns+` FROM crew_chat cs LEFT JOIN users u ON u.id = cs.updated_by WHERE cs.crew_id = $1`, in.CrewID))
		if err != nil {
			return fmt.Errorf("read chat settings: %w", err)
		}
		return nil
	})
	return out, err
}

// ChatCrews lists the crews in scope that people can chat with: chat on and a front desk agent still in the crew.
func (s *Store) ChatCrews(ctx context.Context, scope Scope) ([]model.ChatCrew, error) {
	all, areas := scope.areaArgs()
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.name, c.description, a.name, a.role
		 FROM crew_chat cs
		 JOIN crews c ON c.id = cs.crew_id
		 JOIN crew_agents ca ON ca.crew_id = c.id AND ca.agent_id = cs.front_desk_agent_id
		 JOIN agents a ON a.id = ca.agent_id
		 WHERE cs.enabled AND `+areaFilter("crew_areas", "crew_id", "c.id")+`
		 ORDER BY c.name`, all, areas)
	if err != nil {
		return nil, fmt.Errorf("list chat crews: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ChatCrew, error) {
		var c model.ChatCrew
		err := r.Scan(&c.ID, &c.Name, &c.Description, &c.FrontDeskName, &c.FrontDeskRole)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("read chat crews: %w", err)
	}
	return orEmpty(items), nil
}

// personBudget is the person cap to store: the default when the caller left it out.
func personBudget(v float64) float64 {
	if v <= 0 {
		return model.DefaultChatPersonBudgetUSD
	}
	return v
}
