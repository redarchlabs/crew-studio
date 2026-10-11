package model

import (
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	SourceManual   = "manual"
	SourceAPI      = "api"
	SourceSchedule = "schedule"
	SourceWebhook  = "webhook"
	// SourceChat is a work order a person started by confirming a chat proposal; its source id is the conversation
	SourceChat = "chat"

	// WorkOrderDraft is the status of a work order that has never been submitted
	WorkOrderDraft = "draft"

	AttachmentInput    = "input"
	AttachmentArtifact = "artifact"
	// AttachmentChat is a file sent with a chat message (it belongs to the message, not a work order or run)
	AttachmentChat       = "chat"
	AttachmentTranscript = "transcript"

	maxBriefLen    = 100_000
	maxTitleLen    = 200
	maxURLLen      = 2_000
	maxInputMapLen = 50
	maxPathLen     = 200
)

// WorkOrder is one request to execute a crew: a brief, input files, and every attempt (run) with its journal and
// full output.
type WorkOrder struct {
	ID     string `json:"id"`
	Number int64  `json:"number"`
	Title  string `json:"title"`
	Brief  string `json:"brief"`
	// CrewID is nil once the crew is deleted; TargetName keeps its name
	CrewID        *string           `json:"crew_id"`
	TargetName    string            `json:"target_name"`
	Inputs        map[string]string `json:"inputs"`
	BudgetUSD     *float64          `json:"budget_usd"`
	AreaIDs       []string          `json:"area_ids"`
	Source        string            `json:"source"`
	SourceID      *string           `json:"source_id"`
	CreatedBy     *string           `json:"created_by"`
	CreatedByName string            `json:"created_by_name"`
	// Status is "draft" before the first submit, then the status of the latest run
	Status       string     `json:"status"`
	CurrentRunID *string    `json:"current_run_id"`
	CostUSD      *float64   `json:"cost_usd"`
	RunCount     int        `json:"run_count"`
	SubmittedAt  *time.Time `json:"submitted_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// Detail only
	Attachments *[]Attachment `json:"attachments,omitempty"`
	Runs        *[]Run        `json:"runs,omitempty"`
}

// WorkOrderInput creates or edits a work order.
type WorkOrderInput struct {
	Title     string            `json:"title"`
	Brief     string            `json:"brief"`
	CrewID    *string           `json:"crew_id"`
	Inputs    map[string]string `json:"inputs"`
	BudgetUSD *float64          `json:"budget_usd"`
	// Submit starts the work order straight away (create only)
	Submit bool `json:"submit"`
	LegacyTarget
}

// LegacyTarget holds the fields callers sent when a work order, schedule or trigger could target a single task.
// They are never returned; a task target is refused with a pointer to crews.
type LegacyTarget struct {
	TargetType string  `json:"target_type,omitempty"`
	TaskID     *string `json:"task_id,omitempty"`
}

type Attachment struct {
	ID          string  `json:"id"`
	WorkOrderID *string `json:"work_order_id"`
	RunID       *string `json:"run_id"`
	// MessageID is the chat message a file of kind "chat" was sent with
	MessageID     *string   `json:"message_id,omitempty"`
	Kind          string    `json:"kind"`
	Filename      string    `json:"filename"`
	ContentType   string    `json:"content_type"`
	SizeBytes     int64     `json:"size_bytes"`
	SHA256        string    `json:"sha256"`
	CreatedByName string    `json:"created_by_name"`
	CreatedAt     time.Time `json:"created_at"`
	// StorageKey is internal
	StorageKey string `json:"-"`
}

// WorkOrderSpec is the part of a RunSpec that carries the work order to the runner.
type WorkOrderSpec struct {
	ID          string           `json:"id"`
	Number      int64            `json:"number"`
	Title       string           `json:"title"`
	Brief       string           `json:"brief"`
	Attachments []SpecAttachment `json:"attachments"`
	// Revision is set when the attempt was started from feedback on an earlier one
	Revision *RevisionSpec `json:"revision,omitempty"`
}

// SpecAttachment is an input file copied into the run's workspace before the run starts.
type SpecAttachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	// Path is relative to the run's workspace
	Path string `json:"path"`
}

// noTaskTargets explains how to run a single task now that everything runs a crew.
const noTaskTargets = "work orders, schedules and triggers run crews; to run one task, make a crew with just that task (Make a crew on the task's page) and use its crew_id"

func normalizeCrewTarget(c *checker, crewID *string, legacy LegacyTarget) *string {
	crewID = trimmedPtr(crewID)
	switch {
	case trimmedPtr(legacy.TaskID) != nil:
		c.addf("task_id", "%s", noTaskTargets)
	case legacy.TargetType != "" && legacy.TargetType != "crew":
		c.addf("target_type", "%s", noTaskTargets)
	case crewID == nil:
		c.addf("crew_id", "is required")
	}
	c.optionalUUID("crew_id", crewID)
	return crewID
}

func (c *checker) optionalBudget(field string, v *float64) {
	if v != nil {
		c.positiveBudget(field, *v)
	}
}

func normalizeInputValues(c *checker, in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	if len(in) > maxInputs {
		c.addf("inputs", "at most %d inputs", maxInputs)
	}
	for k, v := range in {
		if !inputKeyPattern.MatchString(k) {
			c.addf("inputs", "%q is not a valid input key", k)
			continue
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}

// NormalizeWorkOrder validates a work order, returning a cleaned copy.
func NormalizeWorkOrder(in WorkOrderInput) (WorkOrderInput, error) {
	out := in
	out.Title = strings.TrimSpace(in.Title)
	out.Brief = strings.TrimSpace(in.Brief)
	var c checker
	c.text("title", out.Title, 1, maxTitleLen)
	c.text("brief", out.Brief, 0, maxBriefLen)
	out.CrewID = normalizeCrewTarget(&c, in.CrewID, in.LegacyTarget)
	out.LegacyTarget = LegacyTarget{}
	out.Inputs = normalizeInputValues(&c, in.Inputs)
	c.optionalBudget("budget_usd", in.BudgetUSD)
	return out, c.err()
}

const (
	OverlapSkip  = "skip"
	OverlapAllow = "allow"
)

// Schedule creates a work order for a crew on a cron schedule.
type Schedule struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	CrewID        *string           `json:"crew_id"`
	TargetName    string            `json:"target_name"`
	Cron          string            `json:"cron"`
	Timezone      string            `json:"timezone"`
	Brief         string            `json:"brief"`
	Inputs        map[string]string `json:"inputs"`
	BudgetUSD     *float64          `json:"budget_usd"`
	Enabled       bool              `json:"enabled"`
	OverlapPolicy string            `json:"overlap_policy"`
	RunAs         *string           `json:"run_as"`
	RunAsName     string            `json:"run_as_name"`
	// RunAsKey is the API key that last saved the schedule; its runs keep that key's limits
	RunAsKey        *string    `json:"-"`
	AreaIDs         []string   `json:"area_ids"`
	NextRunAt       *time.Time `json:"next_run_at"`
	LastRunAt       *time.Time `json:"last_run_at"`
	LastWorkOrderID *string    `json:"last_work_order_id"`
	LastError       *string    `json:"last_error"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LegacyTarget
}

// NormalizeSchedule checks the fields that do not need the cron parser or the database.
func NormalizeSchedule(in Schedule) (Schedule, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Description = strings.TrimSpace(in.Description)
	out.Cron = strings.Join(strings.Fields(in.Cron), " ")
	out.Timezone = strings.TrimSpace(in.Timezone)
	out.Brief = strings.TrimSpace(in.Brief)
	if out.Timezone == "" {
		out.Timezone = "UTC"
	}
	if out.OverlapPolicy == "" {
		out.OverlapPolicy = OverlapSkip
	}
	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	c.text("description", out.Description, 0, maxShortTextLen)
	c.text("cron", out.Cron, 1, maxNameLen)
	c.text("brief", out.Brief, 0, maxBriefLen)
	if _, err := time.LoadLocation(out.Timezone); err != nil {
		c.addf("timezone", "%q is not a known time zone (use names like Europe/London)", out.Timezone)
	}
	if out.OverlapPolicy != OverlapSkip && out.OverlapPolicy != OverlapAllow {
		c.addf("overlap_policy", "must be %q or %q", OverlapSkip, OverlapAllow)
	}
	out.CrewID = normalizeCrewTarget(&c, in.CrewID, in.LegacyTarget)
	out.LegacyTarget = LegacyTarget{}
	out.Inputs = normalizeInputValues(&c, in.Inputs)
	c.optionalBudget("budget_usd", in.BudgetUSD)
	return out, c.err()
}

// Trigger lets another system create a work order by POSTing JSON to a URL with a secret token.
type Trigger struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	CrewID         *string           `json:"crew_id"`
	TargetName     string            `json:"target_name"`
	Enabled        bool              `json:"enabled"`
	TokenPrefix    string            `json:"token_prefix"`
	InputMap       map[string]string `json:"input_map"`
	Brief          string            `json:"brief"`
	IncludePayload bool              `json:"include_payload"`
	BudgetUSD      *float64          `json:"budget_usd"`
	CallbackURL    *string           `json:"callback_url"`
	// SigningSecret is returned only to people who can manage triggers
	SigningSecret string  `json:"signing_secret,omitempty"`
	RunAs         *string `json:"run_as"`
	RunAsName     string  `json:"run_as_name"`
	// RunAsKey is the API key that last saved the trigger; its runs keep that key's limits
	RunAsKey    *string    `json:"-"`
	AreaIDs     []string   `json:"area_ids"`
	LastFiredAt *time.Time `json:"last_fired_at"`
	FireCount   int        `json:"fire_count"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	// Token is set only in the response that creates or rotates it
	Token string `json:"token,omitempty"`
	LegacyTarget
}

// NormalizeTrigger validates a trigger, returning a cleaned copy.
func NormalizeTrigger(in Trigger) (Trigger, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Description = strings.TrimSpace(in.Description)
	out.Brief = strings.TrimSpace(in.Brief)
	out.CallbackURL = trimmedPtr(in.CallbackURL)
	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	c.text("description", out.Description, 0, maxShortTextLen)
	c.text("brief", out.Brief, 0, maxBriefLen)
	out.CrewID = normalizeCrewTarget(&c, in.CrewID, in.LegacyTarget)
	out.LegacyTarget = LegacyTarget{}
	c.optionalBudget("budget_usd", in.BudgetUSD)
	if len(in.InputMap) > maxInputMapLen {
		c.addf("input_map", "at most %d entries", maxInputMapLen)
	}
	out.InputMap = make(map[string]string, len(in.InputMap))
	for key, path := range in.InputMap {
		path = strings.TrimSpace(path)
		switch {
		case !inputKeyPattern.MatchString(key):
			c.addf("input_map", "%q is not a valid input key", key)
		case path == "" || len(path) > maxPathLen || strings.ContainsAny(path, " \t\n"):
			c.addf("input_map", "%s: the payload path must be a dotted path like customer.name", key)
		default:
			out.InputMap[key] = path
		}
	}
	if out.CallbackURL != nil {
		if problem := CheckCallbackURL(*out.CallbackURL); problem != "" {
			c.addf("callback_url", "%s", problem)
		}
	}
	return out, c.err()
}

// CheckCallbackURL returns what is wrong with a callback URL's shape, or "". Where it may point
// (public addresses, or private ones on allow-listed hosts) is checked when it is delivered.
func CheckCallbackURL(raw string) string {
	if len(raw) > maxURLLen {
		return "is too long"
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "":
		return "must be an absolute http(s) URL"
	case u.Scheme != "https" && u.Scheme != "http":
		return "must use https (or http for an allow-listed internal host)"
	case u.User != nil:
		return "must not contain credentials"
	case u.Fragment != "":
		return "must not contain a #fragment"
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsUnspecified() || ip.IsMulticast()) {
		return "must point at a real host"
	}
	return ""
}
