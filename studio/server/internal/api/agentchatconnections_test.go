package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/chat"
	"crewstudio/internal/km2/km2test"
	"crewstudio/internal/model"
)

// The run-state calls a crew-less agent-chat turn may make, matched exactly as the router matches routes.
func TestAgentChatRunRoutes(t *testing.T) {
	cases := []struct {
		method, path string
		allowed      bool
	}{
		{"POST", "/api/run-state/proposals", true},
		{"GET", "/api/run-state/control", true},
		{"GET", "/api/run-state/trackers", true},
		{"GET", "/api/run-state/trackers/projects", true},
		{"GET", "/api/run-state/trackers/issues", true},
		{"GET", "/api/run-state/trackers/issues/PRJ-11", true},
		{"GET", "/api/run-state/trackers/issues/PRJ-11/children", true},
		{"POST", "/api/run-state/km2/search", true},
		{"POST", "/api/run-state/km2/ask", true},
		{"POST", "/api/run-state/questions", true},
		{"GET", "/api/run-state/questions/q1", true},
		{"GET", "/api/run-state/questions", false},
		{"GET", "/api/run-state/questions/q1/answer", false},
		{"POST", "/api/run-state/questions/q1", false},
		{"GET", "/api/run-state/proposals", false},
		{"POST", "/api/run-state/proposals/x", false},
		{"POST", "/api/run-state/x/proposals", false},
		{"POST", "/api/run-state/trackers/issues/PRJ-11/comments", false},
		{"POST", "/api/run-state/trackers/issues/PRJ-11/suggestions", false},
		{"POST", "/api/run-state/trackers/issues", false},
		{"GET", "/api/run-state/trackers/issues/PRJ-11/children/x", false},
		{"POST", "/api/run-state/km2/documents", false},
		{"GET", "/api/run-state/km2/search", false},
		{"GET", "/api/run-state/scope", false},
		{"GET", "/api/run-state/notes", false},
		{"GET", "/api/run-state/tracking", false},
		{"POST", "/api/run-state/memory/remember", false},
		{"GET", "/api/run-state/control/x", false},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			if got := agentChatMayCall(httptest.NewRequest(tc.method, tc.path, nil)); got != tc.allowed {
				t.Errorf("agentChatMayCall = %v, want %v", got, tc.allowed)
			}
		})
	}
	// A path the router would decode differently from the allowlist (an encoded character) is refused outright
	for _, path := range []string{"/api/run-state/trackers/issues/PRJ%2D11", "/api/run-state/trackers/issues/PRJ-11%2Fcomments",
		"/api/run-state/km2%2Fsearch", "/api/run-state/questions/q1%2Fx"} {
		if agentChatMayCall(httptest.NewRequest("GET", path, nil)) || agentChatMayCall(httptest.NewRequest("POST", path, nil)) {
			t.Errorf("%s was allowed", path)
		}
	}
}

// agentChatTracker turns chat on for the deal partner with a tracker read and a tracker write among its tools and
// the given connections listed, as admin.
func (e trackerEnv) agentChatTracker(t *testing.T, connectionIDs ...string) {
	t.Helper()
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE agents SET tools = '{tracker_get_issue,tracker_comment,km_search}' WHERE id = $1`,
		chatPartner); err != nil {
		t.Fatal(err)
	}
	r := e.do(t, "PUT", "/api/agents/"+chatPartner+"/chat", map[string]any{"enabled": true, "llm_id": chatModel,
		"connection_ids": connectionIDs, "daily_budget_usd": 1, "person_daily_budget_usd": 3})
	wantStatus(t, r, http.StatusOK)
}

// An agent reads a tracker in chat through a connection on its list that the person can see. The turn may call only
// the read routes its tools allow (here Get Tracker Issue): other reads, writes, crew state and KM2 are refused, an
// encoded path is refused, the read is journalled, and the turn loses the connection when it is disabled.
func TestAgentChatReadsATrackerThroughItsConnection(t *testing.T) {
	e := newTrackerEnv(t)
	conn := e.jiraConnection(t, nil)
	e.agentChatTracker(t, conn.ID)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	r := e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "What is PRJ-11?"})
	wantStatus(t, r, http.StatusCreated)
	view := decodeData[conversationView](t, r)
	runID := *view.Messages[1].RunID
	spec, err := e.st.GetRunSpec(context.Background(), runID)
	// Its tracker write reaches chat only as the card that proposes it (Ann may run crews)
	if err != nil || !slices.Equal(spec.Chat.Tools, []string{"tracker_get_issue", chat.ProposeTrackerCommentTool}) || len(spec.Chat.Connections) != 1 ||
		spec.Chat.Connections[0].ID != conn.ID {
		t.Fatalf("spec tools = %v, connections = %+v, %v", spec.Chat.Tools, spec.Chat.Connections, err)
	}
	sr := e.runningTurn(t, model.Crew{}, runID)

	issue := decodeData[issueView](t, e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil))
	if issue.Key != "PRJ-11" || issue.Connection != conn.Name {
		t.Errorf("issue = %+v", issue)
	}
	wantStatus(t, e.call(t, sr, "GET", "/control", nil), http.StatusOK)
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues/PRJ%2D11", nil), http.StatusForbidden)
	// Reads its tools do not include, writes, crew state and KM2 without a KM2 connection are refused
	for _, call := range [][2]string{{"GET", "/trackers/issues/PRJ-10/children"}, {"GET", "/trackers/issues?project=PRJ"},
		{"POST", "/trackers/issues/PRJ-11/comments"}, {"GET", "/notes"}, {"GET", "/scope"}, {"GET", "/tracking"}, {"POST", "/km2/documents"}} {
		if got := e.call(t, sr, call[0], call[1], map[string]any{"text": "hi"}); got.status != http.StatusForbidden {
			t.Errorf("%s %s = %d", call[0], call[1], got.status)
		}
	}
	// Without a KM2 connection the turn was not given the KM2 read its agent has
	wantStatus(t, e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"}), http.StatusForbidden)
	if text := e.journalText(t, runID); !strings.Contains(text, "Tracker: read PRJ-11 in Acme Jira") {
		t.Errorf("journal = %s", text)
	}

	// Disabling the connection takes it away from the running turn
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE connections SET enabled = false WHERE id = $1`, conn.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil); got.status != http.StatusConflict || got.body.Error.Code != "no_tracker" {
		t.Errorf("disabled = %d %+v", got.status, got.body.Error)
	}
}

// The agent must still share an area with the connection: an agent moved out of it since the turn started reads nothing.
func TestAgentChatTrackerReadsFollowTheAgentsAreas(t *testing.T) {
	e := newTrackerEnv(t)
	conn := e.jiraConnection(t, nil)
	e.agentChatTracker(t, conn.ID)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea, hrArea}, nil)
	view := decodeData[conversationView](t, e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "PRJ-11?"}))
	sr := e.runningTurn(t, model.Crew{}, *view.Messages[1].RunID)
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil), http.StatusOK)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE agent_areas SET area_id = $2 WHERE agent_id = $1`, chatPartner, hrArea); err != nil {
		t.Fatal(err)
	}
	if got := e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil); got.status != http.StatusConflict || got.body.Error.Code != "no_tracker" {
		t.Errorf("after the agent left the area = %d %+v", got.status, got.body.Error)
	}
}

// With several tracker connections of one kind listed, the agent names the one to read; unnamed it is asked to choose.
func TestAgentChatPicksOneOfSeveralTrackers(t *testing.T) {
	e := newTrackerEnv(t)
	first := e.jiraConnection(t, nil)
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "Beta Jira", "kind": "jira", "area_ids": []string{financeArea},
		"config": map[string]string{"site_url": e.jira, "email": "bot@example.com"}, "secrets": map[string]string{"api_token": jiraTestToken}})
	wantStatus(t, r, http.StatusCreated)
	second := decodeData[model.Connection](t, r)
	e.agentChatTracker(t, first.ID, second.ID)
	view := decodeData[conversationView](t, e.do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "PRJ-11?"}))
	sr := e.runningTurn(t, model.Crew{}, *view.Messages[1].RunID)
	r = e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil)
	wantStatus(t, r, http.StatusBadRequest)
	if !strings.Contains(r.body.Error.Message, "Acme Jira, Beta Jira") {
		t.Errorf("unnamed = %+v", r.body.Error)
	}
	if issue := decodeData[issueView](t, e.call(t, sr, "GET", "/trackers/issues/PRJ-11?connection=Beta%20Jira", nil)); issue.Connection != "Beta Jira" {
		t.Errorf("by name read through %q", issue.Connection)
	}
	if issue := decodeData[issueView](t, e.call(t, sr, "GET", "/trackers/issues/PRJ-11?connection="+first.ID, nil)); issue.Connection != "Acme Jira" {
		t.Errorf("by id read through %q", issue.Connection)
	}
}

// The person chatting must still see the connection: one who lost the area since the turn started reads nothing.
func TestAgentChatTrackerReadsFollowThePerson(t *testing.T) {
	e := newTrackerEnv(t)
	conn := e.jiraConnection(t, nil)
	e.agentChatTracker(t, conn.ID)
	annID, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	view := decodeData[conversationView](t, e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "PRJ-11?"}))
	sr := e.runningTurn(t, model.Crew{}, *view.Messages[1].RunID)
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil), http.StatusOK)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE user_areas SET area_id = $2 WHERE user_id = $1`, annID, hrArea); err != nil {
		t.Fatal(err)
	}
	if got := e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil); got.status != http.StatusConflict || got.body.Error.Code != "no_tracker" {
		t.Errorf("after losing the area = %d %+v", got.status, got.body.Error)
	}
}

// An agent's chat connections: only tracker and KM2 kinds, at most ten, added only from the editor's areas; ones the
// editor cannot see are counted and kept.
func TestAgentChatConnectionSettings(t *testing.T) {
	e := newTrackerEnv(t)
	path := "/api/agents/" + chatPartner + "/chat"
	finance := e.jiraConnection(t, nil)
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "HR Jira", "kind": "jira", "area_ids": []string{hrArea},
		"config": map[string]string{"site_url": e.jira, "email": "bot@example.com"}, "secrets": map[string]string{"api_token": jiraTestToken}})
	wantStatus(t, r, http.StatusCreated)
	hr := decodeData[model.Connection](t, r)
	r = e.do(t, "POST", "/api/connections", map[string]any{"name": "Hook", "kind": "http", "area_ids": []string{financeArea},
		"config": map[string]string{"base_url": "https://hook.example"}})
	wantStatus(t, r, http.StatusCreated)
	hook := decodeData[model.Connection](t, r)
	settings := func(ids ...string) map[string]any {
		return map[string]any{"enabled": true, "daily_budget_usd": 1, "connection_ids": ids}
	}

	r = e.do(t, "PUT", path, settings(hook.ID, missingID))
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if details := strings.Join(r.body.Error.Details, " "); !strings.Contains(details, "Hook is a http connection") || !strings.Contains(details, missingID+" does not exist") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	saved := decodeData[model.AgentChatSettings](t, e.do(t, "PUT", path, settings(hr.ID, finance.ID)))
	if !slices.Equal(saved.ConnectionIDs, []string{hr.ID, finance.ID}) || saved.HiddenConnections != 0 {
		t.Errorf("saved = %+v", saved)
	}

	// A Finance builder sees the Finance connection and a count; saving keeps HR's, and cannot add one outside Finance
	_, builder := e.person(t, "bea@example.com", []string{builderRole}, []string{financeArea}, nil)
	seen := decodeData[model.AgentChatSettings](t, e.as(builder).do(t, "GET", path, nil))
	if !slices.Equal(seen.ConnectionIDs, []string{finance.ID}) || seen.HiddenConnections != 1 {
		t.Errorf("builder sees %+v", seen)
	}
	wantStatus(t, e.as(builder).do(t, "PUT", path, settings()), http.StatusOK)
	if all := decodeData[model.AgentChatSettings](t, e.do(t, "GET", path, nil)); !slices.Equal(all.ConnectionIDs, []string{hr.ID}) {
		t.Errorf("after the builder cleared theirs = %v", all.ConnectionIDs)
	}
	wantStatus(t, e.as(builder).do(t, "PUT", path, settings(finance.ID, hr.ID)), http.StatusOK)
	wantStatus(t, e.do(t, "PUT", path, settings()), http.StatusOK)
	r = e.as(builder).do(t, "PUT", path, settings(hr.ID))
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "outside your functional areas") {
		t.Errorf("details = %v", r.body.Error.Details)
	}

	// Adding a connection needs View connections; saving the ones already listed does not
	_, editor := e.person(t, "eddie@example.com", nil, []string{financeArea}, []string{"agents.view", "agents.edit"})
	wantStatus(t, e.do(t, "PUT", path, settings(finance.ID)), http.StatusOK)
	wantStatus(t, e.as(editor).do(t, "PUT", path, settings(finance.ID)), http.StatusOK)
	r = e.as(editor).do(t, "PUT", path, settings(finance.ID, hook.ID))
	wantStatus(t, r, http.StatusForbidden)

	many := make([]string, model.MaxChatConnections+1)
	for i := range many {
		many[i] = fmt.Sprintf("99999999-0000-4000-8000-%012d", i+1)
	}
	r = e.do(t, "PUT", path, settings(many...))
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !slices.Contains(r.body.Error.Details, fmt.Sprintf("connection_ids: at most %d", model.MaxChatConnections)) {
		t.Errorf("details = %v", r.body.Error.Details)
	}
}

// A specialist with a KM2 read searches KM2 through the agent's listed KM2 connection: the turn's tools are those of
// all its agents, not only the lead's.
func TestAgentChatSpecialistSearchesKM2(t *testing.T) {
	e := newKM2Env(t)
	ctx := context.Background()
	conn := e.km2Connection(t, "KM2 Finance")
	if _, err := e.st.Pool().Exec(ctx, `UPDATE agents SET tools = '{km_search}' WHERE id = $1`, chatInvestigator); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do(t, "PUT", "/api/agents/"+chatPartner+"/chat", map[string]any{"enabled": true, "llm_id": chatModel,
		"specialist_ids": []string{chatInvestigator}, "connection_ids": []string{conn.ID}, "daily_budget_usd": 1}), http.StatusOK)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	view := decodeData[conversationView](t, e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "Atlas?"}))
	runID := *view.Messages[1].RunID
	spec, err := e.st.GetRunSpec(ctx, runID)
	if err != nil || len(spec.Chat.Tools) != 0 || !slices.Equal(spec.Agents[1].Tools, []string{"km_search", chat.AskLeadTool}) {
		t.Fatalf("lead tools = %v, agents = %+v, %v", spec.Chat.Tools, spec.Agents, err)
	}
	sr := e.runningTurn(t, model.Crew{}, runID)
	r := e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"})
	wantStatus(t, r, http.StatusOK)
	if found := decodeData[km2SearchView](t, r); found.Connection != "KM2 Finance" {
		t.Errorf("search = %+v", found)
	}
	wantStatus(t, e.call(t, sr, "POST", "/km2/ask", map[string]any{"question": "Who owns Atlas?"}), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "POST", "/km2/documents", map[string]any{"title": "x"}), http.StatusForbidden)
}

// KM2 reads cannot choose a connection, so an agent lists at most one KM2 connection; the refusal never names a
// connection the editor cannot see.
func TestAgentChatListsOneKM2Connection(t *testing.T) {
	e := newTrackerEnv(t)
	ctx := context.Background()
	path := "/api/agents/" + chatPartner + "/chat"
	km2 := func(id, name, area string) string {
		t.Helper()
		c, err := e.st.CreateConnection(ctx, id, model.ConnectionInput{Name: name, Kind: model.KindKM2, AreaIDs: []string{area},
			Config: map[string]string{"base_url": "https://km2.example"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	finance := km2("99999999-0000-4000-8000-00000000000a", "Finance KM2", financeArea)
	hr := km2("99999999-0000-4000-8000-00000000000b", "HR KM2", hrArea)
	settings := func(ids ...string) map[string]any {
		return map[string]any{"enabled": true, "daily_budget_usd": 1, "connection_ids": ids}
	}
	r := e.do(t, "PUT", path, settings(finance, hr))
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if details := strings.Join(r.body.Error.Details, " "); !strings.Contains(details, "at most one KM2 connection") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	wantStatus(t, e.do(t, "PUT", path, settings(hr)), http.StatusOK)
	_, builder := e.person(t, "bea@example.com", []string{builderRole}, []string{financeArea}, nil)
	r = e.as(builder).do(t, "PUT", path, settings(finance))
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if details := strings.Join(r.body.Error.Details, " "); !strings.Contains(details, "at most one KM2 connection") || strings.Contains(details, "HR KM2") ||
		strings.Contains(details, hr) {
		t.Errorf("details name a hidden connection: %v", r.body.Error.Details)
	}
}

// A turn with two KM2 connections (frozen before the one-KM2 rule, say) picks among those the person can still see:
// both visible is ambiguous, in words for an agent; once the person loses one's area, the other is used.
func TestAgentChatKM2AmbiguityCountsOnlyVisibleConnections(t *testing.T) {
	e := newKM2Env(t)
	ctx := context.Background()
	finance := e.km2Connection(t, "KM2 Finance")
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "KM2 HR", "kind": "km2", "area_ids": []string{hrArea},
		"config": map[string]string{"base_url": e.url, model.ConfigKM2DefaultFolder: km2test.FolderAtlas}, "secrets": map[string]string{"api_key": km2TestKey}})
	wantStatus(t, r, http.StatusCreated)
	hr := decodeData[model.Connection](t, r)
	if _, err := e.st.Pool().Exec(ctx, `UPDATE agents SET tools = '{km_search}' WHERE id = $1`, chatPartner); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Pool().Exec(ctx, `INSERT INTO agent_areas (agent_id, area_id) VALUES ($1, $2)`, chatPartner, hrArea); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do(t, "PUT", "/api/agents/"+chatPartner+"/chat", map[string]any{"enabled": true, "llm_id": chatModel,
		"connection_ids": []string{finance.ID}, "daily_budget_usd": 1}), http.StatusOK)
	annID, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea, hrArea}, nil)
	view := decodeData[conversationView](t, e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "Atlas?"}))
	runID := *view.Messages[1].RunID
	frozen := `[{"id":"` + finance.ID + `","name":"KM2 Finance","kind":"km2"},{"id":"` + hr.ID + `","name":"KM2 HR","kind":"km2"}]`
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET spec = jsonb_set(spec, '{chat,connections}', $2::jsonb) WHERE id = $1`, runID, frozen); err != nil {
		t.Fatal(err)
	}
	sr := e.runningTurn(t, model.Crew{}, runID)
	r = e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "km2_ambiguous" || strings.Contains(r.body.Error.Message, "crew") || !strings.Contains(r.body.Error.Message, "KM2 Finance, KM2 HR") {
		t.Errorf("both visible = %+v", r.body.Error)
	}
	if _, err := e.st.Pool().Exec(ctx, `DELETE FROM user_areas WHERE user_id = $1 AND area_id = $2`, annID, hrArea); err != nil {
		t.Fatal(err)
	}
	r = e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"})
	wantStatus(t, r, http.StatusOK)
	if found := decodeData[km2SearchView](t, r); found.Connection != "KM2 Finance" {
		t.Errorf("search went through %q", found.Connection)
	}
}

// A turn started with an API key keeps the key's limits while it runs: once the key is revoked, it reads nothing.
func TestAgentChatTrackerReadsFollowTheKey(t *testing.T) {
	e := newTrackerEnv(t)
	conn := e.jiraConnection(t, nil)
	e.agentChatTracker(t, conn.ID)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea, hrArea}, []string{auth.AccountAPIKeys})
	created := decodeData[newKeyResponse](t, e.as(ann).do(t, "POST", "/api/account/api-keys", map[string]any{"name": "chat",
		"area_ids": []string{financeArea}}))
	view := decodeData[conversationView](t, e.as("").do(t, "POST", "/api/chat/conversations",
		map[string]any{"agent_id": chatPartner, "content": "PRJ-11?"}, "Authorization", "Bearer "+created.Secret))
	sr := e.runningTurn(t, model.Crew{}, *view.Messages[1].RunID)
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil), http.StatusOK)
	wantStatus(t, e.as(ann).do(t, "DELETE", "/api/account/api-keys/"+created.Key.ID, nil), http.StatusNoContent)
	if got := e.call(t, sr, "GET", "/trackers/issues/PRJ-11", nil); got.status != http.StatusConflict || got.body.Error.Code != "no_tracker" {
		t.Errorf("after the key was revoked = %d %+v", got.status, got.body.Error)
	}
}
