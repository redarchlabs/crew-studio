package chat

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

func TestNormalizeStartHandoff(t *testing.T) {
	worker := map[string]any{"worker_agent_id": analyst, "worker_name": "Analyst", "worker_role": "Financial analyst"}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range worker {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	clean, summary, err := normalizeStartHandoff(with(map[string]any{"brief": "  Compare Acme, Birch and Cedar on price and SSO.  "}))
	if err != nil {
		t.Fatal(err)
	}
	if clean["brief"] != "Compare Acme, Birch and Cedar on price and SSO." || clean["title"] != "Compare Acme, Birch and Cedar on price and SSO." ||
		clean["worker_agent_id"] != analyst {
		t.Errorf("clean = %v", clean)
	}
	if summary != "Hand off to Analyst: Compare Acme, Birch and Cedar on price and SSO." {
		t.Errorf("summary = %q", summary)
	}
	cases := []struct {
		name    string
		payload map[string]any
		problem string
	}{
		{"no brief", with(map[string]any{"brief": " "}), "brief: say what the hand-off should do"},
		{"long brief", with(map[string]any{"brief": strings.Repeat("x", maxProposedBriefLen+1)}), "brief: must be at most 10000 characters"},
		{"long title", with(map[string]any{"brief": "b", "title": strings.Repeat("t", maxProposedTitleLen+1)}), "title: must be at most 200 characters"},
		{"no worker", map[string]any{"brief": "b"}, "worker_agent_id: must be the worker's id"},
		{`payload: json: unknown field "budget_usd"`, with(map[string]any{"brief": "b", "budget_usd": 5}), `payload: json: unknown field "budget_usd"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := normalizeStartHandoff(tc.payload)
			wantInvalid(t, err, tc.problem)
		})
	}
}

// The lead names its worker by role (as it names coworkers), by name or by id; the stored payload carries the
// worker's id, name and role from the frozen turn. Anyone not in the turn is refused, naming who may work.
func TestProposedWorker(t *testing.T) {
	spec := model.RunSpec{
		Agents: []model.Agent{{ID: dealPartner, Name: "Deal Partner", Role: "Deal partner"}, {ID: analyst, Name: "Analyst", Role: "Financial analyst"}},
		Chat: &model.ChatSpec{Agent: &model.ChatAgentSpec{AgentID: dealPartner,
			Specialists: []model.ChatSpecialist{{AgentID: analyst, Name: "Analyst", Role: "Financial analyst"}}}},
	}
	cases := []struct {
		name   string
		worker any
		want   string
	}{
		{"specialist by role", ` "financial  ANALYST" `, analyst},
		{"specialist by name", "analyst", analyst},
		{"specialist by id", analyst, analyst},
		{"the lead by role", "Deal partner", dealPartner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := proposedWorker(spec, map[string]any{"worker": tc.worker, "brief": "b"})
			if err != nil {
				t.Fatal(err)
			}
			if got["worker_agent_id"] != tc.want || got["worker"] != nil || got["brief"] != "b" {
				t.Errorf("payload = %v", got)
			}
		})
	}
	_, err := proposedWorker(spec, map[string]any{"worker": "Somebody", "brief": "b"})
	wantInvalid(t, err, "worker: must be one of Deal partner, Financial analyst")
	_, err = proposedWorker(spec, map[string]any{"worker": "Analyst", "worker_agent_id": analyst, "brief": "b"})
	wantInvalid(t, err, "worker: name the worker in worker")
}

// handoffsOn turns chat and hand-offs on for the deal partner with the analyst as its specialist.
func (e env) handoffsOn(t *testing.T, change func(*model.AgentChatSettings)) {
	t.Helper()
	s := model.AgentChatSettings{AgentID: dealPartner, Enabled: true, SpecialistIDs: []string{analyst}, DailyBudgetUSD: 1,
		PersonDailyBudgetUSD: 5, HandoffsEnabled: true, HandoffBudgetUSD: 0.4, HandoffTimeoutMinutes: 15, MaxActiveHandoffs: 2}
	if change != nil {
		change(&s)
	}
	if _, err := e.st.PutAgentChatSettings(context.Background(), s, ""); err != nil {
		t.Fatal(err)
	}
}

// proposeHandoff starts a conversation whose turn proposes handing work to the analyst, and lets the turn finish.
func (e env) proposeHandoff(t *testing.T) (Turn, model.ChatMessage) {
	t.Helper()
	ctx := context.Background()
	turn, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "Compare the vendors")
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.svc.Propose(ctx, e.running(t, turn.Run.ID), TypeStartHandoff,
		map[string]any{"worker": "Financial analyst", "title": "Vendor comparison", "brief": "Compare Acme, Birch and Cedar."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'succeeded', output = 'Handing it to the analyst.', cost_usd = 0.01 WHERE id = $1`,
		turn.Run.ID); err != nil {
		t.Fatal(err)
	}
	return turn, m
}

func wantConflictCode(t *testing.T, err error, code string) {
	t.Helper()
	var conflict *store.ConflictError
	if !errors.As(err, &conflict) || conflict.Code != code {
		t.Errorf("err = %v, want %s", err, code)
	}
}

// With hand-offs on, the lead of a turn gets Hand off work; confirming its card starts a hand-off run worked by the
// named specialist with the conversation so far, and the result message waits for its output.
func TestStartingAHandoffFromItsCard(t *testing.T) {
	e := agentEnv(t)
	ctx := context.Background()
	e.analystRole(t)
	e.handoffsOn(t, nil)
	turn, m := e.proposeHandoff(t)
	spec, _ := e.st.GetRunSpec(ctx, turn.Run.ID)
	if !slices.Contains(spec.Chat.Tools, StartHandoffTool) {
		t.Errorf("turn tools = %v", spec.Chat.Tools)
	}
	if m.Proposal.Summary != "Hand off to Analyst: Vendor comparison" || m.Proposal.Payload["worker_agent_id"] != analyst ||
		m.Proposal.Payload["budget_usd"] != 0.4 || m.Proposal.Payload["timeout_minutes"] != float64(15) {
		t.Errorf("proposal = %+v", m.Proposal)
	}
	before := e.runs.n.Load()
	done, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if e.runs.n.Load() <= before {
		t.Error("the dispatcher was not woken")
	}
	handoffID, _ := done.ProposalResult["handoff_id"].(string)
	runID, _ := done.ProposalResult["run_id"].(string)
	messageID, _ := done.ProposalResult["message_id"].(string)
	h, err := e.st.GetHandoff(ctx, handoffID)
	if err != nil || h.RunID == nil || *h.RunID != runID || h.ResultMessageID != messageID || h.WorkerName != "Analyst" || h.Title != "Vendor comparison" ||
		h.RequestMessageID == nil || *h.RequestMessageID != m.ID || h.BudgetUSD != 0.4 {
		t.Fatalf("hand-off = %+v, %v", h, err)
	}
	hspec, _ := e.st.GetRunSpec(ctx, runID)
	c := hspec.Chat
	if c == nil || c.Handoff == nil || c.Handoff.WorkerAgentID != analyst || c.Handoff.Brief != "Compare Acme, Birch and Cedar." ||
		c.Handoff.TimeoutMinutes != 15 || c.Message != "" || c.Agent == nil || slices.Contains(c.Tools, StartHandoffTool) {
		t.Errorf("hand-off chat spec = %+v", c)
	}
	if len(c.History) != 2 || c.History[1].Content != "Handing it to the analyst." {
		t.Errorf("history = %+v", c.History)
	}
	// Nobody waits on a hand-off: the agents get their own steps up to the hand-off cap
	if len(hspec.Agents) != 2 || hspec.Agents[1].MaxIter <= maxSpecialistIter || hspec.Agents[0].MaxIter > maxHandoffIter ||
		slices.Contains(hspec.Agents[0].Tools, StartHandoffTool) {
		t.Errorf("agents = %+v", hspec.Agents)
	}

	// A hand-off cannot start another (no chains)
	scope := store.RunScope{RunID: runID, Kind: model.RunKindHandoff}
	if ok, err := e.st.StartRun(ctx, runID); err != nil || !ok {
		t.Fatal(err)
	}
	_, err = e.svc.Propose(ctx, scope, TypeStartHandoff, map[string]any{"worker": "Analyst", "brief": "again"})
	var denied *DeniedError
	if !errors.As(err, &denied) || denied.Code != "no_chains" {
		t.Errorf("hand-off proposing a hand-off = %v", err)
	}

	// While it runs, the next turn tells the lead about it and is not blocked
	next, err := e.svc.SendMessage(ctx, e.ann, turn.Conversation.ID, "Anything else?")
	if err != nil {
		t.Fatal(err)
	}
	nspec, _ := e.st.GetRunSpec(ctx, next.Run.ID)
	if len(nspec.Chat.PendingHandoffs) != 1 || nspec.Chat.PendingHandoffs[0].Title != "Vendor comparison" {
		t.Errorf("pending = %+v", nspec.Chat.PendingHandoffs)
	}
	// It belongs to the person: someone else cannot see it
	if _, err := e.svc.OwnHandoff(ctx, e.ann, handoffID); err != nil {
		t.Errorf("owner = %v", err)
	}
	bob := *e.ann
	bob.UserID = "99999999-0000-4000-8000-000000000001"
	if _, err := e.svc.OwnHandoff(ctx, &bob, handoffID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("someone else = %v", err)
	}
	// Compacting waits for running hand-offs, so a result that lands later is not left behind the summary
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'succeeded', output = 'ok' WHERE id = $1`, next.Run.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Compact(ctx, e.ann, turn.Conversation.ID)
	wantConflictCode(t, err, "handoff_active")
}

// analystRole gives the analyst a role of its own (the seeded agents share none with the deal partner).
func (e env) analystRole(t *testing.T) {
	t.Helper()
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE agents SET role = 'Financial analyst', name = 'Analyst' WHERE id = $1`, analyst); err != nil {
		t.Fatal(err)
	}
}

// Confirming is refused when the agent's settings changed since the card, or the hand-off does not fit its limits; the
// card then stays pending.
func TestStartHandoffRefusals(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, e env, turn Turn)
		code   string
	}{
		{"reply still being written", func(t *testing.T, e env, turn Turn) {
			if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET status = 'running' WHERE id = $1`, turn.Run.ID); err != nil {
				t.Fatal(err)
			}
		}, "chat_turn_active"},
		{"chat off", func(t *testing.T, e env, _ Turn) {
			e.handoffsOn(t, func(s *model.AgentChatSettings) { s.Enabled = false })
		}, "chat_off"},
		{"hand-offs off", func(t *testing.T, e env, _ Turn) {
			e.handoffsOn(t, func(s *model.AgentChatSettings) { s.HandoffsEnabled = false })
		}, "handoffs_off"},
		{"worker gone", func(t *testing.T, e env, _ Turn) {
			e.handoffsOn(t, func(s *model.AgentChatSettings) { s.SpecialistIDs = nil })
		}, "worker_not_offered"},
		{"budget used", func(t *testing.T, e env, _ Turn) {
			e.handoffsOn(t, func(s *model.AgentChatSettings) { s.DailyBudgetUSD = 0.015 })
		}, "chat_budget_used"},
		{"busy", func(t *testing.T, e env, turn Turn) {
			e.handoffsOn(t, func(s *model.AgentChatSettings) { s.MaxActiveHandoffs = 1 })
			other := e.secondProposal(t, turn.Conversation.ID)
			if _, err := e.svc.Decide(context.Background(), e.ann, other.ID, true, time.Now()); err != nil {
				t.Fatal(err)
			}
		}, "handoffs_busy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := agentEnv(t)
			e.analystRole(t)
			e.handoffsOn(t, nil)
			turn, m := e.proposeHandoff(t)
			tc.change(t, e, turn)
			_, err := e.svc.Decide(context.Background(), e.ann, m.ID, true, time.Now())
			wantConflictCode(t, err, tc.code)
			if again, _, _ := e.st.GetMessage(context.Background(), m.ID); again.ProposalState == nil || *again.ProposalState != model.ProposalPending {
				t.Errorf("card after refusal = %v", again.ProposalState)
			}
		})
	}
}

// secondProposal is a later turn of the conversation that proposed another hand-off to the analyst.
func (e env) secondProposal(t *testing.T, conversationID string) model.ChatMessage {
	t.Helper()
	ctx := context.Background()
	turn, err := e.svc.SendMessage(ctx, e.ann, conversationID, "And the second part")
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.svc.Propose(ctx, e.running(t, turn.Run.ID), TypeStartHandoff, map[string]any{"worker": "Analyst", "brief": "Part two."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'succeeded', output = 'Handing that on too.' WHERE id = $1`, turn.Run.ID); err != nil {
		t.Fatal(err)
	}
	return m
}

// Without hand-offs on, the lead is not given the tool, so its turns cannot propose one.
func TestHandoffToolOnlyWhenEnabled(t *testing.T) {
	e := agentEnv(t)
	ctx := context.Background()
	e.analystRole(t)
	e.handoffsOn(t, func(s *model.AgentChatSettings) { s.HandoffsEnabled = false })
	turn, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "hi")
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := e.st.GetRunSpec(ctx, turn.Run.ID)
	if slices.Contains(spec.Chat.Tools, StartHandoffTool) {
		t.Errorf("tools = %v", spec.Chat.Tools)
	}
	_, err = e.svc.Propose(ctx, e.running(t, turn.Run.ID), TypeStartHandoff, map[string]any{"worker": "Analyst", "brief": "b"})
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Errorf("propose without the tool = %v", err)
	}
}

// With the agent's questions on, a hand-off freezes them as its escalation, so its lead desk may ask the person; a chat
// turn never does. Its specialists may ask the lead either way.
func TestAHandoffFreezesTheAgentsQuestionSettings(t *testing.T) {
	e := agentEnv(t)
	ctx := context.Background()
	e.analystRole(t)
	start := func() (model.RunSpec, model.RunSpec) {
		t.Helper()
		turn, m := e.proposeHandoff(t)
		done, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		runID, _ := done.ProposalResult["run_id"].(string)
		tspec, _ := e.st.GetRunSpec(ctx, turn.Run.ID)
		hspec, _ := e.st.GetRunSpec(ctx, runID)
		// Cancel it, so the next one fits the per-person cap
		if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'cancelled' WHERE id = $1`, runID); err != nil {
			t.Fatal(err)
		}
		return tspec, hspec
	}
	e.handoffsOn(t, nil)
	tspec, hspec := start()
	if tspec.Escalation != nil || hspec.Escalation != nil {
		t.Errorf("questions off: turn %+v, hand-off %+v", tspec.Escalation, hspec.Escalation)
	}
	if len(hspec.Agents) != 2 || !slices.Contains(hspec.Agents[1].Tools, AskLeadTool) || slices.Contains(hspec.Agents[0].Tools, AskLeadTool) {
		t.Errorf("agents = %+v", hspec.Agents)
	}
	e.handoffsOn(t, func(s *model.AgentChatSettings) {
		s.QuestionsEnabled, s.QuestionRules, s.QuestionTimeoutMinutes, s.QuestionOnTimeout, s.MaxQuestions = true, "Ask about prices", 9, model.EscalationFail, 2
	})
	tspec, hspec = start()
	if tspec.Escalation != nil {
		t.Errorf("a chat turn froze an escalation: %+v", tspec.Escalation)
	}
	want := model.EscalationSpec{Rules: "Ask about prices", TimeoutMinutes: 9, OnTimeout: model.EscalationFail, MaxQuestions: 2}
	if hspec.Escalation == nil || *hspec.Escalation != want {
		t.Errorf("hand-off escalation = %+v", hspec.Escalation)
	}
}
