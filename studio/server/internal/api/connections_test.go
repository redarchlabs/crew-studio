package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crewstudio/internal/connections"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
)

// newSecretsEnv is newEnv with STUDIO_SECRETS_KEY set, so connections can be saved.
func newSecretsEnv(t *testing.T) testEnv {
	t.Helper()
	e := newEnv(t)
	box, err := secrets.NewBox(secrets.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	e.srv.Config.Handler = NewServer(Deps{
		Store: e.st, Runs: e.runs, Broker: e.broker, Tools: fakeTools{}, Launcher: launch.New(e.st, fakeTools{}, e.runs),
		Scheduler: e.sched, Files: e.files, SSO: e.sso, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Connections: connections.New(e.st, box),
	}, Options{AllowedOrigins: []string{"http://127.0.0.1:5174"}, PublicURL: e.srv.URL, LocalLogin: true}).Handler()
	return e
}

const testSecret = "super-secret-value-123"

func httpConnBody(name, baseURL string) map[string]any {
	return map[string]any{"name": name, "kind": "http", "area_ids": []string{financeArea},
		"config": map[string]string{"base_url": baseURL, "auth_header": "X-API-Key"}, "secrets": map[string]string{"auth_value": testSecret}}
}

func TestConnectionsNeedTheSecretsKey(t *testing.T) {
	e := newEnv(t)
	kinds := decodeData[connectionKindsResponse](t, e.do(t, "GET", "/api/connections/kinds", nil))
	if kinds.SecretsReady || len(kinds.Kinds) != 9 {
		t.Errorf("kinds = %+v", kinds)
	}
	r := e.do(t, "POST", "/api/connections", httpConnBody("api", "https://api.example"))
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "secrets_key_missing" {
		t.Errorf("error = %+v", r.body.Error)
	}
}

func TestConnectionSecretsAreWriteOnly(t *testing.T) {
	e := newSecretsEnv(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != testSecret {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer upstream.Close()

	if !decodeData[connectionKindsResponse](t, e.do(t, "GET", "/api/connections/kinds", nil)).SecretsReady {
		t.Fatal("secrets should be ready")
	}
	r := e.do(t, "POST", "/api/connections", httpConnBody("api", upstream.URL))
	wantStatus(t, r, http.StatusCreated)
	c := decodeData[model.Connection](t, r)
	if strings.Contains(string(r.body.Data), testSecret) || len(c.SecretKeys) != 1 || c.Config["auth_header"] != "X-API-Key" {
		t.Fatalf("created = %s", r.body.Data)
	}
	for _, path := range []string{"/api/connections", "/api/connections/" + c.ID} {
		r := e.do(t, "GET", path, nil)
		wantStatus(t, r, http.StatusOK)
		if strings.Contains(string(r.body.Data), testSecret) {
			t.Errorf("%s leaks the secret", path)
		}
	}
	res := decodeData[connections.TestResult](t, e.do(t, "POST", "/api/connections/"+c.ID+"/test", map[string]any{}))
	if !res.Supported || !res.OK {
		t.Errorf("test = %+v", res)
	}

	// Updates keep the secret and cannot change the kind or carry secrets
	update := map[string]any{"name": "api renamed", "area_ids": []string{financeArea}, "config": map[string]string{"base_url": upstream.URL, "auth_header": "X-API-Key"}}
	r = e.do(t, "PUT", "/api/connections/"+c.ID, update)
	wantStatus(t, r, http.StatusOK)
	if got := decodeData[model.Connection](t, r); got.Name != "api renamed" || got.Kind != "http" || len(got.SecretKeys) != 1 {
		t.Errorf("updated = %+v", got)
	}
	update["secrets"] = map[string]string{"auth_value": "x"}
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+c.ID, update), http.StatusUnprocessableEntity)
	delete(update, "secrets")
	update["kind"] = "jira"
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+c.ID, update), http.StatusUnprocessableEntity)

	r = e.do(t, "PUT", "/api/connections/"+c.ID+"/secret", map[string]any{"secrets": map[string]string{"auth_value": "wrong-now"}})
	wantStatus(t, r, http.StatusOK)
	if strings.Contains(string(r.body.Data), "wrong-now") {
		t.Error("replace answer leaks the secret")
	}
	res = decodeData[connections.TestResult](t, e.do(t, "POST", "/api/connections/"+c.ID+"/test", map[string]any{}))
	if res.OK || res.StatusCode != http.StatusUnauthorized {
		t.Errorf("test with the wrong secret = %+v", res)
	}
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+c.ID+"/secret", map[string]any{"secrets": map[string]string{"nope": "x"}}), http.StatusUnprocessableEntity)

	jira := e.do(t, "POST", "/api/connections", map[string]any{"name": "jira", "kind": "jira", "area_ids": []string{financeArea},
		"config": map[string]string{"site_url": "https://acme.atlassian.net", "email": "bot@acme.example"}})
	wantStatus(t, jira, http.StatusCreated)
	jc := decodeData[model.Connection](t, jira)
	if len(jc.MissingSecrets) != 1 {
		t.Errorf("jira = %+v", jc)
	}
	// Jira is tested by the tracker client (phase 4); without its token it says what is missing
	if res := decodeData[connections.TestResult](t, e.do(t, "POST", "/api/connections/"+jc.ID+"/test", map[string]any{})); res.OK || !strings.Contains(res.Message, "no account email or API token") {
		t.Errorf("jira test = %+v", res)
	}
	wantStatus(t, e.do(t, "POST", "/api/connections", httpConnBody("", "nope")), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "DELETE", "/api/connections/"+c.ID, nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "GET", "/api/connections/"+c.ID, nil), http.StatusNotFound)

	audit := e.do(t, "GET", "/api/admin/audit?limit=50", nil)
	wantStatus(t, audit, http.StatusOK)
	raw := string(audit.body.Data)
	for _, want := range []string{"connection_created", "connection_updated", "connection_secret_replaced", "connection_deleted"} {
		if !strings.Contains(raw, want) {
			t.Errorf("audit has no %s", want)
		}
	}
	if strings.Contains(raw, testSecret) || strings.Contains(raw, "wrong-now") {
		t.Error("audit log leaks a secret")
	}
}

func TestConnectionsFollowPermissionsAndAreas(t *testing.T) {
	e := newSecretsEnv(t)
	c := decodeData[model.Connection](t, e.do(t, "POST", "/api/connections", httpConnBody("finance-api", "https://api.example")))

	_, builder := e.person(t, "builder@example.com", []string{builderRole}, []string{financeArea}, nil)
	_, outsider := e.person(t, "it@example.com", []string{builderRole}, []string{itArea}, nil)
	_, viewer := e.person(t, "viewer@example.com", []string{viewerRole}, []string{financeArea}, nil)

	if got := decodeData[[]model.Connection](t, e.as(builder).do(t, "GET", "/api/connections", nil)); len(got) != 1 {
		t.Errorf("builder sees %d", len(got))
	}
	wantStatus(t, e.as(builder).do(t, "POST", "/api/connections", httpConnBody("mine", "https://x.example")), http.StatusForbidden)
	wantStatus(t, e.as(builder).do(t, "POST", "/api/connections/"+c.ID+"/test", map[string]any{}), http.StatusForbidden)
	if got := decodeData[[]model.Connection](t, e.as(outsider).do(t, "GET", "/api/connections", nil)); len(got) != 0 {
		t.Errorf("IT builder sees %d", len(got))
	}
	wantStatus(t, e.as(outsider).do(t, "GET", "/api/connections/"+c.ID, nil), http.StatusNotFound)
	wantStatus(t, e.as(viewer).do(t, "GET", "/api/connections", nil), http.StatusForbidden)

	// A connections manager in IT cannot put a connection in Finance
	_, itAdmin := e.person(t, "itadmin@example.com", []string{builderRole}, []string{itArea}, []string{"connections.manage"})
	wantStatus(t, e.as(itAdmin).do(t, "POST", "/api/connections", httpConnBody("sneaky", "https://x.example")), http.StatusUnprocessableEntity)
}

func TestCrewConnectionLinks(t *testing.T) {
	e := newSecretsEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	c := decodeData[model.Connection](t, e.do(t, "POST", "/api/connections", httpConnBody("finance-api", "https://api.example")))

	path := "/api/crews/" + crew.ID + "/connections"
	r := e.do(t, "PUT", path, map[string]any{"connection_ids": []string{c.ID, c.ID}})
	wantStatus(t, r, http.StatusOK)
	if got := decodeData[[]model.Connection](t, r); len(got) != 1 || got[0].CrewCount != 1 {
		t.Errorf("linked = %+v", got)
	}
	wantStatus(t, e.do(t, "PUT", path, map[string]any{"connection_ids": []string{"99999999-0000-4000-8000-000000000000"}}), http.StatusUnprocessableEntity)
	if got := decodeData[[]model.Connection](t, e.do(t, "GET", path, nil)); len(got) != 1 {
		t.Errorf("listed = %+v", got)
	}
	_, viewer := e.person(t, "viewer@example.com", []string{viewerRole}, []string{financeArea}, []string{"connections.view"})
	wantStatus(t, e.as(viewer).do(t, "PUT", path, map[string]any{"connection_ids": []string{}}), http.StatusForbidden)
	_, outsider := e.person(t, "it@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", path, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/crews/not-a-uuid/connections", nil), http.StatusNotFound)
}

// Review fix: a new address must never receive secrets saved for the old one, and a connection shared with an
// area the editor is not in cannot be changed by them.
func TestRetargetingAConnectionDropsItsSecrets(t *testing.T) {
	e := newSecretsEnv(t)
	c := decodeData[model.Connection](t, e.do(t, "POST", "/api/connections", httpConnBody("api", "https://api.example")))
	off := false
	same := map[string]any{"name": "api", "area_ids": []string{financeArea}, "enabled": off,
		"config": map[string]string{"base_url": "https://api.example", "auth_header": "X-API-Key"}}
	got := decodeData[model.Connection](t, e.do(t, "PUT", "/api/connections/"+c.ID, same))
	if len(got.SecretKeys) != 1 || got.Enabled {
		t.Fatalf("unchanged address keeps the secret: %+v", got)
	}
	// Leaving enabled out keeps it as it was
	delete(same, "enabled")
	if got := decodeData[model.Connection](t, e.do(t, "PUT", "/api/connections/"+c.ID, same)); got.Enabled {
		t.Error("an edit without enabled turned the connection back on")
	}
	moved := map[string]any{"name": "api", "area_ids": []string{financeArea},
		"config": map[string]string{"base_url": "https://attacker.example", "auth_header": "X-API-Key"}}
	got = decodeData[model.Connection](t, e.do(t, "PUT", "/api/connections/"+c.ID, moved))
	if len(got.SecretKeys) != 0 || len(got.MissingSecrets) != 0 || got.SecretUpdatedAt != nil {
		t.Errorf("a new address must clear the secret: %+v", got)
	}
	res := decodeData[connections.TestResult](t, e.do(t, "POST", "/api/connections/"+c.ID+"/test", map[string]any{}))
	if res.OK || !strings.Contains(res.Message, "no auth header value") {
		t.Errorf("test after retargeting = %+v", res)
	}
	moved["config"] = map[string]string{"base_url": "https://api.example/x?token=abc"}
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+c.ID, moved), http.StatusUnprocessableEntity)
	if !strings.Contains(string(e.do(t, "GET", "/api/admin/audit?limit=5", nil).body.Data), `"secrets_cleared":true`) {
		t.Error("clearing secrets should be audited")
	}
}

func TestSharedConnectionsChangeOnlyWithEveryArea(t *testing.T) {
	e := newSecretsEnv(t)
	body := httpConnBody("shared", "https://api.example")
	body["area_ids"] = []string{financeArea, itArea}
	c := decodeData[model.Connection](t, e.do(t, "POST", "/api/connections", body))
	_, financeOnly := e.person(t, "fin@example.com", []string{builderRole}, []string{financeArea}, []string{"connections.manage"})
	as := e.as(financeOnly)
	wantStatus(t, as.do(t, "GET", "/api/connections/"+c.ID, nil), http.StatusOK)
	update := map[string]any{"name": "shared", "area_ids": []string{financeArea}, "config": map[string]string{"base_url": "https://evil.example"}}
	wantStatus(t, as.do(t, "PUT", "/api/connections/"+c.ID, update), http.StatusForbidden)
	wantStatus(t, as.do(t, "PUT", "/api/connections/"+c.ID+"/secret", map[string]any{"secrets": map[string]string{"auth_value": "x"}}), http.StatusForbidden)
	wantStatus(t, as.do(t, "POST", "/api/connections/"+c.ID+"/test", map[string]any{}), http.StatusForbidden)
	wantStatus(t, as.do(t, "DELETE", "/api/connections/"+c.ID, nil), http.StatusForbidden)
}
