package chat

import (
	"context"
	"slices"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Compacting a conversation: a chat run (with no tools) summarises the conversation so far, and the summary is stored
// as a message of role "summary". Later turns are given that summary and the messages after it (see
// store.RecentTurns), so a long conversation stops resending its whole history with every message.

const (
	// CompactMessages is how many messages since the last summary a compaction reads
	CompactMessages = 200
	// maxCompactChars cuts each message a compaction reads; the summary is meant to keep the gist, not every word
	maxCompactChars = 8_000
	// minCompactMessages is the least there must be since the last summary for a compaction to be worth its cost
	minCompactMessages = 2
)

var errNothingToCompact = &store.ConflictError{Code: "nothing_to_compact",
	Message: "There is nothing new to compact yet: the conversation has fewer than two messages since it was last compacted."}

// Compact starts a compaction of the caller's conversation.
func (s *Service) Compact(ctx context.Context, p *auth.Principal, conversationID string) (Turn, error) {
	if err := checkChatter(p); err != nil {
		return Turn{}, err
	}
	conv, crew, err := s.OwnConversation(ctx, p, conversationID)
	if err != nil {
		return Turn{}, err
	}
	base, settings, err := s.turnBase(ctx, p, conv, crew)
	if err != nil {
		return Turn{}, err
	}
	return s.compact(ctx, p, conv, base, settings)
}

// CompactWorkOrderConversation starts a compaction of the caller's conversation about a work order.
func (s *Service) CompactWorkOrderConversation(ctx context.Context, p *auth.Principal, wo model.WorkOrder) (Turn, error) {
	conv, err := s.WorkOrderConversation(ctx, p, wo)
	if err != nil {
		return Turn{}, err
	}
	return s.Compact(ctx, p, conv.ID)
}

func (s *Service) compact(ctx context.Context, p *auth.Principal, conv model.Conversation, base model.RunSpec,
	settings model.ChatSettings) (Turn, error) {
	if err := s.store.SettleReplies(ctx, conv.ID, ProposalTTL); err != nil {
		return Turn{}, err
	}
	// Read before the history: CreateChatTurn refuses the compaction if a message came after it
	through, err := s.store.LatestMessageSeq(ctx, conv.ID)
	if err != nil {
		return Turn{}, err
	}
	turns, err := s.store.RecentTurns(ctx, conv.ID, CompactMessages)
	if err != nil {
		return Turn{}, err
	}
	history, fresh := make([]model.ChatTurn, 0, len(turns)), 0
	for _, t := range turns {
		if t.Role != model.ChatRoleSummary {
			fresh++
		}
		t.Content = cut(t.Content, maxCompactChars)
		history = append(history, t)
	}
	if fresh < minCompactMessages {
		return Turn{}, errNothingToCompact
	}
	spec := base
	// A summary needs no tools: it only reads the conversation it is given. In agent chat the specialists stay out too.
	spec.Agents = []model.Agent{base.Agents[0]}
	spec.Agents[0].Tools = []string{}
	spec.Agents[0].MaxIter = 1
	spec.Agents[0].AllowDelegation = false
	spec.Crew.AgentIDs = []string{base.Agents[0].ID}
	spec.LLMs = slices.DeleteFunc(slices.Clone(base.LLMs), func(l model.LLM) bool { return base.Agents[0].LLMID == nil || l.ID != *base.Agents[0].LLMID })
	spec.Chat = &model.ChatSpec{ConversationID: conv.ID, PersonName: p.Name, History: history, Notes: []model.ChatNote{},
		Tools: []string{}, Compact: true}
	trigger := "manual"
	if p.Via == auth.ViaAPIKey {
		trigger = "api"
	}
	started, err := s.store.CreateChatTurn(ctx, store.ChatTurn{
		ConversationID: conv.ID, Compact: true, HistoryThrough: through, Spec: spec, DailyBudgetUSD: settings.DailyBudgetUSD,
		PersonBudgetUSD: settings.PersonDailyBudgetUSD, MinBudgetUSD: MinTurnBudgetUSD,
		Meta: store.RunMeta{AreaIDs: base.Crew.AreaIDs, CreatedBy: p.UserID, CreatedByName: p.Name, TriggerType: trigger,
			ChatAgentID: chatAgentID(conv)},
	})
	if err != nil {
		return Turn{}, err
	}
	s.runs.Notify()
	conv.Title, conv.UpdatedAt = started.Title, time.Now()
	return Turn{Conversation: conv, Assistant: started.Assistant, Run: started.Run}, nil
}
