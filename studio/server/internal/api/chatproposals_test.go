package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// chatTurn starts a turn and gives its run a token, as the run manager does when it starts the run.
func (e testEnv) chatTurn(t *testing.T, crew model.Crew, text string) (conversationView, stateRun) {
	t.Helper()
	view := e.startChat(t, crew.ID, text)
	reply := view.Messages[len(view.Messages)-1]
	run, err := e.st.GetRun(context.Background(), *reply.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return view, e.runningTurn(t, crew, run.ID)
}

// runningTurn starts a turn's run and gives it a token, as the run manager does.
func (e testEnv) runningTurn(t *testing.T, crew model.Crew, runID string) stateRun {
	t.Helper()
	if ok, err := e.st.StartRun(context.Background(), runID); err != nil || !ok {
		t.Fatalf("start run: %v %v", ok, err)
	}
	token, err := e.st.CreateRunToken(context.Background(), runID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.st.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return stateRun{run: run, crew: crew, token: token}
}

func TestChatTurnsReadStateAndOnlyPropose(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	_, sr := e.chatTurn(t, crew, "Start a sweep")

	scope := decodeData[store.RunScope](t, e.call(t, sr, "GET", "/scope", nil))
	if scope.Kind != model.RunKindChat || scope.ScheduleID != nil {
		t.Errorf("scope = %+v", scope)
	}
	wantStatus(t, e.call(t, sr, "GET", "/notes", nil), http.StatusOK)
	wantStatus(t, e.call(t, sr, "GET", "/log/check?subject=x&action=y", nil), http.StatusOK)
	// Writes are refused in code, whatever the model was told
	wantStatus(t, e.call(t, sr, "PUT", "/notes/risks", map[string]any{"content": "x"}), http.StatusForbidden)
	wantStatus(t, e.call(t, sr, "POST", "/log", map[string]any{"subject": "x", "action": "y"}), http.StatusForbidden)

	wantStatus(t, e.call(t, sr, "POST", "/proposals", map[string]any{"type": "drop_tables", "payload": map[string]any{}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order", "payload": map[string]any{"brief": ""}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order", "payload": map[string]any{"brief": "x", "evil": true}}), http.StatusUnprocessableEntity)
	// Inputs the crew does not have are refused, naming the key and the crew's keys, so the front desk can retry
	r := e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order",
		"payload": map[string]any{"brief": "Sweep", "inputs": map[string]string{"ticker": "ACME"}}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if d := strings.Join(r.body.Error.Details, "; "); !strings.Contains(d, "inputs.ticker: the crew has no input with this key") ||
		!strings.Contains(d, "company, location") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	r = e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order",
		"payload": map[string]any{"brief": "Sweep Atlas now\nand report", "inputs": map[string]string{"company": "Acme"}}})
	wantStatus(t, r, http.StatusCreated)
	got := decodeData[proposeResponse](t, r)
	if got.Proposal.Summary != "Start a work order: Sweep Atlas now and report" || got.State != model.ProposalPending {
		t.Errorf("proposal = %+v", got)
	}
	// One proposal per reply
	r = e.call(t, sr, "POST", "/proposals", map[string]any{"type": "notes_put", "payload": map[string]any{"name": "x", "content": "y", "base_version": 0}})
	wantStatus(t, r, http.StatusConflict)

	// Scheduled (and every other non-chat) runs cannot propose at all
	sched := e.scheduledRun(t)
	r = e.call(t, sched, "POST", "/proposals", map[string]any{"type": "start_work_order", "payload": map[string]any{"brief": "stop tracking"}})
	wantStatus(t, r, http.StatusForbidden)

	lines := e.stateLines(t, sr.run.ID)
	if len(lines) == 0 || lines[len(lines)-1] != "Chat: proposal refused: You already proposed a change in this reply. Tell the person about it; they can confirm or decline it." {
		t.Errorf("journal = %v", lines)
	}
}

// stateLines are a run's journal "state" lines.
func (e testEnv) stateLines(t *testing.T, runID string) []string {
	t.Helper()
	var lines []string
	for _, ev := range decodeData[[]model.RunEvent](t, e.do(t, "GET", "/api/runs/"+runID+"/events", nil)) {
		if ev.Type == "state" {
			lines = append(lines, ev.Payload["message"].(string))
		}
	}
	return lines
}

func TestConfirmingAWorkOrderProposal(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	view, sr := e.chatTurn(t, crew, "Sweep please")
	wantStatus(t, e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order",
		"payload": map[string]any{"title": "Atlas sweep", "brief": "Sweep Atlas", "inputs": map[string]string{"company": "Acme"}}}), http.StatusCreated)
	msgID := view.Messages[1].ID
	path := "/api/chat/messages/" + msgID

	// Only the conversation's owner, signed in (not through a key), can decide
	_, op := e.person(t, "olga@example.com", []string{operatorRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(op).do(t, "POST", path+"/confirm", map[string]any{}), http.StatusNotFound)
	key := e.personalKey(t, e.token)
	r := e.as("").do(t, "POST", path+"/confirm", map[string]any{}, "Authorization", "Bearer "+key)
	wantStatus(t, r, http.StatusForbidden)
	wantStatus(t, e.do(t, "POST", "/api/chat/messages/"+view.Messages[0].ID+"/confirm", map[string]any{}), http.StatusConflict)
	wantStatus(t, e.do(t, "POST", "/api/chat/messages/"+missingID+"/confirm", map[string]any{}), http.StatusNotFound)

	r = e.do(t, "POST", path+"/confirm", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	m := decodeData[model.ChatMessage](t, r)
	if *m.ProposalState != model.ProposalConfirmed || m.DecidedByName != "Ada Admin" || m.ProposalResult["work_order_id"] == nil {
		t.Fatalf("confirmed = %+v", m)
	}
	wo, err := e.st.GetWorkOrder(context.Background(), m.ProposalResult["work_order_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if wo.Source != model.SourceChat || wo.SourceID == nil || *wo.SourceID != view.Conversation.ID || wo.Title != "Atlas sweep" ||
		wo.CreatedByName != "Ada Admin" || wo.Status != string(model.RunQueued) {
		t.Errorf("work order = %+v", wo)
	}
	wantStatus(t, e.do(t, "POST", path+"/confirm", map[string]any{}), http.StatusConflict)
	wantStatus(t, e.do(t, "POST", path+"/undo", map[string]any{}), http.StatusConflict)
	if list := decodeData[[]model.WorkOrder](t, e.do(t, "GET", "/api/work-orders?source=chat", nil)); len(list) != 1 {
		t.Errorf("chat work orders = %+v", list)
	}
}

func TestDecliningAndExpiringProposals(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	view, sr := e.chatTurn(t, crew, "Sweep please")
	wantStatus(t, e.call(t, sr, "POST", "/proposals", map[string]any{"type": "start_work_order", "payload": map[string]any{"brief": "Sweep"}}), http.StatusCreated)
	path := "/api/chat/messages/" + view.Messages[1].ID
	m := decodeData[model.ChatMessage](t, e.do(t, "POST", path+"/decline", map[string]any{}))
	if *m.ProposalState != model.ProposalDeclined {
		t.Errorf("declined = %+v", m)
	}
	wantStatus(t, e.do(t, "POST", path+"/confirm", map[string]any{}), http.StatusConflict)

	e.finishTurn(t, view.Messages[1], "Proposed.", 0.01)
	r := e.do(t, "POST", "/api/chat/conversations/"+view.Conversation.ID+"/messages", map[string]any{"content": "again"})
	wantStatus(t, r, http.StatusCreated)
	next := decodeData[conversationView](t, r)
	reply := next.Messages[3]
	sr2 := e.runningTurn(t, crew, *reply.RunID)
	wantStatus(t, e.call(t, sr2, "POST", "/proposals", map[string]any{"type": "start_work_order", "payload": map[string]any{"brief": "Sweep"}}), http.StatusCreated)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE conversation_messages SET proposed_at = now() - interval '25 hours' WHERE id = $1`, reply.ID); err != nil {
		t.Fatal(err)
	}
	r = e.do(t, "POST", "/api/chat/messages/"+reply.ID+"/confirm", map[string]any{})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "proposal_expired" {
		t.Errorf("error = %+v", r.body.Error)
	}
	got := decodeData[conversationView](t, e.do(t, "GET", "/api/chat/conversations/"+view.Conversation.ID, nil))
	if *got.Messages[3].ProposalState != model.ProposalExpired {
		t.Errorf("state = %v", *got.Messages[3].ProposalState)
	}
}

func TestConfirmingAndUndoingANoteChange(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	ctx := context.Background()
	key := store.NoteKey{StateScope: store.StateScope{CrewID: crew.ID}, Name: "risks"}
	zero := 0
	if _, err := e.st.PutNote(ctx, key, "v1 text", &zero, store.NoteWriter{Name: "run"}); err != nil {
		t.Fatal(err)
	}
	view, sr := e.chatTurn(t, crew, "Add the vendor risk")
	r := e.call(t, sr, "POST", "/proposals", map[string]any{"type": "notes_put", "payload": map[string]any{"name": "risks", "content": "v2 text", "base_version": 1}})
	wantStatus(t, r, http.StatusCreated)
	if s := decodeData[proposeResponse](t, r).Proposal.Summary; s != "Change the note risks (version 1)" {
		t.Errorf("summary = %q", s)
	}
	path := "/api/chat/messages/" + view.Messages[1].ID
	m := decodeData[model.ChatMessage](t, e.do(t, "POST", path+"/confirm", map[string]any{}))
	note, err := e.st.LatestNote(ctx, key)
	if err != nil || note.Content != "v2 text" || note.WrittenBy != "Ada Admin" || m.ProposalResult["version"].(float64) != 2 {
		t.Fatalf("note = %+v, result %+v, err %v", note, m.ProposalResult, err)
	}
	m = decodeData[model.ChatMessage](t, e.do(t, "POST", path+"/undo", map[string]any{}))
	note, _ = e.st.LatestNote(ctx, key)
	if m.UndoneAt == nil || note.Content != "v1 text" || note.Version != 3 {
		t.Errorf("after undo: message %+v note %+v", m, note)
	}
	wantStatus(t, e.do(t, "POST", path+"/undo", map[string]any{}), http.StatusConflict)

	// A note changed since the proposal was made is not overwritten
	e.finishTurn(t, view.Messages[1], "Done.", 0.01)
	r = e.do(t, "POST", "/api/chat/conversations/"+view.Conversation.ID+"/messages", map[string]any{"content": "new note"})
	reply := decodeData[conversationView](t, r).Messages[3]
	sr2 := e.runningTurn(t, crew, *reply.RunID)
	wantStatus(t, e.call(t, sr2, "POST", "/proposals", map[string]any{"type": "notes_put", "payload": map[string]any{"name": "risks", "content": "stale", "base_version": 1}}), http.StatusCreated)
	r = e.do(t, "POST", "/api/chat/messages/"+reply.ID+"/confirm", map[string]any{})
	wantStatus(t, r, http.StatusConflict)
	got := decodeData[conversationView](t, e.do(t, "GET", "/api/chat/conversations/"+view.Conversation.ID, nil))
	if *got.Messages[3].ProposalState != model.ProposalPending {
		t.Errorf("a failed confirm should leave the proposal pending: %+v", got.Messages[3])
	}

	wantStatus(t, e.do(t, "POST", "/api/chat/messages/"+reply.ID+"/decline", map[string]any{}), http.StatusOK)
	wantStatus(t, e.do(t, "POST", "/api/chat/messages/"+reply.ID+"/undo", map[string]any{}), http.StatusConflict)
}

func TestUndoingANewNoteRemovesIt(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	view, sr := e.chatTurn(t, crew, "Start a risk register")
	wantStatus(t, e.call(t, sr, "POST", "/proposals", map[string]any{"type": "notes_put",
		"payload": map[string]any{"name": "risk-register", "subject": "project:atlas", "content": "none yet", "base_version": 0}}), http.StatusCreated)
	path := "/api/chat/messages/" + view.Messages[1].ID
	m := decodeData[model.ChatMessage](t, e.do(t, "POST", path+"/confirm", map[string]any{}))
	if m.Proposal.Summary != "Create the note risk-register about project:atlas" {
		t.Errorf("summary = %q", m.Proposal.Summary)
	}
	wantStatus(t, e.do(t, "POST", path+"/undo", map[string]any{}), http.StatusOK)
	key := store.NoteKey{StateScope: store.StateScope{CrewID: crew.ID}, Subject: "project:atlas", Name: "risk-register"}
	if _, err := e.st.LatestNote(context.Background(), key); err != store.ErrNotFound {
		t.Errorf("note still there: %v", err)
	}
}
