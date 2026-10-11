package mcpserver

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"crewstudio/internal/model"
)

// kind is one type of Crew Studio item the tools can reach, and the API path that serves it.
type kind struct {
	name  string
	about string
	path  string
	model reflect.Type
	// key is the field that identifies an item ("id", or "key" for agent tools)
	key      string
	writable bool
	// getByID is false where the API only lists (areas, agent tools)
	getByID bool
	// history is the revision entity type for list_history ("" = none)
	history string
	// summary is what list_items shows per item; get_item shows everything
	summary []string
	// readOnly fields are set by Crew Studio, beyond id, created_at and updated_at
	readOnly []string
	// hidden fields are retired and never shown or accepted
	hidden []string
	hints  map[string]string
	// itemPath, when set, is the API path of one item for an id (a watch lives under its schedule's id)
	itemPath func(id string) string
	// unwrap is the field of a get answer that holds the item; "" when the answer is the item
	unwrap string
	// listHint, when set, says why list_items cannot list this kind and how to find its items
	listHint string
	// createByID creates with PUT at itemPath(id) instead of POST to path (a schedule's watch)
	createByID bool
	// refuse checks a create's fields or an update's changes before anything is sent
	refuse func(fields map[string]any) error
	// extra is more for describe_kind (the settings of each kind of connection)
	extra func() any
}

var serverSet = []string{"id", "created_at", "updated_at"}

var kinds = []kind{
	{
		name: "agent", about: "A CrewAI agent: a role with a goal, a backstory, an LLM and tools.",
		path: "/agents", model: reflect.TypeFor[model.Agent](), key: "id", writable: true, getByID: true, history: "agent",
		summary: []string{"id", "name", "role", "llm_id", "tools", "area_ids", "updated_at"},
		hints: map[string]string{
			"llm_id": `id of an LLM (list_items kind "llm"). Pick the cheapest LLM that can do the job (list_items kind ` +
				`"pricing" has the prices). Keep the most expensive reasoning models for the hardest reasoning, and never give ` +
				`one to an agent with google_search or scrape_website: every result is resent on each of its LLM calls`,
			"tools":    `tool keys (list_items kind "tool")`,
			"max_iter": "at least 1",
			"area_ids": `functional area ids (list_items kind "area"); at least one`,
		},
	},
	{
		name: "task", about: "A CrewAI task: what to do, the expected output and the agent that does it.",
		path: "/tasks", model: reflect.TypeFor[model.Task](), key: "id", writable: true, getByID: true, history: "task",
		summary: []string{"id", "name", "agent_id", "context_task_ids", "area_ids", "updated_at"},
		hints: map[string]string{
			"agent_id":         `id of the agent that does the task (list_items kind "agent")`,
			"context_task_ids": "ids of tasks whose output this task reads",
			"markdown":         "ask the agent to answer in Markdown",
			"area_ids":         `functional area ids (list_items kind "area"); at least one`,
		},
	},
	{
		name: "crew", about: "A CrewAI crew: agents and tasks run together by a process. Work orders, schedules and triggers run crews.",
		path: "/crews", model: reflect.TypeFor[model.Crew](), key: "id", writable: true, getByID: true, history: "crew",
		summary: []string{"id", "name", "process", "agent_ids", "task_ids", "budget_usd", "area_ids", "updated_at"},
		hints: map[string]string{
			"process":        `"sequential" or "hierarchical"`,
			"manager_llm_id": "LLM id for the manager; required by the hierarchical process",
			"task_ids":       "tasks in run order",
			"inputs":         "values the crew asks for when it runs; {key} placeholders in agents and tasks use them",
			"budget_usd":     "default spending limit for one run, in US dollars",
			"area_ids":       `functional area ids (list_items kind "area"); at least one`,
		},
	},
	{
		name: "llm", about: "A model configuration agents and crews use (provider/model, temperature, reasoning effort).",
		path: "/llms", model: reflect.TypeFor[model.LLM](), key: "id", writable: true, getByID: true, history: "llm",
		summary: []string{"id", "name", "model", "api", "reasoning_effort", "vision", "updated_at"},
		hints: map[string]string{
			"model":            `LiteLLM model name such as "openai/gpt-6-luna" or "anthropic/claude-sonnet-5-5"`,
			"api":              `"completions" or "responses" (responses is only for openai/ models)`,
			"reasoning_effort": "none, minimal, low, medium, high, xhigh, max, or null",
			"vision":           "the model accepts images and PDFs",
		},
	},
	{
		name: "pricing", about: "The price of a model, used to cost runs and enforce budgets.",
		path: "/pricing", model: reflect.TypeFor[model.ModelPrice](), key: "id", writable: true, getByID: true, history: "pricing",
		summary: []string{"id", "model", "input_per_mtok", "cached_input_per_mtok", "cache_write_per_mtok", "output_per_mtok", "updated_at"},
		hints: map[string]string{
			"model":                 "the model name exactly as LLMs use it",
			"input_per_mtok":        "US dollars per million input tokens",
			"cached_input_per_mtok": "US dollars per million cached input tokens",
			"cache_write_per_mtok":  "US dollars per million tokens written to the prompt cache; null means the input price",
			"output_per_mtok":       "US dollars per million output tokens",
		},
	},
	{
		name: "schedule", about: "Runs a crew on a cron schedule, creating a work order each time. It runs as whoever saved it last.",
		path: "/schedules", model: reflect.TypeFor[model.Schedule](), key: "id", writable: true, getByID: true, history: "schedule",
		summary:  []string{"id", "name", "crew_id", "cron", "timezone", "enabled", "next_run_at", "updated_at"},
		readOnly: []string{"target_name", "run_as", "run_as_name", "next_run_at", "last_run_at", "last_work_order_id", "last_error"},
		hidden:   []string{"target_type", "task_id"},
		hints: map[string]string{
			"crew_id":        "id of the crew to run",
			"cron":           `five-field cron expression such as "0 9 * * 1-5"`,
			"timezone":       `IANA time zone such as "Europe/London"`,
			"inputs":         "values for the crew's inputs",
			"budget_usd":     "spending limit per run in US dollars; null uses the crew's",
			"overlap_policy": `"skip" (do not start while the last one is still running) or "allow"`,
			"area_ids":       `functional area ids (list_items kind "area"); at least one`,
		},
	},
	{
		name: "connection", about: "A connection to an outside system (Jira, Linear, KM2 issues, KM2, HTTP, Slack, Teams, email) that crews' " +
			"tools and watches use. Secrets (API keys, tokens, passwords, webhook URLs) are never set or shown here: set them in the Crew " +
			"Studio UI (Connections → Replace secret). secret_keys and missing_secrets only name which secrets are set or still missing.",
		path: "/connections", model: reflect.TypeFor[model.Connection](), key: "id", writable: true, getByID: true,
		summary:  []string{"id", "name", "kind", "enabled", "area_ids", "missing_secrets", "updated_at"},
		readOnly: []string{"secret_keys", "missing_secrets", "secret_updated_at", "crew_count", "created_by"},
		hints: map[string]string{
			"kind": "one of " + strings.Join(connectionKindNames(), ", ") + "; cannot change after creating (describe_kind lists each kind's settings under extra)",
			"config": "the kind's non-secret settings as strings, by key (describe_kind lists them under extra); a change replaces the whole object, " +
				"so send every setting to keep. A new URL, host, port, header or entity prefix clears the saved secrets",
			"area_ids": `functional area ids (list_items kind "area"); at least one`,
			"enabled":  "runs and watches only use enabled connections",
		},
		refuse: refuseConnectionSecrets, extra: connectionKindDetails,
	},
	{
		name: "watch", about: "A schedule's watch: what it reads before each run (a tracker project, the crew's tracking list, a JSON API, " +
			"a web page or a workspace file), what counts as a change, and the follow-up ladder. A schedule has at most one. Its id here is " +
			"the schedule's id: get_item, update_item and delete_item take the schedule id, and create_item takes it as id. preview_watch " +
			"shows what a check would find; run_schedule_now starts a run.",
		path: "/schedules", model: reflect.TypeFor[model.Watch](), key: "id", writable: true, getByID: true,
		itemPath: func(id string) string { return "/schedules/" + id + "/watch" }, unwrap: "watch", createByID: true,
		listHint: `watches belong to schedules: list_items kind "schedule", then get_item kind "watch" with the schedule's id`,
		summary:  []string{"id", "schedule_id", "source", "connection_id", "dry_run", "updated_at"},
		readOnly: []string{"schedule_id", "schedule_name", "crew_id", "connection_name", "last_checked_at", "last_changed_at", "last_error",
			"snapshot_at", "snapshot_items"},
		hints: map[string]string{
			"connection_id": `the connection the source reads through (list_items kind "connection"); the schedule's crew must be allowed ` +
				"to use it (set_crew_connections) and share an area with it; null for sources that need none",
			"source": `{kind, ...}: kind is one of ` + strings.Join(watchSourceKindNames(), ", ") + `. tracker: project (such as "ERP"), ` +
				"include_done, lead, comments, stale_days; jql only for Jira. tracking_list: comments, stale_days. http_json: path, items_path. " +
				"web_page: url, selector. workspace_file: file, format",
			"id_field":       `the field that identifies an item; "key" for tracker sources; empty for web pages`,
			"tracked_fields": "fields whose changes count; tracker sources: title, status, status_category, assignee, due_date, priority, updated_at, parent, labels",
			"run_when":       `"changed" (default), "always" or "changed_or_followups"`,
			"dry_run":        "true (the default) lets tracker tools only say what they would post; false posts for real",
			"max_items":      "1 to 10000 (default 2000); more items fail the check instead of being cut",
		},
	},
	{
		name: "area", about: "A functional area (such as Finance or IT). Items belong to areas, and people see the areas they belong to.",
		path: "/areas", model: reflect.TypeFor[model.Area](), key: "id",
		summary: []string{"id", "name", "description"},
	},
	{
		name: "tool", about: "A tool agents can use. available is false when the server lacks what it needs (see requires_env).",
		path: "/tools", model: reflect.TypeFor[model.Tool](), key: "key",
		summary: []string{"key", "name", "description", "available", "requires_env", "priced_model"},
	},
}

// pathOf is the API path of one item.
func (k kind) pathOf(id string) string {
	if k.itemPath != nil {
		return k.itemPath(id)
	}
	return k.path + "/" + id
}

func watchSourceKindNames() []string {
	var names []string
	for _, k := range model.WatchSourceKinds {
		names = append(names, k.Kind)
	}
	return names
}

func kindNamed(name string) (kind, error) {
	for _, k := range kinds {
		if k.name == name {
			return k, nil
		}
	}
	return kind{}, fmt.Errorf("unknown kind %q: use one of %s", name, strings.Join(kindNames(false), ", "))
}

func kindNames(writableOnly bool) []string {
	var names []string
	for _, k := range kinds {
		if k.writable || !writableOnly {
			names = append(names, k.name)
		}
	}
	return names
}

func historyKindNames() []string {
	var names []string
	for _, k := range kinds {
		if k.history != "" {
			names = append(names, k.name)
		}
	}
	return names
}

type fieldInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Hint     string `json:"hint,omitempty"`
}

type kindInfo struct {
	Kind     string      `json:"kind"`
	About    string      `json:"about"`
	Writable bool        `json:"writable"`
	Fields   []fieldInfo `json:"fields"`
	Extra    any         `json:"extra,omitempty"`
}

// describe lists the kind's fields from its Go type, so the description cannot drift from what the API accepts.
func (k kind) describe() kindInfo {
	info := kindInfo{Kind: k.name, About: k.about, Writable: k.writable}
	if k.extra != nil {
		info.Extra = k.extra()
	}
	for _, f := range jsonFields(k.model) {
		if slices.Contains(k.hidden, f.name) {
			continue
		}
		info.Fields = append(info.Fields, fieldInfo{
			Name: f.name, Type: typeName(f.typ), Hint: k.hints[f.name],
			ReadOnly: slices.Contains(serverSet, f.name) || slices.Contains(k.readOnly, f.name),
		})
	}
	return info
}

// checkFields refuses fields the kind does not have and fields Crew Studio sets itself.
func (k kind) checkFields(fields map[string]any) error {
	known := map[string]fieldInfo{}
	for _, f := range k.describe().Fields {
		known[f.Name] = f
	}
	var problems []string
	for _, name := range sortedKeys(fields) {
		f, ok := known[name]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%q is not a field of %s (describe_kind lists them)", name, k.name))
		case f.ReadOnly:
			problems = append(problems, fmt.Sprintf("%q is set by Crew Studio and cannot be changed", name))
		case fields[name] == nil && !strings.HasSuffix(f.Type, " or null"):
			// The API would quietly store the zero value ("" or false) instead
			problems = append(problems, fmt.Sprintf("%q cannot be null (it is %s)", name, f.Type))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

type jsonField struct {
	name string
	typ  reflect.Type
}

func jsonFields(t reflect.Type) []jsonField {
	var out []jsonField
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			out = append(out, jsonFields(f.Type)...)
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, jsonField{name: name, typ: f.Type})
	}
	return out
}

var timeType = reflect.TypeFor[time.Time]()

func typeName(t reflect.Type) string {
	switch {
	case t == timeType:
		return "timestamp"
	case t.Kind() == reflect.Pointer:
		return typeName(t.Elem()) + " or null"
	case t.Kind() == reflect.Slice:
		return "array of " + typeName(t.Elem())
	case t.Kind() == reflect.Map:
		return "object of " + typeName(t.Elem())
	case t.Kind() == reflect.Struct:
		var names []string
		for _, f := range jsonFields(t) {
			names = append(names, f.name)
		}
		return "object {" + strings.Join(names, ", ") + "}"
	case t.Kind() == reflect.Bool:
		return "boolean"
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Uint64:
		return "integer"
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		return "number"
	case t.Kind() == reflect.String:
		return "string"
	}
	return "any"
}
