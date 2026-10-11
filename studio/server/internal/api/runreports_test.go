package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"crewstudio/internal/connections"
	"crewstudio/internal/delivery"
	"crewstudio/internal/delivery/deliverytest"
	"crewstudio/internal/km2"
	"crewstudio/internal/km2/km2test"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
)

const reportKM2Key = "km2_report-test-key"

// reportEnv has connections on, a fake Slack and Teams, a fake SMTP server and a fake KM2. Nothing real is reached.
type reportEnv struct {
	testEnv
	hooks *deliverytest.Hooks
	smtp  *deliverytest.SMTPServer
	km2   *km2test.Data
	km2At string
}

func newReportEnv(t *testing.T) reportEnv {
	t.Helper()
	e := newEnv(t)
	box, err := secrets.NewBox(secrets.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	hooks := deliverytest.NewHooks(t)
	mail := deliverytest.NewSMTP(t, deliverytest.SMTPOptions{})
	kd := km2test.NewData(reportKM2Key)
	ksrv := km2test.Server(t, kd)
	conns := connections.New(e.st, box)
	sender := delivery.New(conns, delivery.Options{Origins: []string{hooks.URL}, SlackAPIURL: hooks.URL + "/slack/api", TLS: mail.Pool})
	e.srv.Config.Handler = NewServer(Deps{
		Store: e.st, Runs: e.runs, Broker: e.broker, Tools: fakeTools{}, Launcher: launch.New(e.st, fakeTools{}, e.runs),
		Scheduler: e.sched, Files: e.files, SSO: e.sso, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Connections: conns, KM2: km2.NewService(e.st, conns, km2.Options{HTTP: ksrv.Client()}), Delivery: sender,
	}, Options{AllowedOrigins: []string{"http://127.0.0.1:5174"}, PublicURL: e.srv.URL, LocalLogin: true}).Handler()
	return reportEnv{testEnv: e, hooks: hooks, smtp: mail, km2: kd, km2At: ksrv.URL}
}

func (e reportEnv) connection(t *testing.T, name, kind string, config, secrets map[string]string) model.Connection {
	t.Helper()
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": name, "kind": kind, "area_ids": []string{financeArea},
		"config": config, "secrets": secrets})
	wantStatus(t, r, http.StatusCreated)
	return decodeData[model.Connection](t, r)
}

// destinations makes one connection of each kind, lets the crew use them, and sets them on the run's schedule.
func (e reportEnv) destinations(t *testing.T, sr stateRun) map[string]model.Connection {
	t.Helper()
	host, port := e.smtp.HostPort()
	conns := map[string]model.Connection{
		"slack": e.connection(t, "PMO Slack", "slack", map[string]string{}, map[string]string{"webhook_url": e.hooks.URL + "/slack/hook/T/B/x"}),
		"teams": e.connection(t, "PMO Teams", "teams", map[string]string{}, map[string]string{"post_url": e.hooks.URL + "/teams/wf?sig=s3cret"}),
		"email": e.connection(t, "PMO Mail", "email", map[string]string{"host": host, "port": port, "from_address": "pmo@example.com"}, map[string]string{"password": "unused"}),
		"km2": e.connection(t, "PMO KM2", "km2", map[string]string{"base_url": e.km2At, model.ConfigKM2DefaultFolder: km2test.FolderAtlas},
			map[string]string{"api_key": reportKM2Key}),
	}
	ids := []string{}
	for _, c := range conns {
		ids = append(ids, c.ID)
	}
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, ids, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	r := e.do(t, "PUT", "/api/schedules/"+sr.schedule.ID+"/report-destinations", map[string]any{"destinations": []map[string]any{
		{"kind": "slack", "connection_id": conns["slack"].ID},
		{"kind": "teams", "connection_id": conns["teams"].ID},
		{"kind": "email", "connection_id": conns["email"].ID, "recipients": []string{"sam@example.com", "dana@example.com"}},
		{"kind": "km2", "connection_id": conns["km2"].ID},
	}})
	wantStatus(t, r, http.StatusOK)
	if view := decodeData[destinationsView](t, r); len(view.Destinations) != 4 || len(view.Connections) != 4 || view.Destinations[2].ConnectionName != "PMO Mail" {
		t.Fatalf("destinations = %+v", view)
	}
	return conns
}

func statusesOf(res reportResult) map[string]string {
	out := map[string]string{}
	for _, d := range res.Deliveries {
		out[d.Destination] = d.Status
	}
	return out
}

func TestDeliverReportSendsEverywhereOnceAndLogs(t *testing.T) {
	e := newReportEnv(t)
	sr := e.scheduledRun(t)
	e.destinations(t, sr)
	body := map[string]any{"project": "Project Atlas", "markdown": "# Atlas\nOn track."}
	r := e.call(t, sr, "POST", "/reports", body)
	wantStatus(t, r, http.StatusCreated)
	res := decodeData[reportResult](t, r)
	week := model.ISOWeek(time.Now(), time.UTC)
	want := map[string]string{"work_order": "sent", "slack": "sent", "teams": "sent", "email": "sent", "km2": "sent"}
	if got := statusesOf(res); len(got) != 5 || res.DryRun || res.Week != week || strings.Join(sortedValues(got), ",") != strings.Join(sortedValues(want), ",") {
		t.Fatalf("result = %+v", res)
	}
	if n := len(e.hooks.Requests()); n != 2 {
		t.Errorf("webhook requests = %d", n)
	}
	mails := e.smtp.Mails()
	if len(mails) != 1 || len(mails[0].To) != 2 || !mails[0].TLS || !strings.Contains(mails[0].Data, "Subject: Project Atlas status, "+week) {
		t.Errorf("mails = %+v", mails)
	}
	saved := e.km2.Saved()
	if len(saved) != 1 || saved[0][0] != km2test.FolderAtlas || saved[0][1] != "project-atlas-weekly-"+week {
		t.Errorf("km2 = %+v", saved)
	}

	// The same report again sends nothing; different text for the same project is refused
	r = e.call(t, sr, "POST", "/reports", body)
	wantStatus(t, r, http.StatusOK)
	if again := decodeData[reportResult](t, r); again.ReportID != res.ReportID || !strings.Contains(again.Message, "already delivered") {
		t.Errorf("again = %+v", again)
	}
	r = e.call(t, sr, "POST", "/reports", map[string]any{"project": "project atlas", "markdown": "changed"})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "report_exists" || len(e.hooks.Requests()) != 2 || len(e.smtp.Mails()) != 1 {
		t.Errorf("changed report: %+v", r.body.Error)
	}

	// Every delivery is in the action log under its destination, and the report is next week's starting note
	entries, _, err := e.st.ListActions(context.Background(), store.ActionFilter{Scope: store.StateScope{CrewID: sr.crew.ID}, AllSchedules: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, a := range entries {
		if a.Subject == "project:project-atlas" && !a.DryRun {
			actions[a.Action] = true
		}
	}
	// KM2 went through the km_save_document path: a km2:save entry with the report's external_ref
	if !hasKM2Save(entries, "km2:project-atlas-weekly-"+week) {
		t.Errorf("no km2:save entry: %v", actions)
	}
	for _, a := range []string{"report:work_order", "report:slack", "report:teams", "report:email", "report:km2"} {
		if !actions[a] {
			t.Errorf("action log lacks %s: %v", a, actions)
		}
	}
	note, err := e.st.LatestNote(context.Background(), store.NoteKey{StateScope: store.StateScope{CrewID: sr.crew.ID, ScheduleID: &sr.schedule.ID},
		Subject: "project:project-atlas", Name: model.ReportNote})
	if err != nil || note.Content != "# Atlas\nOn track." {
		t.Errorf("note = %+v, %v", note, err)
	}
	journal := e.stateLines(t, sr.run.ID)
	if !containsLine(journal, "Report: Project Atlas delivered to PMO Mail (2 recipients)") {
		t.Errorf("journal = %v", journal)
	}

	// The work order shows the report with nothing to retry
	view := decodeData[workOrderReportsView](t, e.do(t, "GET", "/api/work-orders/"+*sr.run.WorkOrderID+"/reports", nil))
	if len(view.Reports) != 1 || view.Reports[0].Retryable != 0 || len(view.Reports[0].Deliveries) != 5 {
		t.Errorf("work order reports = %+v", view)
	}
}

func sortedValues(m map[string]string) []string {
	out := []string{}
	for _, k := range []string{"email", "km2", "slack", "teams", "work_order"} {
		out = append(out, k+"="+m[k])
	}
	return out
}

func containsLine(lines []string, fragment string) bool {
	for _, l := range lines {
		if strings.Contains(l, fragment) {
			return true
		}
	}
	return false
}

func TestDeliverReportInDryRunSendsNothing(t *testing.T) {
	e := newReportEnv(t)
	sr := e.scheduledRun(t)
	e.destinations(t, sr)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET inputs = '{"dry_run": "true"}' WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": "Dry status"})
	wantStatus(t, r, http.StatusCreated)
	res := decodeData[reportResult](t, r)
	got := statusesOf(res)
	if !res.DryRun || got["slack"] != "would_send" || got["teams"] != "would_send" || got["email"] != "would_send" || got["km2"] != "would_send" ||
		got["work_order"] != "sent" || !strings.Contains(res.Message, "Dry run") {
		t.Fatalf("dry run = %+v", res)
	}
	if len(e.hooks.Requests()) != 0 || len(e.smtp.Mails()) != 0 || len(e.km2.Saved()) != 0 {
		t.Error("a dry run sent something")
	}
	entries, _, err := e.st.ListActions(context.Background(), store.ActionFilter{Scope: store.StateScope{CrewID: sr.crew.ID, DryRun: true}, AllSchedules: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	dry := 0
	for _, a := range entries {
		if strings.HasPrefix(a.Action, "report:") && a.DryRun {
			dry++
		}
	}
	if dry != 5 {
		t.Errorf("dry-run entries = %d", dry)
	}
	// Dry-run reports are never retried
	view := decodeData[workOrderReportsView](t, e.do(t, "POST", "/api/work-orders/"+*sr.run.WorkOrderID+"/reports/retry", map[string]any{}))
	if view.Retried != 0 || len(e.hooks.Requests()) != 0 {
		t.Errorf("retry of a dry run = %+v", view)
	}
}

func TestRetryFailedDeliveriesSendsOnlyWhatFailed(t *testing.T) {
	e := newReportEnv(t)
	sr := e.scheduledRun(t)
	e.destinations(t, sr)
	e.hooks.Fail(http.StatusBadGateway, "")
	r := e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": "Status"})
	wantStatus(t, r, http.StatusCreated)
	res := decodeData[reportResult](t, r)
	if got := statusesOf(res); got["slack"] != "failed" || got["teams"] != "failed" || got["email"] != "sent" || !strings.Contains(res.Message, "2 failed") {
		t.Fatalf("first try = %+v", res)
	}
	path := "/api/work-orders/" + *sr.run.WorkOrderID + "/reports"
	view := decodeData[workOrderReportsView](t, e.do(t, "GET", path, nil))
	if view.Reports[0].Retryable != 2 {
		t.Fatalf("retryable = %+v", view.Reports[0])
	}
	// Viewers may see reports but not retry them
	_, viewer := e.person(t, "v@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "GET", path, nil), http.StatusOK)
	wantStatus(t, e.as(viewer).do(t, "POST", path+"/retry", map[string]any{}), http.StatusForbidden)

	e.hooks.Fail(0, "")
	r = e.do(t, "POST", path+"/retry", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	view = decodeData[workOrderReportsView](t, r)
	if view.Retried != 2 || view.Sent != 2 || view.Reports[0].Retryable != 0 {
		t.Errorf("retry = %+v", view)
	}
	if mails := e.smtp.Mails(); len(mails) != 1 {
		t.Errorf("email was sent again: %d", len(mails))
	}
	for _, d := range view.Reports[0].Deliveries {
		if d.Kind == "teams" && (d.Attempts != 2 || d.Status != "sent") {
			t.Errorf("teams delivery = %+v", d)
		}
	}
	webhooks := len(e.hooks.Requests())
	wantStatus(t, e.do(t, "POST", path+"/retry", map[string]any{}), http.StatusOK)
	if len(e.hooks.Requests()) != webhooks {
		t.Error("a second retry sent again")
	}
	wantStatus(t, e.do(t, "GET", "/api/work-orders/"+missingID+"/reports", nil), http.StatusNotFound)
}

func TestDeliverReportLimitsAndRefusals(t *testing.T) {
	e := newReportEnv(t)
	sr := e.scheduledRun(t)
	wantStatus(t, e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": strings.Repeat("x", model.MaxReportBytes+1)}),
		http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/reports", map[string]any{"project": "", "markdown": "x"}), http.StatusUnprocessableEntity)
	// With no destinations, only the work order gets the report; a run delivers at most MaxReportsPerRun reports
	for i := range model.MaxReportsPerRun {
		r := e.call(t, sr, "POST", "/reports", map[string]any{"project": "P" + string(rune('a'+i)), "markdown": "x"})
		wantStatus(t, r, http.StatusCreated)
		if i == 0 && len(decodeData[reportResult](t, r).Deliveries) != 1 {
			t.Errorf("deliveries without destinations = %s", r.body.Data)
		}
	}
	r := e.call(t, sr, "POST", "/reports", map[string]any{"project": "One more", "markdown": "x"})
	wantStatus(t, r, http.StatusTooManyRequests)
	if r.body.Error.Code != "report_cap" {
		t.Errorf("cap = %+v", r.body.Error)
	}
	// Chat turns never deliver
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET kind = 'chat' WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.call(t, sr, "POST", "/reports", map[string]any{"project": "Chat", "markdown": "x"}), http.StatusForbidden)
}

func TestDeliveriesPerRunCap(t *testing.T) {
	e := newReportEnv(t)
	sr := e.scheduledRun(t)
	e.destinations(t, sr)
	e.hooks.Fail(http.StatusInternalServerError, "") // fail fast; failed sends still count
	sent := 0
	for i := 0; ; i++ {
		r := e.call(t, sr, "POST", "/reports", map[string]any{"project": "Project " + string(rune('A'+i)), "markdown": "x"})
		if r.status == http.StatusTooManyRequests {
			break
		}
		wantStatus(t, r, http.StatusCreated)
		sent += 4
	}
	if sent != model.MaxDeliveriesPerRun {
		t.Errorf("sends before the cap = %d, want %d", sent, model.MaxDeliveriesPerRun)
	}
}

func TestReportDestinationsAreChecked(t *testing.T) {
	e := newReportEnv(t)
	sr := e.scheduledRun(t)
	path := "/api/schedules/" + sr.schedule.ID + "/report-destinations"
	slack := e.connection(t, "Slack", "slack", map[string]string{"channel": "C1"}, map[string]string{"bot_token": "xoxb-1"})
	view := decodeData[destinationsView](t, e.do(t, "GET", path, nil))
	if len(view.Destinations) != 0 || len(view.Connections) != 0 || len(view.Kinds) != 4 {
		t.Errorf("view = %+v", view)
	}
	// Not yet a connection the crew may use
	r := e.do(t, "PUT", path, map[string]any{"destinations": []map[string]any{{"kind": "slack", "connection_id": slack.ID}}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "the crew may not use Slack") {
		t.Errorf("unlinked: %+v", r.body.Error)
	}
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{slack.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{
		{"kind": "teams", "connection_id": slack.ID},
		{"kind": "slack", "connection_id": missingID},
		{"kind": "email", "connection_id": slack.ID},
		{"kind": "fax", "connection_id": slack.ID},
	} {
		wantStatus(t, e.do(t, "PUT", path, map[string]any{"destinations": []map[string]any{bad}}), http.StatusUnprocessableEntity)
	}
	// An email connection limited to some domains refuses recipients elsewhere
	host, port := e.smtp.HostPort()
	mail := e.connection(t, "Domain mail", "email", map[string]string{"host": host, "port": port, "from_address": "pmo@example.com",
		"allowed_domains": "example.com"}, map[string]string{"password": "x"})
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{slack.ID, mail.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	r = e.do(t, "PUT", path, map[string]any{"destinations": []map[string]any{{"kind": "email", "connection_id": mail.ID, "recipients": []string{"a@example.com", "b@evil.io"}}}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "may only send to example.com") {
		t.Errorf("domains: %+v", r.body.Error)
	}
	wantStatus(t, e.do(t, "PUT", path, map[string]any{"destinations": []map[string]any{{"kind": "slack", "connection_id": slack.ID}}}), http.StatusOK)
	// A connection a destination uses cannot be deleted
	wantStatus(t, e.do(t, "DELETE", "/api/connections/"+slack.ID, nil), http.StatusConflict)
	// Someone outside the schedule's areas sees nothing
	_, outsider := e.person(t, "it@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", path, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "PUT", path, map[string]any{"destinations": []map[string]any{}}), http.StatusOK)
}

func TestDeliveryConnectionTestsSendATestMessage(t *testing.T) {
	e := newReportEnv(t)
	saved := deliveryTests
	deliveryTests = newKeyedLimiter(rate.Inf, 1)
	t.Cleanup(func() { deliveryTests = saved })
	host, port := e.smtp.HostPort()
	for _, tc := range []struct {
		name, kind     string
		config, secret map[string]string
		ok             bool
		want           string
	}{
		{"Hook", "slack", map[string]string{}, map[string]string{"webhook_url": e.hooks.URL + "/slack/hook/1"}, true, "Sent a test message"},
		{"Bot", "slack", map[string]string{"channel": "C1"}, map[string]string{"bot_token": e.hooks.BotToken}, true, "Sent a test message"},
		{"Flow", "teams", map[string]string{}, map[string]string{"post_url": e.hooks.URL + "/teams/x"}, true, "Sent a test message"},
		{"Old", "teams", map[string]string{}, map[string]string{"post_url": "https://acme.webhook.office.com/x"}, false, "connectors were retired"},
		{"Mail", "email", map[string]string{"host": host, "port": port, "from_address": "pmo@example.com"}, map[string]string{"password": "x"}, true,
			"Sent a test email to admin@example.com"},
	} {
		c := e.connection(t, tc.name, tc.kind, tc.config, tc.secret)
		res := decodeData[deliveryTestResult](t, e.do(t, "POST", "/api/connections/"+c.ID+"/test", map[string]any{}))
		if res.OK != tc.ok || !res.Supported || !strings.Contains(res.Message, tc.want) {
			t.Errorf("%s: %+v", tc.name, res)
		}
	}
	if mails := e.smtp.Mails(); len(mails) != 1 || mails[0].To[0] != "admin@example.com" {
		t.Errorf("mails = %+v", mails)
	}
	if reqs := e.hooks.Requests(); len(reqs) != 3 || !strings.Contains(toJSONString(reqs[2].Body), "Crew Studio test message") {
		t.Errorf("hooks = %d", len(reqs))
	}
	// Test messages are throttled per person
	deliveryTests = newKeyedLimiter(rate.Every(time.Hour), 1)
	c := e.connection(t, "Throttled", "teams", map[string]string{}, map[string]string{"post_url": e.hooks.URL + "/teams/y"})
	wantStatus(t, e.do(t, "POST", "/api/connections/"+c.ID+"/test", map[string]any{}), http.StatusOK)
	wantStatus(t, e.do(t, "POST", "/api/connections/"+c.ID+"/test", map[string]any{}), http.StatusTooManyRequests)
}

func hasKM2Save(entries []model.ActionEntry, subject string) bool {
	for _, a := range entries {
		if a.Action == model.ActionKM2Save && a.Subject == subject && a.Details["tool"] == "deliver_report" && a.Details["outcome"] == "created" {
			return true
		}
	}
	return false
}

func TestReportToKM2NeedsTheCrewsOnlyKM2Connection(t *testing.T) {
	e := newReportEnv(t)
	sr := e.scheduledRun(t)
	conns := e.destinations(t, sr)
	other := e.connection(t, "Second KM2", "km2", map[string]string{"base_url": e.km2At, model.ConfigKM2DefaultFolder: km2test.FolderAtlas},
		map[string]string{"api_key": reportKM2Key})
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{conns["slack"].ID, conns["teams"].ID, conns["email"].ID,
		conns["km2"].ID, other.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	res := decodeData[reportResult](t, e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": "x"}))
	for _, d := range res.Deliveries {
		if d.Destination == "km2" && (d.Status != "failed" || !strings.Contains(d.Message, "more than one KM2 connection")) {
			t.Errorf("km2 = %+v", d)
		}
	}
	if len(e.km2.Saved()) != 0 {
		t.Error("saved with two KM2 connections")
	}
}
