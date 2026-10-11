package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

const unknownArea = "55555555-0000-4000-8000-0000000000ff"

func TestAPIKeyLimitsNarrowTheKey(t *testing.T) {
	e := newEnv(t)
	_, tok := e.person(t, "builder@example.com", []string{builderRole}, []string{itArea, financeArea}, nil)
	me := e.as(tok)

	created := decodeData[newKeyResponse](t, me.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "read crews",
		"allow_mcp": false, "permissions": []string{auth.CrewsView, auth.CrewsView}, "area_ids": []string{itArea}}))
	k := created.Key
	if !k.AllowAPI || k.AllowMCP || !slices.Equal(k.Permissions, []string{auth.CrewsView}) || !slices.Equal(k.AreaIDs, []string{itArea}) {
		t.Fatalf("created = %+v", k)
	}
	key := "Bearer " + created.Secret
	api := e.as("")

	who := decodeData[meResponse](t, api.do(t, "GET", "/api/auth/me", nil, "Authorization", key))
	if !slices.Equal(who.Permissions, []string{auth.CrewsView}) || who.AllAreas || !slices.Equal(who.AreaIDs, []string{itArea}) {
		t.Errorf("me via key = %+v", who)
	}
	wantStatus(t, api.do(t, "GET", "/api/crews", nil, "Authorization", key), http.StatusOK)
	wantStatus(t, api.do(t, "GET", "/api/agents", nil, "Authorization", key), http.StatusForbidden)
	wantStatus(t, api.do(t, "POST", "/api/agents", map[string]any{"name": "x", "role": "r", "goal": "g", "backstory": "b",
		"area_ids": []string{itArea}}, "Authorization", key), http.StatusForbidden)
	// The key is for the REST API only
	if got := rawMCP(t, e, "Authorization", key); got != http.StatusForbidden {
		t.Errorf("MCP with an API-only key = %d", got)
	}

	// Lists show each key's limits; a key made without limits keeps everything, on both channels
	open := decodeData[newKeyResponse](t, me.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "everything"}))
	if !open.Key.AllowAPI || !open.Key.AllowMCP || open.Key.Permissions != nil || open.Key.AreaIDs != nil {
		t.Errorf("unlimited key = %+v", open.Key)
	}
	keys := decodeData[[]model.AccountAPIKey](t, me.do(t, "GET", "/api/account/api-keys", nil))
	if len(keys) != 2 || keys[1].ID != k.ID || keys[1].AllowMCP || !slices.Equal(keys[1].Permissions, []string{auth.CrewsView}) {
		t.Errorf("keys = %+v", keys)
	}
	all := decodeData[meResponse](t, api.do(t, "GET", "/api/auth/me", nil, "Authorization", "Bearer "+open.Secret))
	if len(all.Permissions) < 10 || len(all.AreaIDs) != 2 {
		t.Errorf("me via unlimited key = %+v", all)
	}
}

func TestAPIKeyLimitsCannotExceedTheAccount(t *testing.T) {
	e := newEnv(t)
	_, tok := e.person(t, "builder@example.com", []string{builderRole}, []string{itArea}, nil)
	me := e.as(tok)
	for name, body := range map[string]map[string]any{
		"a permission you lack":     {"permissions": []string{auth.CrewsView, auth.AdminUsers}},
		"an unknown permission":     {"permissions": []string{"crews.everything"}},
		"no permissions":            {"permissions": []string{}},
		"an area you are not in":    {"area_ids": []string{financeArea}},
		"an area that is not an id": {"area_ids": []string{"finance"}},
		"no areas":                  {"area_ids": []string{}},
		"neither channel":           {"allow_api": false, "allow_mcp": false},
	} {
		body["name"] = "too much"
		r := me.do(t, "POST", "/api/account/api-keys", body)
		wantStatus(t, r, http.StatusUnprocessableEntity)
		if r.body.Error == nil || len(r.body.Error.Details) == 0 {
			t.Errorf("%s: error = %+v", name, r.body.Error)
		}
	}
	if keys := decodeData[[]model.APIKey](t, me.do(t, "GET", "/api/account/api-keys", nil)); len(keys) != 0 {
		t.Errorf("refused requests made keys: %+v", keys)
	}

	// Service accounts: an admin can only give a key what the service account holds
	sa := decodeData[model.User](t, e.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "service", Name: "ERP",
		RoleIDs: []string{viewerRole}, AreaIDs: []string{itArea}}))
	base := "/api/admin/users/" + sa.ID + "/api-keys"
	wantStatus(t, e.do(t, "POST", base, map[string]any{"name": "x", "permissions": []string{auth.CrewsEdit}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", base, map[string]any{"name": "x", "area_ids": []string{financeArea}}), http.StatusUnprocessableEntity)
	ok := decodeData[newKeyResponse](t, e.do(t, "POST", base, map[string]any{"name": "x", "permissions": []string{auth.CrewsView},
		"allow_api": false}))
	if ok.Key.AllowAPI || !ok.Key.AllowMCP {
		t.Errorf("service key = %+v", ok.Key)
	}

	// An all-areas account may limit a key to any area that exists, but not to one that does not
	r := e.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "finance only", "area_ids": []string{financeArea}})
	wantStatus(t, r, http.StatusCreated)
	wantStatus(t, e.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "x", "area_ids": []string{unknownArea}}), http.StatusUnprocessableEntity)
	who := decodeData[meResponse](t, e.as("").do(t, "GET", "/api/auth/me", nil, "Authorization", "Bearer "+decodeData[newKeyResponse](t, r).Secret))
	if who.AllAreas || !slices.Equal(who.AreaIDs, []string{financeArea}) {
		t.Errorf("admin's finance key = %+v", who)
	}
}

// A key's limits are checked against the account on every request, so taking access from the account takes it
// from its keys too.
func TestAPIKeyLimitsFollowTheAccount(t *testing.T) {
	e := newEnv(t)
	perms := []string{auth.AccountAPIKeys, auth.CrewsView, auth.AgentsView}
	id, tok := e.person(t, "pat@example.com", nil, []string{itArea, financeArea}, perms)
	created := decodeData[newKeyResponse](t, e.as(tok).do(t, "POST", "/api/account/api-keys", map[string]any{"name": "k",
		"permissions": []string{auth.CrewsView, auth.AgentsView}, "area_ids": []string{itArea, financeArea}}))
	key := "Bearer " + created.Secret

	user := decodeData[model.User](t, e.do(t, "GET", "/api/admin/users/"+id, nil))
	in := model.UserInput{Kind: user.Kind, Email: *user.Email, Name: user.Name, Status: user.Status,
		AreaIDs: []string{financeArea}, Permissions: []string{auth.AccountAPIKeys, auth.CrewsView}}
	wantStatus(t, e.do(t, "PUT", "/api/admin/users/"+id, in), http.StatusOK)

	who := decodeData[meResponse](t, e.as("").do(t, "GET", "/api/auth/me", nil, "Authorization", key))
	if !slices.Equal(who.Permissions, []string{auth.CrewsView}) || !slices.Equal(who.AreaIDs, []string{financeArea}) {
		t.Errorf("me after the account lost access = %+v", who)
	}

	// Long-lived streams re-check the key with its limits
	p, err := e.st.APIKeyPrincipal(context.Background(), created.Secret)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := e.st.StillValid(context.Background(), p)
	if err != nil || fresh.Can(auth.AccountAPIKeys) || !fresh.Can(auth.CrewsView) || fresh.APIKeyID != created.Key.ID {
		t.Errorf("still valid = %+v, %v", fresh, err)
	}
}

func TestMCPOnlyKeys(t *testing.T) {
	e := newEnv(t)
	_, tok := e.person(t, "builder@example.com", []string{builderRole}, []string{itArea}, nil)
	created := decodeData[newKeyResponse](t, e.as(tok).do(t, "POST", "/api/account/api-keys", map[string]any{"name": "claude",
		"allow_api": false, "permissions": []string{auth.AgentsView}}))

	r := e.as("").do(t, "GET", "/api/auth/me", nil, "Authorization", "Bearer "+created.Secret)
	wantStatus(t, r, http.StatusForbidden)
	if !strings.Contains(r.body.Error.Message, "MCP") {
		t.Errorf("message = %q", r.body.Error.Message)
	}

	cs, err := e.mcpSession(t, created.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if text, isErr := mcpCall(t, cs, "list_items", map[string]any{"kind": "agent"}); isErr {
		t.Errorf("list agents = %s", text)
	}
	// Tool calls get only the key's permissions, not the builder's
	text, isErr := mcpCall(t, cs, "list_items", map[string]any{"kind": "crew"})
	if !isErr || !strings.Contains(text, "HTTP 403") {
		t.Errorf("list crews = %s", text)
	}
}

// A trigger or schedule saved with a key runs within the key's limits, and stops when the key stops working, rather
// than running later with the account's full access.
func TestWorkSavedWithAKeyKeepsItsLimits(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pe := e.crewByName(t, "PE Due Diligence Crew")
	newKey := func(perms []string) newKeyResponse {
		return decodeData[newKeyResponse](t, e.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "automation", "permissions": perms}))
	}
	fire := func(trig model.Trigger) response {
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/hooks/"+trig.ID, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+trig.Token)
		return e.send(t, req)
	}
	postTrigger := func(key newKeyResponse) response {
		return e.as("").do(t, "POST", "/api/triggers", model.Trigger{Name: "Hook " + key.Key.ID[:8], CrewID: &pe.ID, Enabled: true,
			InputMap: map[string]string{}}, "Authorization", "Bearer "+key.Secret)
	}

	// A key that may manage triggers but not create work orders cannot save one that would
	wantStatus(t, postTrigger(newKey([]string{auth.CrewsView, auth.TriggersView, auth.TriggersManage})), http.StatusForbidden)
	// With the permission it fires; once the key is revoked it stops
	runner := newKey([]string{auth.CrewsView, auth.CrewsRun, auth.TriggersView, auth.TriggersManage, auth.WorkOrdersCreate, auth.WorkOrdersView})
	r := postTrigger(runner)
	wantStatus(t, r, http.StatusCreated)
	trig := decodeData[model.Trigger](t, r)
	wantStatus(t, fire(trig), http.StatusAccepted)
	wantStatus(t, e.do(t, "DELETE", "/api/account/api-keys/"+runner.Key.ID, nil), http.StatusNoContent)
	r = fire(trig)
	if r.status != http.StatusUnprocessableEntity || r.body.Error == nil || !strings.Contains(r.body.Error.Message, "no longer works") {
		t.Errorf("trigger after its key was revoked = %d %+v", r.status, r.body.Error)
	}
	// Saving it again in the app runs it as the person, without the key
	trig.Name = "Hook resaved"
	wantStatus(t, e.do(t, "PUT", "/api/triggers/"+trig.ID, trig), http.StatusOK)
	wantStatus(t, fire(trig), http.StatusAccepted)

	// A schedule saved with a key that is then revoked pauses itself
	sk := newKey([]string{auth.CrewsView, auth.CrewsRun, auth.SchedulesView, auth.SchedulesManage, auth.WorkOrdersCreate, auth.WorkOrdersView})
	r = e.as("").do(t, "POST", "/api/schedules", model.Schedule{Name: "Nightly", CrewID: &pe.ID, Cron: "0 2 * * *", Timezone: "UTC",
		Enabled: true}, "Authorization", "Bearer "+sk.Secret)
	wantStatus(t, r, http.StatusCreated)
	sc := decodeData[model.Schedule](t, r)
	wantStatus(t, e.do(t, "DELETE", "/api/account/api-keys/"+sk.Key.ID, nil), http.StatusNoContent)
	if _, err := e.st.Pool().Exec(ctx, `UPDATE schedules SET next_run_at = now() - interval '1 minute' WHERE id = $1`, sc.ID); err != nil {
		t.Fatal(err)
	}
	e.sched.Tick(ctx)
	paused := decodeData[model.Schedule](t, e.do(t, "GET", "/api/schedules/"+sc.ID, nil))
	if paused.Enabled || paused.LastError == nil || !strings.Contains(*paused.LastError, "no longer works") {
		t.Errorf("schedule after its key was revoked = %+v", paused)
	}
}
