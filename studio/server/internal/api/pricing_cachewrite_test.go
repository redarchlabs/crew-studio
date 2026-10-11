package api

import (
	"net/http"
	"testing"

	"crewstudio/internal/model"
)

func TestCacheWritePrices(t *testing.T) {
	e := newEnv(t)
	prices := decodeData[[]model.ModelPrice](t, e.do(t, "GET", "/api/pricing", nil))
	byModel := map[string]model.ModelPrice{}
	for _, p := range prices {
		byModel[p.Model] = p
	}
	// Migration 0023 prices Claude cache writes at 1.25 times input; other models have none (the input price)
	sonnet, ok := byModel["claude-sonnet-5-5"]
	if !ok || sonnet.CacheWritePerMTok == nil || *sonnet.CacheWritePerMTok != sonnet.InputPerMTok*1.25 {
		t.Fatalf("claude-sonnet-5-5 = %+v", sonnet)
	}
	if luna := byModel["gpt-6-luna"]; luna.CacheWritePerMTok != nil {
		t.Errorf("gpt-6-luna cache write = %v", *luna.CacheWritePerMTok)
	}

	// Set, then clear
	write := 3.0
	in := sonnet
	in.CacheWritePerMTok = &write
	got := decodeData[model.ModelPrice](t, e.do(t, "PUT", "/api/pricing/"+sonnet.ID, in))
	if got.CacheWritePerMTok == nil || *got.CacheWritePerMTok != 3 {
		t.Errorf("after set = %+v", got)
	}
	in.CacheWritePerMTok = nil
	if got = decodeData[model.ModelPrice](t, e.do(t, "PUT", "/api/pricing/"+sonnet.ID, in)); got.CacheWritePerMTok != nil {
		t.Errorf("after clear = %+v", got)
	}
	negative := -1.0
	in.CacheWritePerMTok = &negative
	wantStatus(t, e.do(t, "PUT", "/api/pricing/"+sonnet.ID, in), http.StatusUnprocessableEntity)

	created := decodeData[model.ModelPrice](t, e.do(t, "POST", "/api/pricing",
		map[string]any{"model": "claude-test-1", "input_per_mtok": 4, "cached_input_per_mtok": 0.4, "cache_write_per_mtok": 5, "output_per_mtok": 20}))
	if created.CacheWritePerMTok == nil || *created.CacheWritePerMTok != 5 {
		t.Errorf("created = %+v", created)
	}
}
