package reports

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"crewstudio/internal/delivery"
	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

const (
	peCrew  = "44444444-0000-4000-8000-000000000001"
	finance = "55555555-0000-4000-8000-000000000001"
)

// fakeSender records what would be sent and fails for the connections named in fail.
type fakeSender struct {
	mu   sync.Mutex
	sent []string
	msgs map[string]delivery.Message
	fail map[string]error
}

func (f *fakeSender) Send(_ context.Context, conn model.Connection, to delivery.Target, msg delivery.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[conn.Name]; err != nil {
		return err
	}
	f.sent = append(f.sent, conn.Name+"|"+strings.Join(to.Recipients, ",")+"|"+msg.Title+"|"+msg.Footer)
	if f.msgs == nil {
		f.msgs = map[string]delivery.Message{}
	}
	f.msgs[conn.Name] = msg
	return nil
}

type fakeKM2 struct {
	err   error
	saved *[]KM2Request
}

func (f fakeKM2) SaveReport(_ context.Context, req KM2Request) (KM2Saved, error) {
	if f.err != nil {
		return KM2Saved{}, f.err
	}
	if f.saved != nil {
		*f.saved = append(*f.saved, req)
	}
	return KM2Saved{DocumentID: "doc-1", Outcome: "created"}, nil
}

type fixture struct {
	st    *store.Store
	svc   *Service
	send  *fakeSender
	scope store.RunScope
	wo    model.WorkOrder
	conns map[string]model.Connection
	saved []KM2Request
}

func newFixture(t *testing.T, dryRun bool) *fixture {
	t.Helper()
	st := store.New(testdb.New(t, "reports_test"))
	ctx := context.Background()
	f := &fixture{st: st, send: &fakeSender{fail: map[string]error{}}, conns: map[string]model.Connection{}}
	f.svc = New(st, f.send, fakeKM2{saved: &f.saved}, nil)
	f.svc.now = func() time.Time { return time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC) }
	crewID := peCrew
	sc, err := st.SaveSchedule(ctx, "", model.Schedule{Name: "weekly", CrewID: &crewID, Cron: "0 8 * * 1", Timezone: "UTC",
		OverlapPolicy: model.OverlapSkip, Inputs: map[string]string{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.wo, err = st.CreateWorkOrder(ctx, model.WorkOrderInput{Title: "weekly", CrewID: &crewID},
		store.WorkOrderMeta{TargetName: "PE", AreaIDs: []string{finance}, Source: model.SourceSchedule, SourceID: sc.ID})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := st.CreateRun(ctx, model.RunSpec{BudgetUSD: 1}, store.RunMeta{AreaIDs: []string{finance}, WorkOrderID: f.wo.ID})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var dests []model.ReportDestinationInput
	for i, kind := range []string{"slack", "teams", "email", "km2"} {
		id := []string{"aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000002",
			"aaaaaaaa-0000-4000-8000-000000000003", "aaaaaaaa-0000-4000-8000-000000000004"}[i]
		c, err := st.CreateConnection(ctx, id, model.ConnectionInput{Name: strings.ToUpper(kind), Kind: kind, AreaIDs: []string{finance},
			Config: map[string]string{}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.conns[kind], ids = c, append(ids, c.ID)
		d := model.ReportDestinationInput{Kind: kind, ConnectionID: c.ID}
		if kind == "email" {
			d.Recipients = []string{"sam@example.com"}
		}
		dests = append(dests, d)
	}
	if err := st.SetCrewConnections(ctx, peCrew, ids, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetReportDestinations(ctx, sc.ID, dests); err != nil {
		t.Fatal(err)
	}
	f.scope = store.RunScope{RunID: run.ID, CrewID: peCrew, CrewName: "PE Due Diligence Crew", ScheduleID: &sc.ID, ScheduleName: "weekly", DryRun: dryRun}
	return f
}

func (f *fixture) submit(t *testing.T, project, md string) Outcome {
	t.Helper()
	out, err := f.svc.Submit(context.Background(), Submission{Scope: f.scope, WorkOrderID: &f.wo.ID, WorkOrderNumber: f.wo.Number,
		Report: model.ReportRequest{Project: project, Markdown: md}})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func statuses(rep model.Report) string {
	var parts []string
	for _, d := range rep.Deliveries {
		parts = append(parts, d.Kind+"="+d.Status)
	}
	return strings.Join(parts, " ")
}

func TestSubmitDeliversAndRetries(t *testing.T) {
	f := newFixture(t, false)
	f.send.fail["TEAMS"] = &delivery.Error{Message: "teams had a problem (HTTP 502); retry later", Retryable: true}
	out := f.submit(t, "Atlas", "# Atlas\nOn track")
	if got := statuses(out.Report); got != "work_order=sent slack=sent teams=failed email=sent km2=sent" {
		t.Fatalf("statuses = %s", got)
	}
	if out.Report.Week != "2026-W41" || out.Report.Title != "Atlas status, 2026-W41" {
		t.Errorf("report = %+v", out.Report)
	}
	if len(f.send.sent) != 2 || !strings.Contains(strings.Join(f.send.sent, "\n"), "EMAIL|sam@example.com|Atlas status, 2026-W41|Sent by Crew Studio (PE Due Diligence Crew, weekly), work order #") {
		t.Errorf("sent = %v", f.send.sent)
	}
	if len(f.saved) != 1 || f.saved[0].ExternalRef != "atlas-weekly-2026-W41" || f.saved[0].Metadata["project"] != "atlas" || f.saved[0].DryRun ||
		f.saved[0].RunID != f.scope.RunID || f.saved[0].ConnectionID != f.conns["km2"].ID {
		t.Errorf("km2 = %+v", f.saved)
	}
	teams := out.Report.Deliveries[2]
	if teams.Message != "Teams had a problem (HTTP 502); retry later." || teams.Attempts != 1 {
		t.Errorf("teams = %+v", teams)
	}

	delete(f.send.fail, "TEAMS")
	res, err := f.svc.Retry(context.Background(), f.wo.ID, f.wo.Number)
	if err != nil {
		t.Fatal(err)
	}
	if res.Retried != 1 || res.Sent != 1 || statuses(res.Reports[0]) != "work_order=sent slack=sent teams=sent email=sent km2=sent" {
		t.Errorf("retry = %+v", res)
	}
	if len(f.send.sent) != 3 || !strings.Contains(f.send.sent[2], "(retried)") {
		t.Errorf("sent after retry = %v", f.send.sent)
	}
	// A delivery interrupted mid-send (its reservation is still "sending") is never sent a second time
	interrupted := res.Reports[0].Deliveries[2]
	if _, err := f.st.Pool().Exec(context.Background(), `UPDATE action_log SET details = details || '{"outcome": "sending"}' WHERE idempotency_key = $1`,
		interrupted.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.Pool().Exec(context.Background(), `UPDATE report_deliveries SET status = 'failed' WHERE id = $1`, interrupted.ID); err != nil {
		t.Fatal(err)
	}
	res, err = f.svc.Retry(context.Background(), f.wo.ID, f.wo.Number)
	if err != nil || len(f.send.sent) != 3 || !strings.Contains(res.Reports[0].Deliveries[2].Message, "interrupted while sending") {
		t.Errorf("interrupted = %+v, %v, sent %d", res.Reports[0].Deliveries[2], err, len(f.send.sent))
	}
	// A delivery already in the action log is not sent again, even if its row says it failed
	if _, err := f.st.Pool().Exec(context.Background(), `UPDATE report_deliveries SET status = 'failed' WHERE kind = 'slack'`); err != nil {
		t.Fatal(err)
	}
	res, err = f.svc.Retry(context.Background(), f.wo.ID, f.wo.Number)
	if err != nil || res.Retried != 1 || res.Sent != 1 || len(f.send.sent) != 3 {
		t.Errorf("logged retry = %+v, %v, sent %d", res, err, len(f.send.sent))
	}
}

func TestSubmitInDryRunAndConnectionProblems(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	// The email connection is turned off, Teams is no longer the crew's, Slack was deleted from under its destination
	if _, err := f.st.Pool().Exec(ctx, `UPDATE connections SET enabled = false WHERE name = 'EMAIL'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.Pool().Exec(ctx, `DELETE FROM crew_connections WHERE connection_id = $1`, f.conns["teams"].ID); err != nil {
		t.Fatal(err)
	}
	out := f.submit(t, "Birch", "status")
	if got := statuses(out.Report); got != "work_order=sent slack=would_send teams=failed email=failed km2=would_send" {
		t.Fatalf("statuses = %s", got)
	}
	// KM2 records its own dry-run save through the KM2 save path; nothing else is called
	if len(f.send.sent) != 0 || len(f.saved) != 1 || !f.saved[0].DryRun {
		t.Errorf("a dry run sent something: %v %+v", f.send.sent, f.saved)
	}
	for _, d := range out.Report.Deliveries {
		switch d.Kind {
		case "teams":
			if !strings.Contains(d.Message, "may no longer use the connection TEAMS") {
				t.Errorf("teams = %s", d.Message)
			}
		case "email":
			if !strings.Contains(d.Message, "is turned off") {
				t.Errorf("email = %s", d.Message)
			}
		}
	}
	// Dry-run reports are never retried
	if res, err := f.svc.Retry(ctx, f.wo.ID, 0); err != nil || res.Retried != 0 {
		t.Errorf("retry = %+v %v", res, err)
	}
	// The same report again is not delivered again
	if again := f.submit(t, "birch", "status"); !again.Existing {
		t.Error("not recognised as the same report")
	}
}

func TestDeliveryEdgeCases(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	rep := model.Report{ID: "r", CrewID: peCrew, ProjectSlug: "x"}
	run := deliveryRun{scope: store.StateScope{CrewID: peCrew}}
	if status, msg := f.svc.attempt(ctx, rep, model.ReportDelivery{Kind: model.DestinationTracker}, run); status != model.DeliveryFailed ||
		!strings.Contains(msg, "not available yet") {
		t.Errorf("tracker = %s %s", status, msg)
	}
	if status, msg := f.svc.attempt(ctx, rep, model.ReportDelivery{Kind: "slack", ConnectionName: "Gone", IdempotencyKey: "k1"}, run); status != model.DeliveryFailed ||
		!strings.Contains(msg, "Gone was deleted") {
		t.Errorf("deleted = %s %s", status, msg)
	}
	missing := "aaaaaaaa-0000-4000-8000-0000000000ff"
	if _, msg := f.svc.attempt(ctx, rep, model.ReportDelivery{Kind: "slack", ConnectionID: &missing, ConnectionName: "Gone", IdempotencyKey: "k2"}, run); !strings.Contains(msg, "was deleted") {
		t.Errorf("missing = %s", msg)
	}
	email := f.conns["email"].ID
	if _, msg := f.svc.attempt(ctx, rep, model.ReportDelivery{Kind: "slack", ConnectionID: &email, IdempotencyKey: "k3"}, run); !strings.Contains(msg, "is a email connection, not a slack one") {
		t.Errorf("kind = %s", msg)
	}
	km := f.conns["km2"].ID
	runID := f.scope.RunID
	withRun := rep
	withRun.RunID = &runID
	f.svc.km2 = nil
	if _, msg := f.svc.attempt(ctx, rep, model.ReportDelivery{Kind: "km2", ConnectionID: &km, IdempotencyKey: "k4"}, run); msg != "KM2 is not available on this server." {
		t.Errorf("no km2 = %s", msg)
	}
	f.svc.km2 = fakeKM2{err: &km2.FolderError{Message: "Saving is off for the KM2 connection KM2."}}
	if _, msg := f.svc.attempt(ctx, withRun, model.ReportDelivery{Kind: "km2", ConnectionID: &km, IdempotencyKey: "k5"}, run); msg != "Saving is off for the KM2 connection KM2." {
		t.Errorf("folder = %s", msg)
	}
	f.svc.km2 = fakeKM2{err: errors.New("boom")}
	if _, msg := f.svc.attempt(ctx, withRun, model.ReportDelivery{Kind: "km2", ConnectionID: &km, IdempotencyKey: "k6"}, run); !strings.HasPrefix(msg, "The request failed") {
		t.Errorf("km2 error = %s", msg)
	}
	f.svc.km2 = fakeKM2{err: &SafeError{Message: "The run already saved 10 documents to KM2, the most one run may."}}
	if _, msg := f.svc.attempt(ctx, withRun, model.ReportDelivery{Kind: "km2", ConnectionID: &km, IdempotencyKey: "k7"}, run); !strings.Contains(msg, "already saved 10") {
		t.Errorf("km2 cap = %s", msg)
	}
	f.svc.km2 = fakeKM2{}
	if _, msg := f.svc.attempt(ctx, rep, model.ReportDelivery{Kind: "km2", ConnectionID: &km, IdempotencyKey: "k8"}, run); !strings.Contains(msg, "run that wrote this report was deleted") {
		t.Errorf("no run = %s", msg)
	}
	if capitalize("") != "" || capitalize("Already") != "Already" {
		t.Error("capitalize")
	}
	old := time.Now().Add(-time.Hour)
	slack := "c1"
	if !Retryable(model.Report{}, model.ReportDelivery{Kind: "slack", ConnectionID: &slack, Status: model.DeliveryPending, UpdatedAt: old}, time.Now()) ||
		Retryable(model.Report{}, model.ReportDelivery{Kind: "slack", ConnectionID: &slack, Status: model.DeliveryPending, UpdatedAt: time.Now()}, time.Now()) ||
		Retryable(model.Report{DryRun: true}, model.ReportDelivery{Kind: "slack", ConnectionID: &slack, Status: model.DeliveryFailed}, time.Now()) ||
		Retryable(model.Report{}, model.ReportDelivery{Kind: "slack", Status: model.DeliveryFailed}, time.Now()) ||
		Retryable(model.Report{}, model.ReportDelivery{Kind: model.DestinationTracker, ConnectionID: &slack, Status: model.DeliveryFailed}, time.Now()) {
		t.Error("Retryable")
	}
}

func TestSubmitCaps(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	if _, err := f.svc.Submit(ctx, Submission{Scope: f.scope, Report: model.ReportRequest{Project: "", Markdown: "x"}}); err == nil {
		t.Error("empty project accepted")
	}
	for i := 0; i < model.MaxDeliveriesPerRun/4; i++ {
		f.submit(t, "P"+string(rune('a'+i)), "x")
	}
	_, err := f.svc.Submit(ctx, Submission{Scope: f.scope, Report: model.ReportRequest{Project: "Over", Markdown: "x"}})
	var capErr *CapError
	if !errors.As(err, &capErr) || !strings.Contains(capErr.Error(), "limit of 60") {
		t.Errorf("cap = %v", err)
	}
	// Without a schedule only the work order output gets reports, up to MaxReportsPerRun
	noSchedule := f.scope
	noSchedule.ScheduleID = nil
	if dests, err := f.svc.destinations(ctx, noSchedule); err != nil || len(dests) != 0 {
		t.Errorf("destinations = %v %v", dests, err)
	}
	if err := f.svc.withinCaps(ctx, f.scope.RunID, 0); err == nil || !strings.Contains(err.Error(), "already sent") {
		t.Logf("caps with no destinations: %v", err)
	}
	if got := footer(Submission{Scope: store.RunScope{CrewName: "PMO"}}); got != "Sent by Crew Studio (PMO)." {
		t.Errorf("footer = %q", got)
	}
}
