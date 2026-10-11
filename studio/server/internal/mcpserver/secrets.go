package mcpserver

import (
	"fmt"
	"slices"
	"strings"

	"crewstudio/internal/model"
)

// Connection secrets never pass through MCP: an API key typed into a chat with an AI tool could end up in its logs,
// its context or its provider's records. Secrets are set once in the Crew Studio UI (Connections → Replace secret);
// MCP can only see which ones are set (secret_keys, missing_secrets), as the REST API shows.

const secretsInUI = "set secrets in the Crew Studio UI (Connections → Replace secret)"

func connectionKindNames() []string {
	names := make([]string, 0, len(model.ConnectionKinds))
	for _, k := range model.ConnectionKinds {
		names = append(names, k.Kind)
	}
	return names
}

// secretKeys are the secret fields of every kind of connection. A key that is secret for any kind is refused for all
// of them: no kind has a setting of that name, so nothing legitimate is lost.
func secretKeys() []string {
	var out []string
	for _, k := range model.ConnectionKinds {
		for _, f := range k.Fields {
			if f.Secret && !slices.Contains(out, f.Key) {
				out = append(out, f.Key)
			}
		}
	}
	return out
}

// refuseConnectionSecrets refuses a create or change that carries a secret anywhere: a "secrets" field, or a key
// any kind of connection declares secret, at any depth (in config, at the top, or in a nested object). Names are
// compared ignoring case, because encoding/json matches struct fields that way ("Secrets" fills Secrets).
func refuseConnectionSecrets(fields map[string]any) error {
	secret := append(secretKeys(), "secrets")
	isSecret := func(key string) bool {
		return slices.ContainsFunc(secret, func(s string) bool { return strings.EqualFold(s, key) })
	}
	if found := findKey(fields, isSecret); found != "" {
		return fmt.Errorf("%q is a secret, and secrets are never set through MCP: %s", found, secretsInUI)
	}
	return nil
}

// findKey is the first key (depth first, in key order) for which match is true in a JSON value, or "".
func findKey(v any, match func(string) bool) string {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range sortedKeys(x) {
			if match(key) {
				return key
			}
			if found := findKey(x[key], match); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range x {
			if found := findKey(item, match); found != "" {
				return found
			}
		}
	}
	return ""
}

// connectionSetting is one non-secret setting of a connection kind, for describe_kind.
type connectionSetting struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Input    string   `json:"input"`
	Required bool     `json:"required,omitempty"`
	Hint     string   `json:"hint,omitempty"`
	Options  []string `json:"options,omitempty"`
}

type connectionKindDetail struct {
	Kind     string              `json:"kind"`
	Label    string              `json:"label"`
	Testable bool                `json:"testable"`
	Config   []connectionSetting `json:"config"`
	// SecretsInUI names the kind's secrets, which are set in the Crew Studio UI only
	SecretsInUI []string `json:"secrets_set_in_ui"`
}

// connectionKindDetails lists each kind of connection with its config settings and the names of its secrets.
func connectionKindDetails() any {
	out := map[string]any{"secrets": "never set or shown through MCP: " + secretsInUI}
	kinds := make([]connectionKindDetail, 0, len(model.ConnectionKinds))
	for _, k := range model.ConnectionKinds {
		d := connectionKindDetail{Kind: k.Kind, Label: k.Label, Testable: k.Testable, Config: []connectionSetting{}, SecretsInUI: []string{}}
		for _, f := range k.Fields {
			if f.Secret {
				d.SecretsInUI = append(d.SecretsInUI, f.Key)
				continue
			}
			d.Config = append(d.Config, connectionSetting{Key: f.Key, Label: f.Label, Input: f.Input, Required: f.Required, Hint: f.Hint, Options: f.Options})
		}
		kinds = append(kinds, d)
	}
	out["connection_kinds"] = kinds
	return out
}
