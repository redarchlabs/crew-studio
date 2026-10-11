// Package pmo sets up the ready-made PMO crew (docs/crew-state-and-pmo-design.md, section 12) in one step: it copies
// the PMO template (seeded by migration 0016, editable like any agent, task or crew) into the chosen functional
// areas, links the tracker and report connections, turns chat on with the coordinator as front desk, and adds two
// schedules with watches: a weekday follow-up sweep with the default follow-up ladder, and a Monday weekly report
// sent to the chosen destinations. Both start in dry run.
package pmo

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// The template's fixed ids (migration 0016).
const (
	TemplateCrewID        = "44444444-0000-4000-8000-000000000201"
	TemplateCoordinatorID = "22222222-0000-4000-8000-000000000201"
	TemplateReporterID    = "22222222-0000-4000-8000-000000000202"
	TemplateSweepTaskID   = "33333333-0000-4000-8000-000000000201"
	TemplateReportTaskID  = "33333333-0000-4000-8000-000000000202"
)

// Schedule times: weekdays at 07:00 and Mondays at 08:00 in the chosen time zone.
const (
	SweepCron  = "0 7 * * 1-5"
	ReportCron = "0 8 * * 1"
	// chatBudgetUSD is what one conversation with the coordinator may spend a day
	chatBudgetUSD  = 1.0
	maxPMONameLen  = 80
	defaultPMOName = "Team PMO"
)

// Template is the PMO template as it is in the library now.
type Template struct {
	Crew        model.Crew
	Coordinator model.Agent
	Reporter    model.Agent
	Sweep       model.Task
	Report      model.Task
}

// Request is what the setup wizard sends.
type Request struct {
	// Name names the crew; its agents, tasks and schedules are named after it
	Name                string                         `json:"name"`
	AreaIDs             []string                       `json:"area_ids"`
	TrackerConnectionID string                         `json:"tracker_connection_id"`
	Timezone            string                         `json:"timezone"`
	CoordinatorLLMID    *string                        `json:"coordinator_llm_id"`
	ReporterLLMID       *string                        `json:"reporter_llm_id"`
	Destinations        []model.ReportDestinationInput `json:"destinations"`
	// Enabled turns the schedules on (default true; they start in dry run either way)
	Enabled *bool `json:"enabled"`
}

// Normalize checks the parts of a request that need no database and returns a cleaned copy.
func Normalize(in Request) (Request, error) {
	out := in
	out.Name = strings.Join(strings.Fields(in.Name), " ")
	if out.Name == "" {
		out.Name = defaultPMOName
	}
	out.Timezone = strings.TrimSpace(in.Timezone)
	out.TrackerConnectionID = strings.TrimSpace(in.TrackerConnectionID)
	var problems []string
	if len([]rune(out.Name)) > maxPMONameLen {
		problems = append(problems, fmt.Sprintf("name: must be at most %d characters", maxPMONameLen))
	}
	out.AreaIDs = uniqueIDs(in.AreaIDs)
	if len(out.AreaIDs) == 0 {
		problems = append(problems, "area_ids: pick at least one functional area")
	}
	for _, id := range out.AreaIDs {
		if !model.IsUUID(id) {
			problems = append(problems, fmt.Sprintf("area_ids: %q is not an id", id))
		}
	}
	switch {
	case out.TrackerConnectionID == "":
		problems = append(problems, "tracker_connection_id: pick the tracker connection (Jira, Linear or KM2 issues) the PMO follows")
	case !model.IsUUID(out.TrackerConnectionID):
		problems = append(problems, "tracker_connection_id: is not a connection id")
	}
	for field, id := range map[string]*string{"coordinator_llm_id": in.CoordinatorLLMID, "reporter_llm_id": in.ReporterLLMID} {
		if id != nil && !model.IsUUID(*id) {
			problems = append(problems, field+": is not an LLM id")
		}
	}
	if _, err := time.LoadLocation(out.Timezone); err != nil || out.Timezone == "" || out.Timezone == "Local" {
		problems = append(problems, fmt.Sprintf("timezone: %q is not a known time zone (use names like America/Denver)", out.Timezone))
	}
	dests, err := model.NormalizeDestinations(in.Destinations)
	if err != nil {
		problems = append(problems, err.(*model.ValidationError).Details...)
	}
	out.Destinations = dests
	if out.Enabled == nil {
		on := true
		out.Enabled = &on
	}
	if len(problems) > 0 {
		return out, &model.ValidationError{Details: problems}
	}
	return out, nil
}

// Names are what an install calls everything, so a team can have several PMOs side by side.
type Names struct {
	Crew, Coordinator, Reporter, SweepTask, ReportTask, SweepSchedule, ReportSchedule string
}

// NamesFor names an install.
func NamesFor(name string) Names {
	return Names{Crew: name, Coordinator: name + " Coordinator", Reporter: name + " Status Reporter",
		SweepTask: name + ": daily follow-up sweep", ReportTask: name + ": weekly status report",
		SweepSchedule: name + ": daily sweep", ReportSchedule: name + ": weekly report"}
}

// Ladder is the default follow-up ladder (design section 6): a heads-up to the owner three days before the due date
// and on the day, then the lead two days after, and weekly after that; nothing on weekends.
func Ladder(timezone string) *model.FollowupLadder {
	return &model.FollowupLadder{
		DateField: "due_date",
		SkipWhen:  map[string][]string{"status_category": {"done", "canceled"}},
		Steps: []model.FollowupStep{
			{Name: "due_soon", Offset: "-3d", Notify: "owner"},
			{Name: "due_today", Offset: "0d", Notify: "owner"},
			{Name: "overdue_escalate", Offset: "+2d", Notify: "lead"},
			{Name: "overdue_weekly", Offset: "+9d", Notify: "lead", Repeat: "7d"},
		},
		QuietDays: []string{"Sat", "Sun"},
		Timezone:  timezone,
	}
}

// trackedFields are what both watches compare on each tracked issue.
var trackedFields = []string{"status", "status_category", "assignee", "due_date", "priority"}

// Watches are the two watches: the sweep runs when something changed or a follow-up is due, the report every week.
func Watches(timezone string) (sweep, report model.WatchInput, err error) {
	dry := true
	source := model.WatchSource{Kind: model.WatchSourceTrackingList, Comments: true, StaleDays: model.DefaultStaleDays}
	sweep, err = model.NormalizeWatch(model.WatchInput{Source: source, IDField: model.TrackerIDField, TrackedFields: trackedFields,
		RunWhen: model.WatchRunChangedOrFollowups, Followups: Ladder(timezone), DryRun: &dry})
	if err != nil {
		return sweep, report, err
	}
	report, err = model.NormalizeWatch(model.WatchInput{Source: source, IDField: model.TrackerIDField, TrackedFields: trackedFields,
		RunWhen: model.WatchRunAlways, DryRun: &dry})
	return sweep, report, err
}

// copyAgent is a template agent renamed for an install, on the chosen LLM.
func copyAgent(a model.Agent, name string, llmID *string) model.Agent {
	out := a
	out.ID, out.Name, out.AreaIDs = "", name, nil
	out.Tools = append([]string(nil), a.Tools...)
	if llmID != nil {
		id := *llmID
		out.LLMID = &id
	}
	return out
}

func copyTask(t model.Task, name string) model.Task {
	out := t
	out.ID, out.Name, out.AreaIDs, out.ContextTaskIDs = "", name, nil, nil
	return out
}

// Plan builds the install from a checked request and the template. connectionIDs are the connections the crew may
// use (the tracker and every destination's).
func Plan(req Request, tmpl Template, connectionIDs []string, sweepNext, reportNext *time.Time, userID string) (store.PMOInstall, error) {
	names := NamesFor(req.Name)
	sweepWatch, reportWatch, err := Watches(req.Timezone)
	if err != nil {
		return store.PMOInstall{}, err
	}
	crew := tmpl.Crew
	crew.ID, crew.Name, crew.AreaIDs = "", names.Crew, nil
	crew.Description = fmt.Sprintf("Project management office made from the PMO template: the coordinator follows up on due and "+
		"overdue work in the tracker and answers chat; the status reporter writes and delivers the weekly report per project. "+
		"Schedules: %s and %s (%s).", names.SweepSchedule, names.ReportSchedule, req.Timezone)
	crew.Inputs = []model.CrewInput{}
	enabled := req.Enabled == nil || *req.Enabled
	schedule := func(name, cron, description string) model.Schedule {
		return model.Schedule{Name: name, Cron: cron, Timezone: req.Timezone, Enabled: enabled, OverlapPolicy: model.OverlapSkip,
			Description: description}
	}
	if !enabled {
		sweepNext, reportNext = nil, nil
	}
	return store.PMOInstall{
		AreaIDs: req.AreaIDs,
		Agents: []store.PMOAgent{
			{Agent: copyAgent(tmpl.Coordinator, names.Coordinator, req.CoordinatorLLMID), Task: copyTask(tmpl.Sweep, names.SweepTask)},
			{Agent: copyAgent(tmpl.Reporter, names.Reporter, req.ReporterLLMID), Task: copyTask(tmpl.Report, names.ReportTask)},
		},
		Crew: crew, ConnectionIDs: connectionIDs, ChatBudgetUSD: chatBudgetUSD, UserID: userID,
		Schedules: []store.PMOSchedule{
			{Schedule: schedule(names.SweepSchedule, SweepCron, "Weekdays at 07:00: follow-ups and suggestions for the tracking list. "+
				"Starts in dry run; turn dry run off on the Watch panel when the would-post output looks right."),
				NextRunAt: sweepNext, TaskIndex: 0, Watch: sweepWatch},
			{Schedule: schedule(names.ReportSchedule, ReportCron, "Mondays at 08:00: the weekly status report per project, sent to "+
				"the report destinations below. Starts in dry run."),
				NextRunAt: reportNext, TaskIndex: 1, Watch: reportWatch, Destinations: req.Destinations},
		},
	}, nil
}

// uniqueIDs trims ids and drops blanks and repeats, keeping the order.
func uniqueIDs(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
