package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/chat"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Work order follow-up: each person's conversation with the crew about a work order, and revising the work order
// with feedback (a new attempt given the previous output, the feedback and, if wanted, the conversation).

// revisionConversationTurns is how many of the latest conversation messages a revision is given
const revisionConversationTurns = 20

// followUpRoutes hang off /work-orders (see workOrderRoutes).
func (s *Server) followUpRoutes(r chi.Router) {
	r.With(s.need(auth.WorkOrdersView)).Get("/{id}/conversation", s.getWorkOrderConversation)
	r.With(s.need(auth.WorkOrdersView)).Post("/{id}/conversation/messages", s.sendWorkOrderMessage)
	r.With(s.need(auth.WorkOrdersView)).Post("/{id}/conversation/compact", s.compactWorkOrderConversation)
	r.With(s.need(auth.WorkOrdersView)).Get("/{id}/feedback", s.listWorkOrderFeedback)
	r.With(s.need(auth.WorkOrdersCreate)).Post("/{id}/revise", s.reviseWorkOrder)
}

// getWorkOrderConversation answers the caller's conversation about the work order, or null before they start one.
func (s *Server) getWorkOrderConversation(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	conv, err := s.chat.WorkOrderConversation(r.Context(), principal(r), wo)
	if errors.Is(err, store.ErrNotFound) {
		ok(w, nil)
		return
	}
	if err != nil {
		s.chatFail(w, r, err)
		return
	}
	view, err := s.conversationView(r.Context(), conv)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, view)
}

func (s *Server) sendWorkOrderMessage(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	in, ok := s.readChatMessage(w, r)
	if !ok {
		return
	}
	s.startMessageTurn(w, r, in, func(p *auth.Principal) (chat.Turn, error) {
		return s.chat.SendWorkOrderMessage(r.Context(), p, wo, in.Content, in.Files...)
	})
}

func (s *Server) compactWorkOrderConversation(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	s.startTurn(w, r, func(p *auth.Principal) (chat.Turn, error) {
		return s.chat.CompactWorkOrderConversation(r.Context(), p, wo)
	})
}

func (s *Server) listWorkOrderFeedback(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	list, err := s.store.ListFeedback(r.Context(), wo.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, list)
}

type reviseRequest struct {
	Feedback            string `json:"feedback"`
	IncludeConversation bool   `json:"include_conversation"`
}

type reviseResponse struct {
	Run      model.Run               `json:"run"`
	Feedback model.WorkOrderFeedback `json:"feedback"`
}

// reviseWorkOrder starts a new attempt that revises the latest one with the caller's feedback.
func (s *Server) reviseWorkOrder(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	var in reviseRequest
	if !decode(w, r, &in) {
		return
	}
	feedback, err := model.NormalizeFeedback(in.Feedback)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	p := principal(r)
	if !s.runStarts.allow(p.UserID) {
		tooMany(w, "Work orders are being submitted too quickly; wait a moment.")
		return
	}
	revision, err := s.revision(r, wo, feedback, in.IncludeConversation)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	note, err := s.store.AddFeedback(r.Context(), store.NewFeedback{
		WorkOrderID: wo.ID, RunID: *wo.CurrentRunID, Feedback: feedback, WithConversation: len(revision.Conversation) > 0,
		CreatedBy: p.UserID, CreatedByName: p.Name,
	})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	run, _, err := s.launcher.SubmitWorkOrder(r.Context(), p, wo.ID, launch.SubmitOptions{Revision: revision})
	if err != nil {
		// Nothing acted on the feedback, so it is not kept as if it had been
		if derr := s.store.DeleteFeedback(r.Context(), note.ID); derr != nil {
			s.log.Warn("delete feedback of a revision that did not start", "feedback_id", note.ID, "err", derr)
		}
		failErr(w, r, s.log, err)
		return
	}
	if note, err = s.store.SetFeedbackRevision(r.Context(), note.ID, run.ID); err != nil {
		// The revision runs; only the link from the feedback is missing
		s.log.Warn("link feedback to its revision", "feedback_id", note.ID, "run_id", run.ID, "err", err)
	}
	created(w, reviseResponse{Run: run, Feedback: note})
}

// revision is what the new attempt gets: the latest attempt's output, the feedback and, if wanted, the caller's
// conversation about the work order. The latest attempt must have finished.
func (s *Server) revision(r *http.Request, wo model.WorkOrder, feedback string, withConversation bool) (*model.RevisionSpec, error) {
	if wo.CurrentRunID == nil {
		return nil, &store.ConflictError{Code: "not_submitted", Message: "This work order has not run yet, so there is nothing to revise; submit it first."}
	}
	run, err := s.store.GetRun(r.Context(), *wo.CurrentRunID)
	if err != nil {
		return nil, err
	}
	if !run.Status.Terminal() {
		return nil, store.ErrWorkOrderActive
	}
	p := principal(r)
	rev := &model.RevisionSpec{Attempt: wo.RunCount, Feedback: feedback, FeedbackBy: p.Name}
	if run.Output != nil {
		rev.PreviousOutput = model.CutText(*run.Output, model.MaxRevisionOutputChars)
	}
	if !withConversation {
		return rev, nil
	}
	conv, err := s.store.WorkOrderConversation(r.Context(), wo.ID, p.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return rev, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.store.SettleReplies(r.Context(), conv.ID, chat.ProposalTTL); err != nil {
		return nil, err
	}
	turns, err := s.store.RecentTurns(r.Context(), conv.ID, revisionConversationTurns)
	if err != nil {
		return nil, err
	}
	for _, t := range turns {
		t.Content = model.CutText(t.Content, 4_000)
		rev.Conversation = append(rev.Conversation, t)
	}
	return rev, nil
}
