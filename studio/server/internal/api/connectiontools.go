package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"crewstudio/internal/auth"
	"crewstudio/internal/connections"
	"crewstudio/internal/mcpclient"
	"crewstudio/internal/model"
)

// The tools of MCP connections (docs/agent-chat-handoffs-design.md, slice 3e). Discover tools asks the server for its
// tool list and keeps it; an administrator then marks each tool Reads or Changes and turns tools off. Agent chat may use
// enabled tools that read. Studio calls the server itself with the stored secret, which never leaves the server.

const (
	// mcpDiscoversPerMinute bounds Discover tools and Test on MCP connections, per person
	mcpDiscoversPerMinute = 10
	mcpDiscoverBurst      = 5
)

var mcpDiscovers = newKeyedLimiter(rate.Every(time.Minute/mcpDiscoversPerMinute), mcpDiscoverBurst)

// connectionToolRoutes hang off /connections (see connectionRoutes).
func (s *Server) connectionToolRoutes(r chi.Router) {
	r.With(s.need(auth.ConnectionsView)).Get("/{id}/tools", s.listConnectionTools)
	r.With(s.need(auth.ConnectionsManage)).Post("/{id}/tools/discover", s.discoverConnectionTools)
	r.With(s.need(auth.ConnectionsManage)).Put("/{id}/tools", s.setConnectionTools)
}

// mcpConnection answers 422 itself when c is not an MCP connection.
func mcpConnection(w http.ResponseWriter, c model.Connection) bool {
	if c.Kind != model.KindMCP {
		fail(w, http.StatusUnprocessableEntity, "not_mcp", c.Name+" is not an MCP connection, so it has no tools.")
		return false
	}
	return true
}

func (s *Server) listConnectionTools(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadConnection(w, r)
	if !found || !mcpConnection(w, c) {
		return
	}
	tools, err := s.store.ConnectionTools(r.Context(), c.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, tools)
}

type discoverToolsView struct {
	Tools []model.ConnectionTool `json:"tools"`
	// Found is how many tools the server listed; Skipped says why each one not kept was left out
	Found   int      `json:"found"`
	Skipped []string `json:"skipped"`
	// Review names the tools that are new since the last discovery or describe themselves differently: they start as
	// Changes until an administrator marks them Reads
	Review []string `json:"review"`
}

func (s *Server) discoverConnectionTools(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadOwnedConnection(w, r)
	if !found || !mcpConnection(w, c) {
		return
	}
	if !mcpDiscovers.allow(principal(r).UserID) {
		tooMany(w, "MCP servers are being asked for their tools too quickly; wait a moment.")
		return
	}
	listed, err := s.listMCPTools(r.Context(), c)
	if err != nil {
		s.mcpFailure(w, r, err)
		return
	}
	kept, skipped := model.NormalizeDiscoveredTools(listed)
	tools, review, err := s.store.SaveDiscoveredTools(r.Context(), c.ID, kept)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditConnection(r, "connection_tools_discovered", c, map[string]any{"found": len(listed), "kept": len(kept), "skipped": len(skipped),
		"review": review})
	ok(w, discoverToolsView{Tools: tools, Found: len(listed), Skipped: skipped, Review: review})
}

func (s *Server) setConnectionTools(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadOwnedConnection(w, r)
	if !found || !mcpConnection(w, c) {
		return
	}
	var in model.ConnectionToolsInput
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeConnectionToolsInput(in)
	var tools []model.ConnectionTool
	if err == nil {
		tools, err = s.store.UpdateConnectionTools(r.Context(), c.ID, clean.Tools)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	changes := make([]map[string]any, 0, len(clean.Tools))
	for _, t := range clean.Tools {
		changes = append(changes, map[string]any{"name": t.Name, "effect": t.Effect, "enabled": t.Enabled})
	}
	s.auditConnection(r, "connection_tools_updated", c, map[string]any{"tools": changes})
	ok(w, tools)
}

// listMCPTools opens the connection's secret and lists the server's tools.
func (s *Server) listMCPTools(ctx context.Context, c model.Connection) ([]model.DiscoveredTool, error) {
	target, err := s.mcpTarget(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.mcp.ListTools(ctx, target)
}

// mcpTarget is where and how to reach an MCP connection's server, with its secret opened.
func (s *Server) mcpTarget(ctx context.Context, c model.Connection) (mcpclient.Target, error) {
	secrets, err := s.conns.Secrets(ctx, c.ID)
	if err != nil {
		return mcpclient.Target{}, err
	}
	return mcpclient.TargetOf(c, secrets)
}

// mcpFailure answers a failed MCP exchange: the reason, and the server's own words (untrusted) as a detail.
func (s *Server) mcpFailure(w http.ResponseWriter, r *http.Request, err error) {
	var e *mcpclient.Error
	if !errors.As(err, &e) {
		failErr(w, r, s.log, err)
		return
	}
	details := []string{}
	if e.Detail != "" {
		details = append(details, e.Detail)
	}
	fail(w, http.StatusBadGateway, "mcp_failed", e.Message, details...)
}

// testMCPConnection connects to the server and counts its tools, without keeping them (Discover tools keeps them).
func (s *Server) testMCPConnection(w http.ResponseWriter, r *http.Request, c model.Connection) {
	if !mcpDiscovers.allow(principal(r).UserID) {
		tooMany(w, "MCP servers are being tested too quickly; wait a moment.")
		return
	}
	start := time.Now()
	listed, err := s.listMCPTools(r.Context(), c)
	res := connections.TestResult{Supported: true, DurationMS: time.Since(start).Milliseconds()}
	var e *mcpclient.Error
	switch {
	case errors.As(err, &e):
		res.Message, res.StatusCode = e.Message, e.Status
	case err != nil:
		failErr(w, r, s.log, err)
		return
	default:
		readOnly := 0
		for _, t := range listed {
			if t.ReadOnly {
				readOnly++
			}
		}
		res.OK = true
		res.Message = fmt.Sprintf("Connected: the server offers %d tools (%d say they only read). Use Tools, then Discover tools, to keep them.",
			len(listed), readOnly)
	}
	ok(w, res)
}
