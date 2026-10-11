package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"crewstudio/internal/model"
	"crewstudio/internal/pmo"
)

// Actions on connections, watches and the PMO that are not create, read, update or delete. Each calls the API
// endpoint the Crew Studio UI uses, so permissions, areas, rate limits and audit records are the same.

// pmoKind lets setup_pmo reuse the field checks on the PMO setup request.
var pmoKind = kind{name: "PMO setup", model: reflect.TypeFor[pmo.Request]()}

// watchInputKind checks preview_watch changes against what the API accepts for a watch definition.
var watchInputKind = kind{name: "watch", model: reflect.TypeFor[model.WatchInput]()}

type connectionArgs struct {
	ConnectionID string `json:"connection_id" jsonschema:"the connection's id (a UUID)"`
}

type linkPersonArgs struct {
	ConnectionID string  `json:"connection_id" jsonschema:"the connection's id (a UUID)"`
	AccountID    string  `json:"account_id" jsonschema:"the tracker account id, as get_people_map lists it"`
	UserID       *string `json:"user_id" jsonschema:"the Crew Studio user it is (a UUID from get_people_map's users), or null to unlink"`
}

type crewConnectionsArgs struct {
	CrewID        string   `json:"crew_id" jsonschema:"the crew's id (a UUID)"`
	ConnectionIDs []string `json:"connection_ids" jsonschema:"every connection the crew's tools and watches may use (the whole list; connections outside your areas stay as they are)"`
}

type scheduleArgs struct {
	ScheduleID string `json:"schedule_id" jsonschema:"the schedule's id (a UUID)"`
}

type previewWatchArgs struct {
	ScheduleID string         `json:"schedule_id" jsonschema:"the schedule's id (a UUID)"`
	Changes    map[string]any `json:"changes,omitempty" jsonschema:"fields to try on top of the saved watch (or the whole definition when the schedule has none); leave out to preview the saved watch"`
}

type pmoArgs struct {
	Fields map[string]any `json:"fields" jsonschema:"name, area_ids, tracker_connection_id, timezone and optionally coordinator_llm_id, reporter_llm_id, destinations, enabled (get_pmo_setup_options lists the choices)"`
}

func (t *tools) addTrackerTools(s *mcp.Server) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	change := &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}

	mcp.AddTool(s, &mcp.Tool{Name: "test_connection", Title: "Test a connection", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
		Description: "Check that Crew Studio can reach a connection's system with its saved settings and secret. Tracker connections answer " +
			"with their account; KM2 issues connections also list missing tracker entities. A missing secret is set by a person in the UI " +
			"(Connections → Replace secret), never here."}, t.testConnection)
	mcp.AddTool(s, &mcp.Tool{Name: "get_people_map", Title: "Read a people map", Annotations: readOnly,
		Description: "List a tracker connection's accounts and the Crew Studio users they are linked to, with the users they can be linked to."},
		t.getPeopleMap)
	mcp.AddTool(s, &mcp.Tool{Name: "sync_people_map", Title: "Read people from the tracker", Annotations: change,
		Description: "Read a tracker connection's people into its people map and link them to Crew Studio users with the same email."},
		t.syncPeopleMap)
	mcp.AddTool(s, &mcp.Tool{Name: "link_person", Title: "Link a tracker account", Annotations: change,
		Description: "Link a tracker account to a Crew Studio user (or unlink it with user_id null). A link made here is kept when people are read again."},
		t.linkPerson)
	mcp.AddTool(s, &mcp.Tool{Name: "list_crew_connections", Title: "List a crew's connections", Annotations: readOnly,
		Description: "List the connections you can see, and whether the crew may use each."}, t.listCrewConnections)
	mcp.AddTool(s, &mcp.Tool{Name: "set_crew_connections", Title: "Choose a crew's connections",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), IdempotentHint: true, OpenWorldHint: ptr(false)},
		Description: "Set which connections a crew's tools and watches may use. The list replaces the crew's links among the connections you can see."},
		t.setCrewConnections)
	mcp.AddTool(s, &mcp.Tool{Name: "preview_watch", Title: "Preview a watch", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
		Description: "Run a schedule's watch once without saving anything or starting a run: what it reads, what changed since the last " +
			"check, follow-ups due and roll-ups."}, t.previewWatch)
	mcp.AddTool(s, &mcp.Tool{Name: "run_schedule_now", Title: "Run a schedule now",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
		Description: "Start a schedule's work order now, as its Run now button does. It runs the crew, which calls paid models and costs money, " +
			"and with a watch whose dry_run is false its tracker tools post for real: only do this when asked. Returns the work order."},
		t.runScheduleNow)
	mcp.AddTool(s, &mcp.Tool{Name: "get_pmo_setup_options", Title: "PMO setup choices", Annotations: readOnly,
		Description: "List what setting up a PMO can use: tracker connections, report destinations, LLMs and the default schedules."},
		t.pmoOptions)
	mcp.AddTool(s, &mcp.Tool{Name: "setup_pmo", Title: "Set up a PMO", Annotations: change,
		Description: "Set up a PMO as the Set up a PMO wizard does: copies the PMO template into a new crew with a daily sweep and a weekly " +
			"report schedule (with watches, in dry run), chat and report destinations. It runs nothing by itself."},
		t.setupPMO)
}

func (t *tools) testConnection(ctx context.Context, req *mcp.CallToolRequest, in connectionArgs) (*mcp.CallToolResult, any, error) {
	return t.connectionAction(ctx, req, in.ConnectionID, http.MethodPost, "/test", map[string]any{})
}

func (t *tools) getPeopleMap(ctx context.Context, req *mcp.CallToolRequest, in connectionArgs) (*mcp.CallToolResult, any, error) {
	return t.connectionAction(ctx, req, in.ConnectionID, http.MethodGet, "/people", nil)
}

func (t *tools) syncPeopleMap(ctx context.Context, req *mcp.CallToolRequest, in connectionArgs) (*mcp.CallToolResult, any, error) {
	return t.connectionAction(ctx, req, in.ConnectionID, http.MethodPost, "/people/sync", map[string]any{})
}

func (t *tools) linkPerson(ctx context.Context, req *mcp.CallToolRequest, in linkPersonArgs) (*mcp.CallToolResult, any, error) {
	if !model.ValidAccountID(in.AccountID) {
		return nil, nil, fmt.Errorf("account_id must be a tracker account id as get_people_map lists it (got %q)", in.AccountID)
	}
	if in.UserID != nil {
		if err := checkID("user_id", *in.UserID); err != nil {
			return nil, nil, err
		}
	}
	return t.connectionAction(ctx, req, in.ConnectionID, http.MethodPut, "/people/"+url.PathEscape(in.AccountID), map[string]any{"user_id": in.UserID})
}

func (t *tools) connectionAction(ctx context.Context, req *mcp.CallToolRequest, id, method, suffix string, body any) (*mcp.CallToolResult, any, error) {
	if err := checkID("connection_id", id); err != nil {
		return nil, nil, err
	}
	data, err := t.call(ctx, req, method, "/connections/"+id+suffix, body)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) listCrewConnections(ctx context.Context, req *mcp.CallToolRequest, in crewArgs) (*mcp.CallToolResult, any, error) {
	if err := checkID("crew_id", in.CrewID); err != nil {
		return nil, nil, err
	}
	data, err := t.call(ctx, req, http.MethodGet, "/crews/"+in.CrewID+"/connections", nil)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) setCrewConnections(ctx context.Context, req *mcp.CallToolRequest, in crewConnectionsArgs) (*mcp.CallToolResult, any, error) {
	if err := checkID("crew_id", in.CrewID); err != nil {
		return nil, nil, err
	}
	ids := in.ConnectionIDs
	if ids == nil {
		ids = []string{}
	}
	for _, id := range ids {
		if err := checkID("connection_ids", id); err != nil {
			return nil, nil, err
		}
	}
	data, err := t.call(ctx, req, http.MethodPut, "/crews/"+in.CrewID+"/connections", map[string]any{"connection_ids": ids})
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) previewWatch(ctx context.Context, req *mcp.CallToolRequest, in previewWatchArgs) (*mcp.CallToolResult, any, error) {
	if err := checkID("schedule_id", in.ScheduleID); err != nil {
		return nil, nil, err
	}
	path := "/schedules/" + in.ScheduleID + "/watch"
	view, err := t.object(ctx, req, path)
	if err != nil {
		return nil, nil, err
	}
	def, _ := view["watch"].(map[string]any)
	if def == nil && len(in.Changes) == 0 {
		return nil, nil, fmt.Errorf("the schedule has no watch: pass the definition to try as changes, or create one with create_item kind watch")
	}
	if def == nil {
		def = map[string]any{}
	}
	if len(in.Changes) > 0 {
		if err := watchInputKind.checkFields(in.Changes); err != nil {
			return nil, nil, err
		}
		for k, v := range in.Changes {
			def[k] = v
		}
	}
	data, err := t.call(ctx, req, http.MethodPost, path+"/preview", def)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) runScheduleNow(ctx context.Context, req *mcp.CallToolRequest, in scheduleArgs) (*mcp.CallToolResult, any, error) {
	if err := checkID("schedule_id", in.ScheduleID); err != nil {
		return nil, nil, err
	}
	data, err := t.call(ctx, req, http.MethodPost, "/schedules/"+in.ScheduleID+"/run-now", map[string]any{})
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) pmoOptions(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	data, err := t.call(ctx, req, http.MethodGet, "/pmo/setup", nil)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) setupPMO(ctx context.Context, req *mcp.CallToolRequest, in pmoArgs) (*mcp.CallToolResult, any, error) {
	if len(in.Fields) == 0 {
		return nil, nil, fmt.Errorf("fields is empty: give at least area_ids, tracker_connection_id and timezone")
	}
	if err := pmoKind.checkFields(in.Fields); err != nil {
		return nil, nil, err
	}
	data, err := t.call(ctx, req, http.MethodPost, "/pmo/setup", in.Fields)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}
