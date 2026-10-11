// Package mcptest is a small outside MCP server for tests of MCP connections: it offers a reading tool, a changing tool,
// a tool with a huge result and a failing tool, and can require an auth header.
package mcptest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names the server offers.
const (
	// ReadTool echoes its "query" argument: "notes for <query>"
	ReadTool = "search_notes"
	// ChangeTool would delete a note; it has no readOnlyHint
	ChangeTool = "delete_note"
	// BigTool returns 100 KB of text
	BigTool = "big_result"
	// FailTool reports a failure (isError)
	FailTool = "fail_tool"
)

// Server is a running fake MCP server.
type Server struct {
	*httptest.Server
	mu    sync.Mutex
	calls []Call
	// Header and Value, when set, must be on every request (otherwise HTTP 401)
	header, value string
	// Extra tools added before the server started
	extra []*mcp.Tool
	// hangDelete makes the closing DELETE hang until the client gives up
	hangDelete bool
}

// Call is one tools/call the server received.
type Call struct {
	Name      string
	Arguments map[string]any
	// Auth is the auth header the request carried
	Auth string
}

// Option changes the fake server.
type Option func(*Server)

// WithAuth makes the server refuse requests without header: value.
func WithAuth(header, value string) Option {
	return func(s *Server) { s.header, s.value = header, value }
}

// WithHangingDelete makes the server never answer the DELETE that closes a session.
func WithHangingDelete() Option {
	return func(s *Server) { s.hangDelete = true }
}

// WithTool adds a tool (answered like ReadTool).
func WithTool(t *mcp.Tool) Option {
	return func(s *Server) { s.extra = append(s.extra, t) }
}

// New starts the server; it stops when the test ends.
func New(t interface{ Cleanup(func()) }, opts ...Option) *Server {
	s := &Server{}
	for _, o := range opts {
		o(s)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "fake-mcp", Version: "1.0"}, nil)
	schema := map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "description": "What to look for"}},
		"required": []string{"query"}}
	server.AddTool(&mcp.Tool{Name: ReadTool, Description: "Searches the notes.", InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, s.handle)
	server.AddTool(&mcp.Tool{Name: ChangeTool, Description: "Deletes a note.", InputSchema: map[string]any{"type": "object"}}, s.handle)
	server.AddTool(&mcp.Tool{Name: BigTool, Description: "Returns a lot.", InputSchema: map[string]any{"type": "object"},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, s.handle)
	server.AddTool(&mcp.Tool{Name: FailTool, Description: "Always fails.", InputSchema: map[string]any{"type": "object"},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, s.handle)
	for _, tool := range s.extra {
		server.AddTool(tool, s.handle)
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true})
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.hangDelete && r.Method == http.MethodDelete {
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			return
		}
		if s.header != "" && r.Header.Get(s.header) != s.value {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authKey{}, r.Header.Get(s.header))))
	}))
	t.Cleanup(s.Close)
	return s
}

type authKey struct{}

// URL is the server's MCP address.
func (s *Server) URL() string { return s.Server.URL + "/mcp" }

// Calls are the tool calls received so far.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

func (s *Server) handle(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := map[string]any{}
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &args)
	}
	auth, _ := ctx.Value(authKey{}).(string)
	s.mu.Lock()
	s.calls = append(s.calls, Call{Name: req.Params.Name, Arguments: args, Auth: auth})
	s.mu.Unlock()
	switch req.Params.Name {
	case BigTool:
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("é", 50<<10)}}}, nil
	case FailTool:
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "the tool failed"}}}, nil
	}
	query, _ := args["query"].(string)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "notes for " + query}}}, nil
}
