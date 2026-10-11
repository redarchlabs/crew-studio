package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

// handoffAgent turns chat and hand-offs on for the deal partner with the investigator as its specialist and the PE crew.
func (e testEnv) handoffAgent(t *testing.T, extra map[string]any) {
	t.Helper()
	body := map[string]any{
		"enabled": true, "llm_id": chatModel, "specialist_ids": []string{chatInvestigator}, "crew_ids": []string{chatPECrew},
		"daily_budget_usd": 2, "person_daily_budget_usd": 5, "handoffs_enabled": true, "handoff_budget_usd": 0.5,
		"handoff_timeout_minutes": 10, "max_active_handoffs": 2,
	}
	for k, v := range extra {
		body[k] = v
	}
	wantStatus(t, e.do(t, "PUT", "/api/agents/"+chatPartner+"/chat", body), http.StatusOK)
}

// proposedHandoff starts a conversation as token whose turn proposes handing work to the investigator; the turn then
// finishes. It returns the conversation and the card's message id.
func (e testEnv) proposedHandoff(t *testing.T, token string) (conversationView, string) {
	t.Helper()
	r := e.as(token).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "Compare the vendors"})
	wantStatus(t, r, http.StatusCreated)
	view := decodeData[conversationView](t, r)
	sr := e.runningTurn(t, model.Crew{}, *view.Messages[1].RunID)
	r = e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_handoff",
		"payload": map[string]any{"worker": "Corporate Investigator", "title": "Vendor comparison", "brief": "Compare Acme, Birch and Cedar."}})
	wantStatus(t, r, http.StatusCreated)
	proposed := decodeData[proposeResponse](t, r)
	if !strings.HasPrefix(proposed.Proposal.Summary, "Hand off to ") || !strings.HasSuffix(proposed.Proposal.Summary, ": Vendor comparison") {
		t.Errorf("summary = %q", proposed.Proposal.Summary)
	}
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET status = 'succeeded', output = 'Handing it on.', cost_usd = 0.01 WHERE id = $1`,
		sr.run.ID); err != nil {
		t.Fatal(err)
	}
	return view, proposed.MessageID
}

// A hand-off starts when its owner confirms the card; it shows on the conversation, only its owner can read or cancel
// it (signed in), and its run may only read and propose, never start another hand-off.
func TestAgentChatHandoffs(t *testing.T) {
	e := newEnv(t)
	e.handoffAgent(t, nil)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, []string{auth.RunsView, auth.RunsDelete, auth.AccountAPIKeys})
	_, bob := e.person(t, "bob@example.com", []string{operatorRole}, []string{financeArea}, []string{auth.RunsView})
	view, cardID := e.proposedHandoff(t, ann)

	r := e.as(ann).do(t, "POST", "/api/chat/messages/"+cardID+"/confirm", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	card := decodeData[model.ChatMessage](t, r)
	handoffID, _ := card.ProposalResult["handoff_id"].(string)
	runID, _ := card.ProposalResult["run_id"].(string)
	if handoffID == "" || runID == "" || card.ProposalResult["message_id"] == nil {
		t.Fatalf("result = %v", card.ProposalResult)
	}

	got := decodeData[conversationView](t, e.as(ann).do(t, "GET", "/api/chat/conversations/"+view.Conversation.ID, nil))
	if len(got.Handoffs) != 1 || got.Handoffs[0].ID != handoffID || got.Handoffs[0].RunStatus == nil || *got.Handoffs[0].RunStatus != model.RunQueued {
		t.Fatalf("hand-offs = %+v", got.Handoffs)
	}
	result := got.Messages[len(got.Messages)-1]
	if result.RunKind != model.RunKindHandoff || result.Handoff == nil || result.Handoff.Title != "Vendor comparison" || result.Content != "" {
		t.Errorf("result message = %+v", result)
	}
	if one := decodeData[model.Handoff](t, e.as(ann).do(t, "GET", "/api/chat/handoffs/"+handoffID, nil)); one.RunID == nil || *one.RunID != runID {
		t.Errorf("hand-off = %+v", one)
	}
	wantStatus(t, e.as(bob).do(t, "GET", "/api/chat/handoffs/"+handoffID, nil), http.StatusNotFound)
	wantStatus(t, e.as(ann).do(t, "GET", "/api/chat/handoffs/x", nil), http.StatusNotFound)

	// The hand-off's run: each proposal is a message of its own (at most five), never another hand-off; no crew state
	sr := e.runningTurn(t, model.Crew{}, runID)
	r = e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_handoff", "payload": map[string]any{"worker": "Corporate Investigator", "brief": "b"}})
	wantStatus(t, r, http.StatusForbidden)
	if r.body.Error.Code != "no_chains" {
		t.Errorf("code = %s", r.body.Error.Code)
	}
	wantStatus(t, e.call(t, sr, "GET", "/notes", nil), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "?"}), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "GET", "/control", nil), http.StatusOK)
	r = e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order",
		"payload": map[string]any{"brief": "Diligence on Acme", "crew": "PE Due Diligence Crew", "inputs": map[string]any{"company": "Acme"}}})
	wantStatus(t, r, http.StatusCreated)
	first := decodeData[proposeResponse](t, r).MessageID
	if first == result.ID {
		t.Error("the hand-off's proposal went onto its result message")
	}
	for i := 1; i <= 5; i++ {
		r = e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order",
			"payload": map[string]any{"brief": fmt.Sprintf("Diligence %d", i), "crew": "PE Due Diligence Crew", "inputs": map[string]any{"company": "Acme"}}})
		if i < 5 {
			wantStatus(t, r, http.StatusCreated)
			continue
		}
		wantStatus(t, r, http.StatusConflict)
		if r.body.Error.Code != "too_many_proposals" {
			t.Errorf("sixth proposal code = %s", r.body.Error.Code)
		}
	}
	got = decodeData[conversationView](t, e.as(ann).do(t, "GET", "/api/chat/conversations/"+view.Conversation.ID, nil))
	cards := 0
	for _, m := range got.Messages {
		if m.HandoffProposal {
			cards++
			if m.Handoff == nil || m.Handoff.ID != handoffID || m.RunID != nil || m.Proposal == nil || m.Content != m.Proposal.Summary {
				t.Errorf("hand-off card = %+v", m)
			}
		}
	}
	if cards != 5 || got.Handoffs[0].ResultMessageID != result.ID {
		t.Errorf("cards = %d, result = %s", cards, got.Handoffs[0].ResultMessageID)
	}

	// While it runs the conversation cannot be deleted
	r = e.as(ann).do(t, "DELETE", "/api/chat/conversations/"+view.Conversation.ID, nil)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "handoff_active" {
		t.Errorf("delete code = %s", r.body.Error.Code)
	}

	// Hand-off runs are private to their person on the Runs page and kept for the budgets
	if runs := decodeData[[]model.Run](t, e.as(ann).do(t, "GET", "/api/runs?kind=handoff", nil)); len(runs) != 1 || runs[0].ID != runID {
		t.Errorf("ann's hand-off runs = %+v", runs)
	}
	if runs := decodeData[[]model.Run](t, e.as(bob).do(t, "GET", "/api/runs?kind=handoff", nil)); len(runs) != 0 {
		t.Errorf("bob sees %d hand-off runs", len(runs))
	}
	wantStatus(t, e.as(bob).do(t, "GET", "/api/runs/"+runID, nil), http.StatusNotFound)

	// Its context is compacted by itself, not on request from the Runs page
	r = e.as(ann).do(t, "POST", "/api/runs/"+runID+"/compact", map[string]any{})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "chat_turn" {
		t.Errorf("compact code = %s", r.body.Error.Code)
	}

	// Cancelling: only the owner, signed in
	wantStatus(t, e.as(bob).do(t, "POST", "/api/chat/handoffs/"+handoffID+"/cancel", map[string]any{}), http.StatusNotFound)
	key := e.personalKey(t, ann)
	wantStatus(t, e.as("").do(t, "POST", "/api/chat/handoffs/"+handoffID+"/cancel", map[string]any{}, "Authorization", "Bearer "+key),
		http.StatusForbidden)
	r = e.as(ann).do(t, "POST", "/api/chat/handoffs/"+handoffID+"/cancel", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	if h := decodeData[model.Handoff](t, r); h.RunStatus == nil || *h.RunStatus != model.RunCancelled {
		t.Errorf("after cancel = %+v", h)
	}
	r = e.as(ann).do(t, "POST", "/api/chat/handoffs/"+handoffID+"/cancel", map[string]any{})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "handoff_finished" {
		t.Errorf("second cancel code = %s", r.body.Error.Code)
	}
	r = e.as(ann).do(t, "DELETE", "/api/runs/"+runID, nil)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "chat_turn_recent" {
		t.Errorf("delete run code = %s", r.body.Error.Code)
	}
	wantStatus(t, e.as(ann).do(t, "DELETE", "/api/chat/conversations/"+view.Conversation.ID, nil), http.StatusNoContent)
}

// The hand-off settings round-trip through the agent's chat settings and are checked.
func TestAgentChatHandoffSettings(t *testing.T) {
	e := newEnv(t)
	e.handoffAgent(t, map[string]any{"handoff_budget_usd": 0.75, "handoff_timeout_minutes": 30, "max_active_handoffs": 3})
	got := decodeData[model.AgentChatSettings](t, e.do(t, "GET", "/api/agents/"+chatPartner+"/chat", nil))
	if !got.HandoffsEnabled || got.HandoffBudgetUSD != 0.75 || got.HandoffTimeoutMinutes != 30 || got.MaxActiveHandoffs != 3 {
		t.Errorf("settings = %+v", got)
	}
	r := e.do(t, "PUT", "/api/agents/"+chatPartner+"/chat", map[string]any{"enabled": true, "daily_budget_usd": 1,
		"handoff_timeout_minutes": 500, "max_active_handoffs": 9})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if details := strings.Join(r.body.Error.Details, " "); !strings.Contains(details, "handoff_timeout_minutes") || !strings.Contains(details, "max_active_handoffs") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	wantStatus(t, e.do(t, "GET", "/api/runs?kind=other", nil), http.StatusUnprocessableEntity)
}
