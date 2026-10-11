package chat

import (
	"context"
	"slices"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// withContext adds the turn's message, the conversation's latest messages and the crew's crew-wide notes to a spec.
// None of it is trusted: the runner presents it to the front desk as data.
func (s *Service) withContext(ctx context.Context, base model.RunSpec, conversationID string, p *auth.Principal, text string) (model.RunSpec, error) {
	if err := s.store.SettleReplies(ctx, conversationID, ProposalTTL); err != nil {
		return base, err
	}
	turns, err := s.store.RecentTurns(ctx, conversationID, HistoryMessages)
	if err != nil {
		return base, err
	}
	history := make([]model.ChatTurn, 0, len(turns))
	for _, t := range turns {
		t.Content = cut(t.Content, maxHistoryChars)
		history = append(history, t)
	}
	// A turn keeps what its base froze: an agent chat's specialists, crews and connections, a crew chat's crew inputs
	var agent *model.ChatAgentSpec
	var crewInputs []model.ChatInput
	var conns []model.RunConnection
	var mcpTools []model.ChatMCPTool
	if base.Chat != nil {
		agent, crewInputs, conns, mcpTools = base.Chat.Agent, base.Chat.CrewInputs, base.Chat.Connections, base.Chat.MCPTools
	}
	// Notes go only to turns of people who may read them, and an agent has none
	notes, omitted := []model.ChatNote{}, 0
	if p.Can(auth.StateView) && agent == nil {
		if notes, omitted, err = s.turnNotes(ctx, base.Crew.ID); err != nil {
			return base, err
		}
	}
	spec := base
	spec.Chat = &model.ChatSpec{
		ConversationID: conversationID, PersonName: p.Name, Message: text, History: history,
		Notes: notes, NotesOmitted: omitted, Tools: slices.Clone(base.Agents[0].Tools), Agent: agent, CrewInputs: crewInputs,
		Connections: conns, MCPTools: mcpTools,
	}
	return spec, nil
}

// turnNotes reads the crew's crew-wide notes, cut to fit a turn. Chat never sees a schedule's notes.
func (s *Service) turnNotes(ctx context.Context, crewID string) ([]model.ChatNote, int, error) {
	infos, err := s.store.ListNotes(ctx, store.NoteFilter{Scope: store.StateScope{CrewID: crewID}})
	if err != nil {
		return nil, 0, err
	}
	out := []model.ChatNote{}
	total := 0
	for i, info := range infos {
		if i >= maxTurnNotes || total >= maxTotalNoteChars {
			return out, len(infos) - i, nil
		}
		note, err := s.store.LatestNote(ctx, store.NoteKey{StateScope: store.StateScope{CrewID: crewID}, Subject: info.Subject, Name: info.Name})
		if err != nil {
			return nil, 0, err
		}
		content := cut(note.Content, min(maxNoteChars, maxTotalNoteChars-total))
		total += len([]rune(content))
		out = append(out, model.ChatNote{Name: note.Name, Subject: note.Subject, Version: note.Version, Content: content,
			Truncated: content != note.Content})
	}
	return out, 0, nil
}

// cut keeps the first n characters of s, marking the cut.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n[… cut]"
}
