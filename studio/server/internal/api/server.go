package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"

	"crewstudio/internal/auth"
	"crewstudio/internal/chat"
	"crewstudio/internal/connections"
	"crewstudio/internal/delivery"
	"crewstudio/internal/duereport"
	"crewstudio/internal/help"
	"crewstudio/internal/km2"
	"crewstudio/internal/launch"
	"crewstudio/internal/mcpclient"
	"crewstudio/internal/memory"
	"crewstudio/internal/model"
	"crewstudio/internal/reports"
	"crewstudio/internal/runner"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
	"crewstudio/internal/watch"
)

const (
	requestTimeout = 30 * time.Second
	// Uploads and downloads stream large files, so they get longer
	fileRequestTimeout = 10 * time.Minute
	// Per principal (or IP when signed out): generous for a person clicking around, low enough to stop a runaway loop
	requestsPerSecond = 50
	requestBurst      = 200
	// Everyone behind one address (an office, a proxy) shares this, so it is higher
	ipRequestsPerSecond = 200
	ipRequestBurst      = 800
	// Starting runs costs money, so it gets its own tighter limit per principal
	runStartsPerMinute = 20
	runStartBurst      = 5
	loginsPerMinute    = 10
	loginBurst         = 5
	hooksPerMinute     = 60
	hookBurst          = 10
	// Each open run page holds one stream; this bounds what a misbehaving page can hold open
	maxOpenStreams = 32
	// The first-run setup link stops working after this; restart the service for a new one
	setupTokenTTL = 24 * time.Hour
	// One person or key cannot take every stream slot
	maxStreamsPerPrincipal = 8
	// A leaked or misbehaving webhook caller cannot queue unlimited paid work
	maxActivePerTrigger = 5
)

// RunControl is what the API needs from the run manager.
type RunControl interface {
	Notify()
	Cancel(ctx context.Context, id string) (model.Run, error)
}

// ToolCatalog lists the tools agents can use.
type ToolCatalog interface {
	Tools() ([]model.Tool, error)
	ByKey() (map[string]model.Tool, error)
}

// Scheduler is what the API needs from the scheduler.
type Scheduler interface {
	Notify()
	Launch(ctx context.Context, p *auth.Principal, sc model.Schedule, now time.Time) (model.WorkOrder, error)
}

// SSO runs sign-ins with external providers.
type SSO interface {
	Providers() []auth.ProviderInfo
	Has(key string) bool
	Begin(ctx context.Context, key string) (auth.LoginStart, error)
	Complete(ctx context.Context, key, code, codeVerifier, nonce string) (auth.Identity, error)
}

type Server struct {
	store     *store.Store
	runs      RunControl
	broker    *runner.Broker
	tools     ToolCatalog
	launcher  *launch.Launcher
	scheduler Scheduler
	files     storage.Store
	sso       SSO
	help      *help.Index
	log       *slog.Logger
	opts      Options

	// conns seals connection secrets; stateEvents writes run-state calls into run journals
	conns       *connections.Service
	stateEvents StateRecorder
	// watches checks watch definitions for previews
	watches *watch.Service
	// chat starts chat turns and applies the proposals people confirm
	chat *chat.Service
	// trackers reaches Jira and Linear for tracker tools, tracking lists and people maps
	trackers *tracker.Service
	// km2 reaches KM2 for the knowledge tools and connection tests
	km2 *km2.Service
	// mcp reaches outside MCP servers for MCP connections (not Studio's own MCP server)
	mcp *mcpclient.Client
	// sender posts to Slack, Teams and email (connection tests); reports delivers finished reports
	sender  *delivery.Sender
	reports *reports.Service
	// memory embeds crew memories and memory queries; it may be unavailable (no embedder)
	memory *memory.Service
	// answers wakes runs waiting on a question when a person answers it; questions wakes people's question streams
	answers            *answerSignals
	questions          *questionHub
	questionStreams    *counter
	questionStreamsAll *counter

	requests  *keyedLimiter
	byIP      *keyedLimiter
	runStarts *keyedLimiter
	logins    *keyedLimiter
	hooks     *keyedLimiter
	// helpSearches bounds embedding API calls made on someone's behalf
	helpSearches *keyedLimiter
	// runStateCalls bounds run-state API calls per run
	runStateCalls *keyedLimiter
	// keyRejections audits a refused API key once per prefix and address per window, so a client stuck on a
	// revoked key cannot flood the audit log
	keyRejections *keyedLimiter
	streams       chan struct{}
	streamsPer    *counter

	setupMu      sync.Mutex
	setupToken   string
	setupExpires time.Time
}

// Deps are the collaborators the API uses.
type Deps struct {
	Store     *store.Store
	Runs      RunControl
	Broker    *runner.Broker
	Tools     ToolCatalog
	Launcher  *launch.Launcher
	Scheduler Scheduler
	Files     storage.Store
	SSO       SSO
	// Help searches the in-app help; nil means keyword search over the compiled-in articles
	Help *help.Index
	Log  *slog.Logger

	// Connections seals connection secrets; nil means none can be saved (no STUDIO_SECRETS_KEY)
	Connections *connections.Service
	// StateEvents records run-state calls in run journals; nil appends to the run's events directly
	StateEvents StateRecorder
	// Watches runs watch previews; nil uses one without a workspace folder (workspace files cannot be read)
	Watches *watch.Service
	// Chat runs crew chat; nil makes one from the store, launcher and run manager
	Chat *chat.Service
	// Trackers reaches Jira and Linear; nil builds one with the default options
	Trackers *tracker.Service
	// KM2 reaches KM2; nil builds one with the default options
	KM2 *km2.Service
	// Delivery posts to Slack, Teams and email; nil builds one with the default options
	Delivery *delivery.Sender
	// MCP reaches outside MCP servers for MCP connections; nil builds one with the default options
	MCP *mcpclient.Client
	// Memory embeds crew memories; nil means memory is unavailable (the Memory tab says why)
	Memory *memory.Service
}

type Options struct {
	AllowedOrigins []string
	// AllowedHosts are extra Host header names to accept besides loopback (only for remote setups)
	AllowedHosts []string
	WebDir       string
	// PublicURL is the address people use (SSO redirects and secure cookies depend on it)
	PublicURL string
	// LocalLogin enables password sign-in for administrators
	LocalLogin bool
	// SetupToken, when set, allows creating the first administrator once
	SetupToken string
}

func NewServer(d Deps, opts Options) *Server {
	if d.Help == nil {
		d.Help = defaultHelp()
	}
	if d.Connections == nil {
		d.Connections = connections.New(d.Store, nil)
	}
	if d.Trackers == nil {
		d.Trackers = tracker.NewService(d.Store, d.Connections, tracker.Options{})
	}
	if d.Watches == nil {
		d.Watches = watch.New(d.Store, d.Connections, "").WithTrackers(d.Trackers)
	}
	if d.Chat == nil {
		d.Chat = chat.New(d.Store, d.Launcher, d.Runs)
	}
	d.Chat.UseTrackers(d.Trackers)
	if d.StateEvents == nil {
		d.StateEvents = storeStateRecorder{store: d.Store, broker: d.Broker}
	}
	if d.KM2 == nil {
		d.KM2 = km2.NewService(d.Store, d.Connections, km2.Options{})
	}
	// Agent chat's KM2 save cards (slice 3d) save through the same service
	d.Chat.UseKM2(d.KM2)
	if d.Delivery == nil {
		d.Delivery = delivery.New(d.Connections, delivery.Options{})
	}
	if d.Memory == nil {
		d.Memory = memory.NewService(nil, memory.Options{}, nil, d.Log)
	}
	if d.MCP == nil {
		d.MCP = mcpclient.New()
	}
	srv := &Server{
		store: d.Store, runs: d.Runs, broker: d.Broker, tools: d.Tools, launcher: d.Launcher, scheduler: d.Scheduler,
		files: d.Files, sso: d.SSO, help: d.Help, log: d.Log, opts: opts,
		conns: d.Connections, stateEvents: d.StateEvents, watches: d.Watches, chat: d.Chat, trackers: d.Trackers, km2: d.KM2,
		sender: d.Delivery, memory: d.Memory, mcp: d.MCP, answers: newAnswerSignals(), questions: newQuestionHub(),
		questionStreams:    &counter{counts: map[string]int{}, max: maxQuestionStreamsPerPerson},
		questionStreamsAll: &counter{counts: map[string]int{}, max: maxQuestionStreams},
		runStateCalls:      newKeyedLimiter(rate.Limit(runStatePerSecond), runStateBurst),
		requests:           newKeyedLimiter(rate.Limit(requestsPerSecond), requestBurst),
		byIP:               newKeyedLimiter(rate.Limit(ipRequestsPerSecond), ipRequestBurst),
		runStarts:          newKeyedLimiter(rate.Every(time.Minute/runStartsPerMinute), runStartBurst),
		logins:             newKeyedLimiter(rate.Every(time.Minute/loginsPerMinute), loginBurst),
		hooks:              newKeyedLimiter(rate.Every(time.Minute/hooksPerMinute), hookBurst),
		helpSearches:       newKeyedLimiter(rate.Every(time.Minute/helpSearchesPerMinute), helpSearchBurst),
		keyRejections:      newKeyedLimiter(rate.Every(rejectedKeyAuditEvery), 1),
		streams:            make(chan struct{}, maxOpenStreams),
		streamsPer:         &counter{counts: map[string]int{}, max: maxStreamsPerPrincipal},
		setupToken:         opts.SetupToken,
		setupExpires:       time.Now().Add(setupTokenTTL),
	}
	// Reports save to KM2 through the server's own KM2 save path
	srv.reports = reports.New(d.Store, d.Delivery, srv, d.Log)
	if d.Trackers != nil {
		srv.reports = srv.reports.WithSnapshots(reportSnapshots{builder: duereport.NewBuilder(d.Store, d.Trackers), log: srv.log})
	}
	return srv
}

// secureCookies is on when people reach the service over https.
func (s *Server) secureCookies() bool {
	u, err := url.Parse(s.opts.PublicURL)
	return err == nil && u.Scheme == "https"
}

// Handler builds the router: /api for JSON, /healthz, and optionally the built UI at /.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(checkHost(append(HostsFromOrigins(append(s.opts.AllowedOrigins, s.opts.PublicURL)), s.opts.AllowedHosts...)))
	r.Use(securityHeaders)
	r.Use(requestLogger(s.log))

	r.Get("/healthz", s.health)
	s.mountMCP(r)

	r.Route("/api", func(r chi.Router) {
		r.Use(checkOrigin(s.opts.AllowedOrigins))
		r.Use(requireJSON)
		// Per address first (so guessing keys or tokens is throttled), then per principal
		r.Use(s.limitByIP)
		r.Use(s.authenticate)
		r.Use(s.keyMayUseREST)
		r.Use(s.rateLimit)

		r.Get("/openapi.yaml", serveOpenAPI)
		s.authRoutes(r)
		r.With(middleware.Timeout(requestTimeout)).Post("/hooks/{id}", s.fireTrigger)
		// Running crews' state tools: run token only, never a session or API key
		s.runStateRoutes(r)

		r.Group(func(r chi.Router) {
			r.Use(s.requireSignedIn)
			// Live streams and file transfers must not have the short request timeout
			r.Get("/runs/{id}/stream", s.streamRun)
			r.Get("/questions/stream", s.streamQuestions)
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(fileRequestTimeout))
				r.Post("/work-orders/{id}/attachments", s.uploadAttachments)
				r.Get("/attachments/{id}/content", s.attachmentContent)
			})
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(requestTimeout))
				s.routes(r)
			})
		})
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			fail(w, http.StatusNotFound, "not_found", "No such API endpoint.")
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "That method is not allowed here.")
		})
	})

	if s.opts.WebDir != "" {
		r.Handle("/*", spaHandler(s.opts.WebDir))
	}
	return r
}

func (s *Server) routes(r chi.Router) {
	r.With(s.need(auth.OverviewView)).Get("/overview", s.overview)
	r.With(s.need(auth.AgentsView)).Get("/tools", s.listTools)
	r.With(s.needAny(auth.HistoryView, auth.AdminAudit)).Get("/history", s.listHistory)
	r.With(s.need(auth.SettingsView)).Get("/settings", s.getSettings)
	r.With(s.need(auth.SettingsManage)).Put("/settings", s.putSettings)

	s.mountLibrary(r)
	s.runRoutes(r)
	s.workOrderRoutes(r)
	s.scheduleRoutes(r)
	s.triggerRoutes(r)
	s.areaRoutes(r)
	s.helpRoutes(r)
	s.connectionRoutes(r)
	s.pmoRoutes(r)
	s.chatRoutes(r)
	s.questionRoutes(r)
	s.accountRoutes(r)
	r.Route("/admin", s.adminRoutes)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Pool().Ping(ctx); err != nil {
		fail(w, http.StatusServiceUnavailable, "db_unavailable", "The database is not reachable.")
		return
	}
	ok(w, map[string]string{"status": "ok"})
}

// pathID reads the {id} URL parameter and answers 404 itself when it is not a UUID.
func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	return pathUUID(w, r, "id")
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := chi.URLParam(r, name)
	if !uuidPattern.MatchString(id) {
		fail(w, http.StatusNotFound, "not_found", "Not found.")
		return "", false
	}
	return id, true
}
