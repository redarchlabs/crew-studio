package pmo

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

const finance = "55555555-0000-4000-8000-000000000001"

func TestNormalize(t *testing.T) {
	conn := "bbbbbbbb-0000-4000-8000-000000000009"
	out, err := Normalize(Request{Name: "  Atlas   PMO ", AreaIDs: []string{finance, " " + finance}, TrackerConnectionID: " " + conn + " ", Timezone: "America/Denver"})
	if err != nil || out.Name != "Atlas PMO" || out.TrackerConnectionID != conn || len(out.AreaIDs) != 1 || out.Enabled == nil || !*out.Enabled || len(out.Destinations) != 0 {
		t.Fatalf("out = %+v, %v", out, err)
	}
	if out, _ := Normalize(Request{AreaIDs: []string{finance}, TrackerConnectionID: conn, Timezone: "UTC"}); out.Name != "Team PMO" {
		t.Errorf("default name = %q", out.Name)
	}
	_, err = Normalize(Request{Name: strings.Repeat("x", 81), Timezone: "Nowhere/Land",
		Destinations: []model.ReportDestinationInput{{Kind: "fax"}}})
	details := strings.Join(err.(*model.ValidationError).Details, "\n")
	for _, want := range []string{"name: must be at most 80", "area_ids", "tracker_connection_id", "timezone", "destinations[0].kind"} {
		if !strings.Contains(details, want) {
			t.Errorf("missing %q:\n%s", want, details)
		}
	}
}

func TestLadderWatchesAndPlan(t *testing.T) {
	l := Ladder("Europe/London")
	if len(l.Steps) != 4 || l.Steps[0].Offset != "-3d" || l.Steps[1].Offset != "0d" || l.Steps[2].Offset != "+2d" || l.Steps[3].Offset != "+9d" ||
		l.Steps[3].Repeat != "7d" || l.Steps[0].Notify != "owner" || l.Steps[3].Notify != "lead" || !slices.Equal(l.QuietDays, []string{"Sat", "Sun"}) {
		t.Errorf("ladder = %+v", l)
	}
	sweep, report, err := Watches("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	if sweep.RunWhen != model.WatchRunChangedOrFollowups || report.RunWhen != model.WatchRunAlways || !*sweep.DryRun || !*report.DryRun ||
		!sweep.Source.Comments || sweep.Followups == nil || report.Followups != nil {
		t.Errorf("watches = %+v / %+v", sweep, report)
	}
	if _, _, err := Watches("Nowhere/Land"); err == nil {
		t.Error("bad time zone accepted")
	}

	llm := "11111111-0000-4000-8000-000000000003"
	tmpl := Template{
		Crew:        model.Crew{ID: TemplateCrewID, Name: "PMO (template)", Process: model.ProcessSequential, BudgetUSD: 2},
		Coordinator: model.Agent{ID: TemplateCoordinatorID, Name: "PMO Coordinator", Tools: []string{"tracker_comment"}},
		Reporter:    model.Agent{ID: TemplateReporterID, Name: "Status Reporter", Tools: []string{"deliver_report"}},
		Sweep:       model.Task{ID: TemplateSweepTaskID, Name: "sweep", ContextTaskIDs: []string{"x"}},
		Report:      model.Task{ID: TemplateReportTaskID, Name: "report"},
	}
	off := false
	now := time.Now()
	plan, err := Plan(Request{Name: "Atlas", AreaIDs: []string{finance}, Timezone: "UTC", CoordinatorLLMID: &llm, Enabled: &off}, tmpl, []string{"c"}, &now, &now, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Crew.ID != "" || plan.Crew.Name != "Atlas" || plan.Agents[0].Agent.Name != "Atlas Coordinator" || *plan.Agents[0].Agent.LLMID != llm ||
		plan.Agents[1].Agent.LLMID != nil || plan.Agents[0].Task.Name != "Atlas: daily follow-up sweep" || plan.Agents[0].Task.ContextTaskIDs != nil {
		t.Errorf("plan = %+v", plan)
	}
	if plan.Schedules[0].Schedule.Enabled || plan.Schedules[0].NextRunAt != nil || plan.Schedules[1].TaskIndex != 1 {
		t.Errorf("disabled schedules = %+v", plan.Schedules)
	}
	// The template is copied, never changed
	plan.Agents[0].Agent.Tools[0] = "changed"
	if tmpl.Coordinator.Tools[0] != "tracker_comment" {
		t.Error("the template's tools were shared")
	}
	if _, err := Plan(Request{Timezone: "Nowhere/Land"}, tmpl, nil, nil, nil, ""); err == nil {
		t.Error("bad time zone planned")
	}
}

func TestServiceAgainstTheDatabase(t *testing.T) {
	st := store.New(testdb.New(t, "pmo_test"))
	svc := NewService(st)
	ctx := store.WithActor(context.Background(), store.Actor{Name: "test"})
	tmpl, err := svc.LoadTemplate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The migration picks the coordinator's model from the llms table, preferring a fast, cheap one, and a stronger
	// writer for the reporter
	coord, _ := st.GetLLM(ctx, *tmpl.Coordinator.LLMID)
	reporter, _ := st.GetLLM(ctx, *tmpl.Reporter.LLMID)
	if coord.Model != "anthropic/claude-haiku-4-5" || reporter.Model != "anthropic/claude-sonnet-5-5" {
		t.Errorf("template models = %s / %s", coord.Model, reporter.Model)
	}
	if !slices.Equal(tmpl.Crew.TaskIDs, []string{TemplateSweepTaskID, TemplateReportTaskID}) || slices.Contains(tmpl.Coordinator.Tools, "tracker_create_issue") {
		t.Errorf("template = %+v", tmpl)
	}

	p := &auth.Principal{UserID: "", AllAreas: true, AreaIDs: []string{finance}, Permissions: map[string]bool{}}
	opts, err := svc.SetupOptions(ctx, p)
	if err != nil || len(opts.Trackers) != 0 || len(opts.LLMs) == 0 {
		t.Fatalf("options = %+v %v", opts, err)
	}
	conn, err := st.CreateConnection(ctx, "bbbbbbbb-0000-4000-8000-000000000001", model.ConnectionInput{Name: "Jira", Kind: "jira",
		AreaIDs: []string{finance}, Config: map[string]string{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, err := Normalize(Request{Name: "Atlas", AreaIDs: []string{finance}, TrackerConnectionID: conn.ID, Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Prepare(ctx, p, req, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Install(ctx, plan)
	if err != nil || out.Crew.Name != "Atlas" || len(out.Schedules) != 2 || out.Schedules[0].NextRunAt == nil {
		t.Fatalf("installed = %+v %v", out, err)
	}
	limited := &auth.Principal{AreaIDs: []string{"55555555-0000-4000-8000-000000000002"}, Permissions: map[string]bool{}}
	if _, err := svc.Prepare(ctx, limited, req, time.Now()); err == nil || !strings.Contains(err.Error(), "not one of your functional areas") {
		t.Errorf("limited = %v", err)
	}
}
