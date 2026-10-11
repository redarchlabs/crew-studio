package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/model"
)

// Seeded agents: 001-005 are the PE crew's (Finance), 006 is the AI research analyst (IT).
const (
	chatInvestigator = "22222222-0000-4000-8000-000000000001"
	chatAnalyst      = "22222222-0000-4000-8000-000000000002"
	chatPartner      = "22222222-0000-4000-8000-000000000005"
	chatResearcher   = "22222222-0000-4000-8000-000000000006"
	chatPECrew       = "44444444-0000-4000-8000-000000000001"
	chatAICrew       = "44444444-0000-4000-8000-000000000002"
	chatModel        = "11111111-0000-4000-8000-000000000003"
)

// chatAgent turns chat on for the deal partner with two specialists (one in IT) and both seeded crews.
func (e testEnv) chatAgent(t *testing.T) {
	t.Helper()
	r := e.do(t, "PUT", "/api/agents/"+chatPartner+"/chat", map[string]any{
		"enabled": true, "llm_id": chatModel, "specialist_ids": []string{chatInvestigator, chatResearcher},
		"crew_ids": []string{chatPECrew, chatAICrew}, "daily_budget_usd": 1, "person_daily_budget_usd": 3,
	})
	wantStatus(t, r, http.StatusOK)
}

func TestAgentChatSettings(t *testing.T) {
	e := newEnv(t)
	path := "/api/agents/" + chatPartner + "/chat"
	got := decodeData[model.AgentChatSettings](t, e.do(t, "GET", path, nil))
	if got.Enabled || got.DailyBudgetUSD != model.DefaultChatDailyBudgetUSD || len(got.SpecialistIDs) != 0 || got.CrewIDs == nil {
		t.Errorf("default = %+v", got)
	}
	r := e.do(t, "PUT", path, map[string]any{"enabled": true, "daily_budget_usd": 1, "specialist_ids": []string{chatPartner}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	r = e.do(t, "PUT", path, map[string]any{"enabled": true, "daily_budget_usd": 1, "llm_id": missingID,
		"specialist_ids": []string{missingID}, "crew_ids": []string{missingID}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if len(r.body.Error.Details) != 3 {
		t.Errorf("details = %v", r.body.Error.Details)
	}

	// A specialist with the lead's role (or another specialist's) cannot be told apart by CrewAI
	twin := decodeData[model.Agent](t, e.do(t, "POST", "/api/agents", map[string]any{"name": "Twin", "role": " corporate  INVESTIGATOR",
		"goal": "g", "backstory": "b", "tools": []string{}, "max_iter": 5, "allow_delegation": false, "verbose": false, "area_ids": []string{financeArea}}))
	r = e.do(t, "PUT", path, map[string]any{"enabled": true, "daily_budget_usd": 1, "specialist_ids": []string{chatInvestigator, twin.ID}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "same role") {
		t.Errorf("details = %v", r.body.Error.Details)
	}

	e.chatAgent(t)
	got = decodeData[model.AgentChatSettings](t, e.do(t, "GET", path, nil))
	if !got.Enabled || got.PersonDailyBudgetUSD != 3 || got.UpdatedByName != "Ada Admin" || len(got.SpecialistIDs) != 2 ||
		got.SpecialistIDs[0] != chatInvestigator || len(got.CrewIDs) != 2 || got.CrewIDs[1] != chatAICrew {
		t.Errorf("saved = %+v", got)
	}
	// Its chat model, specialists and crews cannot be deleted while it uses them
	helper := decodeData[model.Agent](t, e.do(t, "POST", "/api/agents", map[string]any{"name": "Helper", "role": "Helper",
		"goal": "g", "backstory": "b", "tools": []string{}, "max_iter": 5, "allow_delegation": false, "verbose": false, "area_ids": []string{financeArea}}))
	wantStatus(t, e.do(t, "PUT", path, map[string]any{"enabled": true, "llm_id": chatModel, "daily_budget_usd": 1,
		"specialist_ids": []string{chatInvestigator, chatResearcher, helper.ID}, "crew_ids": []string{chatPECrew, chatAICrew}}), http.StatusOK)
	for path, used := range map[string]string{
		"/api/llms/" + chatModel:   `agent "Deal Partner" (chat)`,
		"/api/agents/" + helper.ID: `agent "Deal Partner" (chat specialist)`,
		"/api/crews/" + chatAICrew: `agent "Deal Partner" (chat)`,
	} {
		r = e.do(t, "DELETE", path, nil)
		wantStatus(t, r, http.StatusConflict)
		if !strings.Contains(r.body.Error.Message, used) {
			t.Errorf("%s: error = %+v", path, r.body.Error)
		}
	}

	// Viewers may read, not change; other areas see nothing
	_, viewer := e.person(t, "vic@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "GET", path, nil), http.StatusOK)
	wantStatus(t, e.as(viewer).do(t, "PUT", path, map[string]any{"daily_budget_usd": 1}), http.StatusForbidden)
	_, outsider := e.person(t, "ozzy@example.com", []string{builderRole}, []string{hrArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", path, nil), http.StatusNotFound)
	wantStatus(t, e.as(outsider).do(t, "GET", "/api/agents/nope/chat", nil), http.StatusNotFound)
	testAgentChatHiddenRefs(t, e, helper.ID)
}

// A Finance builder sees only the specialists and crews in Finance, with a count of the others. Saving keeps the IT
// specialist and crew where they were, and they still cannot add one from outside their areas.
func testAgentChatHiddenRefs(t *testing.T, e testEnv, helper string) {
	t.Helper()
	path := "/api/agents/" + chatPartner + "/chat"
	_, builder := e.person(t, "bea@example.com", []string{builderRole}, []string{financeArea}, nil)
	seen := decodeData[model.AgentChatSettings](t, e.as(builder).do(t, "GET", path, nil))
	if !slices.Equal(seen.SpecialistIDs, []string{chatInvestigator, helper}) || seen.HiddenSpecialists != 1 ||
		!slices.Equal(seen.CrewIDs, []string{chatPECrew}) || seen.HiddenCrews != 1 {
		t.Errorf("builder sees %+v", seen)
	}
	r := e.as(builder).do(t, "PUT", path, map[string]any{"enabled": true, "daily_budget_usd": 1,
		"specialist_ids": []string{helper}, "crew_ids": []string{chatPECrew}})
	wantStatus(t, r, http.StatusOK)
	if saved := decodeData[model.AgentChatSettings](t, r); !slices.Equal(saved.SpecialistIDs, []string{helper}) ||
		saved.HiddenSpecialists != 1 || !slices.Equal(saved.CrewIDs, []string{chatPECrew}) || saved.HiddenCrews != 1 {
		t.Errorf("builder's save answered %+v", saved)
	}
	all := decodeData[model.AgentChatSettings](t, e.do(t, "GET", path, nil))
	if !slices.Equal(all.SpecialistIDs, []string{helper, chatResearcher}) || !slices.Equal(all.CrewIDs, []string{chatPECrew, chatAICrew}) ||
		all.HiddenSpecialists != 0 || all.HiddenCrews != 0 {
		t.Errorf("after the builder's save = %+v", all)
	}
	scout := decodeData[model.Agent](t, e.do(t, "POST", "/api/agents", map[string]any{"name": "Scout", "role": "Scout",
		"goal": "g", "backstory": "b", "tools": []string{}, "max_iter": 5, "allow_delegation": false, "verbose": false, "area_ids": []string{itArea}}))
	r = e.as(builder).do(t, "PUT", path, map[string]any{"enabled": true, "daily_budget_usd": 1, "specialist_ids": []string{helper, scout.ID}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
}

func TestKeepHiddenRefs(t *testing.T) {
	visible := map[string]bool{"a": true, "b": true, "c": true}
	cases := []struct {
		name         string
		sent, before []string
		want         []string
	}{
		{"nothing saved", []string{"a"}, nil, []string{"a"}},
		{"nothing hidden", []string{"b", "a"}, []string{"a", "b"}, []string{"b", "a"}},
		{"hidden keeps its place", []string{"b", "c"}, []string{"a", "h", "b"}, []string{"b", "h", "c"}},
		{"hidden first", []string{"a"}, []string{"h", "a"}, []string{"h", "a"}},
		{"all visible removed", []string{}, []string{"a", "h", "b", "g"}, []string{"h", "g"}},
		{"more sent than saved", []string{"a", "b", "c"}, []string{"h"}, []string{"h", "a", "b", "c"}},
		{"hidden sent again is not doubled", []string{"h", "a"}, []string{"a", "h"}, []string{"h", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keepHiddenRefs(tc.sent, tc.before, visible); !slices.Equal(got, tc.want) {
				t.Errorf("keepHiddenRefs(%v, %v) = %v, want %v", tc.sent, tc.before, got, tc.want)
			}
		})
	}
}

func TestAgentChatTurnsAndProposals(t *testing.T) {
	e := newEnv(t)
	if agents := decodeData[[]model.ChatAgent](t, e.do(t, "GET", "/api/chat/agents", nil)); len(agents) != 0 {
		t.Errorf("chat agents before = %+v", agents)
	}
	wantStatus(t, e.do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "hi"}), http.StatusUnprocessableEntity)
	e.chatAgent(t)

	// A Finance operator sees the agent with only the specialist and crew in Finance
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	agents := decodeData[[]model.ChatAgent](t, e.as(ann).do(t, "GET", "/api/chat/agents", nil))
	if len(agents) != 1 || agents[0].ID != chatPartner || len(agents[0].Specialists) != 1 || agents[0].Specialists[0].ID != chatInvestigator ||
		agents[0].Specialists[0].Role != "Corporate Investigator" || len(agents[0].Crews) != 1 || agents[0].Crews[0].ID != chatPECrew {
		t.Fatalf("chat agents = %+v", agents)
	}
	wantStatus(t, e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "crew_id": chatPECrew, "content": "hi"}),
		http.StatusUnprocessableEntity)
	wantStatus(t, e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": "x", "content": "hi"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatResearcher, "content": "hi"}), http.StatusNotFound)

	r := e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "Is Acme worth a look?"})
	wantStatus(t, r, http.StatusCreated)
	view := decodeData[conversationView](t, r)
	if view.Conversation.AgentID == nil || view.Conversation.CrewID != nil || view.Conversation.AgentName != "Deal Partner" ||
		!view.ChatEnabled || view.Budget.DailyUSD != 1 || view.Budget.PersonDailyUSD != 3 || len(view.Messages) != 2 {
		t.Errorf("view = %+v", view)
	}
	list := decodeData[[]model.Conversation](t, e.as(ann).do(t, "GET", "/api/chat/conversations?agent_id="+chatPartner, nil))
	if len(list) != 1 || list[0].ID != view.Conversation.ID {
		t.Errorf("conversations = %+v", list)
	}
	wantStatus(t, e.as(ann).do(t, "GET", "/api/chat/conversations?agent_id=x", nil), http.StatusUnprocessableEntity)
	if list := decodeData[[]model.Conversation](t, e.as(ann).do(t, "GET", "/api/chat/conversations?crew_id="+chatPECrew, nil)); len(list) != 0 {
		t.Errorf("crew conversations = %+v", list)
	}

	// The turn's run has no crew: it may only propose, and only on its crews
	reply := view.Messages[1]
	sr := e.runningTurn(t, model.Crew{}, *reply.RunID)
	wantStatus(t, e.call(t, sr, "GET", "/scope", nil), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "GET", "/notes", nil), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order",
		"payload": map[string]any{"brief": "Diligence", "crew": "AI Research Crew"}}), http.StatusUnprocessableEntity)
	// An input the named crew does not have is refused
	r = e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order",
		"payload": map[string]any{"brief": "Diligence", "crew": "PE Due Diligence Crew", "inputs": map[string]any{"topic": "Acme"}}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !slices.Contains(r.body.Error.Details, "inputs.topic: the crew has no input with this key") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	r = e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order",
		"payload": map[string]any{"brief": "Diligence on Acme", "crew": "PE Due Diligence Crew", "inputs": map[string]any{"company": "Acme"}}})
	wantStatus(t, r, http.StatusCreated)
	proposed := decodeData[proposeResponse](t, r)
	if !strings.HasPrefix(proposed.Proposal.Summary, "Start a work order on PE Due Diligence Crew") || proposed.Proposal.Payload["crew_id"] != chatPECrew {
		t.Errorf("proposal = %+v", proposed.Proposal)
	}

	// Turning chat off keeps the conversation readable but stops new messages
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE agent_chat SET enabled = false`); err != nil {
		t.Fatal(err)
	}
	view = decodeData[conversationView](t, e.as(ann).do(t, "GET", "/api/chat/conversations/"+view.Conversation.ID, nil))
	if view.ChatEnabled {
		t.Error("chat still enabled after turning it off")
	}
	if agents := decodeData[[]model.ChatAgent](t, e.as(ann).do(t, "GET", "/api/chat/agents", nil)); len(agents) != 0 {
		t.Errorf("chat agents after = %+v", agents)
	}
}
