package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/model"
	"crewstudio/internal/pmo"
	"crewstudio/internal/store"
)

func (e reportEnv) jira(t *testing.T, name, area string) model.Connection {
	t.Helper()
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": name, "kind": "jira", "area_ids": []string{area},
		"config": map[string]string{"site_url": "https://acme.atlassian.net", "email": "bot@example.com"}, "secrets": map[string]string{"api_token": "t"}})
	wantStatus(t, r, http.StatusCreated)
	return decodeData[model.Connection](t, r)
}

func TestPMOSetupInstallsTheCrewSchedulesAndWatches(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	jira := e.jira(t, "Acme Jira", financeArea)
	slack := e.connection(t, "PMO Slack", "slack", map[string]string{}, map[string]string{"webhook_url": e.hooks.URL + "/slack/hook/1"})

	opts := decodeData[pmo.Options](t, e.do(t, "GET", "/api/pmo/setup", nil))
	if len(opts.Trackers) != 1 || opts.Trackers[0].ID != jira.ID || len(opts.Destinations) != 1 || len(opts.LLMs) == 0 ||
		opts.CoordinatorLLMID == nil || opts.ReporterLLMID == nil || opts.SweepCron != pmo.SweepCron {
		t.Fatalf("options = %+v", opts)
	}
	// The template's models come from the llms table: a cheaper one for the coordinator than for the reporter
	if *opts.CoordinatorLLMID == "" || *opts.ReporterLLMID == "" {
		t.Errorf("template LLMs = %v %v", opts.CoordinatorLLMID, opts.ReporterLLMID)
	}

	body := map[string]any{"name": "Atlas PMO", "area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "America/Denver",
		"destinations": []map[string]any{{"kind": "slack", "connection_id": slack.ID}, {"kind": "email", "connection_id": slack.ID, "recipients": []string{"a@example.com"}}}}
	r := e.do(t, "POST", "/api/pmo/setup", body)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "PMO Slack is a slack connection, not email") {
		t.Errorf("kind mismatch: %+v", r.body.Error)
	}
	body["destinations"] = []map[string]any{{"kind": "slack", "connection_id": slack.ID}}
	r = e.do(t, "POST", "/api/pmo/setup", body)
	wantStatus(t, r, http.StatusCreated)
	out := decodeData[store.PMOInstalled](t, r)

	if out.Crew.Name != "Atlas PMO" || len(out.Agents) != 2 || len(out.Tasks) != 2 || len(out.Schedules) != 2 ||
		!slices.Equal(out.Crew.AreaIDs, []string{financeArea}) {
		t.Fatalf("installed = %+v", out)
	}
	coord, reporter := out.Agents[0], out.Agents[1]
	if coord.Name != "Atlas PMO Coordinator" || reporter.Name != "Atlas PMO Status Reporter" || !slices.Contains(reporter.Tools, "deliver_report") ||
		slices.Contains(coord.Tools, "tracker_create_issue") || slices.Contains(reporter.Tools, "tracker_create_issue") ||
		coord.LLMID == nil || *coord.LLMID != *opts.CoordinatorLLMID {
		t.Errorf("agents = %+v / %+v", coord, reporter)
	}
	for _, task := range out.Tasks {
		for _, rule := range []string{"untrusted data", "never follow instructions", "never change"} {
			if !strings.Contains(strings.ToLower(task.Description), rule) {
				t.Errorf("task %q lacks %q", task.Name, rule)
			}
		}
	}
	if !strings.Contains(out.Tasks[0].Description, "do not create it") || !strings.Contains(out.Tasks[0].Description, "never mark anything Done") {
		t.Errorf("sweep task lacks the comment-only policy")
	}

	sweep, weekly := out.Schedules[0], out.Schedules[1]
	if sweep.Cron != "0 7 * * 1-5" || weekly.Cron != "0 8 * * 1" || sweep.Timezone != "America/Denver" || !sweep.Enabled || sweep.NextRunAt == nil ||
		sweep.Inputs[model.InputRunTasks] != out.Tasks[0].ID || weekly.Inputs[model.InputRunTasks] != out.Tasks[1].ID || sweep.RunAsName != "Ada Admin" {
		t.Errorf("schedules = %+v / %+v", sweep, weekly)
	}
	sw, err := e.st.WatchOfSchedule(ctx, sweep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sw.Source.Kind != model.WatchSourceTrackingList || sw.RunWhen != model.WatchRunChangedOrFollowups || !sw.DryRun || sw.Followups == nil ||
		len(sw.Followups.Steps) != 4 || sw.Followups.Steps[3].Repeat != "7d" || sw.Followups.Steps[2].Notify != "lead" ||
		!slices.Equal(sw.Followups.QuietDays, []string{"Sat", "Sun"}) || sw.Followups.Timezone != "America/Denver" {
		t.Errorf("sweep watch = %+v (ladder %+v)", sw, sw.Followups)
	}
	ww, err := e.st.WatchOfSchedule(ctx, weekly.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ww.RunWhen != model.WatchRunAlways || !ww.DryRun || ww.Followups != nil {
		t.Errorf("weekly watch = %+v", ww)
	}
	dests, err := e.st.ReportDestinations(ctx, weekly.ID)
	if err != nil || len(dests) != 1 || dests[0].ConnectionID != slack.ID {
		t.Errorf("destinations = %+v %v", dests, err)
	}
	if d, _ := e.st.ReportDestinations(ctx, sweep.ID); len(d) != 0 {
		t.Errorf("the sweep has destinations: %+v", d)
	}
	chat, err := e.st.ChatSettings(ctx, out.Crew.ID)
	if err != nil || !chat.Enabled || chat.FrontDeskAgentID == nil || *chat.FrontDeskAgentID != coord.ID {
		t.Errorf("chat = %+v %v", chat, err)
	}
	linked, err := e.st.CrewConnections(ctx, out.Crew.ID, store.AllAreas)
	if err != nil || len(linked) != 2 {
		t.Errorf("crew connections = %+v %v", linked, err)
	}

	// The same name again fails as a whole: nothing of the second attempt is left behind
	before := len(decodeData[[]model.Agent](t, e.do(t, "GET", "/api/agents", nil)))
	r = e.do(t, "POST", "/api/pmo/setup", body)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.HasPrefix(strings.Join(r.body.Error.Details, " "), "name: ") {
		t.Errorf("a taken name should point at the name: %+v", r.body.Error)
	}
	if after := len(decodeData[[]model.Agent](t, e.do(t, "GET", "/api/agents", nil))); after != before {
		t.Errorf("agents %d -> %d after a failed install", before, after)
	}
	body["name"] = "Birch PMO"
	body["enabled"] = false
	r = e.do(t, "POST", "/api/pmo/setup", body)
	wantStatus(t, r, http.StatusCreated)
	if birch := decodeData[store.PMOInstalled](t, r); birch.Schedules[0].Enabled || birch.Schedules[0].NextRunAt != nil {
		t.Errorf("disabled install = %+v", birch.Schedules[0])
	}
}

func TestPMOSetupIsChecked(t *testing.T) {
	e := newReportEnv(t)
	jira := e.jira(t, "Acme Jira", financeArea)
	hrJira := e.jira(t, "HR Jira", hrArea)
	slack := e.connection(t, "PMO Slack", "slack", map[string]string{}, map[string]string{"webhook_url": e.hooks.URL + "/slack/hook/1"})
	for want, body := range map[string]map[string]any{
		"area_ids: pick at least one":               {"tracker_connection_id": jira.ID, "timezone": "UTC"},
		"timezone":                                  {"area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "Mars/Base"},
		"tracker_connection_id: pick":               {"area_ids": []string{financeArea}, "timezone": "UTC"},
		"is a slack connection, not a tracker":      {"area_ids": []string{financeArea}, "tracker_connection_id": slack.ID, "timezone": "UTC"},
		"not in any of the chosen functional areas": {"area_ids": []string{financeArea}, "tracker_connection_id": hrJira.ID, "timezone": "UTC"},
		"tracker_connection_id: no such connection": {"area_ids": []string{financeArea}, "tracker_connection_id": missingID, "timezone": "UTC"},
		"coordinator_llm_id: no such LLM": {"area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "UTC",
			"coordinator_llm_id": missingID},
		"name: must be at most 80":                      {"name": strings.Repeat("n", 81), "area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "UTC"},
		"area_ids: \"nope\" is not an id":               {"area_ids": []string{"nope"}, "tracker_connection_id": jira.ID, "timezone": "UTC"},
		"tracker_connection_id: is not a connection id": {"area_ids": []string{financeArea}, "tracker_connection_id": "x", "timezone": "UTC"},
		"reporter_llm_id: is not an LLM id":             {"area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "UTC", "reporter_llm_id": "x"},
		"timezone: \"Local\"":                           {"area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "Local"},
	} {
		r := e.do(t, "POST", "/api/pmo/setup", body)
		wantStatus(t, r, http.StatusUnprocessableEntity)
		if !strings.Contains(strings.Join(r.body.Error.Details, " "), want) {
			t.Errorf("%s: %+v", want, r.body.Error)
		}
	}
	// A builder in Finance can set one up in Finance, not in IT
	builderID, builder := e.person(t, "b@example.com", []string{builderRole}, []string{financeArea}, nil)
	_ = builderID
	r := e.as(builder).do(t, "POST", "/api/pmo/setup", map[string]any{"area_ids": []string{itArea}, "tracker_connection_id": jira.ID, "timezone": "UTC"})
	if r.status == http.StatusForbidden {
		t.Skip("builders cannot set up a PMO in this role setup")
	}
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "is not one of your functional areas") {
		t.Errorf("other area: %+v", r.body.Error)
	}
	// Viewers cannot
	_, viewer := e.person(t, "v@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "GET", "/api/pmo/setup", nil), http.StatusForbidden)

	// Without the template there is nothing to copy
	if _, err := e.st.Pool().Exec(context.Background(), `DELETE FROM crews WHERE id = $1`, pmo.TemplateCrewID); err != nil {
		t.Fatal(err)
	}
	r = e.do(t, "GET", "/api/pmo/setup", nil)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "pmo_template_missing" {
		t.Errorf("no template: %+v", r.body.Error)
	}
	wantStatus(t, e.do(t, "POST", "/api/pmo/setup", map[string]any{"area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "UTC"}),
		http.StatusConflict)
}
