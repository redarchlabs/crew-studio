package store

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"crewstudio/internal/model"
)

// handoffChat is a person's conversation with an agent, ready for hand-offs.
type handoffChat struct {
	agent  model.Agent
	person model.User
	conv   model.Conversation
	area   string
}

func (tc *fixture) handoffChat(name string) handoffChat {
	tc.t.Helper()
	a := tc.area(name + "-area")
	agent := tc.agent(name+"-lead", nil, a)
	person := tc.human(name+"@example.com", model.UserInput{AreaIDs: []string{a}})
	conv := must(tc.s.CreateAgentConversation(bg(), agent.ID, person.ID, "Vendors"))
	return handoffChat{agent: agent, person: person, conv: conv, area: a}
}

// newHandoff is a hand-off of the conversation to its lead with generous limits; tests narrow what they check.
func (h handoffChat) newHandoff(title string) NewHandoff {
	spec := model.RunSpec{Crew: model.Crew{Name: h.agent.Name, AreaIDs: []string{h.area}}, Inputs: map[string]string{}, BudgetUSD: 1,
		Chat: &model.ChatSpec{ConversationID: h.conv.ID, History: []model.ChatTurn{}}}
	return NewHandoff{
		ConversationID: h.conv.ID,
		Handoff: model.ChatHandoffSpec{Title: title, Brief: "Compare the vendors.", WorkerAgentID: h.agent.ID, WorkerName: h.agent.Name,
			WorkerRole: h.agent.Role, TimeoutMinutes: 20},
		Spec: spec, Meta: RunMeta{AreaIDs: []string{h.area}, CreatedBy: h.person.ID, CreatedByName: h.person.Name, ChatAgentID: h.agent.ID},
		HandoffBudgetUSD: 0.5, DailyBudgetUSD: 1, PersonBudgetUSD: 10, MinBudgetUSD: 0.01, MaxActive: 5, MaxPerson: 5,
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// Starting a hand-off stores a queued run of kind handoff, the hand-off and its empty result message, all linked; the
// run's frozen spec carries the hand-off's id and deadline.
func TestCreateHandoffStoresRunHandoffAndResultMessage(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("ann")
	before := time.Now()
	started := must(f.s.CreateHandoff(bg(), h.newHandoff("Vendor comparison")))
	run, hand, msg := started.Run, started.Handoff, started.Message
	if run.Kind != model.RunKindHandoff || run.Status != model.RunQueued || run.BudgetUSD != 0.5 || run.CreatedBy == nil || *run.CreatedBy != h.person.ID {
		t.Errorf("run = %+v", run)
	}
	if hand.RunID == nil || *hand.RunID != run.ID || hand.ConversationID != h.conv.ID || hand.ResultMessageID != msg.ID || hand.Title != "Vendor comparison" ||
		hand.BudgetUSD != 0.5 || hand.RunStatus == nil || *hand.RunStatus != model.RunQueued || hand.WorkerName != h.agent.Name {
		t.Errorf("hand-off = %+v", hand)
	}
	if d := hand.DeadlineAt.Sub(before); d < 19*time.Minute || d > 21*time.Minute {
		t.Errorf("deadline in %v", d)
	}
	if msg.Role != model.ChatRoleAssistant || msg.Content != "" || msg.RunID == nil || *msg.RunID != run.ID || msg.RunKind != model.RunKindHandoff ||
		msg.Handoff == nil || msg.Handoff.ID != hand.ID || msg.Handoff.Title != "Vendor comparison" || msg.Handoff.BudgetUSD != 0.5 {
		t.Errorf("result message = %+v (handoff %+v)", msg, msg.Handoff)
	}
	spec := must(f.s.GetRunSpec(bg(), run.ID))
	if spec.Chat == nil || spec.Chat.Handoff == nil || spec.Chat.Handoff.ID != hand.ID || !spec.Chat.Handoff.DeadlineAt.Equal(hand.DeadlineAt) ||
		spec.BudgetUSD != 0.5 {
		t.Errorf("frozen spec = %+v", spec.Chat)
	}
	if got := must(f.s.GetHandoff(bg(), hand.ID)); got.ID != hand.ID || got.ResultMessageID != msg.ID {
		t.Errorf("get = %+v", got)
	}
	if list := must(f.s.ListHandoffs(bg(), h.conv.ID, 20)); len(list) != 1 || list[0].ID != hand.ID {
		t.Errorf("list = %+v", list)
	}
	if conv := must(f.s.GetConversation(bg(), h.conv.ID)); !conv.UpdatedAt.After(h.conv.UpdatedAt) {
		t.Error("the conversation was not touched")
	}
	_, err := f.s.GetHandoff(bg(), missingID)
	wantNotFound(t, err)
	// An agent's role may be long; the hand-off keeps the first 200 characters of it
	in := h.newHandoff("Long role")
	in.Handoff.WorkerRole = strings.Repeat("r", 300)
	if long := must(f.s.CreateHandoff(bg(), in)); len(long.Handoff.WorkerRole) != 200 {
		t.Errorf("worker role kept %d characters", len(long.Handoff.WorkerRole))
	}
}

// A hand-off reserves its whole budget until it finishes, but never all that is left: MinBudgetUSD stays free for the
// next turn, so the person can keep chatting while it works. Once nothing more is left, the next is refused with the
// conversation's or the person's budget code. Finished, a hand-off counts at what it cost.
func TestCreateHandoffReservesBudget(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("bob")
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
	first := must(f.s.CreateHandoff(bg(), h.newHandoff("one")))
	in := h.newHandoff("two")
	in.HandoffBudgetUSD = 0.8
	second := must(f.s.CreateHandoff(bg(), in))
	if !near(first.Run.BudgetUSD, 0.5) || !near(second.Run.BudgetUSD, 0.49) {
		t.Fatalf("budgets = %v, %v; want 0.5 and what is left less a turn's least (0.49)", first.Run.BudgetUSD, second.Run.BudgetUSD)
	}
	_, err := f.s.CreateHandoff(bg(), h.newHandoff("three"))
	wantCode(t, err, "chat_budget_used")
	if spent := must(f.s.ChatSpent(bg(), h.conv.ID)); !near(spent, 0.99) {
		t.Errorf("conversation spent = %v, want both reserved", spent)
	}
	if spent := must(f.s.PersonAgentChatSpent(bg(), h.agent.ID, h.person.ID)); !near(spent, 0.99) {
		t.Errorf("person spent = %v, want hand-offs counted", spent)
	}
	// What is left is still enough for a turn
	turn, err := f.s.CreateChatTurn(bg(), ChatTurn{ConversationID: h.conv.ID, Content: "Meanwhile?", Spec: h.newHandoff("x").Spec,
		DailyBudgetUSD: 1, PersonBudgetUSD: 10, MinBudgetUSD: 0.01, Meta: RunMeta{CreatedBy: h.person.ID, ChatAgentID: h.agent.ID}})
	if err != nil || !near(turn.Run.BudgetUSD, 0.01) {
		t.Fatalf("turn beside the hand-offs = %+v, %v", turn.Run, err)
	}
	f.finish(turn.Run.ID, model.RunFailed)
	// The person's cap works the same way
	f.exec(`UPDATE runs SET status = 'succeeded', cost_usd = 0.1 WHERE id = $1`, first.Run.ID)
	in = h.newHandoff("four")
	in.PersonBudgetUSD = 0.62
	fourth := must(f.s.CreateHandoff(bg(), in))
	if !near(fourth.Run.BudgetUSD, 0.02) {
		t.Errorf("fourth budget = %v, want what the person has left less a turn's least (0.02)", fourth.Run.BudgetUSD)
	}
	_, err = f.s.CreateHandoff(bg(), in)
	wantCode(t, err, "chat_person_budget_used")
}

// A hand-off without the person who started it or the agent cannot be counted against their caps, so it is refused.
func TestCreateHandoffNeedsItsPersonAndAgent(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("hal")
	for name, change := range map[string]func(*NewHandoff){
		"no person": func(in *NewHandoff) { in.Meta.CreatedBy = "" },
		"no agent":  func(in *NewHandoff) { in.Meta.ChatAgentID = "" },
	} {
		in := h.newHandoff(name)
		change(&in)
		if _, err := f.s.CreateHandoff(bg(), in); err == nil {
			t.Errorf("%s: started", name)
		}
	}
}

// Deleting a hand-off's run (allowed after a day) keeps the hand-off and its result, as a chat reply is kept.
func TestDeletingAHandoffRunKeepsItsResult(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("ida")
	started := must(f.s.CreateHandoff(bg(), h.newHandoff("Kept")))
	out := "The table."
	_, err := f.s.FinishRun(bg(), started.Run.ID, RunResult{Status: model.RunSucceeded, Output: &out})
	mustOK(t, err)
	mustOK(t, f.s.SettleReplies(bg(), h.conv.ID, time.Hour))
	mustOK(t, f.s.DeleteRun(bg(), started.Run.ID))
	got := must(f.s.GetHandoff(bg(), started.Handoff.ID))
	if got.RunID != nil || got.RunStatus != nil || got.ResultMessageID != started.Message.ID {
		t.Errorf("hand-off after deleting its run = %+v", got)
	}
	m, _ := must2(f.s.GetMessage(bg(), started.Message.ID))
	if m.Content != out || m.Handoff == nil || m.Handoff.Title != "Kept" {
		t.Errorf("result after deleting its run = %+v", m)
	}
	if turns := must(f.s.RecentTurns(bg(), h.conv.ID, 20)); len(turns) != 1 || turns[0].Kind != model.ChatTurnHandoffResult {
		t.Errorf("turns = %+v", turns)
	}
}

// A hand-off's time limit counts from when its run starts, not while it waits for a slot.
func TestHandoffDeadlineCountsFromTheStart(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("jo")
	started := must(f.s.CreateHandoff(bg(), h.newHandoff("Queued")))
	f.exec(`UPDATE chat_handoffs SET deadline_at = deadline_at - interval '1 hour' WHERE id = $1`, started.Handoff.ID)
	f.startTurnRun(started.Run.ID)
	got := must(f.s.GetHandoff(bg(), started.Handoff.ID))
	if got.StartedAt == nil || got.DeadlineAt.Sub(*got.StartedAt) != 20*time.Minute || got.TimeoutMinutes != 20 {
		t.Fatalf("deadline %v, started %v", got.DeadlineAt, got.StartedAt)
	}
	spec := must(f.s.GetRunSpec(bg(), started.Run.ID))
	if !spec.Chat.Handoff.DeadlineAt.Equal(got.DeadlineAt) {
		t.Errorf("spec deadline %v, hand-off %v", spec.Chat.Handoff.DeadlineAt, got.DeadlineAt)
	}
	if m, _ := must2(f.s.GetMessage(bg(), started.Message.ID)); m.Handoff == nil || m.Handoff.TimeoutMinutes != 20 || m.Handoff.StartedAt == nil {
		t.Errorf("result message hand-off = %+v", m.Handoff)
	}
	// Other runs start as before
	crew := f.simpleCrew("plain", h.area)
	run := f.run(crew, RunMeta{AreaIDs: crew.AreaIDs})
	f.startTurnRun(run.ID)
}

// At most MaxActive hand-offs run at once in a conversation and MaxPerson per person and agent; two started at once
// cannot both take the last place.
func TestCreateHandoffCapsHowManyRunAtOnce(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("cat")
	in := h.newHandoff("one")
	in.MaxActive = 1
	first := must(f.s.CreateHandoff(bg(), in))
	_, err := f.s.CreateHandoff(bg(), in)
	wantCode(t, err, "handoffs_busy")
	f.finish(first.Run.ID, model.RunSucceeded)
	must(f.s.CreateHandoff(bg(), in))

	// Per person, across conversations with the agent
	other := must(f.s.CreateAgentConversation(bg(), h.agent.ID, h.person.ID, "Other"))
	in2 := h.newHandoff("elsewhere")
	in2.ConversationID, in2.Spec.Chat.ConversationID, in2.MaxPerson = other.ID, other.ID, 1
	_, err = f.s.CreateHandoff(bg(), in2)
	wantCode(t, err, "handoffs_busy")

	// Two at once in a fresh conversation with room for one
	race := must(f.s.CreateAgentConversation(bg(), h.agent.ID, h.person.ID, "Race"))
	in3 := h.newHandoff("race")
	in3.ConversationID, in3.Spec.Chat.ConversationID, in3.MaxActive = race.ID, race.ID, 1
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.s.CreateHandoff(bg(), in3)
		}()
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Errorf("both or neither started: %v / %v", errs[0], errs[1])
	}
}

// A running hand-off does not block the next message, but it does block deleting the conversation.
func TestHandoffDoesNotBlockTurnsButBlocksDelete(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("dan")
	started := must(f.s.CreateHandoff(bg(), h.newHandoff("one")))
	f.startTurnRun(started.Run.ID)
	turn, err := f.s.CreateChatTurn(bg(), ChatTurn{ConversationID: h.conv.ID, Content: "Meanwhile?", Spec: h.newHandoff("x").Spec,
		DailyBudgetUSD: 1, PersonBudgetUSD: 10, MinBudgetUSD: 0.01, Meta: RunMeta{CreatedBy: h.person.ID, ChatAgentID: h.agent.ID}})
	if err != nil {
		t.Fatalf("turn while a hand-off runs: %v", err)
	}
	if math.Abs(turn.Run.BudgetUSD-0.5) > 1e-9 {
		t.Errorf("turn budget = %v, want what the hand-off left", turn.Run.BudgetUSD)
	}
	f.finish(turn.Run.ID, model.RunSucceeded)
	err = f.s.DeleteConversation(bg(), h.conv.ID)
	wantCode(t, err, "handoff_active")
	f.finish(started.Run.ID, model.RunCancelled)
	mustOK(t, f.s.DeleteConversation(bg(), h.conv.ID))
	// The run is kept so budgets still count it
	if run := must(f.s.GetRun(bg(), started.Run.ID)); run.Kind != model.RunKindHandoff {
		t.Errorf("run after delete = %+v", run)
	}
}

// A finished hand-off's output fills its result message, which later turns get labelled as a hand-off's result;
// running hand-offs are listed for the lead instead.
func TestHandoffResultSettlesAndIsLabelled(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("eve")
	done := must(f.s.CreateHandoff(bg(), h.newHandoff("Done one")))
	running := must(f.s.CreateHandoff(bg(), h.newHandoff("Running one")))
	f.startTurnRun(running.Run.ID)
	out := "| vendor | price |"
	_, err := f.s.FinishRun(bg(), done.Run.ID, RunResult{Status: model.RunSucceeded, Output: &out})
	mustOK(t, err)
	mustOK(t, f.s.SettleReplies(bg(), h.conv.ID, time.Hour))
	m, _ := must2(f.s.GetMessage(bg(), done.Message.ID))
	if m.Content != out {
		t.Errorf("result = %q", m.Content)
	}
	turns := must(f.s.RecentTurns(bg(), h.conv.ID, 20))
	if len(turns) != 1 || turns[0].Kind != model.ChatTurnHandoffResult || turns[0].Title != "Done one" || turns[0].Worker != h.agent.Name {
		t.Errorf("turns = %+v", turns)
	}
	pending := must(f.s.PendingHandoffs(bg(), h.conv.ID))
	if len(pending) != 1 || pending[0].Title != "Running one" || pending[0].WorkerName != h.agent.Name {
		t.Errorf("pending = %+v", pending)
	}
}

// Hand-offs are private to their person on the Runs page, like chat turns, and count as chat spend in the overview.
func TestHandoffRunsArePrivateAndCountAsChatSpend(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("fay")
	started := must(f.s.CreateHandoff(bg(), h.newHandoff("one")))
	cost := 0.2
	f.exec(`UPDATE runs SET status = 'succeeded', cost_usd = $2 WHERE id = $1`, started.Run.ID, cost)
	other := f.human("other@example.com", model.UserInput{AreaIDs: []string{h.area}})
	scope := Scope{AreaIDs: []string{h.area}}
	for _, tc := range []struct {
		viewer string
		want   int
	}{{h.person.ID, 1}, {other.ID, 0}} {
		_, total := must2(f.s.ListRuns(bg(), RunFilter{Kind: model.RunKindHandoff, Scope: scope, Viewer: tc.viewer, Limit: 10}))
		if total != tc.want {
			t.Errorf("viewer %s sees %d hand-offs, want %d", tc.viewer, total, tc.want)
		}
	}
	o := must(f.s.Overview(bg(), scope))
	if math.Abs(o.ChatSpendLast30dUSD-cost) > 1e-9 {
		t.Errorf("chat spend = %v", o.ChatSpendLast30dUSD)
	}
}

// An agent's hand-off settings are saved with its other chat settings.
func TestAgentChatHandoffSettingsAreSaved(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	agent := f.agent("lead", nil, a)
	if d := must(f.s.AgentChatSettings(bg(), agent.ID)); d.HandoffsEnabled || d.HandoffBudgetUSD != model.DefaultHandoffBudgetUSD {
		t.Errorf("default = %+v", d)
	}
	in := model.AgentChatSettings{AgentID: agent.ID, Enabled: true, DailyBudgetUSD: 1, PersonDailyBudgetUSD: 2,
		HandoffsEnabled: true, HandoffBudgetUSD: 0.75, HandoffTimeoutMinutes: 45, MaxActiveHandoffs: 3}
	got := must(f.s.PutAgentChatSettings(bg(), in, ""))
	if !got.HandoffsEnabled || got.HandoffBudgetUSD != 0.75 || got.HandoffTimeoutMinutes != 45 || got.MaxActiveHandoffs != 3 {
		t.Errorf("saved = %+v", got)
	}
	if strings.TrimSpace(got.AgentID) != agent.ID {
		t.Errorf("agent = %q", got.AgentID)
	}
}

// An agent cannot be deleted while a hand-off with it (as the lead or the worker) is queued or running.
func TestAgentWithARunningHandoffCannotBeDeleted(t *testing.T) {
	f := newFixture(t)
	h := f.handoffChat("gus")
	worker := f.agent("gus-worker", nil, h.area)
	in := h.newHandoff("Busy")
	in.Handoff.WorkerAgentID = worker.ID
	started := must(f.s.CreateHandoff(bg(), in))
	wantConflict(t, f.s.DeleteAgent(bg(), h.agent.ID), `hand-off "Busy"`)
	wantConflict(t, f.s.DeleteAgent(bg(), worker.ID), `hand-off "Busy"`)
	f.finish(started.Run.ID, model.RunSucceeded)
	mustOK(t, f.s.DeleteAgent(bg(), worker.ID))
}
