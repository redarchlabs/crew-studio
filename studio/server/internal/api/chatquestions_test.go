package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

// questionsOn are the agent settings that let a hand-off's lead ask the person (slice 3c).
var questionsOn = map[string]any{"questions_enabled": true, "question_rules": "Ask before choosing between price models",
	"question_timeout_minutes": 5, "question_on_timeout": "proceed", "max_questions": 2}

// startedHandoff confirms a hand-off card as token and returns the conversation, the hand-off's id and its running run.
func (e testEnv) startedHandoff(t *testing.T, token string) (conversationView, string, stateRun) {
	t.Helper()
	view, cardID := e.proposedHandoff(t, token)
	r := e.as(token).do(t, "POST", "/api/chat/messages/"+cardID+"/confirm", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	card := decodeData[model.ChatMessage](t, r)
	handoffID, _ := card.ProposalResult["handoff_id"].(string)
	runID, _ := card.ProposalResult["run_id"].(string)
	return view, handoffID, e.runningTurn(t, model.Crew{}, runID)
}

// With the agent's questions on, a hand-off's lead may ask the person: the question is the conversation owner's, shows
// on the conversation and its hand-off, and only the owner answers it, signed in. The run hears the answer.
func TestAHandoffAsksThePerson(t *testing.T) {
	e := newEnv(t)
	e.handoffAgent(t, questionsOn)
	annID, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, []string{auth.AccountAPIKeys})
	_, bob := e.person(t, "bob@example.com", []string{operatorRole}, []string{financeArea}, nil)
	view, handoffID, sr := e.startedHandoff(t, ann)
	convID := view.Conversation.ID

	r := e.call(t, sr, "POST", "/questions", map[string]any{"question": "List or negotiated prices?", "context": "The table needs one.",
		"task_name": "Hand-off", "asked_for": "Corporate Investigator"})
	wantStatus(t, r, http.StatusCreated)
	q := decodeData[runQuestion](t, r)

	got := decodeData[conversationView](t, e.as(ann).do(t, "GET", "/api/chat/conversations/"+convID, nil))
	if len(got.Questions) != 1 || got.Questions[0].ID != q.ID || got.Questions[0].AssigneeID == nil || *got.Questions[0].AssigneeID != annID ||
		got.Questions[0].AskedFor != "Corporate Investigator" || got.Questions[0].HandoffID == nil || *got.Questions[0].HandoffID != handoffID ||
		got.Questions[0].ConversationID == nil || *got.Questions[0].ConversationID != convID || got.Questions[0].HandoffTitle != "Vendor comparison" {
		t.Fatalf("questions = %+v", got.Questions)
	}
	if h := got.Handoffs[0]; h.OpenQuestionID == nil || *h.OpenQuestionID != q.ID {
		t.Errorf("hand-off open question = %v", h.OpenQuestionID)
	}
	if res := got.Messages[len(got.Messages)-1]; res.Handoff == nil || res.Handoff.OpenQuestionID == nil || *res.Handoff.OpenQuestionID != q.ID {
		t.Errorf("result message = %+v", res.Handoff)
	}
	mine := decodeData[[]model.HumanQuestion](t, e.as(ann).do(t, "GET", "/api/questions", nil))
	if len(mine) != 1 || mine[0].ConversationTitle == "" || mine[0].ConversationID == nil {
		t.Errorf("ann's waiting questions = %+v", mine)
	}
	listed := decodeData[[]model.HumanQuestion](t, e.as(ann).do(t, "GET", "/api/chat/conversations/"+convID+"/questions", nil))
	if len(listed) != 1 || listed[0].ID != q.ID {
		t.Errorf("conversation questions = %+v", listed)
	}
	// Someone else cannot list them, nor answer: to them the question does not exist
	wantStatus(t, e.as(bob).do(t, "GET", "/api/chat/conversations/"+convID+"/questions", nil), http.StatusNotFound)
	answerPath := "/api/questions/" + q.ID + "/answer"
	wantStatus(t, e.as(bob).do(t, "POST", answerPath, map[string]any{"answer": "List"}), http.StatusNotFound)
	// The owner reads and answers signed in, not with an API key
	key := e.personalKey(t, ann)
	withKey := func(method, path string, body any) response {
		return e.as("").do(t, method, path, body, "Authorization", "Bearer "+key)
	}
	if got := decodeData[[]model.HumanQuestion](t, withKey("GET", "/api/questions", nil)); len(got) != 0 {
		t.Errorf("an API key lists %d hand-off questions", len(got))
	}
	if got := decodeData[[]model.HumanQuestion](t, withKey("GET", "/api/chat/conversations/"+convID+"/questions", nil)); len(got) != 0 {
		t.Errorf("an API key lists the conversation's questions: %+v", got)
	}
	if got := decodeData[conversationView](t, withKey("GET", "/api/chat/conversations/"+convID, nil)); len(got.Questions) != 0 {
		t.Errorf("an API key reads the conversation's open questions: %+v", got.Questions)
	}
	wantStatus(t, e.as("").do(t, "POST", answerPath, map[string]any{"answer": "List"}, "Authorization", "Bearer "+key), http.StatusForbidden)
	wantStatus(t, e.as(ann).do(t, "POST", answerPath, map[string]any{"answer": "List prices."}), http.StatusOK)
	// Once only
	wantStatus(t, e.as(ann).do(t, "POST", answerPath, map[string]any{"answer": "Negotiated."}), http.StatusConflict)
	polled := decodeData[runQuestion](t, e.call(t, sr, "GET", "/questions/"+q.ID, nil))
	if polled.Status != model.QuestionAnswered || polled.Answer != "List prices." || polled.OnTimeout != model.EscalationProceed {
		t.Errorf("polled = %+v", polled)
	}
	got = decodeData[conversationView](t, e.as(ann).do(t, "GET", "/api/chat/conversations/"+convID, nil))
	if len(got.Questions) != 0 || got.Handoffs[0].OpenQuestionID != nil {
		t.Errorf("after the answer: questions %+v, open %v", got.Questions, got.Handoffs[0].OpenQuestionID)
	}

	// The journal says who it was asked for; the agent's limit holds
	events, err := e.st.ListEvents(context.Background(), sr.run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	asked := false
	for _, ev := range events {
		if msg, _ := ev.Payload["message"].(string); strings.HasPrefix(msg, "Asked a human for Corporate Investigator: List or negotiated") {
			asked = true
		}
	}
	if !asked {
		t.Error("no journal line for the question")
	}
	wantStatus(t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "Which region?"}), http.StatusCreated)
	wantStatus(t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "Which year?"}), http.StatusTooManyRequests)
}

// The hand-off's deadline does not pause for a question: one asked with under a minute left before the hand-off must
// stop is refused, and the run is told to decide itself.
func TestAHandoffOutOfTimeCannotAsk(t *testing.T) {
	e := newEnv(t)
	e.handoffAgent(t, questionsOn)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	_, handoffID, sr := e.startedHandoff(t, ann)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE chat_handoffs SET deadline_at = now() + interval '80 seconds' WHERE id = $1`,
		handoffID); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, sr, "POST", "/questions", map[string]any{"question": "Prices?"})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "handoff_out_of_time" {
		t.Errorf("code = %s", r.body.Error.Code)
	}
}

// Only a crew run or a hand-off may ask: a chat turn is refused even when an escalation found its way into its spec.
func TestAChatTurnNeverAsksThePerson(t *testing.T) {
	e := newEnv(t)
	e.handoffAgent(t, questionsOn)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	r := e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner, "content": "Compare the vendors"})
	wantStatus(t, r, http.StatusCreated)
	view := decodeData[conversationView](t, r)
	runID := *view.Messages[1].RunID
	if _, err := e.st.Pool().Exec(context.Background(),
		`UPDATE runs SET spec = jsonb_set(spec, '{escalation}', '{"rules":"x","timeout_minutes":5,"on_timeout":"proceed","max_questions":3}') WHERE id = $1`,
		runID); err != nil {
		t.Fatal(err)
	}
	sr := e.runningTurn(t, model.Crew{}, runID)
	r = e.call(t, sr, "POST", "/questions", map[string]any{"question": "Prices?"})
	wantStatus(t, r, http.StatusForbidden)
	if r.body.Error.Code != "escalation_off" {
		t.Errorf("code = %s", r.body.Error.Code)
	}
	wantStatus(t, e.call(t, sr, "GET", "/questions/"+missingID, nil), http.StatusNotFound)
	// Its conversation lists none (an empty list, not null)
	if got := decodeData[conversationView](t, e.as(ann).do(t, "GET", "/api/chat/conversations/"+view.Conversation.ID, nil)); got.Questions == nil ||
		len(got.Questions) != 0 {
		t.Errorf("questions = %#v", got.Questions)
	}
}

// The question settings round-trip through the agent's chat settings; rules are required when questions are on.
func TestAgentChatQuestionSettings(t *testing.T) {
	e := newEnv(t)
	e.handoffAgent(t, map[string]any{"questions_enabled": true, "question_rules": "  Ask about prices  ", "question_timeout_minutes": 12,
		"question_on_timeout": "fail", "max_questions": 4})
	got := decodeData[model.AgentChatSettings](t, e.do(t, "GET", "/api/agents/"+chatPartner+"/chat", nil))
	if !got.QuestionsEnabled || got.QuestionRules != "Ask about prices" || got.QuestionTimeoutMinutes != 12 || got.QuestionOnTimeout != "fail" ||
		got.MaxQuestions != 4 {
		t.Errorf("settings = %+v", got)
	}
	r := e.do(t, "PUT", "/api/agents/"+chatPartner+"/chat", map[string]any{"enabled": true, "daily_budget_usd": 1,
		"questions_enabled": true, "question_rules": " ", "question_on_timeout": "wait", "max_questions": 50})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	details := strings.Join(r.body.Error.Details, " ")
	for _, field := range []string{"question_rules", "question_on_timeout", "max_questions"} {
		if !strings.Contains(details, field) {
			t.Errorf("details %v miss %s", r.body.Error.Details, field)
		}
	}
}

// A hand-off's question is only ever its conversation owner's: with the owner gone (no assignee) no work order operator
// may see or answer it, unlike a crew's unaddressed question. Only a hand-off may say whom it asks for.
func TestAHandoffQuestionIsOnlyTheOwners(t *testing.T) {
	e := newEnv(t)
	e.handoffAgent(t, questionsOn)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	_, bob := e.person(t, "bob@example.com", []string{operatorRole}, []string{financeArea}, []string{auth.WorkOrdersCancel})
	_, _, sr := e.startedHandoff(t, ann)
	q := decodeData[runQuestion](t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "Prices?"}))
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE human_questions SET assignee_id = NULL WHERE id = $1`, q.ID); err != nil {
		t.Fatal(err)
	}
	if mine := decodeData[[]model.HumanQuestion](t, e.as(bob).do(t, "GET", "/api/questions", nil)); len(mine) != 0 {
		t.Errorf("an operator sees the hand-off's question: %+v", mine)
	}
	// Answering it is the same 404 as for a question that does not exist
	wantStatus(t, e.as(bob).do(t, "POST", "/api/questions/"+q.ID+"/answer", map[string]any{"answer": "List"}), http.StatusNotFound)
	wantStatus(t, e.as(bob).do(t, "POST", "/api/questions/"+missingID+"/answer", map[string]any{"answer": "List"}), http.StatusNotFound)

	// A crew run's claim to ask for another agent is dropped
	crewRun := e.askingRun(t, e.adminID, escalationOn)
	asked := decodeData[runQuestion](t, e.call(t, crewRun, "POST", "/questions", map[string]any{"question": "Budget?", "asked_for": "Someone else"}))
	got, _, err := e.st.GetQuestion(context.Background(), asked.ID)
	if err != nil || got.AskedFor != "" {
		t.Errorf("crew question asked for %q, %v", got.AskedFor, err)
	}
}

// A hand-off whose owner can no longer answer (disabled, or gone) asks nobody: it is told at once to decide itself
// instead of waiting until the question times out.
func TestAHandoffWithNobodyToAskIsRefused(t *testing.T) {
	e := newEnv(t)
	e.handoffAgent(t, questionsOn)
	annID, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, nil)
	_, _, sr := e.startedHandoff(t, ann)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE users SET status = 'disabled' WHERE id = $1`, annID); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, sr, "POST", "/questions", map[string]any{"question": "Prices?"})
	wantStatus(t, r, http.StatusForbidden)
	if r.body.Error.Code != "escalation_off" || !strings.Contains(r.body.Error.Message, "no one to ask") {
		t.Errorf("error = %+v", r.body.Error)
	}
}
