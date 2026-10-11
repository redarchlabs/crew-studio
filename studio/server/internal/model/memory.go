package model

import (
	"time"
)

// Memory (docs/crew-state-and-pmo-design.md, section 19): soft lessons a crew learns by doing its work, recalled
// by similarity before each task. Studio owns the rows and the vectors; the runner keeps CrewAI's recall and
// extraction and stores through the run-state API (/api/run-state/memory/*). Scopes are paths relative to the
// crew ("/", "/people/sam"); the crew is the boundary.

// Memory modes.
const (
	MemoryOff        = "off"
	MemoryRecallOnly = "recall_only"
	MemoryExplicit   = "explicit"
	MemoryAutomatic  = "automatic"

	MemoryShallow = "shallow"
	MemoryDeep    = "deep"

	MemoryActive  = "active"
	MemoryPending = "pending"
)

// Memory limits.
const (
	MaxMemoryContent       = 2000
	MaxMemoryCategories    = 10
	MaxMemoryCategoryLen   = 40
	MaxMemoryMetadataKeys  = 20
	MaxMemoryMetadataBytes = 4096
	MaxMemoryScopeSegments = 8
	MaxMemoryScopeLen      = 200
	MaxMemoryScopeSegment  = 64
	MaxMemoriesPerCrew     = 5000
	// MaxPendingMemoriesPerCrew bounds the memories waiting for review in one crew; runs cannot save more until
	// people approve or delete some (people can still add)
	MaxPendingMemoriesPerCrew = 500
	DefaultMemoryRetention    = 365
	PendingMemoryExpiryDays   = 30
	MaxMemoryRecallLimit      = 20
	MaxMemorySavesPerRun      = 100
	MaxMemoryRetentionDays    = 3650
	MaxMemoryHalfLifeDays     = 3650
)

// MemorySettings say whether and how a crew remembers between runs.
type MemorySettings struct {
	CrewID string `json:"crew_id"`
	Mode   string `json:"mode"`
	// LLMID is the model for automatic extraction and deep recall; nil uses the crew's cheapest agent model
	LLMID       *string `json:"llm_id"`
	RecallDepth string  `json:"recall_depth"`
	RecallLimit int     `json:"recall_limit"`
	SavesPerRun int     `json:"saves_per_run"`
	// RetentionDays deletes unpinned memories older than this; nil keeps them
	RetentionDays *int `json:"retention_days"`
	// ReviewRunSaves makes every memory a run saves (the Remember tool, automatic extraction) wait as pending, not
	// recalled, until a person approves it; memories people add are active at once. On by default: text a crew reads
	// (tickets, emails, web pages) can plant instructions that a run saves as a lesson
	ReviewRunSaves      bool       `json:"review_run_saves"`
	RecencyWeight       float64    `json:"recency_weight"`
	SemanticWeight      float64    `json:"semantic_weight"`
	ImportanceWeight    float64    `json:"importance_weight"`
	RecencyHalfLifeDays int        `json:"recency_half_life_days"`
	UpdatedByName       string     `json:"updated_by_name"`
	UpdatedAt           *time.Time `json:"updated_at"`
}

// DefaultMemorySettings is a crew's memory before anyone sets it up: off, with CrewAI's ranking defaults.
func DefaultMemorySettings(crewID string) MemorySettings {
	retention := DefaultMemoryRetention
	return MemorySettings{CrewID: crewID, Mode: MemoryOff, RecallDepth: MemoryShallow, RecallLimit: 5, SavesPerRun: 20,
		RetentionDays: &retention, ReviewRunSaves: true, RecencyWeight: 0.3, SemanticWeight: 0.5, ImportanceWeight: 0.2,
		RecencyHalfLifeDays: 30}
}

// NeedsLLM reports whether the settings make memory call a model: automatic extraction, or deep recall.
func (m MemorySettings) NeedsLLM() bool {
	return m.Mode == MemoryAutomatic || (m.Mode != MemoryOff && m.RecallDepth == MemoryDeep)
}

// NormalizeMemorySettings validates memory settings (not whether the LLM exists or is priced) and returns a copy.
func NormalizeMemorySettings(in MemorySettings) (MemorySettings, error) {
	out := in
	out.LLMID = trimmedPtr(in.LLMID)
	var c checker
	switch out.Mode {
	case MemoryOff, MemoryRecallOnly, MemoryExplicit, MemoryAutomatic:
	default:
		c.addf("mode", "must be off, recall_only, explicit or automatic")
	}
	if out.RecallDepth == "" {
		out.RecallDepth = MemoryShallow
	}
	if out.RecallDepth != MemoryShallow && out.RecallDepth != MemoryDeep {
		c.addf("recall_depth", "must be shallow or deep")
	}
	c.optionalUUID("llm_id", out.LLMID)
	if out.RecallLimit < 1 || out.RecallLimit > MaxMemoryRecallLimit {
		c.addf("recall_limit", "must be between 1 and %d", MaxMemoryRecallLimit)
	}
	if out.SavesPerRun < 0 || out.SavesPerRun > MaxMemorySavesPerRun {
		c.addf("saves_per_run", "must be between 0 and %d", MaxMemorySavesPerRun)
	}
	if out.RetentionDays != nil && (*out.RetentionDays < 1 || *out.RetentionDays > MaxMemoryRetentionDays) {
		c.addf("retention_days", "must be between 1 and %d, or null to keep memories", MaxMemoryRetentionDays)
	}
	c.floatRange("recency_weight", out.RecencyWeight, 0, 1)
	c.floatRange("semantic_weight", out.SemanticWeight, 0, 1)
	c.floatRange("importance_weight", out.ImportanceWeight, 0, 1)
	if out.RecencyWeight+out.SemanticWeight+out.ImportanceWeight <= 0 {
		c.addf("semantic_weight", "the three ranking weights cannot all be 0")
	}
	if out.RecencyHalfLifeDays < 1 || out.RecencyHalfLifeDays > MaxMemoryHalfLifeDays {
		c.addf("recency_half_life_days", "must be between 1 and %d", MaxMemoryHalfLifeDays)
	}
	return out, c.err()
}

// MemorySpec is a crew's memory as frozen into a run (RunSpec.Memory). It is left out when memory is off, when the
// server has no embedder, and for chat turns.
type MemorySpec struct {
	Mode string `json:"mode"`
	// LLMID is in RunSpec.LLMs when memory calls a model (automatic mode or deep recall); otherwise ""
	LLMID               string  `json:"llm_id"`
	RecallDepth         string  `json:"recall_depth"`
	RecallLimit         int     `json:"recall_limit"`
	SavesPerRun         int     `json:"saves_per_run"`
	ReviewRunSaves      bool    `json:"review_run_saves"`
	RecencyWeight       float64 `json:"recency_weight"`
	SemanticWeight      float64 `json:"semantic_weight"`
	ImportanceWeight    float64 `json:"importance_weight"`
	RecencyHalfLifeDays int     `json:"recency_half_life_days"`
	// EmbedModel is the server's embedder when the run started
	EmbedModel string `json:"embed_model"`
}

// SavedStatus is the status of a memory a run saves: pending while review applies, otherwise active.
func (m MemorySpec) SavedStatus() string {
	if m.ReviewRunSaves {
		return MemoryPending
	}
	return MemoryActive
}

// ReadOnly reports whether CrewAI may not save on its own: recall only, or explicit saves (the Remember tool only).
func (m MemorySpec) ReadOnly() bool { return m.Mode == MemoryRecallOnly || m.Mode == MemoryExplicit }

// MemoryRecord is one memory as the run-state API and the Memory tab show it (never its vector).
type MemoryRecord struct {
	ID         string         `json:"id"`
	Content    string         `json:"content"`
	Scope      string         `json:"scope"`
	Categories []string       `json:"categories"`
	Metadata   map[string]any `json:"metadata"`
	Importance float64        `json:"importance"`
	// Source is run:<run id> or user:<name>
	Source string `json:"source"`
	// Private is always false: every memory is visible to the whole crew (CrewAI's field, kept for the runner)
	Private       bool      `json:"private"`
	Pinned        bool      `json:"pinned"`
	Status        string    `json:"status"`
	RunID         *string   `json:"run_id"`
	AgentRole     string    `json:"agent_role"`
	TaskName      string    `json:"task_name"`
	WrittenBy     string    `json:"written_by"`
	CreatedAt     time.Time `json:"created_at"`
	LastAccessed  time.Time `json:"last_accessed"`
	UpdatedAt     time.Time `json:"updated_at"`
	KM2DocumentID *string   `json:"km2_document_id"`
	// EmbedModel and Vector are for the server only
	EmbedModel string    `json:"-"`
	Vector     []float32 `json:"-"`
}

// MemoryScopeInfo answers CrewAI's get_scope_info.
type MemoryScopeInfo struct {
	Path         string     `json:"path"`
	RecordCount  int        `json:"record_count"`
	Categories   []string   `json:"categories"`
	OldestRecord *time.Time `json:"oldest_record"`
	NewestRecord *time.Time `json:"newest_record"`
	ChildScopes  []string   `json:"child_scopes"`
}

// MemoryScopeCount is one scope on the Memory tab with how many memories it holds.
type MemoryScopeCount struct {
	Scope string `json:"scope"`
	Count int    `json:"count"`
}
