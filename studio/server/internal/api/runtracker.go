package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// Tracker tools during a run (design section 11, option B): agents call these with the run token and Studio calls
// Jira or Linear itself, so tracker secrets never leave the server. Only the tracker connections in the run's scope
// (the crew may use them and they share an area with the run) can be reached. Every call is journalled. Writes are
// limited to comments (and, when an admin allows it per connection, new issues); fields are never edited.

const (
	maxFoundIssues     = 50
	maxSearchedIssues  = 1000
	maxListedChildren  = 200
	issueCommentsShown = 10
	maxListedProjects  = 200
)

// trackerRunRoutes hangs off /run-state (see runStateRoutes).
func (s *Server) trackerRunRoutes(r chi.Router) {
	r.Get("/tracking", s.chatTool("tracking_list_get", s.runTrackingList))
	r.Get("/trackers", s.chatTool("tracker_list_projects", s.runTrackers))
	r.Get("/trackers/projects", s.chatTool("tracker_list_projects", s.runTrackerProjects))
	r.Get("/trackers/issues", s.chatTool("tracker_find_issues", s.runFindIssues))
	r.Get("/trackers/issues/{ref}", s.chatTool("tracker_get_issue", s.runGetIssue))
	r.Get("/trackers/issues/{ref}/children", s.chatTool("tracker_get_children", s.runIssueChildren))
	r.Post("/trackers/issues/{ref}/comments", s.refuseChat("tracker_comment", s.runComment))
	r.Post("/trackers/issues/{ref}/suggestions", s.refuseChat("tracker_suggest_change", s.runSuggestChange))
	r.Post("/trackers/issues", s.refuseChat("tracker_create_issue", s.runCreateIssue))
}

// runTracker is a tracker connection resolved for a run call.
type runTracker struct {
	conn   model.Connection
	client tracker.Client
}

// trackerOf picks the connection named by ?connection= (an id or a name) among the run's tracker connections, or
// the only one when the run has exactly one. It answers itself on failure.
func (s *Server) trackerOf(w http.ResponseWriter, r *http.Request, tool, wanted string) (runTracker, bool) {
	call := runCallOf(r)
	// A chat turn may only use the connections the person chatting can see
	conns, err := s.runConnections(r.Context(), call.scope)
	if err != nil {
		failErr(w, r, s.log, err)
		return runTracker{}, false
	}
	var matches []store.RunConnection
	for _, c := range conns {
		if !model.IsTrackerKind(c.Kind) {
			continue
		}
		if wanted == "" || c.ID == wanted || strings.EqualFold(c.Name, wanted) {
			matches = append(matches, c)
		}
	}
	switch {
	case len(matches) == 0 && wanted == "":
		s.journal(r, tool, "Tracker: refused, the run has no tracker connection", "")
		fail(w, http.StatusConflict, "no_tracker", noConnectionText(call.scope, "tracker connection (Jira, Linear or KM2 issues)"))
		return runTracker{}, false
	case len(matches) == 0:
		s.journal(r, tool, "Tracker: refused, unknown connection "+safeName(wanted), "")
		fail(w, http.StatusNotFound, "not_found", "This run has no tracker connection named "+strconv.Quote(safeName(wanted))+".")
		return runTracker{}, false
	case len(matches) > 1:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		fail(w, http.StatusBadRequest, "invalid", "Say which connection to use: "+strings.Join(names, ", ")+".")
		return runTracker{}, false
	}
	if !s.trackers.Allow(matches[0].ID) {
		s.journal(r, tool, "Tracker: refused, too many calls to "+matches[0].Name+" just now", "")
		tooMany(w, "Too many tracker calls through "+matches[0].Name+" just now; slow down.")
		return runTracker{}, false
	}
	conn, client, err := s.runTrackerClient(r.Context(), call.scope, matches[0].ID)
	if err != nil {
		s.journal(r, tool, "Tracker: refused: "+tracker.SafeMessage(err), "")
		s.trackerFailure(w, r, err)
		return runTracker{}, false
	}
	return runTracker{conn: conn, client: client}, true
}

// runRef reads {ref} as an issue key.
func (s *Server) runRef(w http.ResponseWriter, r *http.Request, tool string) (string, bool) {
	ref, valid := model.NormalizeItemRef(chi.URLParam(r, "ref"))
	if !valid {
		s.journal(r, tool, "Tracker: refused, "+safeName(chi.URLParam(r, "ref"))+" is not an issue key", "")
		fail(w, http.StatusBadRequest, "invalid", "Give the issue's key, such as PRJ-15.")
		return "", false
	}
	return ref, true
}

// trackerCallFailed journals and answers a tracker failure.
func (s *Server) trackerCallFailed(w http.ResponseWriter, r *http.Request, tool, what string, err error) {
	s.journal(r, tool, "Tracker: "+what+" failed: "+tracker.SafeMessage(err), "")
	s.trackerFailure(w, r, err)
}

func (s *Server) runTrackers(w http.ResponseWriter, r *http.Request) {
	out := []store.RunConnection{}
	// A chat turn lists only the connections the person chatting can see
	conns, err := s.runConnections(r.Context(), runCallOf(r).scope)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	for _, c := range conns {
		if model.IsTrackerKind(c.Kind) {
			out = append(out, c)
		}
	}
	s.journal(r, "tracker_list_projects", fmt.Sprintf("Tracker: listed %d tracker connections", len(out)), "")
	ok(w, out)
}

func (s *Server) runTrackingList(w http.ResponseWriter, r *http.Request) {
	call := runCallOf(r)
	items, err := s.store.ListTrackingItems(r.Context(), store.TrackingFilter{CrewID: call.scope.CrewID, ScheduleID: call.scope.ScheduleID})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	items, visible := s.visibleTrackingItems(w, r, items)
	if !visible {
		return
	}
	s.journal(r, "tracking_list_get", fmt.Sprintf("Tracking list: read %d items", len(items)), "")
	ok(w, items)
}

func (s *Server) runTrackerProjects(w http.ResponseWriter, r *http.Request) {
	t, found := s.trackerOf(w, r, "tracker_list_projects", r.URL.Query().Get("connection"))
	if !found {
		return
	}
	projects, err := t.client.Projects(r.Context(), maxListedProjects)
	if err != nil {
		s.trackerCallFailed(w, r, "tracker_list_projects", "list projects", err)
		return
	}
	s.journal(r, "tracker_list_projects", fmt.Sprintf("Tracker: listed %d projects in %s", len(projects), t.conn.Name), "")
	ok(w, projects)
}

// findQuery reads tracker_find_issues' filters.
func findQuery(r *http.Request) (string, tracker.Filter, bool, error) {
	q := r.URL.Query()
	project, valid := model.NormalizeProjectKey(q.Get("project"))
	if !valid {
		return "", tracker.Filter{}, false, invalid("project: give the Jira project key, Linear team key or KM2 project key, such as PRJ")
	}
	f := tracker.Filter{Overdue: q.Get("overdue") == "true", Unassigned: q.Get("unassigned") == "true", Blocked: q.Get("blocked") == "true",
		Owner: strings.TrimSpace(q.Get("owner"))}
	var err error
	if f.DueWithin, err = dayParam(q.Get("due_within_days")); err == nil {
		f.StaleDays, err = dayParam(q.Get("not_updated_days"))
	}
	if err == nil && len(f.Owner) > 200 {
		err = invalid("owner: is too long")
	}
	return project, f, q.Get("include_done") == "true", err
}

func dayParam(text string) (int, error) {
	if text == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 || n > 3650 {
		return 0, invalid("days must be a whole number from 0 to 3650")
	}
	return n, nil
}

type foundIssues struct {
	Issues []tracker.Issue `json:"issues"`
	// Total is how many matched; only the first 50 are listed
	Total int `json:"total"`
}

func (s *Server) runFindIssues(w http.ResponseWriter, r *http.Request) {
	const tool = "tracker_find_issues"
	project, filter, includeDone, err := findQuery(r)
	if err != nil {
		s.journal(r, tool, "Tracker: find refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	t, found := s.trackerOf(w, r, tool, r.URL.Query().Get("connection"))
	if !found {
		return
	}
	issues, err := t.client.Search(r.Context(), tracker.Query{Project: project, IncludeDone: includeDone, Max: maxSearchedIssues})
	if err != nil {
		s.trackerCallFailed(w, r, tool, "find in "+project, err)
		return
	}
	matched := filter.Apply(issues, time.Now(), time.UTC)
	out := foundIssues{Issues: matched[:min(len(matched), maxFoundIssues)], Total: len(matched)}
	s.journal(r, tool, fmt.Sprintf("Tracker: found %d issues in %s %s", len(matched), t.conn.Name, project), "")
	ok(w, out)
}

type issueView struct {
	tracker.Issue
	Connection string `json:"connection"`
	// Comments are the newest comments, newest first
	Comments []tracker.Comment `json:"comments"`
}

func (s *Server) runGetIssue(w http.ResponseWriter, r *http.Request) {
	const tool = "tracker_get_issue"
	ref, valid := s.runRef(w, r, tool)
	if !valid {
		return
	}
	t, found := s.trackerOf(w, r, tool, r.URL.Query().Get("connection"))
	if !found {
		return
	}
	issue, err := t.client.Issue(r.Context(), ref)
	var comments []tracker.Comment
	if err == nil {
		comments, err = t.client.Comments(r.Context(), issue.Key, issueCommentsShown)
	}
	if err != nil {
		s.trackerCallFailed(w, r, tool, "read "+ref, err)
		return
	}
	s.journal(r, tool, fmt.Sprintf("Tracker: read %s in %s with %d comments", issue.Key, t.conn.Name, len(comments)), "")
	ok(w, issueView{Issue: issue, Connection: t.conn.Name, Comments: comments})
}

type childrenView struct {
	Parent   string          `json:"parent"`
	Depth    string          `json:"depth"`
	Children []tracker.Issue `json:"children"`
}

func (s *Server) runIssueChildren(w http.ResponseWriter, r *http.Request) {
	const tool = "tracker_get_children"
	ref, valid := s.runRef(w, r, tool)
	if !valid {
		return
	}
	depth, err := dayParam(r.URL.Query().Get("depth"))
	if err != nil || depth > tracker.MaxChildDepth {
		fail(w, http.StatusBadRequest, "invalid", fmt.Sprintf("depth: 0 (all levels) to %d", tracker.MaxChildDepth))
		return
	}
	t, found := s.trackerOf(w, r, tool, r.URL.Query().Get("connection"))
	if !found {
		return
	}
	parent, err := t.client.Issue(r.Context(), ref)
	var children []tracker.Issue
	if err == nil {
		children, err = tracker.Children(r.Context(), t.client, parent, depth, tracker.Query{}, maxListedChildren)
	}
	var limit *tracker.LimitError
	if errors.As(err, &limit) {
		s.journal(r, tool, "Tracker: "+ref+" has more than "+strconv.Itoa(maxListedChildren)+" children", "")
		fail(w, http.StatusUnprocessableEntity, "too_many", fmt.Sprintf("%s has more than %d children; ask for fewer levels (depth=1).", ref, maxListedChildren))
		return
	}
	if err != nil {
		s.trackerCallFailed(w, r, tool, "read the children of "+ref, err)
		return
	}
	levels := "all levels"
	if depth > 0 {
		levels = strconv.Itoa(depth) + " level(s)"
	}
	s.journal(r, tool, fmt.Sprintf("Tracker: read %d children of %s (%s)", len(children), parent.Key, levels), "")
	ok(w, childrenView{Parent: parent.Key, Depth: levels, Children: children})
}
