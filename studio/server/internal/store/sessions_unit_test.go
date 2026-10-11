package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestSessionLifecycle(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{Permissions: []string{auth.RunsView}})

	tok := must(f.s.CreateSession(bg(), u.ID, "local", "10.0.0.1", "agent/1.0"))
	p, err := f.s.SessionPrincipal(bg(), tok)
	mustOK(t, err)
	if p.UserID != u.ID || p.Via != auth.ViaSession || p.SessionHash != auth.HashToken(tok) || !p.Can(auth.RunsView) {
		t.Errorf("principal = %+v", p)
	}
	// only the hash is stored
	var n int
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM sessions WHERE token_hash = $1`, tok).Scan(&n))
	if n != 0 {
		t.Error("raw token stored")
	}
	_, err = f.s.SessionPrincipal(bg(), "not-a-token")
	wantNotFound(t, err)
	_, err = f.s.SessionPrincipal(bg(), "")
	wantNotFound(t, err)

	mustOK(t, f.s.DeleteSession(bg(), p.SessionHash))
	_, err = f.s.SessionPrincipal(bg(), tok)
	wantNotFound(t, err)
	mustOK(t, f.s.DeleteSession(bg(), p.SessionHash)) // idempotent
}

func TestSessionExpiryIdleAndAbsolute(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{})
	hashOf := auth.HashToken

	tests := []struct {
		name  string
		sql   string
		valid bool
	}{
		{"fresh", `SELECT 1`, true},
		{"idle just under limit", `UPDATE sessions SET last_seen_at = now() - interval '11 hours 59 minutes' WHERE token_hash = $1`, true},
		{"idle over limit", `UPDATE sessions SET last_seen_at = now() - interval '12 hours 1 minute' WHERE token_hash = $1`, false},
		{"absolute expiry passed", `UPDATE sessions SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, false},
		{"absolute expiry ahead", `UPDATE sessions SET expires_at = now() + interval '1 second' WHERE token_hash = $1`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := must(f.s.CreateSession(bg(), u.ID, "local", "ip", "ua"))
			if strings.Contains(tt.sql, "$1") {
				f.exec(tt.sql, hashOf(tok))
			}
			_, err := f.s.SessionPrincipal(bg(), tok)
			if tt.valid && err != nil {
				t.Errorf("want valid, got %v", err)
			}
			if !tt.valid {
				wantNotFound(t, err)
			}
		})
	}
}

func TestSessionLastSeenTouchIsThrottled(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{})
	tok := must(f.s.CreateSession(bg(), u.ID, "local", "ip", "ua"))
	h := auth.HashToken(tok)
	lastSeen := func() time.Time {
		var ts time.Time
		mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT last_seen_at FROM sessions WHERE token_hash = $1`, h).Scan(&ts))
		return ts
	}
	before := lastSeen()
	_, err := f.s.SessionPrincipal(bg(), tok)
	mustOK(t, err)
	if !lastSeen().Equal(before) {
		t.Error("touched within the first minute")
	}
	f.exec(`UPDATE sessions SET last_seen_at = now() - interval '5 minutes' WHERE token_hash = $1`, h)
	stale := lastSeen()
	_, err = f.s.SessionPrincipal(bg(), tok)
	mustOK(t, err)
	if !lastSeen().After(stale.Add(4 * time.Minute)) {
		t.Error("stale session not refreshed")
	}
}

func TestSessionUserAgentSanitised(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{})
	long := strings.Repeat("x", 400)
	tok := must(f.s.CreateSession(bg(), u.ID, "local", "ip", long))
	var ua string
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT user_agent FROM sessions WHERE token_hash = $1`, auth.HashToken(tok)).Scan(&ua))
	if len(ua) != 300 {
		t.Errorf("ua length %d", len(ua))
	}
	tok = must(f.s.CreateSession(bg(), u.ID, "local", "ip", "a\x00b\xffc"))
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT user_agent FROM sessions WHERE token_hash = $1`, auth.HashToken(tok)).Scan(&ua))
	if ua != "abc" {
		t.Errorf("ua = %q (NUL and invalid UTF-8 must be stripped)", ua)
	}
	// truncating mid-rune must not break the insert
	multi := strings.Repeat("é", 200) // 400 bytes; cut at 300 lands inside a rune boundary? 300 is even so fine; use odd prefix
	if _, err := f.s.CreateSession(bg(), u.ID, "local", "ip", "x"+multi); err != nil {
		t.Errorf("mid-rune truncation: %v", err)
	}
	if _, err := f.s.CreateSession(bg(), "99999999-0000-4000-8000-000000000000", "local", "ip", "ua"); err == nil {
		t.Error("session for unknown user must fail")
	}
}

func TestDeleteUserSessionsAndPurge(t *testing.T) {
	f := newFixture(t)
	f.admin()
	u := f.human("p@example.com", model.UserInput{})
	v := f.human("q@example.com", model.UserInput{})
	t1 := must(f.s.CreateSession(bg(), u.ID, "local", "", ""))
	must(f.s.CreateSession(bg(), u.ID, "local", "", ""))
	t3 := must(f.s.CreateSession(bg(), v.ID, "local", "", ""))
	if n := must(f.s.DeleteUserSessions(bg(), u.ID)); n != 2 {
		t.Errorf("ended %d", n)
	}
	_, err := f.s.SessionPrincipal(bg(), t1)
	wantNotFound(t, err)
	_, err = f.s.SessionPrincipal(bg(), t3)
	mustOK(t, err)
	if n := must(f.s.DeleteUserSessions(bg(), u.ID)); n != 0 {
		t.Errorf("second call ended %d", n)
	}

	// purge removes expired and idle sessions plus stale login states, keeping live ones
	expired := must(f.s.CreateSession(bg(), v.ID, "local", "", ""))
	idle := must(f.s.CreateSession(bg(), v.ID, "local", "", ""))
	f.exec(`UPDATE sessions SET expires_at = now() - interval '1 minute' WHERE token_hash = $1`, auth.HashToken(expired))
	f.exec(`UPDATE sessions SET last_seen_at = now() - interval '13 hours' WHERE token_hash = $1`, auth.HashToken(idle))
	mustOK(t, f.s.SaveLoginState(bg(), "old-state", LoginState{Provider: "clerk", Nonce: "n", CodeVerifier: "v"}))
	mustOK(t, f.s.SaveLoginState(bg(), "new-state", LoginState{Provider: "clerk", Nonce: "n", CodeVerifier: "v"}))
	f.exec(`UPDATE login_states SET expires_at = now() - interval '1 minute' WHERE state_hash = $1`, auth.HashToken("old-state"))
	mustOK(t, f.s.PurgeExpired(bg()))
	var sessions, states int
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM sessions`).Scan(&sessions))
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM login_states`).Scan(&states))
	if sessions != 1 || states != 1 {
		t.Errorf("after purge: %d sessions, %d login states; want 1 and 1", sessions, states)
	}
}

func TestLoginStateIsSingleUseAndExpires(t *testing.T) {
	s := newUnitStore(t)
	ls := LoginState{Provider: "clerk", Nonce: "nonce", CodeVerifier: "verifier", ReturnTo: "/runs"}
	mustOK(t, s.SaveLoginState(bg(), "state-1", ls))
	got, err := s.TakeLoginState(bg(), "state-1")
	mustOK(t, err)
	if got != ls {
		t.Errorf("got %+v", got)
	}
	_, err = s.TakeLoginState(bg(), "state-1")
	wantNotFound(t, err)
	_, err = s.TakeLoginState(bg(), "never-saved")
	wantNotFound(t, err)

	mustOK(t, s.SaveLoginState(bg(), "state-2", ls))
	if _, err := s.pool.Exec(bg(), `UPDATE login_states SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	_, err = s.TakeLoginState(bg(), "state-2")
	wantNotFound(t, err)
	// the same state twice conflicts (hash is the primary key)
	mustOK(t, s.SaveLoginState(bg(), "dup", ls))
	if err := s.SaveLoginState(bg(), "dup", ls); err == nil {
		t.Error("duplicate state accepted")
	}
	// concurrent takers: exactly one wins
	mustOK(t, s.SaveLoginState(bg(), "race", ls))
	wins := 0
	for _, e := range parallel(6, func(int) error { _, err := s.TakeLoginState(bg(), "race"); return err }) {
		if e == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("%d concurrent takers succeeded, want 1", wins)
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	f := newFixture(t)
	f.admin()
	human := f.human("p@example.com", model.UserInput{})
	svc := f.service("bot", model.UserInput{Permissions: []string{auth.CrewsRun}})
	ctx := WithActor(bg(), Actor{ID: human.ID, Name: "Pat"})

	_, _, err := f.s.CreateAPIKey(ctx, human.ID, "k", nil)
	wantConflict(t, err, "service accounts")
	_, _, err = f.s.CreateAPIKey(ctx, "99999999-0000-4000-8000-000000000000", "k", nil)
	wantNotFound(t, err)

	key, secret, err := f.s.CreateAPIKey(ctx, svc.ID, "ci", nil)
	mustOK(t, err)
	if !strings.HasPrefix(secret, "csk_"+key.Prefix+"_") || key.UserID != svc.ID || key.RevokedAt != nil {
		t.Errorf("key %+v secret %q", key, secret)
	}
	p, err := f.s.APIKeyPrincipal(bg(), secret)
	mustOK(t, err)
	if p.UserID != svc.ID || p.Via != auth.ViaAPIKey || p.APIKeyID != key.ID || !p.Can(auth.CrewsRun) || p.Kind != auth.KindService {
		t.Errorf("principal = %+v", p)
	}
	if got := keysOf(t, f.s, svc.ID); len(got) != 1 || got[0].LastUsedAt == nil {
		t.Errorf("last used not recorded: %+v", got)
	}

	// the secret never reaches revisions
	revs, _, _ := f.s.ListRevisions(bg(), RevisionFilter{EntityType: "api_key", Types: []string{"api_key"}, Scope: AllAreas, Limit: 10})
	if len(revs) != 1 || strings.Contains(toJSON(revs[0].Snapshot), secret) {
		t.Errorf("revisions = %+v", revs)
	}

	// malformed, tampered and unknown keys
	for name, bad := range map[string]string{
		"empty":     "",
		"garbage":   "hello",
		"tampered":  secret[:len(secret)-1] + flip(secret[len(secret)-1]),
		"wrongkind": "cst_" + secret[4:],
		"unknown":   "csk_abcdefgh_" + strings.Repeat("z", 43),
	} {
		if _, err := f.s.APIKeyPrincipal(bg(), bad); !IsNotFound(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	// revoke: idempotent, scoped to owner
	_, _, err = f.s.RevokeAPIKey(bg(), human.ID, key.ID)
	wantNotFound(t, err)
	rev1, changed1, err := f.s.RevokeAPIKey(ctx, svc.ID, key.ID)
	mustOK(t, err)
	rev2, changed2, err := f.s.RevokeAPIKey(ctx, svc.ID, key.ID)
	mustOK(t, err)
	if rev1.RevokedAt == nil || !rev1.RevokedAt.Equal(*rev2.RevokedAt) {
		t.Error("second revoke must keep the first time")
	}
	if !changed1 || changed2 {
		t.Errorf("changed = %v, %v; only the first revoke changes the key", changed1, changed2)
	}
	if revs, _, _ := f.s.ListRevisions(bg(), RevisionFilter{EntityType: "api_key", Types: []string{"api_key"}, Scope: AllAreas, Limit: 10}); len(revs) != 2 {
		t.Errorf("revisions after create and two revokes = %d, want 2", len(revs))
	}
	_, err = f.s.APIKeyPrincipal(bg(), secret)
	wantNotFound(t, err)
	_, _, err = f.s.RevokeAPIKey(ctx, svc.ID, "99999999-0000-4000-8000-000000000000")
	wantNotFound(t, err)
}

func flip(b byte) string {
	if b == 'A' {
		return "B"
	}
	return "A"
}

func TestAPIKeyExpiryAndDisabledOwner(t *testing.T) {
	f := newFixture(t)
	f.admin()
	svc := f.service("bot", model.UserInput{})

	past := time.Now().Add(-time.Minute)
	_, expired, err := f.s.CreateAPIKey(bg(), svc.ID, "old", &past)
	mustOK(t, err)
	_, err = f.s.APIKeyPrincipal(bg(), expired)
	wantNotFound(t, err)

	future := time.Now().Add(time.Hour)
	k, live, err := f.s.CreateAPIKey(bg(), svc.ID, "new", &future)
	mustOK(t, err)
	if k.ExpiresAt == nil {
		t.Error("expiry lost")
	}
	_, err = f.s.APIKeyPrincipal(bg(), live)
	mustOK(t, err)

	_, err = f.s.UpdateUser(bg(), svc.ID, model.UserInput{Kind: auth.KindService, Name: "bot", Status: model.UserDisabled})
	mustOK(t, err)
	_, err = f.s.APIKeyPrincipal(bg(), live)
	wantNotFound(t, err)

	// listing: newest first, empty slice for none
	other := f.service("other", model.UserInput{})
	if got := keysOf(t, f.s, other.ID); got == nil || len(got) != 0 {
		t.Errorf("empty list = %#v", got)
	}
	if got := keysOf(t, f.s, svc.ID); len(got) != 2 || !got[0].CreatedAt.After(got[1].CreatedAt) {
		t.Errorf("order = %+v", got)
	}
	// same name for the same account is a conflict? (not unique) - just ensure deleting the user removes keys
	mustOK(t, f.s.DeleteUser(bg(), svc.ID))
	if got := keysOf(t, f.s, svc.ID); len(got) != 0 {
		t.Error("keys outlive their account")
	}
}

func TestStillValid(t *testing.T) {
	f := newFixture(t)
	f.admin()
	role := must(f.s.CreateRole(bg(), model.Role{Name: "R", Permissions: []string{auth.RunsView}}))
	u := f.human("p@example.com", model.UserInput{RoleIDs: []string{role.ID}})
	svc := f.service("bot", model.UserInput{})

	tok := must(f.s.CreateSession(bg(), u.ID, "local", "", ""))
	sp := must(f.s.SessionPrincipal(bg(), tok))
	fresh, err := f.s.StillValid(bg(), sp)
	mustOK(t, err)
	if fresh.Via != auth.ViaSession || fresh.SessionHash != sp.SessionHash || !fresh.Can(auth.RunsView) {
		t.Errorf("fresh = %+v", fresh)
	}
	// permissions are reloaded
	_, err = f.s.UpdateRole(bg(), role.ID, model.Role{Name: "R", Permissions: []string{auth.TasksView}})
	mustOK(t, err)
	fresh, err = f.s.StillValid(bg(), sp)
	mustOK(t, err)
	if fresh.Can(auth.RunsView) || !fresh.Can(auth.TasksView) {
		t.Errorf("stale permissions: %v", fresh.PermissionList())
	}
	// session ends: expired
	f.exec(`UPDATE sessions SET expires_at = now() - interval '1 second'`)
	_, err = f.s.StillValid(bg(), sp)
	wantNotFound(t, err)

	// API keys
	key, secret, _ := f.s.CreateAPIKey(bg(), svc.ID, "k", nil)
	kp := must(f.s.APIKeyPrincipal(bg(), secret))
	fresh, err = f.s.StillValid(bg(), kp)
	mustOK(t, err)
	if fresh.Via != auth.ViaAPIKey || fresh.APIKeyID != key.ID {
		t.Errorf("fresh = %+v", fresh)
	}
	f.exec(`UPDATE api_keys SET expires_at = now() - interval '1 second'`)
	_, err = f.s.StillValid(bg(), kp)
	wantNotFound(t, err)
	f.exec(`UPDATE api_keys SET expires_at = NULL`)
	_, err = f.s.StillValid(bg(), kp)
	mustOK(t, err)
	_, _, err = f.s.RevokeAPIKey(bg(), svc.ID, key.ID)
	mustOK(t, err)
	_, err = f.s.StillValid(bg(), kp)
	wantNotFound(t, err)

	// system principals have neither; they are valid while the user is
	sys := must(f.s.LoadPrincipal(bg(), u.ID))
	sys.Via = auth.ViaSystem
	if got, err := f.s.StillValid(bg(), sys); err != nil || got.Via != auth.ViaSystem {
		t.Errorf("system = %+v %v", got, err)
	}
	// deleted user
	mustOK(t, f.s.DeleteUser(bg(), u.ID))
	_, err = f.s.StillValid(bg(), sys)
	wantNotFound(t, err)
}

func TestAccessSettings(t *testing.T) {
	f := newFixture(t)
	f.admin()
	def := must(f.s.GetAccessSettings(bg()))
	if def.SSOAutoProvision || def.AutoProvisionRoleID != nil || def.CallbackAllowedHosts == nil || len(def.CallbackAllowedHosts) != 0 {
		t.Errorf("defaults = %+v", def)
	}
	role := must(f.s.CreateRole(bg(), model.Role{Name: "R"}))
	out, err := f.s.UpdateAccessSettings(bg(), model.AccessSettings{SSOAutoProvision: true, AutoProvisionRoleID: &role.ID,
		CallbackAllowedHosts: []string{"internal.example.com"}})
	mustOK(t, err)
	got := must(f.s.GetAccessSettings(bg()))
	if !got.SSOAutoProvision || *got.AutoProvisionRoleID != role.ID || len(got.CallbackAllowedHosts) != 1 || out.CallbackAllowedHosts[0] != "internal.example.com" {
		t.Errorf("got = %+v", got)
	}
	// nil hosts are stored as empty
	out, err = f.s.UpdateAccessSettings(bg(), model.AccessSettings{})
	mustOK(t, err)
	if out.CallbackAllowedHosts == nil {
		t.Error("nil hosts leaked")
	}
	// an unknown role is refused
	bad := "99999999-0000-4000-8000-000000000000"
	_, err = f.s.UpdateAccessSettings(bg(), model.AccessSettings{AutoProvisionRoleID: &bad})
	wantConflict(t, err, "does not exist")
	// deleting the role resets the pointer
	_, err = f.s.UpdateAccessSettings(bg(), model.AccessSettings{AutoProvisionRoleID: &role.ID})
	mustOK(t, err)
	mustOK(t, f.s.DeleteRole(bg(), role.ID))
	if got := must(f.s.GetAccessSettings(bg())); got.AutoProvisionRoleID != nil {
		t.Errorf("dangling role id: %v", *got.AutoProvisionRoleID)
	}
}

func TestAuditLog(t *testing.T) {
	f := newFixture(t)
	uid := f.admin()
	if ev, total, err := f.s.ListAudit(bg(), 10, 0); err != nil || total != 0 || ev == nil || len(ev) != 0 {
		t.Fatalf("empty audit = %v %d %v", ev, total, err)
	}
	for i, a := range []string{"login", "logout", "login_failed", "login", "login"} {
		e := AuditEntry{Action: a, ActorName: "n", IP: "1.1.1.1"}
		if i%2 == 0 {
			e.ActorID = uid
			e.Details = map[string]any{"i": i, "provider": "local"}
		}
		mustOK(t, f.s.AppendAudit(bg(), e))
	}
	page1, total, err := f.s.ListAudit(bg(), 2, 0)
	mustOK(t, err)
	if total != 5 || len(page1) != 2 || page1[0].ID < page1[1].ID {
		t.Errorf("page1 = %+v (total %d)", page1, total)
	}
	page3, _, _ := f.s.ListAudit(bg(), 2, 4)
	if len(page3) != 1 || page3[0].Action != "login" {
		t.Errorf("last page = %+v", page3)
	}
	if beyond, _, _ := f.s.ListAudit(bg(), 2, 10); len(beyond) != 0 {
		t.Errorf("beyond = %+v", beyond)
	}
	all, _, _ := f.s.ListAudit(bg(), 10, 0)
	oldest := all[len(all)-1]
	if oldest.ActorID == nil || *oldest.ActorID != uid || oldest.Details["provider"] != "local" || oldest.IP != "1.1.1.1" {
		t.Errorf("oldest = %+v", oldest)
	}
	if all[len(all)-2].ActorID != nil || all[len(all)-2].Details == nil {
		t.Errorf("no actor / nil details should be null / {}: %+v", all[len(all)-2])
	}
	if err := f.s.AppendAudit(bg(), AuditEntry{Action: "x", ActorID: "not-a-uuid"}); err == nil {
		t.Error("bad actor id accepted")
	}
}
