package model

import "strings"

// providerKeyEnv is the provider env file variable LiteLLM reads for each model prefix Crew Studio offers.
var providerKeyEnv = map[string]string{"openai": "OPENAI_API_KEY", "anthropic": "ANTHROPIC_API_KEY", "gemini": "GEMINI_API_KEY"}

// ProviderKeyEnv is the environment variable a LiteLLM model name (such as anthropic/claude-haiku-4-5) needs for its
// provider's key, or "" when Crew Studio does not know (another provider, or no prefix).
func ProviderKeyEnv(modelName string) string {
	provider, _, found := strings.Cut(strings.TrimSpace(modelName), "/")
	if !found {
		return ""
	}
	return providerKeyEnv[strings.ToLower(provider)]
}
