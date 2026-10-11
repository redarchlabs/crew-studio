package store

import (
	"slices"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

func TestPersonalAPIKeys(t *testing.T) {
	f := newFixture(t)
	f.admin()
	pat := f.human("pat@example.com", model.UserInput{Permissions: []string{auth.AccountAPIKeys, auth.CrewsView}})
	svc := f.service("bot", model.UserInput{})
	ctx := WithActor(bg(), Actor{ID: pat.ID, Name: "Pat"})

	_, _, err := f.s.CreatePersonalAPIKey(ctx, svc.ID, "k", nil, model.FullAccessKey())
	wantConflict(t, err, "people")
	_, _, err = f.s.CreatePersonalAPIKey(ctx, "99999999-0000-4000-8000-000000000000", "k", nil, model.FullAccessKey())
	wantNotFound(t, err)

	key, secret, err := f.s.CreatePersonalAPIKey(ctx, pat.ID, "laptop", nil, model.FullAccessKey())
	mustOK(t, err)
	p, err := f.s.APIKeyPrincipal(bg(), secret)
	mustOK(t, err)
	if p.UserID != pat.ID || p.Kind != auth.KindHuman || p.Via != auth.ViaAPIKey || p.APIKeyID != key.ID || !p.Can(auth.CrewsView) {
		t.Errorf("principal = %+v", p)
	}

	// Taking the permission away stops the person's keys at once; giving it back restores them
	_, err = f.s.UpdateUser(bg(), pat.ID, model.UserInput{Kind: auth.KindHuman, Email: "pat@example.com", Name: "Pat",
		Status: model.UserActive, Permissions: []string{auth.CrewsView}})
	mustOK(t, err)
	_, err = f.s.APIKeyPrincipal(bg(), secret)
	wantNotFound(t, err)
	_, err = f.s.StillValid(bg(), p)
	wantNotFound(t, err)
	_, err = f.s.UpdateUser(bg(), pat.ID, model.UserInput{Kind: auth.KindHuman, Email: "pat@example.com", Name: "Pat",
		Status: model.UserActive, Permissions: []string{auth.AccountAPIKeys, auth.CrewsView}})
	mustOK(t, err)
	_, err = f.s.APIKeyPrincipal(bg(), secret)
	mustOK(t, err)
	_, err = f.s.StillValid(bg(), p)
	mustOK(t, err)
}

func TestAPIKeyCapPerAccount(t *testing.T) {
	f := newFixture(t)
	f.admin()
	svc := f.service("bot", model.UserInput{})
	var first model.APIKey
	for i := range MaxActiveAPIKeys {
		k, _, err := f.s.CreateAPIKey(bg(), svc.ID, "k", nil, model.FullAccessKey())
		mustOK(t, err)
		if i == 0 {
			first = k
		}
	}
	_, _, err := f.s.CreateAPIKey(bg(), svc.ID, "one too many", nil, model.FullAccessKey())
	wantConflict(t, err, "revoke one")
	// Revoked keys do not count
	_, _, err = f.s.RevokeAPIKey(bg(), svc.ID, first.ID)
	mustOK(t, err)
	_, _, err = f.s.CreateAPIKey(bg(), svc.ID, "replacement", nil, model.FullAccessKey())
	mustOK(t, err)
}

func TestListAPIKeys(t *testing.T) {
	f := newFixture(t)
	adminID := f.admin()
	pat := f.human("pat@example.com", model.UserInput{Name: "Pat Lee", Permissions: []string{auth.AccountAPIKeys}})
	svc := f.service("ERP", model.UserInput{})
	ctx := WithActor(bg(), Actor{ID: adminID, Name: "Admin"})
	sk, _, err := f.s.CreateAPIKey(ctx, svc.ID, "prod", nil, model.FullAccessKey())
	mustOK(t, err)
	gone, _, err := f.s.CreateAPIKey(ctx, svc.ID, "old_100%", nil, model.FullAccessKey())
	mustOK(t, err)
	_, _, err = f.s.RevokeAPIKey(ctx, svc.ID, gone.ID)
	mustOK(t, err)
	past := time.Now().Add(-time.Minute)
	expired, _, err := f.s.CreateAPIKey(ctx, svc.ID, "stale", &past, model.FullAccessKey())
	mustOK(t, err)
	pk, _, err := f.s.CreatePersonalAPIKey(WithActor(bg(), Actor{ID: pat.ID, Name: "Pat Lee"}), pat.ID, "laptop", nil, model.FullAccessKey())
	mustOK(t, err)

	both := []string{auth.KindHuman, auth.KindService}
	ids := func(filter APIKeyFilter) ([]string, int) {
		t.Helper()
		filter.Limit = max(filter.Limit, 10)
		keys, total, err := f.s.ListAPIKeys(bg(), filter)
		mustOK(t, err)
		out := make([]string, len(keys))
		for i, k := range keys {
			out[i] = k.ID
		}
		return out, total
	}

	// Active keys first, each group newest first
	got, total := ids(APIKeyFilter{Kinds: both})
	if want := []string{pk.ID, sk.ID, expired.ID, gone.ID}; !slices.Equal(got, want) || total != 4 {
		t.Errorf("all = %v (%d), want %v", got, total, want)
	}
	all, _, err := f.s.ListAPIKeys(bg(), APIKeyFilter{Kinds: both, Limit: 10})
	mustOK(t, err)
	if all[0].UserName != "Pat Lee" || all[0].UserKind != auth.KindHuman || all[0].CreatedByName == nil || *all[0].CreatedByName != "Pat Lee" {
		t.Errorf("personal = %+v", all[0])
	}
	if all[1].UserName != "ERP" || all[1].UserKind != auth.KindService || *all[1].CreatedByName != "Admin" {
		t.Errorf("service = %+v", all[1])
	}

	for name, c := range map[string]struct {
		filter APIKeyFilter
		want   []string
	}{
		"one account":        {APIKeyFilter{Kinds: both, UserID: svc.ID}, []string{sk.ID, expired.ID, gone.ID}},
		"visible kinds":      {APIKeyFilter{Kinds: []string{auth.KindService}}, []string{sk.ID, expired.ID, gone.ID}},
		"no kinds":           {APIKeyFilter{}, []string{}},
		"kind filter":        {APIKeyFilter{Kinds: both, Kind: auth.KindHuman}, []string{pk.ID}},
		"kind outside kinds": {APIKeyFilter{Kinds: []string{auth.KindService}, Kind: auth.KindHuman}, []string{}},
		"active":             {APIKeyFilter{Kinds: both, Status: "active"}, []string{pk.ID, sk.ID}},
		"expired":            {APIKeyFilter{Kinds: both, Status: "expired"}, []string{expired.ID}},
		"revoked":            {APIKeyFilter{Kinds: both, Status: "revoked"}, []string{gone.ID}},
		"key name":           {APIKeyFilter{Kinds: both, Query: " PROD "}, []string{sk.ID}},
		"account name":       {APIKeyFilter{Kinds: both, Query: "lee"}, []string{pk.ID}},
		"prefix":             {APIKeyFilter{Kinds: both, Query: pk.Prefix}, []string{pk.ID}},
		"like is literal":    {APIKeyFilter{Kinds: both, Query: "0%"}, []string{gone.ID}},
		"underscore literal": {APIKeyFilter{Kinds: both, Query: "d_1"}, []string{gone.ID}},
	} {
		if got, total := ids(c.filter); !slices.Equal(got, c.want) || total != len(c.want) {
			t.Errorf("%s: %v (%d), want %v", name, got, total, c.want)
		}
	}

	// Paging keeps the total
	page, total, err := f.s.ListAPIKeys(bg(), APIKeyFilter{Kinds: both, Limit: 2, Offset: 2})
	mustOK(t, err)
	if len(page) != 2 || page[0].ID != expired.ID || total != 4 {
		t.Errorf("page 2 = %+v (%d)", page, total)
	}
}

func TestUsersByID(t *testing.T) {
	f := newFixture(t)
	f.admin()
	pat := f.human("pat@example.com", model.UserInput{Permissions: []string{auth.CrewsView}})
	svc := f.service("ERP", model.UserInput{})
	got, err := f.s.UsersByID(bg(), []string{pat.ID, svc.ID, pat.ID, "99999999-0000-4000-8000-000000000000"})
	mustOK(t, err)
	if len(got) != 2 || got[pat.ID].Name != "pat@example.com" || !slices.Contains(got[pat.ID].EffectivePermissions, auth.CrewsView) || got[svc.ID].Kind != auth.KindService {
		t.Errorf("users = %+v", got)
	}
	if got := must(f.s.UsersByID(bg(), nil)); len(got) != 0 {
		t.Errorf("none = %+v", got)
	}
}

// keysOf lists every key of one account, active first then newest.
func keysOf(t *testing.T, s *Store, userID string) []model.AccountAPIKey {
	t.Helper()
	keys, _, err := s.ListAPIKeys(bg(), APIKeyFilter{UserID: userID, Kinds: []string{auth.KindHuman, auth.KindService}, Limit: 100})
	mustOK(t, err)
	return keys
}
