package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

func TestPersonalAPIKeys(t *testing.T) {
	e := newEnv(t)
	builderID, builderTok := e.person(t, "builder@example.com", []string{builderRole}, []string{itArea}, nil)
	_, viewerTok := e.person(t, "viewer@example.com", []string{viewerRole}, []string{itArea}, nil)
	me := e.as(builderTok)

	if keys := decodeData[[]model.APIKey](t, me.do(t, "GET", "/api/account/api-keys", nil)); len(keys) != 0 {
		t.Errorf("keys = %v", keys)
	}
	wantStatus(t, me.do(t, "POST", "/api/account/api-keys", map[string]any{"name": " "}), http.StatusUnprocessableEntity)
	past := time.Now().Add(-time.Hour)
	wantStatus(t, me.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "old", "expires_at": past}), http.StatusUnprocessableEntity)

	created := decodeData[newKeyResponse](t, me.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "laptop"}))
	if !strings.HasPrefix(created.Secret, "csk_"+created.Key.Prefix+"_") || created.Key.UserID != builderID {
		t.Fatalf("created = %+v", created)
	}
	key := "Bearer " + created.Secret
	api := e.as("")
	who := decodeData[meResponse](t, api.do(t, "GET", "/api/auth/me", nil, "Authorization", key))
	if who.ID != builderID || who.Kind != auth.KindHuman || who.Via != auth.ViaAPIKey {
		t.Errorf("me = %+v", who)
	}
	// A key cannot mint more keys, but can see and revoke its owner's
	wantStatus(t, api.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "x"}, "Authorization", key), http.StatusForbidden)
	if keys := decodeData[[]model.APIKey](t, api.do(t, "GET", "/api/account/api-keys", nil, "Authorization", key)); len(keys) != 1 {
		t.Errorf("keys via key = %v", keys)
	}
	// Personal keys cannot ask for callbacks; those are for service accounts
	crew := e.crewByName(t, "AI Research Crew")
	wantStatus(t, api.do(t, "POST", "/api/crews/"+crew.ID+"/runs", map[string]any{"callback_url": "https://hooks.example.com/x"},
		"Authorization", key), http.StatusUnprocessableEntity)

	// Someone without the permission can list (nothing) but not create
	viewer := e.as(viewerTok)
	wantStatus(t, viewer.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "x"}), http.StatusForbidden)
	if keys := decodeData[[]model.APIKey](t, viewer.do(t, "GET", "/api/account/api-keys", nil)); len(keys) != 0 {
		t.Errorf("viewer sees %v", keys)
	}
	// ...nor revoke someone else's
	wantStatus(t, viewer.do(t, "DELETE", "/api/account/api-keys/"+created.Key.ID, nil), http.StatusNotFound)
	wantStatus(t, viewer.do(t, "DELETE", "/api/account/api-keys/nope", nil), http.StatusNotFound)

	wantStatus(t, me.do(t, "DELETE", "/api/account/api-keys/"+created.Key.ID, nil), http.StatusNoContent)
	wantStatus(t, api.do(t, "GET", "/api/auth/me", nil, "Authorization", key), http.StatusUnauthorized)
}

func TestAdminAPIKeyList(t *testing.T) {
	e := newEnv(t)
	personID, personTok := e.person(t, "builder@example.com", []string{builderRole}, []string{itArea}, nil)
	personal := decodeData[newKeyResponse](t, e.as(personTok).do(t, "POST", "/api/account/api-keys", map[string]any{"name": "laptop"}))
	sa := decodeData[model.User](t, e.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "service", Name: "ERP",
		RoleIDs: []string{viewerRole}, AreaIDs: []string{itArea}}))
	service := decodeData[newKeyResponse](t, e.do(t, "POST", "/api/admin/users/"+sa.ID+"/api-keys", map[string]any{"name": "prod"}))

	all := decodeData[[]model.AccountAPIKey](t, e.do(t, "GET", "/api/admin/api-keys", nil))
	if len(all) != 2 || all[0].ID != service.Key.ID || all[0].UserName != "ERP" || all[1].ID != personal.Key.ID || all[1].UserKind != auth.KindHuman {
		t.Fatalf("all = %+v", all)
	}

	// Each admin permission shows its own kind of account
	_, saAdmin := e.person(t, "sa-admin@example.com", nil, []string{itArea}, []string{auth.AdminServiceAccounts})
	if got := decodeData[[]model.AccountAPIKey](t, e.as(saAdmin).do(t, "GET", "/api/admin/api-keys", nil)); len(got) != 1 || got[0].ID != service.Key.ID {
		t.Errorf("service-account admin sees %+v", got)
	}
	_, userAdmin := e.person(t, "user-admin@example.com", nil, nil, []string{auth.AdminUsers})
	if got := decodeData[[]model.AccountAPIKey](t, e.as(userAdmin).do(t, "GET", "/api/admin/api-keys", nil)); len(got) != 1 || got[0].ID != personal.Key.ID {
		t.Errorf("user admin sees %+v", got)
	}
	wantStatus(t, e.as(personTok).do(t, "GET", "/api/admin/api-keys", nil), http.StatusForbidden)

	// Admins can see and revoke a person's keys (when someone leaves) but never issue one for them
	base := "/api/admin/users/" + personID + "/api-keys"
	if got := decodeData[[]model.APIKey](t, e.do(t, "GET", base, nil)); len(got) != 1 {
		t.Errorf("person's keys = %+v", got)
	}
	wantStatus(t, e.do(t, "POST", base, map[string]any{"name": "impersonate"}), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", base+"/"+personal.Key.ID, nil), http.StatusNoContent)
	wantStatus(t, e.as("").do(t, "GET", "/api/auth/me", nil, "Authorization", "Bearer "+personal.Secret), http.StatusUnauthorized)
	// A key id must belong to the account in the path
	wantStatus(t, e.do(t, "DELETE", base+"/"+service.Key.ID, nil), http.StatusNotFound)
}

// A stolen key must not be able to plant access that outlives it: admin changes need a signed-in session.
func TestAPIKeysCannotChangeAdminSettings(t *testing.T) {
	e := newEnv(t)
	created := decodeData[newKeyResponse](t, e.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "admin laptop"}))
	key := "Bearer " + created.Secret
	api := e.as("")
	sa := decodeData[model.User](t, e.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "service", Name: "ERP",
		RoleIDs: []string{viewerRole}, AreaIDs: []string{itArea}}))

	// Reads still work
	wantStatus(t, api.do(t, "GET", "/api/admin/api-keys", nil, "Authorization", key), http.StatusOK)
	wantStatus(t, api.do(t, "GET", "/api/admin/users?kind=service", nil, "Authorization", key), http.StatusOK)
	// Anything that changes accounts, credentials or sign-in settings is refused
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/admin/users/" + sa.ID + "/api-keys"},
		{"POST", "/api/admin/users/" + sa.ID + "/signing-secret/rotate"},
		{"PUT", "/api/admin/users/" + e.adminID + "/password"},
		{"POST", "/api/admin/users"},
		{"PUT", "/api/admin/access"},
		{"POST", "/api/admin/roles"},
	} {
		r := api.do(t, c.method, c.path, map[string]any{"name": "x", "password": "a long enough password"}, "Authorization", key)
		wantStatus(t, r, http.StatusForbidden)
		if !strings.Contains(r.body.Error.Message, "signed in") {
			t.Errorf("%s %s: message = %q", c.method, c.path, r.body.Error.Message)
		}
	}
	// The signing secret is a credential too, even though reading it is a GET
	wantStatus(t, api.do(t, "GET", "/api/admin/users/"+sa.ID+"/signing-secret", nil, "Authorization", key), http.StatusForbidden)
	// The same calls work from a session
	wantStatus(t, e.do(t, "POST", "/api/admin/users/"+sa.ID+"/api-keys", map[string]any{"name": "prod"}), http.StatusCreated)
}

func TestAPIKeyListsArePagedAndFiltered(t *testing.T) {
	e := newEnv(t)
	sa := decodeData[model.User](t, e.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "service", Name: "ERP",
		RoleIDs: []string{viewerRole}, AreaIDs: []string{itArea}}))
	base := "/api/admin/users/" + sa.ID + "/api-keys"
	var ids []string
	for _, name := range []string{"one", "two", "three"} {
		ids = append(ids, decodeData[newKeyResponse](t, e.do(t, "POST", base, map[string]any{"name": name})).Key.ID)
	}
	wantStatus(t, e.do(t, "DELETE", base+"/"+ids[2], nil), http.StatusNoContent)
	mine := decodeData[newKeyResponse](t, e.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "laptop"}))

	// Per account: active first, then newest; paged with a total
	r := e.do(t, "GET", base+"?limit=2", nil)
	got := decodeData[[]model.AccountAPIKey](t, r)
	if r.body.Meta == nil || r.body.Meta.Total != 3 || len(got) != 2 || got[0].ID != ids[1] || got[1].ID != ids[0] || !got[0].CanRevoke {
		t.Fatalf("page 1 = %+v meta %+v", got, r.body.Meta)
	}
	if got := decodeData[[]model.AccountAPIKey](t, e.do(t, "GET", base+"?limit=2&offset=2", nil)); len(got) != 1 || got[0].ID != ids[2] {
		t.Errorf("page 2 = %+v", got)
	}
	// Mine
	r = e.do(t, "GET", "/api/account/api-keys?limit=1", nil)
	if got := decodeData[[]model.AccountAPIKey](t, r); len(got) != 1 || got[0].ID != mine.Key.ID || !got[0].CanRevoke || r.body.Meta.Total != 1 {
		t.Errorf("mine = %+v", got)
	}
	// Every key, filtered on the server
	for query, want := range map[string]int{
		"":                            4,
		"?kind=human":                 1,
		"?kind=service":               3,
		"?status=revoked":             1,
		"?status=active&kind=service": 2,
		"?q=lap":                      1,
		"?q=erp":                      3,
		"?status=expired":             0,
	} {
		r := e.do(t, "GET", "/api/admin/api-keys"+query, nil)
		if got := decodeData[[]model.AccountAPIKey](t, r); len(got) != want || r.body.Meta.Total != want {
			t.Errorf("%q: %d keys (total %d), want %d", query, len(got), r.body.Meta.Total, want)
		}
	}
	wantStatus(t, e.do(t, "GET", "/api/admin/api-keys?status=lost", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "GET", "/api/admin/api-keys?kind=robot", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "GET", "/api/admin/api-keys?q="+strings.Repeat("x", 201), nil), http.StatusUnprocessableEntity)
	// The limit counts characters, not bytes
	wantStatus(t, e.do(t, "GET", "/api/admin/api-keys?q="+url.QueryEscape(strings.Repeat("é", 200)), nil), http.StatusOK)
}

func TestRevokeIsOfferedOnlyWhereItWouldWork(t *testing.T) {
	e := newEnv(t)
	_, builderTok := e.person(t, "builder@example.com", []string{builderRole}, []string{itArea}, nil)
	personal := decodeData[newKeyResponse](t, e.as(builderTok).do(t, "POST", "/api/account/api-keys", map[string]any{"name": "laptop"}))
	// A user admin who lacks the builder's permissions cannot manage them
	_, weakAdmin := e.person(t, "weak@example.com", nil, nil, []string{auth.AdminUsers})
	weak := e.as(weakAdmin)
	got := decodeData[[]model.AccountAPIKey](t, weak.do(t, "GET", "/api/admin/api-keys", nil))
	if len(got) != 1 || got[0].CanRevoke {
		t.Fatalf("weak admin sees %+v", got)
	}
	base := "/api/admin/users/" + personal.Key.UserID + "/api-keys"
	if got := decodeData[[]model.AccountAPIKey](t, weak.do(t, "GET", base, nil)); len(got) != 1 || got[0].CanRevoke {
		t.Errorf("weak admin, account list = %+v", got)
	}
	r := weak.do(t, "DELETE", base+"/"+personal.Key.ID, nil)
	wantStatus(t, r, http.StatusForbidden)
	if !strings.Contains(r.body.Error.Message, "cannot change this account") {
		t.Errorf("message = %q", r.body.Error.Message)
	}
	// The full administrator can
	if got := decodeData[[]model.AccountAPIKey](t, e.do(t, "GET", "/api/admin/api-keys", nil)); len(got) != 1 || !got[0].CanRevoke {
		t.Errorf("admin sees %+v", got)
	}
}

func TestAPIKeyEventsAreAudited(t *testing.T) {
	e := newEnv(t)
	created := decodeData[newKeyResponse](t, e.do(t, "POST", "/api/account/api-keys", map[string]any{"name": "laptop"}))
	sa := decodeData[model.User](t, e.do(t, "POST", "/api/admin/users", model.UserInput{Kind: "service", Name: "ERP",
		RoleIDs: []string{viewerRole}, AreaIDs: []string{itArea}}))
	service := decodeData[newKeyResponse](t, e.do(t, "POST", "/api/admin/users/"+sa.ID+"/api-keys", map[string]any{"name": "prod"}))
	wantStatus(t, e.do(t, "DELETE", "/api/account/api-keys/"+created.Key.ID, nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "DELETE", "/api/admin/users/"+sa.ID+"/api-keys/"+service.Key.ID, nil), http.StatusNoContent)
	// The revoked key is now refused, and so is something that only looks like a key
	// (repeats from the same address are audited once per window)
	for range 3 {
		wantStatus(t, e.as("").do(t, "GET", "/api/auth/me", nil, "Authorization", "Bearer "+created.Secret), http.StatusUnauthorized)
	}
	wantStatus(t, e.as("").do(t, "GET", "/api/auth/me", nil, "Authorization", "Bearer csk_nonsense"), http.StatusUnauthorized)
	// Revoking again changes nothing and is not audited again
	wantStatus(t, e.do(t, "DELETE", "/api/account/api-keys/"+created.Key.ID, nil), http.StatusNoContent)

	events := decodeData[[]model.AuditEvent](t, e.do(t, "GET", "/api/admin/audit?limit=50", nil))
	byAction := map[string][]model.AuditEvent{}
	for _, ev := range events {
		byAction[ev.Action] = append(byAction[ev.Action], ev)
		if strings.Contains(toJSONString(ev.Details), created.Secret) || strings.Contains(toJSONString(ev.Details), service.Secret) {
			t.Errorf("secret in audit details: %+v", ev)
		}
	}
	if c := byAction["api_key_created"]; len(c) != 2 {
		t.Fatalf("created events = %+v", c)
	}
	for _, ev := range byAction["api_key_created"] {
		if ev.ActorID == nil || *ev.ActorID != e.adminID || ev.Details["prefix"] == "" || ev.Details["account_kind"] == nil {
			t.Errorf("created event = %+v", ev)
		}
	}
	if r := byAction["api_key_revoked"]; len(r) != 2 || r[0].Details["key_id"] == nil {
		t.Errorf("revoked events = %+v", r)
	}
	rejected := byAction["api_key_rejected"]
	if len(rejected) != 2 {
		t.Fatalf("rejected events = %+v", rejected)
	}
	prefixes := map[any]bool{}
	for _, ev := range rejected {
		prefixes[ev.Details["prefix"]] = true
		if ev.ActorID != nil || ev.IP == "" {
			t.Errorf("rejected event = %+v", ev)
		}
	}
	// The malformed one has no prefix to record
	if !prefixes[created.Key.Prefix] || !prefixes[nil] {
		t.Errorf("rejected prefixes = %v", prefixes)
	}
}

func toJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
