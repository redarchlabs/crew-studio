package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MCP connections (docs/agent-chat-handoffs-design.md, slice 3e): a connection of kind "mcp" points at an outside MCP
// server (Streamable HTTP). An administrator discovers the tools it offers and says what each one does; agent chat may
// use the enabled tools that only read. Everything the server says about a tool (name, description, input schema) and
// everything a tool returns is the server's words: untrusted text, kept to sizes set here.
//
// Not to be confused with Studio's own MCP server (internal/mcpserver), which Claude Code calls.

const (
	// KindMCP is a connection to an outside MCP server
	KindMCP = "mcp"
	// ConfigMCPURL is the MCP server's Streamable HTTP address
	ConfigMCPURL = "url"
	// NeedMCP is what an MCP connection meets in agent chat: its own tools, frozen per turn (never a catalog tool's need)
	NeedMCP = "mcp"

	// MaxMCPToolNameLen is the longest tool name kept (the MCP specification's limit); names also become URL segments
	MaxMCPToolNameLen = 128
	// MaxMCPToolDescriptionLen is the longest description kept, in characters; longer ones are cut
	MaxMCPToolDescriptionLen = 4000
	// MaxMCPToolSchemaBytes is the largest input schema kept; a tool with a larger one is skipped
	MaxMCPToolSchemaBytes = 16 << 10
	// MaxMCPTools is the most tools kept for one connection
	MaxMCPTools = 200
	// MaxChatMCPTools is the most MCP tools one agent-chat turn is given, over all its connections
	MaxChatMCPTools = 40
	// MCPResultMaxBytes is the most of a tool's result an agent gets; the rest is cut
	MCPResultMaxBytes = 64 << 10
	// MaxMCPArgumentsBytes is the largest arguments object a run may send to a tool
	MaxMCPArgumentsBytes = 32 << 10
)

// mcpToolNamePattern is the MCP specification's recommended tool name: letters, digits, '_', '.' and '-'. Other names
// are skipped on discovery, so a name is always safe in a URL path and a prompt.
var mcpToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// ValidMCPToolName reports whether name is a tool name Studio keeps.
func ValidMCPToolName(name string) bool { return mcpToolNamePattern.MatchString(name) }

// ConnectionTool is one tool of an MCP connection as Studio keeps it.
type ConnectionTool struct {
	ConnectionID string `json:"connection_id"`
	Name         string `json:"name"`
	// Description is the MCP server's own text: show it as text, never as markup
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	// Effect is "reads" or "changes"; only enabled tools that read are given in agent chat
	Effect        string    `json:"effect"`
	Enabled       bool      `json:"enabled"`
	DiscoveredAt  time.Time `json:"discovered_at"`
	UpdatedByName string    `json:"updated_by_name"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// DiscoveredTool is a tool as an MCP server listed it (after NormalizeDiscoveredTools, cleaned).
type DiscoveredTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	// ReadOnly is the server's readOnlyHint annotation; it only sets the effect a new tool starts with
	ReadOnly bool
}

// NormalizeDiscoveredTools keeps the tools Studio can use, cleaned (descriptions cut to size, control characters
// removed), and says why each other one was skipped: a name outside the allowed characters, a repeated name, an input
// schema that is not a JSON object or is too large, or one tool too many.
func NormalizeDiscoveredTools(in []DiscoveredTool) ([]DiscoveredTool, []string) {
	out := []DiscoveredTool{}
	skipped := []string{}
	for _, t := range in {
		name := safeToolLabel(t.Name)
		switch {
		case !ValidMCPToolName(t.Name):
			skipped = append(skipped, fmt.Sprintf("%q: the name must be 1 to %d letters, digits, '_', '.' or '-'", name, MaxMCPToolNameLen))
		case slices.ContainsFunc(out, func(o DiscoveredTool) bool { return o.Name == t.Name }):
			skipped = append(skipped, fmt.Sprintf("%q: listed twice", name))
		case len(out) >= MaxMCPTools:
			skipped = append(skipped, fmt.Sprintf("%q: more than %d tools", name, MaxMCPTools))
		default:
			schema, problem := cleanToolSchema(t.InputSchema)
			if problem != "" {
				skipped = append(skipped, fmt.Sprintf("%q: %s", name, problem))
				continue
			}
			out = append(out, DiscoveredTool{Name: t.Name, Description: CleanUntrustedText(t.Description, MaxMCPToolDescriptionLen),
				InputSchema: schema, ReadOnly: t.ReadOnly})
		}
	}
	return out, skipped
}

// cleanToolSchema is a tool's input schema compacted, or why it cannot be kept.
func cleanToolSchema(raw json.RawMessage) (json.RawMessage, string) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return json.RawMessage(`{"type":"object"}`), ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, "the input schema is not a JSON object"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, "the input schema is not valid JSON"
	}
	if buf.Len() > MaxMCPToolSchemaBytes {
		return nil, fmt.Sprintf("the input schema is larger than %d KB", MaxMCPToolSchemaBytes>>10)
	}
	return json.RawMessage(buf.Bytes()), ""
}

// CleanUntrustedText removes control characters (keeping new lines and tabs) and invalid UTF-8 from text an outside
// system wrote, and cuts it to maxRunes characters.
func CleanUntrustedText(s string, maxRunes int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxRunes {
		s = string([]rune(s)[:maxRunes])
	}
	return s
}

// safeToolLabel is a tool name fit for a message even when it was refused: one line, at most 60 characters.
func safeToolLabel(name string) string {
	return CleanUntrustedText(strings.Join(strings.Fields(name), " "), 60)
}

// ConnectionToolChange sets one tool's effect and whether it is enabled.
type ConnectionToolChange struct {
	Name    string `json:"name"`
	Effect  string `json:"effect"`
	Enabled bool   `json:"enabled"`
}

// ConnectionToolsInput is PUT /connections/{id}/tools: the tools to change (others stay as they are).
type ConnectionToolsInput struct {
	Tools []ConnectionToolChange `json:"tools"`
}

// NormalizeConnectionToolsInput checks the changes: known effects, valid names, no name twice, at most MaxMCPTools.
func NormalizeConnectionToolsInput(in ConnectionToolsInput) (ConnectionToolsInput, error) {
	var c checker
	out := ConnectionToolsInput{Tools: []ConnectionToolChange{}}
	if len(in.Tools) == 0 {
		c.addf("tools", "give at least one tool")
	}
	if len(in.Tools) > MaxMCPTools {
		c.addf("tools", "at most %d", MaxMCPTools)
		return out, c.err()
	}
	for i, t := range in.Tools {
		field := fmt.Sprintf("tools[%d]", i)
		switch {
		case !ValidMCPToolName(t.Name):
			c.addf(field+".name", "is not a tool name")
		case t.Effect != EffectReads && t.Effect != EffectChanges:
			c.addf(field+".effect", "must be reads or changes")
		case slices.ContainsFunc(out.Tools, func(o ConnectionToolChange) bool { return o.Name == t.Name }):
			c.addf(field+".name", "%s is listed twice", t.Name)
		default:
			out.Tools = append(out.Tools, t)
		}
	}
	return out, c.err()
}

// ChatMCPTool is an MCP tool frozen into an agent-chat turn: one that read and was enabled on a connection the turn
// may use when it started. The run-state API checks every call against this list and the tool's settings now.
type ChatMCPTool struct {
	ConnectionID   string          `json:"connection_id"`
	ConnectionName string          `json:"connection_name"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	InputSchema    json.RawMessage `json:"input_schema"`
}

// MCPCallInput is what a run sends to call an MCP tool: the arguments as a JSON object.
type MCPCallInput struct {
	Arguments json.RawMessage `json:"arguments"`
}

// NormalizeMCPArguments returns the arguments as a JSON object ({} when left out), or a validation error.
func NormalizeMCPArguments(raw json.RawMessage) (map[string]any, error) {
	var c checker
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return map[string]any{}, nil
	}
	if len(trimmed) > MaxMCPArgumentsBytes {
		c.addf("arguments", "must be at most %d KB", MaxMCPArgumentsBytes>>10)
		return nil, c.err()
	}
	var args map[string]any
	if err := json.Unmarshal(trimmed, &args); err != nil || args == nil {
		c.addf("arguments", "must be a JSON object")
		return nil, c.err()
	}
	return args, nil
}

// MCPCallResult is what Studio answers a run's MCP tool call with. Content is the tool's text, cut to
// MCPResultMaxBytes: untrusted, which the runner fences before an agent sees it.
type MCPCallResult struct {
	Connection string `json:"connection"`
	Tool       string `json:"tool"`
	Content    string `json:"content"`
	// IsError is true when the tool itself reported a failure (its text says why)
	IsError bool `json:"is_error"`
	// Truncated is true when the result was cut to size
	Truncated bool `json:"truncated"`
}

// FrozenMCPTool finds a tool in a turn's frozen list by connection and name.
func FrozenMCPTool(tools []ChatMCPTool, connectionID, name string) (ChatMCPTool, bool) {
	i := slices.IndexFunc(tools, func(t ChatMCPTool) bool { return t.ConnectionID == connectionID && t.Name == name })
	if i < 0 {
		return ChatMCPTool{}, false
	}
	return tools[i], true
}
