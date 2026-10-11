package main

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"crewstudio/internal/config"
)

func TestMemoryAndHelpShareEmbedders(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	cfg := config.Config{ProviderEnvFile: filepath.Join(t.TempDir(), "missing.env"), HelpSearch: config.HelpSearchOpenAI, MemoryEmbeddings: config.HelpSearchOpenAI}
	e := newEmbedders(cfg)
	if e.get(config.HelpSearchOpenAI) != e.get(config.HelpSearchOpenAI) || e.get(config.HelpSearchKeyword) != nil {
		t.Error("one embedder per provider")
	}
	if _, err := newHelpIndex(cfg, e, nil, log); err != nil {
		t.Fatal(err)
	}
	keyword := cfg
	keyword.HelpSearch = config.HelpSearchKeyword
	if _, err := newHelpIndex(keyword, e, nil, log); err != nil {
		t.Fatal(err)
	}

	// No key: unavailable, saying which key
	if ok, reason := newMemoryService(cfg, e, nil, log).Available(); ok || !strings.Contains(reason, "OPENAI_API_KEY") {
		t.Errorf("openai without key = %v %q", ok, reason)
	}
	cfg.MemoryEmbeddings = config.HelpSearchGemini
	if ok, reason := newMemoryService(cfg, e, nil, log).Available(); ok || !strings.Contains(reason, "GEMINI_API_KEY") {
		t.Errorf("gemini without key = %v %q", ok, reason)
	}
	cfg.MemoryEmbeddings = config.MemoryEmbeddingsOff
	if ok, reason := newMemoryService(cfg, e, nil, log).Available(); ok || !strings.Contains(reason, "STUDIO_MEMORY_EMBEDDINGS=off") {
		t.Errorf("off = %v %q", ok, reason)
	}
	t.Setenv("OPENAI_API_KEY", "sk-test")
	cfg.MemoryEmbeddings = config.HelpSearchOpenAI
	svc := newMemoryService(cfg, e, nil, log)
	if ok, _ := svc.Available(); !ok || svc.Model() != "text-embedding-3-small:768" {
		t.Errorf("openai with key = %v %q", ok, svc.Model())
	}
}
