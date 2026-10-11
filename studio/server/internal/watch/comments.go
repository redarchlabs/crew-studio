package watch

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
)

const (
	// ExcerptChars caps each new comment's excerpt; the crew reads the whole thread with tracker_get_issue
	ExcerptChars = 500
	// MaxCommentsPerTick caps how many new comments one check passes on
	MaxCommentsPerTick = 50
	// maxCommentReads bounds how many issues one check reads comments for, one call each (Jira); the rest are read on
	// later checks, because their seen time does not move
	maxCommentReads  = 200
	commentsPerIssue = 100
)

// commentReader finds new comments on tracker issues. Each issue remembers the newest comment time already seen
// (in the snapshot). On a first check, or for an issue new to the watch, that time starts at the check's time, so
// old threads are not reported as new. Comments by the connection's own account are left out, so a crew does not
// react to itself.
type commentReader struct {
	client   tracker.Client
	me       tracker.Person
	kind     string
	previous map[string]ItemMeta
	now      time.Time
	reads    int
}

// since is the earliest seen time among the issues, for trackers that read comments with a search (Linear, KM2).
func commentsSince(previous map[string]ItemMeta) *time.Time {
	var out *time.Time
	for _, m := range previous {
		if m.CommentsSeenAt != nil && (out == nil || m.CommentsSeenAt.Before(*out)) {
			t := *m.CommentsSeenAt
			out = &t
		}
	}
	return out
}

// read returns an issue's new comments (oldest first) and the seen time to store for it.
func (r *commentReader) read(ctx context.Context, issue tracker.Issue) ([]Comment, *time.Time, error) {
	subject := model.TrackerSubject(r.kind, issue.Key)
	prev := r.previous[subject].CommentsSeenAt
	if prev == nil {
		at := r.now
		return nil, &at, nil
	}
	var found []tracker.Comment
	switch {
	case issue.CommentsRead:
		found = issue.Comments
	case issue.UpdatedAt.After(*prev) || !model.TrackerCommentMovesUpdated(r.kind):
		if r.reads >= maxCommentReads {
			return nil, prev, nil
		}
		r.reads++
		var err error
		if found, err = r.client.Comments(ctx, issue.Key, commentsPerIssue); err != nil {
			return nil, nil, err
		}
	}
	seen := *prev
	var out []Comment
	for _, c := range found {
		// A comment without a readable time (the client logged it) is neither reported nor counted as seen
		if c.At.IsZero() || !c.At.After(*prev) {
			continue
		}
		if c.At.After(seen) {
			seen = c.At
		}
		if r.me.AccountID != "" && c.Author.AccountID == r.me.AccountID {
			continue
		}
		out = append(out, excerpt(subject, c))
	}
	slices.SortFunc(out, func(a, b Comment) int { return a.At.Compare(b.At) })
	return out, &seen, nil
}

func excerpt(subject string, c tracker.Comment) Comment {
	text := strings.Join(strings.Fields(c.Body), " ")
	out := Comment{ID: subject, Author: c.Author.Name, At: c.At, Excerpt: text}
	if utf8.RuneCountInString(text) > ExcerptChars {
		out.Excerpt = string([]rune(text)[:ExcerptChars]) + "…"
		out.Truncated = true
	}
	if out.Author == "" {
		out.Author = "(an app or integration)"
	}
	return out
}

// capComments keeps the newest MaxCommentsPerTick comments (in time order) and counts the rest.
func capComments(all []Comment) ([]Comment, int) {
	slices.SortStableFunc(all, func(a, b Comment) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	if len(all) <= MaxCommentsPerTick {
		return all, 0
	}
	return all[len(all)-MaxCommentsPerTick:], len(all) - MaxCommentsPerTick
}
