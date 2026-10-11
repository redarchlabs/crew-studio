package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Human input: a crew's escalation rules, the questions its agents ask during a run, and the answers people give.
// The runner asks through the run-state API and long-polls for the answer (woken the moment it is given); people answer through /questions.

const askHumanTool = "ask_human"

// escalationSettingsRoutes hangs off /crews (see mountLibrary).
func (s *Server) escalationSettingsRoutes(r chi.Router) {
	r.With(s.need(auth.CrewsView)).Get("/{id}/escalation", s.getEscalationSettings)
	r.With(s.need(auth.CrewsEdit)).Put("/{id}/escalation", s.putEscalationSettings)
}

// questionRoutes are for people: what is waiting on them, and answering it. Who may answer is checked per question.
func (s *Server) questionRoutes(r chi.Router) {
	r.Get("/questions", s.listMyQuestions)
	r.Post("/questions/{id}/answer", s.answerQuestion)
}

// questionRunRoutes are for the running crew's Ask a human tool.
func (s *Server) questionRunRoutes(r chi.Router) {
	r.Post("/questions", s.runAskQuestion)
	r.Get("/questions/{qid}", s.runGetQuestion)
}

func (s *Server) getEscalationSettings(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadCrew(w, r)
	if !found {
		return
	}
	settings, err := s.store.EscalationSettings(r.Context(), crew.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, settings)
}

func (s *Server) putEscalationSettings(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadCrew(w, r)
	if !found {
		return
	}
	var in model.EscalationSettings
	if !decode(w, r, &in) {
		return
	}
	in.CrewID = crew.ID
	clean, err := model.NormalizeEscalationSettings(in)
	var out model.EscalationSettings
	if err == nil {
		out, err = s.store.PutEscalationSettings(r.Context(), clean, principal(r).UserID)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

// canAnswer: the person the question is addressed to, or, for an unaddressed one, anyone who may cancel work orders
// in the run's areas.
func canAnswer(p *auth.Principal, q model.HumanQuestion, areas []string) bool {
	if q.AssigneeID != nil {
		return *q.AssigneeID == p.UserID
	}
	return p.Can(auth.WorkOrdersCancel) && p.SeesAreas(areas)
}

func (s *Server) listMyQuestions(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	list, err := s.store.OpenQuestionsFor(r.Context(), p.UserID, scopeOf(p), p.Can(auth.WorkOrdersCancel))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, readableQuestions(p, list))
}

// readableQuestions leaves out the questions of agent chat hand-offs unless the caller is signed in: like their
// conversation's cards, they are read and answered only in a session, never with an API key or a token.
func readableQuestions(p *auth.Principal, list []model.HumanQuestion) []model.HumanQuestion {
	if p != nil && p.Via == auth.ViaSession {
		return list
	}
	out := make([]model.HumanQuestion, 0, len(list))
	for _, q := range list {
		if q.HandoffID == nil {
			out = append(out, q)
		}
	}
	return out
}

func (s *Server) workOrderQuestions(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	list, err := s.store.WorkOrderQuestions(r.Context(), wo.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, list)
}

type answerInput struct {
	Answer string `json:"answer"`
}

func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in answerInput
	if !decode(w, r, &in) {
		return
	}
	answer := strings.TrimSpace(in.Answer)
	if answer == "" || len([]rune(answer)) > model.MaxAnswerLen {
		failErr(w, r, s.log, invalid(fmt.Sprintf("answer: must be between 1 and %d characters", model.MaxAnswerLen)))
		return
	}
	p := principal(r)
	q, areas, err := s.store.GetQuestion(r.Context(), id)
	if err == nil && !p.SeesAreas(areas) && (q.AssigneeID == nil || *q.AssigneeID != p.UserID) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	// A question from an agent chat's hand-off is answered as its cards are decided: only by the conversation's owner
	// (never by a work order operator, even when the owner is gone), signed in (an API key or a token cannot speak for
	// them in their conversation). To anyone else it does not exist, as their conversation does not.
	if q.HandoffID != nil && (q.AssigneeID == nil || *q.AssigneeID != p.UserID) {
		failErr(w, r, s.log, store.ErrNotFound)
		return
	}
	if !canAnswer(p, q, areas) {
		fail(w, http.StatusForbidden, "not_your_question", "This question is waiting on the person who started the run.")
		return
	}
	if q.HandoffID != nil && p.Via != auth.ViaSession {
		fail(w, http.StatusForbidden, "forbidden", "Questions from a conversation can only be answered by signing in to Crew Studio.")
		return
	}
	answered, err := s.store.AnswerQuestion(r.Context(), id, answer, p.UserID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.answers.signal(answered.ID)
	s.questions.notify(answered.AssigneeID)
	s.recordAnswer(r, answered, p.Name)
	ok(w, answered)
}

// recordAnswer adds the answer to the run's journal; a failure is logged and the answer stands.
func (s *Server) recordAnswer(r *http.Request, q model.HumanQuestion, who string) {
	payload := map[string]any{
		"tool": askHumanTool, "message": fmt.Sprintf("%s answered: %s", who, clip(q.Answer, 300)), "detail": "Question: " + clip(q.Question, 300),
	}
	if q.AgentRole != "" {
		payload["agent_role"] = q.AgentRole
	}
	if err := s.stateEvents.RecordStateEvent(r.Context(), q.RunID, payload); err != nil {
		s.log.Warn("record answer", "run_id", q.RunID, "question_id", q.ID, "err", err)
	}
}

type askInput struct {
	Question string `json:"question"`
	Context  string `json:"context"`
	TaskName string `json:"task_name"`
	// AskedFor is the agent the question is asked for: a hand-off's lead desk asks on behalf of a specialist
	AskedFor string `json:"asked_for"`
}

// mayAskPerson reports whether a run may ask a person: a crew run or a hand-off (docs/agent-chat-handoffs-design.md,
// section 6) with an escalation frozen into its spec. A chat turn never may, whatever its spec says: its reply is the
// question.
func mayAskPerson(scope store.RunScope) bool {
	if scope.Escalation == nil {
		return false
	}
	switch scope.Kind {
	case model.RunKindCrew, model.RunKindHandoff:
		return true
	}
	return false
}

func (s *Server) runAskQuestion(w http.ResponseWriter, r *http.Request) {
	call := runCallOf(r)
	esc := call.scope.Escalation
	if !mayAskPerson(call.scope) {
		s.journal(r, askHumanTool, "Ask a human: refused, escalation is off for this run", "")
		fail(w, http.StatusForbidden, "escalation_off", "Asking a person is not available in this run. Decide yourself and state your assumption.")
		return
	}
	var in askInput
	if !decode(w, r, &in) {
		return
	}
	question, context := strings.TrimSpace(in.Question), strings.TrimSpace(in.Context)
	if question == "" || len([]rune(question)) > model.MaxQuestionLen {
		failErr(w, r, s.log, invalid(fmt.Sprintf("question: must be between 1 and %d characters", model.MaxQuestionLen)))
		return
	}
	// Only a hand-off's lead desk asks for another agent; anyone else's claim is dropped
	askedFor := ""
	if call.scope.Kind == model.RunKindHandoff {
		askedFor = cleanAgentRole(in.AskedFor)
	}
	q, err := s.store.AskQuestion(r.Context(), call.scope.RunID, store.NewQuestion{
		AgentRole: call.agentRole, AskedFor: askedFor, TaskName: clip(strings.TrimSpace(in.TaskName), 200),
		Question: question, Context: clip(context, model.MaxQuestionContextLen),
	}, time.Duration(esc.TimeoutMinutes)*time.Minute, esc.MaxQuestions)
	switch {
	case errors.Is(err, store.ErrQuestionLimit):
		s.journal(r, askHumanTool, fmt.Sprintf("Ask a human: refused, the run already asked %d questions", esc.MaxQuestions), clip(question, 300))
		tooMany(w, fmt.Sprintf("This run has already asked %d questions, the most its rules allow. Decide yourself and state your assumption.", esc.MaxQuestions))
		return
	case errors.Is(err, store.ErrQuestionNobody):
		s.journal(r, askHumanTool, "Ask a human: refused, there is no one to ask (the conversation's owner is gone)", clip(question, 300))
		fail(w, http.StatusForbidden, "escalation_off", "There is no one to ask in this run. Decide yourself and state your assumption.")
		return
	case errors.Is(err, store.ErrQuestionNoTime):
		s.journal(r, askHumanTool, "Ask a human: refused, too little of the hand-off's time is left for an answer", clip(question, 300))
		fail(w, http.StatusConflict, "handoff_out_of_time",
			"The hand-off has too little time left for anyone to answer. Decide yourself and state your assumption.")
		return
	case err != nil:
		failErr(w, r, s.log, err)
		return
	}
	s.questions.notify(q.AssigneeID)
	who := "Asked a human: "
	if askedFor != "" {
		who = fmt.Sprintf("Asked a human for %s: ", askedFor)
	}
	wait := max(1, int(math.Ceil(time.Until(q.DeadlineAt).Minutes())))
	s.journal(r, askHumanTool, who+clip(question, 300), fmt.Sprintf("Waiting up to %d minutes for an answer.", wait))
	created(w, runQuestionView(q, esc.OnTimeout))
}

func (s *Server) runGetQuestion(w http.ResponseWriter, r *http.Request) {
	id, valid := pathUUID(w, r, "qid")
	if !valid {
		return
	}
	call := runCallOf(r)
	q, err := s.store.RunQuestion(r.Context(), call.scope.RunID, id)
	if err == nil && q.Status == model.QuestionOpen {
		q, err = s.waitForAnswer(r, q, waitParam(r))
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	onTimeout := model.EscalationProceed
	if call.scope.Escalation != nil {
		onTimeout = call.scope.Escalation.OnTimeout
	}
	ok(w, runQuestionView(q, onTimeout))
}

// maxAnswerWait keeps a long-poll well inside the run-state request timeout
const maxAnswerWait = 25 * time.Second

// waitParam reads ?wait=<seconds>, capped at maxAnswerWait; 0 (or none) answers at once.
func waitParam(r *http.Request) time.Duration {
	secs, err := strconv.Atoi(r.URL.Query().Get("wait"))
	if err != nil || secs <= 0 {
		return 0
	}
	return min(time.Duration(secs)*time.Second, maxAnswerWait)
}

// waitForAnswer holds the request until the question is answered, its deadline passes, wait runs out or the runner
// goes away, then reads the question again.
func (s *Server) waitForAnswer(r *http.Request, q model.HumanQuestion, wait time.Duration) (model.HumanQuestion, error) {
	if wait <= 0 {
		return q, nil
	}
	answered := s.answers.wait(q.ID)
	defer s.answers.forget(q.ID, answered)
	// Re-read after registering, so an answer that landed in between is not missed
	q, err := s.store.RunQuestion(r.Context(), q.RunID, q.ID)
	if err != nil || q.Status != model.QuestionOpen {
		return q, err
	}
	timer := time.NewTimer(min(wait, time.Until(q.DeadlineAt)+time.Second))
	defer timer.Stop()
	select {
	case <-answered:
	case <-timer.C:
	case <-r.Context().Done():
		return q, r.Context().Err()
	}
	return s.store.RunQuestion(r.Context(), q.RunID, q.ID)
}

// runQuestionView is what the runner sees: the status, the answer, and what to do if nobody answers.
func runQuestionView(q model.HumanQuestion, onTimeout string) map[string]any {
	return map[string]any{
		"id": q.ID, "status": q.Status, "answer": q.Answer, "answered_by": q.AnsweredByName,
		"deadline_at": q.DeadlineAt, "on_timeout": onTimeout,
	}
}

// clip shortens text to at most n runes.
func clip(text string, n int) string {
	if r := []rune(text); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return text
}
