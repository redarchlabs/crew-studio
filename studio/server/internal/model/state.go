package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Crew state: the action log (an exact record of what a crew did) and notes (versioned working memory). Both
// belong to a crew and, optionally, one of its schedules; see docs/crew-state-and-pmo-design.md.

const (
	// MaxNoteBytes keeps notes from growing into a second knowledge base
	MaxNoteBytes       = 64 << 10
	MaxLogDetailsBytes = 16 << 10
	maxSubjectLen      = 200
	maxIdempotencyLen  = 200
	maxWithinDays      = 3650
)

var (
	noteNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,99}$`)
	actionPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,99}$`)
	withinPattern   = regexp.MustCompile(`^([0-9]{1,6})([mhdw])$`)
)

// NoteInfo describes a note's latest version without its content.
type NoteInfo struct {
	ScheduleID   *string   `json:"schedule_id"`
	ScheduleName string    `json:"schedule_name"`
	Subject      string    `json:"subject"`
	Name         string    `json:"name"`
	Version      int       `json:"version"`
	Bytes        int       `json:"bytes"`
	WrittenBy    string    `json:"written_by"`
	RunID        *string   `json:"run_id"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NoteVersion is one saved version of a note.
type NoteVersion struct {
	ID         string    `json:"id"`
	CrewID     string    `json:"crew_id"`
	ScheduleID *string   `json:"schedule_id"`
	Subject    string    `json:"subject"`
	Name       string    `json:"name"`
	Version    int       `json:"version"`
	Content    string    `json:"content"`
	RunID      *string   `json:"run_id"`
	WrittenBy  string    `json:"written_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// NotePut writes a new version of a note. ExpectedVersion, when set, must match the current version (0 means
// the note must not exist yet), so two writers cannot overwrite each other's changes.
type NotePut struct {
	Name            string `json:"name"`
	Subject         string `json:"subject"`
	Content         string `json:"content"`
	ExpectedVersion *int   `json:"expected_version"`
}

// ActionEntry is one row of a crew's action log.
type ActionEntry struct {
	ID             string         `json:"id"`
	CrewID         string         `json:"crew_id"`
	ScheduleID     *string        `json:"schedule_id"`
	ScheduleName   string         `json:"schedule_name"`
	Subject        string         `json:"subject"`
	Action         string         `json:"action"`
	RunID          *string        `json:"run_id"`
	DryRun         bool           `json:"dry_run"`
	Details        map[string]any `json:"details"`
	IdempotencyKey *string        `json:"idempotency_key"`
	At             time.Time      `json:"at"`
}

// LogRecord adds an action log entry. Recording the same idempotency key twice for a crew is a no-op.
type LogRecord struct {
	Subject        string         `json:"subject"`
	Action         string         `json:"action"`
	Details        map[string]any `json:"details"`
	IdempotencyKey string         `json:"idempotency_key"`
}

// ValidNoteName reports whether name can name a note (letters, digits, '.', '_', ':' and '-').
func ValidNoteName(name string) bool {
	return noteNamePattern.MatchString(name)
}

// ValidAction reports whether action can name an action log entry, such as followup:due_soon.
func ValidAction(action string) bool {
	return actionPattern.MatchString(action)
}

// NormalizeSubject trims a subject such as jira:PRJ-15. "" means the whole crew (or schedule).
func NormalizeSubject(subject string) (string, error) {
	s := strings.TrimSpace(subject)
	var c checker
	checkSubject(&c, s)
	return s, c.err()
}

func checkSubject(c *checker, s string) {
	if len([]rune(s)) > maxSubjectLen {
		c.addf("subject", "must be at most %d characters", maxSubjectLen)
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		c.addf("subject", "must not contain control characters")
	}
}

// NormalizeNotePut validates a note write and returns a cleaned copy.
func NormalizeNotePut(in NotePut) (NotePut, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Subject = strings.TrimSpace(in.Subject)
	var c checker
	if !ValidNoteName(out.Name) {
		c.addf("name", "use letters, digits, '.', '_', ':' or '-' (at most 100, starting with a letter or digit)")
	}
	checkSubject(&c, out.Subject)
	if len(in.Content) > MaxNoteBytes {
		c.addf("content", "is over the %d-byte limit (%d bytes); keep long-term material in KM2", MaxNoteBytes, len(in.Content))
	}
	if strings.ContainsRune(in.Content, 0) {
		c.addf("content", "must not contain NUL characters")
	}
	if in.ExpectedVersion != nil && *in.ExpectedVersion < 0 {
		c.addf("expected_version", "must be 0 or more")
	}
	return out, c.err()
}

// NormalizeLogRecord validates a new action log entry and returns a cleaned copy (details is never nil).
func NormalizeLogRecord(in LogRecord) (LogRecord, error) {
	out := LogRecord{
		Subject: strings.TrimSpace(in.Subject), Action: strings.TrimSpace(in.Action),
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey), Details: in.Details,
	}
	if out.Details == nil {
		out.Details = map[string]any{}
	}
	var c checker
	c.text("subject", out.Subject, 1, maxSubjectLen)
	checkSubject(&c, out.Subject)
	if !ValidAction(out.Action) {
		c.addf("action", "use letters, digits, '.', '_', ':', '/' or '-' (at most 100), such as followup:due_soon")
	}
	c.text("idempotency_key", out.IdempotencyKey, 0, maxIdempotencyLen)
	if data, err := json.Marshal(out.Details); err != nil || len(data) > MaxLogDetailsBytes {
		c.addf("details", "is over the %d-byte limit", MaxLogDetailsBytes)
	}
	return out, c.err()
}

// ParseWithin reads a look-back period such as 30m, 24h, 7d or 2w. "" means no limit (0).
func ParseWithin(text string) (time.Duration, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	m := withinPattern.FindStringSubmatch(text)
	if m == nil {
		return 0, errors.New("within: use a number and a unit, such as 30m, 24h, 7d or 2w")
	}
	n, _ := strconv.Atoi(m[1])
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
	d := time.Duration(n) * unit
	if n == 0 || d > maxWithinDays*24*time.Hour {
		return 0, fmt.Errorf("within: must be more than 0 and at most %d days", maxWithinDays)
	}
	return d, nil
}
