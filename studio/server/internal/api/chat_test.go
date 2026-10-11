package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

const operatorRole = "66666666-0000-4000-8000-000000000003"

// chatCrew turns chat on for the seeded PE crew (Finance), with its first agent as the front desk.
func (e testEnv) chatCrew(t *testing.T, budget float64) model.Crew {
	t.Helper()
	crew := e.crewByName(t, "PE Due Diligence Crew")
	r := e.do(t, "PUT", "/api/crews/"+crew.ID+"/chat", map[string]any{
		"enabled": true, "front_desk_agent_id": crew.AgentIDs[0], "llm_id": "11111111-0000-4000-8000-000000000003", "daily_budget_usd": budget,
	})
	wantStatus(t, r, http.StatusOK)
	return crew
}

func (e testEnv) startChat(t *testing.T, crewID, text string) conversationView {
	t.Helper()
	r := e.do(t, "POST", "/api/chat/conversations", map[string]any{"crew_id": crewID, "content": text})
	wantStatus(t, r, http.StatusCreated)
	return decodeData[conversationView](t, r)
}

// finishTurn completes the run behind a reply, as the run manager would.
func (e testEnv) finishTurn(t *testing.T, m model.ChatMessage, output string, cost float64) {
	t.Helper()
	if _, err := e.st.FinishRun(context.Background(), *m.RunID, store.RunResult{Status: model.RunSucceeded, Output: &output, CostUSD: &cost}); err != nil {
		t.Fatal(err)
	}
}

func TestChatSettings(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	path := "/api/crews/" + crew.ID + "/chat"

	got := decodeData[model.ChatSettings](t, e.do(t, "GET", path, nil))
	if got.Enabled || got.DailyBudgetUSD != model.DefaultChatDailyBudgetUSD || got.FrontDeskAgentID != nil {
		t.Errorf("default = %+v", got)
	}
	r := e.do(t, "PUT", path, map[string]any{"enabled": true, "daily_budget_usd": 1})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	r = e.do(t, "PUT", path, map[string]any{"enabled": true, "front_desk_agent_id": missingID, "llm_id": missingID, "daily_budget_usd": 0})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if len(r.body.Error.Details) != 1 || !strings.HasPrefix(r.body.Error.Details[0], "daily_budget_usd") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	r = e.do(t, "PUT", path, map[string]any{"enabled": true, "front_desk_agent_id": missingID, "llm_id": missingID, "daily_budget_usd": 2})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if len(r.body.Error.Details) != 2 {
		t.Errorf("details = %v", r.body.Error.Details)
	}

	e.chatCrew(t, 2.5)
	got = decodeData[model.ChatSettings](t, e.do(t, "GET", path, nil))
	if !got.Enabled || got.DailyBudgetUSD != 2.5 || got.UpdatedByName != "Ada Admin" || *got.FrontDeskAgentID != crew.AgentIDs[0] {
		t.Errorf("saved = %+v", got)
	}

	// The chat model cannot be deleted while a crew uses it
	r = e.do(t, "DELETE", "/api/llms/11111111-0000-4000-8000-000000000003", nil)
	wantStatus(t, r, http.StatusConflict)
	if !strings.Contains(r.body.Error.Message, "(chat)") {
		t.Errorf("error = %+v", r.body.Error)
	}

	// Viewers may read, not change; other areas see nothing
	_, viewer := e.person(t, "vic@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "GET", path, nil), http.StatusOK)
	wantStatus(t, e.as(viewer).do(t, "PUT", path, map[string]any{"daily_budget_usd": 1}), http.StatusForbidden)
	_, outsider := e.person(t, "ozzy@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", path, nil), http.StatusNotFound)
	wantStatus(t, e.as(outsider).do(t, "GET", "/api/crews/nope/chat", nil), http.StatusNotFound)
}

func TestChatTurns(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	wantStatus(t, e.do(t, "POST", "/api/chat/conversations", map[string]any{"crew_id": crew.ID, "content": "hi"}), http.StatusUnprocessableEntity)
	if crews := decodeData[[]model.ChatCrew](t, e.do(t, "GET", "/api/chat/crews", nil)); len(crews) != 0 {
		t.Errorf("chat crews before = %+v", crews)
	}
	e.chatCrew(t, 1)
	crews := decodeData[[]model.ChatCrew](t, e.do(t, "GET", "/api/chat/crews", nil))
	if len(crews) != 1 || crews[0].ID != crew.ID || crews[0].FrontDeskName == "" {
		t.Fatalf("chat crews = %+v", crews)
	}
	wantStatus(t, e.do(t, "POST", "/api/chat/conversations", map[string]any{"crew_id": crew.ID, "content": "   "}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/chat/conversations", map[string]any{"crew_id": "x", "content": "hi"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/chat/conversations", map[string]any{"crew_id": missingID, "content": "hi"}), http.StatusNotFound)

	// A crew-wide note is given to every turn; a schedule's notes are not
	ctx := context.Background()
	zero := 0
	if _, err := e.st.PutNote(ctx, store.NoteKey{StateScope: store.StateScope{CrewID: crew.ID}, Name: "risks"}, "Vendor late", &zero, store.NoteWriter{Name: "run"}); err != nil {
		t.Fatal(err)
	}
	before := e.runs.notified.Load()
	view := e.startChat(t, crew.ID, "What is slipping?\nPlease be brief.")
	if e.runs.notified.Load() != before+1 {
		t.Error("the run manager was not woken")
	}
	if view.Conversation.Title != "What is slipping? Please be brief." || len(view.Messages) != 2 || !view.ChatEnabled {
		t.Fatalf("view = %+v", view)
	}
	user, reply := view.Messages[0], view.Messages[1]
	if user.Role != model.ChatRoleUser || reply.Role != model.ChatRoleAssistant || reply.RunID == nil || *reply.RunStatus != model.RunQueued {
		t.Fatalf("messages = %+v", view.Messages)
	}
	run, err := e.st.GetRunDetail(ctx, *reply.RunID)
	if err != nil {
		t.Fatal(err)
	}
	spec := run.Spec
	if run.Kind != model.RunKindChat || run.CreatedByName != "Ada Admin" || run.BudgetUSD != 1 || spec.Chat == nil || len(spec.Tasks) != 0 {
		t.Fatalf("run = %+v spec = %+v", run, spec)
	}
	// Every chat tool but the tracker ones, which need a Jira or Linear connection the crew may use
	if !slices.Equal(spec.Agents[0].Tools, []string{"notes_get", "notes_list", "log_check", "start_work_order", "propose_note_change"}) || *spec.Agents[0].LLMID != "11111111-0000-4000-8000-000000000003" ||
		spec.Agents[0].AllowDelegation || len(spec.LLMs) != 1 {
		t.Errorf("front desk = %+v", spec.Agents[0])
	}
	if spec.Chat.Message != "What is slipping?\nPlease be brief." || len(spec.Chat.History) != 0 || len(spec.Chat.Notes) != 1 ||
		spec.Chat.Notes[0].Content != "Vendor late" || spec.Chat.PersonName != "Ada Admin" {
		t.Errorf("chat spec = %+v", spec.Chat)
	}

	// One turn at a time
	path := "/api/chat/conversations/" + view.Conversation.ID
	r := e.do(t, "POST", path+"/messages", map[string]any{"content": "and?"})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "chat_turn_active" {
		t.Errorf("error = %+v", r.body.Error)
	}

	// The reply arrives with the run's output; the next turn sees it as history
	e.finishTurn(t, reply, "Atlas is slipping.", 0.25)
	got := decodeData[conversationView](t, e.do(t, "GET", path, nil))
	if got.Messages[1].Content != "Atlas is slipping." || got.Budget.SpentUSD != 0.25 || got.Budget.RemainingUSD != 0.75 {
		t.Errorf("after reply = %+v", got)
	}
	r = e.do(t, "POST", path+"/messages", map[string]any{"content": "Why?"})
	wantStatus(t, r, http.StatusCreated)
	second := decodeData[conversationView](t, r)
	next, err := e.st.GetRunSpec(ctx, *second.Messages[3].RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Chat.History) != 2 || next.Chat.History[1].Content != "Atlas is slipping." || next.BudgetUSD != 0.75 {
		t.Errorf("second turn = %+v budget %v", next.Chat.History, next.BudgetUSD)
	}

	// A failed reply says why; a spent budget stops the conversation
	msg := "Budget used up.\nLast output: secret-ish detail"
	cost := 0.75
	if _, err := e.st.FinishRun(ctx, *second.Messages[3].RunID, store.RunResult{Status: model.RunFailed, Error: &msg, CostUSD: &cost}); err != nil {
		t.Fatal(err)
	}
	got = decodeData[conversationView](t, e.do(t, "GET", path, nil))
	if got.Messages[3].RunError != "Budget used up." || got.Messages[3].Content != "" || got.Budget.RemainingUSD != 0 {
		t.Errorf("failed reply = %+v", got.Messages[3])
	}
	r = e.do(t, "POST", path+"/messages", map[string]any{"content": "More?"})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "chat_budget_used" {
		t.Errorf("error = %+v", r.body.Error)
	}

	list := decodeData[[]model.Conversation](t, e.do(t, "GET", "/api/chat/conversations?crew_id="+crew.ID, nil))
	if len(list) != 1 || list[0].CrewName != crew.Name {
		t.Errorf("list = %+v", list)
	}
	wantStatus(t, e.do(t, "GET", "/api/chat/conversations?crew_id=x", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "DELETE", path, nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "GET", path, nil), http.StatusNotFound)
}

func TestChatIsPersonalAndNeedsThePermission(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	view := e.startChat(t, crew.ID, "Hello")
	path := "/api/chat/conversations/" + view.Conversation.ID

	// Operators may chat; their list holds only their own conversations, and others' are not found
	_, op := e.person(t, "olga@example.com", []string{operatorRole}, []string{financeArea}, nil)
	if list := decodeData[[]model.Conversation](t, e.as(op).do(t, "GET", "/api/chat/conversations", nil)); len(list) != 0 {
		t.Errorf("operator list = %+v", list)
	}
	wantStatus(t, e.as(op).do(t, "GET", path, nil), http.StatusNotFound)
	wantStatus(t, e.as(op).do(t, "POST", path+"/messages", map[string]any{"content": "hi"}), http.StatusNotFound)
	wantStatus(t, e.as(op).do(t, "DELETE", path, nil), http.StatusNotFound)
	wantStatus(t, e.as(op).do(t, "POST", "/api/chat/conversations", map[string]any{"crew_id": crew.ID, "content": "hi"}), http.StatusCreated)

	// Viewers lack crews.chat; people in other areas cannot see the crew
	_, viewer := e.person(t, "vic@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "GET", "/api/chat/crews", nil), http.StatusForbidden)
	_, outsider := e.person(t, "ozzy@example.com", []string{operatorRole}, []string{itArea}, nil)
	if crews := decodeData[[]model.ChatCrew](t, e.as(outsider).do(t, "GET", "/api/chat/crews", nil)); len(crews) != 0 {
		t.Errorf("outsider crews = %+v", crews)
	}
	wantStatus(t, e.as(outsider).do(t, "POST", "/api/chat/conversations", map[string]any{"crew_id": crew.ID, "content": "hi"}), http.StatusNotFound)

	// Turning chat off keeps conversations readable but stops new turns
	wantStatus(t, e.do(t, "PUT", "/api/crews/"+crew.ID+"/chat", map[string]any{"enabled": false, "daily_budget_usd": 1}), http.StatusOK)
	e.finishTurn(t, view.Messages[1], "Hi.", 0.01)
	got := decodeData[conversationView](t, e.do(t, "GET", path, nil))
	if got.ChatEnabled {
		t.Error("chat should show as off")
	}
	wantStatus(t, e.do(t, "POST", path+"/messages", map[string]any{"content": "still there?"}), http.StatusUnprocessableEntity)
	if !auth.Known(auth.CrewsChat) {
		t.Error("crews.chat missing from the catalog")
	}
}

func TestChatRunsInListsAndOverview(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	view := e.startChat(t, crew.ID, "Hello")
	e.finishTurn(t, view.Messages[1], "Hi.", 0.2)
	e.startRun(t)

	chats := decodeData[[]model.Run](t, e.do(t, "GET", "/api/runs?kind=chat", nil))
	crewRuns := decodeData[[]model.Run](t, e.do(t, "GET", "/api/runs?kind=crew", nil))
	if len(chats) != 1 || chats[0].Kind != model.RunKindChat || len(crewRuns) != 1 || crewRuns[0].Kind != model.RunKindCrew {
		t.Errorf("chat runs %+v, crew runs %+v", chats, crewRuns)
	}
	wantStatus(t, e.do(t, "GET", "/api/runs?kind=other", nil), http.StatusUnprocessableEntity)
	// Chat turns are private: someone else who can view runs in the area sees neither the turn nor its prompts
	_, op := e.person(t, "olga@example.com", []string{operatorRole}, []string{financeArea}, nil)
	if runs := decodeData[[]model.Run](t, e.as(op).do(t, "GET", "/api/runs", nil)); len(runs) != 1 || runs[0].Kind != model.RunKindCrew {
		t.Errorf("operator sees %+v", runs)
	}
	for _, path := range []string{"", "/journal", "/events", "/llm-calls"} {
		wantStatus(t, e.as(op).do(t, "GET", "/api/runs/"+chats[0].ID+path, nil), http.StatusNotFound)
	}
	wantStatus(t, e.do(t, "GET", "/api/runs/"+chats[0].ID, nil), http.StatusOK)
	// A recent turn counts towards chat budgets, so it cannot be deleted yet
	r := e.do(t, "DELETE", "/api/runs/"+chats[0].ID, nil)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "chat_turn_recent" {
		t.Errorf("error = %+v", r.body.Error)
	}
	o := decodeData[model.Overview](t, e.do(t, "GET", "/api/overview", nil))
	if len(o.RecentRuns) != 1 || o.RecentRuns[0].Kind != model.RunKindCrew || o.ChatSpendLast30dUSD != 0.2 || o.SpendLast30dUSD != 0.2 {
		t.Errorf("overview = %+v", o)
	}
}

func TestChatTurnsGetTheToolsOfThePersonChatting(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	// Someone who may chat but not read state: no notes or action log for their turns
	_, token := e.person(t, "carl@example.com", nil, []string{financeArea}, []string{"crews.chat"})
	r := e.as(token).do(t, "POST", "/api/chat/conversations", map[string]any{"crew_id": crew.ID, "content": "hi"})
	wantStatus(t, r, http.StatusCreated)
	reply := decodeData[conversationView](t, r).Messages[1]
	sr := e.runningTurn(t, crew, *reply.RunID)
	wantStatus(t, e.call(t, sr, "GET", "/notes", nil), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "GET", "/notes/risks", nil), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "GET", "/log/check?subject=x&action=y", nil), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order", "payload": map[string]any{"brief": "x"}}), http.StatusForbidden)
	// Deleting a conversation while its reply is being written is refused
	r = e.as(token).do(t, "DELETE", "/api/chat/conversations/"+reply.ConversationID, nil)
	wantStatus(t, r, http.StatusConflict)
}
