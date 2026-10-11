package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"crewstudio/internal/auth"
	"crewstudio/internal/mcpserver"
	"crewstudio/internal/store"
)

// mcpVersion is reported to MCP clients.
const mcpVersion = "1"

// auditTimeout bounds writing one audit record after a tool call.
const auditTimeout = 5 * time.Second

// mcpCallerKey holds the caller in the MCP token info, which is how a tool call learns who made the request.
const mcpCallerKey = "crewstudio_caller"

type mcpCaller struct {
	principal  *auth.Principal
	remoteAddr string
}

// mountMCP serves the MCP endpoint (Streamable HTTP, stateless) at /mcp. It takes Crew Studio API keys only:
// a browser session cookie would let any page the person visits drive it. Each request is authenticated,
// rate limited and checked like an /api request, and each tool call runs through the /api handlers as the key's
// owner, so permissions, functional areas, validation and history all apply.
func (s *Server) mountMCP(r chi.Router) {
	server := mcpserver.New(mcpDispatcher{s: s, api: s.internalAPI()}, mcpVersion)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, Logger: s.log,
		// checkHost already allows only this machine and the configured host names, which also covers a
		// reverse proxy on the same machine that the SDK's own loopback check would refuse
		DisableLocalhostProtection: true,
	})
	r.With(
		checkOrigin(s.opts.AllowedOrigins),
		limitBody,
		refuseBatches,
		s.limitByIP,
		s.authenticate,
		keyMayUseMCP,
		s.rateLimit,
		mcpauth.RequireBearerToken(s.mcpVerify, nil),
		middleware.Timeout(requestTimeout),
	).Handle("/mcp", handler)
}

// keyMayUseMCP refuses API keys that are limited to the REST API.
func keyMayUseMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := principal(r); p != nil && p.Via == auth.ViaAPIKey && p.NoMCP {
			fail(w, http.StatusForbidden, "forbidden", "This API key works only with the REST API (/api), not MCP.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// refuseBatches turns away JSON-RPC batches. Older protocol versions allow them, and one request could then carry
// thousands of tool calls past the per-request rate limit.
func refuseBatches(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fail(w, http.StatusRequestEntityTooLarge, "too_large", "The request body is too large.")
			return
		}
		if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
			fail(w, http.StatusBadRequest, "batch_not_supported", "Send one JSON-RPC message per request.")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

// mcpVerify accepts the request when authenticate found a valid API key. The key was already checked (expiry,
// revocation, the owner's permission to hold keys), so this only hands the caller on to the tools.
func (s *Server) mcpVerify(_ context.Context, _ string, r *http.Request) (*mcpauth.TokenInfo, error) {
	p := principal(r)
	if p == nil || p.Via != auth.ViaAPIKey {
		return nil, fmt.Errorf("%w: send a Crew Studio API key (csk_…) as a Bearer token", mcpauth.ErrInvalidToken)
	}
	return &mcpauth.TokenInfo{
		UserID: p.UserID,
		// Stateless: the key is checked again on every request, so this only has to outlive one
		Expiration: time.Now().Add(requestTimeout),
		Extra:      map[string]any{mcpCallerKey: mcpCaller{principal: p, remoteAddr: r.RemoteAddr}},
	}, nil
}

// internalAPI routes tool calls to the same handlers /api uses. Authentication and rate limits ran on the
// /mcp request, and the caller is set on each request's context.
func (s *Server) internalAPI() http.Handler {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.Recoverer)
		r.Use(requireJSON)
		r.Use(s.requireSignedIn)
		r.Use(middleware.Timeout(requestTimeout))
		r.Get("/auth/me", s.me)
		s.routes(r)
	})
	return r
}

type mcpDispatcher struct {
	s   *Server
	api http.Handler
}

// Do runs one API request as the MCP caller. Each call spends a request from the caller's rate limit, as it would
// over /api, and changes are audited so admins can tell MCP edits apart.
func (d mcpDispatcher) Do(ctx context.Context, ti *mcpauth.TokenInfo, in mcpserver.Request) (status int, body []byte, err error) {
	var c mcpCaller
	if ti != nil {
		c, _ = ti.Extra[mcpCallerKey].(mcpCaller)
	}
	if c.principal == nil {
		return 0, nil, errors.New("no caller on this request")
	}
	if !d.s.requests.allow("user:" + c.principal.UserID) {
		return 0, nil, errors.New("too many requests; slow down and try again")
	}
	// Tool calls run on the SDK's own goroutines, outside net/http's recovery: a panic here would end the process
	defer func() {
		if p := recover(); p != nil {
			d.s.log.Error("mcp tool call panicked", "method", in.Method, "path", in.Path, "panic", p, "stack", string(debug.Stack()))
			status, body, err = 0, nil, errors.New("an internal error happened in Crew Studio; the change may not have been saved")
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	// The tool call's context comes from the /mcp request, and chi would reuse that request's routing state
	// (treating this as a sub-route of /mcp) instead of routing the new path
	ctx = context.WithValue(ctx, chi.RouteCtxKey, (*chi.Context)(nil))
	ctx = auth.WithPrincipal(ctx, c.principal)
	ctx = store.WithActor(ctx, store.Actor{ID: c.principal.UserID, Name: c.principal.Name, APIKeyID: c.principal.APIKeyID})
	req, err := http.NewRequestWithContext(ctx, in.Method, in.Path, bytes.NewReader(in.Body))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.RemoteAddr = c.remoteAddr
	if in.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if in.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", in.IdempotencyKey)
	}
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	d.api.ServeHTTP(rec, req)
	if in.Method != http.MethodGet {
		d.auditChange(req, c, rec.status)
	}
	return rec.status, rec.body.Bytes(), nil
}

// auditChange records an MCP change. It gets its own deadline: a call that used up the request's time is exactly
// the one whose record matters.
func (d mcpDispatcher) auditChange(req *http.Request, c mcpCaller, status int) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), auditTimeout)
	defer cancel()
	d.s.audit(req.WithContext(ctx), "mcp_change", c.principal.UserID, c.principal.Name, map[string]any{
		"method": req.Method, "path": req.URL.Path, "status": status, "key_id": c.principal.APIKeyID,
	})
}

// recorder captures a handler's response in memory.
type recorder struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = status, true
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.body.Write(b)
}
