// Package tracker reads issues from Jira Cloud, Linear and KM2 custom entities and posts comments on them, behind
// one interface (docs/crew-state-and-pmo-design.md, section 12; the API facts used are in docs/trackers.md).
//
// The clients are small and written by hand. They pin every request to the connection's own host, never follow
// redirects, back off on rate limits, cap response sizes, and keep secrets out of every error. They never change
// an issue's fields: writing is limited to comments and, when an admin allows it per connection, new issues.
package tracker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"crewstudio/internal/model"
)

// LinearProfileLink matches a Linear profile URL, which Linear turns into a mention: text agents write may not notify
// people that way (posts name people in mentions, under Studio's caps).
var LinearProfileLink = regexp.MustCompile(`(?i)linear\.app/[^\s/]+/profiles/`)

// Person is a tracker account.
type Person struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	// Email is "" when the tracker hides it
	Email string `json:"email,omitempty"`
	// URL is the profile page (Linear mentions are profile URLs)
	URL string `json:"-"`
}

// Issue is a tracker item with the fields every tracker has, already in Studio's terms.
type Issue struct {
	// ID is the tracker's internal id (Jira's numeric id, Linear's UUID)
	ID    string `json:"-"`
	Key   string `json:"key"`
	Title string `json:"title"`
	URL   string `json:"url"`
	// Status is the tracker's status name; StatusCategory is to_do, in_progress, done or canceled
	Status         string    `json:"status"`
	StatusCategory string    `json:"status_category"`
	Assignee       *Person   `json:"assignee"`
	DueDate        string    `json:"due_date"`
	Priority       string    `json:"priority"`
	UpdatedAt      time.Time `json:"updated_at"`
	Parent         string    `json:"parent"`
	Labels         []string  `json:"labels"`
	// Project is the Jira project key, Linear team key or KM2 project key
	Project string `json:"project"`
	// Lead is the lead of the issue's project when the tracker gives it with the issue (Linear projects)
	Lead *Person `json:"-"`
	// Description is only read for a single issue
	Description string `json:"description,omitempty"`
	// Comments are new comments read along with the issue (Query.CommentsSince); CommentsRead says they were
	Comments     []Comment `json:"-"`
	CommentsRead bool      `json:"-"`
}

// Done reports whether the issue is finished (done or canceled).
func (i Issue) Done() bool {
	return i.StatusCategory == model.StatusDone || i.StatusCategory == model.StatusCanceled
}

// Comment is one comment on an issue, as plain text.
type Comment struct {
	ID     string    `json:"id"`
	Author Person    `json:"author"`
	At     time.Time `json:"at"`
	Body   string    `json:"body"`
}

// Project is a Jira project or a Linear team.
type Project struct {
	ID   string  `json:"-"`
	Key  string  `json:"key"`
	Name string  `json:"name"`
	Lead *Person `json:"lead"`
}

// Query selects issues: a project's (or a parent's children), open ones unless IncludeDone.
type Query struct {
	Project string
	// JQL is an extra clause ANDed in (Jira only)
	JQL string
	// Parent, when set, selects that issue's direct children instead of a project's issues
	Parent      *Issue
	IncludeDone bool
	// Max is the most issues the caller accepts; past it Search fails with a *LimitError
	Max int
	// CommentsSince asks for comments created after it with each issue, when the tracker can do so in one query
	CommentsSince *time.Time
}

// NewIssue is an issue to create.
type NewIssue struct {
	Project     string
	Title       string
	Description string
	// Parent is an optional parent issue key
	Parent string
}

// Posted is a comment just posted.
type Posted struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// Client is one tracker connection.
type Client interface {
	Kind() string
	// Myself is the connection's own account (to test it, and to leave its own comments out)
	Myself(ctx context.Context) (Person, error)
	Projects(ctx context.Context, max int) ([]Project, error)
	// Project returns one project with its lead (Linear teams have none)
	Project(ctx context.Context, key string) (Project, error)
	Issue(ctx context.Context, ref string) (Issue, error)
	Search(ctx context.Context, q Query) ([]Issue, error)
	// Comments returns an issue's newest comments, newest first
	Comments(ctx context.Context, ref string, limit int) ([]Comment, error)
	PostComment(ctx context.Context, ref, text string, mentions []Person) (Posted, error)
	CreateIssue(ctx context.Context, in NewIssue) (Issue, error)
	// Users lists the tracker's people (for the people map)
	Users(ctx context.Context, max int) ([]Person, error)
	User(ctx context.Context, accountID string) (Person, error)
}

// Checker is a client that can check more than its account when its connection is tested (a KM2 organisation's
// tracker entities). Check lists what is missing; an error means the check itself failed.
type Checker interface {
	Check(ctx context.Context) (CheckResult, error)
}

// CheckResult is what a Checker found: problems that keep the connection from working, and warnings.
type CheckResult struct {
	Problems []string
	Warnings []string
}

// Options are the server's tracker settings.
type Options struct {
	// JiraSites are extra Jira site origins (such as https://jira.example.com) an admin allowed besides
	// https://*.atlassian.net
	JiraSites []string
	// LinearURL is the Linear GraphQL endpoint (default https://api.linear.app/graphql)
	LinearURL string
	// KM2TrackerSites, when set, are the only KM2 origins km2_tracker connections may use (STUDIO_KM2_TRACKER_SITES)
	KM2TrackerSites []string
	// Log receives warnings about tracker data Studio cannot use; nil is slog.Default()
	Log *slog.Logger
	// HTTP is the client to use; nil builds one on the connection transport (no proxy, no link-local addresses)
	HTTP *http.Client
	// Sleep waits between retries; nil waits for real
	Sleep func(ctx context.Context, d time.Duration) error
	// km2 keeps the people of KM2 connections between clients (set by NewService)
	km2 *km2Caches
}

// DefaultLinearURL is Linear's GraphQL endpoint.
const DefaultLinearURL = "https://api.linear.app/graphql"

// New builds the client for a tracker connection (Jira, Linear or KM2 issues) from its settings and secrets.
func New(conn model.Connection, secrets map[string]string, opts Options) (Client, error) {
	h := newHTTP(opts)
	switch conn.Kind {
	case model.TrackerJira:
		return newJira(conn, secrets, opts, h)
	case model.TrackerLinear:
		return newLinear(conn, secrets, opts, h)
	case model.TrackerKM2:
		return newKM2(conn, secrets, opts, h)
	}
	return nil, &Error{Message: fmt.Sprintf("%s is a %s connection, not a tracker (Jira, Linear or KM2 issues) one", conn.Name, conn.Kind)}
}

// Error is a tracker problem that is safe to show to people and agents: it never holds secrets or whole responses.
type Error struct {
	Message string
	// Status is the HTTP status the tracker answered, 0 when none
	Status      int
	NotFound    bool
	Auth        bool
	RateLimited bool
	// Unreachable is set when the tracker could not be reached or read (as opposed to a setting that is wrong)
	Unreachable bool
	// Rejected is set when the tracker answered and refused the request (a 4xx, a rate limit, Linear's GraphQL errors):
	// a write was then not made. Any other failure of a write (a 2xx Studio could not read, a 5xx, no answer) leaves it
	// unknown whether it was made.
	Rejected bool
}

// rejectedIf marks e as a refusal when the tracker answered with a 4xx.
func rejectedIf(e *Error, status int) *Error {
	e.Rejected = status >= 400 && status < 500
	return e
}

// unreachable is an error for a tracker that could not be reached or read.
func unreachable(format string, args ...any) *Error {
	e := errorf(format, args...)
	e.Unreachable = true
	return e
}

func (e *Error) Error() string { return e.Message }

func errorf(format string, args ...any) *Error { return &Error{Message: fmt.Sprintf(format, args...)} }

// LimitError means a search found more issues than the caller allows.
type LimitError struct {
	Count int
	Max   int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("the tracker returned more than %d issues; narrow the query or raise the limit", e.Max)
}

// IsNotFound reports whether err says the issue (or project) does not exist or is not visible.
func IsNotFound(err error) bool {
	var te *Error
	return errors.As(err, &te) && te.NotFound
}

// oneLine trims text to one line of at most n characters, for error messages built from tracker answers.
func oneLine(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > n {
		return string(r[:n]) + "…"
	}
	return text
}
