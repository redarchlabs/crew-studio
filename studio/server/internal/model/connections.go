package model

import (
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Connections hold credentials for outside systems (trackers, KM2, chat, email, plain HTTP APIs). Settings are
// stored as given; secret fields are encrypted and can only ever be replaced, never read back.

const (
	maxConfigValueLen = 2_000
	maxSecretLen      = 8_192
)

// Field input kinds, which also drive the form in the web UI.
const (
	FieldText   = "text"
	FieldURL    = "url"
	FieldEmail  = "email"
	FieldPort   = "port"
	FieldHeader = "header"
	// FieldHTTPSURL is a secret URL (such as a webhook) that must use https (plain http only to this machine, for tests)
	FieldHTTPSURL = "https_url"
	// FieldToggle is an on/off setting, stored as "true" when on and left out when off
	FieldToggle = "toggle"
	// FieldCloudID is an Atlassian cloud id (a UUID)
	FieldCloudID = "cloud_id"
	// FieldSelect is one of the field's Options ("" when left out)
	FieldSelect = "select"
)

// Email connection settings read by the SMTP client.
const (
	ConfigSMTPSecurity = "security"
	SMTPStartTLS       = "starttls"
	SMTPTLS            = "tls"
	// SMTPNone sends in clear text; only allowed to a relay on this machine
	SMTPNone = "none"
)

// Tracker connection settings read by the tracker clients.
const (
	ConfigAllowCreate = "allow_create"
	ConfigIssueType   = "issue_type"
	ConfigCloudID     = "cloud_id"
)

const allowCreateHint = "Off by default. When on, crews' Create Issue tool may open new issues through this connection; " +
	"otherwise they only propose new issues in their reports."

// ConnectionField is one setting of a connection kind.
type ConnectionField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Input    string `json:"input"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
	Hint     string `json:"hint,omitempty"`
	// Options are the choices of a select field
	Options []string `json:"options,omitempty"`
	// Prefix, when set, must start the value (KM2 keys start with km2_)
	Prefix string `json:"prefix,omitempty"`
}

// ConnectionKind describes what a kind of connection needs.
type ConnectionKind struct {
	Kind   string            `json:"kind"`
	Label  string            `json:"label"`
	Fields []ConnectionField `json:"fields"`
	// AnySecret lists secrets of which at least one must be set (Slack: a webhook or a bot token)
	AnySecret []string `json:"any_secret,omitempty"`
	// Testable is true when "Test connection" can reach the system today
	Testable bool `json:"testable"`
}

// ConnectionKinds lists every kind in display order.
var ConnectionKinds = []ConnectionKind{
	{Kind: "jira", Label: "Jira Cloud", Testable: true, Fields: []ConnectionField{
		{Key: "site_url", Label: "Site URL", Input: FieldURL, Required: true, Hint: "Such as https://acme.atlassian.net"},
		{Key: "email", Label: "Account email", Input: FieldEmail, Required: true},
		{Key: ConfigCloudID, Label: "Cloud ID", Input: FieldCloudID,
			Hint: "Only for scoped API tokens, which must call api.atlassian.com. Find it at <site>/_edge/tenant_info. Leave empty for a classic token."},
		{Key: ConfigIssueType, Label: "Issue type for new issues", Input: FieldText, Hint: "Used by the Create Issue tool. Default: Task."},
		{Key: ConfigAllowCreate, Label: "Let crews create issues", Input: FieldToggle, Hint: allowCreateHint},
		{Key: "api_token", Label: "API token", Input: FieldText, Secret: true, Required: true},
	}},
	{Kind: "linear", Label: "Linear", Testable: true, Fields: []ConnectionField{
		{Key: ConfigAllowCreate, Label: "Let crews create issues", Input: FieldToggle, Hint: allowCreateHint},
		{Key: "api_key", Label: "API key", Input: FieldText, Secret: true, Required: true, Hint: "A personal API key. The workspace is implied by the key."},
	}},
	{Kind: TrackerKM2, Label: "KM2 issues (tracker)", Testable: true, Fields: []ConnectionField{
		{Key: "base_url", Label: "Base URL", Input: FieldURL, Required: true,
			Hint: "The KM2 API address, such as https://km2.example.com (https; plain http only to this machine)."},
		{Key: ConfigSelfEmail, Label: "Crew Studio's person email", Input: FieldEmail, Required: true,
			Hint: "The email of the person record (trk_person) that stands for Crew Studio: comments are posted as it, and its own comments are left out of watches."},
		{Key: ConfigEntityPrefix, Label: "Entity prefix", Input: FieldText,
			Hint: "The slug prefix of the tracker entities (trk_person, trk_project, trk_issue, trk_comment). Default: trk_."},
		{Key: ConfigActivityField, Label: "Activity field", Input: FieldText,
			Hint: "The issue field that says when an issue last changed, for stale checks. Default: updated_at (a simulator may keep its own, such as activity_at)."},
		{Key: ConfigIssueURLTemplate, Label: "Issue link", Input: FieldText,
			Hint: "Optional link to an issue in KM2, with {id} for the record id or {key} for its key, such as https://km2.example.com/records/trk_issue?record_id={id}."},
		{Key: ConfigAllowCreate, Label: "Let crews create issues", Input: FieldToggle, Hint: allowCreateHint},
		{Key: "api_key", Label: "Org API key", Input: FieldText, Secret: true, Required: true, Prefix: KM2KeyPrefix,
			Hint: "An org-wide km2_ key (not a profile key) with entities:read, records:read and records:write."},
	}},
	{Kind: KindKM2, Label: "KM2 (Knowledge Manager)", Testable: true, Fields: []ConnectionField{
		{Key: "base_url", Label: "Base URL", Input: FieldURL, Required: true, Hint: "The KM2 API address, such as https://km2.example.com."},
		{Key: ConfigKM2DefaultFolder, Label: "Default folder for saves", Input: FieldUUID,
			Hint: "The KM2 folder id crews save documents to when they name no folder. Without it crews must name one of the other folders; with neither, saving is off."},
		{Key: ConfigKM2WriteFolders, Label: "Other folders crews may save to", Input: FieldUUIDList,
			Hint: "Optional KM2 folder ids, separated by commas. Crews may name only these and the default folder."},
		{Key: "api_key", Label: "Profile API key", Input: FieldText, Secret: true, Required: true,
			Hint: "A km2_ key bound to a non-admin KM2 profile for this connection's functional areas, with search:read and knowledge:write (and knowledge:read for Test)."},
	}},
	{Kind: "http", Label: "HTTP (JSON API)", Testable: true, Fields: []ConnectionField{
		{Key: "base_url", Label: "Base URL", Input: FieldURL, Required: true},
		{Key: "auth_header", Label: "Auth header name", Input: FieldHeader, Hint: "Such as Authorization or X-API-Key. Leave empty for none."},
		{Key: "auth_value", Label: "Auth header value", Input: FieldText, Secret: true},
	}},
	{Kind: KindMCP, Label: "MCP server (tools for agent chat)", Testable: true, Fields: []ConnectionField{
		{Key: ConfigMCPURL, Label: "Server URL", Input: FieldURL, Required: true,
			Hint: "The Streamable HTTP address of an outside MCP server, such as https://mcp.example.com/mcp (https; plain http only to this machine)."},
		{Key: "auth_header", Label: "Auth header name", Input: FieldHeader, Hint: "Such as Authorization or X-API-Key. Leave empty for none."},
		{Key: "auth_value", Label: "Auth header value", Input: FieldText, Secret: true,
			Hint: "Such as Bearer <token>. Crew Studio sends it to the server itself; agents and runs never see it."},
	}},
	{Kind: "slack", Label: "Slack", Testable: true, AnySecret: []string{"webhook_url", "bot_token"}, Fields: []ConnectionField{
		{Key: "channel", Label: "Channel", Input: FieldText,
			Hint: "Needed with a bot token: a channel ID such as C0123456789 (or #pmo-reports). Invite the bot to the channel. A webhook posts to its own channel."},
		{Key: "webhook_url", Label: "Incoming webhook URL", Input: FieldHTTPSURL, Secret: true,
			Hint: "An incoming webhook of a Slack app, https://hooks.slack.com/services/... Give this or a bot token."},
		{Key: "bot_token", Label: "Bot token", Input: FieldText, Secret: true, Hint: "A bot token (xoxb-...) with the chat:write scope."},
	}},
	{Kind: "teams", Label: "Microsoft Teams", Testable: true, Fields: []ConnectionField{
		{Key: "post_url", Label: "Workflows webhook URL", Input: FieldHTTPSURL, Secret: true, Required: true,
			Hint: "The URL of a Teams workflow made from the \"Send webhook alerts to a channel\" template (trigger: When a Teams webhook request is received, who can trigger: Anyone). Old Office 365 connector URLs no longer work."},
	}},
	{Kind: "email", Label: "Email (SMTP)", Testable: true, Fields: []ConnectionField{
		{Key: "host", Label: "SMTP host", Input: FieldText, Required: true},
		{Key: "port", Label: "Port", Input: FieldPort, Required: true, Hint: "Usually 587 (STARTTLS) or 465 (TLS)."},
		{Key: ConfigSMTPSecurity, Label: "Security", Input: FieldSelect, Options: []string{SMTPStartTLS, SMTPTLS, SMTPNone},
			Hint: "starttls (the default) or tls. none sends in clear text and only works with a relay on this machine."},
		{Key: "username", Label: "User name", Input: FieldText},
		{Key: "from_address", Label: "From address", Input: FieldEmail, Required: true},
		{Key: ConfigEmailDomains, Label: "Allowed recipient domains", Input: FieldText,
			Hint: "Optional: reports may only be emailed to these domains, separated by commas, such as example.com, example.org."},
		{Key: "password", Label: "Password", Input: FieldText, Secret: true},
	}},
}

// ConnectionKindOf returns the description of a kind.
func ConnectionKindOf(kind string) (ConnectionKind, bool) {
	i := slices.IndexFunc(ConnectionKinds, func(k ConnectionKind) bool { return k.Kind == kind })
	if i < 0 {
		return ConnectionKind{}, false
	}
	return ConnectionKinds[i], true
}

func (k ConnectionKind) field(key string) (ConnectionField, bool) {
	i := slices.IndexFunc(k.Fields, func(f ConnectionField) bool { return f.Key == key })
	if i < 0 {
		return ConnectionField{}, false
	}
	return k.Fields[i], true
}

// Connection is a connection as the API shows it: settings and which secrets are set, never their values.
type Connection struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Config map[string]string `json:"config"`
	// SecretKeys are the secret fields that are set
	SecretKeys []string `json:"secret_keys"`
	// MissingSecrets are required secret fields that are not set yet
	MissingSecrets  []string   `json:"missing_secrets"`
	SecretUpdatedAt *time.Time `json:"secret_updated_at"`
	AreaIDs         []string   `json:"area_ids"`
	Enabled         bool       `json:"enabled"`
	// CrewCount is how many crews may use the connection
	CrewCount int       `json:"crew_count"`
	CreatedBy *string   `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConnectionInput creates or changes a connection. Secrets are only accepted when creating; afterwards they are
// replaced through their own endpoint.
type ConnectionInput struct {
	Name    string            `json:"name"`
	Kind    string            `json:"kind"`
	Config  map[string]string `json:"config"`
	AreaIDs []string          `json:"area_ids"`
	// Enabled defaults to true
	Enabled *bool             `json:"enabled"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,100}$")

func kindNames() string {
	names := make([]string, len(ConnectionKinds))
	for i, k := range ConnectionKinds {
		names[i] = k.Kind
	}
	return strings.Join(names, ", ")
}

// NormalizeConnection validates a connection and returns a cleaned copy (the input is not changed).
func NormalizeConnection(in ConnectionInput) (ConnectionInput, error) {
	out := ConnectionInput{Name: strings.TrimSpace(in.Name), Kind: strings.TrimSpace(in.Kind), Config: map[string]string{}}
	enabled := in.Enabled == nil || *in.Enabled
	out.Enabled = &enabled
	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	out.AreaIDs = c.uuidList("area_ids", in.AreaIDs, "")
	if len(in.AreaIDs) == 0 {
		c.addf("area_ids", "pick at least one functional area")
	}
	kind, ok := ConnectionKindOf(out.Kind)
	if !ok {
		c.addf("kind", "must be one of %s", kindNames())
		return out, c.err()
	}
	out.Config = normalizeConfig(&c, kind, in.Config)
	if kind.Kind == TrackerKM2 {
		if e := out.Config[ConfigSelfEmail]; e != "" {
			out.Config[ConfigSelfEmail] = strings.ToLower(e)
		}
		checkKM2TrackerConfig(&c, out.Config)
	}
	if in.Secrets != nil {
		secrets, err := NormalizeSecrets(kind.Kind, in.Secrets)
		if err != nil {
			c.details = append(c.details, err.(*ValidationError).Details...)
		}
		out.Secrets = secrets
	}
	return out, c.err()
}

func normalizeConfig(c *checker, kind ConnectionKind, config map[string]string) map[string]string {
	out := map[string]string{}
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		f, known := kind.field(key)
		switch {
		case !known:
			c.addf("config", "unknown setting %q for %s connections", key, kind.Kind)
		case f.Secret:
			c.addf("config", "%q is a secret; send it under secrets", key)
		default:
			if v := cleanField(c, "config."+key, f, config[key], maxConfigValueLen); v != "" {
				out[key] = v
			}
		}
	}
	for _, f := range kind.Fields {
		if !f.Secret && f.Required && out[f.Key] == "" {
			c.addf("config."+f.Key, "is required")
		}
	}
	return out
}

// NormalizeSecrets validates a full set of secret fields for a kind and returns a cleaned copy.
func NormalizeSecrets(kindName string, secrets map[string]string) (map[string]string, error) {
	var c checker
	kind, ok := ConnectionKindOf(kindName)
	if !ok {
		c.addf("kind", "must be one of %s", kindNames())
		return nil, c.err()
	}
	if len(secrets) == 0 {
		c.addf("secrets", "give at least one secret")
	}
	out := map[string]string{}
	for key, value := range secrets {
		f, known := kind.field(key)
		switch {
		case !known:
			c.addf("secrets", "unknown secret %q for %s connections", key, kind.Kind)
		case !f.Secret:
			c.addf("secrets", "%q is not a secret; send it under config", key)
		default:
			field := "secrets." + key
			if strings.TrimSpace(value) == "" {
				c.addf(field, "must not be empty")
				continue
			}
			v := cleanValue(&c, field, f.Input, value, maxSecretLen)
			if f.Prefix != "" && v != "" && !strings.HasPrefix(v, f.Prefix) {
				c.addf(field, "must start with %s", f.Prefix)
				continue
			}
			if v != "" {
				out[key] = v
			}
		}
	}
	slices.Sort(c.details)
	return out, c.err()
}

// cleanField is cleanValue for a field with choices.
func cleanField(c *checker, field string, f ConnectionField, raw string, maxLen int) string {
	if f.Input != FieldSelect {
		return cleanValue(c, field, f.Input, raw, maxLen)
	}
	v := strings.ToLower(strings.TrimSpace(raw))
	if v != "" && !slices.Contains(f.Options, v) {
		c.addf(field, "must be one of %s", strings.Join(f.Options, ", "))
		return ""
	}
	return v
}

// cleanValue trims and checks one setting or secret. Values are single lines, so they are safe in headers.
func cleanValue(c *checker, field, input, raw string, maxLen int) string {
	v := strings.TrimSpace(raw)
	if len(v) > maxLen {
		c.addf(field, "must be at most %d characters", maxLen)
		return ""
	}
	if strings.ContainsAny(v, "\r\n\x00") {
		c.addf(field, "must be one line")
		return ""
	}
	switch input {
	case FieldURL, FieldHTTPSURL:
		v = strings.TrimRight(v, "/")
		checkURL(c, field, v, input == FieldHTTPSURL)
	case FieldEmail:
		if addr, err := mail.ParseAddress(v); err != nil || addr.Address != v {
			c.addf(field, "must be an email address")
		}
	case FieldPort:
		if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 65535 {
			c.addf(field, "must be a port number (1 to 65535)")
		}
	case FieldHeader:
		if v != "" && !headerNamePattern.MatchString(v) {
			c.addf(field, "must be an HTTP header name, such as Authorization")
		}
	case FieldToggle:
		switch strings.ToLower(v) {
		case "true":
			return "true"
		case "", "false":
			return ""
		}
		c.addf(field, "must be true or false")
	case FieldCloudID:
		if v != "" && !uuidPattern.MatchString(v) {
			c.addf(field, "must be an Atlassian cloud id (a UUID)")
		}
		return strings.ToLower(v)
	case FieldUUID, FieldUUIDList:
		return cleanFolderIDs(c, field, input, v)
	}
	return v
}

func checkURL(c *checker, field, v string, httpsOnly bool) {
	u, err := url.Parse(v)
	switch {
	case err != nil || u.Host == "" || (u.Scheme != "https" && (httpsOnly && !loopbackHTTP(u) || u.Scheme != "http")):
		if httpsOnly {
			c.addf(field, "must be an https URL")
		} else {
			c.addf(field, "must be an http or https URL")
		}
	case u.User != nil:
		c.addf(field, "must not contain a user name or password")
	case !httpsOnly && (u.RawQuery != "" || u.Fragment != ""):
		// Settings are shown to everyone who can view the connection; keys in a query string would be too
		c.addf(field, "must not contain a query string or fragment; put credentials in the secret")
	}
}

// MissingSecrets lists the required secret fields of a kind that are not among set (never nil).
func MissingSecrets(kindName string, set []string) []string {
	out := []string{}
	kind, ok := ConnectionKindOf(kindName)
	if !ok {
		return out
	}
	for _, f := range kind.Fields {
		if f.Secret && f.Required && !slices.Contains(set, f.Key) {
			out = append(out, f.Key)
		}
	}
	if len(kind.AnySecret) > 0 && !slices.ContainsFunc(kind.AnySecret, func(k string) bool { return slices.Contains(set, k) }) {
		out = append(out, strings.Join(kind.AnySecret, " or "))
	}
	return out
}

// destinationKeys are settings that decide where secrets are sent, besides URL fields.
var destinationKeys = map[string]bool{"host": true, "port": true, "auth_header": true, ConfigCloudID: true, ConfigSMTPSecurity: true,
	// The KM2 org key may read other entities of the organisation; a new prefix points it at other records
	ConfigEntityPrefix: true}

// DestinationChanged reports whether a change of settings would send the saved secrets somewhere else (a new
// URL, host, port or header). Such a change must not keep the old secrets, or whoever edits the settings could
// point them at a server they control and have "Test" deliver the credential.
func DestinationChanged(kindName string, before, after map[string]string) bool {
	kind, ok := ConnectionKindOf(kindName)
	if !ok {
		return false
	}
	for _, f := range kind.Fields {
		dest := f.Input == FieldURL || f.Input == FieldHTTPSURL || destinationKeys[f.Key]
		if dest && !f.Secret && before[f.Key] != after[f.Key] {
			return true
		}
	}
	return false
}

// loopbackHTTP is a plain-http URL to this machine (localhost or a loopback address), which webhook fields accept for
// tests and local relays; the sender still refuses it unless an administrator listed its origin.
func loopbackHTTP(u *url.URL) bool {
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
