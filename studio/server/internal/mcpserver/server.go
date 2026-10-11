// Package mcpserver exposes Crew Studio configuration to MCP clients such as Claude Code. Every tool goes through
// the Crew Studio API as the caller, so permissions, functional areas, validation and history apply unchanged.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Request is one call to the Crew Studio API.
type Request struct {
	Method string
	// Path starts with /api and may carry a query string
	Path string
	Body []byte
	// IdempotencyKey is sent as the Idempotency-Key header when set
	IdempotencyKey string
}

// API sends one request to the Crew Studio API as the caller the token describes, and returns the HTTP status and
// response body.
type API interface {
	Do(ctx context.Context, caller *auth.TokenInfo, req Request) (int, []byte, error)
}

const instructions = `Crew Studio holds CrewAI configuration: agents, tasks, crews, LLMs, model pricing, schedules,
connections and schedule watches.
You act as the person or service account whose API key you were given, with their permissions and functional
areas, or the part of them the key is limited to (whoami lists what the key may do).

To change something:
1. Find it with list_items, then read it with get_item.
2. If you are unsure of the fields, call describe_kind.
3. Call update_item with only the fields to change, passing the updated_at you read as expected_updated_at
   so someone else's newer edit is not overwritten. Use dry_run first when the change is large.
4. After changing agents, tasks or LLMs a crew uses, call validate_crew on that crew.

start_run, resume_run and run_schedule_now start real runs that cost money; only use them when asked to.
Every change is recorded in Crew Studio's history (list_history) and can be reviewed there.

Connections (kind "connection") hold settings only: secrets such as API keys are never set or shown through MCP.
Create the connection with its non-secret config (describe_kind "connection" lists each kind's settings), then ask
the person to paste the secret in the Crew Studio UI (Connections → Replace secret), then call test_connection.
For a tracker: sync_people_map and link_person fill its people map, set_crew_connections lets a crew use it, and a
schedule's watch (kind "watch", addressed by the schedule's id) reads it; preview_watch shows what a check would find.
setup_pmo sets up a PMO crew with its schedules and watches (get_pmo_setup_options lists the choices).`

// New builds the MCP server. version is reported to clients.
func New(api API, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "crew-studio", Title: "Crew Studio", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	t := &tools{api: api}
	t.addItemTools(s)
	t.addOtherTools(s)
	t.addTrackerTools(s)
	return s
}

type tools struct {
	api API
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// checkID refuses anything but a UUID, so an id can never change which API path is called.
func checkID(field, id string) error {
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("%s must be a UUID such as 3f2b6c1e-0000-4000-8000-000000000000 (got %q)", field, id)
	}
	return nil
}

type apiEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string   `json:"code"`
		Message string   `json:"message"`
		Details []string `json:"details"`
	} `json:"error"`
}

// call sends a request and returns the envelope's data. API errors come back as errors carrying the message and
// details, which the SDK reports to the client as a failed tool call.
func (t *tools) call(ctx context.Context, req *mcp.CallToolRequest, method, path string, body any) (json.RawMessage, error) {
	return t.send(ctx, req, Request{Method: method, Path: path}, body)
}

// send is call with the whole request; out.Path is relative to /api and body is encoded into out.Body.
func (t *tools) send(ctx context.Context, req *mcp.CallToolRequest, out Request, body any) (json.RawMessage, error) {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}
	var caller *auth.TokenInfo
	if req != nil && req.Extra != nil {
		caller = req.Extra.TokenInfo
	}
	out.Path, out.Body = "/api"+out.Path, raw
	status, resp, err := t.api.Do(ctx, caller, out)
	if err != nil {
		return nil, fmt.Errorf("call Crew Studio: %w", err)
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	var env apiEnvelope
	if err := json.Unmarshal(resp, &env); err != nil {
		return nil, fmt.Errorf("the API answered %d with a body that is not JSON (%.80q): %w", status, resp, err)
	}
	if status >= 400 || env.Error != nil {
		if env.Error == nil {
			return nil, fmt.Errorf("the API answered %d", status)
		}
		msg := fmt.Sprintf("%s (%s, HTTP %d)", env.Error.Message, env.Error.Code, status)
		if len(env.Error.Details) > 0 {
			msg += ": " + strings.Join(env.Error.Details, "; ")
		}
		return nil, errors.New(msg)
	}
	return env.Data, nil
}

// object fetches one JSON object.
func (t *tools) object(ctx context.Context, req *mcp.CallToolRequest, path string) (map[string]any, error) {
	data, err := t.call(ctx, req, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if data == nil {
		return nil, fmt.Errorf("the API returned nothing for %s", path)
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return obj, nil
}

// text is a tool result holding v as JSON.
func text(v any) (*mcp.CallToolResult, any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("encode result: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
}

// rawText is a tool result holding JSON the API already produced.
func rawText(data json.RawMessage) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}
