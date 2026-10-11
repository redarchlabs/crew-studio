package store

import (
	"errors"
	"testing"
	"time"

	"crewstudio/internal/model"
)

// An agent's question settings (slice 3c) are saved with its other chat settings; left out, they get their defaults.
func TestAgentChatQuestionSettingsRoundTrip(t *testing.T) {
	f := newFixture(t)
	agent := f.agent("lead", nil, f.area("Alpha"))
	in := model.AgentChatSettings{AgentID: agent.ID, Enabled: true, DailyBudgetUSD: 1, PersonDailyBudgetUSD: 2,
		QuestionsEnabled: true, QuestionRules: "Ask before choosing between price models", QuestionTimeoutMinutes: 7,
		QuestionOnTimeout: model.EscalationFail, MaxQuestions: 4}
	got := must(f.s.PutAgentChatSettings(bg(), in, ""))
	if !got.QuestionsEnabled || got.QuestionRules != in.QuestionRules || got.QuestionTimeoutMinutes != 7 ||
		got.QuestionOnTimeout != model.EscalationFail || got.MaxQuestions != 4 {
		t.Errorf("saved = %+v", got)
	}
	got = must(f.s.PutAgentChatSettings(bg(), model.AgentChatSettings{AgentID: agent.ID, DailyBudgetUSD: 1}, ""))
	if got.QuestionsEnabled || got.QuestionRules != "" || got.QuestionTimeoutMinutes != model.DefaultQuestionTimeoutMinutes ||
		got.QuestionOnTimeout != model.EscalationProceed || got.MaxQuestions != model.DefaultChatMaxQuestions {
		t.Errorf("defaults = %+v", got)
	}
}

// startedHandoff is a running hand-off of a fresh conversation.
func (tc *fixture) startedHandoff(name string) (handoffChat, StartedHandoff) {
	tc.t.Helper()
	h := tc.handoffChat(name)
	started := must(tc.s.CreateHandoff(bg(), h.newHandoff("Vendor comparison")))
	tc.startTurnRun(started.Run.ID)
	return h, started
}

// A hand-off's question is addressed to the conversation's owner, says who it was asked for, carries the conversation
// and the hand-off, and is due a minute before the hand-off must stop at the latest: the hand-off's deadline does not
// pause while it waits. With less than a minute left for an answer the question is refused.
func TestHandoffQuestionsAreCappedByTheHandoffDeadline(t *testing.T) {
	f := newFixture(t)
	h, started := f.startedHandoff("ann")
	hand := must(f.s.GetHandoff(bg(), started.Handoff.ID))
	q := must(f.s.AskQuestion(bg(), started.Run.ID, NewQuestion{AgentRole: h.agent.Role, AskedFor: "Market analyst", TaskName: "Hand-off",
		Question: "List or negotiated prices?", Context: "The comparison needs one."}, 3*time.Hour, 3))
	if q.AssigneeID == nil || *q.AssigneeID != h.person.ID || q.AskedFor != "Market analyst" || q.ConversationID == nil ||
		*q.ConversationID != h.conv.ID || q.ConversationTitle != "Vendors" || q.HandoffID == nil || *q.HandoffID != hand.ID ||
		q.HandoffTitle != "Vendor comparison" || q.Status != model.QuestionOpen {
		t.Fatalf("question = %+v", q)
	}
	if want := hand.DeadlineAt.Add(-time.Minute); !q.DeadlineAt.Equal(want) {
		t.Errorf("deadline = %v, want the hand-off's less a minute (%v)", q.DeadlineAt, want)
	}
	// A short timeout stays as it is
	short := must(f.s.AskQuestion(bg(), started.Run.ID, NewQuestion{Question: "Which region?"}, 2*time.Minute, 3))
	if d := time.Until(short.DeadlineAt); d < time.Minute || d > 3*time.Minute {
		t.Errorf("short question due in %v", d)
	}
	// The hand-off and its result message point at the open question due soonest
	if got := must(f.s.GetHandoff(bg(), hand.ID)); got.OpenQuestionID == nil || *got.OpenQuestionID != short.ID {
		t.Errorf("open question = %v, want %s", got.OpenQuestionID, short.ID)
	}
	msg, _ := must2(f.s.GetMessage(bg(), started.Message.ID))
	if msg.Handoff == nil || msg.Handoff.OpenQuestionID == nil || *msg.Handoff.OpenQuestionID != short.ID {
		t.Errorf("result message hand-off = %+v", msg.Handoff)
	}
	// Under a minute left before the hand-off must stop: refused
	f.exec(`UPDATE chat_handoffs SET deadline_at = now() + interval '90 seconds' WHERE id = $1`, hand.ID)
	if _, err := f.s.AskQuestion(bg(), started.Run.ID, NewQuestion{Question: "Too late?"}, time.Hour, 5); !errors.Is(err, ErrQuestionNoTime) {
		t.Errorf("err = %v, want ErrQuestionNoTime", err)
	}
	// A crew's question is not capped and carries no conversation
	crew := f.simpleCrew("crew", h.area)
	run := f.run(crew, RunMeta{AreaIDs: crew.AreaIDs, CreatedBy: h.person.ID})
	cq := must(f.s.AskQuestion(bg(), run.ID, NewQuestion{Question: "Crew question"}, 3*time.Hour, 3))
	if cq.ConversationID != nil || cq.HandoffID != nil || cq.AskedFor != "" || time.Until(cq.DeadlineAt) < 2*time.Hour {
		t.Errorf("crew question = %+v", cq)
	}
}

// A conversation's questions list open ones first (soonest due first), then the rest newest first; the open list
// leaves answered and timed-out questions out, and both leave other conversations' questions out.
func TestConversationQuestions(t *testing.T) {
	f := newFixture(t)
	h, started := f.startedHandoff("bob")
	_, other := f.startedHandoff("cid")
	ask := func(runID, text string, timeout time.Duration) model.HumanQuestion {
		return must(f.s.AskQuestion(bg(), runID, NewQuestion{Question: text}, timeout, 10))
	}
	answered := ask(started.Run.ID, "answered", time.Hour)
	must(f.s.AnswerQuestion(bg(), answered.ID, "Yes", h.person.ID))
	late := ask(started.Run.ID, "due later", 10*time.Minute)
	soon := ask(started.Run.ID, "due soon", 5*time.Minute)
	expired := ask(started.Run.ID, "timed out", 5*time.Minute)
	f.exec(`UPDATE human_questions SET deadline_at = now() - interval '1 second' WHERE id = $1`, expired.ID)
	ask(other.Run.ID, "elsewhere", time.Hour)

	all := must(f.s.ConversationQuestions(bg(), h.conv.ID))
	ids := make([]string, 0, len(all))
	for _, q := range all {
		ids = append(ids, q.ID)
	}
	if len(ids) != 4 || ids[0] != soon.ID || ids[1] != late.ID || ids[2] != expired.ID || ids[3] != answered.ID {
		t.Errorf("order = %v; want soon, late, expired, answered", ids)
	}
	open := must(f.s.OpenConversationQuestions(bg(), h.conv.ID))
	if len(open) != 2 || open[0].ID != soon.ID || open[1].ID != late.ID {
		t.Errorf("open = %+v", open)
	}
	// Once the run ends its open questions are closed
	f.finish(started.Run.ID, model.RunCancelled)
	if open := must(f.s.OpenConversationQuestions(bg(), h.conv.ID)); len(open) != 0 {
		t.Errorf("open after the run ended = %+v", open)
	}
	if got := must(f.s.GetHandoff(bg(), started.Handoff.ID)); got.OpenQuestionID != nil {
		t.Errorf("open question after the run ended = %v", *got.OpenQuestionID)
	}
	if none := must(f.s.ConversationQuestions(bg(), missingID)); len(none) != 0 {
		t.Errorf("unknown conversation = %+v", none)
	}
}

// A hand-off's question needs its owner to be an active person; with the owner disabled or gone there is nobody to ask.
// A crew's question keeps going to the run's creator (or, with none, to work order operators) as before.
func TestHandoffQuestionsNeedSomeoneToAsk(t *testing.T) {
	f := newFixture(t)
	h, started := f.startedHandoff("dee")
	f.exec(`UPDATE users SET status = 'disabled' WHERE id = $1`, h.person.ID)
	if _, err := f.s.AskQuestion(bg(), started.Run.ID, NewQuestion{Question: "Prices?"}, time.Hour, 3); !errors.Is(err, ErrQuestionNobody) {
		t.Errorf("disabled owner: err = %v, want ErrQuestionNobody", err)
	}
	f.exec(`UPDATE runs SET created_by = NULL WHERE id = $1`, started.Run.ID)
	if _, err := f.s.AskQuestion(bg(), started.Run.ID, NewQuestion{Question: "Prices?"}, time.Hour, 3); !errors.Is(err, ErrQuestionNobody) {
		t.Errorf("no owner: err = %v, want ErrQuestionNobody", err)
	}
	crew := f.simpleCrew("crew", h.area)
	run := f.run(crew, RunMeta{AreaIDs: crew.AreaIDs})
	if _, err := f.s.AskQuestion(bg(), run.ID, NewQuestion{Question: "Crew question"}, time.Hour, 3); err != nil {
		t.Errorf("a crew run without a creator still asks: %v", err)
	}
}
