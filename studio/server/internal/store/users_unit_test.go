package store

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

func TestListPermissionsMatchesCatalog(t *testing.T) {
	s := newUnitStore(t)
	defs := must(s.ListPermissions(bg()))
	keys := make([]string, len(defs))
	for i, d := range defs {
		keys[i] = d.Key
		if d.Label == "" || d.Feature == "" {
			t.Errorf("permission %s lacks label/feature", d.Key)
		}
	}
	if !slices.Equal(keys, auth.Catalog) {
		t.Errorf("stored permissions differ from catalog:\n%v\n%v", keys, auth.Catalog)
	}
}

func TestEffectivePermissionsFromRolesAndDirectGrants(t *testing.T) {
	f := newFixture(t)
	viewer := must(f.s.CreateRole(bg(), model.Role{Name: "T-Viewer", Permissions: []string{auth.CrewsView, auth.RunsView}}))
	editor := must(f.s.CreateRole(bg(), model.Role{Name: "T-Editor", Permissions: []string{auth.RunsView, auth.CrewsEdit}}))
	f.admin()

	u := f.human("p@example.com", model.UserInput{RoleIDs: []string{viewer.ID, editor.ID}, Permissions: []string{auth.AdminAudit, auth.CrewsView}})
	want := []string{auth.CrewsView, auth.CrewsEdit, auth.RunsView, auth.AdminAudit}
	if !sameSet(u.EffectivePermissions, want) {
		t.Errorf("effective = %v, want %v", u.EffectivePermissions, want)
	}
	if !sameSet(u.Permissions, []string{auth.AdminAudit, auth.CrewsView}) {
		t.Errorf("direct = %v", u.Permissions)
	}
	// effective permissions come back in catalog order
	idx := func(k string) int { return slices.Index(auth.Catalog, k) }
	if !slices.IsSortedFunc(u.EffectivePermissions, func(a, b string) int { return idx(a) - idx(b) }) {
		t.Errorf("not in catalog order: %v", u.EffectivePermissions)
	}

	p, err := f.s.LoadPrincipal(bg(), u.ID)
	mustOK(t, err)
	if !p.Can(auth.RunsView) || !p.Can(auth.AdminAudit) || p.Can(auth.AdminUsers) || p.AllAreas || p.Email != "p@example.com" || p.Kind != auth.KindHuman {
		t.Errorf("principal = %+v", p)
	}
	// the Administrator role grants everything, even permissions added later
	adm, err := f.s.LoadPrincipal(bg(), f.s.mustAdminID(t))
	mustOK(t, err)
	for _, k := range auth.Catalog {
		if !adm.Can(k) {
			t.Errorf("administrator lacks %s", k)
		}
	}
	if !adm.AllAreas {
		t.Error("first admin should see all areas")
	}

	// changing a role changes every holder
	_, err = f.s.UpdateRole(bg(), viewer.ID, model.Role{Name: "T-Viewer", Permissions: []string{auth.TasksView}})
	mustOK(t, err)
	got := must(f.s.GetUser(bg(), u.ID))
	if slices.Contains(got.EffectivePermissions, auth.CrewsView) == false {
		// crews.view is still granted directly
		t.Error("direct grant lost")
	}
	if !slices.Contains(got.EffectivePermissions, auth.TasksView) {
		t.Errorf("role change not applied: %v", got.EffectivePermissions)
	}
}

func (s *Store) mustAdminID(t *testing.T) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(bg(), `SELECT id FROM users WHERE lower(email) = 'admin@example.com'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLoadPrincipalRefusesDisabledAndMissing(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{})
	_, err := f.s.UpdateUser(bg(), u.ID, model.UserInput{Kind: auth.KindHuman, Email: "p@example.com", Name: "P", Status: model.UserDisabled})
	mustOK(t, err)
	_, err = f.s.LoadPrincipal(bg(), u.ID)
	wantNotFound(t, err)
	_, err = f.s.LoadPrincipal(bg(), "99999999-0000-4000-8000-000000000000")
	wantNotFound(t, err)
}

func TestUserCRUDAndConflicts(t *testing.T) {
	f := newFixture(t)
	f.admin()
	a := f.area("Ops")
	u := f.human("Pat@Example.com", model.UserInput{Name: "Pat", AreaIDs: []string{a}})
	if *u.Email != "pat@example.com" || u.HasPassword || len(u.AreaIDs) != 1 || u.Kind != auth.KindHuman {
		t.Errorf("user = %+v", u)
	}
	_, err := f.s.CreateUser(bg(), model.UserInput{Kind: auth.KindHuman, Email: "PAT@example.com", Name: "x", Status: model.UserActive})
	wantConflict(t, err, "already exists")
	_, err = f.s.CreateUser(bg(), model.UserInput{Kind: auth.KindHuman, Email: "q@example.com", Name: "x", Status: model.UserActive,
		RoleIDs: []string{"99999999-0000-4000-8000-000000000000"}})
	wantConflict(t, err, "does not exist")
	if _, err := f.s.FindLocalLogin(bg(), "q@example.com"); !errors.Is(err, ErrNotFound) {
		t.Error("failed create must roll back")
	}

	svc := f.service("bot", model.UserInput{})
	if svc.Email != nil || svc.Kind != auth.KindService {
		t.Errorf("service = %+v", svc)
	}
	_, err = f.s.CreateUser(bg(), model.UserInput{Kind: auth.KindService, Name: "BOT", Status: model.UserActive})
	wantConflict(t, err, "BOT")

	humans := must(f.s.ListUsers(bg(), auth.KindHuman))
	services := must(f.s.ListUsers(bg(), auth.KindService))
	if len(humans) != 2 || len(services) != 1 || services[0].ID != svc.ID {
		t.Errorf("lists: %d humans, %d services", len(humans), len(services))
	}
	if humans[0].Name != "Admin" { // ordered by lower(name)
		t.Errorf("order = %s first", humans[0].Name)
	}

	// UpdateUser cannot change kind: wrong kind -> not found
	_, err = f.s.UpdateUser(bg(), u.ID, model.UserInput{Kind: auth.KindService, Name: "x", Status: model.UserActive})
	wantNotFound(t, err)
	up, err := f.s.UpdateUser(bg(), u.ID, model.UserInput{Kind: auth.KindHuman, Email: "pat2@example.com", Name: "Pat 2", Status: model.UserActive, AllAreas: true})
	mustOK(t, err)
	if *up.Email != "pat2@example.com" || !up.AllAreas || len(up.AreaIDs) != 0 {
		t.Errorf("up = %+v", up)
	}
	_, err = f.s.UpdateUser(bg(), u.ID, model.UserInput{Kind: auth.KindHuman, Email: "admin@example.com", Name: "x", Status: model.UserActive})
	wantConflict(t, err, "admin@example.com")
	_, err = f.s.GetUser(bg(), "99999999-0000-4000-8000-000000000000")
	wantNotFound(t, err)
	_, err = f.s.UpdateUser(bg(), "99999999-0000-4000-8000-000000000000", model.UserInput{Kind: auth.KindHuman, Email: "z@example.com", Name: "z", Status: model.UserActive})
	wantNotFound(t, err)
	wantNotFound(t, f.s.DeleteUser(bg(), "99999999-0000-4000-8000-000000000000"))

	mustOK(t, f.s.DeleteUser(bg(), u.ID))
	_, err = f.s.GetUser(bg(), u.ID)
	wantNotFound(t, err)
}

func TestDisablingUserEndsSessions(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{})
	tok := must(f.s.CreateSession(bg(), u.ID, "local", "1.2.3.4", "ua"))
	_, err := f.s.SessionPrincipal(bg(), tok)
	mustOK(t, err)
	_, err = f.s.UpdateUser(bg(), u.ID, model.UserInput{Kind: auth.KindHuman, Email: "p@example.com", Name: "P", Status: model.UserDisabled})
	mustOK(t, err)
	_, err = f.s.SessionPrincipal(bg(), tok)
	wantNotFound(t, err)
}

func TestLastAdminProtection(t *testing.T) {
	f := newFixture(t)
	adminID := f.admin()
	adminIn := model.UserInput{Kind: auth.KindHuman, Email: "admin@example.com", Name: "Admin", Status: model.UserActive}

	// every way of removing the only administrator is refused, and leaves state unchanged
	_, err := f.s.UpdateUser(bg(), adminID, model.UserInput{Kind: auth.KindHuman, Email: "admin@example.com", Name: "Admin", Status: model.UserDisabled})
	if !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable last admin: %v", err)
	}
	_, err = f.s.UpdateUser(bg(), adminID, adminIn) // drops the Administrator role
	if !errors.Is(err, ErrLastAdmin) {
		t.Errorf("remove role from last admin: %v", err)
	}
	if err := f.s.DeleteUser(bg(), adminID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete last admin: %v", err)
	}
	got := must(f.s.GetUser(bg(), adminID))
	if got.Status != model.UserActive || !slices.Contains(got.RoleIDs, auth.AdministratorRoleID) {
		t.Errorf("admin changed despite refusal: %+v", got)
	}
	var cerr *ConflictError
	if !errors.As(ErrLastAdmin, &cerr) || !strings.Contains(cerr.Message, "Manage users") {
		t.Error("ErrLastAdmin message")
	}

	// a custom role that carries admin.users counts as an administrator
	adminRole := must(f.s.CreateRole(bg(), model.Role{Name: "UserAdmin", Permissions: []string{auth.AdminUsers}}))
	second := f.human("second@example.com", model.UserInput{RoleIDs: []string{adminRole.ID}})
	// service accounts with admin.users do not count
	f.service("svc", model.UserInput{Permissions: []string{auth.AdminUsers}})

	mustOK(t, f.s.DeleteUser(bg(), adminID))
	// now `second` is the last one
	if err := f.s.DeleteUser(bg(), second.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("service accounts must not count as administrators: %v", err)
	}
	if _, err := f.s.UpdateRole(bg(), adminRole.ID, model.Role{Name: "UserAdmin", Permissions: []string{auth.AdminAudit}}); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("stripping admin.users from last holder's role: %v", err)
	}
	if err := f.s.DeleteRole(bg(), adminRole.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("deleting last holder's role: %v", err)
	}
	// role still intact after the refused update
	if r := must(f.s.GetRole(bg(), adminRole.ID)); !slices.Contains(r.Permissions, auth.AdminUsers) {
		t.Errorf("role changed despite refusal: %+v", r)
	}
	// granting directly lets the role go
	_, err = f.s.UpdateUser(bg(), second.ID, model.UserInput{Kind: auth.KindHuman, Email: "second@example.com", Name: "S", Status: model.UserActive,
		RoleIDs: []string{adminRole.ID}, Permissions: []string{auth.AdminUsers}})
	mustOK(t, err)
	mustOK(t, f.s.DeleteRole(bg(), adminRole.ID))
}

func TestBuiltinRoleIsImmutable(t *testing.T) {
	s := newUnitStore(t)
	r := must(s.GetRole(bg(), auth.AdministratorRoleID))
	if !r.Builtin || len(r.Permissions) != len(auth.Catalog) {
		t.Errorf("admin role = %+v", r)
	}
	_, err := s.UpdateRole(bg(), r.ID, model.Role{Name: "x"})
	wantConflict(t, err, "built in")
	wantConflict(t, s.DeleteRole(bg(), r.ID), "built in")
	_, err = s.UpdateRole(bg(), "99999999-0000-4000-8000-000000000000", model.Role{Name: "x"})
	wantNotFound(t, err)
	wantNotFound(t, s.DeleteRole(bg(), "99999999-0000-4000-8000-000000000000"))
	_, err = s.GetRole(bg(), "99999999-0000-4000-8000-000000000000")
	wantNotFound(t, err)
}

func TestRolesCRUD(t *testing.T) {
	f := newFixture(t)
	f.admin()
	role := must(f.s.CreateRole(bg(), model.Role{Name: "R", Description: "d", Permissions: []string{auth.RunsView, auth.CrewsView}}))
	if role.Builtin || role.UserCount != 0 || !slices.Equal(role.Permissions, []string{auth.CrewsView, auth.RunsView}) {
		t.Errorf("role = %+v", role) // catalog order, not input order
	}
	_, err := f.s.CreateRole(bg(), model.Role{Name: "R"})
	wantConflict(t, err, "already exists")
	_, err = f.s.CreateRole(bg(), model.Role{Name: "Bad", Permissions: []string{"nope.nothing"}})
	wantConflict(t, err, "does not exist")
	f.human("p@example.com", model.UserInput{RoleIDs: []string{role.ID}})
	if got := must(f.s.GetRole(bg(), role.ID)); got.UserCount != 1 {
		t.Errorf("user count = %d", got.UserCount)
	}
	other := must(f.s.CreateRole(bg(), model.Role{Name: "Other"}))
	_, err = f.s.UpdateRole(bg(), other.ID, model.Role{Name: "R"})
	wantConflict(t, err, "already exists")
	roles := must(f.s.ListRoles(bg()))
	if len(roles) != 6 || !roles[0].Builtin {
		t.Errorf("builtin first expected: %+v", roles)
	}
	mustOK(t, f.s.DeleteRole(bg(), role.ID))
	// deleting a role removes it from holders
	if u := must(f.s.ListUsers(bg(), auth.KindHuman)); len(u) != 2 {
		t.Fatal("users lost")
	}
}

func TestCreateFirstAdminConcurrency(t *testing.T) {
	s := newUnitStore(t)
	emails := []string{"a@x.io", "b@x.io", "c@x.io", "d@x.io", "e@x.io", "f@x.io", "g@x.io", "h@x.io"}
	results := parallel(len(emails), func(i int) idResult {
		id, err := s.CreateFirstAdmin(bg(), emails[i], "Admin "+emails[i], "hash")
		return idResult{id, err}
	})
	won := 0
	for _, r := range results {
		switch {
		case r.err == nil:
			won++
		case errors.Is(r.err, ErrSetupDone):
		default:
			t.Errorf("unexpected error: %v", r.err)
		}
	}
	if won != 1 {
		t.Fatalf("%d setups succeeded, want exactly 1", won)
	}
	if n := must(s.HumanUserCount(bg())); n != 1 {
		t.Errorf("human users = %d", n)
	}
	if _, err := s.CreateFirstAdmin(bg(), "late@x.io", "Late", "h"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("late setup = %v", err)
	}
}

func TestCreateFirstAdminCreatesAdministrator(t *testing.T) {
	s := newUnitStore(t)
	if n := must(s.HumanUserCount(bg())); n != 0 {
		t.Fatal("fresh db has users")
	}
	id, err := s.CreateFirstAdmin(bg(), "Boss@X.io", "Boss", "bcrypt-hash")
	mustOK(t, err)
	u := must(s.GetUser(bg(), id))
	if *u.Email != "boss@x.io" || !u.AllAreas || !u.HasPassword || !slices.Contains(u.RoleIDs, auth.AdministratorRoleID) {
		t.Errorf("admin = %+v", u)
	}
	l := must(s.FindLocalLogin(bg(), "BOSS@x.io"))
	if l.PasswordHash != "bcrypt-hash" || !l.Active || l.UserID != id {
		t.Errorf("login = %+v", l)
	}
	_, total, _ := s.ListRevisions(bg(), RevisionFilter{EntityType: "user", Types: []string{"user"}, Scope: AllAreas, Limit: 5})
	if total != 1 {
		t.Errorf("revisions = %d", total)
	}
}

func TestLockoutAndPasswords(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{})
	_, err := f.s.FindLocalLogin(bg(), "nobody@example.com")
	wantNotFound(t, err)

	l := must(f.s.FindLocalLogin(bg(), "p@example.com"))
	if l.PasswordHash != "" || l.LockedUntil != nil || !l.Active {
		t.Errorf("fresh login = %+v", l)
	}
	for i := 1; i < maxFailedLogins; i++ {
		mustOK(t, f.s.RecordLoginFailure(bg(), u.ID))
		if l := must(f.s.FindLocalLogin(bg(), "p@example.com")); l.LockedUntil != nil {
			t.Fatalf("locked after only %d failures", i)
		}
	}
	mustOK(t, f.s.RecordLoginFailure(bg(), u.ID))
	l = must(f.s.FindLocalLogin(bg(), "p@example.com"))
	if l.LockedUntil == nil {
		t.Fatal("not locked after 5 failures")
	}
	if d := time.Until(*l.LockedUntil); d < 14*time.Minute || d > 15*time.Minute+5*time.Second {
		t.Errorf("lockout lasts %v", d)
	}
	// success clears
	mustOK(t, f.s.RecordLoginSuccess(bg(), u.ID))
	l = must(f.s.FindLocalLogin(bg(), "p@example.com"))
	if l.LockedUntil != nil {
		t.Error("success must clear lock")
	}
	if got := must(f.s.GetUser(bg(), u.ID)); got.LastLoginAt == nil {
		t.Error("last login not recorded")
	}
	// the counter restarted
	for i := 1; i < maxFailedLogins; i++ {
		mustOK(t, f.s.RecordLoginFailure(bg(), u.ID))
	}
	if l := must(f.s.FindLocalLogin(bg(), "p@example.com")); l.LockedUntil != nil {
		t.Error("counter not reset by success")
	}

	// password reset clears the lockout too
	mustOK(t, f.s.RecordLoginFailure(bg(), u.ID))
	h := "hash-1"
	mustOK(t, f.s.SetPassword(bg(), u.ID, &h))
	l = must(f.s.FindLocalLogin(bg(), "p@example.com"))
	if l.LockedUntil != nil || l.PasswordHash != "hash-1" {
		t.Errorf("after set = %+v", l)
	}
	if !must(f.s.GetUser(bg(), u.ID)).HasPassword {
		t.Error("HasPassword")
	}
	mustOK(t, f.s.SetPassword(bg(), u.ID, nil))
	if must(f.s.GetUser(bg(), u.ID)).HasPassword {
		t.Error("password not cleared")
	}
	svc := f.service("bot", model.UserInput{})
	wantNotFound(t, f.s.SetPassword(bg(), svc.ID, &h))
	wantNotFound(t, f.s.SetPassword(bg(), "99999999-0000-4000-8000-000000000000", &h))

	// revisions of password changes never contain the hash
	revs, _, _ := f.s.ListRevisions(bg(), RevisionFilter{EntityType: "user", EntityID: u.ID, Types: []string{"user"}, Scope: AllAreas, Limit: 20})
	for _, r := range revs {
		if strings.Contains(strings.ToLower(toJSON(r.Snapshot)), "hash-1") {
			t.Errorf("hash leaked into revision: %v", r.Snapshot)
		}
	}
}

func TestLoginFailureOnUnknownUserIsHarmless(t *testing.T) {
	s := newUnitStore(t)
	mustOK(t, s.RecordLoginFailure(bg(), "99999999-0000-4000-8000-000000000000"))
	mustOK(t, s.RecordLoginSuccess(bg(), "99999999-0000-4000-8000-000000000000"))
}

func TestFindLocalLoginDisabledAndServiceAccounts(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{Status: model.UserDisabled})
	if l := must(f.s.FindLocalLogin(bg(), "p@example.com")); l.Active || l.UserID != u.ID {
		t.Errorf("login = %+v", l)
	}
}

func TestResolveSSOUserLinksByEmailThenByIdentity(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("pat@example.com", model.UserInput{})
	id := auth.Identity{Provider: auth.ProviderClerk, Subject: "sub-1", Email: "pat@example.com", Name: "Pat"}

	got, err := f.s.ResolveSSOUser(bg(), id)
	mustOK(t, err)
	if got != u.ID {
		t.Fatalf("linked to %s, want %s", got, u.ID)
	}
	if ids := must(f.s.GetUser(bg(), u.ID)).Identities; len(ids) != 1 || ids[0].Provider != auth.ProviderClerk {
		t.Errorf("identities = %+v", ids)
	}
	// second sign-in finds the identity even if the email at the provider changed
	id.Email = "renamed@example.com"
	got, err = f.s.ResolveSSOUser(bg(), id)
	mustOK(t, err)
	if got != u.ID {
		t.Errorf("by identity = %s", got)
	}
	if ids := must(f.s.GetUser(bg(), u.ID)).Identities; len(ids) != 1 || ids[0].Email != "renamed@example.com" {
		t.Errorf("identity email not refreshed: %+v", ids)
	}
	// a second provider links to the same person by email
	ms := auth.Identity{Provider: auth.ProviderMicrosoft, Subject: "tid:oid", Email: "pat@example.com", Name: "Pat"}
	got, err = f.s.ResolveSSOUser(bg(), ms)
	mustOK(t, err)
	if got != u.ID || len(must(f.s.GetUser(bg(), u.ID)).Identities) != 2 {
		t.Error("second provider not linked")
	}
	// the same subject at another provider is a different identity
	other := auth.Identity{Provider: auth.ProviderClerk, Subject: "sub-2", Email: "stranger@example.com"}
	if _, err := f.s.ResolveSSOUser(bg(), other); !errors.Is(err, ErrNoAccount) {
		t.Errorf("stranger = %v", err)
	}
}

func TestResolveSSOUserNoAccountWhenAutoProvisionOff(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.ResolveSSOUser(bg(), auth.Identity{Provider: auth.ProviderClerk, Subject: "s", Email: "new@example.com", Name: "New"})
	if !errors.Is(err, ErrNoAccount) {
		t.Fatalf("err = %v", err)
	}
	if n := must(f.s.HumanUserCount(bg())); n != 0 {
		t.Errorf("a user was created despite the refusal: %d", n)
	}
	var n int
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM user_identities`).Scan(&n))
	if n != 0 {
		t.Errorf("identity rows = %d", n)
	}
}

func TestResolveSSOUserAutoProvisions(t *testing.T) {
	f := newFixture(t)
	f.admin()
	role := must(f.s.CreateRole(bg(), model.Role{Name: "Basic", Permissions: []string{auth.CrewsView}}))
	_, err := f.s.UpdateAccessSettings(bg(), model.AccessSettings{SSOAutoProvision: true, AutoProvisionRoleID: &role.ID})
	mustOK(t, err)

	id := auth.Identity{Provider: auth.ProviderClerk, Subject: "s1", Email: "new@example.com", Name: "Newbie"}
	uid, err := f.s.ResolveSSOUser(bg(), id)
	mustOK(t, err)
	u := must(f.s.GetUser(bg(), uid))
	if *u.Email != "new@example.com" || u.Name != "Newbie" || u.AllAreas || len(u.AreaIDs) != 0 || !slices.Equal(u.RoleIDs, []string{role.ID}) || u.HasPassword {
		t.Errorf("provisioned = %+v", u)
	}
	if !slices.Contains(u.EffectivePermissions, auth.CrewsView) || slices.Contains(u.EffectivePermissions, auth.AdminUsers) {
		t.Errorf("perms = %v", u.EffectivePermissions)
	}
	revs, _, _ := f.s.ListRevisions(bg(), RevisionFilter{EntityType: "user", EntityID: uid, Types: []string{"user"}, Scope: AllAreas, Limit: 5})
	if len(revs) != 1 || !strings.Contains(revs[0].ActorName, "auto-provisioned via clerk") {
		t.Errorf("revision = %+v", revs)
	}
	again, err := f.s.ResolveSSOUser(bg(), id)
	mustOK(t, err)
	if again != uid {
		t.Error("second sign-in made another user")
	}

	// without a role: still created, no permissions
	_, err = f.s.UpdateAccessSettings(bg(), model.AccessSettings{SSOAutoProvision: true})
	mustOK(t, err)
	uid2, err := f.s.ResolveSSOUser(bg(), auth.Identity{Provider: auth.ProviderClerk, Subject: "s2", Email: "bare@example.com", Name: "Bare"})
	mustOK(t, err)
	if u := must(f.s.GetUser(bg(), uid2)); len(u.RoleIDs) != 0 || len(u.EffectivePermissions) != 0 {
		t.Errorf("bare = %+v", u)
	}
}

func TestResolveSSOUserConcurrentFirstLogin(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.UpdateAccessSettings(bg(), model.AccessSettings{SSOAutoProvision: true})
	mustOK(t, err)
	id := auth.Identity{Provider: auth.ProviderClerk, Subject: "same", Email: "race@example.com", Name: "Race"}
	results := parallel(6, func(int) idResult {
		uid, err := f.s.ResolveSSOUser(bg(), id)
		return idResult{uid, err}
	})
	if n := must(f.s.HumanUserCount(bg())); n != 1 {
		t.Fatalf("%d users created by concurrent first logins, want 1", n)
	}
	ok := 0
	for _, r := range results {
		if r.err == nil {
			ok++
		}
	}
	if ok == 0 {
		t.Fatalf("no concurrent login succeeded: %v", results)
	}
	// every success points at the one user
	var want string
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT id FROM users`).Scan(&want))
	for _, r := range results {
		if r.err == nil && r.id != want {
			t.Errorf("login resolved to %s, want %s", r.id, want)
		}
	}
	if ok < len(results) {
		t.Errorf("%d of %d simultaneous first sign-ins failed instead of resolving to the one created user: %v", len(results)-ok, len(results), firstErr(results))
	}
}

func firstErr(rs []idResult) error {
	for _, r := range rs {
		if r.err != nil {
			return r.err
		}
	}
	return nil
}

type idResult struct {
	id  string
	err error
}

func TestSigningSecrets(t *testing.T) {
	f := newFixture(t)
	f.admin()
	human := f.human("p@example.com", model.UserInput{})
	svc := f.service("bot", model.UserInput{})
	sec1 := must(f.s.SigningSecret(bg(), svc.ID))
	if len(sec1) < 40 {
		t.Errorf("secret too short: %d", len(sec1))
	}
	_, err := f.s.SigningSecret(bg(), human.ID)
	wantNotFound(t, err)
	sec2 := must(f.s.RotateSigningSecret(bg(), svc.ID))
	if sec2 == sec1 || must(f.s.SigningSecret(bg(), svc.ID)) != sec2 {
		t.Error("rotation did not replace the secret")
	}
	_, err = f.s.RotateSigningSecret(bg(), human.ID)
	wantNotFound(t, err)
	// secrets never in revisions
	revs, _, _ := f.s.ListRevisions(bg(), RevisionFilter{EntityType: "user", EntityID: svc.ID, Types: []string{"user"}, Scope: AllAreas, Limit: 10})
	for _, r := range revs {
		j := toJSON(r.Snapshot)
		if strings.Contains(j, sec1) || strings.Contains(j, sec2) {
			t.Errorf("signing secret leaked into revision: %s", j)
		}
	}
}
