package store

import (
	"encoding/json"
	"testing"

	"crewstudio/internal/model"
)

func discovered(name, desc string, readOnly bool) model.DiscoveredTool {
	return model.DiscoveredTool{Name: name, Description: desc, InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: readOnly}
}

// Discovery keeps an administrator's effect for an unchanged tool, takes the hint again for a changed one, starts new
// ones from the hint, and drops the ones the server no longer offers. Agent chat gets only enabled reading tools of
// enabled MCP connections, in the connections' order.
func TestConnectionToolsDiscoveryAndChatTools(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	conn := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000a",
		model.ConnectionInput{Name: "mcp", Kind: model.KindMCP, Config: map[string]string{"url": "https://m.example/mcp"}, AreaIDs: []string{a}}, nil))
	other := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000b",
		model.ConnectionInput{Name: "other", Kind: model.KindMCP, Config: map[string]string{"url": "https://o.example/mcp"}, AreaIDs: []string{a}}, nil))

	tools, review, err := f.s.SaveDiscoveredTools(bg(), conn.ID, []model.DiscoveredTool{
		discovered("read", "Reads.", true), discovered("write", "Writes.", false), discovered("gone", "Old.", true)})
	mustOK(t, err)
	if len(review) != 0 {
		t.Errorf("first discovery review = %v", review)
	}
	if len(tools) != 3 || tools[1].Name != "read" || tools[1].Effect != model.EffectReads || tools[2].Effect != model.EffectChanges || !tools[2].Enabled {
		t.Fatalf("first discovery = %+v", tools)
	}
	must(f.s.UpdateConnectionTools(bg(), conn.ID, []model.ConnectionToolChange{
		{Name: "write", Effect: model.EffectReads, Enabled: true}, {Name: "read", Effect: model.EffectReads, Enabled: true}}))
	if _, err := f.s.UpdateConnectionTools(bg(), conn.ID, []model.ConnectionToolChange{{Name: "nope", Effect: model.EffectReads}}); err == nil {
		t.Error("an unknown tool was updated")
	}
	// "write" is unchanged: the administrator's Reads stays. "read" now says something else and "new" is new: whatever
	// the server's hint says, they start as Changes and are listed for review
	tools, review, err = f.s.SaveDiscoveredTools(bg(), conn.ID, []model.DiscoveredTool{
		discovered("read", "Reads, and now deletes.", true), discovered("write", "Writes.", false), discovered("new", "New.", true)})
	mustOK(t, err)
	if len(review) != 2 || review[0] != "read" || review[1] != "new" {
		t.Errorf("review = %v", review)
	}
	byName := map[string]model.ConnectionTool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	if len(tools) != 3 || byName["write"].Effect != model.EffectReads || byName["read"].Effect != model.EffectChanges ||
		byName["new"].Effect != model.EffectChanges || byName["read"].Description != "Reads, and now deletes." {
		t.Fatalf("rediscovery = %+v", tools)
	}
	_, _, err = f.s.SaveDiscoveredTools(bg(), other.ID, []model.DiscoveredTool{discovered("x", "", true)})
	mustOK(t, err)
	must(f.s.UpdateConnectionTools(bg(), conn.ID, []model.ConnectionToolChange{{Name: "new", Effect: model.EffectReads, Enabled: false}}))

	conns := []model.RunConnection{{ID: other.ID, Kind: model.KindMCP}, {ID: conn.ID, Kind: model.KindMCP}, {ID: missingID, Kind: "jira"}}
	chat := must(f.s.ChatMCPTools(bg(), conns))
	if len(chat) != 2 || chat[0].Name != "x" || chat[0].ConnectionName != "other" || chat[1].Name != "write" || string(chat[1].InputSchema) != `{"type": "object"}` {
		t.Errorf("chat tools = %+v", chat)
	}
	if ok := must(f.s.MCPToolReads(bg(), conn.ID, "write")); !ok {
		t.Error("write does not read")
	}
	if ok := must(f.s.MCPToolReads(bg(), conn.ID, "new")); ok {
		t.Error("a disabled tool reads")
	}
	f.exec(`UPDATE connections SET enabled = false WHERE id = $1`, other.ID)
	if chat := must(f.s.ChatMCPTools(bg(), conns)); len(chat) != 1 || chat[0].Name != "write" {
		t.Errorf("with a disabled connection = %+v", chat)
	}
	if chat := must(f.s.ChatMCPTools(bg(), nil)); chat == nil || len(chat) != 0 {
		t.Errorf("no connections = %#v", chat)
	}
	mustOK(t, f.s.DeleteConnectionTools(bg(), conn.ID))
	if tools := must(f.s.ConnectionTools(bg(), conn.ID)); len(tools) != 0 {
		t.Errorf("after delete = %+v", tools)
	}
}
