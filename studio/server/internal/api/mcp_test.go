package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"crewstudio/internal/auth"
	"crewstudio/internal/mcpserver"
	"crewstudio/internal/model"
)

// bearer adds an Authorization header to every request.
type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

func (e testEnv) mcpSession(t *testing.T, secret string) (*mcp.ClientSession, error) {
	t.Helper()
	transport := &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp", MaxRetries: -1,
		HTTPClient: &http.Client{Transport: bearer{token: secret, base: http.DefaultTransport}}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(), transport, nil)
	if err == nil {
		t.Cleanup(func() { _ = cs.Close() })
	}
	return cs, err
}

// mcpCall returns the tool's text and whether the tool reported an error.
func mcpCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func (e testEnv) personalKey(t *testing.T, sessionToken string) string {
	t.Helper()
	r := e.as(sessionToken).do(t, "POST", "/api/account/api-keys", map[string]any{"name": "claude"})
	wantStatus(t, r, http.StatusCreated)
	return decodeData[newKeyResponse](t, r).Secret
}

const mcpInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`

func rawMCP(t *testing.T, e testEnv, headers ...string) int {
	t.Helper()
	return rawMCPBody(t, e, mcpInitialize, headers...)
}

func rawMCPBody(t *testing.T, e testEnv, body string, headers ...string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestMCPNeedsAnAPIKey(t *testing.T) {
	e := newEnv(t)
	key := e.personalKey(t, e.token)

	if got := rawMCP(t, e); got != http.StatusUnauthorized {
		t.Errorf("no credentials = %d", got)
	}
	// A browser session must not reach MCP: a page on another site could otherwise drive it with the cookie
	if got := rawMCP(t, e, "Cookie", sessionCookie+"="+e.token); got != http.StatusUnauthorized {
		t.Errorf("session cookie = %d", got)
	}
	if got := rawMCP(t, e, "Authorization", "Bearer csk_nonsense"); got != http.StatusUnauthorized {
		t.Errorf("bad key = %d", got)
	}
	if got := rawMCP(t, e, "Authorization", "Bearer "+key, "Origin", "https://evil.example"); got != http.StatusForbidden {
		t.Errorf("foreign origin = %d", got)
	}
	if got := rawMCP(t, e, "Authorization", "Bearer "+key); got != http.StatusOK {
		t.Errorf("good key = %d", got)
	}
	if _, err := e.mcpSession(t, "csk_nonsense"); err == nil {
		t.Error("a client with a bad key connected")
	}
}

func TestMCPActsAsTheKeyOwner(t *testing.T) {
	e := newEnv(t)
	_, builderTok := e.person(t, "builder@example.com", []string{builderRole}, []string{itArea}, nil)
	_, viewerTok := e.person(t, "viewer@example.com", []string{viewerRole}, []string{itArea}, []string{auth.AccountAPIKeys})
	for _, a := range []struct{ name, area string }{{"IT helper", itArea}, {"Finance helper", financeArea}} {
		wantStatus(t, e.do(t, "POST", "/api/agents", map[string]any{"name": a.name, "role": "r", "goal": "g", "backstory": "b",
			"max_iter": 3, "area_ids": []string{a.area}}), http.StatusCreated)
	}

	builder, err := e.mcpSession(t, e.personalKey(t, builderTok))
	if err != nil {
		t.Fatal(err)
	}
	text, isErr := mcpCall(t, builder, "whoami", nil)
	if isErr || !strings.Contains(text, "builder@example.com") || !strings.Contains(text, `"via":"api_key"`) {
		t.Errorf("whoami = %s", text)
	}

	// Functional areas filter what the key sees
	text, _ = mcpCall(t, builder, "list_items", map[string]any{"kind": "agent", "name_contains": "helper"})
	var agents []model.Agent
	if err := json.Unmarshal([]byte(text), &agents); err != nil || len(agents) != 1 || agents[0].Name != "IT helper" {
		t.Fatalf("agents = %s", text)
	}
	itAgent := agents[0]

	full := decodeData[model.Agent](t, e.do(t, "GET", "/api/agents/"+itAgent.ID, nil))
	text, isErr = mcpCall(t, builder, "update_item", map[string]any{"kind": "agent", "id": itAgent.ID,
		"changes": map[string]any{"goal": "help IT faster"}, "expected_updated_at": full.UpdatedAt})
	if isErr || !strings.Contains(text, `"saved":true`) {
		t.Fatalf("update = %s", text)
	}
	after := decodeData[model.Agent](t, e.do(t, "GET", "/api/agents/"+itAgent.ID, nil))
	if after.Goal != "help IT faster" || after.Backstory != "b" || after.MaxIter != 3 {
		t.Errorf("only the goal should change: %+v", after)
	}

	// The change is in the item's history under the key owner's name, and in the audit log as an MCP change
	revs := decodeData[[]model.Revision](t, e.do(t, "GET", "/api/history?entity_type=agent&entity_id="+itAgent.ID, nil))
	if len(revs) != 2 || revs[0].ActorName != "builder@example.com" {
		t.Errorf("history = %+v", revs)
	}
	events := decodeData[[]model.AuditEvent](t, e.do(t, "GET", "/api/admin/audit?limit=50", nil))
	var mcpChanges []model.AuditEvent
	for _, ev := range events {
		if ev.Action == "mcp_change" {
			mcpChanges = append(mcpChanges, ev)
		}
	}
	if len(mcpChanges) != 1 || mcpChanges[0].Details["path"] != "/api/agents/"+itAgent.ID || mcpChanges[0].Details["status"] != 200.0 {
		t.Errorf("mcp audit events = %+v", mcpChanges)
	}

	// Items outside the key's areas are not found, as in the API
	finance := decodeData[[]model.Agent](t, e.do(t, "GET", "/api/agents", nil))
	for _, a := range finance {
		if a.Name == "Finance helper" {
			text, isErr = mcpCall(t, builder, "get_item", map[string]any{"kind": "agent", "id": a.ID})
			if !isErr || !strings.Contains(text, "HTTP 404") {
				t.Errorf("finance agent via IT key = %s", text)
			}
		}
	}

	// Permissions apply: a viewer can read but not change
	viewer, err := e.mcpSession(t, e.personalKey(t, viewerTok))
	if err != nil {
		t.Fatal(err)
	}
	if _, isErr = mcpCall(t, viewer, "get_item", map[string]any{"kind": "agent", "id": itAgent.ID}); isErr {
		t.Error("viewer cannot read")
	}
	text, isErr = mcpCall(t, viewer, "update_item", map[string]any{"kind": "agent", "id": itAgent.ID, "changes": map[string]any{"goal": "x"}})
	if !isErr || !strings.Contains(text, "permission") {
		t.Errorf("viewer update = %s", text)
	}
}

func TestMCPKeyStopsWhenRevoked(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "claude"})
	created := decodeData[newKeyResponse](t, r)
	cs, err := e.mcpSession(t, created.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, isErr := mcpCall(t, cs, "get_settings", nil); isErr {
		t.Fatal("settings refused")
	}
	wantStatus(t, e.do(t, "DELETE", "/api/account/api-keys/"+created.Key.ID, nil), http.StatusNoContent)
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_settings"}); err == nil {
		t.Error("a revoked key still works over an open MCP connection")
	}
}

func TestMCPRefusesBatches(t *testing.T) {
	e := newEnv(t)
	key := e.personalKey(t, e.token)
	if got := rawMCPBody(t, e, " ["+mcpInitialize+"]", "Authorization", "Bearer "+key); got != http.StatusBadRequest {
		t.Errorf("batch = %d, want 400", got)
	}
}

func TestMCPDispatcherSurvivesPanicsAndSpendsRateLimit(t *testing.T) {
	s := NewServer(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Options{})
	caller := &mcpauth.TokenInfo{Extra: map[string]any{mcpCallerKey: mcpCaller{principal: &auth.Principal{UserID: "u1"}}}}
	get := mcpserver.Request{Method: http.MethodGet, Path: "/api/anything"}

	panicky := mcpDispatcher{s: s, api: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })}
	if _, _, err := panicky.Do(context.Background(), caller, get); err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Errorf("panic = %v", err)
	}

	s.requests = newKeyedLimiter(0, 1)
	okay := mcpDispatcher{s: s, api: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { ok(w, "fine") })}
	if status, _, err := okay.Do(context.Background(), caller, get); err != nil || status != http.StatusOK {
		t.Fatalf("first call = %d, %v", status, err)
	}
	if _, _, err := okay.Do(context.Background(), caller, get); err == nil || !strings.Contains(err.Error(), "too many requests") {
		t.Errorf("second call past the limit = %v", err)
	}
	if _, _, err := okay.Do(context.Background(), nil, get); err == nil {
		t.Error("a call without a caller ran")
	}
}
