package api

import (
	"context"
	"net/http"
	"testing"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// A chat turn runs as the person chatting: its tracker tools may only use the crew's tracker connections that this
// person can see, even when the run's own scope (the crew's areas) holds more.
func TestChatTrackerToolsOnlyUseConnectionsThePersonSees(t *testing.T) {
	e := newTrackerEnv(t)
	ctx := context.Background()
	crew := e.chatCrew(t, 1)
	// The crew works for Finance and HR, so a run of it may use connections of either area
	crew.AreaIDs = []string{financeArea, hrArea}
	if _, err := e.st.UpdateCrew(store.WithActor(ctx, store.Actor{ID: e.adminID, Name: "Ada Admin"}), crew.ID, crew); err != nil {
		t.Fatal(err)
	}
	finance := e.jiraConnection(t, nil)
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "HR Jira", "kind": "jira", "area_ids": []string{hrArea},
		"config": map[string]string{"site_url": e.jira, "email": "bot@example.com"}, "secrets": map[string]string{"api_token": jiraTestToken}})
	wantStatus(t, r, http.StatusCreated)
	hr := decodeData[model.Connection](t, r)
	if err := e.st.SetCrewConnections(ctx, crew.ID, []string{finance.ID, hr.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	userID := e.adminID
	for _, conn := range []model.Connection{finance, hr} {
		if _, err := e.st.AddTrackingItem(ctx, crew.ID, model.TrackingItemInput{ConnectionID: conn.ID, ItemRef: "PRJ-10",
			Project: conn.Name, IncludeChildren: "all"}, &userID); err != nil {
			t.Fatal(err)
		}
	}

	// Olga works in Finance only: she sees the Finance Jira, not the HR one
	_, olga := e.person(t, "olga@example.com", []string{operatorRole}, []string{financeArea}, nil)
	_, sr := e.as(olga).chatTurn(t, crew, "What is slipping?")
	if len(sr.run.AreaIDs) != 2 {
		t.Fatalf("run areas = %v", sr.run.AreaIDs)
	}

	r = e.call(t, sr, "GET", "/trackers/issues/PRJ-11?connection=HR%20Jira", nil)
	wantStatus(t, r, http.StatusNotFound)
	r = e.call(t, sr, "GET", "/trackers/issues/PRJ-11?connection="+hr.ID, nil)
	wantStatus(t, r, http.StatusNotFound)
	r = e.call(t, sr, "GET", "/trackers/issues?project=PRJ&connection=HR%20Jira", nil)
	wantStatus(t, r, http.StatusNotFound)
	// With no connection named, the only one she can see is used (not "say which connection")
	issue := decodeData[issueView](t, e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil))
	if issue.Connection != finance.Name {
		t.Errorf("issue read through %q", issue.Connection)
	}
	// Listing the run's tracker connections is not a chat tool at all
	wantStatus(t, e.call(t, sr, "GET", "/trackers", nil), http.StatusForbidden)
	// The tracking list in chat leaves out items behind connections she cannot see
	items := decodeData[[]model.TrackingItem](t, e.call(t, sr, "GET", "/tracking", nil))
	if len(items) != 1 || items[0].ConnectionID != finance.ID {
		t.Errorf("tracking list = %+v", items)
	}

	// The admin sees both, so both work for the admin's own turn
	_, admin := e.chatTurn(t, crew, "And for HR?")
	wantStatus(t, e.call(t, admin, "GET", "/trackers/issues/PRJ-11?connection=HR%20Jira", nil), http.StatusOK)
	if items := decodeData[[]model.TrackingItem](t, e.call(t, admin, "GET", "/tracking", nil)); len(items) != 2 {
		t.Errorf("admin tracking list = %d items", len(items))
	}

	// A person who has lost access since the turn started (disabled) gets no tracker at all
	if _, err := e.st.Pool().Exec(ctx, `UPDATE users SET status = 'disabled' WHERE email = 'olga@example.com'`); err != nil {
		t.Fatal(err)
	}
	r = e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "no_tracker" {
		t.Errorf("disabled person: %+v", r.body.Error)
	}
}

// A chat turn's KM2 tools act as the crew's KM2 connection only while the person chatting can still see it.
func TestChatKM2ToolsNeedAConnectionThePersonSees(t *testing.T) {
	e := newTrackerEnv(t)
	ctx := context.Background()
	crew := e.chatCrew(t, 1)
	crew.AreaIDs = []string{financeArea, hrArea}
	if _, err := e.st.UpdateCrew(store.WithActor(ctx, store.Actor{ID: e.adminID, Name: "Ada Admin"}), crew.ID, crew); err != nil {
		t.Fatal(err)
	}
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "HR KM2", "kind": "km2", "area_ids": []string{hrArea},
		"config":  map[string]string{"base_url": "http://127.0.0.1:1", model.ConfigKM2DefaultFolder: "6a1f0000-0000-4000-8000-000000000001"},
		"secrets": map[string]string{"api_key": "km2_x"}})
	wantStatus(t, r, http.StatusCreated)
	km2Conn := decodeData[model.Connection](t, r)
	if err := e.st.SetCrewConnections(ctx, crew.ID, []string{km2Conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	// Hana works in Finance and HR when the turn starts, so it gets the KM2 tools
	hanaID, hana := e.person(t, "hana@example.com", []string{operatorRole}, []string{financeArea, hrArea}, nil)
	_, sr := e.as(hana).chatTurn(t, crew, "What does the charter say?")
	// She then leaves HR: the turn can no longer act as the HR KM2 connection
	if _, err := e.st.Pool().Exec(ctx, `DELETE FROM user_areas WHERE user_id = $1 AND area_id = $2`, hanaID, hrArea); err != nil {
		t.Fatal(err)
	}
	r = e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "charter"})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "no_km2" {
		t.Errorf("km2 = %+v", r.body.Error)
	}
}
