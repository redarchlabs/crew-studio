package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// KM2 (Knowledge Manager) connections and the knowledge tools (docs/crew-state-and-pmo-design.md, section 9).
// A km2 connection holds one KM2 API key bound to a KM2 profile, a default folder for saves, and an optional list
// of further folders crews may save to. Agents never choose a key; they may only name one of those folders.

const (
	KindKM2 = "km2"
	// ConfigKM2DefaultFolder is the folder km_save_document uses when the agent names none
	ConfigKM2DefaultFolder = "default_folder_id"
	// ConfigKM2WriteFolders lists further folders (comma-separated ids) agents may name when saving
	ConfigKM2WriteFolders = "write_folder_ids"
	// FieldUUID is one KM2 folder id; FieldUUIDList is a comma-separated list of them
	FieldUUID     = "uuid"
	FieldUUIDList = "uuid_list"
	// ActionKM2Save is the action log entry of every save (and every dry-run "would save")
	ActionKM2Save = "km2:save"

	MaxKM2WriteFolders = 50
	// MaxKM2ContentBytes bounds one saved document; long material is split by the crew
	MaxKM2ContentBytes = 1 << 20
	MaxKM2QueryLen     = 5000
	MaxKM2TopK         = 20
	DefaultKM2TopK     = 5

	maxKM2TitleLen      = 255
	maxKM2FolderRefLen  = 300
	maxKM2SourceLen     = 300
	maxKM2MetadataKeys  = 50
	maxKM2MetadataList  = 50
	maxKM2MetadataBytes = 8 << 10
)

// km2RefPattern is KM2's external_ref alphabet. KM2 allows 255 characters; Studio keeps 190 so that the action log
// subject "km2:<ref>" fits its 200-character limit.
var km2RefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,189}$`)

// MaxKM2ExternalRefLen is the longest external_ref Studio accepts (km2RefPattern)
const MaxKM2ExternalRefLen = 190

// km2ReservedMetadata are fields KM2's index filters, scopes or cites on; KM2 refuses them in metadata.
var km2ReservedMetadata = []string{"access_keys", "tenant_id", "tags", "document_key", "document_id", "document_title",
	"type", "text", "summary", "summary_tree", "section", "chunk_order"}

// KM2Subject is the action log subject of a saved document.
func KM2Subject(externalRef string) string { return "km2:" + externalRef }

// KM2Folders lists the folders a km2 connection may save to: the default first, then the others (no repeats).
func KM2Folders(config map[string]string) []string {
	out := []string{}
	if d := config[ConfigKM2DefaultFolder]; d != "" {
		out = append(out, d)
	}
	for _, id := range strings.Split(config[ConfigKM2WriteFolders], ",") {
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// cleanFolderIDs checks a FieldUUID or FieldUUIDList value and returns it lowercased (a list deduplicated and
// joined with commas).
func cleanFolderIDs(c *checker, field, input, v string) string {
	if input == FieldUUID {
		if v != "" && !uuidPattern.MatchString(v) {
			c.addf(field, "must be a KM2 folder id (a UUID)")
		}
		return strings.ToLower(v)
	}
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		id := strings.ToLower(p)
		switch {
		case !uuidPattern.MatchString(p):
			c.addf(field, "%q is not a KM2 folder id (a UUID)", cutText(p, 60))
		case !slices.Contains(out, id):
			out = append(out, id)
		}
	}
	if len(out) > MaxKM2WriteFolders {
		c.addf(field, "list at most %d folders", MaxKM2WriteFolders)
	}
	return strings.Join(out, ",")
}

func cutText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// KM2Save is km_save_document as an agent sends it.
type KM2Save struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	ExternalRef string `json:"external_ref"`
	// Folder is "" (the connection's default), a folder id, or a folder name or path from the connection's list
	Folder string `json:"folder"`
	// Metadata is extra flat metadata; Studio adds source, crew and run_id itself
	Metadata map[string]any `json:"metadata"`
	// SourceFile is the workspace file the content was read from, for the journal and the log
	SourceFile string `json:"source_file"`
}

// NormalizeKM2Save validates a save and returns a trimmed copy (the input is not changed).
func NormalizeKM2Save(in KM2Save) (KM2Save, error) {
	out := KM2Save{Title: strings.TrimSpace(StripHidden(in.Title)), Content: StripHidden(in.Content), ExternalRef: strings.TrimSpace(in.ExternalRef),
		Folder: strings.TrimSpace(in.Folder), Metadata: in.Metadata, SourceFile: strings.TrimSpace(in.SourceFile)}
	var c checker
	c.text("title", out.Title, 1, maxKM2TitleLen)
	if strings.IndexFunc(out.Title, unicode.IsControl) >= 0 {
		c.addf("title", "must not contain control characters")
	}
	switch {
	case strings.TrimSpace(out.Content) == "":
		c.addf("content", "is required (give content or a workspace_file)")
	case len(out.Content) > MaxKM2ContentBytes:
		c.addf("content", "is over the %d-byte limit (%d bytes); split the document", MaxKM2ContentBytes, len(out.Content))
	case strings.ContainsRune(out.Content, 0):
		c.addf("content", "must not contain NUL characters")
	}
	switch {
	case out.ExternalRef == "":
		c.addf("external_ref", "is required, such as atlas-weekly-2026-W41; the same ref saves a new version")
	case !km2RefPattern.MatchString(out.ExternalRef):
		c.addf("external_ref", "use letters, digits, '.', '_', ':' or '-' (at most 190, starting with a letter or digit)")
	}
	c.text("folder", out.Folder, 0, maxKM2FolderRefLen)
	c.text("source_file", out.SourceFile, 0, maxKM2SourceLen)
	c.details = append(c.details, KM2MetadataProblems(out.Metadata)...)
	return out, c.err()
}

// KM2MetadataProblems checks metadata as KM2 does: a flat object of at most 50 keys and 8 KiB whose values are
// strings, numbers, booleans, null or lists (at most 50) of those, with no reserved keys and no NUL characters.
func KM2MetadataProblems(meta map[string]any) []string {
	var c checker
	if len(meta) > maxKM2MetadataKeys {
		c.addf("metadata", "may have at most %d keys", maxKM2MetadataKeys)
	}
	var reserved []string
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
		if slices.Contains(km2ReservedMetadata, k) {
			reserved = append(reserved, k)
		}
	}
	sort.Strings(keys)
	sort.Strings(reserved)
	if len(reserved) > 0 {
		c.addf("metadata", "may not set reserved field(s): %s", strings.Join(reserved, ", "))
	}
	for _, k := range keys {
		checkMetadataValue(&c, k, meta[k])
	}
	if data, err := json.Marshal(meta); err != nil || len(data) > maxKM2MetadataBytes {
		c.addf("metadata", "must be at most %d bytes as JSON", maxKM2MetadataBytes)
	}
	return c.details
}

func checkMetadataValue(c *checker, key string, value any) {
	field := fmt.Sprintf("metadata[%q]", key)
	if key == "" || strings.ContainsRune(key, 0) {
		c.addf("metadata", "keys must be non-empty and must not contain NUL characters")
		return
	}
	list, isList := value.([]any)
	if !isList {
		checkMetadataScalar(c, field, value)
		return
	}
	if len(list) > maxKM2MetadataList {
		c.addf(field, "may hold at most %d items", maxKM2MetadataList)
		return
	}
	for _, item := range list {
		if !checkMetadataScalar(c, field, item) {
			return
		}
	}
}

func checkMetadataScalar(c *checker, field string, value any) bool {
	switch v := value.(type) {
	case nil, bool, float64, float32, int, int64, json.Number:
		return true
	case string:
		if strings.ContainsRune(v, 0) {
			c.addf(field, "must not contain NUL characters")
			return false
		}
		return true
	}
	c.addf(field, "must be a string, number, boolean, null or a list of those")
	return false
}

// KM2Query is km_search's input.
type KM2Query struct {
	Query string `json:"query"`
	// TopK is how many passages to return (1 to 20; 0 means 5)
	TopK int `json:"top_k"`
}

// NormalizeKM2Query validates a search and returns a trimmed copy with the default top_k filled in.
func NormalizeKM2Query(in KM2Query) (KM2Query, error) {
	out := KM2Query{Query: strings.TrimSpace(in.Query), TopK: in.TopK}
	if out.TopK == 0 {
		out.TopK = DefaultKM2TopK
	}
	var c checker
	checkKM2Text(&c, "query", out.Query)
	if out.TopK < 1 || out.TopK > MaxKM2TopK {
		c.addf("top_k", "must be from 1 to %d", MaxKM2TopK)
	}
	return out, c.err()
}

// KM2Question is km_ask's input.
type KM2Question struct {
	Question string `json:"question"`
}

// NormalizeKM2Question validates a question and returns a trimmed copy.
func NormalizeKM2Question(in KM2Question) (KM2Question, error) {
	out := KM2Question{Question: strings.TrimSpace(in.Question)}
	var c checker
	checkKM2Text(&c, "question", out.Question)
	return out, c.err()
}

func checkKM2Text(c *checker, field, v string) {
	c.text(field, v, 1, MaxKM2QueryLen)
	if strings.ContainsRune(v, 0) {
		c.addf(field, "must not contain NUL characters")
	}
}

// km2LogPrefix starts the actions and idempotency keys Studio records for KM2 saves itself.
const km2LogPrefix = "km2:"

// CheckAgentLogRecord refuses log entries an agent may not record with Record Action: KM2 saves are recorded by
// km_save_document only, so an agent cannot forge one, fill the run's save cap, or block a real save's key; the same
// goes for tracker posts, tracker follow-ups and report deliveries (see checkStudioLogRecord).
func CheckAgentLogRecord(in LogRecord) error {
	var c checker
	if strings.HasPrefix(in.Action, km2LogPrefix) {
		c.addf("action", "%s actions are recorded by Crew Studio's KM2 tools only", km2LogPrefix)
	}
	if strings.HasPrefix(in.IdempotencyKey, km2LogPrefix) {
		c.addf("idempotency_key", "must not start with %s", km2LogPrefix)
	}
	// Tracker posts, tracker follow-ups and report deliveries are Studio's too (agentlog.go)
	checkStudioLogRecord(&c, in)
	return c.err()
}
