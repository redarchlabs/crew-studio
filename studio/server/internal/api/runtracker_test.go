package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker/trackertest"
)

// trackerRun is a scheduled run whose crew may use a fake Jira connection.
func (e trackerEnv) trackerRun(t *testing.T) (stateRun, model.Connection) {
	t.Helper()
	sr := e.scheduledRun(t)
	conn := e.jiraConnection(t, nil)
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	return sr, conn
}

func (e trackerEnv) actions(t *testing.T, crewID string) []model.ActionEntry {
	t.Helper()
	items, _, err := e.st.ListActions(context.Background(), store.ActionFilter{Scope: store.StateScope{CrewID: crewID}, AllSchedules: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestRunTrackerReads(t *testing.T) {
	e := newTrackerEnv(t)
	sr, conn := e.trackerRun(t)
	trackers := decodeData[[]store.RunConnection](t, e.call(t, sr, "GET", "/trackers", nil))
	if len(trackers) != 1 || trackers[0].ID != conn.ID {
		t.Errorf("trackers = %+v", trackers)
	}
	projects := decodeData[[]map[string]any](t, e.call(t, sr, "GET", "/trackers/projects?connection=acme%20jira", nil))
	if len(projects) != 1 || projects[0]["key"] != "PRJ" {
		t.Errorf("projects = %+v", projects)
	}
	found := decodeData[foundIssues](t, e.call(t, sr, "GET", "/trackers/issues?project=prj&blocked=true", nil))
	if found.Total != 1 || found.Issues[0].Key != "PRJ-11" {
		t.Errorf("found = %+v", found)
	}
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues?project=", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues?project=PRJ&due_within_days=x", nil), http.StatusUnprocessableEntity)
	issue := decodeData[issueView](t, e.call(t, sr, "GET", "/trackers/issues/jira:prj-11", nil))
	if issue.Key != "PRJ-11" || issue.Connection != "Acme Jira" || len(issue.Comments) != 1 || issue.Assignee == nil {
		t.Errorf("issue = %+v", issue)
	}
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues/PRJ-99", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues/not-a-key!", nil), http.StatusBadRequest)
	children := decodeData[childrenView](t, e.call(t, sr, "GET", "/trackers/issues/PRJ-10/children?depth=1", nil))
	if len(children.Children) != 2 || children.Depth != "1 level(s)" {
		t.Errorf("children = %+v", children)
	}
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues/PRJ-10/children?depth=9", nil), http.StatusBadRequest)
	wantStatus(t, e.call(t, sr, "GET", "/trackers/issues/PRJ-10?connection=nope", nil), http.StatusNotFound)
	if items := decodeData[[]model.TrackingItem](t, e.call(t, sr, "GET", "/tracking", nil)); len(items) != 0 {
		t.Errorf("tracking = %+v", items)
	}
	// tracking_list_get gets the lead's name from the people map
	if _, err := e.st.SeeTrackerPeople(context.Background(), conn.ID, []model.TrackerPerson{{AccountID: "acc-dana", DisplayName: "Dana Ruiz"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddTrackingItem(context.Background(), sr.crew.ID, model.TrackingItemInput{ConnectionID: conn.ID, Project: "PRJ", ItemRef: "PRJ-10",
		IncludeChildren: "all", LeadAccountID: "acc-dana"}, nil); err != nil {
		t.Fatal(err)
	}
	if items := decodeData[[]model.TrackingItem](t, e.call(t, sr, "GET", "/tracking", nil)); len(items) != 1 || items[0].LeadName != "Dana Ruiz" {
		t.Errorf("tracking = %+v", items)
	}

	// Every call is in the journal, and the token never reaches the tracker data
	events, err := e.st.ListEvents(context.Background(), sr.run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	journal := ""
	for _, ev := range events {
		if msg, ok := ev.Payload["message"].(string); ok {
			journal += msg + "\n"
		}
	}
	for _, want := range []string{"Tracker: read PRJ-11 in Acme Jira with 1 comments", "Tracker: found 1 issues", "Tracker: read 2 children of PRJ-10"} {
		if !strings.Contains(journal, want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}

	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, nil, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	other := e.scheduledRunOfNewToken(t, sr)
	r := e.call(t, other, "GET", "/trackers/issues/PRJ-11", nil)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "no_tracker" {
		t.Errorf("no tracker: %+v", r.body.Error)
	}
}

func TestRunTrackerCommentsAreLoggedAndCapped(t *testing.T) {
	e := newTrackerEnv(t)
	sr, _ := e.trackerRun(t)
	clean, err := model.NormalizeWatch(model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceTrackingList}, MentionCapPerDay: 2, CommentCapPerRun: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.SaveWatch(context.Background(), sr.schedule.ID, clean); err != nil {
		t.Fatal(err)
	}

	r := e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "Any update?", "mentions": []string{"acc-sam"}})
	wantStatus(t, r, http.StatusCreated)
	res := decodeData[postResult](t, r)
	if !res.Posted || res.DryRun || res.Logged == nil || res.Logged.Subject != "jira:PRJ-11" || res.Logged.Action != "tracker:comment" ||
		!strings.Contains(res.URL, "/browse/PRJ-11?focusedCommentId=") {
		t.Fatalf("comment = %+v", res)
	}
	posted := e.data.PostedComments()
	if len(posted) != 1 || !strings.Contains(toJSONString(posted[0].Body), `"type":"mention"`) {
		t.Fatalf("posted = %+v", posted)
	}

	// A follow-up: logged under its step, and posted once per run
	follow := map[string]any{"text": "Due today", "mentions": []string{"acc-sam"}, "followup_step": "due_today"}
	res = decodeData[postResult](t, e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", follow))
	if res.Logged == nil || res.Logged.Action != "followup:due_today" || !res.Posted {
		t.Fatalf("follow-up = %+v", res)
	}
	res = decodeData[postResult](t, e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", follow))
	if res.Posted || !strings.Contains(res.Message, "already posted") || len(e.data.PostedComments()) != 2 {
		t.Errorf("repeat follow-up = %+v", res)
	}

	// Sam has been mentioned twice today: the cap (2) refuses a third mention, and nothing is posted
	r = e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "Again", "mentions": []string{"acc-sam"}})
	wantStatus(t, r, http.StatusTooManyRequests)
	if r.body.Error.Code != "mention_cap" || !strings.Contains(r.body.Error.Message, "Account acc-sam was already mentioned 2 times") {
		t.Errorf("mention cap: %+v", r.body.Error)
	}
	r = e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "Hi", "mentions": []string{"acc-zed"}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if r.body.Error.Code != "unknown_person" {
		t.Errorf("unknown person: %+v", r.body.Error)
	}
	// The project lead may be mentioned
	wantStatus(t, e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "Escalating", "mentions": []string{"acc-dana"}}), http.StatusCreated)

	// A suggested change is a comment; the field is never changed
	r = e.call(t, sr, "POST", "/trackers/issues/PRJ-11/suggestions", map[string]any{"field": "due_date", "value": "2026-11-01", "reason": "Vendor slipped"})
	wantStatus(t, r, http.StatusCreated)
	posted = e.data.PostedComments()
	if body := toJSONString(posted[len(posted)-1].Body); !strings.Contains(body, "Suggested change from PE Due Diligence Crew") || !strings.Contains(body, "set due date to") {
		t.Errorf("suggestion = %s", body)
	}
	wantStatus(t, e.call(t, sr, "POST", "/trackers/issues/PRJ-11/suggestions", map[string]any{"field": "summary", "value": "x", "reason": "y"}), http.StatusUnprocessableEntity)

	// Four posts in this run: the per-run cap stops the fifth
	r = e.call(t, sr, "POST", "/trackers/issues/PRJ-10/comments", map[string]any{"text": "One more"})
	wantStatus(t, r, http.StatusTooManyRequests)
	if r.body.Error.Code != "comment_cap" {
		t.Errorf("comment cap: %+v", r.body.Error)
	}
	if n := len(e.data.PostedComments()); n != 4 {
		t.Errorf("posted %d comments", n)
	}
	wantStatus(t, e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": ""}), http.StatusUnprocessableEntity)
}

func TestDryRunNeverPostsToTheTracker(t *testing.T) {
	e := newTrackerEnv(t)
	sr, conn := e.trackerRun(t)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET inputs = '{"dry_run": "true"}' WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "Due soon", "mentions": []string{"acc-sam"}, "followup_step": "due_soon"})
	wantStatus(t, r, http.StatusOK)
	res := decodeData[postResult](t, r)
	if res.Posted || !res.DryRun || res.WouldPost == nil || res.WouldPost.Text != "Due soon" || res.WouldPost.Mentions[0] != "Sam Lee" ||
		res.Logged == nil || !res.Logged.DryRun || res.Logged.Action != "followup:due_soon" {
		t.Fatalf("dry run = %+v", res)
	}
	r = e.call(t, sr, "POST", "/trackers/issues/PRJ-11/suggestions", map[string]any{"field": "status", "value": "Blocked", "reason": "vendor"})
	if res := decodeData[postResult](t, r); res.Posted || !res.DryRun {
		t.Errorf("dry suggestion = %+v", res)
	}
	// Creating issues is off unless an admin turns it on; in a dry run it is still not created
	r = e.call(t, sr, "POST", "/trackers/issues", map[string]any{"project": "PRJ", "title": "Follow up with vendor"})
	wantStatus(t, r, http.StatusForbidden)
	if r.body.Error.Code != "create_off" {
		t.Errorf("create off: %+v", r.body.Error)
	}
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+conn.ID, map[string]any{"name": "Acme Jira", "area_ids": []string{financeArea},
		"config": map[string]string{"site_url": e.jira, "email": "bot@example.com", "allow_create": "true"}}), http.StatusOK)
	r = e.call(t, sr, "POST", "/trackers/issues", map[string]any{"project": "PRJ", "title": "Follow up with vendor", "parent": "PRJ-10"})
	wantStatus(t, r, http.StatusOK)
	if res := decodeData[postResult](t, r); res.Posted || !res.DryRun || res.WouldPost == nil {
		t.Errorf("dry create = %+v", res)
	}
	if len(e.data.PostedComments()) != 0 || len(e.data.Created) != 0 {
		t.Fatalf("a dry run reached the tracker: %d comments, %d issues", len(e.data.PostedComments()), len(e.data.Created))
	}
	for _, a := range e.actions(t, sr.crew.ID) {
		if !a.DryRun || a.Details["would_post"] != true {
			t.Errorf("entry %s %s is not a dry-run entry: %+v", a.Subject, a.Action, a)
		}
	}
}

func TestCreateIssueWhenAllowed(t *testing.T) {
	e := newTrackerEnv(t)
	sr, conn := e.trackerRun(t)
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+conn.ID, map[string]any{"name": "Acme Jira", "area_ids": []string{financeArea},
		"config": map[string]string{"site_url": e.jira, "email": "bot@example.com", "allow_create": "true"}}), http.StatusOK)
	r := e.call(t, sr, "POST", "/trackers/issues", map[string]any{"project": "prj", "title": "Vendor contract", "description": "Needed by Friday"})
	wantStatus(t, r, http.StatusCreated)
	res := decodeData[postResult](t, r)
	if !res.Posted || res.Issue != "NEW-1" || res.Logged == nil || res.Logged.Action != "tracker:create" || len(e.data.Created) != 1 {
		t.Errorf("create = %+v", res)
	}
	wantStatus(t, e.call(t, sr, "POST", "/trackers/issues", map[string]any{"project": "", "title": ""}), http.StatusUnprocessableEntity)
}

func TestChatTurnsCannotPostToTrackers(t *testing.T) {
	e := newTrackerEnv(t)
	sr, _ := e.trackerRun(t)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET kind = 'chat' WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/trackers/issues/PRJ-11/comments", "/trackers/issues/PRJ-11/suggestions", "/trackers/issues"} {
		wantStatus(t, e.call(t, sr, "POST", path, map[string]any{"text": "x", "field": "status", "value": "x", "reason": "y", "project": "PRJ", "title": "t"}), http.StatusForbidden)
	}
	if n := len(e.data.PostedComments()); n != 0 {
		t.Errorf("chat posted %d comments", n)
	}
}

func TestParallelPostsCannotBeatTheCaps(t *testing.T) {
	e := newTrackerEnv(t)
	sr, _ := e.trackerRun(t)
	clean, err := model.NormalizeWatch(model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceTrackingList}, MentionCapPerDay: 2, CommentCapPerRun: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.SaveWatch(context.Background(), sr.schedule.ID, clean); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	statuses := make(chan int, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": fmt.Sprintf("ping %d", i), "mentions": []string{"acc-sam"}})
			statuses <- r.status
		}()
	}
	wg.Wait()
	close(statuses)
	posted := 0
	for s := range statuses {
		if s == http.StatusCreated {
			posted++
		}
	}
	if posted != 2 || len(e.data.PostedComments()) != 2 {
		t.Errorf("posted %d (%d at the tracker), want exactly the mention cap of 2", posted, len(e.data.PostedComments()))
	}
}

func TestARetriedPostIsNotPostedTwice(t *testing.T) {
	e := newTrackerEnv(t)
	sr, _ := e.trackerRun(t)
	body := map[string]any{"text": "Any update?", "mentions": []string{"acc-dana"}}
	wantStatus(t, e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", body), http.StatusCreated)
	r := e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", body)
	wantStatus(t, r, http.StatusOK)
	if res := decodeData[postResult](t, r); res.Posted || !strings.Contains(res.Message, "already posted") {
		t.Errorf("retry = %+v", res)
	}
	// Different text is a different comment
	wantStatus(t, e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "Second question"}), http.StatusCreated)
	if n := len(e.data.PostedComments()); n != 2 {
		t.Errorf("posted %d", n)
	}
}

func TestTrackerLimitsAndThrottle(t *testing.T) {
	e := newTrackerEnv(t)
	sr, _ := e.trackerRun(t)
	for i := 0; i < 1001; i++ {
		e.data.Add(trackertest.Issue{Key: fmt.Sprintf("BIG-%d", i+1), Title: "x"})
	}
	e.data.PageSize = 500
	r := e.call(t, sr, "GET", "/trackers/issues?project=BIG", nil)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if r.body.Error.Code != "too_many" {
		t.Errorf("big project: %+v", r.body.Error)
	}
	// Reads through one connection are throttled (5 a second, burst 30)
	throttled := false
	for i := 0; i < 40 && !throttled; i++ {
		other := e.scheduledRunOfNewToken(t, sr) // a fresh token each time, so the per-run limit is not what stops it
		throttled = e.call(t, other, "GET", "/trackers/projects", nil).status == http.StatusTooManyRequests
	}
	if !throttled {
		t.Error("tracker calls were never throttled")
	}
}

func TestLinearIssuesCannotCarryProfileLinks(t *testing.T) {
	e := newTrackerEnv(t)
	sr := e.scheduledRun(t)
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "Acme Linear", "kind": "linear", "area_ids": []string{financeArea},
		"config": map[string]string{"allow_create": "true"}, "secrets": map[string]string{"api_key": "lin-key"}})
	wantStatus(t, r, http.StatusCreated)
	conn := decodeData[model.Connection](t, r)
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	r = e.call(t, sr, "POST", "/trackers/issues", map[string]any{"project": "PRJ", "title": "x", "description": "cc https://linear.app/acme/profiles/ceo"})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	r = e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "see https://LINEAR.app/acme/profiles/ceo"})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if len(e.data.Created) != 0 || len(e.data.PostedComments()) != 0 {
		t.Error("a profile link reached Linear")
	}
}
