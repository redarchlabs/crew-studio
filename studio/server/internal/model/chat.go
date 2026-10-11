package model

import (
	"strings"
	"time"
	"unicode"
)

// Crew chat (docs/crew-state-and-pmo-design.md, section 8): people talk to a crew's front desk agent. Each message
// is a short single-agent run of kind "chat". Changes the front desk wants are proposals that the person confirms.

const (
	RunKindCrew = "crew"
	RunKindChat = "chat"
	// RunKindHandoff is a hand-off from an agent chat, worked in the background (docs/agent-chat-handoffs-design.md)
	RunKindHandoff = "handoff"

	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
	// ChatRoleSummary is a compaction: a summary of the conversation before it, written by a chat run. Later turns
	// are given the latest summary and the messages after it instead of the whole history
	ChatRoleSummary = "summary"

	ProposalPending   = "pending"
	ProposalConfirmed = "confirmed"
	ProposalDeclined  = "declined"
	ProposalExpired   = "expired"

	// DefaultChatDailyBudgetUSD is what one conversation may spend in any 24 hours unless the crew says otherwise
	DefaultChatDailyBudgetUSD = 1.0
	MaxChatDailyBudgetUSD     = 100.0
	// DefaultChatPersonBudgetUSD is what one person may spend on one crew's chat in any 24 hours, over all their
	// conversations, so deleting a conversation or starting another does not buy more
	DefaultChatPersonBudgetUSD = 5.0
	MaxChatPersonBudgetUSD     = 1000.0
	// MaxChatMessageLen caps one message a person sends (characters)
	MaxChatMessageLen  = 4_000
	maxChatTitleRunes  = 80
	maxChatTitleSource = 200
)

// IsChatKind reports whether runs of this kind come from a chat (a turn or a hand-off): they belong to the person
// chatting, only read and propose, and count towards chat budgets.
func IsChatKind(kind string) bool { return kind == RunKindChat || kind == RunKindHandoff }

// ChatSettings say whether people can chat with a crew, which of its agents answers, and what chat may spend.
type ChatSettings struct {
	CrewID  string `json:"crew_id"`
	Enabled bool   `json:"enabled"`
	// FrontDeskAgentID is one of the crew's agents; it answers every message
	FrontDeskAgentID *string `json:"front_desk_agent_id"`
	// LLMID, when set, replaces the front desk agent's own model in chat (usually a cheaper one)
	LLMID          *string `json:"llm_id"`
	DailyBudgetUSD float64 `json:"daily_budget_usd"`
	// PersonDailyBudgetUSD caps one person's chat with the crew in any 24 hours, over all their conversations
	PersonDailyBudgetUSD float64    `json:"person_daily_budget_usd"`
	UpdatedByName        string     `json:"updated_by_name"`
	UpdatedAt            *time.Time `json:"updated_at"`
}

// DefaultChatSettings is a crew's chat before anyone sets it up: off.
func DefaultChatSettings(crewID string) ChatSettings {
	return ChatSettings{CrewID: crewID, DailyBudgetUSD: DefaultChatDailyBudgetUSD, PersonDailyBudgetUSD: DefaultChatPersonBudgetUSD}
}

// NormalizeChatSettings validates chat settings (not whether the agent is in the crew) and returns a cleaned copy.
func NormalizeChatSettings(in ChatSettings) (ChatSettings, error) {
	out := in
	out.FrontDeskAgentID = trimmedPtr(in.FrontDeskAgentID)
	out.LLMID = trimmedPtr(in.LLMID)
	var c checker
	c.optionalUUID("front_desk_agent_id", out.FrontDeskAgentID)
	c.optionalUUID("llm_id", out.LLMID)
	if out.Enabled && out.FrontDeskAgentID == nil {
		c.addf("front_desk_agent_id", "pick the agent that answers chat")
	}
	if out.DailyBudgetUSD <= 0 || out.DailyBudgetUSD > MaxChatDailyBudgetUSD {
		c.addf("daily_budget_usd", "must be more than 0 and at most %g", MaxChatDailyBudgetUSD)
	}
	if out.PersonDailyBudgetUSD == 0 {
		out.PersonDailyBudgetUSD = DefaultChatPersonBudgetUSD
	}
	if out.PersonDailyBudgetUSD < 0 || out.PersonDailyBudgetUSD > MaxChatPersonBudgetUSD {
		c.addf("person_daily_budget_usd", "must be more than 0 and at most %g", MaxChatPersonBudgetUSD)
	}
	return out, c.err()
}

// ChatCrew is a crew someone can chat with.
type ChatCrew struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	FrontDeskName string `json:"front_desk_name"`
	FrontDeskRole string `json:"front_desk_role"`
}

// Conversation is one person's conversation with one crew or, in agent chat, with one agent.
type Conversation struct {
	ID string `json:"id"`
	// CrewID is set for a conversation with a crew, AgentID for one with an agent (docs/agent-chat-design.md)
	CrewID    *string `json:"crew_id"`
	CrewName  string  `json:"crew_name"`
	AgentID   *string `json:"agent_id"`
	AgentName string  `json:"agent_name"`
	UserID    string  `json:"user_id"`
	// WorkOrderID is set for a conversation about a work order (one per person and work order)
	WorkOrderID *string   `json:"work_order_id"`
	Title       string    `json:"title"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Proposal is a change the front desk wants to make; nothing happens until the person confirms it.
type Proposal struct {
	Type    string         `json:"type"`
	Summary string         `json:"summary"`
	Payload map[string]any `json:"payload"`
}

// ChatMessage is one message in a conversation. Assistant replies are empty until their run succeeds.
type ChatMessage struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	Proposal       *Proposal `json:"proposal"`
	ProposalState  *string   `json:"proposal_state"`
	// ProposedAt is when the front desk made the proposal; it expires a day later
	ProposedAt     *time.Time     `json:"proposed_at"`
	ProposalResult map[string]any `json:"proposal_result"`
	DecidedByName  string         `json:"decided_by_name"`
	DecidedAt      *time.Time     `json:"decided_at"`
	UndoneAt       *time.Time     `json:"undone_at"`
	RunID          *string        `json:"run_id"`
	// RunKind is the kind of that run: "chat" for a reply, "handoff" for a hand-off's result ("" without a run)
	RunKind string `json:"run_kind,omitempty"`
	// Handoff is set on a hand-off's messages: what was handed off to whom
	Handoff *HandoffRef `json:"handoff,omitempty"`
	// HandoffProposal says the message is a card a hand-off proposed while it worked, not its result
	HandoffProposal bool `json:"handoff_proposal,omitempty"`
	// RunStatus is the status of the run writing an assistant reply (null for messages people sent)
	RunStatus *RunStatus `json:"run_status"`
	// RunError says briefly why a reply failed
	RunError  string    `json:"run_error,omitempty"`
	CostUSD   *float64  `json:"cost_usd"`
	CreatedAt time.Time `json:"created_at"`
	// Attachments are the files sent with a person's message
	Attachments []ChatAttachment `json:"attachments"`
}

// ChatAttachment is a file sent with a chat message; its content is at GET /attachments/{id}/content.
type ChatAttachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

// ChatBudget is what a conversation may still spend today. RemainingUSD also respects the person's own cap
// across their conversations with the crew.
type ChatBudget struct {
	DailyUSD       float64 `json:"daily_budget_usd"`
	SpentUSD       float64 `json:"spent_last_24h_usd"`
	PersonDailyUSD float64 `json:"person_daily_budget_usd"`
	PersonSpentUSD float64 `json:"person_spent_last_24h_usd"`
	RemainingUSD   float64 `json:"remaining_usd"`
}

// ChatSpec is the part of a RunSpec that carries one chat turn to the runner. History and notes are data from
// people and earlier runs; the runner presents them as untrusted text, never as instructions.
type ChatSpec struct {
	ConversationID string `json:"conversation_id"`
	PersonName     string `json:"person_name"`
	// Message is what the person just sent
	Message string `json:"message"`
	// History is the conversation's latest messages before this one, oldest first
	History []ChatTurn `json:"history"`
	// Notes are the crew's crew-wide notes, cut to fit; NotesOmitted counts the ones left out
	Notes        []ChatNote `json:"notes"`
	NotesOmitted int        `json:"notes_omitted"`
	// Tools are the only tools the front desk gets in this turn (see internal/chat)
	Tools []string `json:"tools"`
	// WorkOrder is set when the conversation is about a work order: its brief and latest output
	WorkOrder *ChatWorkOrder `json:"work_order,omitempty"`
	// Compact asks for a summary of History (which may start with the previous summary) instead of a reply;
	// Message is empty and there are no tools
	Compact bool `json:"compact,omitempty"`
	// Files were sent with Message; the run manager copies them into the workspace like a work order's inputs
	Files []SpecAttachment `json:"files,omitempty"`
	// Agent is set for agent chat: the specialists the agent may ask and the crews it may start
	Agent *ChatAgentSpec `json:"agent,omitempty"`
	// Connections are, in agent chat, the agent's listed connections the person could see when the turn started.
	// Its tracker and KM2 reads go only through these, and only while they stay enabled (store.RunTokenScope).
	Connections []RunConnection `json:"connections,omitempty"`
	// MCPTools are, in agent chat, the reading tools of the agent's MCP connections in Connections (slice 3e). The lead
	// gets one tool per entry; each call goes through Studio, which checks it against this list and the tool's settings.
	MCPTools []ChatMCPTool `json:"mcp_tools,omitempty"`
	// Handoff is set for a hand-off run: what to work on, and who works. Message is empty then.
	Handoff *ChatHandoffSpec `json:"handoff,omitempty"`
	// PendingHandoffs are, in a turn, the conversation's hand-offs still queued or running, so the lead can say so
	// instead of starting them again
	PendingHandoffs []ChatHandoffRef `json:"pending_handoffs,omitempty"`
	// CrewInputs are the run inputs of the crew a crew chat's Start Work Order runs (the frozen crew has none, as a
	// turn is not a run of the crew), so the front desk can fill them in
	CrewInputs []ChatInput `json:"crew_inputs,omitempty"`
}

// ChatTurn is one earlier message given to the front desk. Role "summary" is a compaction of the messages before it.
type ChatTurn struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
	// Kind is "handoff_result" for the result of a hand-off and "handoff_proposal" for a card it proposed, which Title
	// and Worker then describe
	Kind   string `json:"kind,omitempty"`
	Title  string `json:"title,omitempty"`
	Worker string `json:"worker,omitempty"`
}

// History turns from hand-offs: a hand-off's result, and a card a hand-off proposed (its text is the card's summary).
const (
	ChatTurnHandoffResult   = "handoff_result"
	ChatTurnHandoffProposal = "handoff_proposal"
)

// ChatNote is one of the crew's notes given to the front desk.
type ChatNote struct {
	Name      string `json:"name"`
	Subject   string `json:"subject"`
	Version   int    `json:"version"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// NormalizeChatMessage trims a person's message and checks it is not empty or too long.
func NormalizeChatMessage(content string) (string, error) {
	out := strings.TrimSpace(content)
	var c checker
	c.text("content", out, 1, MaxChatMessageLen)
	if strings.ContainsRune(out, 0) {
		c.addf("content", "must not contain NUL characters")
	}
	return out, c.err()
}

// ChatTitle makes a conversation title from its first message: one line, at most 80 characters.
func ChatTitle(message string) string {
	r := []rune(message)
	if len(r) > maxChatTitleSource {
		r = r[:maxChatTitleSource]
	}
	title := strings.Join(strings.FieldsFunc(string(r), func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }), " ")
	if t := []rune(title); len(t) > maxChatTitleRunes {
		title = strings.TrimSpace(string(t[:maxChatTitleRunes-1])) + "…"
	}
	return title
}
