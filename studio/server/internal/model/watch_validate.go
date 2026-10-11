package model

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	maxTrackedFields = 30
	maxFieldPathLen  = 200
	maxFieldSegments = 12
	maxSourcePathLen = 2_000
	maxSelectorLen   = 500
	maxLadderSteps   = 20
	maxSkipValues    = 20
	maxNotifyLen     = 100
	maxOffsetDays    = 3650
	// WatchFilesDir is the only workspace folder a watch may read, so files there are meant to be watched
	WatchFilesDir      = "watch"
	webPageIDField     = "url"
	webPageTextField   = "text"
	maxWorkspacePathLn = 300
)

var (
	stepNamePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,49}$`)
	dayOffsetPattern = regexp.MustCompile(`^([+-]?)([0-9]{1,4})([dw])$`)
	weekdayNames     = map[string]string{
		"mon": "Mon", "monday": "Mon", "tue": "Tue", "tues": "Tue", "tuesday": "Tue", "wed": "Wed", "wednesday": "Wed",
		"thu": "Thu", "thur": "Thu", "thurs": "Thu", "thursday": "Thu", "fri": "Fri", "friday": "Fri",
		"sat": "Sat", "saturday": "Sat", "sun": "Sun", "sunday": "Sun",
	}
)

// ParseDayOffset reads a ladder offset such as -3d, 0d, +2d or 1w into days.
func ParseDayOffset(text string) (int, error) {
	m := dayOffsetPattern.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return 0, errors.New("use a number of days or weeks, such as -3d, 0d, +2d or 1w")
	}
	n, _ := strconv.Atoi(m[2])
	if m[3] == "w" {
		n *= 7
	}
	if n > maxOffsetDays {
		return 0, fmt.Errorf("must be at most %d days", maxOffsetDays)
	}
	if m[1] == "-" {
		n = -n
	}
	return n, nil
}

// ParseRepeatDays reads a repeat period such as 7d or 1w; it must be at least one day.
func ParseRepeatDays(text string) (int, error) {
	if strings.HasPrefix(strings.TrimSpace(text), "-") {
		return 0, errors.New("must be a positive number of days or weeks, such as 7d")
	}
	n, err := ParseDayOffset(text)
	if err == nil && n < 1 {
		err = errors.New("must be at least 1d")
	}
	return n, err
}

// CleanFieldPath trims a dotted JSON path (an optional leading "$." is dropped). "" stays "".
func CleanFieldPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "$" {
		return ""
	}
	return strings.TrimPrefix(p, "$.")
}

// checkFieldPath reports what is wrong with a dotted path such as fields.status.name, or "".
func checkFieldPath(p string) string {
	if len(p) > maxFieldPathLen {
		return fmt.Sprintf("must be at most %d characters", maxFieldPathLen)
	}
	if strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "must not contain control characters"
	}
	segments := strings.Split(p, ".")
	if len(segments) > maxFieldSegments {
		return fmt.Sprintf("may have at most %d parts", maxFieldSegments)
	}
	for _, s := range segments {
		if strings.TrimSpace(s) == "" {
			return "use a dotted path such as fields.status (no empty parts)"
		}
	}
	return ""
}

// checkColumn reports what is wrong with a CSV column name, or "".
func checkColumn(name string) string {
	if len([]rune(name)) > maxFieldPathLen {
		return fmt.Sprintf("must be at most %d characters", maxFieldPathLen)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "must not contain control characters"
	}
	return ""
}

// NormalizeWatch validates a watch definition and returns a cleaned copy with defaults filled in. Things that
// need the database (the connection's kind and areas) or the source code (CSS selectors) are checked by callers.
func NormalizeWatch(in WatchInput) (WatchInput, error) {
	out := in
	var c checker
	out.Source = normalizeSource(&c, in.Source)
	kind, known := WatchSourceKindOf(out.Source.Kind)
	if !known {
		names := make([]string, len(WatchSourceKinds))
		for i, k := range WatchSourceKinds {
			names[i] = k.Kind
		}
		c.addf("source.kind", "must be one of %s", strings.Join(names, ", "))
	}
	out.ConnectionID = trimmedPtr(in.ConnectionID)
	c.optionalUUID("connection_id", out.ConnectionID)
	switch {
	case known && kind.Connection != "" && out.ConnectionID == nil:
		c.addf("connection_id", "is required: pick a %s connection", connectionLabel(kind.Connection))
	case known && kind.Connection == "" && out.ConnectionID != nil:
		c.addf("connection_id", "a %s source does not use a connection", kind.Kind)
	}
	if known && len(kind.Fields) > 0 {
		out.IDField, out.TrackedFields = normalizeFixedFields(&c, in.IDField, in.TrackedFields, kind.Fields)
	} else if known && kind.Items {
		csv := out.Source.Format == "csv"
		out.IDField, out.TrackedFields = normalizeItemFields(&c, in.IDField, in.TrackedFields, csv)
	} else {
		out.IDField, out.TrackedFields = webPageIDField, []string{webPageTextField}
	}
	out.RunWhen = strings.TrimSpace(in.RunWhen)
	if out.RunWhen == "" {
		out.RunWhen = WatchRunChanged
	}
	if !slices.Contains([]string{WatchRunChanged, WatchRunAlways, WatchRunChangedOrFollowups}, out.RunWhen) {
		c.addf("run_when", "must be %q, %q or %q", WatchRunChanged, WatchRunAlways, WatchRunChangedOrFollowups)
	}
	if out.DryRun == nil {
		out.DryRun = ptrBool(true)
	}
	switch {
	case in.MaxItems == 0:
		out.MaxItems = DefaultWatchMaxItems
	case in.MaxItems < 1 || in.MaxItems > MaxWatchMaxItems:
		c.addf("max_items", "must be between 1 and %d", MaxWatchMaxItems)
	}
	out.MentionCapPerDay = defaultedRange(&c, "mention_cap_per_day", in.MentionCapPerDay, DefaultMentionCapPerDay, MaxMentionCapPerDay)
	out.CommentCapPerRun = defaultedRange(&c, "comment_cap_per_run", in.CommentCapPerRun, DefaultCommentCapPerRun, MaxCommentCapPerRun)
	if in.Followups != nil {
		if known && !kind.Followups {
			c.addf("followups", "a %s source has no items with dates, so it cannot have a follow-up ladder", kind.Kind)
		} else {
			out.Followups = normalizeLadder(&c, *in.Followups, out.TrackedFields)
		}
	}
	return out, c.err()
}

func ptrBool(v bool) *bool { return &v }

func connectionLabel(kind string) string {
	if kind == ConnectionTracker {
		return "tracker (Jira, Linear or KM2 issues)"
	}
	return kind
}

// defaultedRange is value, or def when value is 0, reporting values outside 1..maxValue.
func defaultedRange(c *checker, field string, value, def, maxValue int) int {
	if value == 0 {
		return def
	}
	if value < 1 || value > maxValue {
		c.addf(field, "must be between 1 and %d", maxValue)
	}
	return value
}

// normalizeFixedFields checks the fields of a source whose items always have the same fields (trackers): the id
// field is fixed and tracked fields come from the list. No fields means the default set.
func normalizeFixedFields(c *checker, idField string, tracked, allowed []string) (string, []string) {
	if id := strings.TrimSpace(idField); id != "" && id != TrackerIDField {
		c.addf("id_field", "tracker items are identified by %q", TrackerIDField)
	}
	out := make([]string, 0, len(tracked))
	for _, f := range tracked {
		f = strings.TrimSpace(f)
		switch {
		case f == "" || slices.Contains(out, f):
		case !slices.Contains(allowed, f):
			c.addf("tracked_fields", "%q is not a tracker field; use %s", f, strings.Join(allowed, ", "))
		default:
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		out = slices.Clone(DefaultTrackerFields)
	}
	return TrackerIDField, out
}

func normalizeItemFields(c *checker, idField string, tracked []string, csv bool) (string, []string) {
	check := checkFieldPath
	clean := CleanFieldPath
	if csv {
		check, clean = checkColumn, strings.TrimSpace
	}
	id := clean(idField)
	if id == "" {
		c.addf("id_field", "is required: the field that identifies each item, such as key or id")
	} else if problem := check(id); problem != "" {
		c.addf("id_field", "%s", problem)
	}
	out := make([]string, 0, len(tracked))
	for _, f := range tracked {
		f = clean(f)
		switch {
		case f == "" || slices.Contains(out, f):
			continue
		case check(f) != "":
			c.addf("tracked_fields", "%q: %s", f, check(f))
		default:
			out = append(out, f)
		}
	}
	switch {
	case len(out) == 0:
		c.addf("tracked_fields", "list at least one field to compare, such as status or due_date")
	case len(out) > maxTrackedFields:
		c.addf("tracked_fields", "at most %d fields", maxTrackedFields)
	}
	return id, out
}

func normalizeSource(c *checker, in WatchSource) WatchSource {
	out := WatchSource{Kind: strings.TrimSpace(in.Kind)}
	kind := out.Kind
	only := func(field, value, forKind string) {
		if strings.TrimSpace(value) != "" && kind != forKind {
			c.addf("source."+field, "only applies to %s sources", forKind)
		}
	}
	only("path", in.Path, WatchSourceHTTPJSON)
	only("url", in.URL, WatchSourceWebPage)
	only("selector", in.Selector, WatchSourceWebPage)
	only("file", in.File, WatchSourceWorkspaceFile)
	only("format", in.Format, WatchSourceWorkspaceFile)
	only("project", in.Project, WatchSourceTracker)
	only("jql", in.JQL, WatchSourceTracker)
	only("lead", in.Lead, WatchSourceTracker)
	if in.IncludeDone && kind != WatchSourceTracker {
		c.addf("source.include_done", "only applies to %s sources", WatchSourceTracker)
	}
	tracker := kind == WatchSourceTracker || kind == WatchSourceTrackingList
	if (in.Comments || in.StaleDays != 0) && !tracker {
		c.addf("source.comments", "comments and stale_days only apply to tracker and tracking-list sources")
	}
	switch kind {
	case WatchSourceHTTPJSON:
		out.Path = strings.TrimSpace(in.Path)
		if problem := CheckRequestPath(out.Path); problem != "" {
			c.addf("source.path", "%s", problem)
		}
		out.ItemsPath = cleanItemsPath(c, in.ItemsPath)
	case WatchSourceWebPage:
		out.URL = strings.TrimSpace(in.URL)
		if problem := checkPageURL(out.URL); problem != "" {
			c.addf("source.url", "%s", problem)
		}
		out.Selector = strings.TrimSpace(in.Selector)
		if len(out.Selector) > maxSelectorLen {
			c.addf("source.selector", "must be at most %d characters", maxSelectorLen)
		}
		if strings.TrimSpace(in.ItemsPath) != "" {
			c.addf("source.items_path", "does not apply to web pages")
		}
	case WatchSourceWorkspaceFile:
		out.File, out.Format = normalizeWorkspaceFile(c, in.File, in.Format)
		if out.Format == "csv" && strings.TrimSpace(in.ItemsPath) != "" {
			c.addf("source.items_path", "does not apply to CSV files (every row is an item)")
		} else {
			out.ItemsPath = cleanItemsPath(c, in.ItemsPath)
		}
	case WatchSourceTracker:
		normalizeTrackerSource(c, in, &out)
	case WatchSourceTrackingList:
		out.Comments = in.Comments
		out.StaleDays = defaultedRange(c, "source.stale_days", in.StaleDays, DefaultStaleDays, MaxStaleDays)
	}
	if tracker && strings.TrimSpace(in.ItemsPath) != "" {
		c.addf("source.items_path", "does not apply to tracker sources")
	}
	return out
}

const maxJQLLen = 1_000

func normalizeTrackerSource(c *checker, in WatchSource, out *WatchSource) {
	project, ok := NormalizeProjectKey(in.Project)
	if !ok {
		c.addf("source.project", "is required: the Jira project key, Linear team key or KM2 project key, such as PRJ")
	}
	out.Project = project
	out.JQL = strings.TrimSpace(in.JQL)
	switch {
	case len(out.JQL) > maxJQLLen:
		c.addf("source.jql", "must be at most %d characters", maxJQLLen)
	case strings.IndexFunc(out.JQL, unicode.IsControl) >= 0:
		c.addf("source.jql", "must be one line")
	case hasOrderBy(out.JQL):
		c.addf("source.jql", "must not contain ORDER BY (Studio orders the results itself)")
	case !balancedJQL(out.JQL):
		c.addf("source.jql", "must have balanced parentheses and quotes (it is ANDed with the project and cannot widen it)")
	}
	out.Lead = strings.TrimSpace(in.Lead)
	if out.Lead != "" && !ValidAccountID(out.Lead) {
		c.addf("source.lead", "must be a tracker account id")
	}
	out.IncludeDone, out.Comments = in.IncludeDone, in.Comments
	out.StaleDays = defaultedRange(c, "source.stale_days", in.StaleDays, DefaultStaleDays, MaxStaleDays)
}

// balancedJQL reports whether every parenthesis outside quoted strings closes in order and every quote closes, so
// the clause stays one group inside "project = X AND (...)" and cannot break out with ") OR (".
func balancedJQL(jql string) bool {
	depth := 0
	var quote rune
	escaped := false
	for _, r := range jql {
		switch {
		case escaped:
			escaped = false
		case quote != 0 && r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			if depth--; depth < 0 {
				return false
			}
		}
	}
	return depth == 0 && quote == 0
}

func hasOrderBy(jql string) bool {
	return strings.Contains(strings.Join(strings.Fields(strings.ToLower(jql)), " "), "order by")
}

func cleanItemsPath(c *checker, p string) string {
	p = CleanFieldPath(p)
	if p != "" {
		if problem := checkFieldPath(p); problem != "" {
			c.addf("source.items_path", "%s", problem)
		}
	}
	return p
}

// CheckRequestPath allows only a path (and query) under a connection's base URL: never another host, and never
// "." or ".." segments (plain or percent-encoded) that would climb out of the base URL's path.
func CheckRequestPath(p string) string {
	switch {
	case p == "":
		return "is required, such as /issues?state=open"
	case len(p) > maxSourcePathLen:
		return fmt.Sprintf("must be at most %d characters", maxSourcePathLen)
	case !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//"):
		return "must start with a single / (it is added to the connection's base URL)"
	case strings.ContainsAny(p, " \t\r\n#\\") || strings.IndexFunc(p, unicode.IsControl) >= 0:
		return "must not contain spaces, control characters, '\\' or '#'"
	case strings.Contains(p, "://"):
		return "must be a path, not a full URL"
	}
	if _, err := url.Parse(p); err != nil {
		return "is not a valid URL path"
	}
	return checkPathSegments(strings.SplitN(p, "?", 2)[0])
}

func checkPathSegments(pathPart string) string {
	lower := strings.ToLower(pathPart)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "must not contain encoded '/' or '\\'"
	}
	decoded, err := url.PathUnescape(pathPart)
	if err != nil {
		return "is not a valid URL path"
	}
	for _, seg := range strings.Split(decoded, "/") {
		if seg == "." || seg == ".." {
			return "must not contain '.' or '..' segments"
		}
	}
	return ""
}

func checkPageURL(raw string) string {
	if raw == "" {
		return "is required"
	}
	if len(raw) > maxSourcePathLen {
		return fmt.Sprintf("must be at most %d characters", maxSourcePathLen)
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https"):
		return "must be an absolute http(s) URL"
	case u.User != nil:
		return "must not contain credentials"
	case strings.IndexFunc(raw, unicode.IsSpace) >= 0:
		return "must not contain spaces"
	}
	return ""
}

func normalizeWorkspaceFile(c *checker, file, format string) (string, string) {
	file = strings.TrimSpace(file)
	format = strings.ToLower(strings.TrimSpace(format))
	clean := path.Clean(strings.ReplaceAll(file, `\`, "/"))
	switch {
	case file == "":
		c.addf("source.file", "is required: a path in the workspace's %s/ folder, such as %s/courses.csv", WatchFilesDir, WatchFilesDir)
		return "", format
	case len(file) > maxWorkspacePathLn || strings.IndexFunc(file, unicode.IsControl) >= 0:
		c.addf("source.file", "must be at most %d characters without control characters", maxWorkspacePathLn)
	case path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../"):
		c.addf("source.file", "must be a path inside the Studio workspace")
	case !strings.HasPrefix(clean, WatchFilesDir+"/"):
		c.addf("source.file", "must be inside the workspace's %s/ folder, such as %s/courses.csv", WatchFilesDir, WatchFilesDir)
	}
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(path.Ext(clean)), ".")
	}
	if format != "csv" && format != "json" {
		c.addf("source.format", "must be csv or json (or use a .csv or .json file)")
	}
	return clean, format
}

func normalizeLadder(c *checker, in FollowupLadder, tracked []string) *FollowupLadder {
	out := FollowupLadder{DateField: CleanFieldPath(in.DateField), Timezone: strings.TrimSpace(in.Timezone), MaxPerTick: in.MaxPerTick}
	if !slices.Contains(tracked, out.DateField) {
		c.addf("followups.date_field", "must be one of the tracked fields (the date each step counts from)")
	}
	if out.Timezone != "" {
		if _, err := time.LoadLocation(out.Timezone); err != nil {
			c.addf("followups.timezone", "%q is not a known time zone", out.Timezone)
		}
	}
	switch {
	case out.MaxPerTick == 0:
		out.MaxPerTick = DefaultFollowupsPerTick
	case out.MaxPerTick < 1 || out.MaxPerTick > MaxFollowupsPerTick:
		c.addf("followups.max_per_tick", "must be between 1 and %d", MaxFollowupsPerTick)
	}
	out.SkipWhen = normalizeSkipWhen(c, in.SkipWhen, tracked)
	out.Steps = normalizeSteps(c, in.Steps)
	for _, d := range in.QuietDays {
		name, ok := weekdayNames[strings.ToLower(strings.TrimSpace(d))]
		switch {
		case !ok:
			c.addf("followups.quiet_days", "%q is not a day of the week", d)
		case !slices.Contains(out.QuietDays, name):
			out.QuietDays = append(out.QuietDays, name)
		}
	}
	if len(out.QuietDays) == 7 {
		c.addf("followups.quiet_days", "cannot be every day of the week")
	}
	return &out
}

func normalizeSkipWhen(c *checker, in map[string][]string, tracked []string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for field, values := range in {
		field = CleanFieldPath(field)
		if !slices.Contains(tracked, field) {
			c.addf("followups.skip_when", "%q must be one of the tracked fields", field)
			continue
		}
		if len(values) > maxSkipValues {
			c.addf("followups.skip_when", "%q: at most %d values", field, maxSkipValues)
		}
		clean := make([]string, 0, len(values))
		for _, v := range values {
			if v = strings.TrimSpace(v); v != "" && len(v) <= maxNameLen {
				clean = append(clean, v)
			}
		}
		out[field] = clean
	}
	return out
}

func normalizeSteps(c *checker, in []FollowupStep) []FollowupStep {
	if len(in) == 0 || len(in) > maxLadderSteps {
		c.addf("followups.steps", "list between 1 and %d steps", maxLadderSteps)
	}
	out := make([]FollowupStep, 0, len(in))
	seen := map[string]bool{}
	for i, s := range in {
		field := fmt.Sprintf("followups.steps[%d]", i)
		step := FollowupStep{Name: strings.TrimSpace(s.Name), Offset: strings.TrimSpace(s.Offset),
			Notify: strings.TrimSpace(s.Notify), Repeat: strings.TrimSpace(s.Repeat)}
		if !stepNamePattern.MatchString(step.Name) {
			c.addf(field+".name", "use lowercase letters, digits, '_' or '-' (at most 50), such as due_soon")
		} else if seen[step.Name] {
			c.addf(field+".name", "%q is used more than once", step.Name)
		}
		seen[step.Name] = true
		if _, err := ParseDayOffset(step.Offset); err != nil {
			c.addf(field+".offset", "%v", err)
		}
		c.text(field+".notify", step.Notify, 1, maxNotifyLen)
		if step.Repeat != "" {
			if _, err := ParseRepeatDays(step.Repeat); err != nil {
				c.addf(field+".repeat", "%v", err)
			}
		}
		out = append(out, step)
	}
	return out
}
