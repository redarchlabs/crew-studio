package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"crewstudio/internal/model"
)

const (
	defaultHistoryLimit = 20
	maxHistoryLimit     = 100
)

// settingsKind lets update_settings reuse the field checks; settings are one object, not a list of items.
var settingsKind = kind{name: "settings", model: reflect.TypeFor[model.Settings]()}

type noArgs struct{}

type settingsArgs struct {
	Changes map[string]any `json:"changes" jsonschema:"only the settings to change, with their new values"`
	DryRun  bool           `json:"dry_run,omitempty" jsonschema:"show what would change without saving"`
}

type crewArgs struct {
	CrewID string `json:"crew_id" jsonschema:"the crew's id (a UUID)"`
}

type startRunArgs struct {
	CrewID         string            `json:"crew_id" jsonschema:"the crew's id (a UUID)"`
	Inputs         map[string]string `json:"inputs,omitempty" jsonschema:"values for the crew's inputs, by key"`
	BudgetUSD      *float64          `json:"budget_usd,omitempty" jsonschema:"spending limit for this run in US dollars; leave out to use the crew's"`
	IdempotencyKey string            `json:"idempotency_key,omitempty" jsonschema:"any unique text; retrying with the same key returns the first run instead of starting another"`
}

type resumeRunArgs struct {
	RunID          string   `json:"run_id" jsonschema:"the failed or cancelled run's id (a UUID)"`
	BudgetUSD      *float64 `json:"budget_usd,omitempty" jsonschema:"spending limit for the new run's own spending in US dollars; leave out to use the earlier run's"`
	UseCurrent     bool     `json:"use_current,omitempty" jsonschema:"run the remaining tasks with the crew's current agents, tasks and LLMs instead of the ones the run started with"`
	IdempotencyKey string   `json:"idempotency_key,omitempty" jsonschema:"any unique text; retrying with the same key returns the first resume instead of starting another"`
}

type runArgs struct {
	RunID string `json:"run_id" jsonschema:"the run's id (a UUID)"`
}

type historyArgs struct {
	Kind  string `json:"kind,omitempty" jsonschema:"only changes to this kind of item"`
	ID    string `json:"id,omitempty" jsonschema:"only changes to this item (needs kind)"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many changes to return, newest first (default 20, at most 100)"`
}

func (t *tools) addOtherTools(s *mcp.Server) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}

	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Title: "Who am I", Annotations: readOnly,
		Description: "Show the account this API key acts as, with its permissions and functional areas."}, t.whoami)
	mcp.AddTool(s, &mcp.Tool{Name: "get_settings", Title: "Get settings", Annotations: readOnly,
		Description: "Read Crew Studio's settings (default budget, concurrency, search model and costs)."}, t.getSettings)
	mcp.AddTool(s, &mcp.Tool{Name: "update_settings", Title: "Change settings",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false)},
		Description: "Change some settings and leave the rest as they are. Returns each changed setting before and after."},
		t.updateSettings)
	mcp.AddTool(s, &mcp.Tool{Name: "validate_crew", Title: "Check a crew", Annotations: readOnly,
		Description: "Check that a crew can run: its agents, tasks, LLMs, prices and tools are complete and consistent."},
		t.validateCrew)
	mcp.AddTool(s, &mcp.Tool{Name: "start_run", Title: "Run a crew",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
		Description: "Start a real run of a crew. It calls paid models and tools and costs money: only do this when asked. " +
			"Pass an idempotency_key so a retry cannot start a second run. Returns the queued run; follow it with get_run."}, t.startRun)
	mcp.AddTool(s, &mcp.Tool{Name: "resume_run", Title: "Resume a failed run",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
		Description: "Resume a failed or cancelled crew run: a new run reuses the tasks it finished (at no cost) and runs the rest, " +
			"starting with the task that failed. It calls paid models and costs money: only do this when asked. get_run's " +
			"resumable field says where a run would resume. Pass an idempotency_key so a retry cannot start a second run."}, t.resumeRun)
	mcp.AddTool(s, &mcp.Tool{Name: "get_run", Title: "Get a run", Annotations: readOnly,
		Description: "Read a run's status, cost, final output and each task's output."}, t.getRun)
	mcp.AddTool(s, &mcp.Tool{Name: "list_history", Title: "List changes", Annotations: readOnly,
		Description: "List recorded changes (who changed what, and a snapshot after each change), newest first.",
		InputSchema: schemaFor[historyArgs](map[string][]string{"kind": historyKindNames()})}, t.listHistory)
}

func (t *tools) whoami(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	data, err := t.call(ctx, req, http.MethodGet, "/auth/me", nil)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) getSettings(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	data, err := t.call(ctx, req, http.MethodGet, "/settings", nil)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) updateSettings(ctx context.Context, req *mcp.CallToolRequest, in settingsArgs) (*mcp.CallToolResult, any, error) {
	current, err := t.object(ctx, req, "/settings")
	if err != nil {
		return nil, nil, err
	}
	merged, err := settingsKind.applyChanges(current, in.Changes)
	if err != nil {
		return nil, nil, err
	}
	if in.DryRun {
		return text(updateResult{Changes: diff(current, merged)})
	}
	data, err := t.call(ctx, req, http.MethodPut, "/settings", merged)
	if err != nil {
		return nil, nil, err
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, nil, fmt.Errorf("read saved settings: %w", err)
	}
	return text(updateResult{Saved: true, Changes: diff(current, saved)})
}

func (t *tools) validateCrew(ctx context.Context, req *mcp.CallToolRequest, in crewArgs) (*mcp.CallToolResult, any, error) {
	if err := checkID("crew_id", in.CrewID); err != nil {
		return nil, nil, err
	}
	data, err := t.call(ctx, req, http.MethodGet, "/crews/"+in.CrewID+"/validate", nil)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) startRun(ctx context.Context, req *mcp.CallToolRequest, in startRunArgs) (*mcp.CallToolResult, any, error) {
	if err := checkID("crew_id", in.CrewID); err != nil {
		return nil, nil, err
	}
	body := map[string]any{"inputs": in.Inputs, "budget_usd": in.BudgetUSD}
	data, err := t.send(ctx, req, Request{Method: http.MethodPost, Path: "/crews/" + in.CrewID + "/runs",
		IdempotencyKey: in.IdempotencyKey}, body)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) resumeRun(ctx context.Context, req *mcp.CallToolRequest, in resumeRunArgs) (*mcp.CallToolResult, any, error) {
	if err := checkID("run_id", in.RunID); err != nil {
		return nil, nil, err
	}
	body := map[string]any{"budget_usd": in.BudgetUSD, "use_current": in.UseCurrent}
	data, err := t.send(ctx, req, Request{Method: http.MethodPost, Path: "/runs/" + in.RunID + "/resume",
		IdempotencyKey: in.IdempotencyKey}, body)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) getRun(ctx context.Context, req *mcp.CallToolRequest, in runArgs) (*mcp.CallToolResult, any, error) {
	if err := checkID("run_id", in.RunID); err != nil {
		return nil, nil, err
	}
	run, err := t.object(ctx, req, "/runs/"+in.RunID)
	if err != nil {
		return nil, nil, err
	}
	// The frozen copy of the whole configuration is large and rarely what anyone asked about
	delete(run, "spec")
	return text(run)
}

func (t *tools) listHistory(ctx context.Context, req *mcp.CallToolRequest, in historyArgs) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	if in.Kind != "" {
		k, err := kindNamed(in.Kind)
		if err != nil {
			return nil, nil, err
		}
		if k.history == "" {
			return nil, nil, fmt.Errorf("%s items have no history", k.name)
		}
		q.Set("entity_type", k.history)
	}
	if in.ID != "" {
		if in.Kind == "" {
			return nil, nil, errors.New("id needs kind as well")
		}
		if err := checkID("id", in.ID); err != nil {
			return nil, nil, err
		}
		q.Set("entity_id", in.ID)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	q.Set("limit", strconv.Itoa(min(limit, maxHistoryLimit)))
	data, err := t.call(ctx, req, http.MethodGet, "/history?"+q.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}
