package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/chat"
	"crewstudio/internal/model"
)

// leadRole is the seeded deal partner's role, which the runner sends with each of its run-state calls.
const leadRole = "Private Equity Deal Partner"

// A lead's tracker comment reaches chat as a card (slice 3d): the turn proposes it through the run-state API, the card
// names the connection and who drafted it, and only the owner's Confirm, signed in, posts it, through the connection,
// saying who confirmed it. A tracker that refuses the post answers with its reason and leaves the card pending.
func TestAgentChatCommentCardPostsAsThePerson(t *testing.T) {
	e := newTrackerEnv(t)
	conn := e.jiraConnection(t, nil)
	e.agentChatTracker(t, conn.ID)
	_, ann := e.person(t, "ann@example.com", []string{operatorRole}, []string{financeArea}, []string{auth.AccountAPIKeys})
	view := decodeData[conversationView](t, e.as(ann).do(t, "POST", "/api/chat/conversations", map[string]any{"agent_id": chatPartner,
		"content": "Post the summary on PRJ-11"}))
	sr := e.runningTurn(t, model.Crew{}, *view.Messages[1].RunID)
	propose := func(sr stateRun, issue string) proposeResponse {
		t.Helper()
		r := e.as("").do(t, "POST", "/api/run-state/proposals", map[string]any{"type": chat.TypeTrackerComment,
			"payload": map[string]any{"issue": issue, "text": "Vendor access is still blocked.", "connection": ""}},
			"Authorization", "Bearer "+sr.token, agentRoleHeader, leadRole)
		wantStatus(t, r, http.StatusCreated)
		return decodeData[proposeResponse](t, r)
	}
	card := propose(sr, "prj-11")
	p := card.Proposal
	if p.Summary != "Comment on PRJ-11" || p.Payload["connection_id"] != conn.ID || p.Payload["proposed_by"] != "Deal Partner" {
		t.Fatalf("card = %+v", p)
	}
	if text := e.journalText(t, sr.run.ID); !strings.Contains(text, "Chat: proposed Comment on PRJ-11 (waits for the person to confirm)") {
		t.Errorf("journal = %s", text)
	}
	// The turn still cannot post itself
	wantStatus(t, e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "hi"}), http.StatusForbidden)

	// Only the owner, signed in, can confirm it
	key := e.personalKey(t, ann)
	wantStatus(t, e.as("").do(t, "POST", "/api/chat/messages/"+card.MessageID+"/confirm", map[string]any{}, "Authorization", "Bearer "+key),
		http.StatusForbidden)
	r := e.as(ann).do(t, "POST", "/api/chat/messages/"+card.MessageID+"/confirm", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	done := decodeData[model.ChatMessage](t, r)
	if done.ProposalResult["issue"] != "PRJ-11" || done.ProposalResult["connection"] != conn.Name {
		t.Errorf("result = %v", done.ProposalResult)
	}
	posted := e.data.PostedComments()
	if len(posted) != 1 || posted[0].Issue != "PRJ-11" || !strings.Contains(fmt.Sprint(posted[0].Body), "drafted by Deal Partner") {
		t.Fatalf("posted = %+v", posted)
	}

	// A missing issue (in the next turn: one card per reply): the tracker's answer comes back and the card waits
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET status = 'succeeded', output = 'Proposed.' WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	r = e.as(ann).do(t, "POST", "/api/chat/conversations/"+view.Conversation.ID+"/messages", map[string]any{"content": "And PRJ-99"})
	wantStatus(t, r, http.StatusCreated)
	next := decodeData[conversationView](t, r)
	missing := propose(e.runningTurn(t, model.Crew{}, *next.Messages[len(next.Messages)-1].RunID), "PRJ-99")
	r = e.as(ann).do(t, "POST", "/api/chat/messages/"+missing.MessageID+"/confirm", map[string]any{})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if r.body.Error.Code != "tracker_check_failed" {
		t.Errorf("missing issue = %+v", r.body.Error)
	}
	if m, _, err := e.st.GetMessage(context.Background(), missing.MessageID); err != nil || m.ProposalState == nil || *m.ProposalState != model.ProposalPending {
		t.Errorf("card after the refusal = %+v, %v", m.ProposalState, err)
	}
}

// "Specialists may propose changes" round-trips through the agent's chat settings and is off by default.
func TestAgentChatSpecialistsMayProposeSetting(t *testing.T) {
	e := newEnv(t)
	got := decodeData[model.AgentChatSettings](t, e.do(t, "GET", "/api/agents/"+chatPartner+"/chat", nil))
	if got.SpecialistsMayPropose {
		t.Error("on by default")
	}
	e.handoffAgent(t, map[string]any{"specialists_may_propose": true})
	got = decodeData[model.AgentChatSettings](t, e.do(t, "GET", "/api/agents/"+chatPartner+"/chat", nil))
	if !got.SpecialistsMayPropose {
		t.Errorf("settings = %+v", got)
	}
}
