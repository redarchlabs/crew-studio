package store

import (
	"errors"
	"testing"
	"time"

	"crewstudio/internal/model"
)

func memWrite(scope, content, hash string, vec []float32, runID *string) MemoryWrite {
	return MemoryWrite{Scope: scope, Content: content, ContentHash: hash, Categories: []string{"people"}, Metadata: map[string]any{"task": "Sweep"},
		Importance: 0.7, Status: model.MemoryActive, Source: "run:x", WrittenBy: "run", RunID: runID, EmbedModel: "m:2", Vector: vec}
}

func TestMemorySettingsDefaultsAndSave(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	got := must(f.s.MemorySettings(bg(), crew.ID))
	if got.Mode != model.MemoryOff || *got.RetentionDays != 365 || !got.ReviewRunSaves || got.UpdatedAt != nil {
		t.Errorf("defaults = %+v", got)
	}
	llm := f.llm("mem")
	in := model.DefaultMemorySettings(crew.ID)
	in.Mode, in.LLMID, in.RetentionDays, in.RecencyWeight = model.MemoryAutomatic, &llm.ID, nil, 0.3
	saved := must(f.s.PutMemorySettings(bg(), in, ""))
	if saved.Mode != model.MemoryAutomatic || saved.RetentionDays != nil || saved.RecencyWeight != 0.3 || saved.UpdatedAt == nil || *saved.LLMID != llm.ID {
		t.Errorf("saved = %+v", saved)
	}
	// The LLM cannot be deleted while memory uses it
	var conflict *ConflictError
	if err := f.s.DeleteLLM(bg(), llm.ID); !errors.As(err, &conflict) {
		t.Errorf("delete llm = %v", err)
	}
}

func TestSaveMemoriesDedupesAndCaps(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	other := f.run(crew, RunMeta{})
	caps := MemoryCaps{RunID: run.ID, PerRun: 3, PerCrew: 100}

	id := "11111111-2222-4333-8444-555555555555"
	w1 := memWrite("/people/sam", "Sam replies late", "h1", []float32{1, 0}, &run.ID)
	w1.ID = id
	res := must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{w1, memWrite("/", "Vendor page lies", "h2", []float32{0, 1}, &run.ID)}, caps))
	if len(res.Saved) != 2 || res.Saved[0].ID != id || res.Saved[0].Duplicate || res.Remaining != 1 || res.Skipped != 0 || len(res.Inserted) != 2 {
		t.Fatalf("first = %+v", res)
	}
	// The same text in the same scope, and a near-duplicate the caller found, are touched instead
	near := memWrite("/x", "Sam answers late", "h3", []float32{1, 0}, &run.ID)
	near.DuplicateOf = id
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{memWrite("/people/sam", "SAM replies late", "h1", []float32{1, 0}, &run.ID), near}, caps))
	if len(res.Saved) != 2 || !res.Saved[0].Duplicate || res.Saved[0].ID != id || !res.Saved[1].Duplicate || res.Remaining != 1 {
		t.Fatalf("duplicates = %+v", res)
	}
	// Past the run's cap new memories are skipped; a reused id gets a fresh one
	w3 := memWrite("/", "third", "h4", []float32{1, 1}, &run.ID)
	w3.ID = id
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{w3, memWrite("/", "fourth", "h5", []float32{1, 1}, &run.ID)}, caps))
	if len(res.Saved) != 2 || res.Saved[0].ID == id || res.Saved[1] != (MemorySaved{Skipped: SkipSaveCap}) || res.Skipped != 1 || res.Remaining != 0 {
		t.Fatalf("cap = %+v", res)
	}
	// Deleting rows does not give the run more saves
	n := must(f.s.DeleteMemories(bg(), MemoryFilter{CrewID: crew.ID, WrittenByRun: run.ID}, false))
	if n != 3 || must(f.s.RunMemorySaves(bg(), run.ID)) != 3 {
		t.Errorf("deleted %d, saves %d", n, must(f.s.RunMemorySaves(bg(), run.ID)))
	}
	// The crew's cap
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{memWrite("/", "a", "ha", []float32{1}, &other.ID),
		memWrite("/", "b", "hb", []float32{1}, &other.ID)}, MemoryCaps{RunID: other.ID, PerRun: 10, PerCrew: 1}))
	if len(res.Saved) != 2 || res.Saved[1].Skipped != SkipCrewFull || !res.CrewFull || res.Skipped != 1 {
		t.Errorf("crew cap = %+v", res)
	}
	// People have no run cap
	person := memWrite("/", "by hand", "hp", []float32{1}, nil)
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{person}, MemoryCaps{PerCrew: 100}))
	if len(res.Saved) != 1 || res.Remaining != 0 {
		t.Errorf("person = %+v", res)
	}
}

func TestMemoryQueriesAndUpdates(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	otherCrew := f.simpleCrew("d")
	run := f.run(crew, RunMeta{})
	other := f.run(crew, RunMeta{})
	caps := MemoryCaps{PerCrew: 100}
	pending := memWrite("/people/sam", "pending lesson", "p1", []float32{1, 0}, &other.ID)
	pending.Status = model.MemoryPending
	pin := memWrite("/people", "pinned 100% sure_x", "p2", []float32{0, 1}, &run.ID)
	pin.Pinned = true
	oldModel := memWrite("/peoplex", "old model", "p3", []float32{1, 1}, nil)
	oldModel.EmbedModel = "old:2"
	oldModel.Categories = []string{"vendors", "people"}
	res := must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{pending, pin, oldModel, memWrite("/", "mine", "p4", []float32{1, 0}, &run.ID)}, caps))
	must(f.s.SaveMemories(bg(), otherCrew.ID, []MemoryWrite{memWrite("/", "elsewhere", "p5", []float32{1, 0}, nil)}, caps))
	pendingID, pinID := res.Saved[0].ID, res.Saved[1].ID

	// A run sees active memories and its own pending ones; another run's pending ones are hidden
	visible := must(f.s.MemoryRows(bg(), MemoryFilter{CrewID: crew.ID, VisibleToRun: run.ID, EmbedModel: "m:2"}))
	if len(visible) != 2 || len(visible[0].Vector) != 2 {
		t.Errorf("visible to run = %d", len(visible))
	}
	if n := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID, VisibleToRun: other.ID})); n != 4 {
		t.Errorf("visible to writer = %d", n)
	}
	// Scope prefixes match whole parts; LIKE wildcards in content searches are literal
	if n := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID, ScopePrefix: "/people"})); n != 2 {
		t.Errorf("under /people = %d", n)
	}
	if n := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID, Contains: "100% SURE_"})); n != 1 {
		t.Errorf("contains = %d", n)
	}
	if n := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID, Contains: "1_0"})); n != 0 {
		t.Errorf("wildcards = %d", n)
	}
	yes := true
	for name, flt := range map[string]MemoryFilter{
		"categories": {Categories: []string{"vendors"}}, "metadata": {Metadata: map[string]any{"task": "Sweep"}},
		"status": {Status: model.MemoryPending}, "pinned": {Pinned: &yes}, "run": {RunID: other.ID}, "ids": {IDs: []string{pinID}},
	} {
		flt.CrewID = crew.ID
		want := 1
		if name == "metadata" {
			want = 4
		}
		if n := must(f.s.CountMemories(bg(), flt)); n != want {
			t.Errorf("%s = %d, want %d", name, n, want)
		}
	}
	page, total := must2(f.s.ListMemories(bg(), MemoryFilter{CrewID: crew.ID}, 2, 1))
	if total != 4 || len(page) != 2 || page[0].Vector != nil {
		t.Errorf("page = %d of %d", len(page), total)
	}
	scopes := must(f.s.MemoryScopes(bg(), MemoryFilter{CrewID: crew.ID}))
	if len(scopes) != 4 || scopes[0].Scope != "/" || scopes[0].Count != 1 {
		t.Errorf("scopes = %+v", scopes)
	}
	cats := must(f.s.MemoryCategories(bg(), MemoryFilter{CrewID: crew.ID}))
	if cats["people"] != 4 || cats["vendors"] != 1 {
		t.Errorf("categories = %v", cats)
	}
	oldest, newest, err := f.s.MemoryAge(bg(), MemoryFilter{CrewID: crew.ID})
	if err != nil || oldest == nil || newest == nil || newest.Before(*oldest) {
		t.Errorf("age = %v %v %v", oldest, newest, err)
	}

	// Runs may change only what they wrote
	content := "changed"
	if _, err := f.s.UpdateMemory(bg(), crew.ID, pendingID, MemoryUpdate{Content: &content, OnlyRunID: run.ID}); !errors.Is(err, ErrMemoryNotOwned) {
		t.Errorf("not owned = %v", err)
	}
	cats2 := []string{"x"}
	imp := 0.1
	upd := must(f.s.UpdateMemory(bg(), crew.ID, pendingID, MemoryUpdate{Content: &content, ContentHash: "new", Vector: []float32{0, 1},
		EmbedModel: "m:2", Categories: &cats2, Metadata: map[string]any{"a": "b"}, Importance: &imp, OnlyRunID: other.ID}))
	if upd.Content != "changed" || upd.Categories[0] != "x" || upd.Metadata["a"] != "b" || upd.Importance != 0.1 || upd.Status != model.MemoryPending {
		t.Errorf("update = %+v", upd)
	}
	active, scope := model.MemoryActive, "/people"
	if _, err := f.s.UpdateMemory(bg(), crew.ID, pendingID, MemoryUpdate{Scope: &scope, Content: ptr("pinned 100% sure_x"),
		ContentHash: "p2", Vector: []float32{1}, EmbedModel: "m:2"}); err == nil || err.Error() != memoryExists.Message {
		t.Errorf("clash = %v", err)
	}
	if got := must(f.s.UpdateMemory(bg(), crew.ID, pendingID, MemoryUpdate{Status: &active})); got.Status != model.MemoryActive {
		t.Errorf("approve = %+v", got)
	}
	_, err = f.s.UpdateMemory(bg(), otherCrew.ID, pendingID, MemoryUpdate{Status: &active})
	wantNotFound(t, err)
	_, err = f.s.GetMemory(bg(), otherCrew.ID, pendingID)
	wantNotFound(t, err)

	if n := must(f.s.TouchMemories(bg(), MemoryFilter{CrewID: crew.ID, IDs: []string{pinID, pendingID}})); n != 2 {
		t.Errorf("touched %d", n)
	}
	reembed := must(f.s.MemoriesToReembed(bg(), "m:2", 10))
	if len(reembed) != 1 || reembed[0].Content != "old model" {
		t.Fatalf("reembed = %+v", reembed)
	}
	mustOK(t, f.s.SetMemoryVector(bg(), reembed[0].ID, "old model", "m:2", []float32{3, 4}))
	if len(must(f.s.MemoriesToReembed(bg(), "m:2", 10))) != 0 {
		t.Error("re-embedded row still listed")
	}
	mustOK(t, f.s.SetMemoriesKM2Document(bg(), crew.ID, []string{pinID}, "doc-1"))
	if got := must(f.s.GetMemory(bg(), crew.ID, pinID)); got.KM2DocumentID == nil || *got.KM2DocumentID != "doc-1" {
		t.Errorf("km2 = %+v", got.KM2DocumentID)
	}

	// Dry-run bulk delete counts; the real one deletes (pinned included); single delete returns the row
	if n := must(f.s.DeleteMemories(bg(), MemoryFilter{CrewID: crew.ID, ScopePrefix: "/people"}, true)); n != 2 {
		t.Errorf("dry run = %d", n)
	}
	if n := must(f.s.DeleteMemories(bg(), MemoryFilter{CrewID: crew.ID, ScopePrefix: "/people"}, false)); n != 2 {
		t.Errorf("deleted = %d", n)
	}
	gone := must(f.s.DeleteMemory(bg(), crew.ID, reembed[0].ID))
	if gone.Content != "old model" {
		t.Errorf("deleted = %+v", gone)
	}
	_, err = f.s.DeleteMemory(bg(), crew.ID, reembed[0].ID)
	wantNotFound(t, err)
}

func TestMemoryUsageAndPruning(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	kept := f.simpleCrew("k")
	run := f.run(crew, RunMeta{})
	if ok := must(f.s.AddMemoryEmbeds(bg(), run.ID, 0, 5)); !ok {
		t.Error("nothing to count")
	}
	if !must(f.s.AddMemoryEmbeds(bg(), run.ID, 3, 5)) || must(f.s.AddMemoryEmbeds(bg(), run.ID, 3, 5)) || !must(f.s.AddMemoryEmbeds(bg(), run.ID, 2, 5)) {
		t.Error("embed cap")
	}
	if must(f.s.AddMemoryEmbeds(bg(), run.ID, 6, 5)) {
		t.Error("over the cap at once")
	}

	caps := MemoryCaps{PerCrew: 100}
	old := memWrite("/", "old", "o1", []float32{1}, nil)
	oldPinned := memWrite("/", "old pinned", "o2", []float32{1}, nil)
	oldPinned.Pinned = true
	pending := memWrite("/", "pending", "o3", []float32{1}, nil)
	pending.Status = model.MemoryPending
	fresh := memWrite("/", "fresh", "o4", []float32{1}, nil)
	res := must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{old, oldPinned, pending, fresh}, caps))
	keep := must(f.s.SaveMemories(bg(), kept.ID, []MemoryWrite{memWrite("/", "kept forever", "o5", []float32{1}, nil)}, caps))
	set := model.DefaultMemorySettings(kept.ID)
	set.RetentionDays = nil
	must(f.s.PutMemorySettings(bg(), set, ""))
	age := func(id string, days int) {
		_, err := f.s.Pool().Exec(bg(), `UPDATE memories SET created_at = $2 WHERE id = $1`, id, time.Now().AddDate(0, 0, -days))
		mustOK(t, err)
	}
	age(res.Saved[0].ID, 400)
	age(res.Saved[1].ID, 400)
	age(res.Saved[2].ID, 31)
	age(keep.Saved[0].ID, 4000)
	if n := must(f.s.PruneMemories(bg())); n != 2 {
		t.Errorf("pruned %d", n)
	}
	left := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID}))
	if left != 2 || must(f.s.CountMemories(bg(), MemoryFilter{CrewID: kept.ID})) != 1 {
		t.Errorf("left %d", left)
	}
}

func TestPersonKM2Owner(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	scope := StateScope{CrewID: crew.ID}
	owner := PersonKM2Owner("33333333-0000-4000-8000-000000000001")
	rec := func(key string) model.LogRecord {
		r := km2Rec(key)
		r.Details = map[string]any{"connection_id": "conn-p", "promoted_by": "33333333-0000-4000-8000-000000000001"}
		return r
	}
	limits := KM2Limits{PerOwner: 1, PerConnectionDay: 100, ConnectionID: "conn-p"}
	first := must(f.s.ReserveKM2Save(bg(), scope, owner, rec("p1"), limits))
	if first.Entry == nil || first.Entry.RunID != nil {
		t.Fatalf("first = %+v", first)
	}
	if capped := must(f.s.ReserveKM2Save(bg(), scope, owner, rec("p2"), limits)); !capped.CapReached || capped.Saves != 1 {
		t.Errorf("capped = %+v", capped)
	}
}

func must2[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}
