package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/km2"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

const (
	// HistoryMessages is how many earlier messages each turn is given
	HistoryMessages = 20
	// maxHistoryChars cuts each earlier message given to the front desk
	maxHistoryChars = 4_000
	// Notes given to each turn: at most this many, each and in total cut to these sizes
	maxTurnNotes      = 10
	maxNoteChars      = 3_000
	maxTotalNoteChars = 12_000
	// MinTurnBudgetUSD is the least a turn may start with; below it the conversation's daily budget is used up
	MinTurnBudgetUSD = 0.01
	// maxChatIter bounds the front desk's tool loop in one turn
	maxChatIter = 8
)

// Notifier wakes the run dispatcher.
type Notifier interface{ Notify() }

// Service starts chat turns and handles proposals.
type Service struct {
	store    *store.Store
	launcher *launch.Launcher
	runs     Notifier
	// trackers reaches Jira and Linear for tracking-list proposals; nil turns the tracker tools off
	trackers *tracker.Service
	// km2 saves documents for KM2 save cards from agent chat; nil turns them off
	km2 *km2.Service
}

// New makes the chat service.
func New(st *store.Store, launcher *launch.Launcher, runs Notifier) *Service {
	return &Service{store: st, launcher: launcher, runs: runs}
}

// DeniedError means the caller may not do this (403), with a reason people can act on. Code, when set, names the
// refusal for clients ("forbidden" otherwise).
type DeniedError struct {
	Code    string
	Message string
}

func (e *DeniedError) Error() string { return e.Message }

func problem(details ...string) error {
	return &launch.ProblemsError{Message: "Chat with this crew is not possible right now.", Details: details}
}

// checkChatter refuses anyone who may not chat: chat is for people holding crews.chat.
func checkChatter(p *auth.Principal) error {
	if p == nil || !p.Can(auth.CrewsChat) {
		return &launch.ForbiddenError{Permission: auth.CrewsChat}
	}
	if p.Kind != auth.KindHuman {
		return &DeniedError{Message: "Chat is for people; service accounts cannot chat with crews."}
	}
	return nil
}

// Turn is what starting a turn stored.
type Turn struct {
	Conversation model.Conversation
	User         model.ChatMessage
	Assistant    model.ChatMessage
	Run          model.Run
}

// StartConversation starts a conversation with a crew with its first message.
// files (pasted images, PDFs) are already in object storage; the turn records them with the message.
func (s *Service) StartConversation(ctx context.Context, p *auth.Principal, crewID, content string, files ...model.Attachment) (Turn, error) {
	if err := checkChatter(p); err != nil {
		return Turn{}, err
	}
	text, err := model.NormalizeChatMessage(content)
	if err != nil {
		return Turn{}, err
	}
	crew, err := s.visibleCrew(ctx, p, crewID)
	if err != nil {
		return Turn{}, err
	}
	base, settings, err := s.baseSpec(ctx, p, crew)
	if err != nil {
		return Turn{}, err
	}
	conv, err := s.store.CreateConversation(ctx, crew.ID, p.UserID, model.ChatTitle(text))
	if err != nil {
		return Turn{}, err
	}
	turn, err := s.start(ctx, p, conv, base, settings, text, files)
	if err != nil {
		// The turn did not start (start fails only before or while storing it), so nothing was said in the
		// conversation: do not leave it behind empty
		if derr := s.store.DeleteConversation(ctx, conv.ID); derr != nil {
			return Turn{}, errors.Join(err, derr)
		}
	}
	return turn, err
}

// SendMessage adds the person's message to their conversation and starts the turn that answers it.
func (s *Service) SendMessage(ctx context.Context, p *auth.Principal, conversationID, content string, files ...model.Attachment) (Turn, error) {
	if err := checkChatter(p); err != nil {
		return Turn{}, err
	}
	text, err := model.NormalizeChatMessage(content)
	if err != nil {
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
	return s.start(ctx, p, conv, base, settings, text, files)
}

// turnBase is the spec a conversation's turns start from: an agent chat's, a work order conversation's, or the crew
// chat's.
func (s *Service) turnBase(ctx context.Context, p *auth.Principal, conv model.Conversation, crew model.Crew) (model.RunSpec, model.ChatSettings, error) {
	if conv.AgentID != nil {
		return s.agentBase(ctx, p, *conv.AgentID)
	}
	if conv.WorkOrderID != nil {
		return s.workOrderBase(ctx, p, crew)
	}
	return s.baseSpec(ctx, p, crew)
}

// OwnConversation loads a conversation of the caller's with a crew (or agent) they can still see; anything else is
// not found. The crew is empty for a conversation with an agent.
func (s *Service) OwnConversation(ctx context.Context, p *auth.Principal, id string) (model.Conversation, model.Crew, error) {
	conv, err := s.store.GetConversation(ctx, id)
	if err != nil {
		return conv, model.Crew{}, err
	}
	if p == nil || conv.UserID != p.UserID {
		return model.Conversation{}, model.Crew{}, store.ErrNotFound
	}
	if conv.AgentID != nil {
		if _, err := s.visibleAgent(ctx, p, *conv.AgentID); err != nil {
			return model.Conversation{}, model.Crew{}, err
		}
		return conv, model.Crew{}, nil
	}
	if conv.CrewID == nil {
		return model.Conversation{}, model.Crew{}, store.ErrNotFound
	}
	crew, err := s.visibleCrew(ctx, p, *conv.CrewID)
	if err == nil && conv.WorkOrderID != nil {
		_, err = s.visibleWorkOrder(ctx, p, *conv.WorkOrderID)
	}
	if err != nil {
		return model.Conversation{}, model.Crew{}, err
	}
	return conv, crew, nil
}

func (s *Service) visibleCrew(ctx context.Context, p *auth.Principal, crewID string) (model.Crew, error) {
	crew, err := s.store.GetCrew(ctx, crewID)
	if err == nil && !p.SeesAreas(crew.AreaIDs) {
		err = store.ErrNotFound
	}
	return crew, err
}

func (s *Service) start(ctx context.Context, p *auth.Principal, conv model.Conversation, base model.RunSpec,
	settings model.ChatSettings, text string, files []model.Attachment) (Turn, error) {
	spec, err := s.withContext(ctx, base, conv.ID, p, text)
	if err == nil && len(files) > 0 {
		spec.Chat.Files = launch.SpecAttachments(files)
	}
	if err == nil && conv.WorkOrderID != nil {
		spec.Chat.WorkOrder, err = s.chatWorkOrder(ctx, *conv.WorkOrderID)
	}
	if err == nil {
		spec.Chat.PendingHandoffs, err = s.pendingHandoffs(ctx, conv)
	}
	if err != nil {
		return Turn{}, err
	}
	trigger := "manual"
	if p.Via == auth.ViaAPIKey {
		trigger = "api"
	}
	started, err := s.store.CreateChatTurn(ctx, store.ChatTurn{
		ConversationID: conv.ID, Content: text, Files: files, Spec: spec, DailyBudgetUSD: settings.DailyBudgetUSD,
		PersonBudgetUSD: settings.PersonDailyBudgetUSD, MinBudgetUSD: MinTurnBudgetUSD,
		Meta: store.RunMeta{AreaIDs: base.Crew.AreaIDs, CreatedBy: p.UserID, CreatedByName: p.Name, APIKeyID: p.APIKeyID, TriggerType: trigger,
			ChatAgentID: chatAgentID(conv)},
	})
	if err != nil {
		return Turn{}, err
	}
	s.runs.Notify()
	conv.Title, conv.UpdatedAt = started.Title, time.Now()
	return Turn{Conversation: conv, User: started.User, Assistant: started.Assistant, Run: started.Run}, nil
}

// chatAgentID is the agent a conversation is with, or "" for one with a crew.
func chatAgentID(conv model.Conversation) string {
	if conv.AgentID == nil {
		return ""
	}
	return *conv.AgentID
}

// baseSpec checks the crew's chat settings and freezes the front desk agent with only the chat tools.
func (s *Service) baseSpec(ctx context.Context, p *auth.Principal, crew model.Crew) (model.RunSpec, model.ChatSettings, error) {
	settings, err := s.store.ChatSettings(ctx, crew.ID)
	if err != nil {
		return model.RunSpec{}, settings, err
	}
	if !settings.Enabled {
		return model.RunSpec{}, settings, problem("Chat is turned off for this crew.")
	}
	if settings.FrontDeskAgentID == nil || !slices.Contains(crew.AgentIDs, *settings.FrontDeskAgentID) {
		return model.RunSpec{}, settings, problem("The crew's front desk agent is no longer in the crew; someone who can edit the crew must pick another.")
	}
	lib, err := s.launcher.Library(ctx)
	if err != nil {
		return model.RunSpec{}, settings, err
	}
	spec, err := s.deskSpec(ctx, p, crew, lib, *settings.FrontDeskAgentID, settings.LLMID, settings)
	return spec, settings, err
}

// deskSpec freezes agentID (on chatLLMID when set) as the one agent of a chat turn, with only the chat tools the
// person may use. A model whose provider key is missing stops the turn.
func (s *Service) deskSpec(ctx context.Context, p *auth.Principal, crew model.Crew, lib runspec.Library, agentID string,
	chatLLMID *string, settings model.ChatSettings) (model.RunSpec, error) {
	keys, err := s.toolKeys(ctx, p, crew)
	if err == nil {
		keys, err = s.withKM2Tools(ctx, p, crew, keys)
	}
	if err != nil {
		return model.RunSpec{}, err
	}
	agent, llm, err := frontDesk(lib, agentID, chatLLMID, keys)
	if err != nil {
		return model.RunSpec{}, err
	}
	// As in agent chat: without its provider key the front desk's calls fail once the turn has started
	missing, err := s.launcher.MissingProviderKeys(llm.Model)
	if err != nil {
		return model.RunSpec{}, err
	}
	if env := missing[llm.Model]; env != "" {
		return model.RunSpec{}, problem(keyProblem(fmt.Sprintf("The front desk agent %q", agent.Name), llm, env))
	}
	frozen := crew
	frozen.AgentIDs = []string{agent.ID}
	frozen.TaskIDs = []string{}
	frozen.Inputs = []model.CrewInput{}
	return model.RunSpec{
		Crew: frozen, Agents: []model.Agent{agent}, Tasks: []model.Task{}, LLMs: []model.LLM{llm},
		Pricing: slices.Clone(lib.Pricing), Settings: lib.Settings, Inputs: map[string]string{},
		BudgetUSD: settings.DailyBudgetUSD,
		// A turn is not a run of the crew, so its inputs go to the front desk instead, for Start Work Order
		Chat: &model.ChatSpec{CrewInputs: model.ChatInputs(crew.Inputs)},
	}, nil
}

// frontDesk is the agent as it answers chat: its model (or the chat model), no delegation, and only the given chat
// tools (those the person may use).
func frontDesk(lib runspec.Library, agentID string, chatLLMID *string, tools []string) (model.Agent, model.LLM, error) {
	agent, ok := lib.Agents[agentID]
	if !ok {
		return agent, model.LLM{}, problem("The front desk agent no longer exists.")
	}
	llmID := agent.LLMID
	if chatLLMID != nil {
		llmID = chatLLMID
	}
	if llmID == nil {
		return agent, model.LLM{}, problem(fmt.Sprintf("The front desk agent %q has no LLM; pick one or a chat model.", agent.Name))
	}
	llm, ok := lib.LLMs[*llmID]
	if !ok {
		return agent, llm, problem("The chat model no longer exists.")
	}
	if runspec.MatchPrice(llm.Model, lib.Pricing) == nil {
		return agent, llm, problem(fmt.Sprintf("There is no price for the model %q; add one on the Pricing page.", llm.Model))
	}
	agent.LLMID = &llm.ID
	agent.Tools = tools
	agent.AllowDelegation = false
	agent.MaxIter = min(max(agent.MaxIter, 1), maxChatIter)
	return agent, llm, nil
}
