package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crewstudio/internal/mcpclient/mcptest"
	"crewstudio/internal/model"
)

const mcpTestSecret = "Bearer mcp-test-secret"

// mcpConnection makes an MCP connection to the fake server in the finance area, as admin.
func (e trackerEnv) mcpConnection(t *testing.T, fake *mcptest.Server) model.Connection {
	t.Helper()
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "Notes MCP", "kind": "mcp", "area_ids": []string{financeArea},
		"config": map[string]string{"url": fake.URL(), "auth_header": "Authorization"}, "secrets": map[string]string{"auth_value": mcpTestSecret}})
	wantStatus(t, r, http.StatusCreated)
	return decodeData[model.Connection](t, r)
}

func toolsByName(tools []model.ConnectionTool) map[string]model.ConnectionTool {
	out := map[string]model.ConnectionTool{}
	for _, tool := range tools {
		out[tool.Name] = tool
	}
	return out
}

// An administrator adds an MCP connection, tests it, discovers its tools (effects from the read-only hints) and sets
// effects; viewers only list them. A new server URL forgets the tools, and the secret never comes back.
func TestMCPConnectionTools(t *testing.T) {
	e := newTrackerEnv(t)
	fake := mcptest.New(t, mcptest.WithAuth("Authorization", mcpTestSecret))
	// Plain http to another machine would send the secret in clear
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "Evil MCP", "kind": "mcp", "area_ids": []string{financeArea},
		"config": map[string]string{"url": "http://mcp.example.com/mcp"}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	conn := e.mcpConnection(t, fake)
	path := "/api/connections/" + conn.ID

	test := decodeData[map[string]any](t, e.do(t, "POST", path+"/test", map[string]any{}))
	if test["ok"] != true || !strings.Contains(test["message"].(string), "offers 4 tools (3 say they only read)") {
		t.Errorf("test = %+v", test)
	}
	if got := decodeData[[]model.ConnectionTool](t, e.do(t, "GET", path+"/tools", nil)); len(got) != 0 {
		t.Errorf("tools before discovery = %+v", got)
	}
	r = e.do(t, "POST", path+"/tools/discover", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	if strings.Contains(string(r.body.Data), "mcp-test-secret") {
		t.Error("the secret came back")
	}
	view := decodeData[discoverToolsView](t, r)
	tools := toolsByName(view.Tools)
	if view.Found != 4 || len(view.Skipped) != 0 || tools[mcptest.ReadTool].Effect != model.EffectReads ||
		tools[mcptest.ChangeTool].Effect != model.EffectChanges || !tools[mcptest.ChangeTool].Enabled ||
		!strings.Contains(string(tools[mcptest.ReadTool].InputSchema), "query") {
		t.Fatalf("discovered = %+v", view)
	}

	// Setting effects: unknown names refuse the whole change
	wantStatus(t, e.do(t, "PUT", path+"/tools", map[string]any{"tools": []map[string]any{
		{"name": mcptest.FailTool, "effect": "reads", "enabled": false}, {"name": "nope", "effect": "reads", "enabled": true}}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "PUT", path+"/tools", map[string]any{"tools": []map[string]any{{"name": mcptest.FailTool, "effect": "proposes"}}}),
		http.StatusUnprocessableEntity)
	r = e.do(t, "PUT", path+"/tools", map[string]any{"tools": []map[string]any{
		{"name": mcptest.FailTool, "effect": "reads", "enabled": false}, {"name": mcptest.BigTool, "effect": "changes", "enabled": true}}})
	wantStatus(t, r, http.StatusOK)
	tools = toolsByName(decodeData[[]model.ConnectionTool](t, r))
	if tools[mcptest.FailTool].Enabled || tools[mcptest.BigTool].Effect != model.EffectChanges || tools[mcptest.BigTool].UpdatedByName == "" {
		t.Errorf("after set = %+v", tools)
	}
	// Discovering again keeps what the administrator set for unchanged tools
	tools = toolsByName(decodeData[discoverToolsView](t, e.do(t, "POST", path+"/tools/discover", map[string]any{})).Tools)
	if tools[mcptest.FailTool].Enabled || tools[mcptest.BigTool].Effect != model.EffectChanges {
		t.Errorf("after rediscovery = %+v", tools)
	}

	// A viewer lists the tools but cannot discover or change them
	_, viewer := e.person(t, "viewer@example.com", []string{operatorRole}, []string{financeArea}, []string{"connections.view"})
	wantStatus(t, e.as(viewer).do(t, "GET", path+"/tools", nil), http.StatusOK)
	wantStatus(t, e.as(viewer).do(t, "POST", path+"/tools/discover", map[string]any{}), http.StatusForbidden)
	wantStatus(t, e.as(viewer).do(t, "PUT", path+"/tools", map[string]any{"tools": []map[string]any{{"name": mcptest.ReadTool, "effect": "reads", "enabled": true}}}),
		http.StatusForbidden)
	// Other kinds have no tools
	jira := e.jiraConnection(t, nil)
	if got := e.do(t, "GET", "/api/connections/"+jira.ID+"/tools", nil); got.status != http.StatusUnprocessableEntity || got.body.Error.Code != "not_mcp" {
		t.Errorf("jira tools = %d %+v", got.status, got.body.Error)
	}

	// A new server URL forgets the tools (they describe the old server) and the secret
	r = e.do(t, "PUT", path, map[string]any{"name": "Notes MCP", "area_ids": []string{financeArea},
		"config": map[string]string{"url": fake.URL() + "/v2", "auth_header": "Authorization"}})
	wantStatus(t, r, http.StatusOK)
	if updated := decodeData[model.Connection](t, r); len(updated.SecretKeys) != 0 {
		t.Errorf("secret kept after a new URL: %+v", updated)
	}
	if got := decodeData[[]model.ConnectionTool](t, e.do(t, "GET", path+"/tools", nil)); len(got) != 0 {
		t.Errorf("tools kept after a new URL: %+v", got)
	}
	// Without the secret now, the header has no value: discovery says so instead of calling without it
	if got := e.do(t, "POST", path+"/tools/discover", map[string]any{}); got.status != http.StatusBadGateway || !strings.Contains(got.body.Error.Message, "no auth header value") {
		t.Errorf("discover without secret = %d %+v", got.status, got.body.Error)
	}
}

// A server that refuses the credentials, or is not there, gives a clear failure on Test and discovery.
func TestMCPConnectionFailures(t *testing.T) {
	e := newTrackerEnv(t)
	fake := mcptest.New(t, mcptest.WithAuth("Authorization", "Bearer other"))
	conn := e.mcpConnection(t, fake)
	test := decodeData[map[string]any](t, e.do(t, "POST", "/api/connections/"+conn.ID+"/test", map[string]any{}))
	if test["ok"] != false || !strings.Contains(test["message"].(string), "refused the credentials (HTTP 401)") {
		t.Errorf("test = %+v", test)
	}
	if got := e.do(t, "POST", "/api/connections/"+conn.ID+"/tools/discover", map[string]any{}); got.status != http.StatusBadGateway || got.body.Error.Code != "mcp_failed" {
		t.Errorf("discover = %d %+v", got.status, got.body.Error)
	}
}

// startMCPChat lists the MCP connection (tools discovered) for the deal partner and starts a turn as ann.
func (e trackerEnv) startMCPChat(t *testing.T, conn model.Connection) (string, stateRun) {
	t.Helper()
	wantStatus(t, e.do(t, "POST", "/api/connections/"+conn.ID+"/tools/discover", map[string]any{}), http.StatusOK)
	e.agentChatTracker(t, conn.ID)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	r := e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "Search my notes"})
	wantStatus(t, r, http.StatusCreated)
	runID := *decodeData[conversationView](t, r).Messages[1].RunID
	return runID, e.runningTurn(t, model.Crew{}, runID)
}

// An agent-chat turn gets the connection's enabled reading tools, frozen into its spec; it calls them through Studio,
// which sends the stored secret, cuts big results, journals the call and checks the tool and connection on every call.
func TestAgentChatCallsMCPReadTools(t *testing.T) {
	e := newTrackerEnv(t)
	fake := mcptest.New(t, mcptest.WithAuth("Authorization", mcpTestSecret))
	conn := e.mcpConnection(t, fake)
	runID, sr := e.startMCPChat(t, conn)
	spec, err := e.st.GetRunSpec(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range spec.Chat.MCPTools {
		names = append(names, tool.Name)
		if tool.ConnectionID != conn.ID || tool.ConnectionName != "Notes MCP" {
			t.Errorf("frozen tool = %+v", tool)
		}
	}
	if strings.Join(names, ",") != "big_result,fail_tool,search_notes" || len(spec.Chat.Connections) != 1 || spec.Chat.Connections[0].Kind != model.KindMCP {
		t.Fatalf("frozen = %v, connections = %+v", names, spec.Chat.Connections)
	}
	if raw, _ := json.Marshal(spec); strings.Contains(string(raw), "mcp-test-secret") {
		t.Fatal("the secret reached the spec")
	}
	base := "/mcp/" + conn.ID + "/tools/"

	res := decodeData[model.MCPCallResult](t, e.call(t, sr, "POST", base+mcptest.ReadTool, map[string]any{"arguments": map[string]any{"query": "atlas"}}))
	if res.Content != "notes for atlas" || res.IsError || res.Connection != "Notes MCP" {
		t.Errorf("read = %+v", res)
	}
	if calls := fake.Calls(); len(calls) != 1 || calls[0].Auth != mcpTestSecret {
		t.Errorf("calls = %+v", calls)
	}
	big := decodeData[model.MCPCallResult](t, e.call(t, sr, "POST", base+mcptest.BigTool, map[string]any{}))
	if !big.Truncated || len(big.Content) > model.MCPResultMaxBytes {
		t.Errorf("big = %d bytes, truncated %v", len(big.Content), big.Truncated)
	}
	if failed := decodeData[model.MCPCallResult](t, e.call(t, sr, "POST", base+mcptest.FailTool, map[string]any{})); !failed.IsError {
		t.Errorf("fail = %+v", failed)
	}
	// A changing tool was not frozen; arguments must be an object; an encoded path never matches the allowlist
	wantStatus(t, e.call(t, sr, "POST", base+mcptest.ChangeTool, map[string]any{}), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "POST", base+mcptest.ReadTool, map[string]any{"arguments": []int{1}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/mcp/"+conn.ID+"/tools/search%5Fnotes", map[string]any{}), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "POST", "/mcp/not-a-uuid/tools/search_notes", map[string]any{}), http.StatusForbidden)
	if text := e.journalText(t, runID); !strings.Contains(text, "MCP: called search_notes on Notes MCP: 15 bytes") ||
		!strings.Contains(text, "cut to 64 KB") || !strings.Contains(text, "fail_tool on Notes MCP: the tool reported a failure") {
		t.Errorf("journal = %s", text)
	}

	// An administrator marks the tool as changing things: the running turn can no longer call it
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+conn.ID+"/tools", map[string]any{"tools": []map[string]any{
		{"name": mcptest.ReadTool, "effect": "changes", "enabled": true}}}), http.StatusOK)
	if got := e.call(t, sr, "POST", base+mcptest.ReadTool, map[string]any{}); got.status != http.StatusForbidden || got.body.Error.Code != "mcp_tool_off" {
		t.Errorf("after marking changes = %d %+v", got.status, got.body.Error)
	}
	// Disabling the connection takes it away
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE connections SET enabled = false WHERE id = $1`, conn.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.call(t, sr, "POST", base+mcptest.BigTool, map[string]any{}); got.status != http.StatusConflict || got.body.Error.Code != "no_mcp" {
		t.Errorf("disabled = %d %+v", got.status, got.body.Error)
	}
	if len(fake.Calls()) != 3 {
		t.Errorf("the server was called %d times", len(fake.Calls()))
	}
}

// A crew run never reaches MCP tools, even with a frozen-looking path.
func TestCrewRunsCannotCallMCPTools(t *testing.T) {
	e := newTrackerEnv(t)
	fake := mcptest.New(t)
	conn := e.mcpConnection(t, fake)
	sr := e.scheduledRun(t)
	if got := e.call(t, sr, "POST", "/mcp/"+conn.ID+"/tools/"+mcptest.ReadTool, map[string]any{}); got.status != http.StatusForbidden {
		t.Errorf("crew run = %d", got.status)
	}
	if len(fake.Calls()) != 0 {
		t.Error("the server was called")
	}
}

// The MCP route is on the allowlist for crew-less runs only in its strict form.
func TestAgentChatMCPRoute(t *testing.T) {
	const id = "0f8fad5b-d9cb-469f-a165-70867728950e"
	for path, allowed := range map[string]bool{
		"/api/run-state/mcp/" + id + "/tools/search_notes":                true,
		"/api/run-state/mcp/" + id + "/tools/a.b-c_d":                     true,
		"/api/run-state/mcp/" + id + "/tools/":                            false,
		"/api/run-state/mcp/" + id + "/tools/a/b":                         false,
		"/api/run-state/mcp/" + id + "/tools/a b":                         false,
		"/api/run-state/mcp/" + strings.ToUpper(id) + "/tools/x":          false,
		"/api/run-state/mcp/x/tools/search_notes":                         false,
		"/api/run-state/mcp/" + id + "/tools/" + strings.Repeat("n", 129): false,
		"/api/run-state/mcp/" + id:                                        false,
	} {
		req := httptest.NewRequest("POST", "/", nil)
		req.URL.Path = path
		if got := agentChatMayCall(req); got != allowed {
			t.Errorf("%s = %v, want %v", path, got, allowed)
		}
		req.Method = "GET"
		if agentChatMayCall(req) {
			t.Errorf("GET %s allowed", path)
		}
	}
}
