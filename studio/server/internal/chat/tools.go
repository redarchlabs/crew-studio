// Package chat runs crew chat (docs/crew-state-and-pmo-design.md, section 8): a person's message becomes a short
// single-agent run of kind "chat" answered by the crew's front desk agent, and any change the front desk wants is
// a proposal that only that person can confirm.
//
// What the front desk may do is fixed here in code, not by its prompt: it gets exactly the tools in Tools (its own
// agent tools are replaced), the run-state API refuses direct writes from chat runs, and proposals are applied only
// through Service.Decide by the conversation's owner.
package chat

import (
	"slices"

	"crewstudio/internal/auth"
)

// Tool is one tool a front desk gets in chat.
type Tool struct {
	Key string
	// Proposes is the proposal type the tool creates; "" for tools that only read
	Proposes string
	// Needs lists the permissions the person chatting must hold for the front desk to get the tool: a chat turn
	// runs as that person, so it reads and proposes only what they could see and confirm themselves
	Needs []string
	// Tracker tools also need a Jira or Linear connection the crew may use and the person can see
	Tracker bool
	// KM2 tools also need a KM2 connection the crew may use and the person can see (see km2.go)
	KM2 bool
}

// Tools is every tool the front desk gets, in order. Read tools reuse the crew state tools (they only read in a
// chat run); proposal tools create a proposal through the run-state API instead of changing anything.
//
// Tracking-list and tracker tools (phase 4) are only given when the crew may use a Jira or Linear connection the
// person can see. Phase 6 adds KM2 search and ask. The runner keeps the same list
// (studio_runner/chat.py) and refuses anything else.
var Tools = []Tool{
	{Key: "notes_get", Needs: []string{auth.StateView}},
	{Key: "notes_list", Needs: []string{auth.StateView}},
	{Key: "log_check", Needs: []string{auth.StateView}},
	{Key: "start_work_order", Proposes: TypeStartWorkOrder, Needs: []string{auth.WorkOrdersCreate}},
	{Key: "propose_note_change", Proposes: TypeNotesPut, Needs: []string{auth.StateView, auth.StateManage}},
	{Key: "tracking_list_get", Needs: []string{auth.StateView}, Tracker: true},
	{Key: "tracker_get_issue", Needs: []string{auth.StateView}, Tracker: true},
	{Key: "tracker_find_issues", Needs: []string{auth.StateView}, Tracker: true},
	{Key: "propose_tracking_add", Proposes: TypeTrackingAdd, Needs: []string{auth.StateView, auth.StateManage}, Tracker: true},
	{Key: "propose_tracking_remove", Proposes: TypeTrackingRemove, Needs: []string{auth.StateView, auth.StateManage}, Tracker: true},
	{Key: "km_search", Needs: []string{auth.StateView}, KM2: true},
	{Key: "km_ask", Needs: []string{auth.StateView}, KM2: true},
}

// ToolKeys lists the keys of Tools.
func ToolKeys() []string {
	keys := make([]string, 0, len(Tools))
	for _, t := range Tools {
		keys = append(keys, t.Key)
	}
	return keys
}

// ToolKeysFor lists the keys of the tools the front desk gets when p is chatting with a crew that has no tracker.
func ToolKeysFor(p *auth.Principal) []string { return ToolKeysForCrew(p, false) }

// ToolKeysForCrew lists the keys of the tools the front desk gets when p is chatting; hasTracker says the crew may
// use a Jira or Linear connection that p can see.
func ToolKeysForCrew(p *auth.Principal, hasTracker bool) []string {
	keys := []string{}
	for _, t := range Tools {
		if holdsAll(p, t.Needs) && (!t.Tracker || hasTracker) && !t.KM2 {
			keys = append(keys, t.Key)
		}
	}
	return keys
}

func holdsAll(p *auth.Principal, perms []string) bool {
	for _, perm := range perms {
		if !p.Can(perm) {
			return false
		}
	}
	return true
}

// Offers reports whether a turn given these tool keys may make proposals of type typ.
func Offers(toolKeys []string, typ string) bool {
	for _, t := range slices.Concat(Tools, AgentTools) {
		if t.Proposes == typ && slices.Contains(toolKeys, t.Key) {
			return true
		}
	}
	return false
}
