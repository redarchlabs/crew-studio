package chat

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

func TestToolsAndOffers(t *testing.T) {
	keys := ToolKeys()
	if !slices.Equal(keys, []string{"notes_get", "notes_list", "log_check", "start_work_order", "propose_note_change",
		"tracking_list_get", "tracker_get_issue", "tracker_find_issues", "propose_tracking_add", "propose_tracking_remove", "km_search", "km_ask"}) {
		t.Errorf("keys = %v", keys)
	}
	if !Offers(keys, TypeStartWorkOrder) || !Offers(keys, TypeNotesPut) || !Offers(keys, TypeTrackingAdd) || !Offers(keys, TypeTrackingRemove) {
		t.Error("the chat tools should offer every proposal type")
	}
	if Offers([]string{"notes_get"}, TypeStartWorkOrder) || Offers(keys, "drop_everything") {
		t.Error("offers too much")
	}
	// A crew chat's tools never offer a hand-off; only an agent's lead gets Hand off work
	if Offers(keys, TypeStartHandoff) || !Offers([]string{StartHandoffTool}, TypeStartHandoff) {
		t.Error("hand-offs offered wrongly")
	}
	// A crew chat's tools never offer the counterparts' cards either; only agent chat gives their tools
	for _, typ := range []string{TypeTrackerComment, TypeTrackerSuggestion, TypeTrackerIssue, TypeKM2Save} {
		if Offers(keys, typ) {
			t.Errorf("crew chat offers %s", typ)
		}
	}
	if !Offers([]string{ProposeKM2SaveTool}, TypeKM2Save) || !Offers([]string{ProposeTrackerIssueTool}, TypeTrackerIssue) {
		t.Error("a counterpart does not offer its card")
	}
	if !slices.Equal(Types(), []string{TypeKM2Save, TypeNotesPut, TypeStartHandoff, TypeStartWorkOrder, TypeTrackerComment, TypeTrackerIssue,
		TypeTrackerSuggestion, TypeTrackingAdd, TypeTrackingRemove}) {
		t.Errorf("types = %v", Types())
	}
	defer func() {
		if recover() == nil {
			t.Error("registering a type twice should panic")
		}
	}()
	Register(Kind{Type: TypeNotesPut})
}

func TestToolsFollowThePersonsPermissions(t *testing.T) {
	who := func(perms ...string) *auth.Principal {
		m := map[string]bool{}
		for _, p := range perms {
			m[p] = true
		}
		return &auth.Principal{Permissions: m}
	}
	if got := ToolKeysFor(who()); len(got) != 0 {
		t.Errorf("no permissions = %v", got)
	}
	if got := ToolKeysFor(who(auth.StateView)); !slices.Equal(got, []string{"notes_get", "notes_list", "log_check"}) {
		t.Errorf("state.view = %v", got)
	}
	// Everything but the KM2 tools, which withKM2Tools adds (km2_test.go)
	if got := ToolKeysForCrew(who(auth.StateView, auth.StateManage, auth.WorkOrdersCreate), true); !slices.Equal(got, ToolKeys()[:len(ToolKeys())-2]) {
		t.Errorf("everything = %v", got)
	}
	// Without a tracker connection the tracker tools are left out
	if got := ToolKeysFor(who(auth.StateView, auth.StateManage)); slices.Contains(got, "tracker_get_issue") || slices.Contains(got, "propose_tracking_add") ||
		slices.Contains(got, "propose_tracking_remove") || slices.Contains(got, "tracking_list_get") {
		t.Errorf("no tracker = %v", got)
	}
}

func TestNormalizeStartWorkOrder(t *testing.T) {
	k, _ := Lookup(TypeStartWorkOrder)
	clean, summary, err := k.Normalize(map[string]any{"brief": "  Sweep\tAtlas now and write everything down for the team in one report please  ",
		"inputs": map[string]any{"company": " Acme "}})
	if err != nil {
		t.Fatal(err)
	}
	if clean["title"] != "Sweep Atlas now and write everything down for the team in on…" || summary != "Start a work order: "+clean["title"].(string) {
		t.Errorf("title = %q summary %q", clean["title"], summary)
	}
	if clean["inputs"].(map[string]any)["company"] != "Acme" {
		t.Errorf("inputs = %v", clean["inputs"])
	}
	cases := map[string]map[string]any{
		"empty brief":   {"brief": " "},
		"unknown field": {"brief": "x", "budget_usd": 100},
		"long brief":    {"brief": strings.Repeat("x", maxProposedBriefLen+1)},
		"long title":    {"brief": "x", "title": strings.Repeat("t", maxProposedTitleLen+1)},
		"bad key":       {"brief": "x", "inputs": map[string]any{"Bad Key": "v"}},
		"long value":    {"brief": "x", "inputs": map[string]any{"k": strings.Repeat("v", runspec.MaxInputValueLen+1)}},
		"wrong type":    {"brief": 5},
		"many inputs":   {"brief": "x", "inputs": manyInputs(maxProposedInputs + 1)},
		"unencodable":   {"brief": func() {}},
	}
	for name, payload := range cases {
		var verr *model.ValidationError
		if _, _, err := k.Normalize(payload); !errors.As(err, &verr) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func manyInputs(n int) map[string]any {
	out := map[string]any{}
	for i := range n {
		out["k"+strings.Repeat("a", i)] = "v"
	}
	return out
}

func TestNormalizeNoteChange(t *testing.T) {
	k, _ := Lookup(TypeNotesPut)
	clean, summary, err := k.Normalize(map[string]any{"name": " risks ", "content": "x", "base_version": 0})
	if err != nil || clean["name"] != "risks" || summary != "Create the note risks" {
		t.Errorf("clean = %v summary %q err %v", clean, summary, err)
	}
	_, summary, _ = k.Normalize(map[string]any{"name": "risks", "subject": "project:atlas", "content": "x", "base_version": 4})
	if summary != "Change the note risks (version 4) about project:atlas" {
		t.Errorf("summary = %q", summary)
	}
	for name, payload := range map[string]map[string]any{
		"no version":    {"name": "risks", "content": "x"},
		"bad name":      {"name": "no spaces", "content": "x", "base_version": 0},
		"negative":      {"name": "risks", "content": "x", "base_version": -1},
		"too big":       {"name": "risks", "content": strings.Repeat("x", model.MaxNoteBytes+1), "base_version": 0},
		"unknown field": {"name": "risks", "content": "x", "base_version": 0, "schedule_id": "s"},
	} {
		if _, _, err := k.Normalize(payload); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestHelpers(t *testing.T) {
	if cut("abcdef", 3) != "abc\n[… cut]" || cut("abc", 3) != "abc" {
		t.Error("cut")
	}
	if n, ok := intOf(map[string]any{"v": 2.0}, "v"); !ok || n != 2 {
		t.Error("float")
	}
	if _, ok := intOf(map[string]any{"v": 2.5}, "v"); ok {
		t.Error("fraction")
	}
	if n, ok := intOf(map[string]any{"v": 3}, "v"); !ok || n != 3 {
		t.Error("int")
	}
	if _, ok := intOf(map[string]any{"v": "3"}, "v"); ok {
		t.Error("string")
	}
	if defaultTitle("short brief") != "short brief" {
		t.Error("default title")
	}
	if _, err := asMap(func() {}); err == nil {
		t.Error("asMap should fail on a function")
	}
}

func TestCheckChatter(t *testing.T) {
	var forbidden *launch.ForbiddenError
	if err := checkChatter(nil); !errors.As(err, &forbidden) {
		t.Errorf("nil = %v", err)
	}
	if err := checkChatter(&auth.Principal{Kind: auth.KindHuman}); !errors.As(err, &forbidden) {
		t.Errorf("no permission = %v", err)
	}
	var denied *DeniedError
	svc := &auth.Principal{Kind: auth.KindService, Permissions: map[string]bool{auth.CrewsChat: true}}
	if err := checkChatter(svc); !errors.As(err, &denied) || denied.Error() == "" {
		t.Errorf("service = %v", err)
	}
	if err := checkChatter(&auth.Principal{Kind: auth.KindHuman, Permissions: map[string]bool{auth.CrewsChat: true}}); err != nil {
		t.Errorf("person = %v", err)
	}
}

func TestFrontDesk(t *testing.T) {
	llm := "l1"
	cheap := "l2"
	lib := runspec.Library{
		Agents: map[string]model.Agent{"a1": {ID: "a1", Name: "Desk", LLMID: &llm, Tools: []string{"google_search", "notes_put"}, MaxIter: 25, AllowDelegation: true},
			"a2": {ID: "a2", Name: "No model"}},
		LLMs:    map[string]model.LLM{"l1": {ID: "l1", Model: "openai/gpt-6-luna"}, "l2": {ID: "l2", Model: "unpriced"}},
		Pricing: []model.ModelPrice{{Model: "gpt-6-luna"}},
	}
	agent, got, err := frontDesk(lib, "a1", nil, []string{"notes_get"})
	if err != nil || got.ID != "l1" || !slices.Equal(agent.Tools, []string{"notes_get"}) || agent.AllowDelegation || agent.MaxIter != maxChatIter {
		t.Errorf("front desk = %+v %+v %v", agent, got, err)
	}
	for name, tc := range map[string]struct {
		agent string
		llm   *string
	}{"missing agent": {"zz", nil}, "no llm": {"a2", nil}, "unpriced": {"a1", &cheap}, "missing llm": {"a1", strPtr("gone")}} {
		var problems *launch.ProblemsError
		if _, _, err := frontDesk(lib, tc.agent, tc.llm, ToolKeys()); !errors.As(err, &problems) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func strPtr(s string) *string { return &s }

func TestProposeRefusesNonChatRuns(t *testing.T) {
	s := New(nil, nil, nil)
	var denied *DeniedError
	if _, err := s.Propose(context.Background(), store.RunScope{Kind: model.RunKindCrew}, TypeStartWorkOrder, nil); !errors.As(err, &denied) {
		t.Errorf("crew run = %v", err)
	}
	var verr *model.ValidationError
	if _, err := s.Propose(context.Background(), store.RunScope{Kind: model.RunKindChat}, "nope", nil); !errors.As(err, &verr) {
		t.Errorf("unknown type = %v", err)
	}
}
