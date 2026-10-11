package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"crewstudio/internal/auth"
	"crewstudio/internal/connections"
	"crewstudio/internal/model"
)

const secretsInUI = "set secrets in the Crew Studio UI (Connections → Replace secret)"

func (e trackerEnv) adminMCP(t *testing.T) *mcp.ClientSession {
	t.Helper()
	cs, err := e.mcpSession(t, e.personalKey(t, e.token))
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func mcpChanges(t *testing.T, e testEnv) int {
	t.Helper()
	n := 0
	for _, ev := range decodeData[[]model.AuditEvent](t, e.do(t, "GET", "/api/admin/audit?limit=200", nil)) {
		if ev.Action == "mcp_change" {
			n++
		}
	}
	return n
}

// The whole set-up a Claude Code client does: a KM2 issues connection with settings only, the key pasted by a person
// through the REST "Replace secret", then the test, the people map, the crew's links, a watch on a project, its
// preview and one run.
func TestMCPSetsUpATrackerConnectionEndToEnd(t *testing.T) {
	e := newTrackerEnv(t)
	cs := e.adminMCP(t)
	text, isErr := mcpCall(t, cs, "create_item", map[string]any{"kind": "connection", "fields": map[string]any{
		"name": "PMO Sandbox", "kind": model.TrackerKM2, "area_ids": []string{financeArea},
		"config": map[string]any{"base_url": e.km2, "self_email": "BOT@example.com", "issue_url_template": "https://km2.example.com/r?id={id}"}}})
	if isErr {
		t.Fatalf("create = %s", text)
	}
	var conn model.Connection
	if err := json.Unmarshal([]byte(text), &conn); err != nil || conn.Config["self_email"] != "bot@example.com" ||
		!slices.Equal(conn.MissingSecrets, []string{"api_key"}) {
		t.Fatalf("connection = %s", text)
	}
	text, _ = mcpCall(t, cs, "test_connection", map[string]any{"connection_id": conn.ID})
	if strings.Contains(text, `"ok":true`) {
		t.Errorf("a connection without its key passed the test: %s", text)
	}
	// The person pastes the key in the UI
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+conn.ID+"/secret", map[string]any{"secrets": map[string]string{"api_key": km2TrackerKey}}), http.StatusOK)
	text, isErr = mcpCall(t, cs, "get_item", map[string]any{"kind": "connection", "id": conn.ID})
	if isErr || strings.Contains(text, km2TrackerKey) || !strings.Contains(text, `"secret_keys":["api_key"]`) {
		t.Errorf("get = %s", text)
	}
	text, isErr = mcpCall(t, cs, "test_connection", map[string]any{"connection_id": conn.ID})
	if isErr || !strings.Contains(text, "Connected as Studio Bot. The organisation has the tracker entities.") {
		t.Fatalf("test = %s", text)
	}
	text, isErr = mcpCall(t, cs, "sync_people_map", map[string]any{"connection_id": conn.ID})
	if isErr || !strings.Contains(text, `"read":2`) {
		t.Fatalf("sync = %s", text)
	}
	samID, _ := e.person(t, "samuel@example.com", []string{viewerRole}, []string{financeArea}, nil)
	text, isErr = mcpCall(t, cs, "link_person", map[string]any{"connection_id": conn.ID, "account_id": "acc-dana", "user_id": samID})
	if isErr || !strings.Contains(text, `"link":"manual"`) {
		t.Errorf("link = %s", text)
	}
	if text, _ := mcpCall(t, cs, "get_people_map", map[string]any{"connection_id": conn.ID}); !strings.Contains(text, "Dana Ruiz") {
		t.Errorf("people = %s", text)
	}

	sr := e.scheduledRun(t)
	text, isErr = mcpCall(t, cs, "set_crew_connections", map[string]any{"crew_id": sr.crew.ID, "connection_ids": []string{conn.ID}})
	if isErr || !strings.Contains(text, conn.ID) {
		t.Fatalf("crew connections = %s", text)
	}
	def := map[string]any{"connection_id": conn.ID, "id_field": "key", "source": map[string]any{"kind": "tracker", "project": "PRJ", "comments": true}}
	text, isErr = mcpCall(t, cs, "create_item", map[string]any{"kind": "watch", "id": sr.schedule.ID, "fields": def})
	if isErr || !strings.Contains(text, `"project":"PRJ"`) || !strings.Contains(text, `"dry_run":true`) {
		t.Fatalf("watch = %s", text)
	}
	text, isErr = mcpCall(t, cs, "preview_watch", map[string]any{"schedule_id": sr.schedule.ID})
	if isErr || !strings.Contains(text, "km2_tracker:PRJ-11") {
		t.Fatalf("preview = %s", text)
	}
	text, isErr = mcpCall(t, cs, "run_schedule_now", map[string]any{"schedule_id": sr.schedule.ID})
	if isErr || !strings.Contains(text, `"source":"schedule"`) || !strings.Contains(text, `"status":"queued"`) {
		t.Fatalf("run now = %s", text)
	}
	if n := mcpChanges(t, e.testEnv); n < 6 {
		t.Errorf("mcp_change entries = %d", n)
	}
}

func TestMCPRefusesConnectionSecretsAndWritesNothing(t *testing.T) {
	e := newTrackerEnv(t)
	conn := e.km2Connection(t)
	cs := e.adminMCP(t)
	before := decodeData[[]model.Connection](t, e.do(t, "GET", "/api/connections", nil))
	audited := mcpChanges(t, e.testEnv)
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"create_item", map[string]any{"kind": "connection", "fields": map[string]any{"name": "With secret", "kind": model.TrackerKM2, "area_ids": []string{financeArea},
			"config": map[string]any{"base_url": e.km2, "self_email": "a@b.example"}, "secrets": map[string]any{"api_key": "km2_leak"}}}},
		{"create_item", map[string]any{"kind": "connection", "fields": map[string]any{"name": "In config", "kind": model.TrackerKM2, "area_ids": []string{financeArea},
			"config": map[string]any{"base_url": e.km2, "self_email": "a@b.example", "api_key": "km2_leak"}}}},
		{"update_item", map[string]any{"kind": "connection", "id": conn.ID, "changes": map[string]any{"config": map[string]any{"base_url": e.km2,
			"self_email": "bot@example.com", "api_key": "km2_leak"}}}},
		{"update_item", map[string]any{"kind": "connection", "id": conn.ID, "changes": map[string]any{"config": map[string]any{"nested": map[string]any{"api_key": "km2_leak"}}}}},
		{"update_item", map[string]any{"kind": "connection", "id": conn.ID, "changes": map[string]any{"secrets": map[string]any{"api_key": "km2_leak"}}}},
	}
	for _, c := range cases {
		text, isErr := mcpCall(t, cs, c.tool, c.args)
		if !isErr || !strings.Contains(text, secretsInUI) {
			t.Errorf("%s %v: %s", c.tool, c.args, text)
		}
	}
	after := decodeData[[]model.Connection](t, e.do(t, "GET", "/api/connections", nil))
	if len(after) != len(before) || !after[0].UpdatedAt.Equal(before[0].UpdatedAt) || !slices.Equal(after[0].SecretKeys, []string{"api_key"}) {
		t.Errorf("connections changed: %+v -> %+v", before, after)
	}
	// The saved key is still the one the person pasted: the connection still tests fine
	if res := decodeData[connections.TestResult](t, e.do(t, "POST", "/api/connections/"+conn.ID+"/test", map[string]any{})); !res.OK {
		t.Errorf("test after refusals = %+v", res)
	}
	if n := mcpChanges(t, e.testEnv); n != audited {
		t.Errorf("refused calls were audited as changes: %d -> %d", audited, n)
	}
}

func TestMCPConnectionPermissionsMatchREST(t *testing.T) {
	e := newTrackerEnv(t)
	_, tok := e.person(t, "reader@example.com", []string{viewerRole}, []string{financeArea}, []string{auth.AccountAPIKeys, auth.ConnectionsView})
	body := map[string]any{"name": "Nope", "kind": model.TrackerKM2, "area_ids": []string{financeArea},
		"config": map[string]any{"base_url": e.km2, "self_email": "a@b.example"}}
	rest := e.as(tok).do(t, "POST", "/api/connections", body)
	wantStatus(t, rest, http.StatusForbidden)
	cs, err := e.mcpSession(t, e.personalKey(t, tok))
	if err != nil {
		t.Fatal(err)
	}
	text, isErr := mcpCall(t, cs, "create_item", map[string]any{"kind": "connection", "fields": body})
	if !isErr || !strings.Contains(text, rest.body.Error.Message) || !strings.Contains(text, "HTTP 403") {
		t.Errorf("MCP = %s, REST = %+v", text, rest.body.Error)
	}
	conn := e.km2Connection(t)
	if text, isErr := mcpCall(t, cs, "test_connection", map[string]any{"connection_id": conn.ID}); !isErr || !strings.Contains(text, "HTTP 403") {
		t.Errorf("test without connections.manage = %s", text)
	}
	if text, isErr := mcpCall(t, cs, "get_item", map[string]any{"kind": "connection", "id": conn.ID}); isErr || !strings.Contains(text, "Acme KM2") {
		t.Errorf("read with connections.view = %s", text)
	}
}

func TestMCPNewDestinationClearsTheSavedSecret(t *testing.T) {
	e := newTrackerEnv(t)
	cs := e.adminMCP(t)
	for _, change := range []map[string]any{
		{"base_url": "https://km2.example.org", "self_email": "bot@example.com"},
		{"base_url": e.km2, "self_email": "bot@example.com", "entity_prefix": "pm_"},
	} {
		conn := e.km2Connection(t)
		full := decodeData[model.Connection](t, e.do(t, "GET", "/api/connections/"+conn.ID, nil))
		text, isErr := mcpCall(t, cs, "update_item", map[string]any{"kind": "connection", "id": conn.ID, "changes": map[string]any{"config": change},
			"expected_updated_at": full.UpdatedAt})
		if isErr || !strings.Contains(text, `"saved":true`) {
			t.Fatalf("update %v = %s", change, text)
		}
		after := decodeData[model.Connection](t, e.do(t, "GET", "/api/connections/"+conn.ID, nil))
		if len(after.SecretKeys) != 0 || !slices.Equal(after.MissingSecrets, []string{"api_key"}) {
			t.Errorf("%v kept the key: %+v", change, after)
		}
		wantStatus(t, e.do(t, "DELETE", "/api/connections/"+conn.ID, nil), http.StatusNoContent)
	}
}
