package tracker

import (
	"context"
	"slices"
	"strings"
	"time"
)

// MaxChildDepth bounds "all levels": epic, story, sub-task and a little more.
const MaxChildDepth = 5

// maxChildSearches bounds the tracker calls one Children walk makes (one per parent), so a wide tree cannot turn
// one request into thousands of calls on a shared credential (run reads are also throttled per connection).
const maxChildSearches = 500

// Children returns the issues below parent, level by level: depth 1 is its direct children, 0 every level (up to
// MaxChildDepth). More than max issues fails with a *LimitError rather than cutting the list.
func Children(ctx context.Context, c Client, parent Issue, depth int, q Query, max int) ([]Issue, error) {
	if depth <= 0 || depth > MaxChildDepth {
		depth = MaxChildDepth
	}
	seen := map[string]bool{parent.Key: true}
	out := []Issue{}
	level := []Issue{parent}
	searches := 0
	for d := 0; d < depth && len(level) > 0; d++ {
		var next []Issue
		for _, p := range level {
			if searches++; searches > maxChildSearches {
				return nil, &LimitError{Count: len(out), Max: max}
			}
			found, err := c.Search(ctx, Query{Parent: &p, IncludeDone: true, Max: max, CommentsSince: q.CommentsSince})
			if err != nil {
				return nil, err
			}
			for _, child := range found {
				if seen[child.Key] {
					continue
				}
				seen[child.Key] = true
				out = append(out, child)
				next = append(next, child)
			}
			if len(out) > max {
				return nil, &LimitError{Count: len(out), Max: max}
			}
		}
		level = next
	}
	return out, nil
}

// newestFirst sorts comments newest first and keeps at most limit.
func newestFirst(in []Comment, limit int) []Comment {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b Comment) int { return b.At.Compare(a.At) })
	if limit >= 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Filter narrows issues the way tracker_find_issues asks: overdue, due within days, not updated in days,
// unassigned, blocked or owned by someone.
type Filter struct {
	Overdue    bool
	DueWithin  int
	StaleDays  int
	Unassigned bool
	Blocked    bool
	// Owner matches the assignee's account id, name or email (ignoring case)
	Owner string
}

// Blocked reports whether an issue looks blocked: its status or one of its labels says so.
func Blocked(i Issue) bool {
	if strings.Contains(strings.ToLower(i.Status), "block") {
		return true
	}
	return slices.ContainsFunc(i.Labels, func(l string) bool { return strings.Contains(strings.ToLower(l), "block") })
}

// Overdue reports whether an open issue's due date is before today.
func Overdue(i Issue, today string) bool {
	return !i.Done() && i.DueDate != "" && i.DueDate < today
}

// Stale reports whether an open issue has not been updated for days.
func Stale(i Issue, days int, now time.Time) bool {
	return !i.Done() && !i.UpdatedAt.IsZero() && now.Sub(i.UpdatedAt) > time.Duration(days)*24*time.Hour
}

// Apply keeps the issues that match every filter that is set.
func (f Filter) Apply(issues []Issue, now time.Time, loc *time.Location) []Issue {
	today := now.In(loc).Format(time.DateOnly)
	horizon := now.In(loc).AddDate(0, 0, f.DueWithin).Format(time.DateOnly)
	owner := strings.ToLower(strings.TrimSpace(f.Owner))
	out := []Issue{}
	for _, i := range issues {
		switch {
		case f.Overdue && !Overdue(i, today):
		case f.DueWithin > 0 && (i.Done() || i.DueDate == "" || i.DueDate < today || i.DueDate > horizon):
		case f.StaleDays > 0 && !Stale(i, f.StaleDays, now):
		case f.Unassigned && (i.Assignee != nil || i.Done()):
		case f.Blocked && !Blocked(i):
		case owner != "" && !ownedBy(i, owner):
		default:
			out = append(out, i)
		}
	}
	return out
}

func ownedBy(i Issue, owner string) bool {
	a := i.Assignee
	return a != nil && (strings.ToLower(a.AccountID) == owner || strings.ToLower(a.Name) == owner ||
		(a.Email != "" && strings.ToLower(a.Email) == owner))
}
