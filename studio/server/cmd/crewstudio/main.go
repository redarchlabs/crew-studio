// Command crewstudio serves the Crew Studio API (and optionally the built UI) and runs crews.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	// Schedules use IANA time zones; embed the database so they work on hosts without one
	_ "time/tzdata"

	"crewstudio/internal/api"
	"crewstudio/internal/auth"
	"crewstudio/internal/callbacks"
	"crewstudio/internal/config"
	"crewstudio/internal/connections"
	"crewstudio/internal/db"
	"crewstudio/internal/delivery"
	"crewstudio/internal/help"
	"crewstudio/internal/launch"
	"crewstudio/internal/memory"
	"crewstudio/internal/runner"
	"crewstudio/internal/scheduler"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
	"crewstudio/internal/watch"
)

const shutdownTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "crewstudio:", err)
		os.Exit(1)
	}
}

// defaultStudioDir finds the studio folder (the one holding .env) whether started from studio/ or studio/server/.
func defaultStudioDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	if filepath.Base(wd) == "server" {
		return filepath.Dir(wd)
	}
	return wd
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("STUDIO_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func run() error {
	studioDir := flag.String("studio-dir", defaultStudioDir(), "folder holding studio .env; relative paths resolve against it")
	migrateOnly := flag.Bool("migrate", false, "apply database migrations and exit")
	migration := migrationCommand{}
	flag.BoolVar(&migration.status, "migrate-status", false, "list every migration and whether it is applied, then exit")
	flag.StringVar(&migration.downTo, "migrate-down-to", "",
		"roll back applied migrations newer than this version (such as 6 or 0006_llm_vision; 0 rolls back everything), then exit;\n"+
			"lists what it would undo unless -yes is also given")
	flag.BoolVar(&migration.confirmed, "yes", false, "confirm -migrate-down-to: roll back and delete the data those migrations added")
	rotateKey := flag.Bool(rotateSubcommand, false,
		"re-encrypt every connection secret from "+oldKeyEnvVar+" (default STUDIO_SECRETS_KEY) to "+newKeyEnvVar+" in one transaction, then exit;\n"+
			"also available as: crewstudio "+rotateSubcommand)
	resetAdmin := flag.Bool(resetAdminSubcommand, false,
		"set a new password for an administrator (typed twice, hidden; or one line on stdin), clear any lockout and\n"+
			"sign them out everywhere, then exit; also available as: crewstudio "+resetAdminSubcommand)
	adminEmail := flag.String("email", "", "with -"+resetAdminSubcommand+": the administrator's email (not needed when there is only one)")
	if err := flag.CommandLine.Parse(subcommandArgs(os.Args[1:])); err != nil {
		return err
	}
	if err := migration.validate(); err != nil {
		return err
	}

	log := newLogger()
	absDir, err := filepath.Abs(*studioDir)
	if err != nil {
		return fmt.Errorf("resolve studio dir: %w", err)
	}
	cfg, err := config.Load(absDir, os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	// Status and rollback leave the database as they found it apart from what was asked, so they skip migrating up
	if migration.requested() {
		return migration.run(ctx, pool, os.Stdout)
	}
	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	if len(applied) > 0 {
		log.Info("applied migrations", "versions", applied)
	}
	if *migrateOnly {
		return nil
	}

	st := store.New(pool)
	// Before the startup key check: STUDIO_SECRETS_KEY may already hold the new key, which cannot open the secrets yet
	if *rotateKey {
		return rotateSecretsKey(ctx, st, cfg.SecretsKey, os.Getenv, os.Stdout)
	}
	// Also before the key check: the password does not depend on connection secrets
	if *resetAdmin {
		return resetAdminPassword(ctx, st, *adminEmail, terminalPassword(os.Stdin, os.Stderr), cfg.LocalLogin, os.Stdout)
	}
	conns, err := openConnections(ctx, st, cfg.SecretsKey, log)
	if err != nil {
		return err
	}
	files, err := openStorage(ctx, cfg.Storage, log)
	if err != nil {
		return err
	}
	broker := runner.NewBroker()
	catalog := runner.NewCatalog(cfg.CatalogPath, cfg.ProviderEnvFile, os.Getenv)
	// Fail closed: a tool the catalog does not classify (reads, proposes, changes) must never reach agent chat. Only
	// the classification is checked here; the provider env file is read per request, as before.
	if err := runner.CheckCatalog(cfg.CatalogPath); err != nil {
		return err
	}
	manager := runner.NewManager(st, broker, runner.ProcessConfig{
		Python: cfg.Python, Script: cfg.RunnerScript, Workspace: cfg.Workspace, DataDir: cfg.DataDir,
		ProviderEnvFile: cfg.ProviderEnvFile, MaxDuration: cfg.MaxRunDuration, KeepRunFiles: cfg.KeepRunFiles,
		StateURL: cfg.InternalURL(),
	}, files, log)
	embedders := newEmbedders(cfg)
	memories := newMemoryService(cfg, embedders, st, log)
	manager.OnRunEnd(memories.Forget)
	launcher := launch.New(st, catalog, manager).WithMemory(memories)
	sched := scheduler.New(st, launcher, log)
	trackers := tracker.NewService(st, conns, tracker.Options{JiraSites: cfg.JiraSites, KM2TrackerSites: cfg.KM2TrackerSites, LinearURL: cfg.LinearAPIURL})
	watches := watch.New(st, conns, cfg.Workspace).WithTrackers(trackers)
	sched.SetWatches(watches)
	deliverer := callbacks.New(st, log)
	housekeeping := callbacks.NewHousekeeping(st, files, log)
	setupToken, err := firstRunToken(ctx, st, cfg, log)
	if err != nil {
		return err
	}

	helpIndex, err := newHelpIndex(cfg, embedders, st, log)
	if err != nil {
		return err
	}
	webDir := cfg.WebDir
	if webDir != "" {
		if _, err := os.Stat(filepath.Join(webDir, "index.html")); err != nil {
			log.Info("built UI not found; serving the API only (use the Vite dev server for the UI)", "web_dir", webDir)
			webDir = ""
		}
	}
	apiServer := api.NewServer(api.Deps{
		Store: st, Runs: manager, Broker: broker, Tools: catalog, Launcher: launcher, Scheduler: sched,
		Files: files, SSO: newSSO(cfg), Help: helpIndex, Log: log,
		Connections: conns, StateEvents: manager, Watches: watches, Trackers: trackers,
		Delivery: delivery.New(conns, delivery.Options{Origins: cfg.DeliveryOrigins, SlackAPIURL: cfg.SlackAPIURL}),
		Memory:   memories,
	}, api.Options{
		AllowedOrigins: cfg.AllowedOrigins, AllowedHosts: cfg.AllowedHosts, WebDir: webDir,
		PublicURL: cfg.PublicURL, LocalLogin: cfg.LocalLogin, SetupToken: setupToken,
	})
	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: apiServer.Handler(),
		// Requests inherit the signal context, so open run streams end when the service stops
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: run streams stay open for the whole run. Other routes have a handler timeout.
	}

	managerDone := make(chan struct{})
	go func() {
		manager.Run(ctx)
		close(managerDone)
	}()
	var workers sync.WaitGroup
	for _, run := range []func(context.Context){sched.Run, deliverer.Run, housekeeping.Run, helpIndex.Run, memories.Run} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			run(ctx)
		}()
	}
	defer workers.Wait()

	serveErr := make(chan error, 1)
	go func() {
		log.Info("crew studio listening", "addr", "http://"+cfg.Addr, "public_url", cfg.PublicURL, "ui", webDir != "",
			"storage", describeStorage(files))
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		stop()
		<-managerDone
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Warn("http shutdown", "err", err)
		}
		<-managerDone
	}
	return nil
}

// openConnections checks STUDIO_SECRETS_KEY against the connections already saved. Without a key (and with no
// connections yet) connections are simply off.
func openConnections(ctx context.Context, st *store.Store, key string, log *slog.Logger) (*connections.Service, error) {
	box, err := connections.CheckKey(ctx, st, key)
	if err != nil {
		return nil, err
	}
	if box == nil {
		log.Info("connections are off: set STUDIO_SECRETS_KEY (openssl rand -base64 32) to add them")
	}
	return connections.New(st, box), nil
}

func openStorage(ctx context.Context, c config.StorageConfig, log *slog.Logger) (storage.Store, error) {
	var files storage.Store
	switch c.Driver {
	case config.StorageNone:
		log.Warn("file storage is off (STUDIO_STORAGE=none): attachments and run files are not kept")
		return nil, nil
	case config.StorageGCS:
		g, err := storage.NewGCS(ctx, storage.GCSConfig{Bucket: c.GCSBucket, CredentialsFile: c.GCSCredentials, Endpoint: c.GCSEndpoint})
		if err != nil {
			return nil, err
		}
		files = g
	default:
		a, err := storage.NewAzure(c.AzureConnection, c.AzureContainer)
		if err != nil {
			return nil, err
		}
		files = a
	}
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := files.Ready(readyCtx); err != nil {
		return nil, fmt.Errorf("file storage (%s) is not reachable: %w", files.Describe(), err)
	}
	return files, nil
}

func describeStorage(files storage.Store) string {
	if files == nil {
		return "off"
	}
	return files.Describe()
}

// embedder is what help search and crew memory both need from an embedding provider.
type embedder interface {
	help.Embedder
	memory.Embedder
}

// embedders builds each embedding provider once, so help search and memory share one when they use the same.
type embedders struct {
	key func(name string) string
	by  map[string]embedder
}

func newEmbedders(cfg config.Config) *embedders {
	key := func(name string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		// Re-read each time so a key added to the provider .env works without a restart
		values, err := config.ReadDotEnv(cfg.ProviderEnvFile)
		if err != nil {
			return ""
		}
		return values[name]
	}
	return &embedders{key: key, by: map[string]embedder{}}
}

// get returns the provider's embedder ("openai" or "gemini"), or nil for anything else (keyword, off).
func (e *embedders) get(provider string) embedder {
	if got, ok := e.by[provider]; ok {
		return got
	}
	var out embedder
	switch provider {
	case config.HelpSearchGemini:
		out = help.NewGeminiEmbedder(e.key)
	case config.HelpSearchOpenAI:
		out = help.NewOpenAIEmbedder(e.key)
	default:
		return nil
	}
	e.by[provider] = out
	return out
}

// newHelpIndex loads the compiled-in help articles. Semantic search embeds them in the background
// (helpIndex.Run) and searches by keyword until that is done, or when the provider's key is not set.
func newHelpIndex(cfg config.Config, e *embedders, st *store.Store, log *slog.Logger) (*help.Index, error) {
	lib, err := help.LoadEmbedded()
	if err != nil {
		return nil, err
	}
	if emb := e.get(cfg.HelpSearch); emb != nil {
		return help.NewIndex(lib, emb, st, log), nil
	}
	return help.NewIndex(lib, nil, nil, log), nil
}

// newMemoryService embeds crew memories with STUDIO_MEMORY_EMBEDDINGS's provider (by default help search's). Without
// one, or without its key, memory is unavailable and the Memory tab says why.
func newMemoryService(cfg config.Config, e *embedders, st *store.Store, log *slog.Logger) *memory.Service {
	opts := memory.Options{
		Off: "Memory is off on this server (STUDIO_MEMORY_EMBEDDINGS=off). An administrator can set it to openai or gemini.",
	}
	var emb memory.Embedder
	switch cfg.MemoryEmbeddings {
	case config.HelpSearchOpenAI:
		emb = e.get(cfg.MemoryEmbeddings)
		opts.Unconfigured = "Memory needs OPENAI_API_KEY in the provider .env file (STUDIO_MEMORY_EMBEDDINGS=openai)."
	case config.HelpSearchGemini:
		emb = e.get(cfg.MemoryEmbeddings)
		opts.Unconfigured = "Memory needs GEMINI_API_KEY in the provider .env file (STUDIO_MEMORY_EMBEDDINGS=gemini)."
	}
	svc := memory.NewService(emb, opts, st, log)
	if ok, reason := svc.Available(); !ok {
		log.Info("crew memory is unavailable", "reason", reason)
	}
	return svc
}

func newSSO(cfg config.Config) *auth.SSO {
	var providers []auth.ProviderConfig
	if c := cfg.Microsoft; c != nil {
		providers = append(providers, auth.MicrosoftProvider(c.Issuer, c.ClientID, c.ClientSecret))
	}
	if c := cfg.Clerk; c != nil {
		providers = append(providers, auth.ClerkProvider(c.Issuer, c.ClientID, c.ClientSecret))
	}
	return auth.NewSSO(cfg.PublicURL, providers...)
}

// firstRunToken returns a one-time setup token while nobody has an account, and prints the setup link.
func firstRunToken(ctx context.Context, st *store.Store, cfg config.Config, log *slog.Logger) (string, error) {
	n, err := st.HumanUserCount(ctx)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", nil
	}
	token := auth.RandomToken(24)
	log.Warn("no accounts yet: create the first administrator", "setup_url", cfg.PublicURL+"/setup?token="+token)
	fmt.Fprintf(os.Stderr, "\n  Crew Studio has no accounts yet. Create the first administrator at:\n\n    %s/setup?token=%s\n\n", cfg.PublicURL, token)
	return token, nil
}
