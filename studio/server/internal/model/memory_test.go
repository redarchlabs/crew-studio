package model

import (
	"strings"
	"testing"
)

func TestNormalizeMemorySettings(t *testing.T) {
	good := DefaultMemorySettings("c")
	good.Mode, good.RecallDepth = MemoryAutomatic, ""
	llm := "  "
	good.LLMID = &llm
	out, err := NormalizeMemorySettings(good)
	if err != nil || out.RecallDepth != MemoryShallow || out.LLMID != nil {
		t.Fatalf("out = %+v, %v", out, err)
	}
	if !out.NeedsLLM() {
		t.Error("automatic needs a model")
	}
	deep := DefaultMemorySettings("c")
	deep.Mode, deep.RecallDepth = MemoryRecallOnly, MemoryDeep
	if !deep.NeedsLLM() || DefaultMemorySettings("c").NeedsLLM() {
		t.Error("NeedsLLM: deep recall yes, off no")
	}
	bad := func(edit func(*MemorySettings)) MemorySettings {
		s := DefaultMemorySettings("c")
		edit(&s)
		return s
	}
	zero, big, id := 0, 5000, "nope"
	for field, s := range map[string]MemorySettings{
		"mode":                   bad(func(s *MemorySettings) { s.Mode = "sometimes" }),
		"recall_depth":           bad(func(s *MemorySettings) { s.RecallDepth = "medium" }),
		"llm_id":                 bad(func(s *MemorySettings) { s.LLMID = &id }),
		"recall_limit":           bad(func(s *MemorySettings) { s.RecallLimit = 21 }),
		"saves_per_run":          bad(func(s *MemorySettings) { s.SavesPerRun = -1 }),
		"retention_days":         bad(func(s *MemorySettings) { s.RetentionDays = &zero }),
		"retention_days ":        bad(func(s *MemorySettings) { s.RetentionDays = &big }),
		"recency_weight":         bad(func(s *MemorySettings) { s.RecencyWeight = 1.5 }),
		"semantic_weight":        bad(func(s *MemorySettings) { s.RecencyWeight, s.SemanticWeight, s.ImportanceWeight = 0, 0, 0 }),
		"importance_weight":      bad(func(s *MemorySettings) { s.ImportanceWeight = -0.1 }),
		"recency_half_life_days": bad(func(s *MemorySettings) { s.RecencyHalfLifeDays = 0 }),
	} {
		_, err := NormalizeMemorySettings(s)
		if err == nil || !strings.Contains(err.Error(), strings.TrimSpace(field)+":") {
			t.Errorf("%s: err = %v", field, err)
		}
	}
	if spec := (MemorySpec{Mode: MemoryExplicit}); !spec.ReadOnly() || (MemorySpec{Mode: MemoryAutomatic}).ReadOnly() {
		t.Error("ReadOnly")
	}
}
