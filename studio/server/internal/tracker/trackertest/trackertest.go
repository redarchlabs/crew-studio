// Package trackertest runs fake Jira Cloud and Linear servers for tests. They answer the documented request and
// response shapes (docs/trackers.md) from an in-memory set of issues, so no test reaches a real tracker.
package trackertest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Issue is one fake issue.
type Issue struct {
	ID     string
	Key    string
	Title  string
	Status string
	// Category is new, indeterminate or done (Jira's keys); Linear state types are derived from it
	Category string
	Assignee *User
	Due      string
	Priority string
	Updated  time.Time
	Parent   string
	Labels   []string
	Project  string
	Lead     *User
	Comments []Comment
	// Creator is the record id of the person who made it, and Created when (KM2 only; zero: a fixed past time)
	Creator string
	Created time.Time
}

// User is one fake account.
type User struct {
	ID    string
	Name  string
	Email string
}

// Comment is one fake comment.
type Comment struct {
	ID     string
	Author User
	At     time.Time
	Body   string
}

// Posted is a comment a test client posted.
type Posted struct {
	Issue string
	Body  any
}

// Data is the shared state of a fake server. Tests change it between calls through its methods.
type Data struct {
	mu       sync.Mutex
	Issues   map[string]*Issue
	Users    []User
	Me       User
	Projects map[string]*User // project key -> lead
	Posted   []Posted
	Created  []map[string]any
	Requests []string
	// Fail, when set, answers every request with this status (and Retry-After 0 for 429)
	Fail int
	// FailTimes limits Fail to that many requests (0: always)
	FailTimes int
	// PageSize is the search page size (default 2, so tests page)
	PageSize int
	nextID   int
}

// NewData builds an empty data set with one account for the connection itself.
func NewData() *Data {
	return &Data{Issues: map[string]*Issue{}, Projects: map[string]*User{}, Me: User{ID: "me-1", Name: "Studio Bot", Email: "bot@example.com"}, PageSize: 2}
}

// Add stores an issue (its key decides the project when Project is empty).
func (d *Data) Add(i Issue) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if i.Project == "" {
		i.Project = strings.SplitN(i.Key, "-", 2)[0]
	}
	if i.ID == "" {
		d.nextID++
		i.ID = fmt.Sprintf("1%04d", d.nextID)
	}
	if i.Updated.IsZero() {
		i.Updated = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	}
	if i.Category == "" {
		i.Category = "new"
	}
	if i.Status == "" {
		i.Status = map[string]string{"new": "To Do", "indeterminate": "In Progress", "done": "Done"}[i.Category]
	}
	cp := i
	d.Issues[i.Key] = &cp
}

// Update changes an issue in place.
func (d *Data) Update(key string, change func(*Issue)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	change(d.Issues[key])
}

// Comment adds a comment to an issue and moves its updated time.
func (d *Data) Comment(key string, c Comment) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextID++
	if c.ID == "" {
		c.ID = strconv.Itoa(d.nextID)
	}
	i := d.Issues[key]
	i.Comments = append(i.Comments, c)
	if c.At.After(i.Updated) {
		i.Updated = c.At
	}
}

// PostedComments returns what clients posted.
func (d *Data) PostedComments() []Posted {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.Posted)
}

// RequestLog returns "METHOD path" of every request so far.
func (d *Data) RequestLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.Requests)
}

func (d *Data) failing(w http.ResponseWriter) bool {
	if d.Fail == 0 {
		return false
	}
	if d.FailTimes > 0 {
		d.FailTimes--
		if d.FailTimes == 0 {
			defer func() { d.Fail = 0 }()
		}
	}
	if d.Fail == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "0")
	}
	w.WriteHeader(d.Fail)
	_, _ = w.Write([]byte(`{"errorMessages":["fake failure"]}`))
	return true
}

func (d *Data) sortedIssues(match func(*Issue) bool) []*Issue {
	var out []*Issue
	for _, i := range d.Issues {
		if match(i) {
			out = append(out, i)
		}
	}
	slices.SortFunc(out, func(a, b *Issue) int { return strings.Compare(a.Key, b.Key) })
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Server starts a fake server for handler and closes it when the test ends.
func Server(t testing.TB, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}
