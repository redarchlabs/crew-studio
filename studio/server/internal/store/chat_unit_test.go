package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

// chatTurn starts a turn by the conversation's owner, with a generous person budget unless person is set.
func (tc *fixture) chatTurn(convID, text string, crew model.Crew, daily float64, person ...float64) (StartedTurn, error) {
	owner := missingID
	if c, err := tc.s.GetConversation(bg(), convID); err == nil {
		owner = c.UserID
	}
	limit := 100.0
	if len(person) > 0 {
		limit = person[0]
	}
	return tc.s.CreateChatTurn(bg(), ChatTurn{ConversationID: convID, Content: text, Spec: tc.spec(crew), DailyBudgetUSD: daily,
		PersonBudgetUSD: limit, MinBudgetUSD: 0.01, Meta: RunMeta{AreaIDs: crew.AreaIDs, CreatedBy: owner}})
}

// startTurnRun marks a turn's run running, as the run manager does when it starts it.
func (tc *fixture) startTurnRun(runID string) {
	tc.t.Helper()
	if ok, err := tc.s.StartRun(bg(), runID); err != nil || !ok {
		tc.t.Fatalf("start run: %v %v", ok, err)
	}
}

func TestChatSettingsAndCrews(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	uid := f.admin()
	crew := f.simpleCrew("pmo", a)
	other := f.simpleCrew("other", b)
	llm := f.llm("cheap")

	s := must(f.s.ChatSettings(bg(), crew.ID))
	if s.Enabled || s.DailyBudgetUSD != model.DefaultChatDailyBudgetUSD {
		t.Errorf("default = %+v", s)
	}
	desk := crew.AgentIDs[0]
	saved := must(f.s.PutChatSettings(bg(), model.ChatSettings{CrewID: crew.ID, Enabled: true, FrontDeskAgentID: &desk, LLMID: &llm.ID, DailyBudgetUSD: 3}, uid))
	if !saved.Enabled || saved.UpdatedByName != "Admin" || saved.UpdatedAt == nil || *saved.LLMID != llm.ID {
		t.Errorf("saved = %+v", saved)
	}
	_, err := f.s.PutChatSettings(bg(), model.ChatSettings{CrewID: missingID, DailyBudgetUSD: 1}, uid)
	wantConflict(t, err, "refers to something")
	otherDesk := other.AgentIDs[0]
	mustOK(t, func() error {
		_, err := f.s.PutChatSettings(bg(), model.ChatSettings{CrewID: other.ID, Enabled: true, FrontDeskAgentID: &otherDesk, DailyBudgetUSD: 1}, uid)
		return err
	}())

	if crews := must(f.s.ChatCrews(bg(), Scope{AreaIDs: []string{a}})); len(crews) != 1 || crews[0].Name != "pmo" || crews[0].FrontDeskName != "pmo-agent" {
		t.Errorf("alpha chat crews = %+v", crews)
	}
	if crews := must(f.s.ChatCrews(bg(), AllAreas)); len(crews) != 2 {
		t.Errorf("all = %+v", crews)
	}
	// A front desk that left the crew takes the crew out of the list
	f.exec(`DELETE FROM crew_agents WHERE crew_id = $1`, other.ID)
	if crews := must(f.s.ChatCrews(bg(), AllAreas)); len(crews) != 1 {
		t.Errorf("after the agent left = %+v", crews)
	}
	err = f.s.DeleteLLM(bg(), llm.ID)
	wantConflict(t, err, `crew "pmo" (chat)`)
}

func TestConversationsAndTurns(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	crew := f.simpleCrew("pmo", a)
	ann := f.human("ann@example.com", model.UserInput{AreaIDs: []string{a}})
	bob := f.human("bob@example.com", model.UserInput{AreaIDs: []string{a}})

	conv := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
	_ = must(f.s.CreateConversation(bg(), crew.ID, bob.ID, "bob's"))
	_, err := f.s.CreateConversation(bg(), missingID, ann.ID, "")
	wantConflict(t, err, "refers to something")
	wantNotFound(t, func() error { _, err := f.s.GetConversation(bg(), missingID); return err }())

	turn, err := f.chatTurn(conv.ID, "What is slipping?", crew, 1)
	mustOK(t, err)
	if turn.Run.Kind != model.RunKindChat || turn.User.Role != model.ChatRoleUser || turn.Assistant.RunID == nil || *turn.Assistant.RunID != turn.Run.ID {
		t.Errorf("turn = %+v", turn)
	}
	if got := must(f.s.GetConversation(bg(), conv.ID)); got.Title != "What is slipping?" {
		t.Errorf("title = %q", got.Title)
	}
	_, err = f.chatTurn(conv.ID, "again", crew, 1)
	if !IsTurnActive(err) {
		t.Errorf("second turn while active = %v", err)
	}
	_, err = f.chatTurn(missingID, "x", crew, 1)
	wantNotFound(t, err)

	out, cost := strings.Repeat("y", maxReplyChars+10), 0.4
	_, err = f.s.FinishRun(bg(), turn.Run.ID, RunResult{Status: model.RunSucceeded, Output: &out, CostUSD: &cost})
	mustOK(t, err)
	mustOK(t, f.s.SettleReplies(bg(), conv.ID, 24*time.Hour))
	msgs := must(f.s.ListMessages(bg(), conv.ID, 10))
	if len(msgs) != 2 || msgs[0].Role != model.ChatRoleUser || len(msgs[1].Content) != maxReplyChars || *msgs[1].CostUSD != 0.4 {
		t.Fatalf("messages = %d, %+v", len(msgs), msgs[0])
	}
	if spent := must(f.s.ChatSpent(bg(), conv.ID)); spent != 0.4 {
		t.Errorf("spent = %v", spent)
	}
	turns := must(f.s.RecentTurns(bg(), conv.ID, 1))
	if len(turns) != 1 || turns[0].Role != model.ChatRoleAssistant {
		t.Errorf("latest turn = %+v", turns)
	}
	// The run gets what is left of the daily budget; once spent, no more turns
	next, err := f.chatTurn(conv.ID, "Why?", crew, 0.5)
	mustOK(t, err)
	if next.Run.BudgetUSD < 0.0999 || next.Run.BudgetUSD > 0.1001 {
		t.Errorf("turn budget = %v", next.Run.BudgetUSD)
	}
	f.finish(next.Run.ID, model.RunFailed)
	_, err = f.chatTurn(conv.ID, "More?", crew, 0.4)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Code != "chat_budget_used" {
		t.Errorf("over budget = %v", err)
	}

	list := must(f.s.ListConversations(bg(), ConversationFilter{UserID: ann.ID, Scope: Scope{AreaIDs: []string{a}}, Limit: 10}))
	if len(list) != 1 || list[0].ID != conv.ID || list[0].CrewName != "pmo" {
		t.Errorf("ann's list = %+v", list)
	}
	if out := must(f.s.ListConversations(bg(), ConversationFilter{UserID: ann.ID, Scope: Scope{AreaIDs: []string{b}}, Limit: 10})); len(out) != 0 {
		t.Errorf("outside the crew's areas = %+v", out)
	}
	if out := must(f.s.ListConversations(bg(), ConversationFilter{UserID: ann.ID, CrewID: missingID, Scope: AllAreas, Limit: 10})); len(out) != 0 {
		t.Errorf("other crew = %+v", out)
	}
	m, c, err := f.s.GetMessage(bg(), msgs[1].ID)
	if err != nil || m.ID != msgs[1].ID || c.ID != conv.ID {
		t.Errorf("message = %+v %+v %v", m, c, err)
	}
	_, _, err = f.s.GetMessage(bg(), missingID)
	wantNotFound(t, err)
	mustOK(t, f.s.DeleteConversation(bg(), conv.ID))
	wantNotFound(t, f.s.DeleteConversation(bg(), conv.ID))
	if _, err := f.s.GetRun(bg(), turn.Run.ID); err != nil {
		t.Errorf("chat runs outlive their conversation: %v", err)
	}
}

func TestOnlyOneTurnStartsAtATime(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("pmo", a)
	ann := f.human("ann@example.com", model.UserInput{AreaIDs: []string{a}})
	conv := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
	errs := parallel(6, func(int) error { _, err := f.chatTurn(conv.ID, "hi", crew, 1); return err })
	started := 0
	for _, err := range errs {
		switch {
		case err == nil:
			started++
		case !IsTurnActive(err):
			t.Errorf("unexpected error %v", err)
		}
	}
	if started != 1 {
		t.Errorf("%d turns started at once", started)
	}
}

func TestProposalStates(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("pmo", a)
	ann := f.human("ann@example.com", model.UserInput{AreaIDs: []string{a}})
	conv := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
	turn := must(f.chatTurn(conv.ID, "hi", crew, 1))
	p := model.Proposal{Type: "notes_put", Summary: "Create the note x", Payload: map[string]any{"name": "x"}}

	_, err := f.s.AttachProposal(bg(), missingID, p)
	wantNotFound(t, err)
	// A queued (or finished) turn cannot propose; only a running one
	_, err = f.s.AttachProposal(bg(), turn.Run.ID, p)
	wantConflict(t, err, "no longer propose")
	f.startTurnRun(turn.Run.ID)
	m := must(f.s.AttachProposal(bg(), turn.Run.ID, p))
	if m.ProposedAt == nil {
		t.Error("proposed_at not set")
	}
	if m.Proposal == nil || m.Proposal.Summary != p.Summary || *m.ProposalState != model.ProposalPending {
		t.Errorf("attached = %+v", m)
	}
	_, err = f.s.AttachProposal(bg(), turn.Run.ID, p)
	wantConflict(t, err, "already proposed")

	mustOK(t, f.s.ClaimProposal(bg(), m.ID, ann.ID, ann.Name))
	if err := f.s.ClaimProposal(bg(), m.ID, ann.ID, ann.Name); !errors.Is(err, ErrProposalDecided) {
		t.Errorf("second claim = %v", err)
	}
	mustOK(t, f.s.ReleaseProposal(bg(), m.ID))
	mustOK(t, f.s.DeclineProposal(bg(), m.ID, ann.ID, ann.Name))
	got, _, _ := f.s.GetMessage(bg(), m.ID)
	if *got.ProposalState != model.ProposalDeclined || got.DecidedByName != ann.Name || got.DecidedAt == nil {
		t.Errorf("declined = %+v", got)
	}
	// A run whose reply was removed has nothing to attach to
	f.exec(`UPDATE conversation_messages SET run_id = NULL WHERE id = $1`, m.ID)
	_, err = f.s.AttachProposal(bg(), turn.Run.ID, p)
	wantNotFound(t, err)
	f.exec(`UPDATE conversation_messages SET run_id = $2 WHERE id = $1`, m.ID, turn.Run.ID)
	if err := f.s.ExpireProposal(bg(), m.ID); !errors.Is(err, ErrProposalDecided) {
		t.Errorf("expire after decline = %v", err)
	}
	f.exec(`UPDATE conversation_messages SET proposal_state = 'confirmed' WHERE id = $1`, m.ID)
	mustOK(t, f.s.SetProposalResult(bg(), m.ID, map[string]any{"version": 1}))
	mustOK(t, f.s.MarkUndone(bg(), m.ID))
	wantConflict(t, f.s.MarkUndone(bg(), m.ID), "already undone")
	mustOK(t, f.s.UnmarkUndone(bg(), m.ID))
	mustOK(t, f.s.MarkUndone(bg(), m.ID))
	if err := f.s.SetProposalResult(bg(), m.ID, map[string]any{"bad": func() {}}); err == nil {
		t.Error("an unencodable result was saved")
	}
	if _, err := f.s.AttachProposal(bg(), turn.Run.ID, model.Proposal{Payload: map[string]any{"bad": func() {}}}); err == nil {
		t.Error("an unencodable proposal was saved")
	}
}

func TestProposalsExpireOnRead(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("pmo", a)
	ann := f.human("ann@example.com", model.UserInput{AreaIDs: []string{a}})
	conv := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
	turn := must(f.chatTurn(conv.ID, "hi", crew, 1))
	f.startTurnRun(turn.Run.ID)
	m := must(f.s.AttachProposal(bg(), turn.Run.ID, model.Proposal{Type: "notes_put", Payload: map[string]any{}}))
	mustOK(t, f.s.SettleReplies(bg(), conv.ID, time.Hour))
	if got, _, _ := f.s.GetMessage(bg(), m.ID); *got.ProposalState != model.ProposalPending {
		t.Errorf("fresh proposal = %v", *got.ProposalState)
	}
	f.exec(`UPDATE conversation_messages SET proposed_at = now() - interval '2 hours' WHERE id = $1`, m.ID)
	mustOK(t, f.s.SettleReplies(bg(), conv.ID, time.Hour))
	if got, _, _ := f.s.GetMessage(bg(), m.ID); *got.ProposalState != model.ProposalExpired {
		t.Errorf("old proposal = %v", *got.ProposalState)
	}
}

func TestPersonBudgetSpansConversations(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("pmo", a)
	ann := f.human("ann@example.com", model.UserInput{AreaIDs: []string{a}})
	first := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
	turn := must(f.chatTurn(first.ID, "hi", crew, 1, 0.5))
	if turn.Run.BudgetUSD != 0.5 {
		t.Errorf("turn budget = %v, want the person cap", turn.Run.BudgetUSD)
	}
	// Deleting a conversation mid-turn is refused; afterwards its spend still counts for the person
	_, ok := f.s.DeleteConversation(bg(), first.ID).(*ConflictError)
	if !ok {
		t.Error("deleting a conversation mid-turn was allowed")
	}
	out, cost := "x", 0.495
	_, err := f.s.FinishRun(bg(), turn.Run.ID, RunResult{Status: model.RunSucceeded, Output: &out, CostUSD: &cost})
	mustOK(t, err)
	// Read before anything settled the reply: the finished turn already shows its text
	if msgs := must(f.s.ListMessages(bg(), first.ID, 5)); msgs[1].Content != "x" {
		t.Errorf("unsettled reply = %q", msgs[1].Content)
	}
	mustOK(t, f.s.DeleteConversation(bg(), first.ID))
	if spent := must(f.s.PersonChatSpent(bg(), crew.ID, ann.ID)); spent != 0.495 {
		t.Errorf("person spent = %v", spent)
	}
	second := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
	_, err = f.chatTurn(second.ID, "hi", crew, 1, 0.5)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Code != "chat_person_budget_used" {
		t.Errorf("new conversation over the person cap = %v", err)
	}
}

// A crew chat turn still queued or running counts at its whole budget towards the person's cap, so turns started at
// once in several conversations cannot each spend all of it. A finished turn counts at what it cost.
func TestPersonBudgetCountsTurnsInFlight(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	cost := 0.25
	cases := []struct {
		name   string
		status model.RunStatus
		cost   *float64
		want   float64
	}{
		{"queued", model.RunQueued, nil, 0.6},
		{"running", model.RunRunning, nil, 0.6},
		{"succeeded", model.RunSucceeded, &cost, 0.25},
		{"failed without a cost", model.RunFailed, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			crew := f.simpleCrew("pmo-"+strings.ReplaceAll(tc.name, " ", "-"), a)
			ann := f.human(strings.ReplaceAll(tc.name, " ", ".")+"@example.com", model.UserInput{AreaIDs: []string{a}})
			first := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
			turn := must(f.chatTurn(first.ID, "hi", crew, 1, 0.6))
			f.exec(`UPDATE runs SET status = $2, cost_usd = $3 WHERE id = $1`, turn.Run.ID, tc.status, tc.cost)
			if spent := must(f.s.PersonChatSpent(bg(), crew.ID, ann.ID)); spent != tc.want {
				t.Errorf("person spent = %v, want %v", spent, tc.want)
			}
			// A second conversation gets only what the first turn leaves of the person's cap
			second := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
			_, err := f.chatTurn(second.ID, "hi", crew, 1, 0.6)
			var conflict *ConflictError
			if refused := errors.As(err, &conflict) && conflict.Code == "chat_person_budget_used"; refused != (tc.want == 0.6) {
				t.Errorf("second conversation's turn = %v", err)
			}
		})
	}
}

func TestUndoNoteWrite(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("pmo", f.area("Alpha"))
	key := NoteKey{StateScope: StateScope{CrewID: crew.ID}, Name: "risks"}
	zero := 0
	by := NoteWriter{Name: "Ann"}
	_ = must(f.s.PutNote(bg(), key, "one", &zero, by))
	two := must(f.s.PutNote(bg(), key, "two", nil, by))

	_, err := f.s.UndoNoteWrite(bg(), key, 1, by)
	wantConflict(t, err, "changed since")
	back := must(f.s.UndoNoteWrite(bg(), key, two.Version, by))
	if back == nil || back.Content != "one" || back.Version != 3 || back.WrittenBy != "Ann" {
		t.Errorf("restored = %+v", back)
	}
	other := NoteKey{StateScope: StateScope{CrewID: crew.ID}, Name: "fresh"}
	_ = must(f.s.PutNote(bg(), other, "new", &zero, by))
	gone, err := f.s.UndoNoteWrite(bg(), other, 1, by)
	if err != nil || gone != nil {
		t.Errorf("undo of a new note = %+v %v", gone, err)
	}
	_, err = f.s.LatestNote(bg(), other)
	wantNotFound(t, err)
	// A pruned earlier version cannot be restored
	_ = must(f.s.PutNote(bg(), key, "four", nil, by))
	f.exec(`DELETE FROM notes WHERE crew_id = $1 AND name = 'risks' AND version < 4`, crew.ID)
	_, err = f.s.UndoNoteWrite(bg(), key, 4, by)
	wantNotFound(t, err)
}

func TestConversationFileLimits(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("pmo", a)
	ann := f.human("ann@example.com", model.UserInput{AreaIDs: []string{a}})
	conv := must(f.s.CreateConversation(bg(), crew.ID, ann.ID, ""))
	files := func(n int, size int64) []model.Attachment {
		out := make([]model.Attachment, n)
		for i := range out {
			id := auth.NewUUID()
			out[i] = model.Attachment{ID: id, Filename: "shot.png", ContentType: "image/png", SizeBytes: size, SHA256: "x", StorageKey: "chat/" + id}
		}
		return out
	}
	send := func(fs []model.Attachment) (StartedTurn, error) {
		return f.s.CreateChatTurn(bg(), ChatTurn{ConversationID: conv.ID, Content: "look", Files: fs, Spec: f.spec(crew), DailyBudgetUSD: 100,
			PersonBudgetUSD: 100, MinBudgetUSD: 0.01, Meta: RunMeta{AreaIDs: crew.AreaIDs, CreatedBy: ann.ID}})
	}
	turn, err := send(files(maxConversationFiles-1, 10))
	mustOK(t, err)
	if len(turn.User.Attachments) != maxConversationFiles-1 {
		t.Fatalf("attachments = %d", len(turn.User.Attachments))
	}
	if _, err := f.s.FinishRun(bg(), turn.Run.ID, RunResult{Status: model.RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	// Over the count: refused, and nothing of the message is kept
	_, err = send(files(2, 10))
	wantConflict(t, err, "most files")
	if msgs := must(f.s.ListMessages(bg(), conv.ID, 100)); len(msgs) != 2 {
		t.Errorf("messages after a refused turn = %d", len(msgs))
	}
	// Over the size
	_, err = send(files(1, maxConversationFilesBytes))
	wantConflict(t, err, "most files")
	mustOK(t, func() error { _, err := send(files(1, 10)); return err }())
}
