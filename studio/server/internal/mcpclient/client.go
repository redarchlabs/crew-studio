// Package mcpclient calls outside MCP servers for MCP connections (docs/agent-chat-handoffs-design.md, slice 3e):
// it lists a server's tools for Discover tools and Test, and calls one reading tool for an agent-chat run. Studio, never
// the runner, makes these calls, with the connection's secret in the auth header it names.
//
// Not to be confused with internal/mcpserver, Studio's own MCP server for Claude Code.
//
// Every call is bounded: a time limit, no redirects, no proxy, no link-local or cloud metadata addresses (the same
// dialer as http connections), a cap on each response body, and results cut to model.MCPResultMaxBytes.
package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
)

const (
	// DefaultTimeout bounds one whole exchange (connect, initialize, and the list or the call); it is under the
	// run-state API's 30-second request limit, so a slow server still gets a clear answer
	DefaultTimeout = 25 * time.Second
	// maxResponseBytes bounds each response body the server sends; a tool list or result larger than this fails
	maxResponseBytes = 4 << 20
	// maxListPages bounds how many pages of tools/list are read
	maxListPages = 10
	// closeTimeout bounds the DELETE that ends a session
	closeTimeout = 3 * time.Second
	// userAgent names Studio to the MCP server
	userAgent = "CrewStudio/1.0 (MCP connection)"
)

// Target is where and how to reach one MCP server: its URL and, when set, the auth header with its value.
type Target struct {
	URL        string
	AuthHeader string
	AuthValue  string
}

// TargetOf is the target of an MCP connection with its opened secrets.
func TargetOf(c model.Connection, secrets map[string]string) (Target, error) {
	t := Target{URL: c.Config[model.ConfigMCPURL], AuthHeader: c.Config["auth_header"], AuthValue: secrets["auth_value"]}
	if err := CheckURL(t.URL); err != nil {
		return Target{}, err
	}
	if t.AuthHeader != "" && t.AuthValue == "" {
		return Target{}, &Error{Message: "An auth header name is set but no auth header value is saved. Replace the secret first."}
	}
	return t, nil
}

// CheckURL accepts an https address, or plain http to this machine only (for local servers and tests), with no user
// name, query string or fragment.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && !(u.Scheme == "http" && model.LoopbackHTTP(u))) {
		return &Error{Message: "The MCP server URL must be an https address such as https://mcp.example.com/mcp (plain http only to this machine)."}
	}
	return nil
}

// Error is a failure with a message safe to show (it never quotes secrets). Detail, when set, is the MCP server's own
// words (a JSON-RPC error message), which callers treat as untrusted text.
type Error struct {
	Message string
	Detail  string
	// Status is the HTTP status that caused the failure, when there was one
	Status int
}

func (e *Error) Error() string { return e.Message }

// Client reaches MCP servers. The zero value is not usable; use New.
type Client struct {
	timeout   time.Duration
	transport http.RoundTripper
}

// New builds a client with the http connections' transport (no proxy; link-local, multicast and metadata addresses
// refused when dialling).
func New() *Client { return &Client{timeout: DefaultTimeout, transport: connections.Transport()} }

// WithTimeout returns a copy of the client with another time limit (tests use short ones).
func (c *Client) WithTimeout(d time.Duration) *Client {
	out := *c
	out.timeout = d
	return &out
}

// ListTools connects to the server and lists the tools it offers, as the server describes them (not yet cleaned; see
// model.NormalizeDiscoveredTools).
func (c *Client) ListTools(ctx context.Context, t Target) ([]model.DiscoveredTool, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	session, rt, err := c.connect(ctx, t)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	out := []model.DiscoveredTool{}
	cursor := ""
	for page := 0; page < maxListPages; page++ {
		res, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, describe(ctx, err, rt)
		}
		for _, tool := range res.Tools {
			if tool == nil {
				continue
			}
			schema, _ := json.Marshal(tool.InputSchema)
			out = append(out, model.DiscoveredTool{Name: tool.Name, Description: tool.Description, InputSchema: schema,
				ReadOnly: tool.Annotations != nil && tool.Annotations.ReadOnlyHint})
		}
		if res.NextCursor == "" || len(out) > model.MaxMCPTools {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

// CallResult is one tool call's outcome: its text (cut to size) and whether the tool reported a failure.
type CallResult struct {
	Content   string
	IsError   bool
	Truncated bool
}

// CallTool connects to the server and calls one tool with the arguments.
func (c *Client) CallTool(ctx context.Context, t Target, name string, args map[string]any) (CallResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	session, rt, err := c.connect(ctx, t)
	if err != nil {
		return CallResult{}, err
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return CallResult{}, describe(ctx, err, rt)
	}
	text, truncated := ResultText(res, model.MCPResultMaxBytes)
	return CallResult{Content: text, IsError: res.IsError, Truncated: truncated}, nil
}

// connect opens a session: the MCP initialize handshake over Streamable HTTP, without the standalone event stream
// (Studio only makes requests) and without reconnecting.
func (c *Client) connect(ctx context.Context, t Target) (*mcp.ClientSession, *roundTripper, error) {
	if err := CheckURL(t.URL); err != nil {
		return nil, nil, err
	}
	rt := &roundTripper{next: c.transport, header: t.AuthHeader, value: t.AuthValue}
	httpClient := &http.Client{
		Transport: rt,
		// Also bounds the session's closing DELETE, which the SDK sends without a deadline
		Timeout: c.timeout,
		// A redirect could carry the auth header elsewhere; it is reported, never followed
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	transport := &mcp.StreamableClientTransport{Endpoint: t.URL, HTTPClient: httpClient, MaxRetries: -1, DisableStandaloneSSE: true}
	client := mcp.NewClient(&mcp.Implementation{Name: "crew-studio", Title: "Crew Studio", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, describe(ctx, err, rt)
	}
	return session, rt, nil
}

// describe turns a failure into a message that says why in general terms (the server's address is the admin's own,
// but a call must not become a port scanner), keeping a JSON-RPC error's message as untrusted detail.
func describe(ctx context.Context, err error, rt *roundTripper) error {
	status := 0
	if rt != nil {
		status = int(rt.lastStatus.Load())
	}
	var rpc *jsonrpc.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &Error{Message: "The MCP server did not answer in time."}
	case errors.Is(err, connections.ErrBlockedAddress):
		return &Error{Message: "The MCP server's address is not allowed (link-local, multicast or cloud metadata)."}
	case errors.Is(err, errTooLarge):
		return &Error{Message: fmt.Sprintf("The MCP server sent more than %d MB in one answer.", maxResponseBytes>>20)}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return &Error{Status: status, Message: fmt.Sprintf("The MCP server refused the credentials (HTTP %d). Check the auth header and replace the secret.", status)}
	case status >= 300 && status < 400:
		return &Error{Status: status, Message: fmt.Sprintf("The MCP server answered with a redirect (HTTP %d). Use the final address as the server URL.", status)}
	case status != 0:
		return &Error{Status: status, Message: fmt.Sprintf("The MCP server answered HTTP %d.", status)}
	case errors.As(err, &rpc):
		return &Error{Message: "The MCP server answered with an error.", Detail: model.CleanUntrustedText(rpc.Message, 500)}
	}
	return &Error{Message: "Could not reach the MCP server, or it did not speak MCP (Streamable HTTP)."}
}

// ResultText is a tool result as text: its text parts in order, the text of embedded text resources, a note for each
// part that is not text, or the structured result as JSON when there is nothing else. It is cut to maxBytes at a
// character boundary.
func ResultText(res *mcp.CallToolResult, maxBytes int) (string, bool) {
	var b strings.Builder
	add := func(s string) {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	for _, part := range res.Content {
		switch p := part.(type) {
		case *mcp.TextContent:
			add(p.Text)
		case *mcp.EmbeddedResource:
			if p.Resource != nil && p.Resource.Text != "" {
				add(p.Resource.Text)
			} else {
				add("[a resource that is not text was left out]")
			}
		case *mcp.ImageContent:
			add("[an image was left out]")
		case *mcp.AudioContent:
			add("[audio was left out]")
		default:
			add("[content that is not text was left out]")
		}
		if b.Len() > maxBytes {
			break
		}
	}
	if b.Len() == 0 && res.StructuredContent != nil {
		if raw, err := json.Marshal(res.StructuredContent); err == nil {
			b.Write(raw)
		}
	}
	return clip(strings.ToValidUTF8(b.String(), ""), maxBytes)
}

// clip cuts s to at most maxBytes without splitting a character.
func clip(s string, maxBytes int) (string, bool) {
	if len(s) <= maxBytes {
		return s, false
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

var errTooLarge = errors.New("mcp response too large")

// roundTripper adds the auth header and a user agent, caps each response body, and remembers the last error status
// (the SDK reports it only as text).
type roundTripper struct {
	next          http.RoundTripper
	header, value string
	lastStatus    atomic.Int32
}

func (r *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", userAgent)
	if r.header != "" {
		req.Header.Set(r.header, r.value)
	}
	if req.Method == http.MethodDelete {
		return r.closeSession(req)
	}
	resp, err := r.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		r.lastStatus.Store(int32(resp.StatusCode))
	}
	resp.Body = &limitedBody{rc: resp.Body, left: maxResponseBytes}
	return resp, nil
}

// closeSession sends the SDK's closing DELETE with a short deadline of its own and drains its answer (the SDK neither
// bounds it nor closes the body), so closing never holds up a call's answer.
func (r *roundTripper) closeSession(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(req.Context(), closeTimeout)
	defer cancel()
	resp, err := r.next.RoundTrip(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	resp.Body = http.NoBody
	return resp, nil
}

// limitedBody fails a read once more than its limit has been read.
type limitedBody struct {
	rc   io.ReadCloser
	left int64
}

func (l *limitedBody) Read(p []byte) (int, error) {
	if l.left <= 0 {
		// At the limit: an answer that ends here is fine, one more byte is not
		var one [1]byte
		n, err := l.rc.Read(one[:])
		if n > 0 {
			return 0, errTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.rc.Read(p)
	l.left -= int64(n)
	return n, err
}

func (l *limitedBody) Close() error { return l.rc.Close() }
