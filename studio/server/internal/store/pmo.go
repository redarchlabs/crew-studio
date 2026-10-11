package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// Installing a PMO (design section 12) creates its agents, tasks and crew, the crew's connections and chat, and two
// schedules with watches and report destinations, all in one transaction: a problem anywhere leaves nothing behind.

// PMOAgent is one agent to create, with the task it runs.
type PMOAgent struct {
	Agent model.Agent
	Task  model.Task
}

// PMOSchedule is a schedule to create for the PMO crew, with its watch and report destinations. TaskIndex is the
// index in PMOInstall.Agents of the job it runs (its task becomes the schedule's run_tasks input).
type PMOSchedule struct {
	Schedule     model.Schedule
	NextRunAt    *time.Time
	TaskIndex    int
	Watch        model.WatchInput
	Destinations []model.ReportDestinationInput
}

// PMOInstall is everything a PMO install creates; inputs are already validated.
type PMOInstall struct {
	AreaIDs []string
	// Agents with their tasks, in crew order; the first is the chat front desk
	Agents        []PMOAgent
	Crew          model.Crew
	ConnectionIDs []string
	ChatBudgetUSD float64
	Schedules     []PMOSchedule
	// UserID is who installs it (chat settings show them)
	UserID string
}

// PMOInstalled is what an install made.
type PMOInstalled struct {
	Crew      model.Crew       `json:"crew"`
	Agents    []model.Agent    `json:"agents"`
	Tasks     []model.Task     `json:"tasks"`
	Schedules []model.Schedule `json:"schedules"`
}

// InstallPMO creates a PMO in one transaction.
func (s *Store) InstallPMO(ctx context.Context, in PMOInstall) (PMOInstalled, error) {
	var out PMOInstalled
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		if out.Agents, out.Tasks, err = installJobs(ctx, tx, in); err != nil {
			return err
		}
		if out.Crew, err = installCrew(ctx, tx, in, out.Agents, out.Tasks); err != nil {
			return err
		}
		for _, sc := range in.Schedules {
			created, err := installSchedule(ctx, tx, out.Crew.ID, out.Tasks[sc.TaskIndex].ID, sc)
			if err != nil {
				return err
			}
			out.Schedules = append(out.Schedules, created)
		}
		return nil
	})
	return out, err
}

func installJobs(ctx context.Context, tx pgx.Tx, in PMOInstall) ([]model.Agent, []model.Task, error) {
	agents := make([]model.Agent, 0, len(in.Agents))
	tasks := make([]model.Task, 0, len(in.Agents))
	for _, job := range in.Agents {
		a := job.Agent
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO agents (name, role, goal, backstory, llm_id, tools, max_iter, allow_delegation, verbose_output)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			a.Name, a.Role, a.Goal, a.Backstory, a.LLMID, a.Tools, a.MaxIter, a.AllowDelegation, a.Verbose).Scan(&id)
		if err != nil {
			return nil, nil, mapErr(err, "agent", a.Name)
		}
		agent, err := withAreas(ctx, tx, "agent", id, in.AreaIDs, getAgent)
		if err != nil {
			return nil, nil, err
		}
		t := job.Task
		if err := tx.QueryRow(ctx, `INSERT INTO tasks (name, description, expected_output, agent_id, markdown) VALUES ($1, $2, $3, $4, $5)
			RETURNING id`, t.Name, t.Description, t.ExpectedOutput, agent.ID, t.Markdown).Scan(&id); err != nil {
			return nil, nil, mapErr(err, "task", t.Name)
		}
		task, err := withAreas(ctx, tx, "task", id, in.AreaIDs, getTask)
		if err != nil {
			return nil, nil, err
		}
		agents, tasks = append(agents, agent), append(tasks, task)
	}
	return agents, tasks, nil
}

// withAreas puts a new agent, task or crew in its areas, records its revision and reads it back.
func withAreas[T any](ctx context.Context, tx pgx.Tx, entity, id string, areaIDs []string, get func(context.Context, queryer, string) (T, error)) (T, error) {
	var zero T
	if err := replaceAreas(ctx, tx, entity+"_areas", entity+"_id", id, areaIDs); err != nil {
		return zero, err
	}
	out, err := get(ctx, tx, id)
	if err != nil {
		return zero, err
	}
	return out, recordAreaRevision(ctx, tx, entity, id, "create", out, areaIDs)
}

func installCrew(ctx context.Context, tx pgx.Tx, in PMOInstall, agents []model.Agent, tasks []model.Task) (model.Crew, error) {
	c := in.Crew
	inputs, err := encodeInputs(c.Inputs)
	if err != nil {
		return model.Crew{}, err
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO crews (name, description, process, manager_llm_id, inputs, budget_usd)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, c.Name, c.Description, c.Process, c.ManagerLLMID, inputs, c.BudgetUSD).Scan(&id); err != nil {
		return model.Crew{}, mapErr(err, "crew", c.Name)
	}
	agentIDs, taskIDs := make([]string, len(agents)), make([]string, len(tasks))
	for i := range agents {
		agentIDs[i], taskIDs[i] = agents[i].ID, tasks[i].ID
	}
	if err := replaceCrewMembers(ctx, tx, id, agentIDs, taskIDs); err != nil {
		return model.Crew{}, err
	}
	crew, err := withAreas(ctx, tx, "crew", id, in.AreaIDs, getCrew)
	if err != nil {
		return model.Crew{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO crew_connections (crew_id, connection_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`,
		id, orEmpty(in.ConnectionIDs)); err != nil {
		return model.Crew{}, mapErr(err, "crew connection", "")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO crew_chat (crew_id, enabled, front_desk_agent_id, daily_budget_usd, updated_by)
		VALUES ($1, true, $2, $3, $4)`, id, agentIDs[0], in.ChatBudgetUSD, nullableUUID(in.UserID)); err != nil {
		return model.Crew{}, mapErr(err, "chat setting", "")
	}
	return crew, nil
}

func installSchedule(ctx context.Context, tx pgx.Tx, crewID, taskID string, in PMOSchedule) (model.Schedule, error) {
	sc := in.Schedule
	sc.Inputs = map[string]string{model.InputRunTasks: taskID}
	inputs, err := json.Marshal(sc.Inputs)
	if err != nil {
		return model.Schedule{}, fmt.Errorf("encode inputs: %w", err)
	}
	actor := actorFrom(ctx)
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO schedules (name, description, crew_id, cron, timezone, brief, inputs, budget_usd,
		enabled, overlap_policy, run_as, run_as_name, next_run_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
		sc.Name, sc.Description, crewID, sc.Cron, sc.Timezone, sc.Brief, inputs, sc.BudgetUSD, sc.Enabled, sc.OverlapPolicy,
		nullableUUID(actor.ID), actor.Name, in.NextRunAt).Scan(&id); err != nil {
		return model.Schedule{}, mapErr(err, "schedule", sc.Name)
	}
	out, err := getSchedule(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if err := recordAreaRevision(ctx, tx, "schedule", id, "create", out, out.AreaIDs); err != nil {
		return out, err
	}
	if err := insertWatch(ctx, tx, id, in.Watch); err != nil {
		return out, err
	}
	return out, setReportDestinations(ctx, tx, id, in.Destinations)
}

func insertWatch(ctx context.Context, tx pgx.Tx, scheduleID string, in model.WatchInput) error {
	source, err := json.Marshal(in.Source)
	if err != nil {
		return fmt.Errorf("encode watch source: %w", err)
	}
	var followups []byte
	if in.Followups != nil {
		if followups, err = json.Marshal(in.Followups); err != nil {
			return fmt.Errorf("encode watch followups: %w", err)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO watches (schedule_id, connection_id, source, id_field, tracked_fields, run_when, followups, dry_run,
		max_items, created_by, mention_cap_per_day, comment_cap_per_run) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		scheduleID, in.ConnectionID, source, in.IDField, orEmpty(in.TrackedFields), in.RunWhen, followups, in.DryRun == nil || *in.DryRun,
		in.MaxItems, nullableUUID(actorFrom(ctx).ID), capOr(in.MentionCapPerDay, model.DefaultMentionCapPerDay),
		capOr(in.CommentCapPerRun, model.DefaultCommentCapPerRun))
	return mapErr(err, "watch", "")
}
