package store

import (
	"slices"
	"testing"
	"time"

	"crewstudio/internal/model"
)

func connIn(name, kind string, areas ...string) model.ConnectionInput {
	return model.ConnectionInput{Name: name, Kind: kind, AreaIDs: areas, Config: map[string]string{"base_url": "https://x.example"}}
}

func TestConnectionsCRUDAndAreas(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	uid := f.admin()
	ctx := WithActor(bg(), Actor{ID: uid, Name: "Admin"})

	c, err := f.s.CreateConnection(ctx, missingID, connIn("api", "http", a), &SealedSecret{Cipher: []byte{1, 2}, Keys: []string{"auth_value"}})
	mustOK(t, err)
	if c.ID != missingID || !c.Enabled || c.SecretUpdatedAt == nil || !slices.Equal(c.SecretKeys, []string{"auth_value"}) ||
		c.CreatedBy == nil || *c.CreatedBy != uid || c.Config["base_url"] != "https://x.example" || len(c.MissingSecrets) != 0 {
		t.Errorf("created = %+v", c)
	}
	_, err = f.s.CreateConnection(ctx, "99999999-0000-4000-8000-000000000001", connIn("api", "http", a), nil)
	wantConflict(t, err, `named "api" already exists`)
	jira, err := f.s.CreateConnection(ctx, "99999999-0000-4000-8000-000000000002", model.ConnectionInput{Name: "jira", Kind: "jira", AreaIDs: []string{b}}, nil)
	mustOK(t, err)
	if !slices.Equal(jira.MissingSecrets, []string{"api_token"}) || len(jira.SecretKeys) != 0 {
		t.Errorf("jira = %+v", jira)
	}

	seenByA := must(f.s.ListConnections(bg(), Scope{AreaIDs: []string{a}}))
	if len(seenByA) != 1 || seenByA[0].Name != "api" {
		t.Errorf("alpha sees %v", seenByA)
	}
	if all := must(f.s.ListConnections(bg(), AllAreas)); len(all) != 2 {
		t.Errorf("all = %d", len(all))
	}
	if none := must(f.s.ListConnections(bg(), Scope{})); len(none) != 0 {
		t.Errorf("no areas sees %d", len(none))
	}

	off := false
	in := connIn("api-renamed", "http", a, b)
	in.Enabled = &off
	up, err := f.s.UpdateConnection(bg(), c.ID, in, false)
	mustOK(t, err)
	if up.Name != "api-renamed" || up.Enabled || len(up.AreaIDs) != 2 || !slices.Equal(up.SecretKeys, []string{"auth_value"}) {
		t.Errorf("updated = %+v", up)
	}
	_, err = f.s.UpdateConnection(bg(), "99999999-0000-4000-8000-0000000000ff", in, false)
	wantNotFound(t, err)
	cleared, err := f.s.UpdateConnection(bg(), c.ID, in, true)
	mustOK(t, err)
	if len(cleared.SecretKeys) != 0 || cleared.SecretUpdatedAt != nil {
		t.Errorf("cleared = %+v", cleared)
	}
	if cipher := must(f.s.ConnectionCipher(bg(), c.ID)); cipher != nil {
		t.Errorf("cipher kept: %v", cipher)
	}
	_, err = f.s.ReplaceConnectionSecret(bg(), c.ID, SealedSecret{Cipher: []byte{1, 2}, Keys: []string{"auth_value"}})
	mustOK(t, err)
	_, err = f.s.UpdateConnection(bg(), "99999999-0000-4000-8000-0000000000ff", in, false)
	wantNotFound(t, err)

	replaced, err := f.s.ReplaceConnectionSecret(bg(), jira.ID, SealedSecret{Cipher: []byte{9}, Keys: []string{"api_token"}})
	mustOK(t, err)
	if len(replaced.MissingSecrets) != 0 || replaced.SecretUpdatedAt == nil {
		t.Errorf("replaced = %+v", replaced)
	}
	if cipher := must(f.s.ConnectionCipher(bg(), jira.ID)); !slices.Equal(cipher, []byte{9}) {
		t.Errorf("cipher = %v", cipher)
	}
	_, err = f.s.ReplaceConnectionSecret(bg(), "99999999-0000-4000-8000-0000000000ff", SealedSecret{})
	wantNotFound(t, err)
	_, err = f.s.ConnectionCipher(bg(), "99999999-0000-4000-8000-0000000000ff")
	wantNotFound(t, err)

	id, cipher, found, err := f.s.AnySealedConnection(bg())
	if err != nil || !found || id != c.ID || !slices.Equal(cipher, []byte{1, 2}) {
		t.Errorf("any sealed = %s %v %v %v", id, cipher, found, err)
	}
	mustOK(t, f.s.DeleteConnection(bg(), c.ID))
	wantNotFound(t, f.s.DeleteConnection(bg(), c.ID))
	_, err = f.s.GetConnection(bg(), c.ID)
	wantNotFound(t, err)
	mustOK(t, f.s.DeleteConnection(bg(), jira.ID))
	if _, _, found, err := f.s.AnySealedConnection(bg()); found || err != nil {
		t.Errorf("none left: found=%v err=%v", found, err)
	}
	_, err = f.s.CreateConnection(ctx, c.ID, connIn("plain", "http", a), nil)
	mustOK(t, err)
	if id, _, found, _ := f.s.AnySealedConnection(bg()); !found || id != "" {
		t.Errorf("unsealed only: id=%q found=%v", id, found)
	}
}

func TestCrewConnectionsKeepLinksOutsideYourAreas(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	crew := f.simpleCrew("c", a, b)
	ca := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000a", connIn("ca", "http", a), nil))
	cb := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000b", connIn("cb", "http", b), nil))

	mustOK(t, f.s.SetCrewConnections(bg(), crew.ID, []string{ca.ID, cb.ID}, AllAreas))
	if got := must(f.s.CrewConnections(bg(), crew.ID, AllAreas)); len(got) != 2 || got[0].CrewCount != 1 {
		t.Fatalf("linked = %+v", got)
	}
	// Someone in Alpha only clears the list: Beta's link stays
	mustOK(t, f.s.SetCrewConnections(bg(), crew.ID, nil, Scope{AreaIDs: []string{a}}))
	got := must(f.s.CrewConnections(bg(), crew.ID, AllAreas))
	if len(got) != 1 || got[0].ID != cb.ID {
		t.Errorf("after alpha cleared = %+v", got)
	}
	if seen := must(f.s.CrewConnections(bg(), crew.ID, Scope{AreaIDs: []string{a}})); len(seen) != 0 {
		t.Errorf("alpha sees %+v", seen)
	}
	if err := f.s.SetCrewConnections(bg(), crew.ID, []string{missingID}, AllAreas); err == nil {
		t.Error("linking a missing connection should fail")
	}
}

func TestRunTokensScopeAndExpiry(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	crew := f.simpleCrew("c", a)
	sc := must(f.s.SaveSchedule(bg(), "", schedIn("nightly", crew.ID), nil))
	wo := must(f.s.CreateWorkOrder(bg(), model.WorkOrderInput{Title: "t", CrewID: &crew.ID},
		WorkOrderMeta{TargetName: crew.Name, AreaIDs: crew.AreaIDs, Source: model.SourceSchedule, SourceID: sc.ID}))
	run := f.run(crew, RunMeta{AreaIDs: crew.AreaIDs, WorkOrderID: wo.ID})
	inArea := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000a", connIn("in-area", "http", a), nil))
	otherArea := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000b", connIn("other-area", "http", b), nil))
	off := false
	disabledIn := connIn("disabled", "http", a)
	disabledIn.Enabled = &off
	disabled := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000c", disabledIn, nil))
	_ = must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000d", connIn("not-linked", "http", a), nil))
	mustOK(t, f.s.SetCrewConnections(bg(), crew.ID, []string{inArea.ID, otherArea.ID, disabled.ID}, AllAreas))

	token := must(f.s.CreateRunToken(bg(), run.ID, time.Now().Add(time.Hour)))
	if len(token) < 40 || token[:4] != RunTokenPrefix {
		t.Fatalf("token = %q", token)
	}
	scope := must(f.s.RunTokenScope(bg(), token))
	if scope.RunID != run.ID || scope.CrewID != crew.ID || scope.ScheduleID == nil || *scope.ScheduleID != sc.ID ||
		scope.ScheduleName != "nightly" || !slices.Equal(scope.AreaIDs, []string{a}) {
		t.Errorf("scope = %+v", scope)
	}
	if len(scope.Connections) != 1 || scope.Connections[0].ID != inArea.ID {
		t.Errorf("connections = %+v (only the enabled, linked one sharing an area)", scope.Connections)
	}
	_, err := f.s.RunTokenScope(bg(), token+"x")
	wantNotFound(t, err)

	// A new token replaces the old one
	token2 := must(f.s.CreateRunToken(bg(), run.ID, time.Now().Add(time.Hour)))
	_, err = f.s.RunTokenScope(bg(), token)
	wantNotFound(t, err)
	mustOK(t, f.s.ExpireRunToken(bg(), run.ID, time.Now().Add(-time.Second)))
	_, err = f.s.RunTokenScope(bg(), token2)
	wantNotFound(t, err)
	// Expiring never extends
	mustOK(t, f.s.ExpireRunToken(bg(), run.ID, time.Now().Add(time.Hour)))
	_, err = f.s.RunTokenScope(bg(), token2)
	wantNotFound(t, err)

	// A manual run has no schedule; a deleted crew leaves the crew id empty
	manual := f.run(crew, RunMeta{AreaIDs: crew.AreaIDs})
	mt := must(f.s.CreateRunToken(bg(), manual.ID, time.Now().Add(time.Hour)))
	if ms := must(f.s.RunTokenScope(bg(), mt)); ms.ScheduleID != nil || ms.CrewID != crew.ID {
		t.Errorf("manual scope = %+v", ms)
	}
	f.exec(`UPDATE runs SET crew_id = NULL WHERE id = $1`, manual.ID)
	if ms := must(f.s.RunTokenScope(bg(), mt)); ms.CrewID != "" || ms.CrewName != "c" || len(ms.Connections) != 0 {
		t.Errorf("orphan scope = %+v", ms)
	}
}

func TestNotesVersionsAndConflicts(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	sc := must(f.s.SaveSchedule(bg(), "", schedIn("atlas", crew.ID), nil))
	run := f.run(crew, RunMeta{})
	byRun := NoteWriter{RunID: &run.ID, Name: "run"}
	key := NoteKey{StateScope: StateScope{CrewID: crew.ID, ScheduleID: &sc.ID}, Subject: "project:atlas", Name: "weekly-report"}

	_, err := f.s.LatestNote(bg(), key)
	wantNotFound(t, err)
	zero := 0
	v1 := must(f.s.PutNote(bg(), key, "first", &zero, byRun))
	if v1.Version != 1 || v1.RunID == nil || v1.WrittenBy != "run" {
		t.Errorf("v1 = %+v", v1)
	}
	_, err = f.s.PutNote(bg(), key, "again", &zero, byRun)
	wantConflict(t, err, "is at version 1, not 0")
	v2 := must(f.s.PutNote(bg(), key, "second", nil, NoteWriter{Name: "Ada"}))
	one := 1
	_, err = f.s.PutNote(bg(), key, "stale", &one, byRun)
	wantConflict(t, err, "is at version 2, not 1")
	if latest := must(f.s.LatestNote(bg(), key)); latest.Version != 2 || latest.Content != "second" || latest.WrittenBy != "Ada" || latest.RunID != nil {
		t.Errorf("latest = %+v", latest)
	}
	if old := must(f.s.NoteVersion(bg(), key, 1)); old.Content != "first" {
		t.Errorf("v1 = %+v", old)
	}
	_, err = f.s.NoteVersion(bg(), key, 9)
	wantNotFound(t, err)
	if versions := must(f.s.NoteVersions(bg(), key)); len(versions) != 2 || versions[0].ID != v2.ID {
		t.Errorf("versions = %+v", versions)
	}

	// Crew-wide notes are a separate scope, and concurrent writers without a version all land
	crewWide := NoteKey{StateScope: StateScope{CrewID: crew.ID}, Name: "weekly-report"}
	results := parallel(6, func(int) error { _, err := f.s.PutNote(bg(), crewWide, "x", nil, byRun); return err })
	for _, err := range results {
		mustOK(t, err)
	}
	if latest := must(f.s.LatestNote(bg(), crewWide)); latest.Version != 6 {
		t.Errorf("crew-wide latest = %d", latest.Version)
	}

	listed := must(f.s.ListNotes(bg(), NoteFilter{Scope: StateScope{CrewID: crew.ID}, AllSchedules: true}))
	if len(listed) != 2 || listed[0].ScheduleID != nil || listed[1].ScheduleName != "atlas" || listed[1].Bytes != len("second") {
		t.Errorf("all notes = %+v", listed)
	}
	onlySched := must(f.s.ListNotes(bg(), NoteFilter{Scope: key.StateScope}))
	if len(onlySched) != 1 || onlySched[0].Version != 2 {
		t.Errorf("schedule notes = %+v", onlySched)
	}
	other := "person:sam"
	if bySubject := must(f.s.ListNotes(bg(), NoteFilter{Scope: key.StateScope, Subject: &other})); len(bySubject) != 0 {
		t.Errorf("subject filter = %+v", bySubject)
	}

	mustOK(t, f.s.DeleteNote(bg(), key))
	wantNotFound(t, f.s.DeleteNote(bg(), key))
	if ok := must(f.s.ScheduleOfCrew(bg(), sc.ID, crew.ID)); !ok {
		t.Error("schedule runs the crew")
	}
	if ok := must(f.s.ScheduleOfCrew(bg(), sc.ID, missingID)); ok {
		t.Error("schedule does not run a missing crew")
	}
}

func TestActionLogIdempotencyCheckAndList(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	sc := must(f.s.SaveSchedule(bg(), "", schedIn("sweep", crew.ID), nil))
	run := f.run(crew, RunMeta{})
	sweep := StateScope{CrewID: crew.ID, ScheduleID: &sc.ID}
	rec := model.LogRecord{Subject: "jira:PRJ-15", Action: "followup:due_soon", Details: map[string]any{"to": "sam"}, IdempotencyKey: "k1"}

	first, dup, err := f.s.RecordAction(bg(), sweep, &run.ID, rec)
	mustOK(t, err)
	if dup || first.ScheduleName != "sweep" || first.Details["to"] != "sam" || first.RunID == nil || *first.IdempotencyKey != "k1" {
		t.Errorf("first = %+v dup=%v", first, dup)
	}
	again, dup, err := f.s.RecordAction(bg(), sweep, &run.ID, rec)
	mustOK(t, err)
	if !dup || again.ID != first.ID {
		t.Errorf("repeat = %+v dup=%v", again, dup)
	}
	rec.IdempotencyKey = ""
	_, dup, err = f.s.RecordAction(bg(), sweep, nil, rec)
	if err != nil || dup {
		t.Errorf("no key: dup=%v err=%v", dup, err)
	}
	_, _, err = f.s.RecordAction(bg(), StateScope{CrewID: crew.ID}, nil, model.LogRecord{Subject: "jira:PRJ-15", Action: "followup:due_soon"})
	mustOK(t, err)
	_, _, err = f.s.RecordAction(bg(), StateScope{CrewID: missingID}, nil, rec)
	if err == nil {
		t.Error("missing crew accepted")
	}

	latest, err := f.s.CheckAction(bg(), sweep, "jira:PRJ-15", "followup:due_soon", nil)
	if err != nil || latest == nil || latest.ID == first.ID {
		t.Errorf("latest = %+v, %v (want the newer entry)", latest, err)
	}
	future := time.Now().Add(time.Hour)
	if none, err := f.s.CheckAction(bg(), sweep, "jira:PRJ-15", "followup:due_soon", &future); none != nil || err != nil {
		t.Errorf("since the future = %+v %v", none, err)
	}
	if none, _ := f.s.CheckAction(bg(), sweep, "jira:PRJ-99", "followup:due_soon", nil); none != nil {
		t.Errorf("other subject = %+v", none)
	}

	page, total, err := f.s.ListActions(bg(), ActionFilter{Scope: StateScope{CrewID: crew.ID}, AllSchedules: true, Limit: 10})
	if err != nil || total != 3 || len(page) != 3 {
		t.Errorf("all = %d/%d %v", len(page), total, err)
	}
	_, total, _ = f.s.ListActions(bg(), ActionFilter{Scope: StateScope{CrewID: crew.ID}, Limit: 10})
	if total != 1 {
		t.Errorf("crew-wide only = %d", total)
	}
	_, total, _ = f.s.ListActions(bg(), ActionFilter{Scope: sweep, Subject: "prj-15", Action: "DUE", Limit: 1})
	if total != 2 {
		t.Errorf("filtered = %d", total)
	}
	if got := must(f.s.GetAction(bg(), crew.ID, first.ID)); got.ID != first.ID {
		t.Errorf("get = %+v", got)
	}
	mustOK(t, f.s.DeleteAction(bg(), crew.ID, first.ID))
	wantNotFound(t, f.s.DeleteAction(bg(), crew.ID, first.ID))
	_, err = f.s.GetAction(bg(), crew.ID, first.ID)
	wantNotFound(t, err)
}

func TestAppendEventNextNumbersAfterTheLastEvent(t *testing.T) {
	f := newFixture(t)
	run := f.run(f.simpleCrew("c"), RunMeta{})
	mustOK(t, f.s.AppendEvent(bg(), run.ID, model.RunEvent{Seq: 7, Type: "run_status", CreatedAt: time.Now(), Payload: map[string]any{}}))
	seqs := parallel(5, func(int) int {
		seq, err := f.s.AppendEventNext(bg(), run.ID, model.RunEvent{Type: "state", CreatedAt: time.Now(), Payload: map[string]any{"message": "x"}})
		if err != nil {
			t.Error(err)
		}
		return seq
	})
	slices.Sort(seqs)
	if !slices.Equal(seqs, []int{8, 9, 10, 11, 12}) {
		t.Errorf("seqs = %v", seqs)
	}
	if _, err := f.s.AppendEventNext(bg(), missingID, model.RunEvent{Type: "state", CreatedAt: time.Now()}); err == nil {
		t.Error("a missing run should fail")
	}
}

func TestNotesKeepOnlyTheNewestVersions(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	key := NoteKey{StateScope: StateScope{CrewID: crew.ID}, Name: "log"}
	for i := 0; i < maxNoteVersions+2; i++ {
		_, err := f.s.PutNote(bg(), key, "x", nil, NoteWriter{RunID: &run.ID, Name: "run"})
		mustOK(t, err)
	}
	versions := must(f.s.NoteVersions(bg(), key))
	if len(versions) != maxNoteVersions || versions[len(versions)-1].Version != 3 {
		t.Errorf("kept %d, oldest v%d", len(versions), versions[len(versions)-1].Version)
	}
	_, _, err := f.s.RecordAction(bg(), key.StateScope, &run.ID, model.LogRecord{Subject: "s", Action: "a", Details: map[string]any{}})
	mustOK(t, err)
	if n := must(f.s.RunStateWrites(bg(), run.ID)); n != maxNoteVersions+1 {
		t.Errorf("writes = %d", n)
	}
}
