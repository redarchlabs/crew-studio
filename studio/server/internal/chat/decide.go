package chat

import (
	"context"
	"errors"
	"slices"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// ProposalTTL is how long a proposal waits for the person; after that it expires and must be asked for again.
const ProposalTTL = 24 * time.Hour

var errNotChat = &DeniedError{Message: "Only chat turns can propose changes; scheduled and work order runs cannot."}

// Propose attaches a proposal made by a chat run's front desk to the reply that run is writing (see ProposeAs).
func (s *Service) Propose(ctx context.Context, scope store.RunScope, typ string, payload map[string]any) (model.ChatMessage, error) {
	return s.ProposeAs(ctx, scope, "", typ, payload)
}

// ProposeAs attaches a proposal made by an agent of a chat run to the reply that run is writing; a hand-off's proposals
// are messages of their own (store.AttachProposal). The run token's scope says which run is calling; only chat runs
// some of whose agents' tools offer the proposal type may propose it, and a hand-off never starts another. agentRole
// is the role of the agent calling, as the runner sends it, to say on the card who proposed a change.
func (s *Service) ProposeAs(ctx context.Context, scope store.RunScope, agentRole, typ string, payload map[string]any) (model.ChatMessage, error) {
	if !model.IsChatKind(scope.Kind) {
		return model.ChatMessage{}, errNotChat
	}
	if typ == TypeStartHandoff && scope.Kind == model.RunKindHandoff {
		return model.ChatMessage{}, errNoChains
	}
	kind, ok := Lookup(typ)
	if !ok {
		return model.ChatMessage{}, &model.ValidationError{Details: []string{"type: unknown proposal type"}}
	}
	spec, err := s.store.GetRunSpec(ctx, scope.RunID)
	if err != nil {
		return model.ChatMessage{}, err
	}
	if spec.Chat == nil || !Offers(turnTools(spec), typ) {
		return model.ChatMessage{}, &DeniedError{Message: "This chat turn cannot propose that kind of change."}
	}
	switch typ {
	case TypeStartWorkOrder:
		payload, err = proposedCrew(spec.Chat, payload)
	case TypeStartHandoff:
		payload, err = proposedWorker(spec, payload)
	case TypeTrackerComment, TypeTrackerSuggestion, TypeTrackerIssue, TypeKM2Save:
		payload, err = s.proposedChange(ctx, spec, agentRole, typ, payload)
	}
	if err != nil {
		return model.ChatMessage{}, err
	}
	clean, summary, err := kind.Normalize(payload)
	if err == nil && typ == TypeStartWorkOrder {
		err = s.checkProposedInputs(ctx, scope, clean)
	}
	if err == nil && typ == TypeStartHandoff {
		clean, err = s.withHandoffLimits(ctx, spec, clean)
	}
	if err != nil {
		return model.ChatMessage{}, err
	}
	return s.store.AttachProposal(ctx, scope.RunID, model.Proposal{Type: typ, Summary: summary, Payload: clean})
}

// turnTools are every tool a chat run's agents were given: the front desk's or lead's, and each specialist's, since
// specialists propose through the same run (slice 3d).
func turnTools(spec model.RunSpec) []string {
	tools := slices.Clone(spec.Chat.Tools)
	for _, a := range spec.Agents {
		tools = append(tools, a.Tools...)
	}
	return tools
}

// decision is a proposal the caller may decide on: theirs, in a conversation with a crew they can see, made in
// Crew Studio itself.
type decision struct {
	message model.ChatMessage
	conv    model.Conversation
	crew    model.Crew
	kind    Kind
}

func (s *Service) loadDecision(ctx context.Context, p *auth.Principal, messageID string) (decision, error) {
	if err := checkChatter(p); err != nil {
		return decision{}, err
	}
	// A key or a script acting for someone must not be able to confirm what the AI proposed
	if p.Via != auth.ViaSession {
		return decision{}, &DeniedError{Message: "Proposals can only be confirmed, declined or undone by signing in to Crew Studio."}
	}
	m, conv, err := s.store.GetMessage(ctx, messageID)
	if err != nil {
		return decision{}, err
	}
	if conv.UserID != p.UserID {
		return decision{}, store.ErrNotFound
	}
	crew, err := s.decisionCrew(ctx, p, conv)
	if err != nil {
		return decision{}, err
	}
	if m.Proposal == nil || m.ProposalState == nil {
		return decision{}, &store.ConflictError{Code: "no_proposal", Message: "This message has no proposal."}
	}
	kind, ok := Lookup(m.Proposal.Type)
	if !ok {
		return decision{}, &store.ConflictError{Code: "unknown_proposal", Message: "This kind of proposal can no longer be applied."}
	}
	return decision{message: m, conv: conv, crew: crew, kind: kind}, nil
}

// decisionCrew is the crew of a conversation with a crew; for a conversation with an agent it checks the agent is
// still visible and returns no crew (a proposal there names its crew itself).
func (s *Service) decisionCrew(ctx context.Context, p *auth.Principal, conv model.Conversation) (model.Crew, error) {
	switch {
	case conv.AgentID != nil:
		_, err := s.visibleAgent(ctx, p, *conv.AgentID)
		return model.Crew{}, err
	case conv.CrewID != nil:
		return s.visibleCrew(ctx, p, *conv.CrewID)
	}
	return model.Crew{}, store.ErrNotFound
}

// Decide confirms (applies) or declines a pending proposal on the caller's own conversation.
func (s *Service) Decide(ctx context.Context, p *auth.Principal, messageID string, confirm bool, now time.Time) (model.ChatMessage, error) {
	d, err := s.loadDecision(ctx, p, messageID)
	if err != nil {
		return model.ChatMessage{}, err
	}
	if *d.message.ProposalState != model.ProposalPending {
		return model.ChatMessage{}, store.ErrProposalDecided
	}
	if d.message.ProposedAt == nil || now.Sub(*d.message.ProposedAt) > ProposalTTL {
		if err := s.store.ExpireProposal(ctx, messageID); err != nil && !errors.Is(err, store.ErrProposalDecided) {
			return model.ChatMessage{}, err
		}
		return model.ChatMessage{}, &store.ConflictError{Code: "proposal_expired", Message: "This proposal has expired; ask the crew again."}
	}
	if !confirm {
		if err := s.store.DeclineProposal(ctx, messageID, p.UserID, p.Name); err != nil {
			return model.ChatMessage{}, err
		}
		return s.reload(ctx, messageID)
	}
	if err := needs(p, d.kind); err != nil {
		return model.ChatMessage{}, err
	}
	if err := s.store.ClaimProposal(ctx, messageID, p.UserID, p.Name); err != nil {
		return model.ChatMessage{}, err
	}
	// Once claimed, apply even if the person goes away, so the result is recorded with it
	ctx = context.WithoutCancel(ctx)
	result, err := d.kind.Apply(ctx, s.env(), s.request(p, d))
	if err != nil {
		if rerr := s.store.ReleaseProposal(ctx, messageID); rerr != nil {
			return model.ChatMessage{}, errors.Join(err, rerr)
		}
		return model.ChatMessage{}, err
	}
	if err := s.store.SetProposalResult(ctx, messageID, result); err != nil {
		return model.ChatMessage{}, err
	}
	return s.reload(ctx, messageID)
}

// Undo reverses a confirmed proposal on the caller's own conversation, when its kind can be undone.
func (s *Service) Undo(ctx context.Context, p *auth.Principal, messageID string) (model.ChatMessage, error) {
	d, err := s.loadDecision(ctx, p, messageID)
	if err != nil {
		return model.ChatMessage{}, err
	}
	m := d.message
	if *m.ProposalState != model.ProposalConfirmed || m.ProposalResult == nil || d.kind.Undo == nil {
		return model.ChatMessage{}, &store.ConflictError{Code: "not_undoable", Message: "This change cannot be undone from chat."}
	}
	if err := needs(p, d.kind); err != nil {
		return model.ChatMessage{}, err
	}
	// Claim the undo first so two clicks cannot both run it; give it back if the undo fails
	if err := s.store.MarkUndone(ctx, messageID); err != nil {
		return model.ChatMessage{}, err
	}
	ctx = context.WithoutCancel(ctx)
	if err := d.kind.Undo(ctx, s.env(), s.request(p, d)); err != nil {
		if uerr := s.store.UnmarkUndone(ctx, messageID); uerr != nil {
			return model.ChatMessage{}, errors.Join(err, uerr)
		}
		return model.ChatMessage{}, err
	}
	return s.reload(ctx, messageID)
}

// needs checks the person holds what a proposal kind needs, besides crews.chat.
func needs(p *auth.Principal, k Kind) error {
	for _, perm := range k.Needs {
		if !p.Can(perm) {
			return &launch.ForbiddenError{Permission: perm}
		}
	}
	return nil
}

func (s *Service) request(p *auth.Principal, d decision) Request {
	return Request{Principal: p, Crew: d.crew, Conversation: d.conv, MessageID: d.message.ID, Payload: d.message.Proposal.Payload,
		Result: d.message.ProposalResult}
}

func (s *Service) reload(ctx context.Context, messageID string) (model.ChatMessage, error) {
	m, _, err := s.store.GetMessage(ctx, messageID)
	return m, err
}
