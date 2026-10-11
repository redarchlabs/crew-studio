package model

import "testing"

func TestProviderKeyEnv(t *testing.T) {
	for name, want := range map[string]string{"anthropic/claude-haiku-4-5": "ANTHROPIC_API_KEY", "openai/gpt-6-luna": "OPENAI_API_KEY",
		" Gemini/gemini-omni-1.1-flash": "GEMINI_API_KEY", "bedrock/x": "", "gpt-4o": ""} {
		if got := ProviderKeyEnv(name); got != want {
			t.Errorf("%q = %q, want %q", name, got, want)
		}
	}
}
