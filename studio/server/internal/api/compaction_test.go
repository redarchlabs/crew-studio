package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/journal"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

func TestCompactRunningAttempt(t *testing.T) {
	e := newEnv(t)
	sr := e.askingRun(t, e.adminID, nil)
	path := "/api/runs/" + sr.run.ID + "/compact"

	// Only a running attempt can be compacted
	wantStatus(t, e.do(t, "POST", path, map[string]any{}), http.StatusConflict)
	if _, err := e.st.StartRun(context.Background(), sr.run.ID); err != nil {
		t.Fatal(err)
	}
	if got := decodeData[runControl](t, e.call(t, sr, "GET", "/control", nil)); got.CompactRequests != 0 {
		t.Fatalf("control before = %+v", got)
	}
	if got := decodeData[runControl](t, e.do(t, "POST", path, map[string]any{})); got.CompactRequests != 1 {
		t.Fatalf("compact = %+v", got)
	}
	decodeData[runControl](t, e.do(t, "POST", path, map[string]any{}))
	if got := decodeData[runControl](t, e.call(t, sr, "GET", "/control", nil)); got.CompactRequests != 2 {
		t.Errorf("control after = %+v", got)
	}

	// The request and what the runner reports are both in the journal
	if err := e.st.AppendEvent(context.Background(), sr.run.ID, model.RunEvent{Seq: 1000, Type: "context_compacted", Payload: map[string]any{
		"agent_role": "Researcher", "task_name": "Prices", "reason": "auto", "tokens_before": 48210, "tokens_after": 5900,
		"messages_summarised": 14, "limit_tokens": 40000}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AppendEvent(context.Background(), sr.run.ID, model.RunEvent{Seq: 1001, Type: "context_compacted", Payload: map[string]any{
		"agent_role": "Researcher", "reason": "asked", "failed": true, "error": "RuntimeError: provider down"}}); err != nil {
		t.Fatal(err)
	}
	j := decodeData[journal.Journal](t, e.do(t, "GET", "/api/runs/"+sr.run.ID+"/journal", nil))
	var asked, compacted, failed *journal.Entry
	for i, entry := range j.Entries {
		switch {
		case entry.Kind == "state":
			asked = &j.Entries[i]
		case entry.Kind == "compaction" && entry.Seq == 1001:
			failed = &j.Entries[i]
		case entry.Kind == "compaction":
			compacted = &j.Entries[i]
		}
	}
	if failed == nil || failed.Title != "Could not compact Researcher's context" || !strings.Contains(failed.Body, "provider down") {
		t.Errorf("failed compaction = %+v", failed)
	}
	if asked == nil || !strings.Contains(asked.Title, "Ada Admin asked to compact") {
		t.Errorf("request line = %+v", asked)
	}
	if compacted == nil || compacted.Title != "Compacted Researcher's context: about 48,210 → 5,900 tokens" ||
		!strings.Contains(compacted.Body, "40,000-token limit") || !strings.Contains(compacted.Body, "14 messages") {
		t.Errorf("compaction = %+v", compacted)
	}
}

func TestCompactRunNeedsCancelRights(t *testing.T) {
	e := newEnv(t)
	sr := e.askingRun(t, e.adminID, nil)
	if _, err := e.st.StartRun(context.Background(), sr.run.ID); err != nil {
		t.Fatal(err)
	}
	_, viewer := e.person(t, "vi@example.com", []string{viewerRole}, sr.crew.AreaIDs, nil)
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/runs/"+sr.run.ID+"/compact", map[string]any{}), http.StatusForbidden)
	wantStatus(t, e.do(t, "POST", "/api/runs/"+missingID+"/compact", map[string]any{}), http.StatusNotFound)
}

func TestCompactWorkOrderConversation(t *testing.T) {
	e := newEnv(t)
	wo := e.finishedWorkOrder(t, "Gemini is cheapest.")
	path := "/api/work-orders/" + wo.ID + "/conversation"

	// No conversation yet
	wantStatus(t, e.do(t, "POST", path+"/compact", map[string]any{}), http.StatusNotFound)
	first := decodeData[conversationView](t, e.do(t, "POST", path+"/messages", map[string]any{"content": "Why Gemini?"}))
	// Too little to compact while the first reply is still coming (and one message is not worth it)
	wantStatus(t, e.do(t, "POST", path+"/compact", map[string]any{}), http.StatusConflict)
	e.finishTurn(t, first.Messages[1], "Lowest list price.", 0.001)

	r := e.do(t, "POST", path+"/compact", map[string]any{})
	wantStatus(t, r, http.StatusCreated)
	view := decodeData[conversationView](t, r)
	summary := view.Messages[len(view.Messages)-1]
	if summary.Role != model.ChatRoleSummary || summary.RunID == nil || len(view.Messages) != 3 {
		t.Fatalf("view = %+v", view.Messages)
	}
	run, err := e.st.GetRunDetail(context.Background(), *summary.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Spec.Chat == nil || !run.Spec.Chat.Compact || len(run.Spec.Chat.History) != 2 || len(run.Spec.Agents[0].Tools) != 0 ||
		run.Spec.Agents[0].MaxIter != 1 {
		t.Fatalf("compaction spec = %+v", run.Spec.Chat)
	}
	// One turn at a time: no message while the summary is being written
	wantStatus(t, e.do(t, "POST", path+"/messages", map[string]any{"content": "And?"}), http.StatusConflict)
	e.finishTurn(t, summary, "Ada asked why Gemini; lowest list price.", 0.001)

	// A message that arrives after the history was read would be left out of the summary: refused
	stale := *run.Spec
	if _, err := e.st.CreateChatTurn(context.Background(), store.ChatTurn{ConversationID: view.Conversation.ID, Compact: true,
		HistoryThrough: 1, Spec: stale, DailyBudgetUSD: 1, PersonBudgetUSD: 5, MinBudgetUSD: 0.01,
		Meta: store.RunMeta{AreaIDs: stale.Crew.AreaIDs, CreatedBy: e.adminID}}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("stale compaction err = %v", err)
	}
	// The next turn is given the summary instead of the messages before it
	next := decodeData[conversationView](t, e.do(t, "POST", path+"/messages", map[string]any{"content": "And Claude?"}))
	turn, err := e.st.GetRunDetail(context.Background(), *next.Messages[len(next.Messages)-1].RunID)
	if err != nil {
		t.Fatal(err)
	}
	history := turn.Spec.Chat.History
	if len(history) != 1 || history[0].Role != model.ChatRoleSummary || history[0].Content != "Ada asked why Gemini; lowest list price." {
		t.Fatalf("history = %+v", history)
	}
	// Nothing new since the summary but one message: not worth compacting yet
	e.finishTurn(t, next.Messages[len(next.Messages)-1], "Close.", 0.001)
	got := decodeData[conversationView](t, e.do(t, "POST", path+"/compact", map[string]any{}))
	again, err := e.st.GetRunDetail(context.Background(), *got.Messages[len(got.Messages)-1].RunID)
	if err != nil {
		t.Fatal(err)
	}
	// A second compaction folds in the first summary
	if h := again.Spec.Chat.History; len(h) != 3 || h[0].Role != model.ChatRoleSummary {
		t.Errorf("second compaction history = %+v", h)
	}
}

func TestCompactChatConversation(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	conv := e.startChat(t, crew.ID, "Hi")
	path := "/api/chat/conversations/" + conv.Conversation.ID + "/compact"
	e.finishTurn(t, conv.Messages[1], "Hello.", 0.001)
	wantStatus(t, e.do(t, "POST", path, map[string]any{}), http.StatusCreated)
	_, other := e.person(t, "bo@example.com", []string{builderRole}, nil, nil)
	wantStatus(t, e.as(other).do(t, "POST", path, map[string]any{}), http.StatusNotFound)
}

func TestSettingsCompactContextTokens(t *testing.T) {
	e := newEnv(t)
	current := decodeData[model.Settings](t, e.do(t, "GET", "/api/settings", nil))
	if current.CompactContextTokens != 0 {
		t.Fatalf("default = %d", current.CompactContextTokens)
	}
	current.CompactContextTokens = 5_000
	wantStatus(t, e.do(t, "PUT", "/api/settings", current), http.StatusUnprocessableEntity)
	current.CompactContextTokens = 60_000
	if got := decodeData[model.Settings](t, e.do(t, "PUT", "/api/settings", current)); got.CompactContextTokens != 60_000 {
		t.Errorf("saved = %d", got.CompactContextTokens)
	}
}
