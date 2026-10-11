package watch

import (
	"slices"
	"strings"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
)

// Roll-ups (design section 12): for each tracked item, code counts and lists what its children are doing. Only
// tracked items with a change somewhere beneath them go to the crew, so a project where nothing moved costs nothing.

// Group is one tracked item with what is below it, as a tracking_list source fetched it.
type Group struct {
	// Ref identifies the tracking-list entry in item meta (TrackedBy), such as jira:PRJ-10
	Ref        string
	Project    string
	Connection string
	Note       string
	Lead       *NotifyPerson
	Parent     tracker.Issue
	Children   []tracker.Issue
	// Kind is the connection kind (jira or linear), to build subjects
	Kind string
}

// ChildCounts counts a tracked item's children by status category.
type ChildCounts struct {
	Total      int `json:"total"`
	Done       int `json:"done"`
	InProgress int `json:"in_progress"`
	ToDo       int `json:"to_do"`
	Canceled   int `json:"canceled"`
}

// DueAfter is a child due after its parent: an early warning of a slip.
type DueAfter struct {
	ID      string `json:"id"`
	DueDate string `json:"due_date"`
}

// StaleChild is an open child with no update for a while.
type StaleChild struct {
	ID         string `json:"id"`
	LastUpdate string `json:"last_update"`
}

// Rollup is one tracked item's roll-up.
type Rollup struct {
	Item       string        `json:"item"`
	Title      string        `json:"title"`
	Project    string        `json:"project"`
	Connection string        `json:"connection"`
	URL        string        `json:"url"`
	Status     string        `json:"status"`
	DueDate    string        `json:"due_date"`
	Note       string        `json:"note,omitempty"`
	Lead       *NotifyPerson `json:"lead,omitempty"`
	Children   ChildCounts   `json:"children"`
	Overdue    []string      `json:"overdue"`
	// DueAfterParent lists open children due after the tracked item itself
	DueAfterParent []DueAfter   `json:"due_after_parent"`
	Blocked        []string     `json:"blocked"`
	Unassigned     []string     `json:"unassigned"`
	Stale          []StaleChild `json:"stale"`
	NewComments    int          `json:"new_comments"`
	// ChangedSinceLastCheck is true when the item or anything below it was added, changed, removed or commented on
	ChangedSinceLastCheck bool `json:"changed_since_last_check"`
}

// Rollups are every tracked item's roll-up.
type Rollups struct {
	Items []Rollup `json:"items"`
	// StaleDays is how long without an update makes an open child stale
	StaleDays int `json:"stale_days"`
}

// RollupsInput is what the crew receives: only the roll-ups with changes beneath them.
type RollupsInput struct {
	Items          []Rollup `json:"items"`
	UnchangedItems int      `json:"unchanged_items"`
	StaleDays      int      `json:"stale_days"`
	HowToUse       string   `json:"how_to_use"`
}

const rollupHowTo = "Each item is a tracked item with counts and lists worked out by code from its child tickets. " +
	"A child due after its parent is an early warning of a slip. Use the new comments in changes for the why, and " +
	"tracker_get_issue for detail."

// Changed is the input for the crew: the roll-ups with changes, and how many tracked items had none.
func (r *Rollups) Changed() RollupsInput {
	out := RollupsInput{Items: []Rollup{}, StaleDays: r.StaleDays, HowToUse: rollupHowTo}
	for _, item := range r.Items {
		if item.ChangedSinceLastCheck {
			out.Items = append(out.Items, item)
		} else {
			out.UnchangedItems++
		}
	}
	return out
}

// BuildRollups works out each group's roll-up and whether anything beneath it changed since the last check.
func BuildRollups(groups []Group, ch Changes, comments map[string]int, prev map[string]ItemMeta, staleDays int, now time.Time, loc *time.Location) *Rollups {
	if staleDays <= 0 {
		staleDays = model.DefaultStaleDays
	}
	touched := touchedIDs(ch)
	removedUnder := map[string]bool{}
	for _, r := range ch.Removed {
		for _, ref := range prev[r.ID].TrackedBy {
			removedUnder[ref] = true
		}
	}
	out := &Rollups{Items: []Rollup{}, StaleDays: staleDays}
	today := now.In(loc).Format(time.DateOnly)
	for _, g := range groups {
		r := rollupOf(g, today, staleDays, now)
		ids := append([]string{g.Ref}, subjectsOf(g.Kind, g.Children)...)
		r.ChangedSinceLastCheck = removedUnder[g.Ref] || ch.PreviousCheckAt == nil
		for _, id := range ids {
			r.NewComments += comments[id]
			if touched[id] || comments[id] > 0 {
				r.ChangedSinceLastCheck = true
			}
		}
		out.Items = append(out.Items, r)
	}
	return out
}

func touchedIDs(ch Changes) map[string]bool {
	out := map[string]bool{}
	for _, a := range ch.Added {
		out[a.ID] = true
	}
	for _, c := range ch.Changed {
		out[c.ID] = true
	}
	return out
}

func subjectsOf(kind string, issues []tracker.Issue) []string {
	out := make([]string, len(issues))
	for i, issue := range issues {
		out[i] = model.TrackerSubject(kind, issue.Key)
	}
	return out
}

func rollupOf(g Group, today string, staleDays int, now time.Time) Rollup {
	p := g.Parent
	r := Rollup{Item: p.Key, Title: p.Title, Project: g.Project, Connection: g.Connection, URL: p.URL, Status: p.Status,
		DueDate: p.DueDate, Note: g.Note, Lead: g.Lead, Overdue: []string{}, DueAfterParent: []DueAfter{}, Blocked: []string{},
		Unassigned: []string{}, Stale: []StaleChild{}}
	for _, c := range g.Children {
		r.Children.Total++
		switch c.StatusCategory {
		case model.StatusDone:
			r.Children.Done++
		case model.StatusCanceled:
			r.Children.Canceled++
		case model.StatusInProgress:
			r.Children.InProgress++
		default:
			r.Children.ToDo++
		}
		if c.Done() {
			continue
		}
		if tracker.Overdue(c, today) {
			r.Overdue = append(r.Overdue, c.Key)
		}
		if p.DueDate != "" && c.DueDate > p.DueDate {
			r.DueAfterParent = append(r.DueAfterParent, DueAfter{ID: c.Key, DueDate: c.DueDate})
		}
		if tracker.Blocked(c) {
			r.Blocked = append(r.Blocked, c.Key)
		}
		if c.Assignee == nil {
			r.Unassigned = append(r.Unassigned, c.Key)
		}
		if tracker.Stale(c, staleDays, now) {
			r.Stale = append(r.Stale, StaleChild{ID: c.Key, LastUpdate: c.UpdatedAt.UTC().Format(time.DateOnly)})
		}
	}
	slices.SortFunc(r.DueAfterParent, func(a, b DueAfter) int { return strings.Compare(a.ID, b.ID) })
	return r
}
