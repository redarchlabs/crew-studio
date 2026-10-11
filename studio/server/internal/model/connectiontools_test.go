package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeDiscoveredTools(t *testing.T) {
	big := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("x", MaxMCPToolSchemaBytes) + `"}`)
	in := []DiscoveredTool{
		{Name: "search", Description: "  Finds\x00 things\n\tfast \x1b[31m ", InputSchema: json.RawMessage(`{ "type": "object" }`), ReadOnly: true},
		{Name: "search", Description: "again"},
		{Name: "bad name", Description: "spaces"},
		{Name: "a/b", Description: "slash"},
		{Name: "", Description: "empty"},
		{Name: strings.Repeat("n", 129)},
		{Name: "arr", InputSchema: json.RawMessage(`[1]`)},
		{Name: "huge", InputSchema: big},
		{Name: "none", Description: strings.Repeat("é", MaxMCPToolDescriptionLen+10)},
	}
	out, skipped := NormalizeDiscoveredTools(in)
	if len(out) != 2 || out[0].Name != "search" || out[1].Name != "none" {
		t.Fatalf("out = %+v", out)
	}
	if out[0].Description != "Finds things\n\tfast [31m" || string(out[0].InputSchema) != `{"type":"object"}` || !out[0].ReadOnly {
		t.Errorf("search = %+v %s", out[0], out[0].InputSchema)
	}
	if string(out[1].InputSchema) != `{"type":"object"}` || len([]rune(out[1].Description)) != MaxMCPToolDescriptionLen {
		t.Errorf("none = %s, %d", out[1].InputSchema, len([]rune(out[1].Description)))
	}
	if len(skipped) != 7 || !strings.Contains(skipped[0], "listed twice") || !strings.Contains(skipped[5], "not a JSON object") ||
		!strings.Contains(skipped[6], "larger than 16 KB") {
		t.Errorf("skipped = %q", skipped)
	}
	many := make([]DiscoveredTool, MaxMCPTools+2)
	for i := range many {
		many[i] = DiscoveredTool{Name: "t" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + itoa(i)}
	}
	out, skipped = NormalizeDiscoveredTools(many)
	if len(out) != MaxMCPTools || len(skipped) != 2 {
		t.Errorf("many = %d kept, %d skipped", len(out), len(skipped))
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestNormalizeConnectionToolsInput(t *testing.T) {
	ok, err := NormalizeConnectionToolsInput(ConnectionToolsInput{Tools: []ConnectionToolChange{{Name: "a", Effect: EffectReads, Enabled: true}}})
	if err != nil || len(ok.Tools) != 1 {
		t.Fatalf("ok = %+v, %v", ok, err)
	}
	for _, bad := range []ConnectionToolsInput{
		{},
		{Tools: []ConnectionToolChange{{Name: "a b", Effect: EffectReads}}},
		{Tools: []ConnectionToolChange{{Name: "a", Effect: EffectProposes}}},
		{Tools: []ConnectionToolChange{{Name: "a", Effect: EffectReads}, {Name: "a", Effect: EffectChanges}}},
		{Tools: make([]ConnectionToolChange, MaxMCPTools+1)},
	} {
		if _, err := NormalizeConnectionToolsInput(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestNormalizeMCPArguments(t *testing.T) {
	for raw, want := range map[string]int{"": 0, "null": 0, `{"q":"x","n":2}`: 2} {
		args, err := NormalizeMCPArguments(json.RawMessage(raw))
		if err != nil || len(args) != want {
			t.Errorf("%q = %v, %v", raw, args, err)
		}
	}
	for _, raw := range []string{`[1]`, `"x"`, `{`, `{"a":"` + strings.Repeat("x", MaxMCPArgumentsBytes) + `"}`} {
		if _, err := NormalizeMCPArguments(json.RawMessage(raw)); err == nil {
			t.Errorf("%.20q accepted", raw)
		}
	}
}

func TestMCPConnectionKind(t *testing.T) {
	kind, ok := ConnectionKindOf(KindMCP)
	if !ok || !kind.Testable || ChatNeedOf(KindMCP) != NeedMCP {
		t.Fatal("mcp kind missing")
	}
	in := ConnectionInput{Name: "Notes MCP", Kind: KindMCP, AreaIDs: []string{"00000000-0000-0000-0000-000000000001"},
		Config: map[string]string{"url": "https://mcp.example.com/mcp", "auth_header": "Authorization"}, Secrets: map[string]string{"auth_value": "Bearer x"}}
	if _, err := NormalizeConnection(in); err != nil {
		t.Fatal(err)
	}
	if MissingSecrets(KindMCP, nil) == nil || len(MissingSecrets(KindMCP, nil)) != 0 {
		t.Error("the auth value is optional")
	}
	if !DestinationChanged(KindMCP, map[string]string{"url": "https://a.example/mcp"}, map[string]string{"url": "https://b.example/mcp"}) {
		t.Error("a new URL must clear the secrets")
	}
}
