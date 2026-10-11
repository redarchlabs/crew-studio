package chat

import (
	"slices"
	"testing"

	"crewstudio/internal/model"
)

// Only MCP connections with a frozen tool go into the turn's spec, so the run token reaches no other.
func TestMCPConnectionsWithFrozenTools(t *testing.T) {
	notes := model.RunConnection{ID: "n", Kind: model.KindMCP}
	idle := model.RunConnection{ID: "i", Kind: model.KindMCP}
	jira := model.RunConnection{ID: "j", Kind: model.TrackerJira}
	tools := []model.ChatMCPTool{{ConnectionID: "n", Name: "search"}, {ConnectionID: "j", Name: "odd"}}
	got := mcpConnections([]model.RunConnection{jira, idle, notes}, tools)
	if len(got) != 1 || got[0].ID != "n" {
		t.Errorf("got %+v", got)
	}
	if got := mcpConnections(nil, nil); got == nil || len(got) != 0 {
		t.Errorf("none = %#v", got)
	}
}

// What an MCP server returns is untrusted: with MCP tools in a turn, no agent may fetch a URL.
func TestWithoutFetchStripsEveryAgent(t *testing.T) {
	lead := model.Agent{Tools: []string{"google_search", "scrape_website"}}
	specialists := []model.Agent{{Tools: []string{"scrape_website"}}, {Tools: []string{"workspace_read"}}}
	gotLead, gotSpecialists := withoutFetch(lead, specialists)
	if !slices.Equal(gotLead.Tools, []string{"google_search"}) || len(gotSpecialists[0].Tools) != 0 ||
		!slices.Equal(gotSpecialists[1].Tools, []string{"workspace_read"}) {
		t.Errorf("lead %v, specialists %+v", gotLead.Tools, gotSpecialists)
	}
	if !slices.Contains(lead.Tools, "scrape_website") || !slices.Contains(specialists[0].Tools, "scrape_website") {
		t.Error("the agents passed in were changed")
	}
}
