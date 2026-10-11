package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"crewstudio/internal/auth"
	"crewstudio/internal/chat"
	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// Crew chat (docs/crew-state-and-pmo-design.md, section 8). Everything here needs crews.chat and acts as the
// signed-in person: people see and use only their own conversations, with crews in their areas.

const (
	maxConversationList = 100
	maxMessagesShown    = 200
	// Chat turns spend money like runs, but come in quicker succession, so they get their own limit per person
	chatTurnsPerMinute = 30
	chatTurnBurst      = 10
	// chatBudgetWindow is the period chat budgets cover (store.chatBudgetWindow)
	chatBudgetWindow = 24 * time.Hour
)

var chatTurns = newKeyedLimiter(rate.Every(time.Minute/chatTurnsPerMinute), chatTurnBurst)

func (s *Server) chatRoutes(r chi.Router) {
	r.Route("/chat", func(r chi.Router) {
		r.Use(s.need(auth.CrewsChat))
		r.Get("/crews", s.listChatCrews)
		r.Get("/agents", s.listChatAgents)
		r.Get("/conversations", s.listConversations)
		r.Post("/conversations", s.startConversation)
		r.Get("/conversations/{id}", s.getConversation)
		r.Delete("/conversations/{id}", s.deleteConversation)
		r.Post("/conversations/{id}/messages", s.sendChatMessage)
		r.Post("/conversations/{id}/compact", s.compactConversation)
		r.Post("/messages/{id}/confirm", s.decideProposal(true))
		r.Post("/messages/{id}/decline", s.decideProposal(false))
		r.Post("/messages/{id}/undo", s.undoProposal)
		r.Get("/conversations/{id}/questions", s.conversationQuestions)
		r.Get("/handoffs/{id}", s.getHandoff)
		r.Post("/handoffs/{id}/cancel", s.cancelHandoff)
	})
}

// chatFail answers 403 for chat.DeniedError (with its code, "forbidden" by default) and otherwise like failErr.
func (s *Server) chatFail(w http.ResponseWriter, r *http.Request, err error) {
	var denied *chat.DeniedError
	var te *tracker.Error
	var limit *tracker.LimitError
	var fe *km2.FolderError
	var ke *km2.Error
	switch {
	case errors.As(err, &denied):
		code := denied.Code
		if code == "" {
			code = "forbidden"
		}
		fail(w, http.StatusForbidden, code, denied.Message)
	// A confirmed card that posts to a tracker or saves to KM2 (agent chat, slice 3d) fails with the service's answer
	case errors.As(err, &te) && !te.NotFound && (te.Status != 0 || te.Unreachable):
		fail(w, http.StatusBadGateway, "tracker_failed", "The tracker did not accept the change.", te.Message)
	case errors.As(err, &te) || errors.As(err, &limit):
		s.trackerFailure(w, r, err)
	case errors.As(err, &fe) || errors.As(err, &ke):
		s.km2Failure(w, r, err)
	default:
		failErr(w, r, s.log, err)
	}
}

func (s *Server) listChatCrews(w http.ResponseWriter, r *http.Request) {
	crews, err := s.store.ChatCrews(r.Context(), scopeOf(principal(r)))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, crews)
}

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	crewID, agentID := r.URL.Query().Get("crew_id"), r.URL.Query().Get("agent_id")
	if crewID != "" && !uuidPattern.MatchString(crewID) {
		failErr(w, r, s.log, invalid("crew_id: must be a UUID"))
		return
	}
	if agentID != "" && !uuidPattern.MatchString(agentID) {
		failErr(w, r, s.log, invalid("agent_id: must be a UUID"))
		return
	}
	p := principal(r)
	convs, err := s.store.ListConversations(r.Context(), store.ConversationFilter{
		UserID: p.UserID, CrewID: crewID, AgentID: agentID, Scope: scopeOf(p), Limit: maxConversationList,
	})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, convs)
}

// conversationView is a conversation with its latest messages and what it may still spend today.
type conversationView struct {
	Conversation model.Conversation  `json:"conversation"`
	Messages     []model.ChatMessage `json:"messages"`
	Budget       model.ChatBudget    `json:"budget"`
	// ChatEnabled is false once the crew's (or agent's) chat has been turned off; the conversation can be read, not
	// continued
	ChatEnabled bool `json:"chat_enabled"`
	// Handoffs are the conversation's latest hand-offs, newest first (always empty with a crew)
	Handoffs []model.Handoff `json:"handoffs"`
	// Questions are the open questions its hand-offs are waiting on the owner to answer, soonest due first
	Questions []model.HumanQuestion `json:"questions"`
}

// chatBudgetOf is a conversation's chat settings (on its crew or agent) and what its person spent with that crew or
// agent in the last 24 hours.
func (s *Server) chatBudgetOf(ctx context.Context, conv model.Conversation) (enabled bool, daily, personDaily, personSpent float64, err error) {
	if conv.AgentID != nil {
		settings, err := s.store.AgentChatSettings(ctx, *conv.AgentID)
		if err != nil {
			return false, 0, 0, 0, err
		}
		spent, err := s.store.PersonAgentChatSpent(ctx, *conv.AgentID, conv.UserID)
		return settings.Enabled, settings.DailyBudgetUSD, settings.PersonDailyBudgetUSD, spent, err
	}
	crewID := ""
	if conv.CrewID != nil {
		crewID = *conv.CrewID
	}
	settings, err := s.store.ChatSettings(ctx, crewID)
	if err != nil {
		return false, 0, 0, 0, err
	}
	spent, err := s.store.PersonChatSpent(ctx, crewID, conv.UserID)
	// A conversation about a work order can always go on; it does not need the crew's chat turned on
	return settings.Enabled || conv.WorkOrderID != nil, settings.DailyBudgetUSD, settings.PersonDailyBudgetUSD, spent, err
}

func (s *Server) conversationView(ctx context.Context, conv model.Conversation) (conversationView, error) {
	view := conversationView{Conversation: conv, Handoffs: []model.Handoff{}, Questions: []model.HumanQuestion{}}
	if err := s.store.SettleReplies(ctx, conv.ID, chat.ProposalTTL); err != nil {
		return view, err
	}
	var err error
	if view.Messages, err = s.store.ListMessages(ctx, conv.ID, maxMessagesShown); err != nil {
		return view, err
	}
	if view.Handoffs, err = s.conversationHandoffs(ctx, conv); err != nil {
		return view, err
	}
	if view.Questions, err = s.openConversationQuestions(ctx, auth.FromContext(ctx), conv); err != nil {
		return view, err
	}
	enabled, daily, personDaily, personSpent, err := s.chatBudgetOf(ctx, conv)
	if err != nil {
		return view, err
	}
	spent, err := s.store.ChatSpent(ctx, conv.ID)
	if err != nil {
		return view, err
	}
	view.ChatEnabled = enabled
	view.Budget = model.ChatBudget{DailyUSD: daily, SpentUSD: spent, PersonDailyUSD: personDaily, PersonSpentUSD: personSpent,
		RemainingUSD: max(0, min(daily-spent, personDaily-personSpent))}
	return view, nil
}

// startConversationRequest starts a conversation with a crew (crew_id) or an agent (agent_id).
type startConversationRequest struct {
	CrewID  string `json:"crew_id"`
	AgentID string `json:"agent_id"`
	Content string `json:"content"`
}

func (s *Server) startConversation(w http.ResponseWriter, r *http.Request) {
	in, ok := s.readChatMessage(w, r)
	if !ok {
		return
	}
	if in.AgentID != "" {
		if in.CrewID != "" || !uuidPattern.MatchString(in.AgentID) {
			s.refuseMessage(w, r, in, invalid("agent_id: must be an agent's id, without crew_id"))
			return
		}
		s.startMessageTurn(w, r, in, func(p *auth.Principal) (chat.Turn, error) {
			return s.chat.StartAgentConversation(r.Context(), p, in.AgentID, in.Content, in.Files...)
		})
		return
	}
	if !uuidPattern.MatchString(in.CrewID) {
		s.refuseMessage(w, r, in, invalid("crew_id: must be a crew's id (or send agent_id)"))
		return
	}
	s.startMessageTurn(w, r, in, func(p *auth.Principal) (chat.Turn, error) {
		return s.chat.StartConversation(r.Context(), p, in.CrewID, in.Content, in.Files...)
	})
}

// startMessageTurn checks the message and starts its turn; the message's files are removed if it does not start.
func (s *Server) startMessageTurn(w http.ResponseWriter, r *http.Request, in chatMessageForm, start func(*auth.Principal) (chat.Turn, error)) {
	if _, err := model.NormalizeChatMessage(in.Content); err != nil {
		s.refuseMessage(w, r, in, err)
		return
	}
	s.runTurn(w, r, in.Files, start)
}

func (s *Server) refuseMessage(w http.ResponseWriter, r *http.Request, in chatMessageForm, err error) {
	s.dropChatFiles(r.Context(), in.Files)
	failErr(w, r, s.log, err)
}

func (s *Server) sendChatMessage(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	in, ok := s.readChatMessage(w, r)
	if !ok {
		return
	}
	s.startMessageTurn(w, r, in, func(p *auth.Principal) (chat.Turn, error) {
		return s.chat.SendMessage(r.Context(), p, id, in.Content, in.Files...)
	})
}

// startTurn rate-limits a turn (it spends money like a run) and answers with the conversation.
func (s *Server) startTurn(w http.ResponseWriter, r *http.Request, start func(*auth.Principal) (chat.Turn, error)) {
	if s.allowTurn(w, r) {
		s.runTurn(w, r, nil, start)
	}
}

// allowTurn answers 429 itself when the person is sending turns too quickly.
func (s *Server) allowTurn(w http.ResponseWriter, r *http.Request) bool {
	if !chatTurns.allow(principal(r).UserID) {
		tooMany(w, "Messages are being sent too quickly; wait a moment.")
		return false
	}
	return true
}

// runTurn starts a turn that passed the rate limit and answers with the conversation. A message's files are
// removed again when the turn does not start.
func (s *Server) runTurn(w http.ResponseWriter, r *http.Request, files []model.Attachment, start func(*auth.Principal) (chat.Turn, error)) {
	p := principal(r)
	turn, err := start(p)
	if err != nil {
		s.dropChatFiles(r.Context(), files)
		s.chatFail(w, r, err)
		return
	}
	view, err := s.conversationView(r.Context(), turn.Conversation)
	if err != nil {
		// The turn started; answer with what it stored rather than an error the person would retry
		s.log.Warn("read conversation after starting a chat turn", "conversation_id", turn.Conversation.ID, "err", err)
		view = conversationView{Conversation: turn.Conversation, Messages: turnMessages(turn), ChatEnabled: true, Handoffs: []model.Handoff{},
			Questions: []model.HumanQuestion{}}
	}
	created(w, view)
}

// turnMessages is what a turn stored: the person's message (none for a compaction) and the reply.
func turnMessages(turn chat.Turn) []model.ChatMessage {
	if turn.User.ID == "" {
		return []model.ChatMessage{turn.Assistant}
	}
	return []model.ChatMessage{turn.User, turn.Assistant}
}

// compactConversation starts a summary of the conversation so far; later turns are given it instead of the history.
func (s *Server) compactConversation(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	s.startTurn(w, r, func(p *auth.Principal) (chat.Turn, error) {
		return s.chat.Compact(r.Context(), p, id)
	})
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	conv, _, err := s.chat.OwnConversation(r.Context(), principal(r), id)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	view, err := s.conversationView(r.Context(), conv)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, view)
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	conv, _, err := s.chat.OwnConversation(r.Context(), principal(r), id)
	if err == nil {
		err = s.store.DeleteConversation(r.Context(), conv.ID)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

func (s *Server) decideProposal(confirm bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, valid := pathID(w, r)
		if !valid {
			return
		}
		m, err := s.chat.Decide(r.Context(), principal(r), id, confirm, time.Now())
		if err != nil {
			s.chatFail(w, r, err)
			return
		}
		ok(w, m)
	}
}

func (s *Server) undoProposal(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	m, err := s.chat.Undo(r.Context(), principal(r), id)
	if err != nil {
		s.chatFail(w, r, err)
		return
	}
	ok(w, m)
}
