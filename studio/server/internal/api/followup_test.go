package api

import (
	"context"
	"net/http"
	"testing"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// finishedWorkOrder submits a work order of the PE crew and finishes its attempt with output.
func (e testEnv) finishedWorkOrder(t *testing.T, output string) model.WorkOrder {
	t.Helper()
	crew := e.crewByName(t, "PE Due Diligence Crew")
	wo := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "Model prices",
		Brief: "Compare model prices.", CrewID: &crew.ID, Inputs: map[string]string{"company": "Acme"}, Submit: true}))
	if wo.CurrentRunID == nil {
		t.Fatalf("wo = %+v", wo)
	}
	if _, err := e.st.FinishRun(context.Background(), *wo.CurrentRunID, store.RunResult{Status: model.RunSucceeded, Output: &output}); err != nil {
		t.Fatal(err)
	}
	return wo
}

func revisionOf(t *testing.T, e testEnv, runID string) *model.RevisionSpec {
	t.Helper()
	run, err := e.st.GetRunDetail(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Spec == nil || run.Spec.WorkOrder == nil {
		t.Fatalf("run %s has no work order spec", runID)
	}
	return run.Spec.WorkOrder.Revision
}

func TestReviseWithFeedback(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	draft := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "t", CrewID: &crew.ID,
		Inputs: map[string]string{"company": "Acme"}}))
	r := e.do(t, "POST", "/api/work-orders/"+draft.ID+"/revise", map[string]any{"feedback": "more"})
	wantStatus(t, r, http.StatusConflict)

	wo := e.finishedWorkOrder(t, "GPT-5.2 costs $1.75 per million input tokens.")
	path := "/api/work-orders/" + wo.ID + "/revise"
	wantStatus(t, e.do(t, "POST", path, map[string]any{"feedback": "   "}), http.StatusUnprocessableEntity)

	got := decodeData[reviseResponse](t, e.do(t, "POST", path, map[string]any{"feedback": "Add Claude Haiku and cite sources."}))
	if got.Run.ID == *wo.CurrentRunID || got.Feedback.RevisionRunID == nil || *got.Feedback.RevisionRunID != got.Run.ID ||
		got.Feedback.RunID == nil || *got.Feedback.RunID != *wo.CurrentRunID || got.Feedback.CreatedByName != "Ada Admin" {
		t.Fatalf("revise = %+v", got)
	}
	rev := revisionOf(t, e, got.Run.ID)
	if rev == nil || rev.Attempt != 1 || rev.PreviousOutput != "GPT-5.2 costs $1.75 per million input tokens." ||
		rev.Feedback != "Add Claude Haiku and cite sources." || rev.FeedbackBy != "Ada Admin" || len(rev.Conversation) != 0 {
		t.Fatalf("revision = %+v", rev)
	}
	// While the revision runs, another one waits
	wantStatus(t, e.do(t, "POST", path, map[string]any{"feedback": "again"}), http.StatusConflict)

	list := decodeData[[]model.WorkOrderFeedback](t, e.do(t, "GET", "/api/work-orders/"+wo.ID+"/feedback", nil))
	if len(list) != 1 || list[0].Feedback != "Add Claude Haiku and cite sources." {
		t.Errorf("feedback = %+v", list)
	}
	// A plain re-run carries no revision
	if _, err := e.st.FinishRun(context.Background(), got.Run.ID, store.RunResult{Status: model.RunFailed}); err != nil {
		t.Fatal(err)
	}
	rerun := decodeData[model.Run](t, e.do(t, "POST", "/api/work-orders/"+wo.ID+"/submit", map[string]any{}))
	if revisionOf(t, e, rerun.ID) != nil {
		t.Error("a plain re-run was given a revision")
	}
}

func TestWorkOrderConversation(t *testing.T) {
	e := newEnv(t)
	wo := e.finishedWorkOrder(t, "Gemini 3 Pro is the cheapest frontier model here.")
	path := "/api/work-orders/" + wo.ID + "/conversation"

	// Nothing yet: null, not an error
	if r := e.do(t, "GET", path, nil); r.status != http.StatusOK || string(r.body.Data) != "null" {
		t.Fatalf("before = %d %s", r.status, r.body.Data)
	}
	wantStatus(t, e.do(t, "POST", path+"/messages", map[string]any{"content": "  "}), http.StatusUnprocessableEntity)

	// The crew has no chat set up: the agent of its last task answers
	r := e.do(t, "POST", path+"/messages", map[string]any{"content": "Why is Gemini cheapest?"})
	wantStatus(t, r, http.StatusCreated)
	view := decodeData[conversationView](t, r)
	if view.Conversation.WorkOrderID == nil || *view.Conversation.WorkOrderID != wo.ID || !view.ChatEnabled || len(view.Messages) != 2 {
		t.Fatalf("view = %+v", view)
	}
	reply := view.Messages[1]
	run, err := e.st.GetRunDetail(context.Background(), *reply.RunID)
	if err != nil {
		t.Fatal(err)
	}
	cw := run.Spec.Chat.WorkOrder
	if run.Kind != model.RunKindChat || cw == nil || cw.Output != "Gemini 3 Pro is the cheapest frontier model here." ||
		cw.Brief != "Compare model prices." || cw.Attempt != 1 || cw.Status != string(model.RunSucceeded) {
		t.Fatalf("chat work order = %+v", cw)
	}
	e.finishTurn(t, reply, "Its list price is lowest.", 0.001)

	// The same conversation continues, from the work order and from its own id
	again := decodeData[conversationView](t, e.do(t, "POST", path+"/messages", map[string]any{"content": "And Claude?"}))
	if again.Conversation.ID != view.Conversation.ID || len(again.Messages) != 4 {
		t.Fatalf("again = %+v", again)
	}
	e.finishTurn(t, again.Messages[3], "Haiku is close.", 0.001)
	if got := decodeData[conversationView](t, e.do(t, "GET", path, nil)); got.Conversation.ID != view.Conversation.ID {
		t.Errorf("get = %+v", got)
	}
	// It is not one of the Chat page's conversations
	if list := decodeData[[]model.Conversation](t, e.do(t, "GET", "/api/chat/conversations", nil)); len(list) != 0 {
		t.Errorf("chat page lists %+v", list)
	}

	// A revision can carry the conversation
	got := decodeData[reviseResponse](t, e.do(t, "POST", "/api/work-orders/"+wo.ID+"/revise",
		map[string]any{"feedback": "Use what we discussed.", "include_conversation": true}))
	rev := revisionOf(t, e, got.Run.ID)
	if rev == nil || len(rev.Conversation) != 4 || rev.Conversation[0].Content != "Why is Gemini cheapest?" || !got.Feedback.WithConversation {
		t.Fatalf("revision = %+v", rev)
	}
}

func TestWorkOrderConversationIsPersonal(t *testing.T) {
	e := newEnv(t)
	wo := e.finishedWorkOrder(t, "out")
	path := "/api/work-orders/" + wo.ID + "/conversation"
	wantStatus(t, e.do(t, "POST", path+"/messages", map[string]any{"content": "hi"}), http.StatusCreated)

	crew := e.crewByName(t, "PE Due Diligence Crew")
	_, builder := e.person(t, "bo@example.com", []string{builderRole}, crew.AreaIDs, nil)
	if r := e.as(builder).do(t, "GET", path, nil); string(r.body.Data) != "null" {
		t.Errorf("another person sees %s", r.body.Data)
	}
	// Viewers may look at work orders but not chat (no crews.chat)
	_, viewer := e.person(t, "vi@example.com", []string{viewerRole}, crew.AreaIDs, nil)
	wantStatus(t, e.as(viewer).do(t, "POST", path+"/messages", map[string]any{"content": "hi"}), http.StatusForbidden)
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/work-orders/"+wo.ID+"/revise", map[string]any{"feedback": "x"}), http.StatusForbidden)
	// Outside the areas: not found
	_, outsider := e.person(t, "out@example.com", []string{builderRole}, []string{hrArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", path, nil), http.StatusNotFound)
}
