package mcpclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"crewstudio/internal/mcpclient"
	"crewstudio/internal/mcpclient/mcptest"
	"crewstudio/internal/model"
)

func TestListAndCallTools(t *testing.T) {
	fake := mcptest.New(t, mcptest.WithAuth("X-API-Key", "s3cret"))
	c := mcpclient.New()
	target := mcpclient.Target{URL: fake.URL(), AuthHeader: "X-API-Key", AuthValue: "s3cret"}
	tools, err := c.ListTools(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]model.DiscoveredTool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	if len(byName) != 4 || !byName[mcptest.ReadTool].ReadOnly || byName[mcptest.ChangeTool].ReadOnly {
		t.Fatalf("tools = %+v", tools)
	}
	if !strings.Contains(string(byName[mcptest.ReadTool].InputSchema), `"query"`) || byName[mcptest.ReadTool].Description != "Searches the notes." {
		t.Errorf("read tool = %+v", byName[mcptest.ReadTool])
	}

	res, err := c.CallTool(context.Background(), target, mcptest.ReadTool, map[string]any{"query": "atlas"})
	if err != nil || res.Content != "notes for atlas" || res.IsError || res.Truncated {
		t.Fatalf("call = %+v, %v", res, err)
	}
	calls := fake.Calls()
	if len(calls) != 1 || calls[0].Auth != "s3cret" || calls[0].Arguments["query"] != "atlas" {
		t.Errorf("calls = %+v", calls)
	}

	big, err := c.CallTool(context.Background(), target, mcptest.BigTool, nil)
	if err != nil || !big.Truncated || len(big.Content) > model.MCPResultMaxBytes || !utf8.ValidString(big.Content) {
		t.Errorf("big = %d bytes, truncated %v, %v", len(big.Content), big.Truncated, err)
	}
	failed, err := c.CallTool(context.Background(), target, mcptest.FailTool, nil)
	if err != nil || !failed.IsError || failed.Content != "the tool failed" {
		t.Errorf("fail = %+v, %v", failed, err)
	}
	_, err = c.CallTool(context.Background(), target, "no_such_tool", nil)
	var e *mcpclient.Error
	if !errors.As(err, &e) {
		t.Errorf("unknown tool = %v", err)
	}
}

func TestRefusals(t *testing.T) {
	fake := mcptest.New(t, mcptest.WithAuth("Authorization", "Bearer x"))
	c := mcpclient.New()
	_, err := c.ListTools(context.Background(), mcpclient.Target{URL: fake.URL(), AuthHeader: "Authorization", AuthValue: "Bearer wrong"})
	var e *mcpclient.Error
	if !errors.As(err, &e) || e.Status != http.StatusUnauthorized || !strings.Contains(e.Message, "refused the credentials") {
		t.Errorf("wrong key = %v", err)
	}
	// A redirect is reported, never followed (it could carry the auth header elsewhere)
	redirect := httptest.NewServer(http.RedirectHandler(fake.URL(), http.StatusTemporaryRedirect))
	defer redirect.Close()
	_, err = c.ListTools(context.Background(), mcpclient.Target{URL: redirect.URL})
	if !errors.As(err, &e) || !strings.Contains(e.Message, "redirect") {
		t.Errorf("redirect = %v", err)
	}
	// Not an MCP server
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hello")) }))
	defer plain.Close()
	if _, err := c.ListTools(context.Background(), mcpclient.Target{URL: plain.URL}); !errors.As(err, &e) {
		t.Errorf("plain = %v", err)
	}
	// Slow server: the time limit applies
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	_, err = c.WithTimeout(200*time.Millisecond).ListTools(context.Background(), mcpclient.Target{URL: slow.URL})
	if !errors.As(err, &e) || !strings.Contains(e.Message, "in time") {
		t.Errorf("slow = %v", err)
	}
	// Link-local addresses are refused when dialling
	_, err = c.WithTimeout(time.Second).ListTools(context.Background(), mcpclient.Target{URL: "http://169.254.169.254/mcp"})
	if err == nil {
		t.Error("metadata address reached")
	}
	// A huge answer fails rather than filling memory
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"x":"` + strings.Repeat("a", 5<<20) + `"}}`))
	}))
	defer huge.Close()
	if _, err := c.ListTools(context.Background(), mcpclient.Target{URL: huge.URL}); !errors.As(err, &e) {
		t.Errorf("huge = %v", err)
	}
}

// A server that never answers the closing DELETE does not hold up the call's answer.
func TestClosingIsBounded(t *testing.T) {
	fake := mcptest.New(t, mcptest.WithHangingDelete())
	start := time.Now()
	res, err := mcpclient.New().CallTool(context.Background(), mcpclient.Target{URL: fake.URL()}, mcptest.ReadTool, map[string]any{"query": "x"})
	if err != nil || res.Content != "notes for x" {
		t.Fatalf("call = %+v, %v", res, err)
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Errorf("the call took %v", d)
	}
}

func TestCheckURLAndTarget(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://mcp.example.com/mcp": true, "http://127.0.0.1:9000/mcp": true, "http://localhost/mcp": true,
		"http://mcp.example.com/mcp": false, "ftp://x/y": false, "https://u:p@x.example/mcp": false,
		"https://x.example/mcp?key=1": false, "https://x.example/mcp#f": false, "": false,
	} {
		if err := mcpclient.CheckURL(raw); (err == nil) != ok {
			t.Errorf("CheckURL(%q) = %v", raw, err)
		}
	}
	conn := model.Connection{Kind: model.KindMCP, Config: map[string]string{"url": "https://mcp.example.com/mcp", "auth_header": "X-Key"}}
	if _, err := mcpclient.TargetOf(conn, map[string]string{}); err == nil {
		t.Error("header without a value accepted")
	}
	target, err := mcpclient.TargetOf(conn, map[string]string{"auth_value": "v"})
	if err != nil || target.AuthValue != "v" || target.URL != "https://mcp.example.com/mcp" {
		t.Errorf("target = %+v, %v", target, err)
	}
	if _, err := mcpclient.TargetOf(model.Connection{Config: map[string]string{"url": "http://x.example"}}, nil); err == nil {
		t.Error("plain http to another machine accepted")
	}
}

func TestResultText(t *testing.T) {
	res := &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: "one"},
		&mcp.ImageContent{Data: []byte{1}, MIMEType: "image/png"},
		&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "x://a", Text: "two"}},
		&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "x://b", Blob: []byte{1}}},
		&mcp.AudioContent{Data: []byte{1}, MIMEType: "audio/wav"},
	}}
	text, cut := mcpclient.ResultText(res, 1000)
	if cut || text != "one\n[an image was left out]\ntwo\n[a resource that is not text was left out]\n[audio was left out]" {
		t.Errorf("text = %q", text)
	}
	structured, _ := mcpclient.ResultText(&mcp.CallToolResult{StructuredContent: map[string]any{"a": 1}}, 1000)
	if structured != `{"a":1}` {
		t.Errorf("structured = %q", structured)
	}
	short, cut := mcpclient.ResultText(&mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ééé"}}}, 3)
	if !cut || short != "é" {
		t.Errorf("short = %q %v", short, cut)
	}
}
