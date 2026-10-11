package runspec

import (
	"errors"
	"maps"
	"testing"
)

// envKeys says which provider keys are set.
type envKeys struct {
	set map[string]bool
	err error
}

func (e envKeys) EnvSet(keys ...string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = e.set[k]
	}
	return out, e.err
}

func TestMissingProviderKeys(t *testing.T) {
	models := []string{"openai/gpt-6-luna", "anthropic/claude-haiku-4-5", "gemini/gemini-3.8-flash", "ollama/llama", "bare"}
	cases := []struct {
		name string
		set  map[string]bool
		want map[string]string
	}{
		{"all set", map[string]bool{"OPENAI_API_KEY": true, "ANTHROPIC_API_KEY": true, "GEMINI_API_KEY": true}, map[string]string{}},
		{"one missing", map[string]bool{"OPENAI_API_KEY": true, "GEMINI_API_KEY": true},
			map[string]string{"anthropic/claude-haiku-4-5": "ANTHROPIC_API_KEY"}},
		{"none set; unknown providers are never reported", nil, map[string]string{"openai/gpt-6-luna": "OPENAI_API_KEY",
			"anthropic/claude-haiku-4-5": "ANTHROPIC_API_KEY", "gemini/gemini-3.8-flash": "GEMINI_API_KEY"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MissingProviderKeys(envKeys{set: tc.set}, models)
			if err != nil || !maps.Equal(got, tc.want) {
				t.Errorf("MissingProviderKeys = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	if got, err := MissingProviderKeys(envKeys{err: errors.New("unreadable")}, models); err == nil || len(got) != 0 {
		t.Errorf("an unreadable env file = %v, %v", got, err)
	}
	if got, err := MissingProviderKeys(envKeys{err: errors.New("never asked")}, []string{"bare"}); err != nil || len(got) != 0 {
		t.Errorf("no known provider = %v, %v", got, err)
	}
}
