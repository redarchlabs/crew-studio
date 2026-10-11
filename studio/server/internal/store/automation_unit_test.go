package store

import (
	"slices"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

func schedIn(name string, crewID string) model.Schedule {
	return model.Schedule{Name: name, CrewID: &crewID, Cron: "*/5 * * * *", Timezone: "UTC",
		OverlapPolicy: model.OverlapSkip, Enabled: true, Inputs: map[string]string{"a": "b"}, BudgetUSD: ptr(2.0)}
}

func TestScheduleSaveUpdateDelete(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("c", a)
	uid := f.admin()
	ctx := WithActor(bg(), Actor{ID: uid, Name: "Admin"})
	next := time.Now().Add(time.Hour).Truncate(time.Second)

	sc, err := f.s.SaveSchedule(ctx, "", schedIn("nightly", crew.ID), &next)
	mustOK(t, err)
	if sc.Name != "nightly" || sc.TargetName != "c" || sc.RunAs == nil || *sc.RunAs != uid || sc.RunAsName != "Admin" ||
		sc.NextRunAt == nil || !sc.NextRunAt.Equal(next) || sc.Inputs["a"] != "b" || *sc.BudgetUSD != 2 || len(sc.AreaIDs) != 1 || !sc.Enabled {
		t.Errorf("schedule = %+v", sc)
	}
	_, err = f.s.SaveSchedule(ctx, "", schedIn("nightly", crew.ID), nil)
	wantConflict(t, err, "already exists")
	_, err = f.s.SaveSchedule(ctx, "", schedIn("ghost", missingID), nil)
	wantConflict(t, err, "does not exist")

	// updating re-points run-as to the saver and clears the last error
	mustOK(t, f.s.RecordScheduleOutcome(bg(), sc.ID, nil, ptr("boom"), false))
	other := f.human("o@example.com", model.UserInput{})
	ctx2 := WithActor(bg(), Actor{ID: other.ID, Name: "Other"})
	in := schedIn("nightly2", crew.ID)
	in.Enabled = false
	up, err := f.s.SaveSchedule(ctx2, sc.ID, in, nil)
	mustOK(t, err)
	if up.Name != "nightly2" || *up.RunAs != other.ID || up.RunAsName != "Other" || up.LastError != nil || up.Enabled || up.NextRunAt != nil {
		t.Errorf("updated = %+v", up)
	}
	_, err = f.s.SaveSchedule(ctx, missingID, schedIn("x", crew.ID), nil)
	wantNotFound(t, err)
	_, err = f.s.GetSchedule(bg(), missingID)
	wantNotFound(t, err)
	wantNotFound(t, f.s.DeleteSchedule(bg(), missingID))

	revs, total, _ := f.s.ListRevisions(bg(), RevisionFilter{EntityType: "schedule", EntityID: sc.ID, Types: []string{"schedule"}, Scope: AllAreas, Limit: 10})
	if total != 2 || revs[0].Action != "update" {
		t.Errorf("revisions = %d", total)
	}
	// scoped revision visibility follows target areas
	_, total, _ = f.s.ListRevisions(bg(), RevisionFilter{EntityType: "schedule", Types: []string{"schedule"}, Scope: Scope{AreaIDs: []string{missingID}}, Limit: 10})
	if total != 0 {
		t.Errorf("revisions leaked across areas: %d", total)
	}
	mustOK(t, f.s.DeleteSchedule(bg(), sc.ID))
	if got := must(f.s.ListSchedules(bg(), AllAreas)); got == nil || len(got) != 0 {
		t.Errorf("list = %#v", got)
	}
}

func TestScheduleScopeFollowsTargetAreas(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	crewA := f.simpleCrew("ca", a)
	crewB := f.simpleCrew("cb", b)
	f.admin()
	s1 := must(f.s.SaveSchedule(bg(), "", schedIn("s1", crewA.ID), nil))
	s2 := must(f.s.SaveSchedule(bg(), "", schedIn("s2", crewB.ID), nil))
	list := func(sc Scope) []string {
		return sortedIDs(must(f.s.ListSchedules(bg(), sc)), func(x model.Schedule) string { return x.ID })
	}
	if got := list(Scope{AreaIDs: []string{a}}); !sameSet(got, []string{s1.ID}) {
		t.Errorf("A = %v", got)
	}
	if got := list(Scope{AreaIDs: []string{b}}); !sameSet(got, []string{s2.ID}) {
		t.Errorf("B = %v", got)
	}
	if got := list(Scope{AreaIDs: []string{a, b}}); len(got) != 2 {
		t.Errorf("A+B = %v", got)
	}
	if got := list(Scope{}); len(got) != 0 {
		t.Errorf("none = %v", got)
	}
	if got := list(AllAreas); len(got) != 2 {
		t.Errorf("all = %v", got)
	}
	if *s2.CrewID != crewB.ID || s2.TargetName != "cb" || !slices.Equal(s2.AreaIDs, []string{b}) {
		t.Errorf("s2 = %+v", s2)
	}
}

func TestDueSchedulesAndClaim(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	f.admin()
	now := time.Now().Truncate(time.Microsecond)
	mk := func(name string, enabled bool, next *time.Time) model.Schedule {
		in := schedIn(name, crew.ID)
		in.Enabled = enabled
		return must(f.s.SaveSchedule(bg(), "", in, next))
	}
	past1, past2, future := now.Add(-2*time.Hour), now.Add(-time.Hour), now.Add(time.Hour)
	due2 := mk("due2", true, &past2)
	due1 := mk("due1", true, &past1)
	mk("future", true, &future)
	mk("disabled", false, &past1)
	mk("never", true, nil)

	got := must(f.s.DueSchedules(bg(), now, 10))
	if len(got) != 2 || got[0].ID != due1.ID || got[1].ID != due2.ID {
		t.Fatalf("due = %+v (want oldest first, enabled only)", got)
	}
	if got := must(f.s.DueSchedules(bg(), now, 1)); len(got) != 1 || got[0].ID != due1.ID {
		t.Errorf("limit = %+v", got)
	}
	if got := must(f.s.DueSchedules(bg(), now.Add(-3*time.Hour), 10)); got == nil || len(got) != 0 {
		t.Errorf("nothing due = %#v", got)
	}
	if got := must(f.s.DueSchedules(bg(), now.Add(2*time.Hour), 10)); len(got) != 3 {
		t.Errorf("later = %d", len(got))
	}

	// only one of several concurrent claimers wins
	nextAfter := now.Add(24 * time.Hour)
	wins := 0
	for _, ok := range parallel(8, func(int) bool {
		ok, err := f.s.ClaimSchedule(bg(), due1.ID, past1, &nextAfter)
		if err != nil {
			t.Error(err)
		}
		return ok
	}) {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("%d claims won, want 1", wins)
	}
	c := must(f.s.GetSchedule(bg(), due1.ID))
	if c.NextRunAt == nil || !c.NextRunAt.Equal(nextAfter) || c.LastRunAt == nil {
		t.Errorf("claimed = %+v", c)
	}
	// a stale due time loses, a disabled schedule cannot be claimed, a nil next parks the schedule
	if ok, _ := f.s.ClaimSchedule(bg(), due1.ID, past1, &nextAfter); ok {
		t.Error("stale claim won")
	}
	disabled := must(f.s.ListSchedules(bg(), AllAreas))
	for _, sc := range disabled {
		if sc.Name == "disabled" {
			if ok, _ := f.s.ClaimSchedule(bg(), sc.ID, past1, &nextAfter); ok {
				t.Error("disabled schedule claimed")
			}
		}
	}
	if ok, err := f.s.ClaimSchedule(bg(), due2.ID, past2, nil); err != nil || !ok {
		t.Fatalf("claim with nil next: %v %v", ok, err)
	}
	if got := must(f.s.GetSchedule(bg(), due2.ID)); got.NextRunAt != nil {
		t.Error("next should be cleared")
	}
	if ok, _ := f.s.ClaimSchedule(bg(), missingID, past1, nil); ok {
		t.Error("missing schedule claimed")
	}
	if got := must(f.s.DueSchedules(bg(), now, 10)); len(got) != 0 {
		t.Errorf("still due: %+v", got)
	}
}

func TestRecordScheduleOutcome(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	f.admin()
	wo := f.workOrder("wo", crew)
	sc := must(f.s.SaveSchedule(bg(), "", schedIn("s", crew.ID), nil))

	mustOK(t, f.s.RecordScheduleOutcome(bg(), sc.ID, &wo.ID, nil, false))
	got := must(f.s.GetSchedule(bg(), sc.ID))
	if got.LastWorkOrderID == nil || *got.LastWorkOrderID != wo.ID || got.LastError != nil || !got.Enabled {
		t.Errorf("success = %+v", got)
	}
	// a transient failure records the error, keeps the last work order and stays enabled
	mustOK(t, f.s.RecordScheduleOutcome(bg(), sc.ID, nil, ptr("temporary"), false))
	got = must(f.s.GetSchedule(bg(), sc.ID))
	if *got.LastWorkOrderID != wo.ID || *got.LastError != "temporary" || !got.Enabled {
		t.Errorf("transient = %+v", got)
	}
	// a permanent failure pauses
	mustOK(t, f.s.RecordScheduleOutcome(bg(), sc.ID, nil, ptr("run-as gone"), true))
	if got := must(f.s.GetSchedule(bg(), sc.ID)); got.Enabled || *got.LastError != "run-as gone" {
		t.Errorf("permanent = %+v", got)
	}
	// success after pause must not re-enable
	mustOK(t, f.s.RecordScheduleOutcome(bg(), sc.ID, &wo.ID, nil, false))
	if got := must(f.s.GetSchedule(bg(), sc.ID)); got.Enabled || got.LastError != nil {
		t.Errorf("re-enabled by outcome: %+v", got)
	}
	// deleting the work order clears the pointer
	mustOK(t, f.s.DeleteWorkOrder(bg(), wo.ID))
	if got := must(f.s.GetSchedule(bg(), sc.ID)); got.LastWorkOrderID != nil {
		t.Error("dangling work order id")
	}
	// unknown schedule is not an error
	mustOK(t, f.s.RecordScheduleOutcome(bg(), missingID, nil, nil, true))
}

func TestTriggerLifecycleAndTokens(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("c", a)
	uid := f.admin()
	ctx := WithActor(bg(), Actor{ID: uid, Name: "Admin"})

	in := model.Trigger{Name: "hook", CrewID: &crew.ID, Enabled: true, InputMap: map[string]string{"company": "customer.name"},
		Brief: "b", IncludePayload: true, BudgetUSD: ptr(3.0), CallbackURL: ptr("https://example.com/cb")}
	tr, err := f.s.SaveTrigger(ctx, "", in)
	mustOK(t, err)
	if tr.Token == "" || !strings.HasPrefix(tr.Token, "cst_"+tr.TokenPrefix+"_") || len(tr.SigningSecret) < 40 || tr.RunAs == nil || *tr.RunAs != uid ||
		tr.InputMap["company"] != "customer.name" || *tr.CallbackURL != "https://example.com/cb" || len(tr.AreaIDs) != 1 || tr.TargetName != "c" || tr.FireCount != 0 {
		t.Errorf("trigger = %+v", tr)
	}
	_, err = f.s.SaveTrigger(ctx, "", in)
	wantConflict(t, err, "already exists")
	_, err = f.s.SaveTrigger(ctx, "", model.Trigger{Name: "ghost", CrewID: ptr(missingID), InputMap: map[string]string{}})
	wantConflict(t, err, "does not exist")

	// the stored copy never includes the plain token
	if got := must(f.s.GetTrigger(bg(), tr.ID)); got.Token != "" || got.SigningSecret != tr.SigningSecret {
		t.Errorf("get = %+v", got)
	}

	// TriggerByToken
	got, err := f.s.TriggerByToken(bg(), tr.ID, tr.Token)
	mustOK(t, err)
	if got.ID != tr.ID {
		t.Error("wrong trigger")
	}
	other := must(f.s.SaveTrigger(ctx, "", model.Trigger{Name: "other", CrewID: &crew.ID, InputMap: map[string]string{}}))
	for name, c := range map[string]struct{ id, token string }{
		"another trigger's token": {tr.ID, other.Token},
		"unknown id":              {missingID, tr.Token},
		"malformed":               {tr.ID, "nonsense"},
		"empty":                   {tr.ID, ""},
		"tampered":                {tr.ID, tr.Token[:len(tr.Token)-1] + flip(tr.Token[len(tr.Token)-1])},
		"api key shape":           {tr.ID, "csk_" + tr.Token[4:]},
	} {
		if _, err := f.s.TriggerByToken(bg(), c.id, c.token); !IsNotFound(err) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// update keeps the token valid; rotation invalidates it
	upIn := in
	upIn.Brief = "changed"
	up, err := f.s.SaveTrigger(ctx, tr.ID, upIn)
	mustOK(t, err)
	if up.Token != "" || up.Brief != "changed" || up.TokenPrefix != tr.TokenPrefix {
		t.Errorf("update = %+v", up)
	}
	_, err = f.s.TriggerByToken(bg(), tr.ID, tr.Token)
	mustOK(t, err)
	_, err = f.s.SaveTrigger(ctx, missingID, in)
	wantNotFound(t, err)

	rot, err := f.s.RotateTriggerSecrets(ctx, tr.ID)
	mustOK(t, err)
	if rot.Token == "" || rot.Token == tr.Token || rot.SigningSecret == tr.SigningSecret || rot.TokenPrefix == tr.TokenPrefix {
		t.Errorf("rotation = %+v", rot)
	}
	_, err = f.s.TriggerByToken(bg(), tr.ID, tr.Token)
	wantNotFound(t, err)
	_, err = f.s.TriggerByToken(bg(), tr.ID, rot.Token)
	mustOK(t, err)
	if sec := must(f.s.TriggerSigningSecret(bg(), tr.ID)); sec != rot.SigningSecret {
		t.Error("signing secret not updated")
	}
	_, err = f.s.RotateTriggerSecrets(ctx, missingID)
	wantNotFound(t, err)
	_, err = f.s.TriggerSigningSecret(bg(), missingID)
	wantNotFound(t, err)

	// revisions never hold secrets or tokens
	revs, total, _ := f.s.ListRevisions(bg(), RevisionFilter{EntityType: "trigger", EntityID: tr.ID, Types: []string{"trigger"}, Scope: AllAreas, Limit: 20})
	if total != 3 {
		t.Errorf("revisions = %d", total)
	}
	for _, r := range revs {
		j := toJSON(r.Snapshot)
		for _, secret := range []string{tr.Token, rot.Token, tr.SigningSecret, rot.SigningSecret} {
			if strings.Contains(j, secret) {
				t.Errorf("secret in revision %s: %s", r.Action, j)
			}
		}
	}

	// firing counters
	mustOK(t, f.s.RecordTriggerFired(bg(), tr.ID))
	mustOK(t, f.s.RecordTriggerFired(bg(), tr.ID))
	fired := must(f.s.GetTrigger(bg(), tr.ID))
	if fired.FireCount != 2 || fired.LastFiredAt == nil {
		t.Errorf("fired = %+v", fired)
	}
	mustOK(t, f.s.RecordTriggerFired(bg(), missingID))

	list := must(f.s.ListTriggers(bg(), Scope{AreaIDs: []string{a}}))
	if len(list) != 2 { // "other" targets the same crew, so both are in scope
		t.Errorf("scoped list = %d triggers", len(list))
	}
	if got := must(f.s.ListTriggers(bg(), Scope{})); got == nil || len(got) != 0 {
		t.Errorf("empty scope = %#v", got)
	}
	mustOK(t, f.s.DeleteTrigger(bg(), tr.ID))
	_, err = f.s.TriggerByToken(bg(), tr.ID, rot.Token)
	wantNotFound(t, err)
	wantNotFound(t, f.s.DeleteTrigger(bg(), tr.ID))
	_ = auth.TriggerPrefix
}
