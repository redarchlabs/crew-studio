package store

import (
	"testing"

	"crewstudio/internal/model"
)

func TestLLMVisionAndAPIDefaults(t *testing.T) {
	s := newUnitStore(t)
	l, err := s.CreateLLM(bg(), model.LLM{Name: "Eyes", Model: "openai/gpt-x", Vision: true})
	mustOK(t, err)
	if !l.Vision || l.API != model.APICompletions {
		t.Errorf("created = %+v", l)
	}
	up, err := s.UpdateLLM(bg(), l.ID, model.LLM{Name: "Eyes", Model: "openai/gpt-x", API: model.APIResponses})
	mustOK(t, err)
	if up.Vision || up.API != model.APIResponses {
		t.Errorf("updated = %+v", up)
	}
	got := must(s.GetLLM(bg(), l.ID))
	if got.Vision || got.API != model.APIResponses {
		t.Errorf("get = %+v", got)
	}
	_, err = s.UpdateLLM(bg(), missingID, model.LLM{Name: "x", Model: "m"})
	wantNotFound(t, err)
	_, err = s.GetLLM(bg(), missingID)
	wantNotFound(t, err)
	wantNotFound(t, s.DeleteLLM(bg(), missingID))
	other := must(s.CreateLLM(bg(), model.LLM{Name: "Zed", Model: "m"}))
	_, err = s.UpdateLLM(bg(), other.ID, model.LLM{Name: "Eyes", Model: "m"})
	wantConflict(t, err, "Eyes")
	found := 0
	for _, x := range must(s.ListLLMs(bg())) {
		if x.Name == "Eyes" || x.Name == "Zed" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("created LLMs missing from list")
	}
}

func TestDeleteLLMBlockedByManagerCrew(t *testing.T) {
	f := newFixture(t)
	llm := f.llm("mgr")
	_, err := f.s.CreateCrew(bg(), model.Crew{Name: "H", Process: model.ProcessHierarchical, ManagerLLMID: &llm.ID, BudgetUSD: 1})
	mustOK(t, err)
	wantConflict(t, f.s.DeleteLLM(bg(), llm.ID), `crew "H" (manager)`)
}

func TestPricesAndSettingsErrorPaths(t *testing.T) {
	s := newUnitStore(t)
	_, err := s.GetPrice(bg(), missingID)
	wantNotFound(t, err)
	_, err = s.UpdatePrice(bg(), missingID, model.ModelPrice{Model: "x"})
	wantNotFound(t, err)
	wantNotFound(t, s.DeletePrice(bg(), missingID))
	p := must(s.CreatePrice(bg(), model.ModelPrice{Model: "m1", InputPerMTok: 1, CachedInputPerMTok: 0.5, OutputPerMTok: 2, Notes: "n"}))
	got := must(s.GetPrice(bg(), p.ID))
	if got.CachedInputPerMTok != 0.5 || got.Notes != "n" {
		t.Errorf("price = %+v", got)
	}
	p2 := must(s.CreatePrice(bg(), model.ModelPrice{Model: "m2"}))
	_, err = s.UpdatePrice(bg(), p2.ID, model.ModelPrice{Model: "m1"})
	wantConflict(t, err, "m1")

	// settings violating table checks are refused without a partial write
	st := must(s.GetSettings(bg()))
	bad := st
	bad.GroundingFreePeriod = "fortnight"
	if _, err := s.UpdateSettings(bg(), bad); err == nil {
		t.Error("invalid period accepted")
	}
	if after := must(s.GetSettings(bg())); after != st {
		t.Errorf("settings changed by failed update: %+v", after)
	}
	revs, total, _ := s.ListRevisions(bg(), RevisionFilter{EntityType: "settings", Types: []string{"settings"}, Scope: AllAreas, Limit: 5})
	if total != 0 || len(revs) != 0 {
		t.Errorf("failed update recorded a revision: %d", total)
	}
}

func TestMapErrAndHelpers(t *testing.T) {
	if mapErr(nil, "x", "y") != nil {
		t.Error("nil must stay nil")
	}
	if got := orEmpty[int](nil); got == nil || len(got) != 0 {
		t.Error("orEmpty")
	}
	if got := orEmpty([]int{1}); len(got) != 1 {
		t.Error("orEmpty keeps items")
	}
	if nullableUUID("") != nil || *nullableUUID("a") != "a" {
		t.Error("nullableUUID")
	}
	if nullableText("") != nil || *nullableText("a") != "a" {
		t.Error("nullableText")
	}
	if escapeLike(`a%b_c\d`) != `a\%b\_c\\d` {
		t.Errorf("escapeLike = %q", escapeLike(`a%b_c\d`))
	}
	e := inUseError("agent", []string{"x", "y"})
	wantConflict(t, e, "x, y")
	if ErrNotFound.Error() == "" || (&ConflictError{Message: "m"}).Error() != "m" {
		t.Error("error text")
	}
	if actorFrom(bg()) != (Actor{}) {
		t.Error("empty context has no actor")
	}
	if got := actorFrom(WithActor(bg(), Actor{ID: "1", Name: "n"})); got.Name != "n" {
		t.Error("actor lost")
	}
}

func TestRevisionRecordingHandlesUnencodableSnapshot(t *testing.T) {
	s := newUnitStore(t)
	err := recordRevision(bg(), s.pool, "llm", "x", "create", map[string]any{"c": make(chan int)})
	if err == nil {
		t.Error("unencodable snapshot accepted")
	}
}
