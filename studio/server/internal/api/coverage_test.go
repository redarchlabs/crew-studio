package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runner"
	"crewstudio/internal/scheduler"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

const missingID = "99999999-0000-4000-8000-000000000000"

// fetch GETs a path and returns the status, headers and body without judging the status.
func (e testEnv) fetch(t *testing.T, path string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(body)
}

// startRun starts a run of the seeded Finance crew.
func (e testEnv) startRun(t *testing.T) model.Run {
	t.Helper()
	crew := e.crewByName(t, "PE Due Diligence Crew")
	r := e.do(t, "POST", "/api/crews/"+crew.ID+"/runs", map[string]any{"inputs": map[string]string{"company": "Acme", "location": "Ohio"}})
	wantStatus(t, r, http.StatusCreated)
	return decodeData[model.Run](t, r)
}

func TestRolesAreaAndAccessAdminPaths(t *testing.T) {
	e := newEnv(t)

	roles := decodeData[[]model.Role](t, e.do(t, "GET", "/api/admin/roles", nil))
	if len(roles) < 4 {
		t.Fatalf("roles = %d", len(roles))
	}
	role := decodeData[model.Role](t, e.do(t, "GET", "/api/admin/roles/"+builderRole, nil))
	if role.ID != builderRole {
		t.Errorf("role = %+v", role)
	}
	wantStatus(t, e.do(t, "GET", "/api/admin/roles/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/admin/roles/nope", nil), http.StatusNotFound)

	// update: ok, validation, missing
	mine := decodeData[model.Role](t, e.do(t, "POST", "/api/admin/roles", model.Role{Name: "Temp", Permissions: []string{auth.HistoryView}}))
	upd := decodeData[model.Role](t, e.do(t, "PUT", "/api/admin/roles/"+mine.ID, model.Role{Name: "Temp2", Permissions: []string{auth.HistoryView, auth.RunsView}}))
	if upd.Name != "Temp2" || len(upd.Permissions) != 2 {
		t.Errorf("updated = %+v", upd)
	}
	wantStatus(t, e.do(t, "PUT", "/api/admin/roles/"+mine.ID, model.Role{Name: "", Permissions: []string{"bogus"}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "PUT", "/api/admin/roles/"+missingID, model.Role{Name: "X"}), http.StatusNotFound)
	wantStatus(t, e.do(t, "PUT", "/api/admin/roles/nope", model.Role{Name: "X"}), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", "/api/admin/roles/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", "/api/admin/roles/nope", nil), http.StatusNotFound)

	// escalation: a user with roles.manage but not runs.cancel cannot grant it
	_, mgr := e.person(t, "mgr@example.com", nil, nil, []string{auth.AdminRoles})
	r := e.as(mgr).do(t, "POST", "/api/admin/roles", model.Role{Name: "Esc", Permissions: []string{auth.RunsCancel}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	wantStatus(t, e.as(mgr).do(t, "PUT", "/api/admin/roles/"+mine.ID, model.Role{Name: "Temp2", Permissions: []string{auth.RunsCancel}}),
		http.StatusUnprocessableEntity)
	wantStatus(t, e.as(mgr).do(t, "GET", "/api/admin/roles", nil), http.StatusOK)

	// areas
	area := decodeData[model.Area](t, e.do(t, "POST", "/api/areas", model.Area{Name: "Legal2"}))
	up := decodeData[model.Area](t, e.do(t, "PUT", "/api/areas/"+area.ID, model.Area{Name: "Legal3", Description: "d"}))
	if up.Name != "Legal3" {
		t.Errorf("area = %+v", up)
	}
	wantStatus(t, e.do(t, "PUT", "/api/areas/"+area.ID, model.Area{Name: ""}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "PUT", "/api/areas/"+missingID, model.Area{Name: "Nope"}), http.StatusNotFound)
	wantStatus(t, e.do(t, "PUT", "/api/areas/nope", model.Area{Name: "Nope"}), http.StatusNotFound)
	wantStatus(t, e.do(t, "PUT", "/api/areas/"+area.ID, model.Area{Name: "Finance"}), http.StatusConflict)
	wantStatus(t, e.do(t, "POST", "/api/areas", model.Area{Name: "Finance"}), http.StatusConflict)
	wantStatus(t, e.do(t, "DELETE", "/api/areas/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", "/api/areas/nope", nil), http.StatusNotFound)

	// access settings
	acc := decodeData[model.AccessSettings](t, e.do(t, "GET", "/api/admin/access", nil))
	if acc.CallbackAllowedHosts == nil {
		t.Errorf("access = %+v", acc)
	}
	viewer := viewerRole
	put := func(a model.AccessSettings) response { return e.do(t, "PUT", "/api/admin/access", a) }
	r = put(model.AccessSettings{SSOAutoProvision: true, AutoProvisionRoleID: &viewer, CallbackAllowedHosts: []string{" Hooks.Example.com ", "hooks.example.com", ""}})
	wantStatus(t, r, http.StatusOK)
	if got := decodeData[model.AccessSettings](t, r); len(got.CallbackAllowedHosts) != 1 || got.CallbackAllowedHosts[0] != "hooks.example.com" {
		t.Errorf("hosts = %v", got.CallbackAllowedHosts)
	}
	bad := "not-a-uuid"
	wantStatus(t, put(model.AccessSettings{AutoProvisionRoleID: &bad}), http.StatusUnprocessableEntity)
	wantStatus(t, put(model.AccessSettings{CallbackAllowedHosts: []string{"a/b"}}), http.StatusUnprocessableEntity)
	tooMany := make([]string, 200)
	for i := range tooMany {
		tooMany[i] = "h" + strings.Repeat("x", i%5) + ".example.com"
	}
	wantStatus(t, put(model.AccessSettings{CallbackAllowedHosts: tooMany}), http.StatusUnprocessableEntity)
	missing := missingID
	wantStatus(t, put(model.AccessSettings{AutoProvisionRoleID: &missing}), http.StatusUnprocessableEntity)
	// escalation through the auto-provision role
	_, acm := e.person(t, "acm@example.com", nil, nil, []string{auth.AdminAccess})
	builder := builderRole
	wantStatus(t, e.as(acm).do(t, "PUT", "/api/admin/access", model.AccessSettings{AutoProvisionRoleID: &builder}), http.StatusUnprocessableEntity)
	wantStatus(t, e.as(acm).do(t, "GET", "/api/admin/access", nil), http.StatusOK)

	wantStatus(t, e.do(t, "GET", "/api/admin/audit?limit=5", nil), http.StatusOK)
}

func TestUsersPasswordsSessionsAndServiceAccounts(t *testing.T) {
	e := newEnv(t)
	wantStatus(t, e.do(t, "GET", "/api/admin/users?kind=bogus", nil), http.StatusUnprocessableEntity)
	svc := decodeData[[]model.User](t, e.do(t, "GET", "/api/admin/users?kind=service", nil))
	if len(svc) != 0 {
		t.Errorf("service = %v", svc)
	}
	wantStatus(t, e.do(t, "GET", "/api/admin/users/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/admin/users/nope", nil), http.StatusNotFound)

	// A users-only manager cannot list service accounts
	_, um := e.person(t, "um@example.com", nil, nil, []string{auth.AdminUsers})
	wantStatus(t, e.as(um).do(t, "GET", "/api/admin/users?kind=service", nil), http.StatusForbidden)
	wantStatus(t, e.as(um).do(t, "GET", "/api/admin/users", nil), http.StatusOK)

	// Passwords: only administrators can have one
	adminUser := decodeData[model.User](t, e.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "human", Email: "a2@example.com",
		Name: "A2", RoleIDs: []string{adminRole}}))
	wantStatus(t, e.do(t, "PUT", "/api/admin/users/"+adminUser.ID+"/password", map[string]string{"password": "short"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "PUT", "/api/admin/users/"+adminUser.ID+"/password", map[string]string{"password": "a long enough password"}), http.StatusNoContent)
	login := e.as("").do(t, "POST", "/api/auth/login", map[string]string{"email": "a2@example.com", "password": "a long enough password"})
	wantStatus(t, login, http.StatusOK)
	wantStatus(t, e.do(t, "DELETE", "/api/admin/users/"+adminUser.ID+"/password", nil), http.StatusNoContent)
	wantStatus(t, e.as("").do(t, "POST", "/api/auth/login", map[string]string{"email": "a2@example.com", "password": "a long enough password"}),
		http.StatusUnauthorized)
	wantStatus(t, e.do(t, "DELETE", "/api/admin/users/"+missingID+"/password", nil), http.StatusNotFound)

	plainID, _ := e.person(t, "plain@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.do(t, "PUT", "/api/admin/users/"+plainID+"/password", map[string]string{"password": "a long enough password"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "PUT", "/api/admin/users/"+plainID+"/password", "not an object"), http.StatusBadRequest)

	// revoke sessions
	_, plainTok := e.person(t, "plain2@example.com", []string{viewerRole}, []string{financeArea}, nil)
	users := decodeData[[]model.User](t, e.do(t, "GET", "/api/admin/users", nil))
	var plain2 string
	for _, u := range users {
		if u.Email != nil && *u.Email == "plain2@example.com" {
			plain2 = u.ID
		}
	}
	wantStatus(t, e.as(plainTok).do(t, "GET", "/api/crews", nil), http.StatusOK)
	r := e.do(t, "POST", "/api/admin/users/"+plain2+"/sessions/revoke", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	if n := decodeData[map[string]int64](t, r)["ended"]; n < 1 {
		t.Errorf("ended = %d", n)
	}
	wantStatus(t, e.as(plainTok).do(t, "GET", "/api/crews", nil), http.StatusUnauthorized)
	wantStatus(t, e.do(t, "POST", "/api/admin/users/"+missingID+"/sessions/revoke", map[string]any{}), http.StatusNotFound)

	// Service accounts: keys and secrets
	sa := decodeData[model.User](t, e.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "service", Name: "ERP2",
		RoleIDs: []string{"66666666-0000-4000-8000-000000000003"}, AreaIDs: []string{itArea}}))
	base := "/api/admin/users/" + sa.ID
	keys := decodeData[[]model.APIKey](t, e.do(t, "GET", base+"/api-keys", nil))
	if len(keys) != 0 {
		t.Errorf("keys = %v", keys)
	}
	wantStatus(t, e.do(t, "POST", base+"/api-keys", map[string]any{"name": ""}), http.StatusUnprocessableEntity)
	past := time.Now().Add(-time.Hour)
	wantStatus(t, e.do(t, "POST", base+"/api-keys", map[string]any{"name": "old", "expires_at": past}), http.StatusUnprocessableEntity)
	future := time.Now().Add(time.Hour)
	k := decodeData[newKeyResponse](t, e.do(t, "POST", base+"/api-keys", map[string]any{"name": "ok", "expires_at": future}))
	keys = decodeData[[]model.APIKey](t, e.do(t, "GET", base+"/api-keys", nil))
	if len(keys) != 1 {
		t.Errorf("keys = %v", keys)
	}
	wantStatus(t, e.do(t, "DELETE", base+"/api-keys/nope", nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", base+"/api-keys/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", base+"/api-keys/"+k.Key.ID, nil), http.StatusNoContent)

	before := decodeData[signingSecret](t, e.do(t, "GET", base+"/signing-secret", nil))
	after := decodeData[signingSecret](t, e.do(t, "POST", base+"/signing-secret/rotate", map[string]any{}))
	if after.SigningSecret == "" || after.SigningSecret == before.SigningSecret {
		t.Errorf("rotate: %q -> %q", before.SigningSecret, after.SigningSecret)
	}

	// A person is not a service account: their keys can be listed and revoked here, not issued
	h := "/api/admin/users/" + plainID
	wantStatus(t, e.do(t, "GET", h+"/api-keys", nil), http.StatusOK)
	wantStatus(t, e.do(t, "POST", h+"/api-keys", map[string]any{"name": "x"}), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", h+"/api-keys/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", h+"/signing-secret", nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "POST", h+"/signing-secret/rotate", map[string]any{}), http.StatusNotFound)

	// A user manager without service-account rights is refused on service accounts
	wantStatus(t, e.as(um).do(t, "GET", base+"/api-keys", nil), http.StatusForbidden)
	// Nobody manages an account that holds more than they do
	wantStatus(t, e.as(um).do(t, "DELETE", "/api/admin/users/"+e.adminID+"/password", nil), http.StatusForbidden)
}

func TestRunReadEndpointsAndScope(t *testing.T) {
	e := newEnv(t)
	run := e.startRun(t)
	base := "/api/runs/" + run.ID

	// list filter validation
	wantStatus(t, e.do(t, "GET", "/api/runs?crew_id=nope", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "GET", "/api/runs?work_order_id=nope", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "GET", "/api/runs?status=bogus", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "GET", "/api/runs?trigger_type=bogus", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "GET", "/api/runs?status=queued&trigger_type=manual", nil), http.StatusOK)

	// events, journal, artifacts, deliveries
	ctx := context.Background()
	_ = e.st.AppendEvent(ctx, run.ID, model.RunEvent{Seq: 1, Type: "task_started", CreatedAt: time.Now(),
		Payload: map[string]any{"task_name": "Corporate Profile", "agent_role": "Corporate Investigator"}})
	events := decodeData[[]model.RunEvent](t, e.do(t, "GET", base+"/events?after=0", nil))
	if len(events) != 1 {
		t.Errorf("events = %v", events)
	}
	if got := decodeData[[]model.RunEvent](t, e.do(t, "GET", base+"/events?after=5", nil)); len(got) != 0 {
		t.Errorf("after=5 = %v", got)
	}
	wantStatus(t, e.do(t, "GET", base+"/events?after=-3", nil, "Last-Event-ID", "1"), http.StatusOK)
	wantStatus(t, e.do(t, "GET", base+"/journal", nil), http.StatusOK)
	arts := decodeData[[]model.Attachment](t, e.do(t, "GET", base+"/artifacts", nil))
	if len(arts) != 0 {
		t.Errorf("artifacts = %v", arts)
	}
	// A run artifact, downloaded through its content endpoint
	artID := "77777777-0000-4000-8000-000000000001"
	rid := run.ID
	if err := e.files.Put(ctx, "runs/"+rid+"/report.md", strings.NewReader("# Report"), 8, "text/markdown"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddAttachment(ctx, model.Attachment{ID: artID, RunID: &rid, Kind: model.AttachmentArtifact, Filename: "report.md",
		ContentType: "text/markdown; charset=utf-8", SizeBytes: 8, SHA256: "x", StorageKey: "runs/" + rid + "/report.md"}); err != nil {
		t.Fatal(err)
	}
	arts = decodeData[[]model.Attachment](t, e.do(t, "GET", base+"/artifacts", nil))
	if len(arts) != 1 {
		t.Fatalf("artifacts = %v", arts)
	}
	status, header, body := e.fetch(t, "/api/attachments/"+artID+"/content")
	if status != http.StatusOK || body != "# Report" || header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("download = %d %q %v", status, body, header)
	}
	// A run file whose object is gone
	missingKey := "77777777-0000-4000-8000-000000000002"
	if _, err := e.st.AddAttachment(ctx, model.Attachment{ID: missingKey, RunID: &rid, Kind: model.AttachmentArtifact, Filename: "gone.txt",
		ContentType: "text/plain", SizeBytes: 1, SHA256: "x", StorageKey: "runs/" + rid + "/gone.txt"}); err != nil {
		t.Fatal(err)
	}
	if status, _, _ = e.fetch(t, "/api/attachments/"+missingKey+"/content"); status != http.StatusNotFound {
		t.Errorf("missing object = %d", status)
	}
	if status, _, _ = e.fetch(t, "/api/attachments/"+missingID+"/content"); status != http.StatusNotFound {
		t.Errorf("missing attachment = %d", status)
	}
	wantStatus(t, e.do(t, "GET", base+"/deliveries", nil), http.StatusOK)

	// Out-of-area runs are 404 on every sub-resource
	_, it := e.person(t, "it@example.com", []string{viewerRole}, []string{itArea}, nil)
	other := e.as(it)
	for _, sub := range []string{"", "/events", "/interactions", "/llm-calls", "/artifacts", "/deliveries", "/journal"} {
		wantStatus(t, other.do(t, "GET", base+sub, nil), http.StatusNotFound)
	}
	wantStatus(t, other.do(t, "DELETE", base, nil), http.StatusForbidden)
	if status, _, _ = other.fetch(t, "/api/attachments/"+artID+"/content"); status != http.StatusNotFound {
		t.Errorf("out-of-area artifact = %d", status)
	}
	for _, sub := range []string{"/interactions", "/llm-calls", "/journal", "/artifacts"} {
		wantStatus(t, e.do(t, "GET", "/api/runs/"+missingID+sub, nil), http.StatusNotFound)
		wantStatus(t, e.do(t, "GET", "/api/runs/nope"+sub, nil), http.StatusNotFound)
	}

	// Cancel permissions: viewers cannot; a work-order canceller cannot cancel plain runs
	_, viewer := e.person(t, "fv@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "POST", base+"/cancel", map[string]any{}), http.StatusForbidden)
	_, woc := e.person(t, "woc@example.com", nil, []string{financeArea}, []string{auth.RunsView, auth.WorkOrdersCancel})
	wantStatus(t, e.as(woc).do(t, "POST", base+"/cancel", map[string]any{}), http.StatusForbidden)
	wantStatus(t, e.do(t, "POST", "/api/runs/"+missingID+"/cancel", map[string]any{}), http.StatusNotFound)

	// ... but can cancel a work order's attempt
	memo := e.crewByName(t, "PE Due Diligence Crew")
	wo := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "t", CrewID: &memo.ID,
		Inputs: map[string]string{"company": "Acme"}, Submit: true}))
	if wo.CurrentRunID == nil {
		t.Fatalf("wo = %+v", wo)
	}
	got := e.as(woc).do(t, "POST", "/api/runs/"+*wo.CurrentRunID+"/cancel", map[string]any{})
	wantStatus(t, got, http.StatusOK)
}

func TestWorkOrderFiltersAndJournalErrors(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"status=x", "source=x", "source_id=x", "crew_id=x", "q=" + strings.Repeat("a", 201)} {
		wantStatus(t, e.do(t, "GET", "/api/work-orders?"+q, nil), http.StatusUnprocessableEntity)
	}
	wantStatus(t, e.do(t, "GET", "/api/work-orders?status=draft&source=manual&crew_id="+missingID+"&source_id="+missingID, nil), http.StatusOK)

	memo := e.crewByName(t, "PE Due Diligence Crew")
	mk := func(submit bool) model.WorkOrder {
		return decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "t",
			CrewID: &memo.ID, Inputs: map[string]string{"company": "Acme"}, Submit: submit}))
	}
	draft := mk(false)
	r := e.do(t, "POST", "/api/work-orders/"+draft.ID+"/cancel", map[string]any{})
	wantStatus(t, r, http.StatusConflict)
	wantStatus(t, e.do(t, "GET", "/api/work-orders/"+draft.ID+"/journal", nil), http.StatusConflict)
	wantStatus(t, e.do(t, "POST", "/api/work-orders/"+missingID+"/cancel", map[string]any{}), http.StatusNotFound)

	live := mk(true)
	wantStatus(t, e.do(t, "GET", "/api/work-orders/"+live.ID+"/journal?run_id=nope", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "GET", "/api/work-orders/"+live.ID+"/journal?run_id="+missingID, nil), http.StatusNotFound)
	// a run that belongs to some other work order
	other := e.startRun(t)
	wantStatus(t, e.do(t, "GET", "/api/work-orders/"+live.ID+"/journal?run_id="+other.ID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/work-orders/"+live.ID+"/journal?run_id="+*live.CurrentRunID, nil), http.StatusOK)
}

func TestScheduleAndTriggerErrorPaths(t *testing.T) {
	e := newEnv(t)
	pe := e.crewByName(t, "PE Due Diligence Crew")

	// lists
	wantStatus(t, e.do(t, "GET", "/api/schedules", nil), http.StatusOK)
	wantStatus(t, e.do(t, "GET", "/api/triggers", nil), http.StatusOK)
	wantStatus(t, e.do(t, "GET", "/api/schedules/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/triggers/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/schedules/nope", nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", "/api/schedules/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", "/api/triggers/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "POST", "/api/schedules/"+missingID+"/run-now", map[string]any{}), http.StatusNotFound)
	wantStatus(t, e.do(t, "POST", "/api/triggers/"+missingID+"/rotate", map[string]any{}), http.StatusNotFound)
	wantStatus(t, e.do(t, "PUT", "/api/schedules/"+missingID, model.Schedule{Name: "x"}), http.StatusNotFound)
	wantStatus(t, e.do(t, "PUT", "/api/triggers/"+missingID, model.Trigger{Name: "x"}), http.StatusNotFound)

	// preview validation
	wantStatus(t, e.do(t, "POST", "/api/schedules/preview", map[string]string{"cron": strings.Repeat("*", 121)}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/schedules/preview", map[string]string{"cron": "* * * * *", "timezone": strings.Repeat("z", 65)}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/schedules/preview", map[string]string{"cron": "not a cron"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/schedules/preview", "bad"), http.StatusBadRequest)
	ok := e.do(t, "POST", "/api/schedules/preview", map[string]string{"cron": "  0   9 * * 1 "})
	wantStatus(t, ok, http.StatusOK)

	// save validation: bad body, bad target, bad cron
	wantStatus(t, e.do(t, "POST", "/api/schedules", model.Schedule{Name: ""}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/schedules", model.Schedule{Name: "x", CrewID: &pe.ID, Cron: "bad", Timezone: "UTC", Enabled: true}),
		http.StatusUnprocessableEntity)
	missing := missingID
	wantStatus(t, e.do(t, "POST", "/api/schedules", model.Schedule{Name: "x", CrewID: &missing, Cron: "0 9 * * *", Timezone: "UTC", Enabled: true}),
		http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/triggers", model.Trigger{Name: ""}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/triggers", model.Trigger{Name: "x", CrewID: &missing, InputMap: map[string]string{}}),
		http.StatusUnprocessableEntity)

	// A disabled schedule has no next run; enabling then editing keeps it
	sc := decodeData[model.Schedule](t, e.do(t, "POST", "/api/schedules", model.Schedule{Name: "off", CrewID: &pe.ID,
		Cron: "0 9 * * *", Timezone: "UTC", Enabled: false, Inputs: map[string]string{"company": "A"}}))
	if sc.NextRunAt != nil {
		t.Errorf("disabled next = %v", sc.NextRunAt)
	}

	// Scope: people in another area cannot see them, and viewers cannot manage
	_, it := e.person(t, "it2@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(it).do(t, "GET", "/api/schedules/"+sc.ID, nil), http.StatusNotFound)
	wantStatus(t, e.as(it).do(t, "DELETE", "/api/schedules/"+sc.ID, nil), http.StatusNotFound)
	wantStatus(t, e.as(it).do(t, "POST", "/api/schedules/"+sc.ID+"/run-now", map[string]any{}), http.StatusNotFound)
	_, viewer := e.person(t, "fv2@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "DELETE", "/api/schedules/"+sc.ID, nil), http.StatusForbidden)
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/schedules/"+sc.ID+"/run-now", map[string]any{}), http.StatusForbidden)
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/schedules", model.Schedule{Name: "x"}), http.StatusForbidden)
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/triggers/"+missingID+"/rotate", map[string]any{}), http.StatusForbidden)

	trig := decodeData[model.Trigger](t, e.do(t, "POST", "/api/triggers", model.Trigger{Name: "T", CrewID: &pe.ID,
		Enabled: true, InputMap: map[string]string{"company": "c"}}))
	wantStatus(t, e.as(it).do(t, "GET", "/api/triggers/"+trig.ID, nil), http.StatusNotFound)
	wantStatus(t, e.as(it).do(t, "DELETE", "/api/triggers/"+trig.ID, nil), http.StatusNotFound)
	wantStatus(t, e.as(it).do(t, "POST", "/api/triggers/"+trig.ID+"/rotate", map[string]any{}), http.StatusNotFound)
	wantStatus(t, e.as(it).do(t, "PUT", "/api/triggers/"+trig.ID, trig), http.StatusNotFound)
	if got := decodeData[[]model.Trigger](t, e.as(viewer).do(t, "GET", "/api/triggers", nil)); len(got) != 1 || got[0].SigningSecret != "" {
		t.Errorf("viewer list = %+v", got)
	}
	if got := decodeData[[]model.Trigger](t, e.as(it).do(t, "GET", "/api/triggers", nil)); len(got) != 0 {
		t.Errorf("it list = %+v", got)
	}

	// Hook edge cases
	hook := func(id, token, body string, headers ...string) response {
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/hooks/"+id, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("X-CrewStudio-Token", token)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		return e.send(t, req)
	}
	wantStatus(t, hook("nope", trig.Token, `{}`), http.StatusNotFound)
	wantStatus(t, hook(trig.ID, trig.Token, `{not json`), http.StatusBadRequest)
	wantStatus(t, hook(trig.ID, trig.Token, `{}`, "Idempotency-Key", strings.Repeat("k", 201)), http.StatusUnprocessableEntity)
	wantStatus(t, hook(trig.ID, trig.Token, strings.Repeat("a", maxHookBodyBytes+10)), http.StatusRequestEntityTooLarge)
	wantStatus(t, hook(trig.ID, trig.Token, ``), http.StatusAccepted)
}

func TestHookBrief(t *testing.T) {
	tr := model.Trigger{Brief: "Hello", IncludePayload: true}
	if got := hookBrief(tr, nil); got != "Hello" {
		t.Errorf("empty payload = %q", got)
	}
	if got := hookBrief(model.Trigger{Brief: "Hello"}, []byte(`{"a":1}`)); got != "Hello" {
		t.Errorf("excluded payload = %q", got)
	}
	got := hookBrief(tr, []byte(`{"a":1}`))
	if !strings.HasPrefix(got, "Hello\n\n## Payload") || !strings.Contains(got, `"a": 1`) {
		t.Errorf("brief = %q", got)
	}
	if got := hookBrief(model.Trigger{IncludePayload: true}, []byte(`{"a":1}`)); !strings.HasPrefix(got, "## Payload") {
		t.Errorf("no brief = %q", got)
	}
	big := hookBrief(model.Trigger{IncludePayload: true}, []byte(`{"a":"`+strings.Repeat("é", maxPayloadInBrief)+`"}`))
	if !strings.Contains(big, "[payload cut]") {
		t.Errorf("big payload not cut")
	}
	// Not JSON: only the heading remains
	if got := hookBrief(model.Trigger{IncludePayload: true}, []byte(`nope`)); !strings.Contains(got, "## Payload") {
		t.Errorf("non json = %q", got)
	}
}

func TestMapInputsEdgeCases(t *testing.T) {
	if got := MapInputs(nil, map[string]any{"a": 1}); len(got) != 0 {
		t.Errorf("nil map = %v", got)
	}
	payload := map[string]any{"s": "a\x00b", "nil": nil, "arr": []any{1.0}}
	got := MapInputs(map[string]string{"s": "s", "n": "nil", "neg": "arr.-1", "scalar": "s.x", "list": "arr", "nopayload": "zzz"}, payload)
	if got["s"] != "ab" || got["list"] != "[1]" || len(got) != 2 {
		t.Errorf("got = %v", got)
	}
	if got := MapInputs(map[string]string{"k": "a"}, nil); len(got) != 0 {
		t.Errorf("nil payload = %v", got)
	}
}

func TestClassify(t *testing.T) {
	zip := []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00")
	pdf := []byte("%PDF-1.4\n%....")
	gif := []byte("GIF89a\x01\x00\x01\x00")
	jpg := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0}
	webp := append(append([]byte("RIFF\x00\x00\x00\x00"), []byte("WEBPVP8 ")...), make([]byte, 8)...)
	html := []byte("<!DOCTYPE html><html><body>x</body></html>")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	exe := []byte{0x4d, 0x5a, 0x90, 0, 3, 0, 0, 0}
	text := []byte("hello world\n")
	bin := []byte{0, 1, 2, 3, 4, 0xff, 0xfe}
	cases := []struct {
		name string
		file string
		head []byte
		want string
	}{
		{"png", "a.png", tinyPNG, "image/png"},
		{"png with wrong ext", "a.txt", tinyPNG, "image/png"},
		{"jpeg", "a.jpg", jpg, "image/jpeg"},
		{"gif", "a.gif", gif, "image/gif"},
		{"webp", "a.webp", webp, "image/webp"},
		{"pdf", "a.pdf", pdf, "application/pdf"},
		{"pdf wrong ext", "a", pdf, "application/pdf"},
		{"docx", "a.docx", zip, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"xlsx upper", "A.XLSX", zip, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"pptx", "a.pptx", zip, "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{"plain zip", "a.zip", zip, ""},
		{"zip no ext", "a", zip, ""},
		{"txt", "a.txt", text, "text/plain; charset=utf-8"},
		{"md", "a.md", text, "text/markdown; charset=utf-8"},
		{"markdown", "a.markdown", text, "text/markdown; charset=utf-8"},
		{"csv", "a.csv", text, "text/csv; charset=utf-8"},
		{"json", "a.json", text, "application/json"},
		{"xml", "a.xml", text, "application/xml"},
		{"yaml", "a.yaml", text, "application/x-yaml"},
		{"yml", "a.yml", text, "application/x-yaml"},
		{"log", "a.log", text, "text/plain; charset=utf-8"},
		{"tsv", "a.tsv", text, "text/tab-separated-values; charset=utf-8"},
		{"html text", "a.html", html, "text/html; charset=utf-8"},
		{"no extension text", "README", text, "text/plain; charset=utf-8"},
		{"unknown extension text", "a.sh", text, ""},
		{"svg", "a.svg", svg, ""},
		{"exe", "a.exe", exe, ""},
		{"binary", "a.bin", bin, ""},
		{"empty", "a.txt", nil, "text/plain; charset=utf-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.file, tc.head); got != tc.want {
				t.Errorf("classify(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

// envWithoutStorage is a server with no file storage configured.
func envWithoutStorage(t *testing.T) testEnv {
	t.Helper()
	st := store.New(testdb.New(t, "api_test"))
	runs := &fakeRuns{st: st}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	launcher := launch.New(st, fakeTools{}, runs)
	sched := scheduler.New(st, launcher, log)
	webDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html></html>"), 0o600)
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	t.Cleanup(srv.Close)
	srv.Config.Handler = NewServer(Deps{Store: st, Runs: runs, Broker: runner.NewBroker(), Tools: fakeTools{}, Launcher: launcher,
		Scheduler: sched, SSO: &fakeSSO{}, Log: log}, Options{WebDir: webDir, PublicURL: srv.URL, LocalLogin: true, SetupToken: testSetupToken}).Handler()
	e := testEnv{srv: srv, st: st, runs: runs, sched: sched}
	id, err := st.CreateFirstAdmin(context.Background(), "admin@example.com", "Ada Admin", mustHash(t, "correct horse battery"))
	if err != nil {
		t.Fatal(err)
	}
	e.adminID, e.token = id, e.session(t, id)
	return e
}

func TestStorageUnavailable(t *testing.T) {
	e := envWithoutStorage(t)
	memo := e.crewByName(t, "PE Due Diligence Crew")
	wo := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "t", CrewID: &memo.ID}))
	r := e.upload(t, wo.ID, map[string][]byte{"a.png": tinyPNG})
	wantStatus(t, r, http.StatusServiceUnavailable)
	if r.body.Error.Code != "storage_unavailable" {
		t.Errorf("error = %+v", r.body.Error)
	}
	if status, _, _ := e.fetch(t, "/api/attachments/"+missingID+"/content"); status != http.StatusServiceUnavailable {
		t.Errorf("content = %d", status)
	}
}

func TestUploadEdgeCases(t *testing.T) {
	e := newEnv(t)
	memo := e.crewByName(t, "PE Due Diligence Crew")
	wo := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "t", CrewID: &memo.ID}))
	wantStatus(t, e.upload(t, wo.ID, map[string][]byte{"empty.txt": {}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.upload(t, missingID, map[string][]byte{"a.png": tinyPNG}), http.StatusNotFound)
	// no files at all
	wantStatus(t, e.upload(t, wo.ID, map[string][]byte{}), http.StatusBadRequest)
	// not multipart, but with the upload header: still a bad upload
	_, viewer := e.person(t, "fv3@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).upload(t, wo.ID, map[string][]byte{"a.png": tinyPNG}), http.StatusForbidden)
	// a file larger than the per-file limit
	big := make([]byte, maxAttachmentBytes+1)
	copy(big, "hello")
	wantStatus(t, e.upload(t, wo.ID, map[string][]byte{"big.txt": big}), http.StatusRequestEntityTooLarge)
	// too many files
	for i := 0; i < maxAttachmentsPerOrder; i++ {
		wantStatus(t, e.upload(t, wo.ID, map[string][]byte{"n" + string(rune('a'+i)) + ".txt": []byte("x")}), http.StatusCreated)
	}
	wantStatus(t, e.upload(t, wo.ID, map[string][]byte{"extra.txt": []byte("x")}), http.StatusUnprocessableEntity)
	// delete: wrong ids
	wantStatus(t, e.do(t, "DELETE", "/api/work-orders/"+wo.ID+"/attachments/"+missingID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", "/api/work-orders/"+wo.ID+"/attachments/nope", nil), http.StatusNotFound)
}
