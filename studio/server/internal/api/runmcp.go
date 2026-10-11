package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"crewstudio/internal/mcpclient"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// MCP read tools in agent chat (docs/agent-chat-handoffs-design.md, slice 3e). The runner builds one tool per entry of
// the turn's frozen chat.mcp_tools and calls this route with the run token; Studio checks the call against the frozen
// list, the connection (still enabled, still visible to the person and shared with the agent) and the tool (still
// enabled and marked Reads), then calls the MCP server with the stored secret. The result is cut to 64 KB and journalled;
// the runner fences it as untrusted.

const (
	// Per run: an agent loop that calls one tool over and over is stopped
	mcpCallsPerMinute = 30
	mcpCallBurst      = 10
	// Per connection, over every run: one runaway chat cannot flood a server others rely on
	mcpConnectionCallsPerMinute = 120
	mcpConnectionCallBurst      = 30
	maxOpenMCPCalls             = 4
)

var (
	mcpRunCalls        = newKeyedLimiter(rate.Every(time.Minute/mcpCallsPerMinute), mcpCallBurst)
	mcpConnectionCalls = newKeyedLimiter(rate.Every(time.Minute/mcpConnectionCallsPerMinute), mcpConnectionCallBurst)
	// Calls waiting on one server at once, over every chat: each may take up to 25 seconds
	mcpCallsOpen = &counter{counts: map[string]int{}, max: maxOpenMCPCalls}
)

// mcpCallPattern is the only form of the route the allowlist for crew-less runs lets through: a lower-case uuid and a
// tool name of the allowed characters (runMCPCall checks both again).
const mcpCallPattern = "/mcp/{connection_id:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}}/tools/{name:[A-Za-z0-9_.-]{1,128}}"

// mcpRunRoutes hangs off /run-state (see runStateRoutes).
func (s *Server) mcpRunRoutes(r chi.Router) {
	r.Post("/mcp/{connection_id}/tools/{name}", s.runMCPCall)
}

func (s *Server) runMCPCall(w http.ResponseWriter, r *http.Request) {
	const tool = "mcp_call"
	call := runCallOf(r)
	connectionID, name := chi.URLParam(r, "connection_id"), chi.URLParam(r, "name")
	if call.scope.ChatAgentID == "" || call.scope.CrewID != "" || !model.IsChatKind(call.scope.Kind) {
		s.journal(r, tool, "MCP: refused, only agent chat may call MCP tools", "")
		fail(w, http.StatusForbidden, "forbidden", "MCP tools are only given in agent chat.")
		return
	}
	if !uuidPattern.MatchString(connectionID) || !model.ValidMCPToolName(name) {
		fail(w, http.StatusNotFound, "not_found", "There is no such MCP tool.")
		return
	}
	var in model.MCPCallInput
	if !decode(w, r, &in) {
		return
	}
	args, err := model.NormalizeMCPArguments(in.Arguments)
	if err != nil {
		s.journal(r, tool, "MCP: call of "+name+" refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	frozen, conn, allowed := s.mcpCallAllowed(w, r, connectionID, name)
	if !allowed {
		return
	}
	if !mcpRunCalls.allow(call.scope.RunID) || !mcpConnectionCalls.allow(connectionID) {
		s.journal(r, tool, "MCP: refused, too many calls to "+frozen.Name+" just now", "")
		tooMany(w, "This chat is calling MCP tools too often; use what you already found, or wait a minute.")
		return
	}
	if !mcpCallsOpen.acquire(connectionID) {
		s.journal(r, tool, "MCP: refused, "+conn.Name+" already has calls waiting", "")
		tooMany(w, "Other chats are waiting on "+conn.Name+"; wait a moment.")
		return
	}
	defer mcpCallsOpen.release(connectionID)
	target, err := s.mcpTarget(r.Context(), conn)
	var res mcpclient.CallResult
	if err == nil {
		res, err = s.mcp.CallTool(r.Context(), target, name, args)
	}
	if err != nil {
		s.journal(r, tool, fmt.Sprintf("MCP: %s on %s failed: %s", name, conn.Name, mcpProblem(err)), argumentsDetail(args))
		s.mcpFailure(w, r, err)
		return
	}
	outcome := fmt.Sprintf("%d bytes", len(res.Content))
	if res.Truncated {
		outcome += fmt.Sprintf(", cut to %d KB", model.MCPResultMaxBytes>>10)
	}
	if res.IsError {
		outcome = "the tool reported a failure"
	}
	s.journal(r, tool, fmt.Sprintf("MCP: called %s on %s: %s", name, conn.Name, outcome), argumentsDetail(args))
	ok(w, model.MCPCallResult{Connection: conn.Name, Tool: name, Content: res.Content, IsError: res.IsError, Truncated: res.Truncated})
}

// mcpCallAllowed checks a call against the run's frozen tools, its connections as they are now, and the tool's
// settings now. It answers and journals itself when the call is refused.
func (s *Server) mcpCallAllowed(w http.ResponseWriter, r *http.Request, connectionID, name string) (model.ChatMCPTool, model.Connection, bool) {
	const tool = "mcp_call"
	call := runCallOf(r)
	spec, err := s.store.GetRunSpec(r.Context(), call.scope.RunID)
	if err != nil {
		failErr(w, r, s.log, err)
		return model.ChatMCPTool{}, model.Connection{}, false
	}
	var frozen model.ChatMCPTool
	found := false
	if spec.Chat != nil {
		frozen, found = model.FrozenMCPTool(spec.Chat.MCPTools, connectionID, name)
	}
	if !found {
		s.journal(r, tool, "MCP: refused, this chat was not given "+name, "")
		fail(w, http.StatusForbidden, "forbidden", "This chat was not given that MCP tool.")
		return frozen, model.Connection{}, false
	}
	// Frozen, enabled, visible to the person and shared with the agent: the same rule as tracker and KM2 reads
	conns, err := s.runConnections(r.Context(), call.scope)
	if err != nil {
		failErr(w, r, s.log, err)
		return frozen, model.Connection{}, false
	}
	if !slices.ContainsFunc(conns, func(c store.RunConnection) bool { return c.ID == connectionID && c.Kind == model.KindMCP }) {
		s.journal(r, tool, "MCP: refused, the connection of "+name+" is off or no longer available to this chat", "")
		fail(w, http.StatusConflict, "no_mcp", "The MCP connection "+frozen.ConnectionName+" is turned off or no longer available to this chat.")
		return frozen, model.Connection{}, false
	}
	reads, err := s.store.MCPToolReads(r.Context(), connectionID, name)
	if err != nil {
		failErr(w, r, s.log, err)
		return frozen, model.Connection{}, false
	}
	if !reads {
		s.journal(r, tool, "MCP: refused, "+name+" is turned off or no longer marked Reads", "")
		fail(w, http.StatusForbidden, "mcp_tool_off", "An administrator turned "+name+" off or marked it as changing things; it cannot be used in chat.")
		return frozen, model.Connection{}, false
	}
	conn, err := s.store.GetConnection(r.Context(), connectionID)
	if err != nil {
		failErr(w, r, s.log, err)
		return frozen, model.Connection{}, false
	}
	return frozen, conn, true
}

// mcpProblem is a failure for a journal line.
func mcpProblem(err error) string {
	var e *mcpclient.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return problemText(err)
}

// argumentsDetail names the call's arguments and their size for the journal. Their values may hold what the person
// pasted or the agent read, and the journal is read more widely than the chat, so they are left out.
func argumentsDetail(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	names := make([]string, 0, len(args))
	for k := range args {
		names = append(names, model.CleanUntrustedText(k, 40))
	}
	slices.Sort(names)
	raw, _ := json.Marshal(args)
	return fmt.Sprintf("Arguments: %s (%d bytes)", km2Clip(strings.Join(names, ", "), 200), len(raw))
}
