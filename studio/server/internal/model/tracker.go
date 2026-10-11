package model

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Trackers (docs/crew-state-and-pmo-design.md, sections 10 and 12): Jira Cloud and Linear connections, the crew's
// tracking list (tracker items it follows with their child tickets) and the people map of each connection.

const (
	TrackerJira   = "jira"
	TrackerLinear = "linear"
	// ConnectionTracker is the WatchSourceKind.Connection of sources that read any tracker connection
	ConnectionTracker = "tracker"

	IncludeChildrenAll    = "all"
	IncludeChildrenDirect = "direct"
	IncludeChildrenNone   = "none"

	// Status categories every tracker's statuses are folded into
	StatusToDo       = "to_do"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusCanceled   = "canceled"

	// People map links: matched by email, chosen by a person, or never linked
	PersonLinkEmail  = "email"
	PersonLinkManual = "manual"

	maxProjectLen   = 120
	maxTrackingNote = 1_000
	maxAccountIDLen = 128
)

var (
	// Jira issue keys and Linear identifiers: a project or team key, a dash and a number
	itemRefPattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,49}-[0-9]{1,10}$`)
	projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,49}$`)
	accountIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,127}$`)
)

// IsTrackerKind reports whether a connection kind is a tracker (Jira, Linear or KM2 issues).
func IsTrackerKind(kind string) bool {
	return kind == TrackerJira || kind == TrackerLinear || kind == TrackerKM2
}

// ConnectionFits reports whether a connection of kind connKind can serve a source that needs want ("tracker" takes
// any tracker kind).
func ConnectionFits(want, connKind string) bool {
	if want == ConnectionTracker {
		return IsTrackerKind(connKind)
	}
	return want == connKind
}

// NormalizeItemRef cleans a tracker item key such as "prj-10" or "jira:PRJ-10" into "PRJ-10".
func NormalizeItemRef(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	for _, prefix := range []string{TrackerJira + ":", TrackerLinear + ":", TrackerKM2 + ":"} {
		if len(ref) > len(prefix) && strings.EqualFold(ref[:len(prefix)], prefix) {
			ref = ref[len(prefix):]
		}
	}
	ref = strings.ToUpper(ref)
	return ref, itemRefPattern.MatchString(ref)
}

// NormalizeProjectKey cleans a Jira project key, Linear team key or KM2 project key such as "prj" into "PRJ".
func NormalizeProjectKey(key string) (string, bool) {
	key = strings.ToUpper(strings.TrimSpace(key))
	return key, projectKeyPattern.MatchString(key)
}

// ValidAccountID reports whether text looks like a tracker account id (an Atlassian accountId or a Linear user id).
func ValidAccountID(id string) bool { return accountIDPattern.MatchString(id) }

// TrackerSubject is the action log subject (and watch item id) of a tracker item, such as jira:PRJ-15.
func TrackerSubject(kind, ref string) string { return kind + ":" + ref }

// TrackingItem is one entry of a crew's tracking list.
type TrackingItem struct {
	ID             string  `json:"id"`
	CrewID         string  `json:"crew_id"`
	ScheduleID     *string `json:"schedule_id"`
	ScheduleName   string  `json:"schedule_name"`
	ConnectionID   string  `json:"connection_id"`
	ConnectionName string  `json:"connection_name"`
	ConnectionKind string  `json:"connection_kind"`
	Project        string  `json:"project"`
	ItemRef        string  `json:"item_ref"`
	// IncludeChildren is all (every level), direct (children only) or none
	IncludeChildren string `json:"include_children"`
	Note            string `json:"note"`
	LeadAccountID   string `json:"lead_account_id"`
	// LeadName is the lead's name from the connection's people map; empty when the account is not in it
	LeadName      string     `json:"lead_name"`
	AddedBy       *string    `json:"added_by"`
	AddedByName   string     `json:"added_by_name"`
	AddedAt       time.Time  `json:"added_at"`
	RemovedAt     *time.Time `json:"removed_at"`
	RemovedByName string     `json:"removed_by_name"`
}

// TrackingItemInput adds an item to a crew's tracking list.
type TrackingItemInput struct {
	ScheduleID      *string `json:"schedule_id"`
	ConnectionID    string  `json:"connection_id"`
	Project         string  `json:"project"`
	ItemRef         string  `json:"item_ref"`
	IncludeChildren string  `json:"include_children"`
	Note            string  `json:"note"`
	LeadAccountID   string  `json:"lead_account_id"`
}

// NormalizeTrackingItem validates a tracking-list entry and returns a cleaned copy. Whether the item exists and the
// crew may use the connection is checked by callers, against the tracker and the database.
func NormalizeTrackingItem(in TrackingItemInput) (TrackingItemInput, error) {
	var c checker
	out := TrackingItemInput{ScheduleID: trimmedPtr(in.ScheduleID), ConnectionID: strings.TrimSpace(in.ConnectionID),
		Project: strings.TrimSpace(in.Project), Note: strings.TrimSpace(in.Note), LeadAccountID: strings.TrimSpace(in.LeadAccountID),
		IncludeChildren: strings.TrimSpace(in.IncludeChildren)}
	c.optionalUUID("schedule_id", out.ScheduleID)
	if !uuidPattern.MatchString(out.ConnectionID) {
		c.addf("connection_id", "pick a tracker connection (Jira, Linear or KM2 issues)")
	}
	ref, ok := NormalizeItemRef(in.ItemRef)
	if !ok {
		c.addf("item_ref", "use the tracker's key for the item, such as PRJ-10")
	}
	out.ItemRef = ref
	// An empty project is filled in by callers with the item's project key from the tracker
	c.text("project", out.Project, 0, maxProjectLen)
	if strings.IndexFunc(out.Project, unicode.IsControl) >= 0 {
		c.addf("project", "must be one line")
	}
	c.text("note", out.Note, 0, maxTrackingNote)
	if out.IncludeChildren == "" {
		out.IncludeChildren = IncludeChildrenAll
	}
	if !slices.Contains([]string{IncludeChildrenAll, IncludeChildrenDirect, IncludeChildrenNone}, out.IncludeChildren) {
		c.addf("include_children", "must be %q, %q or %q", IncludeChildrenAll, IncludeChildrenDirect, IncludeChildrenNone)
	}
	if out.LeadAccountID != "" && !ValidAccountID(out.LeadAccountID) {
		c.addf("lead_account_id", "must be a tracker account id")
	}
	return out, c.err()
}

// LinkableUser is a Studio user a tracker account can be linked to.
type LinkableUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// TrackerPerson is one tracker account in a connection's people map.
type TrackerPerson struct {
	ConnectionID string `json:"connection_id"`
	AccountID    string `json:"account_id"`
	DisplayName  string `json:"display_name"`
	// Email is "" when the tracker does not show it
	Email    string  `json:"email"`
	UserID   *string `json:"user_id"`
	UserName string  `json:"user_name"`
	// Link is email (matched automatically), manual (chosen by a person) or "" (never linked)
	Link      string    `json:"link"`
	SeenAt    time.Time `json:"seen_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Tracker posts by agents (design section 12). Text is plain; mentions are tracker account ids.
const (
	MaxTrackerCommentChars = 10_000
	maxTrackerMentions     = 10
	maxSuggestValueChars   = 500
	maxSuggestReasonChars  = 2_000
	maxIssueTitleChars     = 255
	maxConnectionRefChars  = 200
)

// SuggestFields are the fields tracker_suggest_change may suggest a change to.
var SuggestFields = []string{"due_date", "assignee", "status", "priority", "title", "labels", "parent"}

var followupStepPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,49}$`)

// TrackerComment is a comment an agent asks to post.
type TrackerComment struct {
	Connection string   `json:"connection"`
	Text       string   `json:"text"`
	Mentions   []string `json:"mentions"`
	// FollowupStep, such as due_soon, logs the comment as that follow-up step (followup:due_soon)
	FollowupStep string `json:"followup_step"`
}

// TrackerSuggestion is a suggested field change, posted as a comment; the field itself is never changed.
type TrackerSuggestion struct {
	Connection string   `json:"connection"`
	Field      string   `json:"field"`
	Value      string   `json:"value"`
	Reason     string   `json:"reason"`
	Mentions   []string `json:"mentions"`
}

// TrackerNewIssue is an issue an agent asks to create (only where an admin allowed it).
type TrackerNewIssue struct {
	Connection  string `json:"connection"`
	Project     string `json:"project"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Parent      string `json:"parent"`
}

// cleanText drops control characters other than line breaks and tabs, and hidden characters (IsHidden), and trims.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || IsHidden(r) {
			return -1
		}
		return r
	}, s))
}

// IsHidden reports whether r is an invisible formatting character a person reviewing text would not see: Unicode's
// format characters (Cf: bidirectional overrides and isolates such as U+202E, zero-width characters such as U+200B,
// the byte order mark) and the tag characters U+E0000 to U+E007F. Text written by a model that a person confirms, or
// that is posted for others to read, drops them, so what is shown is what is sent.
func IsHidden(r rune) bool {
	return unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F)
}

// StripHidden drops the hidden characters (IsHidden) from s.
func StripHidden(s string) string {
	return strings.Map(func(r rune) rune {
		if IsHidden(r) {
			return -1
		}
		return r
	}, s)
}

func cleanMentions(c *checker, in []string) []string {
	out := []string{}
	for _, m := range in {
		m = strings.TrimSpace(m)
		switch {
		case m == "" || slices.Contains(out, m):
		case !ValidAccountID(m):
			c.addf("mentions", "%q is not a tracker account id", oneLineOf(m, 40))
		default:
			out = append(out, m)
		}
	}
	if len(out) > maxTrackerMentions {
		c.addf("mentions", "at most %d people", maxTrackerMentions)
	}
	return out
}

func oneLineOf(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// NormalizeTrackerComment validates a comment and returns a cleaned copy.
func NormalizeTrackerComment(in TrackerComment) (TrackerComment, error) {
	var c checker
	out := TrackerComment{Connection: strings.TrimSpace(in.Connection), Text: cleanText(in.Text), FollowupStep: strings.TrimSpace(in.FollowupStep)}
	c.text("connection", out.Connection, 0, maxConnectionRefChars)
	c.text("text", out.Text, 1, MaxTrackerCommentChars)
	out.Mentions = cleanMentions(&c, in.Mentions)
	if out.FollowupStep != "" && !followupStepPattern.MatchString(out.FollowupStep) {
		c.addf("followup_step", "use the step's name from followups_due, such as due_soon")
	}
	return out, c.err()
}

// NormalizeTrackerSuggestion validates a suggested change and returns a cleaned copy.
func NormalizeTrackerSuggestion(in TrackerSuggestion) (TrackerSuggestion, error) {
	var c checker
	out := TrackerSuggestion{Connection: strings.TrimSpace(in.Connection), Field: strings.TrimSpace(in.Field),
		Value: oneLineOf(cleanText(in.Value), maxSuggestValueChars+1), Reason: cleanText(in.Reason)}
	c.text("connection", out.Connection, 0, maxConnectionRefChars)
	if !slices.Contains(SuggestFields, out.Field) {
		c.addf("field", "must be one of %s", strings.Join(SuggestFields, ", "))
	}
	c.text("value", out.Value, 1, maxSuggestValueChars)
	c.text("reason", out.Reason, 1, maxSuggestReasonChars)
	out.Mentions = cleanMentions(&c, in.Mentions)
	return out, c.err()
}

// NormalizeTrackerNewIssue validates a new issue and returns a cleaned copy.
func NormalizeTrackerNewIssue(in TrackerNewIssue) (TrackerNewIssue, error) {
	var c checker
	out := TrackerNewIssue{Connection: strings.TrimSpace(in.Connection), Title: oneLineOf(cleanText(in.Title), maxIssueTitleChars+1),
		Description: cleanText(in.Description)}
	c.text("connection", out.Connection, 0, maxConnectionRefChars)
	project, ok := NormalizeProjectKey(in.Project)
	if !ok {
		c.addf("project", "give the Jira project key, Linear team key or KM2 project key, such as PRJ")
	}
	out.Project = project
	c.text("title", out.Title, 1, maxIssueTitleChars)
	c.text("description", out.Description, 0, MaxTrackerCommentChars)
	if strings.TrimSpace(in.Parent) != "" {
		parent, ok := NormalizeItemRef(in.Parent)
		if !ok {
			c.addf("parent", "use the parent issue's key, such as PRJ-10")
		}
		out.Parent = parent
	}
	return out, c.err()
}
