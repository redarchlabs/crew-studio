package mcpserver

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"crewstudio/internal/model"
)

const (
	connID     = "44444444-0000-4000-8000-000000000001"
	scheduleID = "55555555-0000-4000-8000-000000000001"
)

// trackerAPI answers like the API for one connection and one schedule's watch, and records every call.
type trackerAPI struct {
	mu    sync.Mutex
	calls []call
	watch map[string]any
}

func (f *trackerAPI) Do(_ context.Context, _ *auth.TokenInfo, req Request) (int, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := call{method: req.Method, path: req.Path}
	if req.Body != nil {
		_ = json.Unmarshal(req.Body, &c.body)
	}
	f.calls = append(f.calls, c)
	conn := map[string]any{"id": connID, "name": "PMO Sandbox", "kind": "km2_tracker", "enabled": true, "area_ids": []any{"a"},
		"config": map[string]any{"base_url": "https://km2.example.com", "self_email": "bot@example.com"}, "secret_keys": []any{"api_key"},
		"missing_secrets": []any{}, "updated_at": "2026-10-06T00:00:00Z"}
	switch req.Method + " " + req.Path {
	case "GET /api/connections/" + connID:
		return envelope(200, conn)
	case "PUT /api/connections/" + connID, "POST /api/connections":
		return envelope(200, c.body)
	case "GET /api/schedules/" + scheduleID + "/watch":
		return envelope(200, map[string]any{"watch": f.watch, "checks": []any{}})
	case "PUT /api/schedules/" + scheduleID + "/watch":
		saved := c.body
		saved["updated_at"] = "2026-10-06T01:00:00Z"
		f.watch = saved
		return envelope(200, map[string]any{"watch": saved, "checks": []any{}})
	case "POST /api/schedules/" + scheduleID + "/watch/preview":
		return envelope(200, map[string]any{"would_run": true})
	case "POST /api/schedules/" + scheduleID + "/run-now":
		return envelope(201, map[string]any{"id": "wo-1"})
	case "POST /api/connections/" + connID + "/test":
		return envelope(200, map[string]any{"ok": true, "message": "Connected as Crew Studio."})
	}
	if strings.HasPrefix(req.Path, "/api/connections/"+connID+"/people") || strings.HasPrefix(req.Path, "/api/crews/") || strings.HasPrefix(req.Path, "/api/pmo/") {
		return envelope(200, map[string]any{"path": req.Path})
	}
	return failure(404, "not_found", "Not found.")
}

func (f *trackerAPI) writes() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.method != "GET" {
			out = append(out, c)
		}
	}
	return out
}

func TestConnectionSecretsAreRefused(t *testing.T) {
	api := &trackerAPI{}
	cs := connect(t, api)
	creates := map[string]map[string]any{
		"secrets":   {"name": "K", "kind": "km2_tracker", "area_ids": []any{"a"}, "config": map[string]any{"base_url": "https://k.example"}, "secrets": map[string]any{"api_key": "km2_x"}},
		"api_key":   {"name": "K", "kind": "km2_tracker", "area_ids": []any{"a"}, "config": map[string]any{"base_url": "https://k.example", "api_key": "km2_x"}},
		"api_token": {"name": "J", "kind": "jira", "area_ids": []any{"a"}, "api_token": "t"},
		"password":  {"name": "M", "kind": "email", "area_ids": []any{"a"}, "config": map[string]any{"nested": map[string]any{"deeper": []any{map[string]any{"password": "p"}}}}},
	}
	for secret, fields := range creates {
		text, isErr := callTool(t, cs, "create_item", map[string]any{"kind": "connection", "fields": fields})
		if !isErr || !strings.Contains(text, `"`+secret+`" is a secret`) || !strings.Contains(text, secretsInUI) {
			t.Errorf("create with %s: %s", secret, text)
		}
	}
	for _, changes := range []map[string]any{
		{"config": map[string]any{"base_url": "https://k.example", "api_key": "km2_y"}},
		{"secrets": map[string]any{"api_key": "km2_y"}},
		{"webhook_url": "https://hooks.slack.com/services/x"},
		// encoding/json fills struct fields ignoring case, so other spellings must be refused too
		{"Secrets": map[string]any{"api_key": "km2_y"}},
		{"SECRETS": map[string]any{"api_key": "km2_y"}},
		{"config": map[string]any{"base_url": "https://k.example", "API_KEY": "km2_y"}},
	} {
		text, isErr := callTool(t, cs, "update_item", map[string]any{"kind": "connection", "id": connID, "changes": changes})
		if !isErr || !strings.Contains(text, secretsInUI) {
			t.Errorf("update %v: %s", changes, text)
		}
	}
	if len(api.calls) != 0 {
		t.Errorf("a refused secret still reached the API: %+v", api.calls)
	}
	// Settings alone go through
	text, isErr := callTool(t, cs, "update_item", map[string]any{"kind": "connection", "id": connID,
		"changes": map[string]any{"config": map[string]any{"base_url": "https://k.example", "self_email": "bot@example.com"}}})
	if isErr || len(api.writes()) != 1 || api.writes()[0].path != "/api/connections/"+connID {
		t.Errorf("settings update: %s %+v", text, api.writes())
	}
}

// No kind of connection has a setting named like another kind's secret, so refusing every secret name never refuses
// a real setting.
func TestNoSettingIsNamedLikeASecret(t *testing.T) {
	secret := secretKeys()
	for _, k := range model.ConnectionKinds {
		for _, f := range k.Fields {
			if !f.Secret && slices.Contains(secret, f.Key) {
				t.Errorf("%s setting %q is a secret's name elsewhere", k.Kind, f.Key)
			}
		}
	}
}

func TestDescribeConnectionListsEachKindsSettings(t *testing.T) {
	cs := connect(t, &trackerAPI{})
	text, isErr := callTool(t, cs, "describe_kind", map[string]any{"kind": "connection"})
	if isErr {
		t.Fatal(text)
	}
	var info struct {
		Fields []fieldInfo `json:"fields"`
		Extra  struct {
			Secrets string                 `json:"secrets"`
			Kinds   []connectionKindDetail `json:"connection_kinds"`
		} `json:"extra"`
	}
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatal(err)
	}
	var km2 *connectionKindDetail
	for i := range info.Extra.Kinds {
		if info.Extra.Kinds[i].Kind == model.TrackerKM2 {
			km2 = &info.Extra.Kinds[i]
		}
	}
	if km2 == nil || !strings.Contains(info.Extra.Secrets, secretsInUI) || !slices.Equal(km2.SecretsInUI, []string{"api_key"}) {
		t.Fatalf("describe = %s", text)
	}
	var keys []string
	for _, f := range km2.Config {
		keys = append(keys, f.Key)
	}
	if !slices.Equal(keys, []string{"base_url", "self_email", "entity_prefix", "activity_field", "issue_url_template", "allow_create"}) {
		t.Errorf("km2_tracker settings = %v", keys)
	}
	for _, f := range info.Fields {
		if f.Name == "kind" && !strings.Contains(f.Hint, "km2_tracker") {
			t.Errorf("kind hint = %q", f.Hint)
		}
		if f.Name == "secret_keys" && !f.ReadOnly {
			t.Error("secret_keys must be read-only")
		}
	}
}

func TestWatchIsAddressedByItsSchedule(t *testing.T) {
	api := &trackerAPI{}
	cs := connect(t, api)
	if text, isErr := callTool(t, cs, "list_items", map[string]any{"kind": "watch"}); !isErr || !strings.Contains(text, `get_item kind "watch"`) {
		t.Errorf("list watches = %s", text)
	}
	if text, isErr := callTool(t, cs, "get_item", map[string]any{"kind": "watch", "id": scheduleID}); !isErr || !strings.Contains(text, "create_item") {
		t.Errorf("no watch yet = %s", text)
	}
	def := map[string]any{"connection_id": connID, "source": map[string]any{"kind": "tracker", "project": "ERP", "comments": true}, "id_field": "key"}
	if text, isErr := callTool(t, cs, "create_item", map[string]any{"kind": "watch", "fields": def}); !isErr || !strings.Contains(text, "created for a schedule") {
		t.Errorf("create without the schedule = %s", text)
	}
	if text, isErr := callTool(t, cs, "create_item", map[string]any{"kind": "agent", "id": scheduleID, "fields": map[string]any{"name": "x"}}); !isErr ||
		!strings.Contains(text, "only applies to kind watch") {
		t.Errorf("id on another kind = %s", text)
	}
	text, isErr := callTool(t, cs, "create_item", map[string]any{"kind": "watch", "id": scheduleID, "fields": def})
	if isErr || !strings.Contains(text, `"project":"ERP"`) || strings.Contains(text, `"checks"`) {
		t.Fatalf("create = %s", text)
	}
	text, isErr = callTool(t, cs, "get_item", map[string]any{"kind": "watch", "id": scheduleID})
	if isErr || !strings.Contains(text, `"id_field":"key"`) {
		t.Errorf("get = %s", text)
	}
	text, isErr = callTool(t, cs, "update_item", map[string]any{"kind": "watch", "id": scheduleID, "changes": map[string]any{"dry_run": false},
		"expected_updated_at": "2026-10-06T01:00:00Z"})
	if isErr || !strings.Contains(text, `"field":"dry_run"`) {
		t.Errorf("update = %s", text)
	}
	last := api.writes()[len(api.writes())-1]
	if last.method != "PUT" || last.path != "/api/schedules/"+scheduleID+"/watch" || last.body["dry_run"] != false || last.body["id_field"] != "key" {
		t.Errorf("update sent %+v", last)
	}
	// Preview tries changes on top of the saved watch without saving them
	text, isErr = callTool(t, cs, "preview_watch", map[string]any{"schedule_id": scheduleID, "changes": map[string]any{"max_items": 50}})
	if isErr || !strings.Contains(text, "would_run") {
		t.Errorf("preview = %s", text)
	}
	last = api.writes()[len(api.writes())-1]
	if last.path != "/api/schedules/"+scheduleID+"/watch/preview" || last.body["max_items"] != 50.0 || last.body["id_field"] != "key" {
		t.Errorf("preview sent %+v", last)
	}
	if text, isErr := callTool(t, cs, "preview_watch", map[string]any{"schedule_id": scheduleID, "changes": map[string]any{"bogus": 1}}); !isErr ||
		!strings.Contains(text, `"bogus" is not a field`) {
		t.Errorf("preview bogus = %s", text)
	}
}

func TestTrackerActionsCallTheirEndpoints(t *testing.T) {
	api := &trackerAPI{}
	cs := connect(t, api)
	user := "66666666-0000-4000-8000-000000000001"
	steps := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"test_connection", map[string]any{"connection_id": connID}, "POST /api/connections/" + connID + "/test"},
		{"get_people_map", map[string]any{"connection_id": connID}, "GET /api/connections/" + connID + "/people"},
		{"sync_people_map", map[string]any{"connection_id": connID}, "POST /api/connections/" + connID + "/people/sync"},
		{"link_person", map[string]any{"connection_id": connID, "account_id": "acc:sam-1", "user_id": user}, "PUT /api/connections/" + connID + "/people/acc:sam-1"},
		{"list_crew_connections", map[string]any{"crew_id": crewID}, "GET /api/crews/" + crewID + "/connections"},
		{"set_crew_connections", map[string]any{"crew_id": crewID, "connection_ids": []any{connID}}, "PUT /api/crews/" + crewID + "/connections"},
		{"run_schedule_now", map[string]any{"schedule_id": scheduleID}, "POST /api/schedules/" + scheduleID + "/run-now"},
		{"get_pmo_setup_options", nil, "GET /api/pmo/setup"},
		{"setup_pmo", map[string]any{"fields": map[string]any{"area_ids": []any{"a"}, "tracker_connection_id": connID, "timezone": "UTC"}}, "POST /api/pmo/setup"},
	}
	for _, s := range steps {
		before := len(api.calls)
		if text, isErr := callTool(t, cs, s.tool, s.args); isErr {
			t.Errorf("%s: %s", s.tool, text)
			continue
		}
		got := api.calls[before]
		if got.method+" "+got.path != s.want {
			t.Errorf("%s called %s %s, want %s", s.tool, got.method, got.path, s.want)
		}
	}
	if api.calls[3].body["user_id"] != user || len(api.calls[5].body["connection_ids"].([]any)) != 1 {
		t.Errorf("bodies: %+v %+v", api.calls[3].body, api.calls[5].body)
	}
	bad := map[string]map[string]any{
		"link_person":     {"connection_id": connID, "account_id": "../../admin", "user_id": nil},
		"test_connection": {"connection_id": "../x"},
		"setup_pmo":       {"fields": map[string]any{"name": "P", "bogus": true}},
	}
	before := len(api.calls)
	for tool, args := range bad {
		if text, isErr := callTool(t, cs, tool, args); !isErr {
			t.Errorf("%s accepted %v: %s", tool, args, text)
		}
	}
	if len(api.calls) != before {
		t.Errorf("refused calls reached the API: %+v", api.calls[before:])
	}
}
