package model

import (
	"time"
)

// Watches (docs/crew-state-and-pmo-design.md, section 5): a schedule can fetch a source on every tick, compare
// it with the last snapshot and only start a work order when something changed or a follow-up step is due.

const (
	WatchRunChanged            = "changed"
	WatchRunAlways             = "always"
	WatchRunChangedOrFollowups = "changed_or_followups"

	WatchSourceHTTPJSON      = "http_json"
	WatchSourceWebPage       = "web_page"
	WatchSourceWorkspaceFile = "workspace_file"
	// Tracker sources (phase 4): a Jira project or Linear team, and the crew's tracking list
	WatchSourceTracker      = "tracker"
	WatchSourceTrackingList = "tracking_list"

	DefaultWatchMaxItems    = 2000
	MaxWatchMaxItems        = 10000
	DefaultFollowupsPerTick = 25
	MaxFollowupsPerTick     = 200

	// Inputs a watched schedule adds to its work orders. Crews need not declare them.
	WatchInputChanges   = "changes"
	WatchInputFollowups = "followups_due"
	WatchInputDryRun    = "dry_run"
	// WatchInputRollups is the tracking list's roll-ups (tracking_list sources only)
	WatchInputRollups = "rollups"

	// Safety caps on what a crew's tracker tools may post (section 16, item 8)
	DefaultMentionCapPerDay = 3
	MaxMentionCapPerDay     = 50
	DefaultCommentCapPerRun = 30
	MaxCommentCapPerRun     = 500
	// DefaultStaleDays is how long a child may go without an update before a roll-up calls it stale
	DefaultStaleDays = 14
	MaxStaleDays     = 365
	// TrackerIDField identifies tracker items: their subject, such as jira:PRJ-15
	TrackerIDField = "key"
)

// WatchInputKeys are the inputs Studio adds to a watched schedule's work orders.
var WatchInputKeys = []string{WatchInputChanges, WatchInputFollowups, WatchInputDryRun, WatchInputRollups}

// TrackerFields are the fields a tracker source can track on each item, already the same for Jira and Linear.
var TrackerFields = []string{"title", "status", "status_category", "assignee", "due_date", "priority", "updated_at", "parent", "labels"}

// DefaultTrackerFields are tracked when a tracker source lists none.
var DefaultTrackerFields = []string{"status", "assignee", "due_date", "priority"}

// WatchSource is where a watch reads its items: a kind plus that kind's query.
type WatchSource struct {
	Kind string `json:"kind"`
	// http_json: path (and query) under the connection's base URL, such as /issues?state=open
	Path string `json:"path,omitempty"`
	// http_json and JSON files: dotted path to the array of items; "" means the document is the array
	ItemsPath string `json:"items_path,omitempty"`
	// web_page: the page and, optionally, a CSS selector for the part to compare
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	// workspace_file: a path inside the Studio workspace, and "csv" or "json" (default: from the extension)
	File   string `json:"file,omitempty"`
	Format string `json:"format,omitempty"`
	// tracker: the Jira project key, Linear team key or KM2 project key, an optional extra JQL clause (Jira only), and whether done
	// and canceled items are included
	Project     string `json:"project,omitempty"`
	JQL         string `json:"jql,omitempty"`
	IncludeDone bool   `json:"include_done,omitempty"`
	// tracker: who escalations go to instead of the project lead (a tracker account id)
	Lead string `json:"lead,omitempty"`
	// tracker and tracking_list: also report new comments, and when a child counts as stale in roll-ups
	Comments  bool `json:"comments,omitempty"`
	StaleDays int  `json:"stale_days,omitempty"`
}

// snapshotShape is the part of a source that decides what its snapshot holds.
func (s WatchSource) snapshotShape() WatchSource {
	out := s
	out.Lead, out.StaleDays = "", 0
	return out
}

// WatchSourceKind describes a source kind for forms and validation.
type WatchSourceKind struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// Connection is the connection kind the source reads through, or "" when it needs none
	Connection string `json:"connection"`
	// Items is true when the source returns many items (id_field and tracked_fields apply)
	Items bool `json:"items"`
	// Followups is true when a follow-up ladder can be set (items with a date field)
	Followups bool `json:"followups"`
	// Fields, when set, are the only fields that can be tracked (tracker sources)
	Fields []string `json:"fields,omitempty"`
}

// WatchSourceKinds lists the kinds available today.
var WatchSourceKinds = []WatchSourceKind{
	{Kind: WatchSourceHTTPJSON, Label: "JSON over HTTP", Connection: "http", Items: true, Followups: true},
	{Kind: WatchSourceWebPage, Label: "Web page", Items: false, Followups: false},
	{Kind: WatchSourceWorkspaceFile, Label: "Workspace file (CSV or JSON)", Items: true, Followups: true},
	{Kind: WatchSourceTracker, Label: "Tracker project (Jira, Linear or KM2)", Connection: ConnectionTracker, Items: true,
		Followups: true, Fields: TrackerFields},
	{Kind: WatchSourceTrackingList, Label: "Tracking list (tracked items and their children)", Items: true, Followups: true,
		Fields: TrackerFields},
}

// WatchSourceKindOf returns the description of a source kind.
func WatchSourceKindOf(kind string) (WatchSourceKind, bool) {
	for _, k := range WatchSourceKinds {
		if k.Kind == kind {
			return k, true
		}
	}
	return WatchSourceKind{}, false
}

// FollowupStep is one rung of a follow-up ladder, such as "due_soon, 3 days before the due date, notify owner".
type FollowupStep struct {
	Name string `json:"name"`
	// Offset from the item's date in days: -3d, 0d, +2d
	Offset string `json:"offset"`
	// Who to notify: a role such as owner or lead, or a tracked field holding the person
	Notify string `json:"notify"`
	// Repeat, such as 7d or 1w, makes the step due again once that long has passed since it was last sent
	Repeat string `json:"repeat,omitempty"`
}

// FollowupLadder is a list of steps tied to a date field on each item (section 6).
type FollowupLadder struct {
	DateField string `json:"date_field"`
	// SkipWhen leaves items out when a tracked field has one of the values (compared ignoring case)
	SkipWhen map[string][]string `json:"skip_when,omitempty"`
	Steps    []FollowupStep      `json:"steps"`
	// QuietDays, such as Sat and Sun: steps falling on them move to the next working day
	QuietDays []string `json:"quiet_days,omitempty"`
	// Timezone for dates; "" means the schedule's time zone
	Timezone string `json:"timezone,omitempty"`
	// MaxPerTick caps how many steps go to one run (default 25); the rest wait for the next tick
	MaxPerTick int `json:"max_per_tick,omitempty"`
}

// Watch is a schedule's watch as the API shows it.
type Watch struct {
	ID             string          `json:"id"`
	ScheduleID     string          `json:"schedule_id"`
	ScheduleName   string          `json:"schedule_name"`
	CrewID         *string         `json:"crew_id"`
	ConnectionID   *string         `json:"connection_id"`
	ConnectionName string          `json:"connection_name"`
	Source         WatchSource     `json:"source"`
	IDField        string          `json:"id_field"`
	TrackedFields  []string        `json:"tracked_fields"`
	RunWhen        string          `json:"run_when"`
	Followups      *FollowupLadder `json:"followups"`
	DryRun         bool            `json:"dry_run"`
	MaxItems       int             `json:"max_items"`
	// Safety caps on the crew's tracker tools in this schedule's runs
	MentionCapPerDay int        `json:"mention_cap_per_day"`
	CommentCapPerRun int        `json:"comment_cap_per_run"`
	LastCheckedAt    *time.Time `json:"last_checked_at"`
	LastChangedAt    *time.Time `json:"last_changed_at"`
	LastError        *string    `json:"last_error"`
	// SnapshotAt is when the stored snapshot was last confirmed; nil means the next check is the first
	SnapshotAt    *time.Time `json:"snapshot_at"`
	SnapshotItems int        `json:"snapshot_items"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// WatchInput creates or replaces a schedule's watch.
type WatchInput struct {
	ConnectionID  *string         `json:"connection_id"`
	Source        WatchSource     `json:"source"`
	IDField       string          `json:"id_field"`
	TrackedFields []string        `json:"tracked_fields"`
	RunWhen       string          `json:"run_when"`
	Followups     *FollowupLadder `json:"followups"`
	// DryRun defaults to true
	DryRun   *bool `json:"dry_run"`
	MaxItems int   `json:"max_items"`
	// MentionCapPerDay (default 3) and CommentCapPerRun (default 30) limit the crew's tracker tools
	MentionCapPerDay int `json:"mention_cap_per_day"`
	CommentCapPerRun int `json:"comment_cap_per_run"`
}

// SameSnapshotShape reports whether two watch definitions produce comparable snapshots. When they do not, the
// stored snapshot is dropped and the next check counts as the first.
func SameSnapshotShape(a Watch, b WatchInput) bool {
	return a.Source.snapshotShape() == b.Source.snapshotShape() && a.IDField == b.IDField && equalStrings(a.TrackedFields, b.TrackedFields) &&
		equalPtr(a.ConnectionID, b.ConnectionID)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// WatchCheck is one check of a watch, kept for the schedule's history.
type WatchCheck struct {
	ID           string    `json:"id"`
	CheckedAt    time.Time `json:"checked_at"`
	Outcome      string    `json:"outcome"`
	Added        int       `json:"added"`
	Changed      int       `json:"changed"`
	Removed      int       `json:"removed"`
	Unchanged    int       `json:"unchanged"`
	FollowupsDue int       `json:"followups_due"`
	WorkOrderID  *string   `json:"work_order_id"`
	Message      string    `json:"message"`
}

// Check outcomes.
const (
	WatchCheckStarted   = "started"
	WatchCheckNoChanges = "no_changes"
	WatchCheckBusy      = "busy"
	WatchCheckError     = "error"
)

// WatchStats shows what a watch saved over the last 30 days.
type WatchStats struct {
	Days        int `json:"days"`
	Checks      int `json:"checks"`
	RunsStarted int `json:"runs_started"`
	RunsSkipped int `json:"runs_skipped"`
	// AvgRunTokens is the crew's recent average tokens per run (prompt plus completion), 0 when unknown
	AvgRunTokens int `json:"avg_run_tokens"`
	// EstTokensSaved is RunsSkipped times AvgRunTokens
	EstTokensSaved  int     `json:"est_tokens_saved"`
	EstCostSavedUSD float64 `json:"est_cost_saved_usd"`
}

// Input is the saved watch as a definition, for checks and for comparing with a new one.
func (w Watch) Input() WatchInput {
	dry := w.DryRun
	return WatchInput{ConnectionID: w.ConnectionID, Source: w.Source, IDField: w.IDField, TrackedFields: w.TrackedFields,
		RunWhen: w.RunWhen, Followups: w.Followups, DryRun: &dry, MaxItems: w.MaxItems,
		MentionCapPerDay: w.MentionCapPerDay, CommentCapPerRun: w.CommentCapPerRun}
}
