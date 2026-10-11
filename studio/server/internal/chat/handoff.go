package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

// Hand-offs (docs/agent-chat-handoffs-design.md, slice 3b): in agent chat the lead may propose handing a piece of work
// to a background run, worked by itself or one of its specialists. The proposal is a card like any other; confirming
// it (StartHandoff) starts a run of kind "handoff" whose result fills a message in the conversation. A hand-off cannot
// start another: it only reads, and anything it wants changed is its own card.

const (
	// TypeStartHandoff is the proposal that starts a hand-off; StartHandoffTool is the lead's tool that makes it
	TypeStartHandoff = "start_handoff"
	StartHandoffTool = "start_handoff"
	// maxHandoffIter bounds each agent's steps in a hand-off: nobody is waiting, but the run must still end. Keep in
	// step with MAX_HANDOFF_ITER in studio_runner/handoff.py.
	maxHandoffIter = 25
	// maxPersonHandoffs caps one person's hand-offs with one agent running at once, over all their conversations
	maxPersonHandoffs = 5
)

var (
	errHandoffsOff = &store.ConflictError{Code: "handoffs_off",
		Message: "Hand-offs have been turned off for this agent, so this card can no longer be confirmed."}
	errWorkerNotOffered = &store.ConflictError{Code: "worker_not_offered",
		Message: "The agent may no longer hand work to this specialist; ask the agent again."}
	errChatOff = &store.ConflictError{Code: "chat_off",
		Message: "Chat with this agent has been turned off, so its proposals can no longer be confirmed."}
	// The reply that proposed the hand-off still reserves its budget; confirming waits until it is written
	errReplyInFlight = &store.ConflictError{Code: "chat_turn_active",
		Message: "The reply is still being written; confirm the hand-off once it has finished."}
	errNoChains = &DeniedError{Code: "no_chains", Message: "A hand-off cannot start another hand-off."}
)

// startHandoff is a stored hand-off proposal: the worker as the turn resolved it (see proposedWorker), and the work.
type startHandoff struct {
	WorkerAgentID string `json:"worker_agent_id"`
	WorkerName    string `json:"worker_name"`
	WorkerRole    string `json:"worker_role"`
	Title         string `json:"title"`
	Brief         string `json:"brief"`
}

// storedHandoff is a hand-off proposal as stored: what the turn proposed, and the agent's limits when it proposed it,
// so the card can say "up to $0.50, up to 20 minutes". Confirming uses the limits as they are then.
type storedHandoff struct {
	startHandoff
	BudgetUSD      float64 `json:"budget_usd"`
	TimeoutMinutes int     `json:"timeout_minutes"`
}

// withHandoffLimits adds the agent's current hand-off limits to a cleaned hand-off proposal, for the card.
func (s *Service) withHandoffLimits(ctx context.Context, spec model.RunSpec, clean map[string]any) (map[string]any, error) {
	settings, err := s.store.AgentChatSettings(ctx, spec.Chat.Agent.AgentID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(clean)+2)
	for k, v := range clean {
		out[k] = v
	}
	out["budget_usd"], out["timeout_minutes"] = settings.HandoffBudgetUSD, settings.HandoffTimeoutMinutes
	return out, nil
}

func startHandoffKind() Kind {
	return Kind{Type: TypeStartHandoff, Normalize: normalizeStartHandoff, Apply: applyStartHandoff}
}

func normalizeStartHandoff(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[startHandoff](payload)
	if err != nil {
		return nil, "", err
	}
	out := startHandoff{WorkerAgentID: strings.TrimSpace(in.WorkerAgentID), WorkerName: oneLine(in.WorkerName),
		WorkerRole: oneLine(in.WorkerRole), Title: oneLine(in.Title), Brief: strings.TrimSpace(in.Brief)}
	var problems []string
	if !uuidPattern.MatchString(out.WorkerAgentID) {
		problems = append(problems, "worker_agent_id: must be the worker's id")
	}
	switch n := len([]rune(out.Brief)); {
	case n == 0:
		problems = append(problems, "brief: say what the hand-off should do")
	case n > maxProposedBriefLen:
		problems = append(problems, fmt.Sprintf("brief: must be at most %d characters", maxProposedBriefLen))
	}
	if out.Title == "" {
		out.Title = defaultTitle(out.Brief)
	}
	if len([]rune(out.Title)) > maxProposedTitleLen {
		problems = append(problems, fmt.Sprintf("title: must be at most %d characters", maxProposedTitleLen))
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, "", &model.ValidationError{Details: problems}
	}
	clean, err := asMap(out)
	return clean, fmt.Sprintf("Hand off to %s: %s", out.WorkerName, out.Title), err
}

// proposedWorker settles who works on a proposed hand-off: the lead or one of the turn's specialists, named in
// "worker" by role (as the lead names coworkers), by name or by id. The payload returned carries the worker's id,
// name and role from the frozen turn instead.
func proposedWorker(spec model.RunSpec, payload map[string]any) (map[string]any, error) {
	for _, key := range []string{"worker_agent_id", "worker_name", "worker_role"} {
		if _, sent := payload[key]; sent {
			return nil, &model.ValidationError{Details: []string{"worker: name the worker in worker"}}
		}
	}
	want, _ := payload["worker"].(string)
	want = strings.TrimSpace(want)
	candidates := handoffCandidates(spec)
	roles := make([]string, 0, len(candidates))
	for _, c := range candidates {
		roles = append(roles, c.Role)
		if want != "" && (c.AgentID == want || model.ChatRoleKey(c.Role) == model.ChatRoleKey(want) || strings.EqualFold(c.Name, want)) {
			out := make(map[string]any, len(payload)+2)
			for k, v := range payload {
				if k != "worker" {
					out[k] = v
				}
			}
			out["worker_agent_id"], out["worker_name"], out["worker_role"] = c.AgentID, c.Name, c.Role
			return out, nil
		}
	}
	return nil, &model.ValidationError{Details: []string{"worker: must be one of " + strings.Join(roles, ", ")}}
}

// handoffCandidates are who may work on a hand-off of this turn: the lead, then its specialists.
func handoffCandidates(spec model.RunSpec) []model.ChatSpecialist {
	if len(spec.Agents) == 0 || spec.Chat == nil || spec.Chat.Agent == nil {
		return nil
	}
	lead := spec.Agents[0]
	return append([]model.ChatSpecialist{{AgentID: lead.ID, Name: lead.Name, Role: lead.Role}}, spec.Chat.Agent.Specialists...)
}

func applyStartHandoff(ctx context.Context, env Env, req Request) (map[string]any, error) {
	if env.Chat == nil {
		return nil, errors.New("chat: starting a hand-off needs the chat service")
	}
	return env.Chat.StartHandoff(ctx, req)
}

// StartHandoff starts the hand-off a confirmed card proposed, as the person who confirmed it: it checks the agent's
// settings again, freezes a fresh turn of the agent (so a specialist taken off the list since is gone) with the
// conversation so far, and stores the run, the hand-off and its empty result message within the hand-off's budget and
// limits. Its result says where the hand-off is.
func (s *Service) StartHandoff(ctx context.Context, req Request) (map[string]any, error) {
	in, err := decodePayload[storedHandoff](req.Payload)
	if err != nil {
		return nil, err
	}
	conv, p := req.Conversation, req.Principal
	if conv.AgentID == nil {
		return nil, errHandoffsOff
	}
	if err := s.checkReplyWritten(ctx, req.MessageID); err != nil {
		return nil, err
	}
	settings, err := s.store.AgentChatSettings(ctx, *conv.AgentID)
	switch {
	case err != nil:
		return nil, err
	case !settings.Enabled:
		return nil, errChatOff
	case !settings.HandoffsEnabled:
		return nil, errHandoffsOff
	}
	base, budgets, err := s.agentBase(ctx, p, *conv.AgentID)
	if err != nil {
		return nil, err
	}
	worker, ok := handoffWorker(base, in.WorkerAgentID)
	if !ok {
		return nil, errWorkerNotOffered
	}
	lib, err := s.launcher.Library(ctx)
	if err != nil {
		return nil, err
	}
	spec, err := s.withContext(ctx, handoffSpec(base, lib), conv.ID, p, "")
	if err != nil {
		return nil, err
	}
	// The lead may bring a specialist's question to the person only in a hand-off, and only as the agent's question
	// settings say now (slice 3c); a chat turn never freezes an escalation
	spec.Escalation = settings.QuestionEscalation()
	spec.Chat.Handoff = &model.ChatHandoffSpec{Title: in.Title, Brief: in.Brief, WorkerAgentID: worker.AgentID, WorkerName: worker.Name,
		WorkerRole: worker.Role, TimeoutMinutes: settings.HandoffTimeoutMinutes, RequestMessageID: req.MessageID}
	started, err := s.store.CreateHandoff(ctx, store.NewHandoff{
		ConversationID: conv.ID, Handoff: *spec.Chat.Handoff, Spec: spec,
		Meta: store.RunMeta{AreaIDs: base.Crew.AreaIDs, CreatedBy: p.UserID, CreatedByName: p.Name, APIKeyID: p.APIKeyID, TriggerType: "manual",
			ChatAgentID: *conv.AgentID},
		HandoffBudgetUSD: settings.HandoffBudgetUSD, DailyBudgetUSD: budgets.DailyBudgetUSD, PersonBudgetUSD: budgets.PersonDailyBudgetUSD,
		MinBudgetUSD: MinTurnBudgetUSD, MaxActive: settings.MaxActiveHandoffs, MaxPerson: maxPersonHandoffs,
	})
	if err != nil {
		return nil, err
	}
	s.runs.Notify()
	return map[string]any{"handoff_id": started.Handoff.ID, "run_id": started.Run.ID, "message_id": started.Message.ID}, nil
}

// checkReplyWritten refuses while the reply that carries the card is still being written: its run counts at its whole
// budget until it ends, so the hand-off would be refused for a budget that is only reserved.
func (s *Service) checkReplyWritten(ctx context.Context, messageID string) error {
	m, _, err := s.store.GetMessage(ctx, messageID)
	if err != nil {
		return err
	}
	if m.RunStatus != nil && !m.RunStatus.Terminal() {
		return errReplyInFlight
	}
	return nil
}

// handoffWorker finds the worker in a freshly frozen turn: the lead, or a specialist still on its list and visible to
// the person.
func handoffWorker(base model.RunSpec, workerID string) (model.ChatSpecialist, bool) {
	for _, c := range handoffCandidates(base) {
		if c.AgentID == workerID {
			return c, true
		}
	}
	return model.ChatSpecialist{}, false
}

// handoffSpec is a hand-off's run made from a frozen turn of the agent: the same agents, each with its own steps up
// to maxHandoffIter (nobody waits on a hand-off), and the lead without Hand off work, so a hand-off cannot start
// another. The agents passed in are left as they are.
func handoffSpec(base model.RunSpec, lib runspec.Library) model.RunSpec {
	spec := base
	spec.Agents = make([]model.Agent, 0, len(base.Agents))
	for i, a := range base.Agents {
		own := a.MaxIter
		if lib, ok := lib.Agents[a.ID]; ok {
			own = lib.MaxIter
		}
		a.MaxIter = min(max(own, 1), maxHandoffIter)
		if i == 0 {
			a.Tools = slices.DeleteFunc(slices.Clone(a.Tools), func(key string) bool { return key == StartHandoffTool })
		}
		spec.Agents = append(spec.Agents, a)
	}
	return spec
}

// OwnHandoff loads a hand-off of the caller's own conversation, with an agent they can still see; anything else is
// not found.
func (s *Service) OwnHandoff(ctx context.Context, p *auth.Principal, id string) (model.Handoff, error) {
	if err := checkChatter(p); err != nil {
		return model.Handoff{}, err
	}
	h, err := s.store.GetHandoff(ctx, id)
	if err != nil {
		return h, err
	}
	if _, _, err := s.OwnConversation(ctx, p, h.ConversationID); err != nil {
		return model.Handoff{}, err
	}
	return h, nil
}

// pendingHandoffs are the conversation's hand-offs still running, for a turn of an agent conversation to tell the lead.
func (s *Service) pendingHandoffs(ctx context.Context, conv model.Conversation) ([]model.ChatHandoffRef, error) {
	if conv.AgentID == nil {
		return nil, nil
	}
	refs, err := s.store.PendingHandoffs(ctx, conv.ID)
	if len(refs) == 0 {
		return nil, err
	}
	return refs, err
}
