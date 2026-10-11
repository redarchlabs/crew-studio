package mcpserver

import (
	"strings"
	"testing"
)

func TestApplyChangesSetsOnlyNamedFields(t *testing.T) {
	agent := mustKind(t, "agent")
	current := map[string]any{"id": "a1", "name": "Scout", "goal": "old", "tools": []any{"google_search"}, "llm_id": "l1"}
	merged, err := agent.applyChanges(current, map[string]any{"goal": "new", "llm_id": nil})
	if err != nil {
		t.Fatal(err)
	}
	if merged["goal"] != "new" || merged["llm_id"] != nil || merged["name"] != "Scout" {
		t.Errorf("merged = %v", merged)
	}
	if _, kept := merged["llm_id"]; !kept {
		t.Error("null must clear a field, not drop it (a dropped field would be reset by the full replace)")
	}
	if current["goal"] != "old" {
		t.Error("applyChanges must not modify the current item")
	}
}

func TestApplyChangesRefusesUnknownAndServerSetFields(t *testing.T) {
	agent := mustKind(t, "agent")
	_, err := agent.applyChanges(map[string]any{}, map[string]any{"gaol": "x", "id": "b", "updated_at": "now"})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{`"gaol" is not a field of agent`, `"id" is set by Crew Studio`, `"updated_at" is set by Crew Studio`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	schedule := mustKind(t, "schedule")
	if _, err := schedule.applyChanges(map[string]any{}, map[string]any{"next_run_at": "x"}); err == nil {
		t.Error("next_run_at is computed by the scheduler and must be refused")
	}
	if _, err := agent.applyChanges(map[string]any{}, map[string]any{}); err == nil {
		t.Error("an empty change set must be refused")
	}
}

func TestDiffListsChangedFieldsInOrder(t *testing.T) {
	before := map[string]any{"name": "a", "goal": "g", "tools": []any{"x"}, "max_iter": 5.0, "updated_at": "t1"}
	after := map[string]any{"name": "a", "goal": "g2", "tools": []any{"x", "y"}, "max_iter": 5, "updated_at": "t2", "extra": true}
	got := diff(before, after)
	var fields []string
	for _, c := range got {
		fields = append(fields, c.Field)
	}
	if strings.Join(fields, ",") != "extra,goal,tools" {
		t.Errorf("changed fields = %v (updated_at is noise, and 5.0 == 5)", fields)
	}
	if got[1].Before != "g" || got[1].After != "g2" {
		t.Errorf("goal change = %+v", got[1])
	}
}

func TestSameInstant(t *testing.T) {
	if !sameInstant("2026-10-03T10:00:00Z", "2026-10-03T12:00:00+02:00") {
		t.Error("equal instants in different zones should match")
	}
	if sameInstant("2026-10-03T10:00:00Z", "2026-10-03T10:00:01Z") {
		t.Error("different instants matched")
	}
	if !sameInstant("not a time", "not a time") || sameInstant("a", "b") {
		t.Error("non-times compare as text")
	}
}

func TestApplyChangesRefusesNullWhereTheTypeHasNone(t *testing.T) {
	llm := mustKind(t, "llm")
	if _, err := llm.applyChanges(map[string]any{}, map[string]any{"temperature": nil, "reasoning_effort": nil}); err != nil {
		t.Errorf("nullable fields can be cleared: %v", err)
	}
	_, err := llm.applyChanges(map[string]any{}, map[string]any{"vision": nil})
	if err == nil || !strings.Contains(err.Error(), `"vision" cannot be null (it is boolean)`) {
		t.Errorf("null boolean = %v", err)
	}
	if _, err := mustKind(t, "agent").applyChanges(map[string]any{}, map[string]any{"tools": nil}); err == nil {
		t.Error("a list cannot be null; send [] instead")
	}
}
