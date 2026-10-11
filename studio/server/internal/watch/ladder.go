package watch

import (
	"slices"
	"strings"
	"time"

	"crewstudio/internal/model"
)

// FollowupActionPrefix starts the action of every follow-up log entry: followup:<step>.
const FollowupActionPrefix = "followup:"

// RecordHint tells the crew how to mark a follow-up as sent, so the next tick does not repeat it.
const RecordHint = "Posting a follow-up with the Comment on Tracker Issue tool and the step's name as followup_step " +
	"records it for you; that is the only way to record a follow-up of a tracker issue. For other items, after " +
	"notifying, record it with the Record Action tool: subject = the item's subject, " +
	"action = the step's action (such as followup:due_soon), details = who was notified and a link. " +
	"Until it is recorded, the step stays due on the next check."

// SentKey identifies the latest log entry for one item and step.
type SentKey struct {
	Subject string
	Action  string
}

// Sent is when each (item, follow-up step) was last recorded in the action log.
type Sent map[SentKey]time.Time

// FollowupDue is one step due for one item, as the run receives it in "followups_due".
type FollowupDue struct {
	Item    string `json:"item"`
	Subject string `json:"subject"`
	Step    string `json:"step"`
	Action  string `json:"action"`
	Notify  string `json:"notify"`
	// NotifyValue is the value of the tracked field named by notify, when it names one (such as an assignee)
	NotifyValue any `json:"notify_value,omitempty"`
	// NotifyPerson is who "owner" (the assignee) or "lead" (the project lead, or the tracked item's lead) is, for
	// tracker items: mention them by account_id
	NotifyPerson *NotifyPerson `json:"notify_person,omitempty"`
	// Date is the item's date; StepDate is when this step fell due (moved past quiet days)
	Date       string         `json:"date"`
	StepDate   string         `json:"step_date"`
	DaysLate   int            `json:"days_late"`
	Repeat     string         `json:"repeat,omitempty"`
	LastSentAt *time.Time     `json:"last_sent_at"`
	Fields     map[string]any `json:"fields"`
}

// Followups is the "followups_due" input: the steps due now, how many wait for the next tick, and how to record them.
type Followups struct {
	Due         []FollowupDue `json:"due"`
	MoreWaiting int           `json:"more_waiting"`
	// Undated counts items (not skipped) whose date field is empty or not a date, so no step can be due for them
	Undated     int    `json:"undated_items"`
	DryRun      bool   `json:"dry_run"`
	HowToRecord string `json:"how_to_record"`
}

type civilDate struct{ t time.Time }

func dateIn(t time.Time, loc *time.Location) civilDate {
	y, m, d := t.In(loc).Date()
	return civilDate{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func (d civilDate) String() string { return d.t.Format(time.DateOnly) }

func (d civilDate) addDays(n int) civilDate { return civilDate{d.t.AddDate(0, 0, n)} }

func (d civilDate) weekday() string { return d.t.Weekday().String()[:3] }

var dateLayouts = []string{time.DateOnly, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04"}

// parseItemDate reads an item's date (ISO 8601 date or date-time) as a day in loc.
func parseItemDate(v any, loc *time.Location) (civilDate, bool) {
	text := strings.TrimSpace(scalarText(v))
	if text == "" {
		return civilDate{}, false
	}
	for _, layout := range dateLayouts {
		if t, err := time.ParseInLocation(layout, text, loc); err == nil {
			return dateIn(t, loc), true
		}
	}
	return civilDate{}, false
}

// DueFollowups walks every item and step of the ladder. A step is due when its date has passed (moved past quiet
// days), the item is not skipped, and the log has no entry for it recorded on or after that date (so a step comes
// back when the item's date moves later), or, for a repeating step, none within the last repeat period (counted in
// calendar days, so a reminder recorded minutes after a tick does not slip a whole period). When several steps' dates have passed only the latest can be due, so someone already overdue
// is not also told it is "due soon". Nothing is due on a quiet day. The oldest steps go first, up to the cap.
func DueFollowups(l model.FollowupLadder, items []Item, sent Sent, now time.Time, loc *time.Location) ([]FollowupDue, int) {
	today := dateIn(now, loc)
	if slices.Contains(l.QuietDays, today.weekday()) {
		return []FollowupDue{}, 0
	}
	steps := sortedSteps(l.Steps)
	due := []FollowupDue{}
	for _, it := range items {
		if skipped(l.SkipWhen, it.Fields) {
			continue
		}
		date, ok := parseItemDate(it.Fields[l.DateField], loc)
		if !ok {
			continue
		}
		if d, ok := currentStep(steps, date, today, l.QuietDays, it, sent, loc); ok {
			d.Fields = displayFields(it.Fields)
			due = append(due, d)
		}
	}
	slices.SortFunc(due, func(a, b FollowupDue) int {
		if c := strings.Compare(a.StepDate, b.StepDate); c != 0 {
			return c
		}
		return strings.Compare(a.Item, b.Item)
	})
	limit := l.MaxPerTick
	if limit <= 0 {
		limit = model.DefaultFollowupsPerTick
	}
	if len(due) > limit {
		return due[:limit], len(due) - limit
	}
	return due, 0
}

type ladderStep struct {
	model.FollowupStep
	offset int
	repeat int
}

func sortedSteps(in []model.FollowupStep) []ladderStep {
	out := make([]ladderStep, 0, len(in))
	for _, s := range in {
		offset, err := model.ParseDayOffset(s.Offset)
		if err != nil {
			continue
		}
		repeat := 0
		if s.Repeat != "" {
			if repeat, err = model.ParseRepeatDays(s.Repeat); err != nil {
				continue
			}
		}
		out = append(out, ladderStep{FollowupStep: s, offset: offset, repeat: repeat})
	}
	slices.SortStableFunc(out, func(a, b ladderStep) int { return a.offset - b.offset })
	return out
}

// UndatedItems counts the items the ladder cannot place: not skipped, but with no usable date.
func UndatedItems(l model.FollowupLadder, items []Item, loc *time.Location) int {
	n := 0
	for _, it := range items {
		if _, ok := parseItemDate(it.Fields[l.DateField], loc); !ok && !skipped(l.SkipWhen, it.Fields) {
			n++
		}
	}
	return n
}

// currentStep finds the latest step whose date has passed and reports it when it is due.
func currentStep(steps []ladderStep, date, today civilDate, quiet []string, it Item, sent Sent, loc *time.Location) (FollowupDue, bool) {
	var step *ladderStep
	var stepDate civilDate
	for i := range steps {
		d := moveOffQuietDays(date.addDays(steps[i].offset), quiet)
		if d.t.After(today.t) {
			continue
		}
		step, stepDate = &steps[i], d
	}
	if step == nil {
		return FollowupDue{}, false
	}
	action := FollowupActionPrefix + step.Name
	last, wasSent := sent[SentKey{Subject: it.ID, Action: action}]
	if wasSent && stillSent(dateIn(last, loc), stepDate, today, step.repeat) {
		return FollowupDue{}, false
	}
	out := FollowupDue{Item: it.ID, Subject: it.ID, Step: step.Name, Action: action, Notify: step.Notify,
		Date: date.String(), StepDate: stepDate.String(), DaysLate: int(today.t.Sub(stepDate.t).Hours() / 24), Repeat: step.Repeat}
	if v, ok := it.Fields[step.Notify]; ok {
		out.NotifyValue = display(v)
	}
	out.NotifyPerson = notifyPerson(it.Meta, step.Notify)
	if wasSent {
		at := last
		out.LastSentAt = &at
	}
	return out, true
}

// stillSent says whether a step recorded on lastDay still counts: it was recorded on or after the step's date
// (an entry from before a moved date does not), and for a repeating step its period has not run out yet.
func stillSent(lastDay, stepDate, today civilDate, repeat int) bool {
	if lastDay.t.Before(stepDate.t) {
		return false
	}
	return repeat == 0 || today.t.Before(lastDay.addDays(repeat).t)
}

// notifyPerson is the person a step's notify role names on a tracker item, or nil.
func notifyPerson(m *ItemMeta, notify string) *NotifyPerson {
	if m == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(notify)) {
	case "owner", "assignee":
		return m.Owner
	case "lead":
		return m.Lead
	}
	return nil
}

func moveOffQuietDays(d civilDate, quiet []string) civilDate {
	for i := 0; i < 7 && slices.Contains(quiet, d.weekday()); i++ {
		d = d.addDays(1)
	}
	return d
}

func skipped(skipWhen map[string][]string, fields map[string]any) bool {
	for field, values := range skipWhen {
		text := strings.TrimSpace(scalarText(fields[field]))
		for _, v := range values {
			if strings.EqualFold(text, v) {
				return true
			}
		}
	}
	return false
}
