package model

import (
	"fmt"
	"slices"
)

// Tool effects (docs/agent-chat-handoffs-design.md, section 7): every tool in the runner's catalog says what it does
// outside the run, and what it needs besides keys. Agent chat gives an agent only the tools that read and whose needs
// the turn meets.
const (
	// EffectReads returns information and changes nothing outside the run
	EffectReads = "reads"
	// EffectProposes makes a Confirm card and changes nothing itself
	EffectProposes = "proposes"
	// EffectChanges writes, posts, sends or spends outside the run
	EffectChanges = "changes"
)

// What a tool needs besides keys.
const (
	// NeedCrew is crew state (notes, the action log, the tracking list); an agent in chat has none
	NeedCrew = "crew"
	// NeedTracker is a Jira, Linear or KM2 issues connection
	NeedTracker = "tracker"
	// NeedKM2 is a KM2 connection
	NeedKM2 = "km2"
	// NeedSchedule is a schedule (report delivery); an agent in chat has none
	NeedSchedule = "schedule"
)

var (
	toolEffects = []string{EffectReads, EffectProposes, EffectChanges}
	toolNeeds   = []string{NeedCrew, NeedTracker, NeedKM2, NeedSchedule}
)

// RunConnection is a connection a run may use (never its secrets).
type RunConnection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// CheckToolEffect says what is wrong with a catalog entry's effect and needs: a missing or unknown effect, needs left
// out (an empty list is fine), or an unknown need. The server refuses to start on any of them, so a tool is never
// given in chat by mistake.
func CheckToolEffect(t Tool) error {
	if t.Needs == nil {
		return fmt.Errorf("tool %q: needs is required (use [] for none)", t.Key)
	}
	if !slices.Contains(toolEffects, t.Effect) {
		return fmt.Errorf("tool %q: effect must be one of reads, proposes or changes, not %q", t.Key, t.Effect)
	}
	for _, need := range t.Needs {
		if !slices.Contains(toolNeeds, need) {
			return fmt.Errorf("tool %q: unknown need %q (crew, tracker, km2 or schedule)", t.Key, need)
		}
	}
	return nil
}
