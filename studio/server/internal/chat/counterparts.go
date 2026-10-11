package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// Specialists that propose (docs/agent-chat-handoffs-design.md, slice 3d). A tool that changes things never reaches
// agent chat; when the agent has one with a proposing counterpart, the counterpart is given instead. It sends the change
// to Studio as a proposal, and the person sees a Confirm card. Only their Confirm applies it, as them: the connection is
// checked again then (still enabled, still on the agent's list, still in their areas and the agent's), and whatever is
// posted or saved says who confirmed it. The lead always gets the counterparts of its own changing tools; its
// specialists only when the agent's builder turned on "Specialists may propose changes".

// Proposal types of the counterparts.
const (
	TypeTrackerComment    = "tracker_comment"
	TypeTrackerSuggestion = "tracker_suggestion"
	TypeTrackerIssue      = "tracker_issue"
	TypeKM2Save           = "km2_save"
)

// The counterparts' tool keys. Keep in step with COUNTERPART_TOOL_KEYS in studio_runner/chat_tools.py.
const (
	ProposeTrackerCommentTool    = "propose_tracker_comment"
	ProposeTrackerSuggestionTool = "propose_tracker_suggestion"
	ProposeTrackerIssueTool      = "propose_tracker_issue"
	ProposeKM2SaveTool           = "propose_km_save"
)

// Counterparts maps a catalog tool that changes things to the tool that proposes the same change in chat.
var Counterparts = map[string]string{
	"tracker_comment":        ProposeTrackerCommentTool,
	"tracker_suggest_change": ProposeTrackerSuggestionTool,
	"tracker_create_issue":   ProposeTrackerIssueTool,
	"km_save_document":       ProposeKM2SaveTool,
}

// counterpartNeeds is what the person must hold to be offered a counterpart, and again to confirm its card: Run crews,
// the permission that already lets a person set off work that posts to trackers and saves to KM2 (a crew run).
var counterpartNeeds = []string{auth.CrewsRun}

// counterpartTools are the counterparts as chat tools: each needs a connection of its kind on the agent's list.
var counterpartTools = []Tool{
	{Key: ProposeTrackerCommentTool, Proposes: TypeTrackerComment, Needs: counterpartNeeds, Tracker: true},
	{Key: ProposeTrackerSuggestionTool, Proposes: TypeTrackerSuggestion, Needs: counterpartNeeds, Tracker: true},
	{Key: ProposeTrackerIssueTool, Proposes: TypeTrackerIssue, Needs: counterpartNeeds, Tracker: true},
	{Key: ProposeKM2SaveTool, Proposes: TypeKM2Save, Needs: counterpartNeeds, KM2: true},
}

// counterpartOf returns the chat tool that proposes what the catalog tool key changes, if there is one.
func counterpartOf(key string) (Tool, bool) {
	want, ok := Counterparts[key]
	if !ok {
		return Tool{}, false
	}
	i := slices.IndexFunc(counterpartTools, func(t Tool) bool { return t.Key == want })
	if i < 0 {
		return Tool{}, false
	}
	return counterpartTools[i], true
}

// toolNeedsOf is what a turn's tool needs from its connections: the catalog's needs, or for a counterpart the kind of
// connection it posts or saves through.
func toolNeedsOf(key string, catalog map[string]model.Tool) []string {
	if tool, ok := catalog[key]; ok {
		return tool.Needs
	}
	i := slices.IndexFunc(counterpartTools, func(t Tool) bool { return t.Key == key })
	switch {
	case i < 0:
		return nil
	case counterpartTools[i].KM2:
		return []string{model.NeedKM2}
	}
	return []string{model.NeedTracker}
}

// proposedVia is where a counterpart's change goes and who proposed it, settled by Studio from the frozen turn (see
// proposedChange), never taken from the model.
type proposedVia struct {
	ConnectionID   string `json:"connection_id"`
	ConnectionName string `json:"connection_name"`
	// ProposedBy is the name of the agent that called the tool, and ProposedByAgentID its id ("" when unknown)
	ProposedBy        string `json:"proposed_by"`
	ProposedByAgentID string `json:"proposed_by_agent_id"`
}

func (v proposedVia) clean() (proposedVia, []string) {
	out := proposedVia{ConnectionID: strings.TrimSpace(v.ConnectionID), ConnectionName: oneLine(v.ConnectionName),
		ProposedBy: oneLine(v.ProposedBy), ProposedByAgentID: strings.TrimSpace(v.ProposedByAgentID)}
	var problems []string
	if !uuidPattern.MatchString(out.ConnectionID) {
		problems = append(problems, "connection_id: must be the connection's id")
	}
	if out.ProposedByAgentID != "" && !uuidPattern.MatchString(out.ProposedByAgentID) {
		problems = append(problems, "proposed_by_agent_id: must be an agent's id")
	}
	if len([]rune(out.ConnectionName)) > maxConnectionRef || len([]rune(out.ProposedBy)) > maxConnectionRef {
		problems = append(problems, fmt.Sprintf("connection_name: must be at most %d characters", maxConnectionRef))
	}
	return out, problems
}

var settledKeys = []string{"connection_id", "connection_name", "proposed_by", "proposed_by_agent_id"}

// proposedChange settles where a counterpart's change goes: the connection the model named in "connection" (by name or
// id, or the only one of its kind when it named none), among those the turn froze. It also records which agent of the
// turn proposed it, from the role the runner sends with the call. A new issue also needs a connection that may create
// issues. The payload returned carries these instead of "connection".
func (s *Service) proposedChange(ctx context.Context, spec model.RunSpec, agentRole, typ string, payload map[string]any) (map[string]any, error) {
	for _, key := range settledKeys {
		if _, sent := payload[key]; sent {
			return nil, &model.ValidationError{Details: []string{"connection: name the connection in connection"}}
		}
	}
	// Only an agent of the turn that was given this counterpart may propose it: the run token is the run's, so the role
	// the runner sends says which agent called, and a specialist without the setting never had the tool
	proposer, ok := turnAgentByRole(spec, agentRole)
	if !ok {
		return nil, &DeniedError{Message: "Studio cannot tell which agent of this chat proposed the change, so it was not accepted."}
	}
	if !slices.Contains(proposer.Tools, counterpartKeyOf(typ)) {
		return nil, &DeniedError{Message: "This agent was not given the tool that proposes that change in this chat."}
	}
	want, _ := payload["connection"].(string)
	conn, err := pickFrozenConnection(spec.Chat.Connections, needOfType(typ), strings.TrimSpace(want))
	if err != nil {
		return nil, err
	}
	// Linear turns a profile link into a mention; refuse it now, before the person sees a card that cannot be posted
	if conn.Kind == model.TrackerLinear {
		for _, v := range payload {
			if text, ok := v.(string); ok && linearProfileLink.MatchString(text) {
				return nil, &model.ValidationError{Details: []string{"text: Linear profile links would mention people; leave them out"}}
			}
		}
	}
	if typ == TypeTrackerIssue {
		c, err := s.store.GetConnection(ctx, conn.ID)
		if err != nil {
			return nil, err
		}
		if !mayCreateIssues(c) {
			return nil, createOff(c.Name)
		}
	}
	out := make(map[string]any, len(payload)+len(settledKeys))
	for k, v := range payload {
		if k != "connection" {
			out[k] = v
		}
	}
	out["connection_id"], out["connection_name"] = conn.ID, conn.Name
	out["proposed_by"], out["proposed_by_agent_id"] = proposer.Name, proposer.ID
	if typ == TypeKM2Save {
		ref, err := conversationRef(spec.Chat.ConversationID, payload["external_ref"])
		if err != nil {
			return nil, err
		}
		out["external_ref"] = ref
	}
	return out, nil
}

// counterpartKeyOf is the tool key of the counterpart that makes proposals of type typ.
func counterpartKeyOf(typ string) string {
	i := slices.IndexFunc(counterpartTools, func(t Tool) bool { return t.Proposes == typ })
	if i < 0 {
		return ""
	}
	return counterpartTools[i].Key
}

// needOfType is the kind of connection a counterpart's proposal goes through.
func needOfType(typ string) string {
	if typ == TypeKM2Save {
		return model.NeedKM2
	}
	return model.NeedTracker
}

// pickFrozenConnection finds the connection a proposal names among the turn's frozen connections of the kind it needs.
func pickFrozenConnection(conns []model.RunConnection, need, want string) (model.RunConnection, error) {
	what := map[string]string{model.NeedTracker: "tracker (Jira, Linear or KM2 issues)", model.NeedKM2: "KM2"}[need]
	var names []string
	var matches []model.RunConnection
	for _, c := range conns {
		if model.ChatNeedOf(c.Kind) != need {
			continue
		}
		names = append(names, c.Name)
		if want == "" || c.ID == want || strings.EqualFold(c.Name, want) {
			matches = append(matches, c)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(names) == 0:
		return model.RunConnection{}, &DeniedError{Message: fmt.Sprintf("This chat has no %s connection to propose the change through.", what)}
	case len(matches) == 0:
		return model.RunConnection{}, &model.ValidationError{Details: []string{"connection: must be one of " + strings.Join(names, ", ")}}
	}
	return model.RunConnection{}, &model.ValidationError{Details: []string{"connection: say which connection, one of " + strings.Join(names, ", ")}}
}

// agentRoleHeaderLen is how much of a role the run-state API keeps from the runner's header; keep in step with
// maxAgentRoleLen in api/runstate.go
const agentRoleHeaderLen = 120

// turnAgentByRole finds the agent of the turn with this role (the lead or a specialist; roles are unique in a turn). A
// role the header cut short matches the one agent whose role starts with it.
func turnAgentByRole(spec model.RunSpec, role string) (model.Agent, bool) {
	if strings.TrimSpace(role) == "" {
		return model.Agent{}, false
	}
	key := model.ChatRoleKey(role)
	match := func(a model.Agent) bool { return model.ChatRoleKey(a.Role) == key }
	if len([]rune(role)) >= agentRoleHeaderLen {
		match = func(a model.Agent) bool { return strings.HasPrefix(model.ChatRoleKey(a.Role), key) }
	}
	var found []model.Agent
	for _, a := range spec.Agents {
		if match(a) {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		return model.Agent{}, false
	}
	return found[0], true
}

func mayCreateIssues(c model.Connection) bool { return c.Config[model.ConfigAllowCreate] == "true" }

func createOff(name string) error {
	return &store.ConflictError{Code: "create_off", Message: fmt.Sprintf("Creating issues is turned off for %s; an administrator can "+
		"turn on \"Let crews create issues\" for the connection.", name)}
}

var (
	errNotAgentChat = &store.ConflictError{Code: "chat_off", Message: "Only a conversation with an agent can apply this change."}
	errProposalsOff = &store.ConflictError{Code: "proposals_off",
		Message: "The agent's specialists may no longer propose changes, so this card can no longer be confirmed."}
	errConnectionNotOffered = &store.ConflictError{Code: "connection_not_offered",
		Message: "The agent may no longer use this connection, or you can no longer see it; ask someone who can edit the agent."}
	errProposerNotOffered = &store.ConflictError{Code: "proposer_not_offered",
		Message: "The agent that proposed this may no longer make this change (its tool or its place as a specialist was taken away); ask the agent again."}
	errConnectionOff = &store.ConflictError{Code: "connection_off", Message: "The connection has been turned off, so this change cannot be made now."}
)

// errOutcomeUnknown is the message on a card whose post or save was sent but whose answer never came: it may have been
// made, so the card stays confirmed rather than being offered again (which could post twice).
const errOutcomeUnknown = "The change was sent, but the answer never came back, so it may or may not have been made. Check before asking the agent again."

// unknownOutcome reports whether a write failed in a way that leaves it unknown whether it was made. Only a clear refusal
// (the tracker or KM2 answered with a 4xx or a rate limit, so nothing was written) means it was not; anything else (a
// 2xx Studio could not read, a 5xx, no answer in time, a service that could not be reached) may have been made.
func unknownOutcome(err error) bool {
	var te *tracker.Error
	var ke *km2.Error
	var fe *km2.FolderError
	switch {
	case errors.As(err, &te):
		return !te.Rejected
	case errors.As(err, &ke):
		return ke.Status < 400 || ke.Status >= 500 || ke.Status == http.StatusRequestTimeout
	case errors.As(err, &fe):
		return false
	}
	return true
}

// written is what Apply returns for a write: its result, or, when it may have been made despite the error, a result
// saying so (the card then stays confirmed); any other error leaves the card pending.
func written(result map[string]any, err error, where map[string]any) (map[string]any, error) {
	if err == nil {
		return result, nil
	}
	if !unknownOutcome(err) {
		return nil, err
	}
	out := map[string]any{"outcome": "unknown", "message": errOutcomeUnknown}
	for k, v := range where {
		out[k] = v
	}
	return out, nil
}

// changingToolOf is the catalog tool whose change a counterpart's proposal type stands for.
func changingToolOf(typ string) string {
	key := counterpartKeyOf(typ)
	for changing, counterpart := range Counterparts {
		if counterpart == key {
			return changing
		}
	}
	return ""
}

// checkProposer checks again that the agent that proposed a card could propose it now: the lead, or one of its
// specialists still on its list (with specialists still allowed to propose) that the person can see, and either way
// still holding the tool whose change the card makes.
func checkProposer(ctx context.Context, env Env, req Request, settings model.AgentChatSettings, via proposedVia, typ string) error {
	lead := *req.Conversation.AgentID
	if via.ProposedByAgentID != lead {
		switch {
		case !settings.SpecialistsMayPropose:
			return errProposalsOff
		case !slices.Contains(settings.SpecialistIDs, via.ProposedByAgentID):
			return errProposerNotOffered
		}
	}
	a, err := env.Store.GetAgent(ctx, via.ProposedByAgentID)
	if errors.Is(err, store.ErrNotFound) {
		return errProposerNotOffered
	}
	if err != nil {
		return err
	}
	if !slices.Contains(a.Tools, changingToolOf(typ)) || (a.ID != lead && !req.Principal.SeesAreas(a.AreaIDs)) {
		return errProposerNotOffered
	}
	return nil
}

// counterpartConnection checks again, as the person confirming, what a counterpart's change (of proposal type typ) goes
// through: the agent's chat is still on, the agent that proposed it could still propose it (checkProposer), and the
// connection is still on the agent's list, enabled, of the kind the change needs, in the person's areas and sharing an
// area with the agent.
func counterpartConnection(ctx context.Context, env Env, req Request, typ string, via proposedVia) (model.Connection, error) {
	need := needOfType(typ)
	conv := req.Conversation
	if conv.AgentID == nil {
		return model.Connection{}, errNotAgentChat
	}
	settings, err := env.Store.AgentChatSettings(ctx, *conv.AgentID)
	switch {
	case err != nil:
		return model.Connection{}, err
	case !settings.Enabled:
		return model.Connection{}, errChatOff
	}
	if err := checkProposer(ctx, env, req, settings, via, typ); err != nil {
		return model.Connection{}, err
	}
	if !slices.Contains(settings.ConnectionIDs, via.ConnectionID) {
		return model.Connection{}, errConnectionNotOffered
	}
	c, err := env.Store.GetConnection(ctx, via.ConnectionID)
	if errors.Is(err, store.ErrNotFound) {
		return c, errConnectionNotOffered
	}
	if err != nil {
		return c, err
	}
	agent, err := env.Store.GetAgent(ctx, *conv.AgentID)
	if err != nil {
		return c, err
	}
	switch {
	case !c.Enabled:
		return c, errConnectionOff
	case model.ChatNeedOf(c.Kind) != need || !req.Principal.SeesAreas(c.AreaIDs) || !sharesArea(c.AreaIDs, agent.AreaIDs):
		return c, errConnectionNotOffered
	}
	return c, nil
}

// writeTimeout bounds a post or save once confirmed, independent of the person's request
const writeTimeout = 90 * time.Second

// attribution is added to what is posted in a tracker: the tracker sees Crew Studio's account, so the text says which
// person confirmed it, and which agent drafted it.
func attribution(p *auth.Principal, proposedBy string) string {
	if proposedBy == "" {
		return fmt.Sprintf("\n\n(Posted by %s from a Crew Studio agent chat.)", oneLine(p.Name))
	}
	return fmt.Sprintf("\n\n(Posted by %s from a Crew Studio agent chat; drafted by %s.)", oneLine(p.Name), proposedBy)
}

// attributionRoom is kept free in a proposed text for the attribution
const attributionRoom = 500
