package chat

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/connections"
	"crewstudio/internal/km2"
	"crewstudio/internal/km2/km2test"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
	"crewstudio/internal/tracker/trackertest"
)

const counterpartKM2Key = "km2_counterpartkey0000000000000000"

// proposeEnv is an agent chat whose lead (the deal partner) may comment on and create tracker issues and save to KM2,
// and whose specialist (the analyst) may comment and suggest changes, through a fake Jira and a fake KM2. Ann holds Run
// crews, which the cards need.
type proposeEnv struct {
	env
	jira, km2Conn model.Connection
	tracker       *trackertest.Data
	km2           *km2test.Data
}

func newProposeEnv(t *testing.T) proposeEnv {
	t.Helper()
	return newProposeEnvWith(t, nil)
}

// newProposeEnvWith is newProposeEnv with the fake Jira's handler wrapped (nil: as it is).
func newProposeEnvWith(t *testing.T, wrap func(http.Handler) http.Handler) proposeEnv {
	t.Helper()
	e := agentEnv(t)
	ctx := context.Background()
	e.analystRole(t)
	e.ann.Permissions = maps.Clone(e.ann.Permissions)
	e.ann.Permissions[auth.CrewsRun] = true
	d := trackertest.NewData()
	d.Add(trackertest.Issue{Key: "PRJ-10", Title: "Atlas"})
	jiraHandler := trackertest.Jira(d, "bot@example.com", "tok")
	if wrap != nil {
		jiraHandler = wrap(jiraHandler)
	}
	srv := trackertest.Server(t, jiraHandler)
	kd := km2test.NewData(counterpartKM2Key)
	ksrv := km2test.Server(t, kd)
	box, err := secrets.NewBox(secrets.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	conns := connections.New(e.st, box)
	jira, err := conns.Create(ctx, model.ConnectionInput{Name: "Acme Jira", Kind: model.TrackerJira, AreaIDs: []string{financeArea},
		Config: map[string]string{"site_url": srv.URL, "email": "bot@example.com", model.ConfigAllowCreate: "true"}, Secrets: map[string]string{"api_token": "tok"}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := conns.Create(ctx, model.ConnectionInput{Name: "Finance KM2", Kind: model.KindKM2, AreaIDs: []string{financeArea},
		Config: map[string]string{"base_url": ksrv.URL, model.ConfigKM2DefaultFolder: km2test.FolderAtlas}, Secrets: map[string]string{"api_key": counterpartKM2Key}})
	if err != nil {
		t.Fatal(err)
	}
	e.svc.UseTrackers(tracker.NewService(e.st, conns, tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client()}))
	e.svc.UseKM2(km2.NewService(e.st, conns, km2.Options{HTTP: ksrv.Client()}))
	for id, tools := range map[string][]string{dealPartner: {"tracker_comment", "tracker_create_issue", "km_save_document", "google_search"},
		analyst: {"tracker_comment", "tracker_suggest_change", "workspace_write"}} {
		if _, err := e.st.Pool().Exec(ctx, `UPDATE agents SET tools = $2 WHERE id = $1`, id, tools); err != nil {
			t.Fatal(err)
		}
	}
	pe := proposeEnv{env: e, jira: jira, km2Conn: k, tracker: d, km2: kd}
	pe.settings(t, nil)
	return pe
}

// settings puts the agent's chat on with the analyst and both connections, changed by change.
func (e proposeEnv) settings(t *testing.T, change func(*model.AgentChatSettings)) {
	t.Helper()
	s := model.AgentChatSettings{AgentID: dealPartner, Enabled: true, SpecialistIDs: []string{analyst}, DailyBudgetUSD: 1, PersonDailyBudgetUSD: 5,
		ConnectionIDs: []string{e.jira.ID, e.km2Conn.ID}, HandoffsEnabled: true, HandoffBudgetUSD: 0.4, HandoffTimeoutMinutes: 15, MaxActiveHandoffs: 2}
	if change != nil {
		change(&s)
	}
	if _, err := e.st.PutAgentChatSettings(context.Background(), s, ""); err != nil {
		t.Fatal(err)
	}
}

// turn starts a conversation and marks its run running, so it may propose.
func (e proposeEnv) turn(t *testing.T) (Turn, model.RunSpec, store.RunScope) {
	t.Helper()
	ctx := context.Background()
	turn, err := e.svc.StartAgentConversation(ctx, e.ann, dealPartner, "Post the summary on PRJ-10")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := e.st.GetRunSpec(ctx, turn.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return turn, spec, e.running(t, turn.Run.ID)
}

// The lead gets the counterparts of its own changing tools; the specialist gets its own only when the builder lets
// specialists propose. The connections they post through are frozen into the turn.
func TestCounterpartsReachChatOnlyAsProposals(t *testing.T) {
	e := newProposeEnv(t)
	_, spec, _ := e.turn(t)
	lead, specialist := spec.Agents[0], spec.Agents[1]
	if want := []string{ProposeTrackerCommentTool, ProposeTrackerIssueTool, ProposeKM2SaveTool, "google_search", StartHandoffTool}; !slices.Equal(lead.Tools, want) {
		t.Errorf("lead tools = %v, want %v", lead.Tools, want)
	}
	if !slices.Equal(specialist.Tools, []string{AskLeadTool}) {
		t.Errorf("specialist tools without the setting = %v", specialist.Tools)
	}
	if len(spec.Chat.Connections) != 2 {
		t.Errorf("frozen connections = %+v", spec.Chat.Connections)
	}

	e.settings(t, func(s *model.AgentChatSettings) { s.SpecialistsMayPropose = true })
	_, spec, _ = e.turn(t)
	if want := []string{ProposeTrackerCommentTool, ProposeTrackerSuggestionTool, AskLeadTool}; !slices.Equal(spec.Agents[1].Tools, want) {
		t.Errorf("specialist tools = %v, want %v", spec.Agents[1].Tools, want)
	}

	// Without Run crews nobody gets them
	delete(e.ann.Permissions, auth.CrewsRun)
	_, spec, _ = e.turn(t)
	if slices.ContainsFunc(turnTools(spec), func(k string) bool { return strings.HasPrefix(k, "propose_") }) {
		t.Errorf("tools without Run crews = %v", turnTools(spec))
	}
}

// A specialist's comment card names the connection and who drafted it; confirming posts it on the issue through the
// connection, saying who confirmed it.
func TestASpecialistsCommentIsPostedAsThePerson(t *testing.T) {
	e := newProposeEnv(t)
	ctx := context.Background()
	e.settings(t, func(s *model.AgentChatSettings) { s.SpecialistsMayPropose = true })
	turn, _, scope := e.turn(t)

	// The model names the connection, never its id or who proposed; a connection the turn does not have is refused
	_, err := e.svc.ProposeAs(ctx, scope, "Financial analyst", TypeTrackerComment, map[string]any{"issue": "PRJ-10", "text": "x", "connection": "Other Jira"})
	wantInvalid(t, err, "connection: must be one of Acme Jira")
	_, err = e.svc.ProposeAs(ctx, scope, "Financial analyst", TypeTrackerComment, map[string]any{"issue": "PRJ-10", "text": "x",
		"connection_id": e.jira.ID})
	wantInvalid(t, err, "connection: name the connection in connection")
	_, err = e.svc.ProposeAs(ctx, scope, "Financial analyst", TypeTrackerComment, map[string]any{"issue": "PRJ-10", "text": "x", "mentions": []string{"a"}})
	wantInvalid(t, err, `payload: json: unknown field "mentions"`)

	m, err := e.svc.ProposeAs(ctx, scope, " financial  ANALYST ", TypeTrackerComment, map[string]any{"issue": "prj-10", "text": "Summary posted.", "connection": ""})
	if err != nil {
		t.Fatal(err)
	}
	p := m.Proposal.Payload
	if m.Proposal.Summary != "Comment on PRJ-10" || p["connection_id"] != e.jira.ID || p["connection_name"] != "Acme Jira" ||
		p["proposed_by"] != "Analyst" || p["proposed_by_agent_id"] != analyst || p["issue"] != "PRJ-10" || m.RunID == nil || *m.RunID != turn.Run.ID {
		t.Fatalf("card = %+v", m)
	}
	done, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if done.ProposalResult["issue"] != "PRJ-10" || done.ProposalResult["connection"] != "Acme Jira" || done.ProposalResult["comment_id"] == "" {
		t.Errorf("result = %v", done.ProposalResult)
	}
	posted := e.tracker.PostedComments()
	if len(posted) != 1 || posted[0].Issue != "PRJ-10" {
		t.Fatalf("posted = %+v", posted)
	}
	body := fmt.Sprint(posted[0].Body)
	if !strings.Contains(body, "Summary posted.") || !strings.Contains(body, "Posted by Ann from a Crew Studio agent chat; drafted by Analyst.") {
		t.Errorf("body = %s", body)
	}
}

// Confirming checks again, as the person, what the card goes through; a refused card stays pending.
func TestCounterpartConfirmRefusals(t *testing.T) {
	cases := []struct {
		name  string
		role  string
		setup func(t *testing.T, e proposeEnv)
		check func(t *testing.T, err error)
	}{
		{"the connection is off the agent's list", "Private Equity Deal Partner", func(t *testing.T, e proposeEnv) {
			e.settings(t, func(s *model.AgentChatSettings) {
				s.ConnectionIDs = []string{e.km2Conn.ID}
				s.SpecialistsMayPropose = true
			})
		}, func(t *testing.T, err error) { wantConflictCode(t, err, "connection_not_offered") }},
		{"the connection was disabled", "Private Equity Deal Partner", func(t *testing.T, e proposeEnv) {
			if _, err := e.st.Pool().Exec(context.Background(), `UPDATE connections SET enabled = false WHERE id = $1`, e.jira.ID); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, err error) { wantConflictCode(t, err, "connection_off") }},
		{"the person left the connection's area", "Private Equity Deal Partner", func(t *testing.T, e proposeEnv) {
			if _, err := e.st.Pool().Exec(context.Background(), `UPDATE connections SET area_ids = '{55555555-0000-4000-8000-000000000002}'
				WHERE id = $1`, e.jira.ID); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, err error) { wantConflictCode(t, err, "connection_not_offered") }},
		{"specialists may no longer propose", "Financial analyst", func(t *testing.T, e proposeEnv) {
			e.settings(t, nil)
		}, func(t *testing.T, err error) { wantConflictCode(t, err, "proposals_off") }},
		{"the specialist was taken off the agent's list", "Financial analyst", func(t *testing.T, e proposeEnv) {
			e.settings(t, func(s *model.AgentChatSettings) { s.SpecialistIDs = []string{}; s.SpecialistsMayPropose = true })
		}, func(t *testing.T, err error) { wantConflictCode(t, err, "proposer_not_offered") }},
		{"the specialist lost its tracker tool", "Financial analyst", func(t *testing.T, e proposeEnv) {
			if _, err := e.st.Pool().Exec(context.Background(), `UPDATE agents SET tools = '{google_search}' WHERE id = $1`, analyst); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, err error) { wantConflictCode(t, err, "proposer_not_offered") }},
		{"the lead lost its tracker tool", "Private Equity Deal Partner", func(t *testing.T, e proposeEnv) {
			if _, err := e.st.Pool().Exec(context.Background(), `UPDATE agents SET tools = '{google_search}' WHERE id = $1`, dealPartner); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, err error) { wantConflictCode(t, err, "proposer_not_offered") }},
		{"chat was turned off", "Private Equity Deal Partner", func(t *testing.T, e proposeEnv) {
			e.settings(t, func(s *model.AgentChatSettings) { s.Enabled = false })
		}, func(t *testing.T, err error) { wantConflictCode(t, err, "chat_off") }},
		{"the person lost Run crews", "Private Equity Deal Partner", func(t *testing.T, e proposeEnv) {
			delete(e.ann.Permissions, auth.CrewsRun)
		}, func(t *testing.T, err error) {
			var forbidden *launch.ForbiddenError
			if !errors.As(err, &forbidden) || forbidden.Permission != auth.CrewsRun {
				t.Errorf("err = %v, want forbidden crews.run", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newProposeEnv(t)
			ctx := context.Background()
			e.settings(t, func(s *model.AgentChatSettings) { s.SpecialistsMayPropose = true })
			_, _, scope := e.turn(t)
			m, err := e.svc.ProposeAs(ctx, scope, tc.role, TypeTrackerComment, map[string]any{"issue": "PRJ-10", "text": "Hello"})
			if err != nil {
				t.Fatal(err)
			}
			tc.setup(t, e)
			_, err = e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
			tc.check(t, err)
			if again, _, _ := e.st.GetMessage(ctx, m.ID); again.ProposalState == nil || *again.ProposalState != model.ProposalPending {
				t.Errorf("state after a refusal = %v", again.ProposalState)
			}
			if len(e.tracker.PostedComments()) != 0 {
				t.Error("something was posted")
			}
		})
	}
}

// A suggestion is posted as a comment and changes no field; a new issue needs the connection's "Let crews create
// issues", at the proposal and again at the confirm.
func TestSuggestionAndNewIssueCards(t *testing.T) {
	e := newProposeEnv(t)
	ctx := context.Background()
	e.settings(t, func(s *model.AgentChatSettings) { s.SpecialistsMayPropose = true })
	_, _, scope := e.turn(t)
	m, err := e.svc.ProposeAs(ctx, scope, "Financial analyst", TypeTrackerSuggestion,
		map[string]any{"issue": "PRJ-10", "field": "due_date", "value": "2026-11-01", "reason": "The vendor slipped."})
	if err != nil {
		t.Fatal(err)
	}
	if m.Proposal.Summary != "Suggest a change on PRJ-10: due date to 2026-11-01" {
		t.Errorf("summary = %q", m.Proposal.Summary)
	}
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if posted := e.tracker.PostedComments(); len(posted) != 1 || !strings.Contains(fmt.Sprint(posted[0].Body), `Suggested change from Ann (Crew Studio): set due date to "2026-11-01"`) {
		t.Errorf("posted = %+v", posted)
	}

	_, _, scope = e.turn(t)
	issue := map[string]any{"project": "prj", "title": "Chase the vendor", "description": "They slipped."}
	m, err = e.svc.ProposeAs(ctx, scope, "Private Equity Deal Partner", TypeTrackerIssue, issue)
	if err != nil {
		t.Fatal(err)
	}
	if m.Proposal.Summary != "Create an issue in PRJ: Chase the vendor" || m.Proposal.Payload["proposed_by_agent_id"] != dealPartner {
		t.Errorf("card = %+v", m.Proposal)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE connections SET config = config - 'allow_create' WHERE id = $1`, e.jira.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	wantConflictCode(t, err, "create_off")
	_, _, scope = e.turn(t)
	_, err = e.svc.ProposeAs(ctx, scope, "Private Equity Deal Partner", TypeTrackerIssue, issue)
	wantConflictCode(t, err, "create_off")
	if _, err := e.st.Pool().Exec(ctx, `UPDATE connections SET config = config || '{"allow_create": "true"}' WHERE id = $1`, e.jira.ID); err != nil {
		t.Fatal(err)
	}
	done, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if done.ProposalResult["issue"] != "NEW-1" || len(e.tracker.Created) != 1 {
		t.Errorf("result = %v, created = %+v", done.ProposalResult, e.tracker.Created)
	}
	if !strings.Contains(fmt.Sprint(e.tracker.Created[0]), "Posted by Ann from a Crew Studio agent chat; drafted by Deal Partner.") {
		t.Errorf("created = %+v", e.tracker.Created[0])
	}
}

// A KM2 card saves to the connection's default folder, under a document id kept apart from crews' own and from other
// conversations'.
func TestKM2SaveCard(t *testing.T) {
	e := newProposeEnv(t)
	ctx := context.Background()
	turn, _, scope := e.turn(t)
	ref := conversationPrefix(turn.Conversation.ID) + "vendor-comparison"
	m, err := e.svc.ProposeAs(ctx, scope, "Private Equity Deal Partner", TypeKM2Save,
		map[string]any{"title": "Vendor comparison", "external_ref": "vendor-comparison", "content": "# Vendors\nAcme wins."})
	if err != nil {
		t.Fatal(err)
	}
	if m.Proposal.Summary != "Save to KM2: Vendor comparison" || m.Proposal.Payload["external_ref"] != ref ||
		m.Proposal.Payload["connection_id"] != e.km2Conn.ID {
		t.Errorf("card = %+v", m.Proposal)
	}
	done, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	saved := e.km2.Saved()
	if len(saved) != 1 || saved[0] != [4]string{km2test.FolderAtlas, ref, "Vendor comparison", "# Vendors\nAcme wins."} {
		t.Errorf("saved = %v", saved)
	}
	if done.ProposalResult["document_id"] == "" || done.ProposalResult["outcome"] != "created" {
		t.Errorf("result = %v", done.ProposalResult)
	}
	// The person's name and the drafting agent go with the document
	reqs := e.km2.Requests()
	meta, _ := reqs[len(reqs)-1].Body["metadata"].(map[string]any)
	if meta["saved_by"] != "Ann" || meta["drafted_by"] != "Deal Partner" || meta["source"] != "crew-studio" {
		t.Errorf("metadata = %v", meta)
	}

	// Another conversation's id is kept inside this one's prefix, and a card carrying another conversation's id (as a
	// tampered payload would) is refused on confirm
	other, _, scope := e.turn(t)
	m, err = e.svc.ProposeAs(ctx, scope, "Private Equity Deal Partner", TypeKM2Save,
		map[string]any{"title": "Vendor comparison", "external_ref": ref, "content": "Replaced."})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Proposal.Payload["external_ref"]; got != conversationPrefix(other.Conversation.ID)+ref {
		t.Errorf("another conversation's ref = %v", got)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE conversation_messages SET proposal = jsonb_set(proposal, '{payload,external_ref}', to_jsonb($2::text))
		WHERE id = $1`, m.ID, ref); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	wantInvalid(t, err, "external_ref: a card may only save under its own conversation's ids")
	_, err = conversationRef(other.Conversation.ID, strings.Repeat("r", model.MaxKM2ExternalRefLen))
	wantInvalid(t, err, fmt.Sprintf("external_ref: at most %d characters in a chat card", model.MaxKM2ExternalRefLen-len(conversationPrefix(other.Conversation.ID))))
}

// Only an agent of the turn that was given the counterpart may propose it, and Studio must be able to tell which.
func TestOnlyTheAgentWithTheCounterpartProposes(t *testing.T) {
	e := newProposeEnv(t)
	ctx := context.Background()
	_, _, scope := e.turn(t)
	comment := map[string]any{"issue": "PRJ-10", "text": "x"}
	var denied *DeniedError
	for name, role := range map[string]string{"no role": "", "an unknown role": "Somebody", "a specialist without the setting": "Financial analyst"} {
		if _, err := e.svc.ProposeAs(ctx, scope, role, TypeTrackerComment, comment); !errors.As(err, &denied) {
			t.Errorf("%s = %v", name, err)
		}
	}
	// The lead's own card needs no setting, and stays confirmable without it
	m, err := e.svc.ProposeAs(ctx, scope, "Private Equity Deal Partner", TypeTrackerComment, comment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now()); err != nil {
		t.Errorf("the lead's card = %v", err)
	}
}

// A write whose answer never came may have been made: the card stays confirmed, saying so, rather than being offered
// again; a clear refusal leaves it pending.
func TestUnknownOutcomes(t *testing.T) {
	where := map[string]any{"issue": "PRJ-10"}
	for _, err := range []error{context.DeadlineExceeded, &tracker.Error{Status: 502}, &tracker.Error{Unreachable: true}, &km2.Error{Status: 0},
		&km2.Error{Status: 503}, &km2.Error{Status: 408}, &tracker.Error{Message: "answered with something that is not the JSON Jira sends"},
		&tracker.Error{Status: 404, NotFound: true}, errors.New("other")} {
		got, gerr := written(nil, err, where)
		if gerr != nil || got["outcome"] != "unknown" || got["issue"] != "PRJ-10" || got["message"] != errOutcomeUnknown {
			t.Errorf("%v = %v, %v", err, got, gerr)
		}
	}
	for _, err := range []error{&tracker.Error{Status: 404, NotFound: true, Rejected: true}, &tracker.Error{Status: 429, RateLimited: true, Rejected: true},
		&km2.Error{Status: 422}, &km2.Error{Status: 429, RateLimited: true}, &km2.FolderError{Message: "no folder"}} {
		if _, gerr := written(nil, err, where); gerr != err {
			t.Errorf("%v = %v", err, gerr)
		}
	}
	if got, err := written(where, nil, nil); err != nil || got["issue"] != "PRJ-10" {
		t.Errorf("success = %v, %v", got, err)
	}
}

// A hand-off's proposals are messages of their own, placed after its result in the thread and at most five per
// hand-off; its result is still the hand-off's first message.
func TestHandoffProposalsAreMessagesOfTheirOwn(t *testing.T) {
	e := newProposeEnv(t)
	ctx := context.Background()
	e.settings(t, func(s *model.AgentChatSettings) { s.SpecialistsMayPropose = true })
	turn, m := e.proposeHandoff(t)
	done, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	runID, _ := done.ProposalResult["run_id"].(string)
	handoffID, _ := done.ProposalResult["handoff_id"].(string)
	resultID, _ := done.ProposalResult["message_id"].(string)
	hspec, _ := e.st.GetRunSpec(ctx, runID)
	if !slices.Contains(hspec.Agents[1].Tools, ProposeTrackerCommentTool) {
		t.Errorf("the hand-off's specialist = %v", hspec.Agents[1].Tools)
	}
	scope := store.RunScope{RunID: runID, Kind: model.RunKindHandoff}
	propose := func() (model.ChatMessage, error) {
		return e.svc.ProposeAs(ctx, scope, "Financial analyst", TypeTrackerComment, map[string]any{"issue": "PRJ-10", "text": "Done."})
	}
	if _, err := propose(); err == nil || !strings.Contains(err.Error(), "finished") {
		t.Errorf("a queued hand-off proposing = %v", err)
	}
	if ok, err := e.st.StartRun(ctx, runID); err != nil || !ok {
		t.Fatal(err)
	}
	var cards []model.ChatMessage
	for range store.MaxHandoffProposals {
		c, err := propose()
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, c)
	}
	_, err = propose()
	wantConflictCode(t, err, "too_many_proposals")
	first := cards[0]
	if first.ID == resultID || first.RunID != nil || first.Handoff == nil || first.Handoff.ID != handoffID || !first.HandoffProposal ||
		first.Content != "Comment on PRJ-10" || first.ProposalState == nil || *first.ProposalState != model.ProposalPending {
		t.Errorf("hand-off card = %+v", first)
	}
	h, err := e.st.GetHandoff(ctx, handoffID)
	if err != nil || h.ResultMessageID != resultID {
		t.Errorf("result message = %q, %v", h.ResultMessageID, err)
	}
	result, _, _ := e.st.GetMessage(ctx, resultID)
	if result.HandoffProposal || result.Proposal != nil {
		t.Errorf("result = %+v", result)
	}
	// The next turn is told about the cards as cards, not as the hand-off's result
	turns, err := e.st.RecentTurns(ctx, turn.Conversation.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, tt := range turns {
		kinds[tt.Kind]++
	}
	if kinds[model.ChatTurnHandoffProposal] != store.MaxHandoffProposals || kinds[model.ChatTurnHandoffResult] != 0 {
		t.Errorf("turn kinds = %v", kinds)
	}
	// Confirming a hand-off's card posts it like any other
	if _, err := e.svc.Decide(ctx, e.ann, first.ID, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(e.tracker.PostedComments()) != 1 {
		t.Errorf("posted = %+v", e.tracker.PostedComments())
	}
	// The result is labelled once the run wrote it, and the run's deletion does not mix it up with the cards
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'succeeded', output = 'The comparison.' WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SettleReplies(ctx, turn.Conversation.ID, ProposalTTL); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Pool().Exec(ctx, `DELETE FROM runs WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	if h, err := e.st.GetHandoff(ctx, handoffID); err != nil || h.ResultMessageID != resultID {
		t.Errorf("after the run's deletion: result message = %q, %v", h.ResultMessageID, err)
	}
	turns, _ = e.st.RecentTurns(ctx, turn.Conversation.ID, 50)
	results := slices.DeleteFunc(turns, func(tt model.ChatTurn) bool { return tt.Kind != model.ChatTurnHandoffResult })
	if len(results) != 1 || results[0].Content != "The comparison." {
		t.Errorf("results = %+v", results)
	}
}

func TestCounterpartNormalize(t *testing.T) {
	via := map[string]any{"connection_id": "99999999-0000-4000-8000-00000000000a", "connection_name": "Acme Jira", "proposed_by": "",
		"proposed_by_agent_id": ""}
	with := func(extra map[string]any) map[string]any {
		out := maps.Clone(via)
		maps.Copy(out, extra)
		return out
	}
	long := strings.Repeat("x", model.MaxTrackerCommentChars)
	cases := []struct {
		name, typ string
		payload   map[string]any
		problem   string
	}{
		{"comment without an issue key", TypeTrackerComment, with(map[string]any{"issue": "?", "text": "x"}), "issue: use the issue's key, such as PRJ-10"},
		{"empty comment", TypeTrackerComment, with(map[string]any{"issue": "A-1", "text": " "}), "text: is required"},
		{"comment leaves room to say who posted it", TypeTrackerComment, with(map[string]any{"issue": "A-1", "text": long}),
			fmt.Sprintf("text: must be at most %d characters", model.MaxTrackerCommentChars-attributionRoom)},
		{"no connection id", TypeTrackerComment, map[string]any{"issue": "A-1", "text": "x"}, "connection_id: must be the connection's id"},
		{"a suggestion of an unknown field", TypeTrackerSuggestion, with(map[string]any{"issue": "A-1", "field": "colour", "value": "v", "reason": "r"}),
			"field: must be one of due_date, assignee, status, priority, title, labels, parent"},
		{"an issue without a project", TypeTrackerIssue, with(map[string]any{"project": "", "title": "t"}),
			"project: give the Jira project key, Linear team key or KM2 project key, such as PRJ"},
		{"a KM2 document without content", TypeKM2Save, with(map[string]any{"title": "t", "external_ref": "r", "content": " "}),
			"content: is required (give content or a workspace_file)"},
		{"a KM2 document too long for a card", TypeKM2Save, with(map[string]any{"title": "t", "external_ref": "r",
			"content": strings.Repeat("y", maxProposedKM2Content+1)}), "content: must be at most 100000 bytes in a chat card; save a shorter document"},
		{"a bad agent id", TypeKM2Save, with(map[string]any{"title": "t", "external_ref": "r", "content": "c", "proposed_by_agent_id": "x"}),
			"proposed_by_agent_id: must be an agent's id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, _ := Lookup(tc.typ)
			_, _, err := kind.Normalize(tc.payload)
			wantInvalid(t, err, tc.problem)
		})
	}
	// Hidden characters never reach a card: what the person reviews is what is posted
	comment, _ := Lookup(TypeTrackerComment)
	if clean, _, err := comment.Normalize(with(map[string]any{"issue": "PRJ-10", "text": "Pay\u202e01 \u200bnow", "connection_name": "Acme\u200b Jira",
		"proposed_by": "An\u202en"})); err != nil || clean["text"] != "Pay01 now" || clean["connection_name"] != "Acme Jira" || clean["proposed_by"] != "Ann" {
		t.Errorf("hidden characters = %v, %v", clean, err)
	}
	kind, _ := Lookup(TypeKM2Save)
	if clean, _, err := kind.Normalize(with(map[string]any{"title": "t", "external_ref": "agent-chat-x", "content": "c"})); err != nil ||
		clean["external_ref"] != "agent-chat-x" {
		t.Errorf("an already chat ref = %v, %v", clean, err)
	}
	for _, typ := range []string{TypeTrackerComment, TypeTrackerSuggestion, TypeTrackerIssue, TypeKM2Save} {
		if k, ok := Lookup(typ); !ok || k.Undo != nil || !slices.Equal(k.Needs, []string{auth.CrewsRun}) {
			t.Errorf("%s = %+v", typ, k)
		}
	}
}

func TestPickFrozenConnection(t *testing.T) {
	jira := model.RunConnection{ID: "j", Name: "Acme Jira", Kind: model.TrackerJira}
	linear := model.RunConnection{ID: "l", Name: "Acme Linear", Kind: model.TrackerLinear}
	k := model.RunConnection{ID: "k", Name: "KM2", Kind: model.KindKM2}
	if got, err := pickFrozenConnection([]model.RunConnection{jira, k}, model.NeedTracker, ""); err != nil || got.ID != "j" {
		t.Errorf("the only tracker = %+v, %v", got, err)
	}
	if got, err := pickFrozenConnection([]model.RunConnection{jira, linear}, model.NeedTracker, "acme LINEAR"); err != nil || got.ID != "l" {
		t.Errorf("by name = %+v, %v", got, err)
	}
	_, err := pickFrozenConnection([]model.RunConnection{jira, linear}, model.NeedTracker, "")
	wantInvalid(t, err, "connection: say which connection, one of Acme Jira, Acme Linear")
	_, err = pickFrozenConnection([]model.RunConnection{jira}, model.NeedKM2, "")
	var denied *DeniedError
	if !errors.As(err, &denied) || !strings.Contains(denied.Message, "no KM2 connection") {
		t.Errorf("no KM2 = %v", err)
	}
	ann := &auth.Principal{Name: "Ann\nRuiz"}
	if got := attribution(ann, ""); got != "\n\n(Posted by Ann Ruiz from a Crew Studio agent chat.)" {
		t.Errorf("attribution = %q", got)
	}
}

// Without a tracker or KM2 service the cards cannot be confirmed; a Linear text with a profile link is refused, since
// Linear would turn it into a mention.
func TestCounterpartsNeedTheirServices(t *testing.T) {
	e := newProposeEnv(t)
	ctx := context.Background()
	via := proposedVia{ConnectionID: e.jira.ID, ProposedByAgentID: dealPartner}
	conv := model.Conversation{AgentID: ptr(dealPartner)}
	req := Request{Principal: e.ann, Conversation: conv}
	var denied *DeniedError
	if _, err := postAsPerson(ctx, Env{Store: e.st}, req, TypeTrackerComment, via, "PRJ-10", "x"); !errors.As(err, &denied) {
		t.Errorf("no trackers = %v", err)
	}
	km2Req := req
	km2Req.Payload = map[string]any{"connection_id": e.km2Conn.ID, "connection_name": "", "proposed_by": "", "proposed_by_agent_id": dealPartner,
		"title": "t", "external_ref": "agent-chat-r", "content": "c", "folder": ""}
	if _, err := applyKM2Save(ctx, Env{Store: e.st}, km2Req); !errors.As(err, &denied) {
		t.Errorf("no KM2 = %v", err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE connections SET kind = 'linear' WHERE id = $1`, e.jira.ID); err != nil {
		t.Fatal(err)
	}
	_, err := postAsPerson(ctx, e.svc.env(), req, TypeTrackerComment, via, "PRJ-10", "See https://linear.app/acme/profiles/dana")
	wantInvalid(t, err, "text: Linear profile links would mention people; ask the agent again without them")
	if _, err := counterpartConnection(ctx, e.svc.env(), Request{Principal: e.ann}, TypeTrackerComment, via); !errors.Is(err, errNotAgentChat) {
		t.Errorf("a crew conversation = %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestTurnAgentByRole(t *testing.T) {
	long := strings.Repeat("r", agentRoleHeaderLen+30)
	spec := model.RunSpec{Agents: []model.Agent{{ID: "lead", Role: "Lead adviser"}, {ID: "long", Role: long}, {ID: "s", Role: "Scout"}}}
	cases := []struct{ role, want string }{
		{" lead  ADVISER ", "lead"},
		{long[:agentRoleHeaderLen], "long"},
		{"Scou", ""},
		{"", ""},
	}
	for _, tc := range cases {
		a, ok := turnAgentByRole(spec, tc.role)
		if a.ID != tc.want || ok != (tc.want != "") {
			t.Errorf("%q = %q, %v", tc.role, a.ID, ok)
		}
	}
}

// A tracker that took the comment but answered with something Studio cannot read may have posted it: the card is
// confirmed with an unknown outcome, never offered again.
func TestAPostAnsweredUnreadablyIsNotOfferedAgain(t *testing.T) {
	e := newProposeEnvWith(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comment") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("<html>ok</html>"))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	ctx := context.Background()
	_, _, scope := e.turn(t)
	m, err := e.svc.ProposeAs(ctx, scope, "Private Equity Deal Partner", TypeTrackerComment, map[string]any{"issue": "PRJ-10", "text": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if done.ProposalState == nil || *done.ProposalState != model.ProposalConfirmed || done.ProposalResult["outcome"] != "unknown" ||
		done.ProposalResult["issue"] != "PRJ-10" {
		t.Errorf("card = %v %v", done.ProposalState, done.ProposalResult)
	}
	if _, err := e.svc.Decide(ctx, e.ann, m.ID, true, time.Now()); !errors.Is(err, store.ErrProposalDecided) {
		t.Errorf("confirming again = %v", err)
	}
}

// On a Linear connection a text with a profile link (which Linear turns into a mention) is refused when proposed, so the
// person never sees a card that cannot be posted.
func TestLinearProfileLinksAreRefusedWhenProposed(t *testing.T) {
	e := newProposeEnv(t)
	ctx := context.Background()
	if _, err := e.st.Pool().Exec(ctx, `UPDATE connections SET kind = 'linear' WHERE id = $1`, e.jira.ID); err != nil {
		t.Fatal(err)
	}
	_, _, scope := e.turn(t)
	_, err := e.svc.ProposeAs(ctx, scope, "Private Equity Deal Partner", TypeTrackerComment,
		map[string]any{"issue": "PRJ-10", "text": "cc https://linear.app/acme/profiles/dana"})
	wantInvalid(t, err, "text: Linear profile links would mention people; leave them out")
}
