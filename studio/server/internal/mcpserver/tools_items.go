package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type kindArgs struct {
	Kind string `json:"kind" jsonschema:"the kind of item"`
}

type listArgs struct {
	Kind         string `json:"kind" jsonschema:"the kind of item"`
	NameContains string `json:"name_contains,omitempty" jsonschema:"only items whose name (or model, or key) contains this, ignoring case"`
}

type itemArgs struct {
	Kind string `json:"kind" jsonschema:"the kind of item"`
	ID   string `json:"id" jsonschema:"the item's id (a UUID)"`
}

type createArgs struct {
	Kind   string         `json:"kind" jsonschema:"the kind of item"`
	Fields map[string]any `json:"fields" jsonschema:"the new item's fields (describe_kind lists them; leave out read-only ones)"`
	ID     string         `json:"id,omitempty" jsonschema:"only for kind watch: the id of the schedule the watch belongs to"`
}

type updateArgs struct {
	Kind              string         `json:"kind" jsonschema:"the kind of item"`
	ID                string         `json:"id" jsonschema:"the item's id (a UUID)"`
	Changes           map[string]any `json:"changes" jsonschema:"only the fields to change, with their new values; null clears a field whose type allows null; a list or object replaces the old one whole"`
	ExpectedUpdatedAt string         `json:"expected_updated_at,omitempty" jsonschema:"the updated_at you read; the update is refused if the item changed since (checked just before saving)"`
	DryRun            bool           `json:"dry_run,omitempty" jsonschema:"show what would change without saving (the API's validation only runs on a real save)"`
}

type updateResult struct {
	Saved     bool          `json:"saved"`
	Changes   []FieldChange `json:"changes"`
	UpdatedAt string        `json:"updated_at,omitempty"`
}

// schemaFor infers T's input schema and limits the named string properties to the given values.
func schemaFor[T any](enums map[string][]string) *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("schema for %T: %v", *new(T), err))
	}
	for name, values := range enums {
		prop := s.Properties[name]
		if prop == nil {
			panic(fmt.Sprintf("schema for %T has no property %q", *new(T), name))
		}
		for _, v := range values {
			prop.Enum = append(prop.Enum, v)
		}
	}
	return s
}

func ptr[T any](v T) *T { return &v }

func (t *tools) addItemTools(s *mcp.Server) {
	all := map[string][]string{"kind": kindNames(false)}
	writable := map[string][]string{"kind": kindNames(true)}
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}

	mcp.AddTool(s, &mcp.Tool{Name: "describe_kind", Title: "Describe a kind of item", Annotations: readOnly,
		Description: "List a kind's fields with their types, which ones Crew Studio sets itself, and hints on allowed values.",
		InputSchema: schemaFor[kindArgs](all)}, t.describeKind)
	mcp.AddTool(s, &mcp.Tool{Name: "list_items", Title: "List items", Annotations: readOnly,
		Description: "List the items of a kind you can see, with their main fields. Use get_item for everything about one.",
		InputSchema: schemaFor[listArgs](all)}, t.listItems)
	mcp.AddTool(s, &mcp.Tool{Name: "get_item", Title: "Get an item", Annotations: readOnly,
		Description: "Read one item with all its fields.", InputSchema: schemaFor[itemArgs](all)}, t.getItem)
	mcp.AddTool(s, &mcp.Tool{Name: "create_item", Title: "Create an item",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(false)},
		Description: "Create an item. Names must be unique within a kind.", InputSchema: schemaFor[createArgs](writable)}, t.createItem)
	mcp.AddTool(s, &mcp.Tool{Name: "update_item", Title: "Change an item",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false)},
		Description: "Change some fields of an item and leave the rest as they are. Returns each changed field before and after. " +
			"Always pass expected_updated_at: without it, an edit someone saved since you read the item is overwritten. " +
			"Saving a schedule makes you the person it runs as.",
		InputSchema: schemaFor[updateArgs](writable)}, t.updateItem)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_item", Title: "Delete an item",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)},
		Description: "Delete an item. Refused while other items still use it. Its history stays.",
		InputSchema: schemaFor[itemArgs](writable)}, t.deleteItem)
}

func (t *tools) describeKind(_ context.Context, _ *mcp.CallToolRequest, in kindArgs) (*mcp.CallToolResult, any, error) {
	k, err := kindNamed(in.Kind)
	if err != nil {
		return nil, nil, err
	}
	return text(k.describe())
}

func (t *tools) listItems(ctx context.Context, req *mcp.CallToolRequest, in listArgs) (*mcp.CallToolResult, any, error) {
	k, err := kindNamed(in.Kind)
	if err != nil {
		return nil, nil, err
	}
	if k.listHint != "" {
		return nil, nil, fmt.Errorf("%s", k.listHint)
	}
	data, err := t.call(ctx, req, http.MethodGet, k.path, nil)
	if err != nil {
		return nil, nil, err
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, nil, fmt.Errorf("read %s list: %w", k.name, err)
	}
	needle := strings.ToLower(strings.TrimSpace(in.NameContains))
	out := []map[string]any{}
	for _, item := range items {
		if needle != "" && !nameMatches(item, needle) {
			continue
		}
		summary := make(map[string]any, len(k.summary))
		for _, f := range k.summary {
			summary[f] = item[f]
		}
		out = append(out, summary)
	}
	return text(out)
}

func nameMatches(item map[string]any, needle string) bool {
	for _, f := range []string{"name", "model", "key"} {
		if s, ok := item[f].(string); ok && strings.Contains(strings.ToLower(s), needle) {
			return true
		}
	}
	return false
}

// itemPath checks the kind and id and returns the item's API path.
func itemPath(kindName, id string, needWritable bool) (kind, string, error) {
	k, err := kindNamed(kindName)
	if err != nil {
		return k, "", err
	}
	if needWritable && !k.writable {
		return k, "", fmt.Errorf("%s items cannot be changed here; writable kinds are %s", k.name, strings.Join(kindNames(true), ", "))
	}
	if !k.getByID {
		return k, "", fmt.Errorf("%s items are only listed: use list_items", k.name)
	}
	if err := checkID("id", id); err != nil {
		return k, "", err
	}
	return k, k.pathOf(id), nil
}

// item reads one item, taking it out of the answer where the API wraps it (a schedule's watch view).
func (t *tools) item(ctx context.Context, req *mcp.CallToolRequest, k kind, path string) (map[string]any, error) {
	obj, err := t.object(ctx, req, path)
	if err != nil || k.unwrap == "" {
		return obj, err
	}
	inner, ok := obj[k.unwrap].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("there is no %s here yet (create_item kind %q makes one)", k.name, k.name)
	}
	return inner, nil
}

func (t *tools) getItem(ctx context.Context, req *mcp.CallToolRequest, in itemArgs) (*mcp.CallToolResult, any, error) {
	k, path, err := itemPath(in.Kind, in.ID, false)
	if err != nil {
		return nil, nil, err
	}
	if k.unwrap != "" {
		obj, err := t.item(ctx, req, k, path)
		if err != nil {
			return nil, nil, err
		}
		return text(obj)
	}
	data, err := t.call(ctx, req, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	return rawText(data)
}

func (t *tools) createItem(ctx context.Context, req *mcp.CallToolRequest, in createArgs) (*mcp.CallToolResult, any, error) {
	k, err := kindNamed(in.Kind)
	if err != nil {
		return nil, nil, err
	}
	if !k.writable {
		return nil, nil, fmt.Errorf("%s items cannot be created here; writable kinds are %s", k.name, strings.Join(kindNames(true), ", "))
	}
	if k.refuse != nil {
		if err := k.refuse(in.Fields); err != nil {
			return nil, nil, err
		}
	}
	if err := k.checkFields(in.Fields); err != nil {
		return nil, nil, err
	}
	method, path := http.MethodPost, k.path
	switch {
	case k.createByID:
		if err := checkID("id", in.ID); err != nil {
			return nil, nil, fmt.Errorf("a %s is created for a schedule: %w", k.name, err)
		}
		method, path = http.MethodPut, k.pathOf(in.ID)
	case in.ID != "":
		return nil, nil, fmt.Errorf("id only applies to kind watch; a new %s gets its id from Crew Studio", k.name)
	}
	data, err := t.call(ctx, req, method, path, in.Fields)
	if err != nil {
		return nil, nil, err
	}
	if k.unwrap != "" {
		saved, err := k.unwrapData(data)
		if err != nil {
			return nil, nil, err
		}
		return text(saved)
	}
	return rawText(data)
}

// unwrapData reads a saved item from an API answer, taking it out of its wrapper where the kind has one.
func (k kind) unwrapData(data json.RawMessage) (map[string]any, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("read saved %s: %w", k.name, err)
	}
	if k.unwrap == "" {
		return obj, nil
	}
	inner, ok := obj[k.unwrap].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the API did not return the saved %s", k.name)
	}
	return inner, nil
}

func (t *tools) updateItem(ctx context.Context, req *mcp.CallToolRequest, in updateArgs) (*mcp.CallToolResult, any, error) {
	k, path, err := itemPath(in.Kind, in.ID, true)
	if err != nil {
		return nil, nil, err
	}
	if k.refuse != nil {
		if err := k.refuse(in.Changes); err != nil {
			return nil, nil, err
		}
	}
	current, err := t.item(ctx, req, k, path)
	if err != nil {
		return nil, nil, err
	}
	stamp, _ := current["updated_at"].(string)
	if in.ExpectedUpdatedAt != "" && !sameInstant(in.ExpectedUpdatedAt, stamp) {
		return nil, nil, fmt.Errorf("this %s changed since you read it (updated_at is now %s, you expected %s): "+
			"read it again with get_item and redo the change", k.name, stamp, in.ExpectedUpdatedAt)
	}
	merged, err := k.applyChanges(current, in.Changes)
	if err != nil {
		return nil, nil, err
	}
	if in.DryRun {
		return text(updateResult{Changes: diff(current, merged), UpdatedAt: stamp})
	}
	data, err := t.call(ctx, req, http.MethodPut, path, merged)
	if err != nil {
		return nil, nil, err
	}
	saved, err := k.unwrapData(data)
	if err != nil {
		return nil, nil, err
	}
	newStamp, _ := saved["updated_at"].(string)
	return text(updateResult{Saved: true, Changes: diff(current, saved), UpdatedAt: newStamp})
}

func (t *tools) deleteItem(ctx context.Context, req *mcp.CallToolRequest, in itemArgs) (*mcp.CallToolResult, any, error) {
	k, path, err := itemPath(in.Kind, in.ID, true)
	if err != nil {
		return nil, nil, err
	}
	if _, err := t.call(ctx, req, http.MethodDelete, path, nil); err != nil {
		return nil, nil, err
	}
	return text(map[string]string{"deleted": k.name, "id": in.ID})
}
