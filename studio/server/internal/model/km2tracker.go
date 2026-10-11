package model

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// KM2 issues as a tracker (docs/trackers.md, "KM2 issues"): a km2_tracker connection reads the custom entities
// <prefix>person, project, cycle, issue and comment of one KM2 organisation through KM2's v1 records API, with an
// org-wide key. Studio only ever creates comments (and, when allowed, issues) there; it never edits a record.

// TrackerKM2 is the connection kind of a KM2 organisation used as a tracker.
const TrackerKM2 = "km2_tracker"

// Settings of a km2_tracker connection.
const (
	ConfigSelfEmail        = "self_email"
	ConfigEntityPrefix     = "entity_prefix"
	ConfigActivityField    = "activity_field"
	ConfigIssueURLTemplate = "issue_url_template"

	DefaultEntityPrefix  = "trk_"
	DefaultActivityField = "updated_at"
	// KM2KeyPrefix starts every KM2 API key
	KM2KeyPrefix = "km2_"
)

var (
	// KM2 entity slugs are ^[a-z][a-z0-9_]*$ of at most 63 characters; the prefix leaves room for "comment"
	entityPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)
	fieldSlugPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
)

// TrackerHasProjectLeads reports whether a tracker kind gives each project a lead (Jira projects, KM2 trk_project
// records); Linear teams have none.
func TrackerHasProjectLeads(kind string) bool { return kind == TrackerJira || kind == TrackerKM2 }

// TrackerCommentMovesUpdated reports whether a new comment moves its issue's updated time (Jira), so comments need
// only be read for issues updated since the last check. Linear and KM2 issues keep their time.
func TrackerCommentMovesUpdated(kind string) bool { return kind == TrackerJira }

// KM2TrackerSettings are a km2_tracker connection's settings with their defaults.
type KM2TrackerSettings struct {
	BaseURL       string
	SelfEmail     string
	EntityPrefix  string
	ActivityField string
	// IssueURLTemplate links an issue: {id} is its record id, {key} its identifier such as ERP-12
	IssueURLTemplate string
}

// KM2TrackerSettingsOf reads a km2_tracker connection's settings, filling in the defaults.
func KM2TrackerSettingsOf(config map[string]string) KM2TrackerSettings {
	s := KM2TrackerSettings{BaseURL: strings.TrimRight(config["base_url"], "/"), SelfEmail: strings.ToLower(strings.TrimSpace(config[ConfigSelfEmail])),
		EntityPrefix: config[ConfigEntityPrefix], ActivityField: config[ConfigActivityField], IssueURLTemplate: config[ConfigIssueURLTemplate]}
	if s.EntityPrefix == "" {
		s.EntityPrefix = DefaultEntityPrefix
	}
	if s.ActivityField == "" {
		s.ActivityField = DefaultActivityField
	}
	return s
}

// Entity is the slug of one tracker entity, such as trk_issue for "issue".
func (s KM2TrackerSettings) Entity(name string) string { return s.EntityPrefix + name }

// IssueURL is an issue's link from the template, or "" without one. A placeholder in the query or fragment is
// query-escaped, one in the path path-escaped; the template keeps placeholders out of the scheme and host.
func (s KM2TrackerSettings) IssueURL(id, key string) string {
	tmpl := s.IssueURLTemplate
	if tmpl == "" || placeholderBeforePath(tmpl) {
		return ""
	}
	query := strings.IndexAny(tmpl, "?#")
	var b strings.Builder
	for i := 0; i < len(tmpl); {
		value, name, found := "", "", false
		for _, p := range []struct{ name, value string }{{"{id}", id}, {"{key}", key}} {
			if strings.HasPrefix(tmpl[i:], p.name) {
				value, name, found = p.value, p.name, true
			}
		}
		switch {
		case !found:
			b.WriteByte(tmpl[i])
			i++
			continue
		case query >= 0 && i > query:
			b.WriteString(url.QueryEscape(value))
		default:
			b.WriteString(url.PathEscape(value))
		}
		i += len(name)
	}
	return b.String()
}

// placeholderBeforePath reports whether {id} or {key} appears in a template's scheme or authority, that is before
// the first '/', '?' or '#' after "://", where it could change the host a link points to.
func placeholderBeforePath(tmpl string) bool {
	start := strings.Index(tmpl, "://")
	if start < 0 {
		return true
	}
	end := len(tmpl)
	if i := strings.IndexAny(tmpl[start+3:], "/?#"); i >= 0 {
		end = start + 3 + i
	}
	head := tmpl[:end]
	return strings.Contains(head, "{id}") || strings.Contains(head, "{key}")
}

// checkKM2TrackerConfig adds the problems of a km2_tracker connection's settings beyond their field types: the key
// may only go to an https address (plain http only to this machine), and the entity prefix and activity field are
// slugs, since they end up in KM2 request paths and filters.
func checkKM2TrackerConfig(c *checker, config map[string]string) {
	if base := config["base_url"]; base != "" {
		if u, err := url.Parse(base); err == nil && u.Scheme != "https" && !loopbackHTTP(u) {
			c.addf("config.base_url", "must be https (plain http only to this machine)")
		}
	}
	if p := config[ConfigEntityPrefix]; p != "" && !entityPrefixPattern.MatchString(p) {
		c.addf("config."+ConfigEntityPrefix, "must be lower-case letters, digits and underscores, starting with a letter, such as trk_")
	}
	if f := config[ConfigActivityField]; f != "" && !fieldSlugPattern.MatchString(f) {
		c.addf("config."+ConfigActivityField, "must be a KM2 field slug, such as updated_at or activity_at")
	}
	if tmpl := config[ConfigIssueURLTemplate]; tmpl != "" {
		u, err := url.Parse(strings.NewReplacer("{id}", "x", "{key}", "x").Replace(tmpl))
		switch {
		case err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil:
			c.addf("config."+ConfigIssueURLTemplate, "must be an http or https address")
		case !strings.Contains(tmpl, "{id}") && !strings.Contains(tmpl, "{key}"):
			c.addf("config."+ConfigIssueURLTemplate, "must contain {id} or {key}")
		case placeholderBeforePath(tmpl):
			c.addf("config."+ConfigIssueURLTemplate, "must keep {id} and {key} out of the scheme and host (put them in the path or query)")
		}
	}
}

// LoopbackHTTP reports whether u is a plain-http URL to this machine (localhost or a loopback address).
func LoopbackHTTP(u *url.URL) bool { return loopbackHTTP(u) }

// CheckKM2TrackerNames checks the entity prefix and activity field, which end up in KM2 request paths and filters.
func CheckKM2TrackerNames(s KM2TrackerSettings) error {
	if !entityPrefixPattern.MatchString(s.EntityPrefix) {
		return errors.New("the entity prefix must be lower-case letters, digits and underscores, such as trk_")
	}
	if !fieldSlugPattern.MatchString(s.ActivityField) {
		return errors.New("the activity field must be a KM2 field slug, such as updated_at")
	}
	return nil
}
