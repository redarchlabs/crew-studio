package model

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Report delivery (docs/crew-state-and-pmo-design.md, section 12): one report per project, written by a crew,
// goes to the work order output and to every destination set on the schedule that started the run. Each delivery
// is logged in the action log as report:<destination>, so a failed one can be retried without repeating the
// others.

// Destination kinds. The work order output is automatic; writing status back to the tracker is not built yet.
const (
	DestinationWorkOrder = "work_order"
	DestinationSlack     = "slack"
	DestinationTeams     = "teams"
	DestinationEmail     = "email"
	DestinationKM2       = KindKM2
	// DestinationTracker is reserved: writing status back to Jira or Linear is deferred (design section 12)
	DestinationTracker = "tracker"
)

// DestinationKinds are the kinds a schedule can send reports to; each needs a connection of the same kind.
var DestinationKinds = []string{DestinationSlack, DestinationTeams, DestinationEmail, DestinationKM2}

// Delivery statuses.
const (
	DeliveryPending   = "pending"
	DeliverySent      = "sent"
	DeliveryWouldSend = "would_send"
	DeliveryFailed    = "failed"
)

// Caps on what one run may deliver.
const (
	// MaxReportBytes is the largest report deliver_report accepts (the size of a note, which keeps it too)
	MaxReportBytes = MaxNoteBytes
	// MaxReportsPerRun is how many reports (projects) one run may deliver
	MaxReportsPerRun = 20
	// MaxDeliveriesPerRun is how many sends (reports times destinations) one run may make
	MaxDeliveriesPerRun = 60
	// MaxDestinations is how many destinations a schedule may have
	MaxDestinations = 10
	// MaxEmailRecipients is how many people one email destination may send to
	MaxEmailRecipients  = 50
	maxReportProjectLen = 100
	maxProjectSlugLen   = 60
	maxEmailAddressLen  = 254
)

// ConfigEmailDomains is an email connection's optional list of the domains reports may be sent to.
const ConfigEmailDomains = "allowed_domains"

// EmailDomains are the domains an email connection may send reports to (empty: any), lower case.
func EmailDomains(config map[string]string) []string {
	out := []string{}
	for _, d := range strings.FieldsFunc(config[ConfigEmailDomains], func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if d = strings.ToLower(strings.TrimPrefix(d, "@")); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// RecipientsOutside lists the recipients whose domain an email connection does not allow (none when it lists none).
func RecipientsOutside(config map[string]string, recipients []string) []string {
	domains := EmailDomains(config)
	out := []string{}
	if len(domains) == 0 {
		return out
	}
	for _, r := range recipients {
		_, domain, _ := strings.Cut(strings.ToLower(r), "@")
		if !slices.Contains(domains, domain) {
			out = append(out, r)
		}
	}
	return out
}

// ActionReport is the action log action of a delivery to kind, such as report:teams.
func ActionReport(kind string) string { return "report:" + kind }

// ProjectSubject is the subject a project's notes and report deliveries are kept under, such as project:atlas.
func ProjectSubject(slug string) string { return "project:" + slug }

// ReportNote is the note that keeps a project's latest weekly report, so next week's builds on it.
const ReportNote = "weekly-report"

// DailyReportNote keeps a project's latest daily report, so the next day's can say what changed. A daily report
// never touches the weekly one.
const DailyReportNote = "daily-report"

// Report cadences: the weekly status report (the default) and the daily report.
const (
	CadenceWeekly = "weekly"
	CadenceDaily  = "daily"
)

// ValidCadence reports whether a cadence is weekly or daily.
func ValidCadence(c string) bool { return c == CadenceWeekly || c == CadenceDaily }

// ReportNoteOf is the note a report of the cadence is kept in.
func ReportNoteOf(cadence string) string {
	if cadence == CadenceDaily {
		return DailyReportNote
	}
	return ReportNote
}

// ReportPeriod is what a report made at t covers, in loc: the ISO week (2026-W41) or, for a daily report, the
// local date (2026-10-06).
func ReportPeriod(cadence string, t time.Time, loc *time.Location) string {
	if cadence != CadenceDaily {
		return ISOWeek(t, loc)
	}
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format(time.DateOnly)
}

// ReportTitle is a report's title: "Atlas status, 2026-W41" or "Atlas daily report, 2026-10-06".
func ReportTitle(project, cadence, period string) string {
	if cadence == CadenceDaily {
		return fmt.Sprintf("%s daily report, %s", project, period)
	}
	return fmt.Sprintf("%s status, %s", project, period)
}

// ReportDestination is one place a schedule's reports go.
type ReportDestination struct {
	ID             string    `json:"id"`
	ScheduleID     string    `json:"schedule_id"`
	Position       int       `json:"position"`
	Kind           string    `json:"kind"`
	ConnectionID   string    `json:"connection_id"`
	ConnectionName string    `json:"connection_name"`
	Recipients     []string  `json:"recipients"`
	Folder         string    `json:"folder"`
	CreatedAt      time.Time `json:"created_at"`
}

// ReportDestinationInput sets one destination.
type ReportDestinationInput struct {
	Kind         string   `json:"kind"`
	ConnectionID string   `json:"connection_id"`
	Recipients   []string `json:"recipients"`
	Folder       string   `json:"folder"`
}

// NormalizeDestinations validates a schedule's whole list of destinations and returns a cleaned copy.
func NormalizeDestinations(in []ReportDestinationInput) ([]ReportDestinationInput, error) {
	var c checker
	if len(in) > MaxDestinations {
		c.addf("destinations", "at most %d destinations", MaxDestinations)
	}
	out := make([]ReportDestinationInput, 0, len(in))
	for i, d := range in {
		field := fmt.Sprintf("destinations[%d]", i)
		clean := ReportDestinationInput{Kind: strings.TrimSpace(d.Kind), ConnectionID: strings.TrimSpace(d.ConnectionID),
			Folder: strings.TrimSpace(d.Folder), Recipients: []string{}}
		if !slices.Contains(DestinationKinds, clean.Kind) {
			c.addf(field+".kind", "must be one of %s", strings.Join(DestinationKinds, ", "))
		}
		if !uuidPattern.MatchString(clean.ConnectionID) {
			c.addf(field+".connection_id", "must be a connection id")
		}
		switch clean.Kind {
		case DestinationEmail:
			clean.Recipients = normalizeRecipients(&c, field+".recipients", d.Recipients)
		default:
			if len(d.Recipients) > 0 {
				c.addf(field+".recipients", "only email destinations have recipients")
			}
		}
		if clean.Folder != "" && (clean.Kind != DestinationKM2 || !uuidPattern.MatchString(clean.Folder)) {
			c.addf(field+".folder", "only KM2 destinations have a folder, and it must be a KM2 folder id")
		}
		clean.Folder = strings.ToLower(clean.Folder)
		if slices.ContainsFunc(out, func(o ReportDestinationInput) bool { return sameDestination(o, clean) }) {
			c.addf(field, "is the same as an earlier destination")
		}
		out = append(out, clean)
	}
	return out, c.err()
}

func sameDestination(a, b ReportDestinationInput) bool {
	return a.Kind == b.Kind && a.ConnectionID == b.ConnectionID && a.Folder == b.Folder && slices.Equal(a.Recipients, b.Recipients)
}

func normalizeRecipients(c *checker, field string, in []string) []string {
	out := []string{}
	if len(in) == 0 {
		c.addf(field, "give at least one email address")
	}
	if len(in) > MaxEmailRecipients {
		c.addf(field, "at most %d addresses", MaxEmailRecipients)
		return out
	}
	for _, raw := range in {
		addr, err := NormalizeEmailAddress(raw)
		switch {
		case err != nil:
			c.addf(field, "%q %s", clipAddress(raw, 80), err.Error())
		case slices.Contains(out, addr):
			c.addf(field, "lists %s more than once", addr)
		default:
			out = append(out, addr)
		}
	}
	return out
}

// NormalizeEmailAddress checks a bare email address (no display name), safe to put in a mail header: one line,
// no angle brackets, commas or quotes, at most 254 characters. The domain is lower-cased.
func NormalizeEmailAddress(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > maxEmailAddressLen || strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 ||
		strings.ContainsAny(s, "<>,;\"()[]\\") {
		return "", fmt.Errorf("is not a plain email address")
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" {
		return "", fmt.Errorf("is not a plain email address")
	}
	local, domain, _ := strings.Cut(s, "@")
	if local == "" || (!strings.Contains(domain, ".") && domain != "localhost") {
		return "", fmt.Errorf("is not a plain email address")
	}
	return local + "@" + strings.ToLower(domain), nil
}

func clipAddress(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// ReportRequest is what deliver_report sends: one project's report as Markdown.
type ReportRequest struct {
	Project  string `json:"project"`
	Markdown string `json:"markdown"`
	// Cadence is weekly (the default) or daily
	Cadence string `json:"cadence,omitempty"`
}

// NormalizeReport validates a report and returns a cleaned copy.
func NormalizeReport(in ReportRequest) (ReportRequest, error) {
	out := ReportRequest{Project: strings.Join(strings.Fields(in.Project), " "), Markdown: strings.TrimSpace(in.Markdown),
		Cadence: strings.ToLower(strings.TrimSpace(in.Cadence))}
	if out.Cadence == "" {
		out.Cadence = CadenceWeekly
	}
	var c checker
	c.text("project", out.Project, 1, maxReportProjectLen)
	if strings.IndexFunc(in.Project, unicode.IsControl) >= 0 {
		c.addf("project", "must not contain control characters")
	}
	switch {
	case out.Markdown == "":
		c.addf("markdown", "is required")
	case len(out.Markdown) > MaxReportBytes:
		c.addf("markdown", "is over the %d-byte limit (%d bytes); shorten the report", MaxReportBytes, len(out.Markdown))
	case !utf8.ValidString(out.Markdown) || strings.ContainsRune(out.Markdown, 0):
		c.addf("markdown", "must be UTF-8 text without NUL characters")
	}
	if out.Project != "" && ProjectSlug(out.Project) == "" {
		c.addf("project", "needs at least one letter or digit")
	}
	if !ValidCadence(out.Cadence) {
		c.addf("cadence", "must be %q or %q", CadenceWeekly, CadenceDaily)
	}
	return out, c.err()
}

var slugRuns = regexp.MustCompile(`[^a-z0-9]+`)

// ProjectSlug turns a project name into the key its notes, KM2 documents and log entries use: lower case ASCII
// letters and digits joined by dashes, such as "Project Atlas (EU)" -> "project-atlas-eu". "" when nothing is left.
func ProjectSlug(project string) string {
	s := strings.Trim(slugRuns.ReplaceAllString(strings.ToLower(project), "-"), "-")
	if len(s) > maxProjectSlugLen {
		s = strings.TrimRight(s[:maxProjectSlugLen], "-")
	}
	return s
}

// ISOWeek is t's ISO week in loc, such as 2026-W41.
func ISOWeek(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	year, week := t.In(loc).ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// ReportExternalRef is the KM2 external_ref of a project's report, such as atlas-weekly-2026-W41 or
// atlas-daily-2026-10-06. Saving the same period again makes a new version in KM2, not a copy.
func ReportExternalRef(slug, cadence, period string) string {
	if cadence == CadenceDaily {
		return slug + "-daily-" + period
	}
	return slug + "-weekly-" + period
}

// Report is one project's report from one run, with where it went.
type Report struct {
	ID          string  `json:"id"`
	CrewID      string  `json:"crew_id"`
	ScheduleID  *string `json:"schedule_id"`
	RunID       *string `json:"run_id"`
	WorkOrderID *string `json:"work_order_id"`
	Project     string  `json:"project"`
	ProjectSlug string  `json:"project_slug"`
	// Week is the ISO week the report was made in
	Week string `json:"week"`
	// Cadence is weekly or daily; Period is what the report covers (the ISO week, or the local date of a daily report)
	Cadence string `json:"cadence"`
	Period  string `json:"period"`
	// Snapshot is the project's tracked work when the report was delivered (internal/duereport); null when the
	// tracker could not be read then
	Snapshot   json.RawMessage  `json:"snapshot"`
	Title      string           `json:"title"`
	Markdown   string           `json:"markdown"`
	Bytes      int              `json:"bytes"`
	DryRun     bool             `json:"dry_run"`
	CreatedAt  time.Time        `json:"created_at"`
	Deliveries []ReportDelivery `json:"deliveries"`
}

// ReportDelivery is one delivery of a report and how it went.
type ReportDelivery struct {
	ID             string     `json:"id"`
	ReportID       string     `json:"report_id"`
	Position       int        `json:"position"`
	Kind           string     `json:"kind"`
	ConnectionID   *string    `json:"connection_id"`
	ConnectionName string     `json:"connection_name"`
	Recipients     []string   `json:"recipients"`
	Folder         string     `json:"folder"`
	Status         string     `json:"status"`
	Message        string     `json:"message"`
	Attempts       int        `json:"attempts"`
	IdempotencyKey string     `json:"-"`
	SentAt         *time.Time `json:"sent_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Target describes where a delivery goes, for people: the connection, and for email how many recipients.
func (d ReportDelivery) Target() string {
	switch d.Kind {
	case DestinationWorkOrder:
		return "the work order output"
	case DestinationEmail:
		return fmt.Sprintf("%s (%d recipients)", d.ConnectionName, len(d.Recipients))
	}
	return d.ConnectionName
}

// DeliveryKey is the action log idempotency key of a report's delivery at position.
func DeliveryKey(reportID string, position int) string {
	return fmt.Sprintf("report:%s:%d", reportID, position)
}
