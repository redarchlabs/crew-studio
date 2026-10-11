package pmo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/scheduler"
	"crewstudio/internal/store"
)

// Needs are the permissions setting up a PMO takes: it creates agents, tasks, a crew and schedules, and turns chat on
// for the crew (which PUT /crews/{id}/chat guards with crews.edit).
var Needs = []string{auth.CrewsCreate, auth.CrewsEdit, auth.AgentsCreate, auth.TasksCreate, auth.SchedulesManage, auth.WorkOrdersCreate, auth.ConnectionsView}

// Service checks setup requests against the database and installs PMOs.
type Service struct {
	store *store.Store
}

// NewService builds the service.
func NewService(st *store.Store) *Service { return &Service{store: st} }

// ErrNoTemplate means someone deleted part of the PMO template.
var ErrNoTemplate = &store.ConflictError{Code: "pmo_template_missing",
	Message: "The PMO template (the PMO (template) crew, its two agents and two tasks) is missing; it is added by database migration 0016."}

// LoadTemplate reads the PMO template from the library.
func (s *Service) LoadTemplate(ctx context.Context) (Template, error) {
	var t Template
	var err error
	if t.Crew, err = s.store.GetCrew(ctx, TemplateCrewID); err == nil {
		if t.Coordinator, err = s.store.GetAgent(ctx, TemplateCoordinatorID); err == nil {
			if t.Reporter, err = s.store.GetAgent(ctx, TemplateReporterID); err == nil {
				if t.Sweep, err = s.store.GetTask(ctx, TemplateSweepTaskID); err == nil {
					t.Report, err = s.store.GetTask(ctx, TemplateReportTaskID)
				}
			}
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		return t, ErrNoTemplate
	}
	return t, err
}

// Choice is a connection or LLM offered in the wizard.
type Choice struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind,omitempty"`
	AreaIDs []string `json:"area_ids,omitempty"`
	// Warning says what is wrong with an LLM before it runs, such as its provider key missing
	Warning string `json:"warning,omitempty"`
}

// Options are what the wizard offers.
type Options struct {
	Template     Template `json:"-"`
	Trackers     []Choice `json:"trackers"`
	Destinations []Choice `json:"destinations"`
	LLMs         []Choice `json:"llms"`
	// Suggested LLMs are the template agents' own: a cheap, fast one for the coordinator and a stronger writer
	CoordinatorLLMID *string `json:"coordinator_llm_id"`
	ReporterLLMID    *string `json:"reporter_llm_id"`
	SweepCron        string  `json:"sweep_cron"`
	ReportCron       string  `json:"report_cron"`
	Ladder           any     `json:"ladder"`
}

// SetupOptions lists what p may pick.
func (s *Service) SetupOptions(ctx context.Context, p *auth.Principal) (Options, error) {
	tmpl, err := s.LoadTemplate(ctx)
	if err != nil {
		return Options{}, err
	}
	out := Options{Template: tmpl, Trackers: []Choice{}, Destinations: []Choice{}, LLMs: []Choice{}, SweepCron: SweepCron, ReportCron: ReportCron,
		CoordinatorLLMID: tmpl.Coordinator.LLMID, ReporterLLMID: tmpl.Reporter.LLMID, Ladder: Ladder("")}
	conns, err := s.store.ListConnections(ctx, store.Scope{All: p.AllAreas, AreaIDs: p.AreaIDs})
	if err != nil {
		return out, err
	}
	for _, c := range conns {
		choice := Choice{ID: c.ID, Name: c.Name, Kind: c.Kind, AreaIDs: c.AreaIDs}
		switch {
		case !c.Enabled:
		case model.IsTrackerKind(c.Kind):
			out.Trackers = append(out.Trackers, choice)
		case slices.Contains(model.DestinationKinds, c.Kind):
			out.Destinations = append(out.Destinations, choice)
		}
	}
	llms, err := s.store.ListLLMs(ctx)
	if err != nil {
		return out, err
	}
	for _, l := range llms {
		out.LLMs = append(out.LLMs, Choice{ID: l.ID, Name: l.Name, Kind: l.Model})
	}
	return out, nil
}

// Prepare checks a normalized request against what p may use and builds the install.
func (s *Service) Prepare(ctx context.Context, p *auth.Principal, req Request, now time.Time) (store.PMOInstall, error) {
	tmpl, err := s.LoadTemplate(ctx)
	if err != nil {
		return store.PMOInstall{}, err
	}
	var problems []string
	for _, area := range req.AreaIDs {
		if !p.HasArea(area) {
			problems = append(problems, "area_ids: "+area+" is not one of your functional areas")
		}
	}
	connIDs, connProblems, err := s.checkConnections(ctx, p, req)
	if err != nil {
		return store.PMOInstall{}, err
	}
	problems = append(problems, connProblems...)
	llmProblems, err := s.checkLLMs(ctx, &req, tmpl)
	if err != nil {
		return store.PMOInstall{}, err
	}
	problems = append(problems, llmProblems...)
	if len(problems) > 0 {
		return store.PMOInstall{}, &model.ValidationError{Details: problems}
	}
	sweepNext, err := scheduler.Next(SweepCron, req.Timezone, now)
	if err != nil {
		return store.PMOInstall{}, &model.ValidationError{Details: []string{err.Error()}}
	}
	reportNext, err := scheduler.Next(ReportCron, req.Timezone, now)
	if err != nil {
		return store.PMOInstall{}, &model.ValidationError{Details: []string{err.Error()}}
	}
	return Plan(req, tmpl, connIDs, sweepNext, reportNext, p.UserID)
}

// checkConnections checks the tracker and every destination: a connection p can see, enabled, of the right kind
// and in one of the chosen areas. It returns the ids the crew will be allowed to use.
func (s *Service) checkConnections(ctx context.Context, p *auth.Principal, req Request) ([]string, []string, error) {
	var ids, problems []string
	check := func(field, id string, kindOK func(string) bool, want string) error {
		c, err := s.store.GetConnection(ctx, id)
		switch {
		case errors.Is(err, store.ErrNotFound) || (err == nil && !p.SeesAreas(c.AreaIDs)):
			problems = append(problems, field+": no such connection")
		case err != nil:
			return err
		case !kindOK(c.Kind):
			problems = append(problems, fmt.Sprintf("%s: %s is a %s connection, not %s", field, c.Name, c.Kind, want))
		case !c.Enabled:
			problems = append(problems, fmt.Sprintf("%s: %s is turned off", field, c.Name))
		case !overlaps(c.AreaIDs, req.AreaIDs):
			problems = append(problems, fmt.Sprintf("%s: %s is not in any of the chosen functional areas", field, c.Name))
		case !slices.Contains(ids, id):
			ids = append(ids, id)
		}
		return nil
	}
	if err := check("tracker_connection_id", req.TrackerConnectionID, model.IsTrackerKind, "a tracker (Jira, Linear or KM2 issues)"); err != nil {
		return nil, nil, err
	}
	for i, d := range req.Destinations {
		kind := d.Kind
		if err := check(fmt.Sprintf("destinations[%d].connection_id", i), d.ConnectionID, func(k string) bool { return k == kind }, kind); err != nil {
			return nil, nil, err
		}
		if c, err := s.store.GetConnection(ctx, d.ConnectionID); err == nil && kind == model.DestinationEmail &&
			len(model.RecipientsOutside(c.Config, d.Recipients)) > 0 {
			problems = append(problems, fmt.Sprintf("destinations[%d].recipients: %s may only send to %s", i, c.Name, strings.Join(model.EmailDomains(c.Config), ", ")))
		}
	}
	return ids, problems, nil
}

// checkLLMs fills in the template agents' LLMs when none were chosen and checks the chosen ones exist.
func (s *Service) checkLLMs(ctx context.Context, req *Request, tmpl Template) ([]string, error) {
	if req.CoordinatorLLMID == nil {
		req.CoordinatorLLMID = tmpl.Coordinator.LLMID
	}
	if req.ReporterLLMID == nil {
		req.ReporterLLMID = tmpl.Reporter.LLMID
	}
	var problems []string
	for field, id := range map[string]*string{"coordinator_llm_id": req.CoordinatorLLMID, "reporter_llm_id": req.ReporterLLMID} {
		if id == nil {
			problems = append(problems, field+": pick an LLM")
			continue
		}
		if _, err := s.store.GetLLM(ctx, *id); errors.Is(err, store.ErrNotFound) {
			problems = append(problems, field+": no such LLM")
		} else if err != nil {
			return nil, err
		}
	}
	slices.Sort(problems)
	return problems, nil
}

func overlaps(a, b []string) bool {
	return slices.ContainsFunc(a, func(x string) bool { return slices.Contains(b, x) })
}

// Install creates the PMO.
func (s *Service) Install(ctx context.Context, plan store.PMOInstall) (store.PMOInstalled, error) {
	return s.store.InstallPMO(ctx, plan)
}
