package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	agentID = "11111111-0000-4000-8000-000000000001"
	crewID  = "22222222-0000-4000-8000-000000000001"
	runID   = "33333333-0000-4000-8000-000000000001"
)

type call struct {
	method, path, idempotencyKey string
	body                         map[string]any
}

// fakeAPI answers like the Crew Studio API for one agent, the settings, and a few fixed endpoints.
type fakeAPI struct {
	mu       sync.Mutex
	calls    []call
	agent    map[string]any
	settings map[string]any
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		agent: map[string]any{"id": agentID, "name": "Scout", "role": "Researcher", "goal": "find things",
			"backstory": "long story", "llm_id": nil, "tools": []any{}, "max_iter": 5.0, "allow_delegation": false,
			"verbose": false, "area_ids": []any{"area-1"}, "created_at": "2026-10-01T00:00:00Z", "updated_at": "2026-10-02T00:00:00Z"},
		settings: map[string]any{"default_budget_usd": 1.0, "max_concurrent_runs": 2.0},
	}
}

func envelope(status int, data any) (int, []byte, error) {
	body, _ := json.Marshal(map[string]any{"success": status < 400, "data": data})
	return status, body, nil
}

func failure(status int, code, message string, details ...string) (int, []byte, error) {
	body, _ := json.Marshal(map[string]any{"success": false, "data": nil,
		"error": map[string]any{"code": code, "message": message, "details": details}})
	return status, body, nil
}

func (f *fakeAPI) Do(_ context.Context, _ *auth.TokenInfo, req Request) (int, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	method, path := req.Method, req.Path
	c := call{method: method, path: path, idempotencyKey: req.IdempotencyKey}
	if req.Body != nil {
		_ = json.Unmarshal(req.Body, &c.body)
	}
	f.calls = append(f.calls, c)
	switch method + " " + path {
	case "GET /api/auth/me":
		return envelope(200, map[string]any{"name": "Ada", "permissions": []string{"agents.view"}})
	case "GET /api/agents":
		return envelope(200, []any{f.agent})
	case "GET /api/agents/" + agentID:
		return envelope(200, f.agent)
	case "PUT /api/agents/" + agentID:
		if c.body["role"] == "" {
			return failure(422, "validation_failed", "Please fix the highlighted fields.", "role: is required")
		}
		c.body["updated_at"] = "2026-10-03T00:00:00Z"
		f.agent = c.body
		return envelope(200, f.agent)
	case "POST /api/agents":
		c.body["id"] = "new-id"
		return envelope(201, c.body)
	case "DELETE /api/agents/" + agentID:
		return http.StatusNoContent, nil, nil
	case "GET /api/settings":
		return envelope(200, f.settings)
	case "PUT /api/settings":
		f.settings = c.body
		return envelope(200, f.settings)
	case "GET /api/crews/" + crewID + "/validate":
		return envelope(200, map[string]any{"ok": true})
	case "POST /api/crews/" + crewID + "/runs":
		return envelope(201, map[string]any{"id": runID, "status": "queued"})
	case "POST /api/runs/" + runID + "/resume":
		return envelope(201, map[string]any{"id": "resumed", "status": "queued", "resumed_from_run_id": runID})
	case "GET /api/runs/" + runID:
		return envelope(200, map[string]any{"id": runID, "status": "succeeded", "output": "done", "spec": map[string]any{"huge": true}})
	case "GET /api/tools":
		return envelope(200, []any{map[string]any{"key": "google_search", "name": "Google Search", "description": "d", "available": true, "requires_env": []any{}}})
	}
	if strings.HasPrefix(path, "/api/history?") {
		return envelope(200, []any{map[string]any{"action": "update"}})
	}
	return failure(404, "not_found", "Not found.")
}

func (f *fakeAPI) last() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func connect(t *testing.T, api API) *mcp.ClientSession {
	t.Helper()
	serverT, clientT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := New(api, "test").Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callTool returns the tool's JSON text and whether the tool reported an error.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: content = %v", name, res.Content)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

// gotThrough reports whether a call got through: the input schema should have stopped it before the tool ran.
func gotThrough(cs *mcp.ClientSession, name string, args map[string]any) bool {
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	return err == nil && !res.IsError
}

func decodeJSON[T any](t *testing.T, text string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	return v
}

func TestToolListAndAnnotations(t *testing.T) {
	cs := connect(t, newFakeAPI())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"whoami", "describe_kind", "list_items", "get_item", "create_item", "update_item",
		"delete_item", "get_settings", "update_settings", "validate_crew", "start_run", "resume_run", "get_run", "list_history",
		"test_connection", "get_people_map", "sync_people_map", "link_person", "list_crew_connections", "set_crew_connections",
		"preview_watch", "run_schedule_now", "get_pmo_setup_options", "setup_pmo"} {
		if byName[name] == nil {
			t.Errorf("missing tool %s", name)
		}
	}
	if !byName["list_items"].Annotations.ReadOnlyHint || byName["update_item"].Annotations.ReadOnlyHint {
		t.Error("read-only hints wrong")
	}
	if d := byName["delete_item"].Annotations.DestructiveHint; d == nil || !*d {
		t.Error("delete_item must be marked destructive")
	}
	schema, _ := json.Marshal(byName["update_item"].InputSchema)
	if !strings.Contains(string(schema), `"enum":["agent","task","crew","llm","pricing","schedule","connection","watch"]`) {
		t.Errorf("update_item kind enum missing: %s", schema)
	}
}

func TestListItemsIsCompactAndFilters(t *testing.T) {
	cs := connect(t, newFakeAPI())
	text, isErr := callTool(t, cs, "list_items", map[string]any{"kind": "agent"})
	if isErr {
		t.Fatal(text)
	}
	items := decodeJSON[[]map[string]any](t, text)
	if len(items) != 1 || items[0]["name"] != "Scout" || items[0]["backstory"] != nil {
		t.Errorf("items = %v (summaries leave out long fields)", items)
	}
	text, _ = callTool(t, cs, "list_items", map[string]any{"kind": "agent", "name_contains": "nobody"})
	if text != "[]" {
		t.Errorf("filtered = %s", text)
	}
	text, _ = callTool(t, cs, "list_items", map[string]any{"kind": "tool", "name_contains": "SEARCH"})
	if tools := decodeJSON[[]map[string]any](t, text); len(tools) != 1 || tools[0]["key"] != "google_search" {
		t.Errorf("tools = %s", text)
	}
}

func TestGetItemValidatesIDs(t *testing.T) {
	api := newFakeAPI()
	cs := connect(t, api)
	text, isErr := callTool(t, cs, "get_item", map[string]any{"kind": "agent", "id": agentID})
	if isErr || decodeJSON[map[string]any](t, text)["backstory"] != "long story" {
		t.Errorf("get = %s", text)
	}
	before := len(api.calls)
	text, isErr = callTool(t, cs, "get_item", map[string]any{"kind": "agent", "id": "../admin/users"})
	if !isErr || !strings.Contains(text, "id must be a UUID") || len(api.calls) != before {
		t.Errorf("a non-UUID id must be refused before any request: %s", text)
	}
	text, isErr = callTool(t, cs, "get_item", map[string]any{"kind": "tool", "id": agentID})
	if !isErr || !strings.Contains(text, "list_items") {
		t.Errorf("tool by id = %s", text)
	}
}

func TestUpdateItemPatchesAndReportsChanges(t *testing.T) {
	api := newFakeAPI()
	cs := connect(t, api)

	text, isErr := callTool(t, cs, "update_item", map[string]any{"kind": "agent", "id": agentID,
		"changes": map[string]any{"goal": "find better things"}, "dry_run": true})
	if isErr {
		t.Fatal(text)
	}
	if api.last().method != "GET" || !strings.Contains(text, `"saved":false`) || !strings.Contains(text, "find better things") {
		t.Errorf("dry run = %s (last call %+v)", text, api.last())
	}

	text, isErr = callTool(t, cs, "update_item", map[string]any{"kind": "agent", "id": agentID,
		"changes": map[string]any{"goal": "find better things"}, "expected_updated_at": "2026-10-02T00:00:00Z"})
	if isErr {
		t.Fatal(text)
	}
	put := api.last()
	if put.method != "PUT" || put.body["backstory"] != "long story" || put.body["goal"] != "find better things" {
		t.Errorf("PUT body must be the full item with the change: %+v", put)
	}
	out := decodeJSON[updateResult](t, text)
	if !out.Saved || len(out.Changes) != 1 || out.Changes[0].Field != "goal" || out.UpdatedAt != "2026-10-03T00:00:00Z" {
		t.Errorf("result = %+v", out)
	}

	// Someone else saved since: updated_at is now 10-03
	text, isErr = callTool(t, cs, "update_item", map[string]any{"kind": "agent", "id": agentID,
		"changes": map[string]any{"goal": "x"}, "expected_updated_at": "2026-10-02T00:00:00Z"})
	if !isErr || !strings.Contains(text, "changed since") || api.last().method != "GET" {
		t.Errorf("stale write = %s", text)
	}

	text, isErr = callTool(t, cs, "update_item", map[string]any{"kind": "agent", "id": agentID, "changes": map[string]any{"role": ""}})
	if !isErr || !strings.Contains(text, "role: is required") {
		t.Errorf("validation errors must carry their details: %s", text)
	}
	text, isErr = callTool(t, cs, "update_item", map[string]any{"kind": "agent", "id": agentID, "changes": map[string]any{"rol": "x"}})
	if !isErr || !strings.Contains(text, `"rol" is not a field of agent`) {
		t.Errorf("typo = %s", text)
	}
}

func TestCreateAndDelete(t *testing.T) {
	api := newFakeAPI()
	cs := connect(t, api)
	text, isErr := callTool(t, cs, "create_item", map[string]any{"kind": "agent", "fields": map[string]any{"name": "New", "role": "r"}})
	if isErr || decodeJSON[map[string]any](t, text)["id"] != "new-id" || api.last().method != "POST" {
		t.Errorf("create = %s", text)
	}
	_, isErr = callTool(t, cs, "create_item", map[string]any{"kind": "agent", "fields": map[string]any{"id": "mine"}})
	if !isErr {
		t.Error("create must refuse server-set fields")
	}
	if gotThrough(cs, "create_item", map[string]any{"kind": "area", "fields": map[string]any{"name": "x"}}) {
		t.Error("areas are read-only over MCP")
	}
	text, isErr = callTool(t, cs, "delete_item", map[string]any{"kind": "agent", "id": agentID})
	if isErr || !strings.Contains(text, agentID) || api.last().method != "DELETE" {
		t.Errorf("delete = %s", text)
	}
}

func TestSettingsRunsAndHistory(t *testing.T) {
	api := newFakeAPI()
	cs := connect(t, api)
	text, isErr := callTool(t, cs, "update_settings", map[string]any{"changes": map[string]any{"max_concurrent_runs": 4}})
	if isErr || api.last().body["default_budget_usd"] != 1.0 || api.last().body["max_concurrent_runs"] != 4.0 {
		t.Errorf("settings = %s, PUT %+v", text, api.last())
	}
	if _, isErr = callTool(t, cs, "update_settings", map[string]any{"changes": map[string]any{"nope": 1}}); !isErr {
		t.Error("unknown settings fields must be refused")
	}

	text, isErr = callTool(t, cs, "validate_crew", map[string]any{"crew_id": crewID})
	if isErr || !strings.Contains(text, "ok") {
		t.Errorf("validate = %s", text)
	}
	text, isErr = callTool(t, cs, "start_run", map[string]any{"crew_id": crewID, "inputs": map[string]any{"topic": "x"},
		"idempotency_key": "retry-1"})
	if isErr || api.last().body["inputs"].(map[string]any)["topic"] != "x" || api.last().idempotencyKey != "retry-1" {
		t.Errorf("start = %s, %+v", text, api.last())
	}
	text, isErr = callTool(t, cs, "resume_run", map[string]any{"run_id": runID, "budget_usd": 2, "use_current": true,
		"idempotency_key": "resume-1"})
	if isErr || !strings.Contains(text, "resumed_from_run_id") || api.last().body["budget_usd"] != 2.0 ||
		api.last().body["use_current"] != true || api.last().idempotencyKey != "resume-1" {
		t.Errorf("resume = %s, %+v", text, api.last())
	}
	if gotThrough(cs, "resume_run", map[string]any{"run_id": "not-a-uuid"}) {
		t.Error("run_id must be a UUID")
	}
	text, isErr = callTool(t, cs, "get_run", map[string]any{"run_id": runID})
	if isErr || strings.Contains(text, "huge") || !strings.Contains(text, "done") {
		t.Errorf("get_run should drop the frozen spec: %s", text)
	}

	_, isErr = callTool(t, cs, "list_history", map[string]any{"kind": "agent", "id": agentID, "limit": 500})
	if isErr || api.last().path != "/api/history?entity_id="+agentID+"&entity_type=agent&limit=100" {
		t.Errorf("history path = %s", api.last().path)
	}
	if gotThrough(cs, "list_history", map[string]any{"kind": "tool"}) {
		t.Error("tools have no history")
	}
	text, isErr = callTool(t, cs, "whoami", nil)
	if isErr || !strings.Contains(text, "Ada") {
		t.Errorf("whoami = %s", text)
	}
}
