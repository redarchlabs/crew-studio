package chat

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
	"crewstudio/internal/tracker/trackertest"
)

// withTracker gives the env a fake Jira connection the PE crew may use, and the service a tracker service.
func withTracker(t *testing.T, e env) (model.Connection, *trackertest.Data) {
	t.Helper()
	ctx := context.Background()
	d := trackertest.NewData()
	d.Add(trackertest.Issue{Key: "PRJ-10", Title: "Atlas"})
	d.Add(trackertest.Issue{Key: "PRJ-11", Title: "Child", Parent: "PRJ-10"})
	srv := trackertest.Server(t, trackertest.Jira(d, "bot@example.com", "tok"))
	box, err := secrets.NewBox(secrets.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	conns := connections.New(e.st, box)
	conn, err := conns.Create(ctx, model.ConnectionInput{Name: "Acme Jira", Kind: "jira", AreaIDs: []string{financeArea},
		Config: map[string]string{"site_url": srv.URL, "email": "bot@example.com"}, Secrets: map[string]string{"api_token": "tok"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetCrewConnections(ctx, e.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	e.svc.UseTrackers(tracker.NewService(e.st, conns, tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client()}))
	return conn, d
}

func TestTrackingProposalsNormalize(t *testing.T) {
	add, _ := Lookup(TypeTrackingAdd)
	clean, summary, err := add.Normalize(map[string]any{"item_ref": "prj-10", "project": "Atlas", "connection": " Acme Jira "})
	if err != nil || clean["item_ref"] != "PRJ-10" || clean["include_children"] != "all" || clean["connection"] != "Acme Jira" ||
		summary != "Track PRJ-10 (Atlas), including all its child tickets, in Acme Jira" {
		t.Errorf("add = %v %q %v", clean, summary, err)
	}
	for _, bad := range []map[string]any{{"item_ref": "nope"}, {"item_ref": "A-1", "include_children": "some"}, {"item_ref": "A-1", "connection": "a\nb"},
		{"item_ref": "A-1", "extra": 1}} {
		if _, _, err := add.Normalize(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	remove, _ := Lookup(TypeTrackingRemove)
	if clean, summary, err := remove.Normalize(map[string]any{"item_ref": "jira:prj-10"}); err != nil || clean["item_ref"] != "PRJ-10" || summary != "Stop tracking PRJ-10" {
		t.Errorf("remove = %v %q %v", clean, summary, err)
	}
	if _, _, err := remove.Normalize(map[string]any{"item_ref": "?"}); err == nil {
		t.Error("bad ref accepted")
	}
	if add.Undo == nil || remove.Undo == nil || !slices.Contains(add.Needs, "state.manage") || !slices.Contains(remove.Needs, "state.manage") {
		t.Error("both can be undone and need state.manage")
	}
}

func TestTrackingProposalsApplyAndUndo(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	add, _ := Lookup(TypeTrackingAdd)
	req := Request{Principal: e.ann, Crew: e.crew, Payload: map[string]any{"item_ref": "PRJ-10", "project": "Atlas", "include_children": "direct",
		"connection": "", "note": "", "lead_account_id": ""}}
	if _, err := add.Apply(ctx, e.svc.env(), req); err == nil {
		t.Fatal("applied without trackers")
	}
	conn, _ := withTracker(t, e)
	result, err := add.Apply(ctx, e.svc.env(), req)
	if err != nil || result["item_ref"] != "PRJ-10" || result["connection"] != "Acme Jira" {
		t.Fatalf("add = %v %v", result, err)
	}
	items, _ := e.st.ListTrackingItems(ctx, store.TrackingFilter{CrewID: e.crew.ID})
	if len(items) != 1 || items[0].IncludeChildren != "direct" || items[0].AddedByName != "Ann" || items[0].ConnectionID != conn.ID {
		t.Fatalf("items = %+v", items)
	}
	// The same item again is refused; a missing one fails with the tracker's answer
	if _, err := add.Apply(ctx, e.svc.env(), req); !errors.Is(err, store.ErrAlreadyTracked) {
		t.Errorf("twice = %v", err)
	}
	missing := req
	missing.Payload = map[string]any{"item_ref": "PRJ-99", "connection": "acme jira"}
	if _, err := add.Apply(ctx, e.svc.env(), missing); !tracker.IsNotFound(err) {
		t.Errorf("missing = %v", err)
	}
	other := req
	other.Payload = map[string]any{"item_ref": "PRJ-10", "connection": "Other"}
	if _, err := add.Apply(ctx, e.svc.env(), other); err == nil || !strings.Contains(err.Error(), "no tracker connection named") {
		t.Errorf("unknown connection = %v", err)
	}

	// Remove, undo the removal (back on the list), then undo the original add (off again)
	remove, _ := Lookup(TypeTrackingRemove)
	rreq := Request{Principal: e.ann, Crew: e.crew, Payload: map[string]any{"item_ref": "PRJ-10", "connection": ""}}
	rresult, err := remove.Apply(ctx, e.svc.env(), rreq)
	if err != nil {
		t.Fatal(err)
	}
	if items, _ := e.st.ListTrackingItems(ctx, store.TrackingFilter{CrewID: e.crew.ID}); len(items) != 0 {
		t.Errorf("after remove = %+v", items)
	}
	if _, err := remove.Apply(ctx, e.svc.env(), rreq); err == nil || !strings.Contains(err.Error(), "not on the tracking list") {
		t.Errorf("remove twice = %v", err)
	}
	rreq.Result = rresult
	if err := remove.Undo(ctx, e.svc.env(), rreq); err != nil {
		t.Fatal(err)
	}
	req.Result = result
	if err := add.Undo(ctx, e.svc.env(), req); err != nil {
		t.Fatal(err)
	}
	if items, _ := e.st.ListTrackingItems(ctx, store.TrackingFilter{CrewID: e.crew.ID}); len(items) != 0 {
		t.Errorf("after undoing the add = %+v", items)
	}
	if err := add.Undo(ctx, e.svc.env(), req); err == nil {
		t.Error("undid twice")
	}
}

func TestChatTurnsGetTrackerToolsOnlyWithATracker(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.turnOn(t, nil, 1)
	turn, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "hi")
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := e.st.GetRunSpec(ctx, turn.Run.ID)
	if slices.Contains(spec.Chat.Tools, "tracker_get_issue") || slices.Contains(spec.Chat.Tools, "tracking_list_get") {
		t.Errorf("no tracker service: %v", spec.Chat.Tools)
	}
	withTracker(t, e)
	turn, err = e.svc.StartConversation(ctx, e.ann, e.crew.ID, "track PRJ-10")
	if err != nil {
		t.Fatal(err)
	}
	spec, _ = e.st.GetRunSpec(ctx, turn.Run.ID)
	for _, key := range []string{"tracking_list_get", "tracker_get_issue", "tracker_find_issues", "propose_tracking_add", "propose_tracking_remove"} {
		if !slices.Contains(spec.Chat.Tools, key) {
			t.Errorf("missing %s in %v", key, spec.Chat.Tools)
		}
	}
	m, err := e.svc.Propose(ctx, e.running(t, turn.Run.ID), TypeTrackingAdd, map[string]any{"item_ref": "PRJ-10", "project": "Atlas"})
	if err != nil || m.Proposal == nil || !strings.HasPrefix(m.Proposal.Summary, "Track PRJ-10 (Atlas)") {
		t.Errorf("propose = %+v %v", m.Proposal, err)
	}
}
