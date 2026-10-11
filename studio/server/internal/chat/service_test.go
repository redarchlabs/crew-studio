package chat

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

const (
	peCrew      = "44444444-0000-4000-8000-000000000001"
	financeArea = "55555555-0000-4000-8000-000000000001"
	shortLLM    = "11111111-0000-4000-8000-000000000003"
)

type notifier struct{ n atomic.Int32 }

func (f *notifier) Notify() { f.n.Add(1) }

type noTools struct{}

func (noTools) ByKey() (map[string]model.Tool, error) { return map[string]model.Tool{}, nil }

type env struct {
	st   *store.Store
	svc  *Service
	runs *notifier
	ann  *auth.Principal
	crew model.Crew
}

func newEnv(t *testing.T) env {
	t.Helper()
	st := store.New(testdb.New(t, "chat_test"))
	runs := &notifier{}
	svc := New(st, launch.New(st, noTools{}, runs), runs)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, model.UserInput{Kind: auth.KindHuman, Email: "ann@example.com", Name: "Ann", Status: model.UserActive,
		AreaIDs: []string{financeArea}, Permissions: []string{auth.CrewsChat, auth.WorkOrdersCreate, auth.StateView, auth.StateManage}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.LoadPrincipal(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Via = auth.ViaSession
	crew, err := st.GetCrew(ctx, peCrew)
	if err != nil {
		t.Fatal(err)
	}
	return env{st: st, svc: svc, runs: runs, ann: p, crew: crew}
}

func (e env) turnOn(t *testing.T, llm *string, budget float64) {
	t.Helper()
	desk := e.crew.AgentIDs[0]
	if _, err := e.st.PutChatSettings(context.Background(), model.ChatSettings{CrewID: e.crew.ID, Enabled: true, FrontDeskAgentID: &desk,
		LLMID: llm, DailyBudgetUSD: budget}, ""); err != nil {
		t.Fatal(err)
	}
}

// running marks a turn's run running, as the run manager does; only running turns may propose.
func (e env) running(t *testing.T, runID string) store.RunScope {
	t.Helper()
	if ok, err := e.st.StartRun(context.Background(), runID); err != nil || !ok {
		t.Fatalf("start run: %v %v", ok, err)
	}
	// As the run token's scope: the run's crew, "" for an agent chat
	var crewID string
	if err := e.st.Pool().QueryRow(context.Background(), `SELECT COALESCE(crew_id::text, '') FROM runs WHERE id = $1`, runID).Scan(&crewID); err != nil {
		t.Fatal(err)
	}
	return store.RunScope{RunID: runID, Kind: model.RunKindChat, CrewID: crewID}
}

func wantProblem(t *testing.T, err error, fragment string) {
	t.Helper()
	var problems *launch.ProblemsError
	if !errors.As(err, &problems) || !strings.Contains(strings.Join(problems.Details, " "), fragment) {
		t.Errorf("err = %v, want a problem with %q", err, fragment)
	}
}

func TestStartingTurns(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "hi")
	wantProblem(t, err, "turned off")
	if _, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, " "); err == nil {
		t.Error("an empty message was accepted")
	}
	if _, err := e.svc.StartConversation(ctx, &auth.Principal{Kind: auth.KindHuman}, e.crew.ID, "hi"); err == nil {
		t.Error("someone without crews.chat started a turn")
	}
	if _, err := e.svc.StartConversation(ctx, e.ann, "99999999-0000-4000-8000-000000000000", "hi"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing crew = %v", err)
	}
	if _, err := e.st.Pool().Exec(ctx, `INSERT INTO crew_chat (crew_id, enabled, front_desk_agent_id, daily_budget_usd) VALUES ($1, true, NULL, 1)`, e.crew.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.StartConversation(ctx, e.ann, e.crew.ID, "hi")
	wantProblem(t, err, "no longer in the crew")

	e.turnOn(t, nil, 0.005)
	// The turn fails on the budget; the empty conversation is not kept
	_, err = e.svc.StartConversation(ctx, e.ann, e.crew.ID, "hi")
	var conflict *store.ConflictError
	if !errors.As(err, &conflict) || conflict.Code != "chat_budget_used" {
		t.Errorf("tiny budget = %v", err)
	}
	if list, _ := e.st.ListConversations(ctx, store.ConversationFilter{UserID: e.ann.UserID, Scope: store.AllAreas, Limit: 5}); len(list) != 0 {
		t.Errorf("left a conversation behind: %+v", list)
	}

	short := shortLLM
	e.turnOn(t, &short, 1)
	e.ann.Via = auth.ViaAPIKey
	turn, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Run.TriggerType != "api" || e.runs.n.Load() != 1 || turn.Conversation.Title != "hi" {
		t.Errorf("turn = %+v", turn)
	}
	_, err = e.svc.SendMessage(ctx, e.ann, turn.Conversation.ID, "again")
	if !store.IsTurnActive(err) {
		t.Errorf("second turn = %v", err)
	}
	// While the turn is queued it counts at its whole budget towards the person's cap
	if spent, err := e.st.PersonChatSpent(ctx, e.crew.ID, e.ann.UserID); err != nil || spent != turn.Run.BudgetUSD || spent == 0 {
		t.Errorf("person spent while queued = %v, %v; want the turn's budget %v", spent, err, turn.Run.BudgetUSD)
	}
	if _, err := e.svc.SendMessage(ctx, e.ann, turn.Conversation.ID, ""); err == nil {
		t.Error("empty message accepted")
	}
	bob := *e.ann
	bob.UserID = "99999999-0000-4000-8000-000000000001"
	if _, err := e.svc.SendMessage(ctx, &bob, turn.Conversation.ID, "hi"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("someone else's conversation = %v", err)
	}
	if _, _, err := e.svc.OwnConversation(ctx, e.ann, "99999999-0000-4000-8000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing conversation = %v", err)
	}
	outsider := *e.ann
	outsider.AreaIDs = []string{}
	if _, _, err := e.svc.OwnConversation(ctx, &outsider, turn.Conversation.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("crew outside the person's areas = %v", err)
	}
	if _, err := e.svc.SendMessage(ctx, &auth.Principal{Kind: auth.KindHuman}, turn.Conversation.ID, "hi"); err == nil {
		t.Error("no permission accepted")
	}
}

func TestTurnContextIsCut(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	zero := 0
	for i := range maxTurnNotes + 2 {
		key := store.NoteKey{StateScope: store.StateScope{CrewID: e.crew.ID}, Name: "note-" + string(rune('a'+i))}
		if _, err := e.st.PutNote(ctx, key, strings.Repeat("x", maxNoteChars+5), &zero, store.NoteWriter{Name: "run"}); err != nil {
			t.Fatal(err)
		}
	}
	notes, omitted, err := e.svc.turnNotes(ctx, e.crew.ID)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range notes {
		total += len([]rune(n.Content))
		if !n.Truncated {
			t.Errorf("%s not marked cut", n.Name)
		}
	}
	if len(notes) != 4 || omitted != maxTurnNotes+2-4 || total > maxTotalNoteChars+4*len("\n[… cut]") {
		t.Errorf("notes = %d, omitted %d, total %d", len(notes), omitted, total)
	}
}

func TestDecidingProposals(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.turnOn(t, nil, 1)
	turn, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "start a sweep")
	if err != nil {
		t.Fatal(err)
	}
	scope := e.running(t, turn.Run.ID)
	if _, err := e.svc.Propose(ctx, scope, TypeStartWorkOrder, map[string]any{"brief": ""}); err == nil {
		t.Error("an empty brief was accepted")
	}
	m, err := e.svc.Propose(ctx, scope, TypeStartWorkOrder, map[string]any{"brief": "Sweep", "inputs": map[string]any{"company": "Acme"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Propose(ctx, store.RunScope{RunID: "99999999-0000-4000-8000-000000000000", Kind: model.RunKindChat}, TypeNotesPut, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown run = %v", err)
	}

	// Keys and scripts cannot decide; neither can people without work_orders.create for a run
	e.ann.Via = auth.ViaAPIKey
	var denied *DeniedError
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now()); !errors.As(err, &denied) {
		t.Errorf("via key = %v", err)
	}
	e.ann.Via = auth.ViaSession
	noWO := *e.ann
	noWO.Permissions = map[string]bool{auth.CrewsChat: true}
	var forbidden *launch.ForbiddenError
	if _, err := e.svc.Decide(ctx, &noWO, m.ID, true, time.Now()); !errors.As(err, &forbidden) || forbidden.Permission != auth.WorkOrdersCreate {
		t.Errorf("without work_orders.create = %v", err)
	}
	if _, err := e.svc.Decide(ctx, e.ann, turn.User.ID, true, time.Now()); err == nil {
		t.Error("a message without a proposal was confirmed")
	}
	if _, err := e.svc.Decide(ctx, e.ann, "99999999-0000-4000-8000-000000000000", true, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing message = %v", err)
	}

	// The crew cannot run (missing price for its search tool) so applying fails and the proposal stays pending
	if _, err := e.st.Pool().Exec(ctx, `DELETE FROM model_prices`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now()); err == nil {
		t.Error("an unrunnable work order was started")
	}
	got, _, _ := e.st.GetMessage(ctx, m.ID)
	if *got.ProposalState != model.ProposalPending {
		t.Errorf("state after a failed apply = %v", *got.ProposalState)
	}
	if n, _ := countWorkOrders(ctx, e.st); n != 0 {
		t.Errorf("left %d draft work orders", n)
	}
	if _, err := e.svc.Undo(ctx, e.ann, m.ID); err == nil {
		t.Error("a pending proposal was undone")
	}
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now()); !errors.Is(err, store.ErrProposalDecided) {
		t.Errorf("after decline = %v", err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE conversation_messages SET proposal = jsonb_set(proposal, '{type}', '"gone"') WHERE id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}
	var conflict *store.ConflictError
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now()); !errors.As(err, &conflict) || conflict.Code != "unknown_proposal" {
		t.Errorf("unknown type = %v", err)
	}
}

func countWorkOrders(ctx context.Context, st *store.Store) (int, error) {
	var n int
	err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM work_orders`).Scan(&n)
	return n, err
}

func TestUndoNeedsAResult(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.turnOn(t, nil, 1)
	turn, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "note please")
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.svc.Propose(ctx, e.running(t, turn.Run.ID), TypeNotesPut,
		map[string]any{"name": "risks", "content": "x", "base_version": 0})
	if err != nil {
		t.Fatal(err)
	}
	// Undo needs the same permissions as confirming
	noManage := *e.ann
	noManage.Permissions = map[string]bool{auth.CrewsChat: true, auth.StateView: true}
	var forbidden *launch.ForbiddenError
	if _, err := e.svc.Decide(ctx, &noManage, m.ID, true, time.Now()); !errors.As(err, &forbidden) || forbidden.Permission != auth.StateManage {
		t.Errorf("confirm without state.manage = %v", err)
	}
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Undo(ctx, &noManage, m.ID); !errors.As(err, &forbidden) {
		t.Errorf("undo without state.manage = %v", err)
	}
	// A failed undo (the note changed since) gives the undo back, so it can be tried again
	key := store.NoteKey{StateScope: store.StateScope{CrewID: e.crew.ID}, Name: "risks"}
	if _, err := e.st.PutNote(ctx, key, "changed", nil, store.NoteWriter{Name: "run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Undo(ctx, e.ann, m.ID); err == nil {
		t.Error("undo over a newer version succeeded")
	}
	if got, _, _ := e.st.GetMessage(ctx, m.ID); got.UndoneAt != nil {
		t.Error("a failed undo stayed marked as undone")
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE conversation_messages SET proposal_result = '{"version": 0}' WHERE id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}
	var conflict *store.ConflictError
	if _, err := e.svc.Undo(ctx, e.ann, m.ID); !errors.As(err, &conflict) || conflict.Code != "not_undoable" {
		t.Errorf("bad result = %v", err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE conversation_messages SET undone_at = now() WHERE id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Undo(ctx, e.ann, m.ID); !errors.As(err, &conflict) || conflict.Code != "already_undone" {
		t.Errorf("already undone = %v", err)
	}
}

func TestTurnsGetOnlyWhatThePersonMayUse(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.turnOn(t, nil, 1)
	zero := 0
	if _, err := e.st.PutNote(ctx, store.NoteKey{StateScope: store.StateScope{CrewID: e.crew.ID}, Name: "secret"}, "x", &zero, store.NoteWriter{Name: "run"}); err != nil {
		t.Fatal(err)
	}
	e.ann.Permissions = map[string]bool{auth.CrewsChat: true}
	turn, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "hi")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := e.st.GetRunSpec(ctx, turn.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Chat.Tools) != 0 || len(spec.Agents[0].Tools) != 0 || len(spec.Chat.Notes) != 0 {
		t.Errorf("tools %v, notes %v", spec.Chat.Tools, spec.Chat.Notes)
	}
	var denied *DeniedError
	if _, err := e.svc.Propose(ctx, e.running(t, turn.Run.ID), TypeStartWorkOrder, map[string]any{"brief": "x"}); !errors.As(err, &denied) {
		t.Errorf("proposing without the tool = %v", err)
	}
}

func TestApplyingANoteTwiceIsSafe(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	k, _ := Lookup(TypeNotesPut)
	payload := map[string]any{"name": "risks", "content": "x", "base_version": 0.0}
	req := Request{Principal: e.ann, Crew: e.crew, Payload: payload}
	env := Env{Store: e.st}
	first, err := k.Apply(ctx, env, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := k.Apply(ctx, env, req)
	if err != nil || again["version"] != first["version"] {
		t.Errorf("second apply = %v %v", again, err)
	}
	req.Payload = map[string]any{"name": "risks", "content": "different", "base_version": 0.0}
	if _, err := k.Apply(ctx, env, req); err == nil {
		t.Error("a stale change was applied")
	}
}
