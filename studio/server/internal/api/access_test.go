package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// person creates an active person with roles and areas and returns their id and a session token.
func (e testEnv) person(t *testing.T, email string, roles, areas, perms []string) (string, string) {
	t.Helper()
	ctx := store.WithActor(context.Background(), store.Actor{ID: e.adminID, Name: "test"})
	u, err := e.st.CreateUser(ctx, model.UserInput{Kind: auth.KindHuman, Email: email, Name: email, Status: model.UserActive,
		RoleIDs: roles, AreaIDs: areas, Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID, e.session(t, u.ID)
}

func TestSignedOutRequestsAreRefused(t *testing.T) {
	e := newEnv(t)
	anon := e.as("")
	r := anon.do(t, "GET", "/api/crews", nil)
	wantStatus(t, r, http.StatusUnauthorized)
	if r.body.Error.Code != "unauthenticated" {
		t.Errorf("code = %s", r.body.Error.Code)
	}
	status := decodeData[authStatus](t, anon.do(t, "GET", "/api/auth/status", nil))
	if status.SetupRequired || !status.LocalLogin || len(status.Providers) != 1 {
		t.Errorf("status = %+v", status)
	}
	wantStatus(t, anon.do(t, "GET", "/api/auth/me", nil), http.StatusUnauthorized)
	res, err := http.Get(e.srv.URL + "/api/openapi.yaml")
	if err != nil || res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/yaml") {
		t.Errorf("openapi = %v %v", res, err)
	}
	res.Body.Close()
	wantStatus(t, e.as("not-a-session").do(t, "GET", "/api/crews", nil), http.StatusUnauthorized)

	me := decodeData[meResponse](t, e.do(t, "GET", "/api/auth/me", nil))
	if !me.AllAreas || len(me.Permissions) != len(auth.Catalog) || me.Via != auth.ViaSession {
		t.Errorf("admin me = %+v", me)
	}
}

func TestFirstRunSetup(t *testing.T) {
	e := newBareEnv(t)
	status := decodeData[authStatus](t, e.do(t, "GET", "/api/auth/status", nil))
	if !status.SetupRequired {
		t.Fatal("setup should be required with no accounts")
	}
	body := map[string]string{"token": "wrong", "email": "boss@example.com", "name": "Boss", "password": "a long enough password"}
	wantStatus(t, e.do(t, "POST", "/api/auth/setup", body), http.StatusForbidden)
	body["token"] = testSetupToken
	body["password"] = "short"
	wantStatus(t, e.do(t, "POST", "/api/auth/setup", body), http.StatusUnprocessableEntity)
	body["password"] = "a long enough password"
	r := e.do(t, "POST", "/api/auth/setup", body)
	wantStatus(t, r, http.StatusCreated)
	if len(r.cookies) == 0 || r.cookies[0].Name != sessionCookie || !r.cookies[0].HttpOnly {
		t.Fatalf("cookies = %+v", r.cookies)
	}
	me := decodeData[meResponse](t, e.as(r.cookies[0].Value).do(t, "GET", "/api/auth/me", nil))
	if me.Email != "boss@example.com" || !me.AllAreas {
		t.Errorf("me = %+v", me)
	}
	// The token works once
	wantStatus(t, e.do(t, "POST", "/api/auth/setup", body), http.StatusForbidden)
}

func TestLocalLogin(t *testing.T) {
	e := newEnv(t)
	anon := e.as("")
	r := anon.do(t, "POST", "/api/auth/login", map[string]string{"email": "ADMIN@example.com", "password": "correct horse battery"})
	wantStatus(t, r, http.StatusOK)
	token := r.cookies[0].Value
	wantStatus(t, anon.as(token).do(t, "GET", "/api/crews", nil), http.StatusOK)

	wantStatus(t, anon.as(token).do(t, "POST", "/api/auth/logout", map[string]any{}), http.StatusNoContent)
	wantStatus(t, anon.as(token).do(t, "GET", "/api/crews", nil), http.StatusUnauthorized)

	// A non-administrator with a password still cannot use password sign-in
	viewerID, _ := e.person(t, "viewer@example.com", []string{viewerRole}, []string{financeArea}, nil)
	hash := mustHash(t, "viewer password ok")
	if err := e.st.SetPassword(context.Background(), viewerID, &hash); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, anon.do(t, "POST", "/api/auth/login", map[string]string{"email": "viewer@example.com", "password": "viewer password ok"}),
		http.StatusUnauthorized)
	wantStatus(t, anon.do(t, "POST", "/api/auth/login", map[string]string{"email": "nobody@example.com", "password": "whatever at all"}),
		http.StatusUnauthorized)
}

func TestLoginLockout(t *testing.T) {
	e := newEnv(t)
	anon := e.as("")
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := e.st.RecordLoginFailure(ctx, e.adminID); err != nil {
			t.Fatal(err)
		}
	}
	r := anon.do(t, "POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct horse battery"})
	wantStatus(t, r, http.StatusLocked)
	audit := decodeData[[]model.AuditEvent](t, e.do(t, "GET", "/api/admin/audit", nil))
	_ = audit // failures before lockout are recorded by the store, not the endpoint
}

func TestPermissionsAndAreasScopeEverything(t *testing.T) {
	e := newEnv(t)
	_, viewer := e.person(t, "v@example.com", []string{viewerRole}, []string{financeArea}, nil)
	v := e.as(viewer)

	crews := decodeData[[]model.Crew](t, v.do(t, "GET", "/api/crews", nil))
	if len(crews) != 1 || crews[0].Name != "PE Due Diligence Crew" {
		t.Fatalf("viewer in Finance sees %+v", crews)
	}
	it := e.crewByName(t, "AI Research Crew")
	wantStatus(t, v.do(t, "GET", "/api/crews/"+it.ID, nil), http.StatusNotFound)
	wantStatus(t, v.do(t, "GET", "/api/crews/"+it.ID+"/validate", nil), http.StatusNotFound)

	pe := crews[0]
	r := v.do(t, "PUT", "/api/crews/"+pe.ID, pe)
	wantStatus(t, r, http.StatusForbidden)
	if r.body.Error.Details[0] != "needs: crews.edit" {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	wantStatus(t, v.do(t, "POST", "/api/crews/"+pe.ID+"/runs", map[string]any{}), http.StatusForbidden)
	wantStatus(t, v.do(t, "GET", "/api/settings", nil), http.StatusForbidden)
	wantStatus(t, v.do(t, "GET", "/api/admin/users", nil), http.StatusForbidden)

	// The 5 PE crew agents plus the 10 library agents in Finance (0007_agent_library.sql)
	const financeAgents = 15
	agents := decodeData[[]model.Agent](t, v.do(t, "GET", "/api/agents", nil))
	if len(agents) != financeAgents {
		t.Errorf("viewer sees %d agents, want the %d Finance ones", len(agents), financeAgents)
	}
	for _, a := range agents {
		if a.Name == "Research Analyst" || a.Name == "QA Engineer" {
			t.Errorf("Finance viewer sees IT-only agent %q", a.Name)
		}
	}
	overview := decodeData[model.Overview](t, v.do(t, "GET", "/api/overview", nil))
	if overview.Counts.Crews != 1 || overview.Counts.Agents != financeAgents {
		t.Errorf("overview = %+v", overview.Counts)
	}
	hist := v.do(t, "GET", "/api/history?entity_type=user", nil)
	wantStatus(t, hist, http.StatusUnprocessableEntity)
}

func TestAreaChangesKeepAreasYouAreNotIn(t *testing.T) {
	e := newEnv(t)
	pe := e.crewByName(t, "PE Due Diligence Crew")
	pe.AreaIDs = []string{financeArea, hrArea}
	wantStatus(t, e.do(t, "PUT", "/api/crews/"+pe.ID, pe), http.StatusOK)

	_, builder := e.person(t, "b@example.com", []string{builderRole}, []string{financeArea}, nil)
	b := e.as(builder)
	pe.AreaIDs = []string{financeArea}
	saved := decodeData[model.Crew](t, b.do(t, "PUT", "/api/crews/"+pe.ID, pe))
	if len(saved.AreaIDs) != 2 {
		t.Errorf("HR should be kept, areas = %v", saved.AreaIDs)
	}
	pe.AreaIDs = []string{financeArea, itArea}
	r := b.do(t, "PUT", "/api/crews/"+pe.ID, pe)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(r.body.Error.Details[0], "only add functional areas you belong to") {
		t.Errorf("details = %v", r.body.Error.Details)
	}

	// A builder cannot pull an IT task they cannot see into their crew
	itCrew := e.crewByName(t, "AI Research Crew")
	pe.AreaIDs = []string{financeArea}
	pe.TaskIDs = append(pe.TaskIDs, itCrew.TaskIDs[0])
	r = b.do(t, "PUT", "/api/crews/"+pe.ID, pe)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "outside your functional areas") {
		t.Errorf("details = %v", r.body.Error.Details)
	}

	r = b.do(t, "POST", "/api/agents", map[string]any{"name": "HR helper", "role": "r", "goal": "g", "backstory": "b",
		"max_iter": 3, "area_ids": []string{hrArea}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
}

func TestAdminCannotEscalateOrLockEveryoneOut(t *testing.T) {
	e := newEnv(t)
	_, userAdmin := e.person(t, "ua@example.com", nil, []string{financeArea}, []string{auth.AdminUsers})
	ua := e.as(userAdmin)

	r := ua.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "human", Email: "x@example.com", Name: "X", RoleIDs: []string{adminRole}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	r = ua.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "human", Email: "x@example.com", Name: "X", AllAreas: true})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	r = ua.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "human", Email: "x@example.com", Name: "X", AreaIDs: []string{itArea}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	r = ua.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "human", Email: "x@example.com", Name: "X",
		AreaIDs: []string{financeArea}, Permissions: []string{auth.AdminUsers}})
	wantStatus(t, r, http.StatusCreated)
	// Service accounts need their own permission
	wantStatus(t, ua.do(t, "GET", "/api/admin/users?kind=service", nil), http.StatusForbidden)

	// ua cannot touch an account that holds more than they do (no takeover through password or email)
	wantStatus(t, ua.do(t, "PUT", "/api/admin/users/"+e.adminID+"/password", map[string]string{"password": "a takeover attempt!"}),
		http.StatusForbidden)
	admin := decodeData[model.User](t, e.do(t, "GET", "/api/admin/users/"+e.adminID, nil))
	in := model.UserInput{Kind: "human", Email: "mine@example.com", Name: admin.Name, Status: model.UserActive, AllAreas: true, RoleIDs: admin.RoleIDs}
	wantStatus(t, ua.do(t, "PUT", "/api/admin/users/"+e.adminID, in), http.StatusForbidden)

	// The last person who can manage users cannot be disabled or stripped of it
	users := decodeData[[]model.User](t, e.do(t, "GET", "/api/admin/users", nil))
	for _, u := range users {
		if u.ID != e.adminID {
			wantStatus(t, e.do(t, "DELETE", "/api/admin/users/"+u.ID, nil), http.StatusNoContent)
		}
	}
	in = model.UserInput{Kind: "human", Email: *admin.Email, Name: admin.Name, Status: model.UserActive, AllAreas: true}
	r = e.do(t, "PUT", "/api/admin/users/"+e.adminID, in)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "last_admin" {
		t.Errorf("code = %q, want last_admin so clients can explain it", r.body.Error.Code)
	}
	wantStatus(t, e.do(t, "DELETE", "/api/admin/users/"+e.adminID, nil), http.StatusUnprocessableEntity)
}

func TestRolesAndAreasAdmin(t *testing.T) {
	e := newEnv(t)
	perms := decodeData[[]model.PermissionDef](t, e.do(t, "GET", "/api/admin/permissions", nil))
	if len(perms) != len(auth.Catalog) {
		t.Fatalf("catalog in database has %d permissions, code has %d", len(perms), len(auth.Catalog))
	}
	for i, p := range perms {
		if p.Key != auth.Catalog[i] {
			t.Errorf("permission %d: database %q, code %q", i, p.Key, auth.Catalog[i])
		}
	}
	r := e.do(t, "POST", "/api/admin/roles", model.Role{Name: "Auditor", Permissions: []string{auth.HistoryView, "made.up"}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	role := decodeData[model.Role](t, e.do(t, "POST", "/api/admin/roles", model.Role{Name: "Auditor", Permissions: []string{auth.HistoryView}}))
	if len(role.Permissions) != 1 {
		t.Errorf("role = %+v", role)
	}
	wantStatus(t, e.do(t, "PUT", "/api/admin/roles/"+adminRole, model.Role{Name: "Administrator"}), http.StatusConflict)
	wantStatus(t, e.do(t, "DELETE", "/api/admin/roles/"+role.ID, nil), http.StatusNoContent)

	area := decodeData[model.Area](t, e.do(t, "POST", "/api/areas", model.Area{Name: "Legal"}))
	wantStatus(t, e.do(t, "DELETE", "/api/areas/"+financeArea, nil), http.StatusConflict)
	wantStatus(t, e.do(t, "DELETE", "/api/areas/"+area.ID, nil), http.StatusNoContent)

	_, viewer := e.person(t, "v@example.com", []string{viewerRole}, []string{financeArea}, nil)
	areas := decodeData[[]model.Area](t, e.as(viewer).do(t, "GET", "/api/areas", nil))
	if len(areas) != 4 || areas[0].Usage != nil {
		t.Errorf("non-admins see area names without usage: %+v", areas)
	}
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/areas", model.Area{Name: "Nope"}), http.StatusForbidden)

	revs := e.do(t, "GET", "/api/history?entity_type=area", nil)
	wantStatus(t, revs, http.StatusOK)
	if revs.body.Meta.Total < 2 {
		t.Errorf("area revisions = %+v", revs.body.Meta)
	}
}

func TestServiceAccountKeysAndIdempotentRuns(t *testing.T) {
	e := newEnv(t)
	sa := decodeData[model.User](t, e.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "service", Name: "ERP",
		RoleIDs: []string{"66666666-0000-4000-8000-000000000003"}, AreaIDs: []string{itArea}}))
	created := decodeData[newKeyResponse](t, e.do(t, "POST", "/api/admin/users/"+sa.ID+"/api-keys", map[string]any{"name": "prod"}))
	if !strings.HasPrefix(created.Secret, "csk_") {
		t.Fatalf("secret = %q", created.Secret)
	}
	key := "Bearer " + created.Secret
	api := e.as("")
	me := decodeData[meResponse](t, api.do(t, "GET", "/api/auth/me", nil, "Authorization", key))
	if me.Kind != auth.KindService || me.Via != auth.ViaAPIKey {
		t.Errorf("me = %+v", me)
	}
	itCrew := e.crewByName(t, "AI Research Crew")
	first := api.do(t, "POST", "/api/crews/"+itCrew.ID+"/runs", map[string]any{}, "Authorization", key, "Idempotency-Key", "order-1")
	wantStatus(t, first, http.StatusCreated)
	again := api.do(t, "POST", "/api/crews/"+itCrew.ID+"/runs", map[string]any{}, "Authorization", key, "Idempotency-Key", "order-1")
	wantStatus(t, again, http.StatusOK)
	if decodeData[model.Run](t, first).ID != decodeData[model.Run](t, again).ID {
		t.Error("the same Idempotency-Key must return the same run")
	}
	run := decodeData[model.Run](t, first)
	if run.TriggerType != "api" || run.CreatedByName != "ERP" {
		t.Errorf("run = %+v", run)
	}
	// Callbacks: allowed for service accounts, refused for people
	cb := api.do(t, "POST", "/api/crews/"+itCrew.ID+"/runs", map[string]any{"callback_url": "https://hooks.example.com/x"}, "Authorization", key)
	wantStatus(t, cb, http.StatusCreated)
	deliveries := decodeData[[]store.Delivery](t, e.do(t, "GET", "/api/runs/"+decodeData[model.Run](t, cb).ID+"/deliveries", nil))
	if len(deliveries) != 1 || deliveries[0].Status != "pending" {
		t.Errorf("deliveries = %+v", deliveries)
	}
	wantStatus(t, e.do(t, "POST", "/api/crews/"+itCrew.ID+"/runs", map[string]any{"callback_url": "https://hooks.example.com/x"}),
		http.StatusUnprocessableEntity)
	// The Finance crew is outside the key's areas
	pe := e.crewByName(t, "PE Due Diligence Crew")
	wantStatus(t, api.do(t, "POST", "/api/crews/"+pe.ID+"/runs", map[string]any{}, "Authorization", key), http.StatusNotFound)

	secret := decodeData[signingSecret](t, e.do(t, "GET", "/api/admin/users/"+sa.ID+"/signing-secret", nil))
	if len(secret.SigningSecret) < 32 {
		t.Errorf("signing secret = %q", secret.SigningSecret)
	}
	wantStatus(t, e.do(t, "DELETE", "/api/admin/users/"+sa.ID+"/api-keys/"+created.Key.ID, nil), http.StatusNoContent)
	wantStatus(t, api.do(t, "GET", "/api/auth/me", nil, "Authorization", key), http.StatusUnauthorized)
}

func TestSSOSignIn(t *testing.T) {
	e := newEnv(t)
	anon := e.as("")
	start := anon.do(t, "GET", "/api/auth/fake/login?return_to=/work-orders", nil)
	if start.status != http.StatusFound || !strings.HasPrefix(start.header.Get("Location"), "https://idp.example/") {
		t.Fatalf("start = %d %s", start.status, start.header.Get("Location"))
	}
	var state string
	for _, c := range start.cookies {
		if c.Name == loginCookie {
			state = c.Value
		}
	}
	callback := func(code string) response {
		req, _ := http.NewRequest("GET", e.srv.URL+"/api/auth/fake/callback?code="+code+"&state="+url.QueryEscape(state), nil)
		req.AddCookie(&http.Cookie{Name: loginCookie, Value: state})
		return e.send(t, req)
	}

	// Unknown person, auto-provisioning off
	e.sso.identity = auth.Identity{Provider: "fake", Subject: "sub-1", Email: "newbie@example.com", Name: "Newbie"}
	r := callback("good")
	if r.status != http.StatusFound || !strings.HasSuffix(r.header.Get("Location"), "/login?error=not_invited") {
		t.Fatalf("not invited = %d %s", r.status, r.header.Get("Location"))
	}

	// Invited by email: first sign-in links the identity
	e.person(t, "newbie@example.com", []string{viewerRole}, []string{financeArea}, nil)
	start = anon.do(t, "GET", "/api/auth/fake/login?return_to=//evil.example", nil)
	for _, c := range start.cookies {
		if c.Name == loginCookie {
			state = c.Value
		}
	}
	r = callback("good")
	if r.status != http.StatusFound || r.header.Get("Location") != e.srv.URL+"/" {
		t.Fatalf("callback = %d %s (an off-site return_to must become /)", r.status, r.header.Get("Location"))
	}
	var session string
	for _, c := range r.cookies {
		if c.Name == sessionCookie {
			session = c.Value
		}
	}
	me := decodeData[meResponse](t, anon.as(session).do(t, "GET", "/api/auth/me", nil))
	if me.Email != "newbie@example.com" {
		t.Errorf("me = %+v", me)
	}
	// A state cannot be replayed
	r = callback("good")
	if !strings.HasSuffix(r.header.Get("Location"), "error=expired") {
		t.Errorf("replay = %s", r.header.Get("Location"))
	}
}

func TestSafeReturnTo(t *testing.T) {
	for in, want := range map[string]string{
		"/work-orders/1": "/work-orders/1", "": "/", "//evil.com": "/", "https://evil.com": "/", "/\\evil.com": "/",
		"/api/auth/logout": "/", "/a\r\nb": "/",
	} {
		if got := safeReturnTo(in); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}
