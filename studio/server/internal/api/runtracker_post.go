package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// Posting to a tracker from a run. Everything a post must respect is enforced here in code, not by the prompt:
//   - dry run: nothing is posted; the run gets a "would post" answer and a dry-run action log entry
//   - every post (or would-post) is written to the action log, under followup:<step> when it answers a follow-up
//   - a run may post at most comment_cap_per_run comments (the schedule's watch, default 30)
//   - one person may be mentioned at most mention_cap_per_day times a day by the crew (default 3)
//   - only known people can be mentioned: the connection's people map, the issue's assignee and lead, and the
//     tracking list's lead overrides
//   - a follow-up step is posted once per run and issue
//   - new issues only where an admin turned that on for the connection

const (
	loggedTextChars  = 4_000
	mentionCapWindow = 24 * time.Hour
	// postTimeout bounds a post and its log entry once started, independent of the caller
	postTimeout = 90 * time.Second
)

// postKey makes a post idempotent within a run: a follow-up step once per issue, and any other post once per issue
// and exact text and mentions, so a tool call retried after a timeout does not post twice.
func postKey(runID, subject string, p post) string {
	if p.once {
		return "run:" + runID + ":" + subject + ":" + p.action
	}
	sum := sha256.Sum256([]byte(p.action + "\x00" + p.text + "\x00" + strings.Join(p.mentions, ",")))
	return "run:" + runID + ":" + subject + ":" + p.action + ":" + hex.EncodeToString(sum[:8])
}

// linearProfileLink is a Linear profile URL, which Linear turns into a mention; agents must use mentions[] instead,
// so the mention rules and caps apply.
var linearProfileLink = tracker.LinearProfileLink

// postResult answers a posting tool.
type postResult struct {
	Posted bool   `json:"posted"`
	DryRun bool   `json:"dry_run"`
	Issue  string `json:"issue"`
	URL    string `json:"url,omitempty"`
	// WouldPost is what a dry run would have posted
	WouldPost *wouldPost         `json:"would_post,omitempty"`
	Logged    *model.ActionEntry `json:"logged"`
	Message   string             `json:"message"`
}

type wouldPost struct {
	Connection string   `json:"connection"`
	Issue      string   `json:"issue"`
	Mentions   []string `json:"mentions"`
	Text       string   `json:"text"`
}

// post is one comment to post (a comment, or a suggestion written as one).
type post struct {
	tool     string
	action   string
	ref      string
	text     string
	mentions []string
	// once makes the post happen at most once per run and issue (follow-ups)
	once bool
}

type trackerCaps struct {
	mentionsPerDay int
	commentsPerRun int
}

// capsOf reads the caps of the run's schedule's watch, or the defaults.
func (s *Server) capsOf(ctx context.Context, scope store.RunScope) (trackerCaps, error) {
	caps := trackerCaps{mentionsPerDay: model.DefaultMentionCapPerDay, commentsPerRun: model.DefaultCommentCapPerRun}
	if scope.ScheduleID == nil {
		return caps, nil
	}
	w, err := s.store.WatchOfSchedule(ctx, *scope.ScheduleID)
	if errors.Is(err, store.ErrNotFound) {
		return caps, nil
	}
	if err != nil {
		return caps, err
	}
	return trackerCaps{mentionsPerDay: w.MentionCapPerDay, commentsPerRun: w.CommentCapPerRun}, nil
}

// refusePost journals and answers a refused post.
func (s *Server) refusePost(w http.ResponseWriter, r *http.Request, tool string, status int, code, msg string) {
	s.journal(r, tool, "Tracker: post refused: "+msg, "")
	fail(w, status, code, msg)
}

// withinCommentCap answers itself when the run has already posted its share.
func (s *Server) withinCommentCap(w http.ResponseWriter, r *http.Request, tool string, caps trackerCaps) bool {
	n, err := s.store.RunTrackerPosts(r.Context(), runCallOf(r).scope.RunID)
	if err != nil {
		failErr(w, r, s.log, err)
		return false
	}
	if n >= caps.commentsPerRun {
		s.refusePost(w, r, tool, http.StatusTooManyRequests, "comment_cap",
			fmt.Sprintf("This run has already posted %d tracker comments, the most one run may (comment_cap_per_run). Put the rest in your output instead.", n))
		return false
	}
	return true
}

// mentionable resolves mentions against the people the crew may mention on this issue, answering itself on failure.
func (s *Server) mentionable(w http.ResponseWriter, r *http.Request, t runTracker, issue tracker.Issue, p post, caps trackerCaps) ([]tracker.Person, bool) {
	if len(p.mentions) == 0 {
		return nil, true
	}
	known, err := s.knownPeople(r.Context(), runCallOf(r).scope.CrewID, t, issue)
	if err != nil {
		s.trackerCallFailed(w, r, p.tool, "look up people", err)
		return nil, false
	}
	call := runCallOf(r)
	out := make([]tracker.Person, 0, len(p.mentions))
	for _, id := range p.mentions {
		person, found := known[id]
		if !found {
			s.refusePost(w, r, p.tool, http.StatusUnprocessableEntity, "unknown_person", fmt.Sprintf("%s is not someone this crew may mention: "+
				"use the issue's assignee or lead, a tracking item's lead, or someone in the connection's people map.", safeName(id)))
			return nil, false
		}
		n, err := s.store.MentionsSince(r.Context(), call.scope.CrewID, id, time.Now().Add(-mentionCapWindow), call.scope.DryRun)
		if err != nil {
			failErr(w, r, s.log, err)
			return nil, false
		}
		if n >= caps.mentionsPerDay {
			// The account id, not the tracker's display name: refusals are not fenced, and names are tracker data
			s.refusePost(w, r, p.tool, http.StatusTooManyRequests, "mention_cap", fmt.Sprintf("Account %s was already mentioned %d times in the "+
				"last 24 hours by this crew (mention_cap_per_day). Nothing was posted; leave them out or try tomorrow.", id, n))
			return nil, false
		}
		if t.client.Kind() == model.TrackerLinear && person.URL == "" {
			if full, err := t.client.User(r.Context(), id); err == nil {
				person = full
			}
		}
		out = append(out, person)
	}
	return out, true
}

func nameOr(p tracker.Person) string {
	if p.Name != "" {
		return p.Name
	}
	return p.AccountID
}

// knownPeople are the accounts a post on this issue may mention, by account id.
func (s *Server) knownPeople(ctx context.Context, crewID string, t runTracker, issue tracker.Issue) (map[string]tracker.Person, error) {
	out := map[string]tracker.Person{}
	people, err := s.store.ListTrackerPeople(ctx, t.conn.ID)
	if err != nil {
		return nil, err
	}
	for _, p := range people {
		out[p.AccountID] = tracker.Person{AccountID: p.AccountID, Name: p.DisplayName}
	}
	leads, err := s.store.TrackingLeads(ctx, crewID, t.conn.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range leads {
		if _, ok := out[id]; !ok {
			out[id] = tracker.Person{AccountID: id}
		}
	}
	for _, p := range []*tracker.Person{issue.Assignee, issue.Lead} {
		if p != nil {
			out[p.AccountID] = *p
		}
	}
	if model.TrackerHasProjectLeads(t.client.Kind()) && issue.Project != "" {
		project, err := t.client.Project(ctx, issue.Project)
		if err == nil && project.Lead != nil {
			out[project.Lead.AccountID] = *project.Lead
		}
	}
	return out, nil
}

// postComment runs the whole pipeline for one comment, holding the crew's post lock so the caps and the follow-up
// repeat check cannot be raced by parallel tool calls.
func (s *Server) postComment(w http.ResponseWriter, r *http.Request, t runTracker, p post) {
	s.withPostLock(w, r, func() { s.postCommentLocked(w, r, t, p) })
}

// withPostLock runs fn under the crew's tracker post lock, answering itself when the lock cannot be taken.
func (s *Server) withPostLock(w http.ResponseWriter, r *http.Request, fn func()) {
	if err := s.store.WithCrewPostLock(r.Context(), runCallOf(r).scope.CrewID, func() error { fn(); return nil }); err != nil {
		failErr(w, r, s.log, err)
	}
}

func (s *Server) postCommentLocked(w http.ResponseWriter, r *http.Request, t runTracker, p post) {
	call := runCallOf(r)
	caps, err := s.capsOf(r.Context(), call.scope)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if t.conn.Kind == model.TrackerLinear && linearProfileLink.MatchString(p.text) {
		s.refusePost(w, r, p.tool, http.StatusUnprocessableEntity, "invalid", "Do not put Linear profile links in the text; name people in mentions instead.")
		return
	}
	if !s.withinCommentCap(w, r, p.tool, caps) {
		return
	}
	issue, err := t.client.Issue(r.Context(), p.ref)
	if err != nil {
		s.trackerCallFailed(w, r, p.tool, "read "+p.ref, err)
		return
	}
	subject := model.TrackerSubject(t.conn.Kind, issue.Key)
	key := postKey(call.scope.RunID, subject, p)
	if prior, err := s.store.ActionByKey(r.Context(), call.scope.CrewID, key); err != nil || prior != nil {
		if err != nil {
			failErr(w, r, s.log, err)
			return
		}
		s.journal(r, p.tool, "Tracker: "+p.action+" for "+subject+" was already posted in this run", "")
		ok(w, postResult{Issue: issue.Key, DryRun: prior.DryRun, Logged: prior, Message: "This was already posted on " +
			issue.Key + " in this run; nothing new was posted."})
		return
	}
	mentions, allowed := s.mentionable(w, r, t, issue, p, caps)
	if !allowed {
		return
	}
	details := map[string]any{"tool": p.tool, "connection": t.conn.Name, "connection_id": t.conn.ID, "issue": issue.Key,
		"mentions": mentionIDs(mentions), "mention_names": mentionNames(mentions), "text": cut(p.text, loggedTextChars)}
	if call.scope.DryRun {
		s.wouldPost(w, r, t, p, issue.Key, subject, key, mentions, details)
		return
	}
	// Once the post is sent it must be logged, even if the caller gave up waiting: a detached context with its own limit
	postCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), postTimeout)
	defer cancel()
	posted, err := t.client.PostComment(postCtx, issue.Key, p.text, mentions)
	if err != nil {
		s.trackerCallFailed(w, r, p.tool, "comment on "+issue.Key, err)
		return
	}
	details["comment_id"], details["url"] = posted.ID, posted.URL
	entry := s.logPost(r, p.tool, subject, p.action, key, details)
	s.journal(r, p.tool, fmt.Sprintf("Tracker: commented on %s in %s%s", issue.Key, t.conn.Name, mentionText(mentions)), "")
	created(w, postResult{Posted: true, Issue: issue.Key, URL: posted.URL, Logged: entry, Message: "Posted on " + issue.Key + "."})
}

// wouldPost answers a dry run: nothing is posted, and a dry-run log entry records what would have been.
func (s *Server) wouldPost(w http.ResponseWriter, r *http.Request, t runTracker, p post, issueKey, subject, key string, mentions []tracker.Person, details map[string]any) {
	details["would_post"] = true
	entry := s.logPost(r, p.tool, subject, p.action, key, details)
	s.journal(r, p.tool, fmt.Sprintf("Tracker: dry run, would comment on %s in %s%s (not posted)", issueKey, t.conn.Name, mentionText(mentions)), "")
	ok(w, postResult{DryRun: true, Issue: issueKey, Logged: entry,
		WouldPost: &wouldPost{Connection: t.conn.Name, Issue: issueKey, Mentions: mentionNames(mentions), Text: p.text},
		Message:   "Dry run: nothing was posted. Add this to the \"Would post\" section of your output."})
}

// logPost records a post in the action log. A failure is logged and journalled; the post itself stands.
func (s *Server) logPost(r *http.Request, tool, subject, action, key string, details map[string]any) *model.ActionEntry {
	call := runCallOf(r)
	rec, err := model.NormalizeLogRecord(model.LogRecord{Subject: subject, Action: action, Details: details, IdempotencyKey: key})
	var entry model.ActionEntry
	if err == nil {
		runID := call.scope.RunID
		ctx := context.WithoutCancel(r.Context())
		// Tried twice: an unlogged post would not count against the caps
		for attempt := 0; attempt < 2; attempt++ {
			if entry, _, err = s.store.RecordAction(ctx, call.stateScope(), &runID, rec); err == nil {
				break
			}
		}
	}
	if err != nil {
		s.log.Error("record tracker post", "run_id", call.scope.RunID, "subject", subject, "err", err)
		s.journal(r, tool, "Log: the post could not be recorded: "+problemText(err), "")
		return nil
	}
	return &entry
}

func mentionIDs(ps []tracker.Person) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.AccountID
	}
	return out
}

func mentionNames(ps []tracker.Person) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = nameOr(p)
	}
	return out
}

func mentionText(ps []tracker.Person) string {
	if len(ps) == 0 {
		return ""
	}
	return " mentioning " + strings.Join(mentionNames(ps), ", ")
}

func cut(text string, n int) string {
	if r := []rune(text); len(r) > n {
		return string(r[:n]) + "…"
	}
	return text
}

func (s *Server) runComment(w http.ResponseWriter, r *http.Request) {
	const tool = "tracker_comment"
	ref, valid := s.runRef(w, r, tool)
	if !valid {
		return
	}
	var in model.TrackerComment
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeTrackerComment(in)
	if err != nil {
		s.journal(r, tool, "Tracker: comment refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	if !s.withinWriteQuota(w, r, tool) {
		return
	}
	t, found := s.trackerOf(w, r, tool, clean.Connection)
	if !found {
		return
	}
	p := post{tool: tool, action: "tracker:comment", ref: ref, text: clean.Text, mentions: clean.Mentions}
	if clean.FollowupStep != "" {
		p.action, p.once = "followup:"+clean.FollowupStep, true
	}
	s.postComment(w, r, t, p)
}

func (s *Server) runSuggestChange(w http.ResponseWriter, r *http.Request) {
	const tool = "tracker_suggest_change"
	ref, valid := s.runRef(w, r, tool)
	if !valid {
		return
	}
	var in model.TrackerSuggestion
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeTrackerSuggestion(in)
	if err != nil {
		s.journal(r, tool, "Tracker: suggestion refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	if !s.withinWriteQuota(w, r, tool) {
		return
	}
	t, found := s.trackerOf(w, r, tool, clean.Connection)
	if !found {
		return
	}
	crew := runCallOf(r).scope.CrewName
	text := fmt.Sprintf("Suggested change from %s (Crew Studio): set %s to %q.\nReason: %s\n\nCrew Studio does not change fields; "+
		"a person decides whether to make this change.", crew, strings.ReplaceAll(clean.Field, "_", " "), clean.Value, clean.Reason)
	s.postComment(w, r, t, post{tool: tool, action: "tracker:suggest", ref: ref, text: text, mentions: clean.Mentions})
}

func (s *Server) runCreateIssue(w http.ResponseWriter, r *http.Request) {
	const tool = "tracker_create_issue"
	var in model.TrackerNewIssue
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeTrackerNewIssue(in)
	if err != nil {
		s.journal(r, tool, "Tracker: create refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	if !s.withinWriteQuota(w, r, tool) {
		return
	}
	t, found := s.trackerOf(w, r, tool, clean.Connection)
	if !found {
		return
	}
	if t.conn.Config[model.ConfigAllowCreate] != "true" {
		s.refusePost(w, r, tool, http.StatusForbidden, "create_off", fmt.Sprintf("Creating issues is turned off for %s. Propose the new "+
			"issue in your output instead; an administrator can turn this on for the connection.", t.conn.Name))
		return
	}
	if t.conn.Kind == model.TrackerLinear && linearProfileLink.MatchString(clean.Title+" "+clean.Description) {
		s.refusePost(w, r, tool, http.StatusUnprocessableEntity, "invalid", "Do not put Linear profile links in a new issue; they would mention people.")
		return
	}
	s.withPostLock(w, r, func() { s.createIssueLocked(w, r, t, clean) })
}

func (s *Server) createIssueLocked(w http.ResponseWriter, r *http.Request, t runTracker, clean model.TrackerNewIssue) {
	const tool = "tracker_create_issue"
	call := runCallOf(r)
	caps, err := s.capsOf(r.Context(), call.scope)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if !s.withinCommentCap(w, r, tool, caps) {
		return
	}
	details := map[string]any{"tool": tool, "connection": t.conn.Name, "connection_id": t.conn.ID, "project": clean.Project,
		"title": clean.Title, "parent": clean.Parent, "text": cut(clean.Description, loggedTextChars)}
	if call.scope.DryRun {
		details["would_post"] = true
		entry := s.logPost(r, tool, model.TrackerSubject(t.conn.Kind, clean.Project), "tracker:create", "", details)
		s.journal(r, tool, fmt.Sprintf("Tracker: dry run, would create %q in %s %s (not created)", clean.Title, t.conn.Name, clean.Project), "")
		ok(w, postResult{DryRun: true, Issue: clean.Project, Logged: entry, WouldPost: &wouldPost{Connection: t.conn.Name, Issue: clean.Project,
			Mentions: []string{}, Text: clean.Title + "\n\n" + clean.Description}, Message: "Dry run: nothing was created. Add it to the \"Would post\" section of your output."})
		return
	}
	issue, err := t.client.CreateIssue(r.Context(), tracker.NewIssue{Project: clean.Project, Title: clean.Title, Description: clean.Description, Parent: clean.Parent})
	if err != nil {
		s.trackerCallFailed(w, r, tool, "create in "+clean.Project, err)
		return
	}
	details["issue"], details["url"] = issue.Key, issue.URL
	entry := s.logPost(r, tool, model.TrackerSubject(t.conn.Kind, issue.Key), "tracker:create", "", details)
	s.journal(r, tool, fmt.Sprintf("Tracker: created %s in %s", issue.Key, t.conn.Name), "")
	created(w, postResult{Posted: true, Issue: issue.Key, URL: issue.URL, Logged: entry, Message: "Created " + issue.Key + "."})
}
