package store

import (
	"slices"
	"testing"
	"time"

	"crewstudio/internal/model"
)

func TestDistinctRolesDropsSpecialistsATurnCannotTellApart(t *testing.T) {
	got := distinctRoles("Lead", []model.ChatNameRef{
		{ID: "a", Role: "Analyst"}, {ID: "b", Role: ` "analyst" `}, {ID: "c", Role: "lead"}, {ID: "d", Role: "Writer"},
	})
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "d" {
		t.Errorf("got %+v", got)
	}
	if got := distinctRoles("Lead", nil); got == nil || len(got) != 0 {
		t.Errorf("empty = %#v", got)
	}
}

// An agent's chat connections are saved in order with its other lists, refused when they do not exist, and dropped
// when the connection is deleted.
func TestAgentChatConnectionsAreSavedInOrder(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	agent := f.agent("lead", nil, a)
	first := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000a", connIn("first", "jira", a), nil))
	second := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000b", connIn("second", "km2", a), nil))
	in := model.AgentChatSettings{AgentID: agent.ID, Enabled: true, DailyBudgetUSD: 1, PersonDailyBudgetUSD: 2,
		ConnectionIDs: []string{second.ID, first.ID}}
	got := must(f.s.PutAgentChatSettings(bg(), in, ""))
	if !slices.Equal(got.ConnectionIDs, []string{second.ID, first.ID}) {
		t.Errorf("saved = %v", got.ConnectionIDs)
	}
	in.ConnectionIDs = []string{missingID}
	_, err := f.s.PutAgentChatSettings(bg(), in, "")
	wantConflict(t, err, "does not exist")
	mustOK(t, f.s.DeleteConnection(bg(), second.ID))
	if got := must(f.s.AgentChatSettings(bg(), agent.ID)); !slices.Equal(got.ConnectionIDs, []string{first.ID}) {
		t.Errorf("after deleting a connection = %v", got.ConnectionIDs)
	}
	if got := must(f.s.AgentChatSettings(bg(), missingID)); got.ConnectionIDs == nil {
		t.Error("default settings have nil connections")
	}
}

// An agent-chat turn has no crew: its run token reaches the connections frozen into its spec, only while they stay
// enabled. A crew-less run that is not an agent chat (its crew was deleted) has none.
func TestAgentChatRunScopeUsesFrozenConnections(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("c", a)
	jira := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000a", connIn("jira", "jira", a), nil))
	km2 := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-00000000000b", connIn("km2", "km2", a), nil))
	run := f.run(crew, RunMeta{AreaIDs: crew.AreaIDs})
	frozen := `[{"id":"` + jira.ID + `","name":"jira","kind":"jira"},{"id":"` + km2.ID + `","name":"km2","kind":"km2"},` +
		`{"id":"` + missingID + `","name":"gone","kind":"jira"}]`
	f.exec(`UPDATE runs SET crew_id = NULL, kind = 'chat', chat_agent_id = $2, spec = jsonb_set(spec, '{chat}', jsonb_build_object('connections', $3::jsonb))
		WHERE id = $1`, run.ID, crew.AgentIDs[0], frozen)
	token := must(f.s.CreateRunToken(bg(), run.ID, time.Now().Add(time.Hour)))
	scope := must(f.s.RunTokenScope(bg(), token))
	if scope.CrewID != "" || scope.ChatAgentID != crew.AgentIDs[0] || len(scope.Connections) != 2 || scope.Connections[0].ID != jira.ID ||
		scope.Connections[1].Kind != "km2" {
		t.Errorf("scope = %+v", scope)
	}
	f.exec(`UPDATE connections SET enabled = false WHERE id = $1`, km2.ID)
	if scope := must(f.s.RunTokenScope(bg(), token)); len(scope.Connections) != 1 || scope.Connections[0].ID != jira.ID {
		t.Errorf("after disabling = %+v", scope.Connections)
	}
	f.exec(`UPDATE runs SET chat_agent_id = NULL WHERE id = $1`, run.ID)
	if scope := must(f.s.RunTokenScope(bg(), token)); len(scope.Connections) != 0 {
		t.Errorf("a crew-less run that is not an agent chat = %+v", scope.Connections)
	}
}
