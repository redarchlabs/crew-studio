package chat

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

// Seeded agents: 001-005 are the PE crew's (Finance), 006 is the AI research analyst (the other area).
const (
	investigator = "22222222-0000-4000-8000-000000000001"
	analyst      = "22222222-0000-4000-8000-000000000002"
	dealPartner  = "22222222-0000-4000-8000-000000000005"
	researcher   = "22222222-0000-4000-8000-000000000006"
	aiCrew       = "44444444-0000-4000-8000-000000000002"
)

func (e env) agentOn(t *testing.T, specialists, crews []string) {
	t.Helper()
	if _, err := e.st.PutAgentChatSettings(context.Background(), model.AgentChatSettings{AgentID: dealPartner, Enabled: true,
		SpecialistIDs: specialists, CrewIDs: crews, DailyBudgetUSD: 1, PersonDailyBudgetUSD: 5}, ""); err != nil {
		t.Fatal(err)
	}
}

// agentEnv is newEnv with a tool catalog that knows the seeded agents' search tool.
func agentEnv(t *testing.T) env {
	t.Helper()
	e := newEnv(t)
	e.svc = New(e.st, launch.New(e.st, searchTools{}, e.runs), e.runs)
	return e
}

func TestAgentChatTurnFreezesLeadSpecialistsAndCrews(t *testing.T) {
	e := agentEnv(t)
	ctx := context.Background()
	_, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "hi")
	wantProblem(t, err, "turned off")
	if _, err := e.svc.StartAgentConversation(ctx, e.ann, researcher, "hi"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an agent outside the person's areas = %v", err)
	}

	// The researcher and the AI crew are outside Ann's areas, so her turns leave them out
	e.agentOn(t, []string{investigator, researcher, analyst}, []string{aiCrew, peCrew})
	turn, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "Should we look at Acme?")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Conversation.AgentID == nil || *turn.Conversation.AgentID != dealPartner || turn.Conversation.CrewID != nil ||
		turn.Conversation.AgentName != "Deal Partner" {
		t.Errorf("conversation = %+v", turn.Conversation)
	}
	spec, err := e.st.GetRunSpec(ctx, turn.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, a := range spec.Agents {
		ids = append(ids, a.ID)
	}
	if !slices.Equal(ids, []string{dealPartner, investigator, analyst}) {
		t.Errorf("agents = %v", ids)
	}
	lead, first := spec.Agents[0], spec.Agents[1]
	// A specialist gets few steps in chat (the seeded investigator has 12) and may ask the lead; the lead keeps the chat cap
	if !lead.AllowDelegation || !slices.Equal(lead.Tools, []string{StartCrewWorkOrderTool}) || first.AllowDelegation ||
		!slices.Equal(first.Tools, []string{"google_search", AskLeadTool}) || first.MaxIter != maxSpecialistIter || lead.MaxIter != maxChatIter {
		t.Errorf("lead = %+v, specialist = %+v", lead, first)
	}
	// The crews carry each input's label and whether it is required, so the agent can fill them in
	if crews := spec.Chat.Agent.Crews; len(crews) == 1 && !slices.Equal(crews[0].Inputs, []model.ChatInput{
		{Key: "company", Label: "Company legal name", Required: true},
		{Key: "location", Label: "Where it is based (helps tell apart same-name companies)"}}) {
		t.Errorf("crew inputs = %+v", crews[0].Inputs)
	}
	if spec.Crew.ID != "" || spec.Crew.Name != "Deal Partner" || spec.Chat == nil || spec.Chat.Agent == nil ||
		len(spec.Chat.Agent.Crews) != 1 || spec.Chat.Agent.Crews[0].ID != peCrew || len(spec.Chat.Agent.Specialists) != 2 ||
		len(spec.Chat.Notes) != 0 || !slices.Equal(spec.Chat.Tools, lead.Tools) {
		t.Errorf("spec crew = %+v, chat = %+v", spec.Crew, spec.Chat)
	}

	// The run records the agent, so the person's budget is counted per agent
	var agentID *string
	if err := e.st.Pool().QueryRow(ctx, `SELECT chat_agent_id::text FROM runs WHERE id = $1`, turn.Run.ID).Scan(&agentID); err != nil ||
		agentID == nil || *agentID != dealPartner {
		t.Errorf("chat_agent_id = %v, %v", agentID, err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'succeeded', cost_usd = 0.4 WHERE id = $1`, turn.Run.ID); err != nil {
		t.Fatal(err)
	}
	if spent, err := e.st.PersonAgentChatSpent(ctx, dealPartner, e.ann.UserID); err != nil || spent != 0.4 {
		t.Errorf("person spent = %v, %v", spent, err)
	}
	if spent, _ := e.st.PersonChatSpent(ctx, peCrew, e.ann.UserID); spent != 0 {
		t.Errorf("agent chat counted towards the crew's chat: %v", spent)
	}

	// Later messages take the same path
	next, err := e.svc.SendMessage(ctx, e.ann, turn.Conversation.ID, "And the risks?")
	if err != nil {
		t.Fatal(err)
	}
	if spec, _ := e.st.GetRunSpec(ctx, next.Run.ID); spec.Chat == nil || spec.Chat.Agent == nil || len(spec.Chat.History) == 0 {
		t.Errorf("second turn = %+v", spec.Chat)
	}
	// While that turn runs it counts at its whole budget towards the person's cap
	if spent, _ := e.st.PersonAgentChatSpent(ctx, dealPartner, e.ann.UserID); spent <= 0.4 {
		t.Errorf("a queued turn did not count: %v", spent)
	}

	// A compaction is given the agent alone: no specialists, no coworker tools
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'succeeded', output = 'Risky.', cost_usd = 0.01 WHERE id = $1`, next.Run.ID); err != nil {
		t.Fatal(err)
	}
	compacted, err := e.svc.Compact(ctx, e.ann, turn.Conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	cspec, _ := e.st.GetRunSpec(ctx, compacted.Run.ID)
	if len(cspec.Agents) != 1 || cspec.Agents[0].AllowDelegation || len(cspec.Agents[0].Tools) != 0 || !slices.Equal(cspec.Crew.AgentIDs, []string{dealPartner}) ||
		len(cspec.LLMs) != 1 || cspec.Chat == nil || !cspec.Chat.Compact || cspec.Chat.Agent != nil {
		t.Errorf("compaction spec = agents %+v, crew %+v, llms %d, chat %+v", cspec.Agents, cspec.Crew.AgentIDs, len(cspec.LLMs), cspec.Chat)
	}
	list, err := e.st.ListConversations(ctx, store.ConversationFilter{UserID: e.ann.UserID, AgentID: dealPartner, Scope: store.AllAreas, Limit: 5})
	if err != nil || len(list) != 1 || list[0].AgentName != "Deal Partner" {
		t.Errorf("agent conversations = %+v, %v", list, err)
	}
	if list, _ := e.st.ListConversations(ctx, store.ConversationFilter{UserID: e.ann.UserID, CrewID: peCrew, Scope: store.AllAreas, Limit: 5}); len(list) != 0 {
		t.Errorf("crew filter matched an agent conversation: %+v", list)
	}

	// Someone else cannot open it
	bob := *e.ann
	bob.UserID = "99999999-0000-4000-8000-000000000001"
	if _, _, err := e.svc.OwnConversation(ctx, &bob, turn.Conversation.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("another person = %v", err)
	}
}

func TestAgentChatWithoutWorkOrdersGetsNoCrews(t *testing.T) {
	e := agentEnv(t)
	ctx := context.Background()
	e.agentOn(t, nil, []string{peCrew})
	e.ann.Permissions = map[string]bool{auth.CrewsChat: true}
	turn, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "hi")
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := e.st.GetRunSpec(ctx, turn.Run.ID)
	if len(spec.Agents) != 1 || spec.Agents[0].AllowDelegation || len(spec.Agents[0].Tools) != 0 || len(spec.Chat.Agent.Crews) != 0 {
		t.Errorf("spec = %+v / %+v", spec.Agents, spec.Chat.Agent)
	}
}

func TestAgentChatSpecialistWithoutModel(t *testing.T) {
	e := agentEnv(t)
	ctx := context.Background()
	e.agentOn(t, []string{investigator}, nil)
	if _, err := e.st.Pool().Exec(ctx, `UPDATE agents SET llm_id = NULL WHERE id = $1`, investigator); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "hi")
	wantProblem(t, err, "has no LLM")
}

// searchTools is a tool catalog that knows the seeded crews' search tool, so their work orders can start.
type searchTools struct{}

func (searchTools) ByKey() (map[string]model.Tool, error) {
	return map[string]model.Tool{
		"google_search":     {Key: "google_search", Name: "Google Search", Available: true, Effect: model.EffectReads},
		"scrape_website":    {Key: "scrape_website", Available: true, Effect: model.EffectReads},
		"tracker_get_issue": {Key: "tracker_get_issue", Available: true, Effect: model.EffectReads, Needs: []string{model.NeedTracker}},
		"tracker_comment":   {Key: "tracker_comment", Available: true, Effect: model.EffectChanges, Needs: []string{model.NeedTracker}},
		"km_search":         {Key: "km_search", Available: true, Effect: model.EffectReads, Needs: []string{model.NeedKM2}},
		"notes_get":         {Key: "notes_get", Available: true, Effect: model.EffectReads, Needs: []string{model.NeedCrew}},
		// Changing tools with proposing counterparts (slice 3d), and one without
		"tracker_suggest_change": {Key: "tracker_suggest_change", Available: true, Effect: model.EffectChanges, Needs: []string{model.NeedTracker}},
		"tracker_create_issue":   {Key: "tracker_create_issue", Available: true, Effect: model.EffectChanges, Needs: []string{model.NeedTracker}},
		"km_save_document":       {Key: "km_save_document", Available: true, Effect: model.EffectChanges, Needs: []string{model.NeedKM2}},
		"workspace_write":        {Key: "workspace_write", Available: true, Effect: model.EffectChanges, Needs: []string{}},
	}, nil
}

func TestAgentChatProposesAWorkOrderOnItsCrews(t *testing.T) {
	e := agentEnv(t)
	ctx := context.Background()
	e.agentOn(t, nil, []string{peCrew})
	turn, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "run the diligence on Acme")
	if err != nil {
		t.Fatal(err)
	}
	scope := e.running(t, turn.Run.ID)
	for name, payload := range map[string]map[string]any{
		"no crew":       {"brief": "Diligence"},
		"unknown crew":  {"brief": "Diligence", "crew": "Somebody else's crew"},
		"crew_id sent":  {"brief": "Diligence", "crew_id": peCrew},
		"unknown input": {"brief": "Diligence", "crew": e.crew.Name, "inputs": map[string]any{"company": "Acme", "ticker": "ACME"}},
	} {
		if _, err := e.svc.Propose(ctx, scope, TypeStartWorkOrder, payload); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A required input without a default must be filled in; the error names it so the agent can retry
	if _, err := e.st.Pool().Exec(ctx, `UPDATE crews SET inputs = jsonb_set(inputs, '{0,default}', '""') WHERE id = $1`, peCrew); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Propose(ctx, scope, TypeStartWorkOrder, map[string]any{"brief": "Diligence", "crew": e.crew.Name})
	wantInvalid(t, err, "inputs.company: is required")
	m, err := e.svc.Propose(ctx, scope, TypeStartWorkOrder, map[string]any{"brief": "Diligence", "crew": e.crew.Name,
		"inputs": map[string]any{"company": "Acme"}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Proposal.Payload["crew_id"] != peCrew || m.Proposal.Payload["crew_name"] != e.crew.Name || m.Proposal.Payload["crew"] != nil {
		t.Errorf("payload = %v", m.Proposal.Payload)
	}
	if want := "Start a work order on " + e.crew.Name + ": Diligence"; m.Proposal.Summary != want {
		t.Errorf("summary = %q", m.Proposal.Summary)
	}
	done, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	woID, _ := done.ProposalResult["work_order_id"].(string)
	wo, err := e.st.GetWorkOrder(ctx, woID)
	if err != nil || wo.CrewID == nil || *wo.CrewID != peCrew || wo.Source != model.SourceChat {
		t.Errorf("work order = %+v, %v", wo, err)
	}

	// A crew taken off the agent's list can no longer be started from an earlier proposal
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'succeeded', output = 'Proposed' WHERE id = $1`, turn.Run.ID); err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.SendMessage(ctx, e.ann, turn.Conversation.ID, "once more")
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.svc.Propose(ctx, e.running(t, second.Run.ID), TypeStartWorkOrder, map[string]any{"brief": "Again", "crew": peCrew,
		"inputs": map[string]any{"company": "Acme"}})
	if err != nil {
		t.Fatal(err)
	}
	e.agentOn(t, nil, nil)
	var conflict *store.ConflictError
	if _, err := e.svc.Decide(ctx, e.ann, again.ID, true, time.Now()); !errors.As(err, &conflict) || conflict.Code != "crew_not_offered" {
		t.Errorf("crew taken off the list = %v", err)
	}
	// Nor once chat with the agent is turned off
	e.agentOn(t, nil, []string{peCrew})
	if _, err := e.st.Pool().Exec(ctx, `UPDATE agent_chat SET enabled = false`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Decide(ctx, e.ann, again.ID, true, time.Now()); !errors.As(err, &conflict) || conflict.Code != "chat_off" {
		t.Errorf("chat turned off = %v", err)
	}
}

func TestToolProblems(t *testing.T) {
	lib := runspec.Library{
		Tools:    map[string]model.Tool{"google_search": {Key: "google_search", Available: false, RequiresEnv: []string{"GEMINI_API_KEY"}}},
		Settings: model.Settings{SearchModel: "gemini-x"},
	}
	agents := []model.Agent{{Name: "A", Tools: []string{"google_search", StartCrewWorkOrderTool}}, {Name: "B", Tools: []string{"scrape_website"}}}
	got := toolProblems(lib, agents)
	if len(got) != 3 || !strings.Contains(got[0], "GEMINI_API_KEY") || !strings.Contains(got[1], "unknown tool") || !strings.Contains(got[2], "search model") {
		t.Errorf("problems = %v", got)
	}
	lib.Tools["google_search"] = model.Tool{Available: true}
	lib.Tools["scrape_website"] = model.Tool{Available: true}
	lib.Pricing = []model.ModelPrice{{Model: "gemini-x"}}
	if got := toolProblems(lib, agents); len(got) != 0 {
		t.Errorf("problems = %v", got)
	}
}

func TestCrewChatCannotNameAnotherCrew(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.turnOn(t, nil, 1)
	turn, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "start a sweep")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Propose(ctx, e.running(t, turn.Run.ID), TypeStartWorkOrder, map[string]any{"brief": "x", "crew": aiCrew}); err == nil {
		t.Error("a crew chat named another crew")
	}
}

func TestProposedCrewAndWorkOrderCrew(t *testing.T) {
	chat := &model.ChatSpec{Agent: &model.ChatAgentSpec{Crews: []model.ChatCrewOption{{ID: peCrew, Name: "PE Diligence"}}}}
	got, err := proposedCrew(chat, map[string]any{"brief": "b", "crew": " pe diligence "})
	if err != nil || got["crew_id"] != peCrew || got["crew_name"] != "PE Diligence" {
		t.Errorf("by name = %v, %v", got, err)
	}
	if _, err := proposedCrew(chat, map[string]any{"brief": "b", "crew": 7}); err == nil {
		t.Error("a non-string crew was accepted")
	}
	if got, err := proposedCrew(&model.ChatSpec{}, map[string]any{"brief": "b"}); err != nil || len(got) != 1 {
		t.Errorf("crew chat = %v, %v", got, err)
	}
	if _, err := workOrderCrew(context.Background(), Env{}, Request{Crew: model.Crew{ID: peCrew}}, startWorkOrder{CrewID: aiCrew}); err == nil {
		t.Error("a crew chat proposal started another crew")
	}
	if id, err := workOrderCrew(context.Background(), Env{}, Request{Crew: model.Crew{ID: peCrew}}, startWorkOrder{}); err != nil || id != peCrew {
		t.Errorf("crew chat = %q, %v", id, err)
	}
	if _, _, err := normalizeStartWorkOrder(map[string]any{"brief": "b", "crew_id": "nope"}); err == nil {
		t.Error("a bad crew_id was accepted")
	}
}

func TestChatSpecialistsAndOwnTools(t *testing.T) {
	llm := "l1"
	lib := runspec.Library{
		LLMs:    map[string]model.LLM{llm: {ID: llm, Model: "gpt-4o"}},
		Pricing: []model.ModelPrice{{Model: "gpt-4o"}},
		Agents: map[string]model.Agent{
			"a": {ID: "a", Name: "A", Role: "Analyst", LLMID: &llm, Tools: []string{"google_search", "workspace_write", "google_search"}, MaxIter: 50,
				AllowDelegation: true, AreaIDs: []string{financeArea}},
			"b": {ID: "b", Name: "B", Role: ` "analyst" `, LLMID: &llm, AreaIDs: []string{financeArea}},
			"c": {ID: "c", Name: "C", Role: "Lead", LLMID: &llm, AreaIDs: []string{financeArea}},
			"d": {ID: "d", Name: "D", Role: "Hidden", LLMID: &llm, AreaIDs: []string{"elsewhere"}},
		},
	}
	p := &auth.Principal{AreaIDs: []string{financeArea}}
	lib.Tools = map[string]model.Tool{"google_search": {Effect: model.EffectReads}, "workspace_write": {Effect: model.EffectChanges}}
	got, err := chatSpecialists(lib, p, model.Agent{Role: "lead"}, []string{"a", "b", "c", "d", "missing"}, nil, nil)
	if err != nil || len(got) != 1 || got[0].ID != "a" || got[0].AllowDelegation || got[0].MaxIter != maxSpecialistIter ||
		!slices.Equal(got[0].Tools, []string{"google_search", AskLeadTool}) {
		t.Errorf("specialists = %+v, %v", got, err)
	}
	if model.ChatRoleKey(` Senior  "Analyst"`) != "senior analyst" {
		t.Errorf("role key = %q", model.ChatRoleKey(` Senior  "Analyst"`))
	}
	unpriced := lib
	unpriced.Pricing = nil
	if _, err := chatSpecialists(unpriced, p, model.Agent{Role: "lead"}, []string{"a"}, nil, nil); err == nil {
		t.Error("a specialist without a price was accepted")
	}
}

// chatToolsFor keeps, in the agent's order, the tools the catalog says only read and whose needs the turn meets.
func TestChatToolsFor(t *testing.T) {
	catalog := map[string]model.Tool{
		"google_search":     {Effect: model.EffectReads},
		"scrape_website":    {Effect: model.EffectReads},
		"workspace_read":    {Effect: model.EffectReads},
		"workspace_write":   {Effect: model.EffectChanges},
		"generate_video":    {Effect: model.EffectChanges},
		"notes_get":         {Effect: model.EffectReads, Needs: []string{model.NeedCrew}},
		"notes_put":         {Effect: model.EffectChanges, Needs: []string{model.NeedCrew}},
		"tracker_get_issue": {Effect: model.EffectReads, Needs: []string{model.NeedTracker}},
		"tracker_comment":   {Effect: model.EffectChanges, Needs: []string{model.NeedTracker}},
		"km_search":         {Effect: model.EffectReads, Needs: []string{model.NeedKM2}},
		"km_save_document":  {Effect: model.EffectChanges, Needs: []string{model.NeedKM2}},
		"deliver_report":    {Effect: model.EffectChanges, Needs: []string{model.NeedCrew, model.NeedSchedule}},
		"unclassified":      {},
	}
	all := []string{"notes_put", "km_search", "tracker_comment", "google_search", "tracker_get_issue", "workspace_write", "notes_get",
		"workspace_read", "generate_video", "deliver_report", "km_save_document", "unclassified", "missing", "google_search", "scrape_website"}
	cases := []struct {
		name string
		met  map[string]bool
		want []string
	}{
		{"no connections: only plain reads", nil, []string{"google_search", "workspace_read", "scrape_website"}},
		{"a tracker connection adds tracker reads", map[string]bool{model.NeedTracker: true},
			[]string{"google_search", "tracker_get_issue", "workspace_read", "scrape_website"}},
		{"a KM2 connection adds KM2 reads", map[string]bool{model.NeedKM2: true},
			[]string{"km_search", "google_search", "workspace_read", "scrape_website"}},
		{"crew and schedule reads still need what they say", map[string]bool{model.NeedTracker: true, model.NeedKM2: true, model.NeedCrew: true},
			[]string{"km_search", "google_search", "tracker_get_issue", "notes_get", "workspace_read", "scrape_website"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatToolsFor(all, catalog, tc.met, nil); !slices.Equal(got, tc.want) {
				t.Errorf("chatToolsFor = %v, want %v", got, tc.want)
			}
		})
	}
	if got := chatToolsFor(nil, catalog, nil, nil); got == nil || len(got) != 0 {
		t.Errorf("no tools = %#v", got)
	}

	// With a proposer, changing tools with a counterpart become that counterpart, when the needs are met and the person
	// holds Run crews; changing tools without one (workspace_write, notes_put, deliver_report) never reach chat
	runner := &auth.Principal{Permissions: map[string]bool{auth.CrewsChat: true, auth.CrewsRun: true}}
	chatter := &auth.Principal{Permissions: map[string]bool{auth.CrewsChat: true}}
	catalog["tracker_suggest_change"] = model.Tool{Effect: model.EffectChanges, Needs: []string{model.NeedTracker}}
	catalog["tracker_create_issue"] = model.Tool{Effect: model.EffectChanges, Needs: []string{model.NeedTracker}}
	both := map[string]bool{model.NeedTracker: true, model.NeedKM2: true}
	withChanges := append(slices.Clone(all), "tracker_suggest_change", "tracker_create_issue", "tracker_comment")
	propose := []struct {
		name     string
		met      map[string]bool
		proposer *auth.Principal
		want     []string
	}{
		{"every counterpart", both, runner, []string{"km_search", ProposeTrackerCommentTool, "google_search", "tracker_get_issue",
			"workspace_read", ProposeKM2SaveTool, "scrape_website", ProposeTrackerSuggestionTool, ProposeTrackerIssueTool}},
		{"only those whose connection is listed", map[string]bool{model.NeedKM2: true}, runner,
			[]string{"km_search", "google_search", "workspace_read", ProposeKM2SaveTool, "scrape_website"}},
		{"not without Run crews", both, chatter, []string{"km_search", "google_search", "tracker_get_issue", "workspace_read", "scrape_website"}},
	}
	for _, tc := range propose {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatToolsFor(withChanges, catalog, tc.met, tc.proposer); !slices.Equal(got, tc.want) {
				t.Errorf("chatToolsFor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNeedsMetAndUsedConnections(t *testing.T) {
	jira := model.RunConnection{ID: "j", Kind: model.TrackerJira}
	km2 := model.RunConnection{ID: "k", Kind: model.KindKM2}
	smtp := model.RunConnection{ID: "s", Kind: "smtp"}
	if met := needsMet([]model.RunConnection{jira, smtp}); !met[model.NeedTracker] || met[model.NeedKM2] || len(met) != 1 {
		t.Errorf("needs met = %v", met)
	}
	catalog := map[string]model.Tool{"tracker_get_issue": {Needs: []string{model.NeedTracker}}, "google_search": {}}
	agents := []model.Agent{{Tools: []string{"google_search"}}, {Tools: []string{"tracker_get_issue"}}}
	if got := usedConnections([]model.RunConnection{jira, km2}, agents, catalog); len(got) != 1 || got[0].ID != "j" {
		t.Errorf("used = %+v", got)
	}
	if got := usedConnections([]model.RunConnection{jira}, agents[:1], catalog); got == nil || len(got) != 0 {
		t.Errorf("no tool uses it = %#v", got)
	}
	// A turn reads KM2 through one connection only (KM2 reads cannot choose); a second listed one is left out
	catalog["km_search"] = model.Tool{Needs: []string{model.NeedKM2}}
	other := model.RunConnection{ID: "k2", Kind: model.KindKM2}
	both := []model.Agent{{Tools: []string{"km_search", "tracker_get_issue"}}}
	jira2 := model.RunConnection{ID: "j2", Kind: model.TrackerLinear}
	got := usedConnections([]model.RunConnection{km2, jira, other, jira2}, both, catalog)
	if ids := []string{got[0].ID, got[1].ID, got[2].ID}; len(got) != 3 || !slices.Equal(ids, []string{"k", "j", "j2"}) {
		t.Errorf("one KM2 = %+v", got)
	}
}

// Tracker and KM2 text is untrusted: in a turn where any agent reads it, no agent may fetch a URL, so injected text
// cannot send it out through Scrape Website. Google Search stays (its queries go to Google, not to a chosen URL).
func TestFetchIsDroppedBesidePrivateReads(t *testing.T) {
	catalog := map[string]model.Tool{
		"google_search":     {Effect: model.EffectReads},
		"scrape_website":    {Effect: model.EffectReads},
		"tracker_get_issue": {Effect: model.EffectReads, Needs: []string{model.NeedTracker}},
		"km_search":         {Effect: model.EffectReads, Needs: []string{model.NeedKM2}},
	}
	web := []string{"google_search", "scrape_website"}
	cases := []struct {
		name              string
		lead, specialist  []string
		wantLead, wantSpc []string
	}{
		{"web only keeps the fetch", web, web, web, web},
		{"the lead reads a tracker", []string{"tracker_get_issue", "scrape_website", StartCrewWorkOrderTool}, web,
			[]string{"tracker_get_issue", StartCrewWorkOrderTool}, []string{"google_search"}},
		{"a specialist reads KM2", web, []string{"km_search", "scrape_website"}, []string{"google_search"}, []string{"km_search"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lead := model.Agent{Tools: tc.lead}
			specialists := []model.Agent{{Tools: tc.specialist}}
			gotLead, gotSpecialists := withoutFetchBesidePrivateReads(lead, specialists, catalog)
			if !slices.Equal(gotLead.Tools, tc.wantLead) || !slices.Equal(gotSpecialists[0].Tools, tc.wantSpc) {
				t.Errorf("lead %v, specialist %v", gotLead.Tools, gotSpecialists[0].Tools)
			}
			if !slices.Equal(lead.Tools, tc.lead) || !slices.Equal(specialists[0].Tools, tc.specialist) {
				t.Error("the agents passed in were changed")
			}
		})
	}
}

// keyTools is searchTools that also says which provider keys are set, as the runner's catalog does.
type keyTools struct {
	searchTools
	set map[string]bool
}

func (k keyTools) EnvSet(keys ...string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, key := range keys {
		out[key] = k.set[key]
	}
	return out, nil
}

func TestAgentChatNeedsProviderKeys(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.agentOn(t, []string{investigator}, nil)
	e.svc = New(e.st, launch.New(e.st, keyTools{}, e.runs), e.runs)
	_, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "hi")
	wantProblem(t, err, `The agent "Deal Partner" uses GPT-6 Luna (memo) (openai/gpt-6-luna), whose provider key OPENAI_API_KEY is not set`)
	wantProblem(t, err, `The specialist "Corporate Investigator"`)
	e.svc = New(e.st, launch.New(e.st, keyTools{set: map[string]bool{"OPENAI_API_KEY": true}}, e.runs), e.runs)
	if _, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "hi"); err != nil {
		t.Errorf("with the key set = %v", err)
	}
}

func TestKeyProblems(t *testing.T) {
	luna, haiku := "l1", "l2"
	llms := map[string]model.LLM{luna: {ID: luna, Name: "Luna", Model: "openai/luna"}, haiku: {ID: haiku, Name: "Haiku", Model: "anthropic/haiku"}}
	lead := model.Agent{Name: "Lead", LLMID: &luna}
	specialists := []model.Agent{{Name: "Scout", LLMID: &haiku}, {Name: "Clerk", LLMID: &luna}}
	cases := []struct {
		name    string
		missing map[string]string
		want    []string
	}{
		{"nothing missing", map[string]string{}, nil},
		{"the specialist's provider", map[string]string{"anthropic/haiku": "ANTHROPIC_API_KEY"},
			[]string{`The specialist "Scout" uses Haiku (anthropic/haiku), whose provider key ANTHROPIC_API_KEY is not set in the provider env file; whoever runs Crew Studio can add it.`}},
		{"the lead's provider names everyone on it", map[string]string{"openai/luna": "OPENAI_API_KEY"},
			[]string{`The agent "Lead" uses Luna (openai/luna), whose provider key OPENAI_API_KEY is not set in the provider env file; whoever runs Crew Studio can add it.`,
				`The specialist "Clerk" uses Luna (openai/luna), whose provider key OPENAI_API_KEY is not set in the provider env file; whoever runs Crew Studio can add it.`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keyProblems(tc.missing, lead, llms[luna], specialists, llms); !slices.Equal(got, tc.want) {
				t.Errorf("keyProblems = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProposedInputProblems(t *testing.T) {
	crew := model.Crew{Inputs: []model.CrewInput{{Key: "company", Required: true}, {Key: "region", Required: true, Default: "US"},
		{Key: "location"}}}
	cases := []struct {
		name   string
		crew   model.Crew
		inputs map[string]string
		want   []string
	}{
		{"all given", crew, map[string]string{"company": "Acme", "location": "Spokane"}, nil},
		{"a required input with a default may be left out", crew, map[string]string{"company": "Acme"}, nil},
		{"a required input without a default", crew, map[string]string{"location": "x"}, []string{"inputs.company: is required"}},
		{"given empty counts as left out", crew, map[string]string{"company": " "}, []string{"inputs.company: is required"}},
		{"an unknown key names the crew's keys", crew, map[string]string{"company": "Acme", "ticker": "A"},
			[]string{"inputs.ticker: the crew has no input with this key", "inputs: this crew's input keys are company, region, location"}},
		{"a crew without inputs", model.Crew{}, map[string]string{"topic": "x"},
			[]string{"inputs.topic: the crew has no input with this key", "inputs: this crew has no inputs"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := proposedInputProblems(tc.crew, tc.inputs)
			if !slices.Equal(got, tc.want) {
				t.Errorf("proposedInputProblems = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCrewChatProposalsFitTheCrewsInputs(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.turnOn(t, nil, 1)
	turn, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "run the diligence on Acme")
	if err != nil {
		t.Fatal(err)
	}
	// The frozen crew has no inputs (a chat turn is not a run of the crew), so the turn carries them for the prompt
	spec, err := e.st.GetRunSpec(ctx, turn.Run.ID)
	if err != nil || len(spec.Crew.Inputs) != 0 || len(spec.Chat.CrewInputs) != 2 || spec.Chat.CrewInputs[0] !=
		(model.ChatInput{Key: "company", Label: "Company legal name", Required: true}) {
		t.Errorf("crew inputs = %+v / %+v, %v", spec.Crew.Inputs, spec.Chat.CrewInputs, err)
	}
	scope := e.running(t, turn.Run.ID)
	_, err = e.svc.Propose(ctx, scope, TypeStartWorkOrder, map[string]any{"brief": "Diligence", "inputs": map[string]any{"ticker": "ACME"}})
	wantInvalid(t, err, "inputs.ticker: the crew has no input with this key")
	if _, err := e.st.Pool().Exec(ctx, `UPDATE crews SET inputs = jsonb_set(inputs, '{0,default}', '""') WHERE id = $1`, peCrew); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Propose(ctx, scope, TypeStartWorkOrder, map[string]any{"brief": "Diligence"})
	wantInvalid(t, err, "inputs.company: is required")
	if _, err := e.svc.Propose(ctx, scope, TypeStartWorkOrder, map[string]any{"brief": "Diligence", "inputs": map[string]any{"company": "Acme"}}); err != nil {
		t.Errorf("a proposal with its required input = %v", err)
	}
}

func wantInvalid(t *testing.T, err error, detail string) {
	t.Helper()
	var invalid *model.ValidationError
	if !errors.As(err, &invalid) || !slices.Contains(invalid.Details, detail) {
		t.Errorf("err = %v, want a validation error with %q", err, detail)
	}
}

// An agent reads trackers and KM2 in chat only through the connections on its list that are enabled and that the
// person and the agent share an area with; the turn freezes those it has a tool for. Changing tools never reach it.
func TestAgentChatReadsThroughItsListedConnections(t *testing.T) {
	e := agentEnv(t)
	ctx := context.Background()
	const otherArea = "55555555-0000-4000-8000-000000000002"
	conn := func(id, name, kind, area string, enabled bool) string {
		t.Helper()
		c, err := e.st.CreateConnection(ctx, id, model.ConnectionInput{Name: name, Kind: kind, AreaIDs: []string{area}, Enabled: &enabled,
			Config: map[string]string{"site_url": "https://x.example"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	jira := conn("99999999-0000-4000-8000-00000000000a", "Finance Jira", model.TrackerJira, financeArea, true)
	other := conn("99999999-0000-4000-8000-00000000000b", "Other Jira", model.TrackerJira, otherArea, true)
	off := conn("99999999-0000-4000-8000-00000000000c", "Old Jira", model.TrackerJira, financeArea, false)
	km2 := conn("99999999-0000-4000-8000-00000000000d", "Finance KM2", model.KindKM2, financeArea, true)
	mail := conn("99999999-0000-4000-8000-00000000000e", "Mail", "http", financeArea, true)
	if _, err := e.st.Pool().Exec(ctx, `UPDATE agents SET tools = $2 WHERE id = $1`, dealPartner,
		[]string{"notes_get", "tracker_comment", "tracker_get_issue", "google_search", "scrape_website"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE agents SET tools = $2 WHERE id = $1`, investigator, []string{"km_search", "scrape_website", "tracker_get_issue"}); err != nil {
		t.Fatal(err)
	}
	put := func(connections ...string) {
		t.Helper()
		if _, err := e.st.PutAgentChatSettings(ctx, model.AgentChatSettings{AgentID: dealPartner, Enabled: true, SpecialistIDs: []string{investigator},
			ConnectionIDs: connections, DailyBudgetUSD: 1, PersonDailyBudgetUSD: 5}, ""); err != nil {
			t.Fatal(err)
		}
	}
	turnSpec := func() model.RunSpec {
		t.Helper()
		turn, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "What is PRJ-11 about?")
		if err != nil {
			t.Fatal(err)
		}
		spec, err := e.st.GetRunSpec(ctx, turn.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		return spec
	}

	put(other, off, mail, jira, km2)
	spec := turnSpec()
	lead, specialist := spec.Agents[0], spec.Agents[1]
	if !slices.Equal(lead.Tools, []string{"tracker_get_issue", "google_search"}) || !slices.Equal(specialist.Tools, []string{"km_search", "tracker_get_issue", AskLeadTool}) {
		t.Errorf("lead tools = %v, specialist tools = %v", lead.Tools, specialist.Tools)
	}
	if want := []model.RunConnection{{ID: jira, Name: "Finance Jira", Kind: model.TrackerJira}, {ID: km2, Name: "Finance KM2", Kind: model.KindKM2}}; !slices.Equal(spec.Chat.Connections, want) {
		t.Errorf("frozen connections = %+v", spec.Chat.Connections)
	}

	// Without a listed tracker connection the tracker reads go, and with no private reads the fetch comes back; with
	// no tool using it, the KM2 connection is not frozen
	put(other, km2)
	if _, err := e.st.Pool().Exec(ctx, `UPDATE agents SET tools = '{tracker_get_issue}' WHERE id = $1`, investigator); err != nil {
		t.Fatal(err)
	}
	spec = turnSpec()
	if !slices.Equal(spec.Agents[0].Tools, []string{"google_search", "scrape_website"}) || !slices.Equal(spec.Agents[1].Tools, []string{AskLeadTool}) ||
		len(spec.Chat.Connections) != 0 {
		t.Errorf("tools = %v / %v, connections = %+v", spec.Agents[0].Tools, spec.Agents[1].Tools, spec.Chat.Connections)
	}
}

// Crew chat's front desk is refused when its model's provider key is missing, as agent chat is: its calls would fail
// to authenticate after the turn started.
func TestCrewChatNeedsTheFrontDesksProviderKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.turnOn(t, nil, 1)
	e.svc = New(e.st, launch.New(e.st, keyTools{}, e.runs), e.runs)
	_, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "hi")
	wantProblem(t, err, `The front desk agent "`)
	wantProblem(t, err, "whose provider key OPENAI_API_KEY is not set")
	e.svc = New(e.st, launch.New(e.st, keyTools{set: map[string]bool{"OPENAI_API_KEY": true}}, e.runs), e.runs)
	if _, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, "hi"); err != nil {
		t.Errorf("with the key set = %v", err)
	}
}
