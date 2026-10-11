package tracker_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
	"crewstudio/internal/tracker"
	"crewstudio/internal/tracker/trackertest"
)

var schemaSeq atomic.Int64

type svcEnv struct {
	st    *store.Store
	conns *connections.Service
	svc   *tracker.Service
	data  *trackertest.Data
	crew  model.Crew
	conn  model.Connection
}

// newSvcEnv is a crew in one area with a Jira connection (fake server) it may use.
func newSvcEnv(t *testing.T) svcEnv {
	t.Helper()
	ctx := context.Background()
	schema := fmt.Sprintf("tracker_svc_%d_%d", os.Getpid(), schemaSeq.Add(1))
	pool := testdb.New(t, schema)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
	st := store.New(pool)
	area, err := st.CreateArea(ctx, model.Area{Name: "Alpha"})
	must(t, err)
	llm, err := st.CreateLLM(ctx, model.LLM{Name: "L", Model: "openai/m"})
	must(t, err)
	agent, err := st.CreateAgent(ctx, model.Agent{Name: "A", Role: "r", Goal: "g", Backstory: "b", LLMID: &llm.ID, MaxIter: 3, Tools: []string{}, AreaIDs: []string{area.ID}})
	must(t, err)
	task, err := st.CreateTask(ctx, model.Task{Name: "T", Description: "d", ExpectedOutput: "e", AgentID: &agent.ID, AreaIDs: []string{area.ID}})
	must(t, err)
	crew, err := st.CreateCrew(ctx, model.Crew{Name: "C", Process: model.ProcessSequential, BudgetUSD: 1, AgentIDs: []string{agent.ID},
		TaskIDs: []string{task.ID}, AreaIDs: []string{area.ID}})
	must(t, err)
	box, err := secrets.NewBox(secrets.GenerateKey())
	must(t, err)
	conns := connections.New(st, box)
	d := seed()
	srv := trackertest.Server(t, trackertest.Jira(d, "bot@example.com", jiraToken))
	conn, err := conns.Create(ctx, model.ConnectionInput{Name: "Acme Jira", Kind: "jira", AreaIDs: []string{area.ID},
		Config: map[string]string{"site_url": srv.URL, "email": "bot@example.com"}, Secrets: map[string]string{"api_token": jiraToken}})
	must(t, err)
	svc := tracker.NewService(st, conns, tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client(), Sleep: noSleep})
	return svcEnv{st: st, conns: conns, svc: svc, data: d, crew: crew, conn: conn}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestServiceChecksTheCrewMayUseTheConnection(t *testing.T) {
	e := newSvcEnv(t)
	ctx := context.Background()
	if _, _, err := e.svc.ForCrew(ctx, e.conn.ID, e.crew.ID); err == nil || !strings.Contains(err.Error(), "may not use") {
		t.Fatalf("unlinked: %v", err)
	}
	must(t, e.st.SetCrewConnections(ctx, e.crew.ID, []string{e.conn.ID}, store.AllAreas))
	conn, client, err := e.svc.ForCrew(ctx, e.conn.ID, e.crew.ID)
	if err != nil || conn.ID != e.conn.ID || client.Kind() != "jira" {
		t.Fatalf("linked: %v", err)
	}
	if _, _, err := e.svc.ForCrew(ctx, "99999999-0000-4000-8000-000000000000", e.crew.ID); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("missing: %v", err)
	}
	check, err := e.svc.CheckItem(ctx, e.crew.ID, e.conn.ID, "PRJ-10")
	if err != nil || check.Issue.Title != "Atlas rollout" || check.DirectChildren != 2 || check.Connection.Name != "Acme Jira" {
		t.Errorf("check = %+v, %v", check, err)
	}
	if _, err := e.svc.CheckItem(ctx, e.crew.ID, e.conn.ID, "PRJ-99"); !tracker.IsNotFound(err) || !strings.Contains(err.Error(), "PRJ-99 was not found in Acme Jira") {
		t.Errorf("missing item: %v", err)
	}
	// Myself is remembered
	me, err := e.svc.Myself(ctx, e.conn.ID, client)
	before := len(e.data.RequestLog())
	again, err2 := e.svc.Myself(ctx, e.conn.ID, client)
	if err != nil || err2 != nil || me.AccountID != "me-1" || again != me || len(e.data.RequestLog()) != before {
		t.Errorf("myself = %+v %+v (%v %v), requests %d -> %d", me, again, err, err2, before, len(e.data.RequestLog()))
	}
	// A disabled or non-tracker connection is refused with a reason
	httpConn, err := e.conns.Create(ctx, model.ConnectionInput{Name: "API", Kind: "http", AreaIDs: e.crew.AreaIDs,
		Config: map[string]string{"base_url": "https://api.example"}})
	must(t, err)
	if problem := tracker.UseProblem(store.CrewConnectionUse{Connection: httpConn, Linked: true, SharesArea: true}); !strings.Contains(problem, "not a tracker") {
		t.Errorf("http problem = %q", problem)
	}
	disabled := e.conn
	disabled.Enabled = false
	if problem := tracker.UseProblem(store.CrewConnectionUse{Connection: disabled, Linked: true, SharesArea: true}); !strings.Contains(problem, "disabled") {
		t.Errorf("disabled problem = %q", problem)
	}
	if problem := tracker.UseProblem(store.CrewConnectionUse{Connection: e.conn, Linked: true}); !strings.Contains(problem, "shares no functional area") {
		t.Errorf("area problem = %q", problem)
	}
	if _, err := e.svc.Client(ctx, httpConn); err == nil {
		t.Error("an http connection made a tracker client")
	}
}

func TestServiceTestAndPeopleSync(t *testing.T) {
	e := newSvcEnv(t)
	ctx := context.Background()
	if res := e.svc.Test(ctx, e.conn); !res.OK || res.Message != "Connected as Studio Bot." {
		t.Errorf("test = %+v", res)
	}
	read, linked, err := e.svc.SyncPeople(ctx, e.conn)
	if err != nil || read != 2 || linked != 0 {
		t.Fatalf("sync = %d %d %v", read, linked, err)
	}
	people, err := e.st.ListTrackerPeople(ctx, e.conn.ID)
	if err != nil || len(people) != 2 || people[0].DisplayName != "Dana Ruiz" || people[1].Email != "sam@example.com" {
		t.Errorf("people = %+v %v", people, err)
	}
	e.data.Fail = 401
	if res := e.svc.Test(ctx, e.conn); res.OK || res.StatusCode != 401 || strings.Contains(res.Message, jiraToken) {
		t.Errorf("failed test = %+v", res)
	}
	if _, _, err := e.svc.SyncPeople(ctx, e.conn); err == nil {
		t.Error("a failing tracker synced")
	}
	if got := tracker.SafeMessage(context.DeadlineExceeded); got != "the tracker did not answer in time" {
		t.Errorf("deadline = %q", got)
	}
	if got := tracker.SafeMessage(fmt.Errorf("db password=x")); strings.Contains(got, "password") {
		t.Errorf("other errors are not passed on: %q", got)
	}
	if got := tracker.SafeMessage(&tracker.LimitError{Count: 9, Max: 3}); !strings.Contains(got, "more than 3") {
		t.Errorf("limit = %q", got)
	}
}

func TestServiceTestsAKM2Organisation(t *testing.T) {
	e := newSvcEnv(t)
	ctx := context.Background()
	k := trackertest.NewKM2(seed(), km2Key)
	srv := trackertest.Server(t, k.Handler())
	conn, err := e.conns.Create(ctx, model.ConnectionInput{Name: "Acme KM2", Kind: model.TrackerKM2, AreaIDs: e.crew.AreaIDs,
		Config: map[string]string{"base_url": srv.URL, "self_email": "bot@example.com"}, Secrets: map[string]string{"api_key": km2Key}})
	must(t, err)
	svc := tracker.NewService(e.st, e.conns, tracker.Options{HTTP: srv.Client(), Sleep: noSleep})
	if res := svc.Test(ctx, conn); !res.OK || res.Message != "Connected as Studio Bot. The organisation has the tracker entities." {
		t.Errorf("test = %+v", res)
	}
	k.CatalogEdit = func(entity string, f map[string]any) {
		if entity == "issue" && f["slug"] == "identifier" {
			f["is_unique"] = false
		}
	}
	if res := svc.Test(ctx, conn); !res.OK || !strings.HasSuffix(res.Message, "Warning: trk_issue.identifier is not unique, so two writers could make the same issue key; make it unique in KM2.") {
		t.Errorf("warning = %+v", res)
	}
	k.CatalogEdit = nil
	k.Hidden["issue"] = []string{"due_date"}
	k.Hidden["comment"] = nil
	res := svc.Test(ctx, conn)
	if res.OK || res.Message != "Connected as Studio Bot, but the KM2 organisation is not ready: trk_issue lacks the fields due_date; there is no trk_comment entity." {
		t.Errorf("test = %+v", res)
	}
	read, _, err := svc.SyncPeople(ctx, conn)
	if err != nil || read != 2 {
		t.Errorf("people = %d %v", read, err)
	}
}

// An agent-chat turn has no crew: its tracker connection is loaded without the crew checks (the run-state API has
// checked the turn froze it), but a disabled, missing or non-tracker connection is still refused.
func TestServiceForAgentChat(t *testing.T) {
	e := newSvcEnv(t)
	ctx := context.Background()
	conn, client, err := e.svc.ForAgentChat(ctx, e.conn.ID)
	if err != nil || conn.ID != e.conn.ID || client.Kind() != "jira" {
		t.Fatalf("agent chat: %v", err)
	}
	if _, _, err := e.svc.ForAgentChat(ctx, "99999999-0000-4000-8000-000000000000"); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("missing: %v", err)
	}
	httpConn, err := e.conns.Create(ctx, model.ConnectionInput{Name: "API", Kind: "http", AreaIDs: e.crew.AreaIDs,
		Config: map[string]string{"base_url": "https://api.example"}})
	must(t, err)
	if _, _, err := e.svc.ForAgentChat(ctx, httpConn.ID); err == nil || !strings.Contains(err.Error(), "not a tracker") {
		t.Errorf("http: %v", err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE connections SET enabled = false WHERE id = $1`, e.conn.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.ForAgentChat(ctx, e.conn.ID); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled: %v", err)
	}
}
