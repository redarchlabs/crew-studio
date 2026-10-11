package runner

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
)

const (
	dispatchInterval  = 5 * time.Second
	cancelWaitTimeout = 15 * time.Second
	// Long enough for stopped runs to upload their transcripts and files
	shutdownWaitTimeout = 2 * time.Minute
	dbWriteTimeout      = 10 * time.Second
)

// ProcessConfig says how to launch the Python runner.
type ProcessConfig struct {
	Python    string
	Script    string
	Workspace string
	// DataDir holds each run's transcript and console log until they are uploaded; agents cannot reach it
	DataDir         string
	ProviderEnvFile string
	MaxDuration     time.Duration
	// KeepRunFiles leaves run folders on disk after upload (for debugging the service itself)
	KeepRunFiles bool
	// StateURL is the service's own base address (such as http://127.0.0.1:8088) for the run-state API that
	// state tools call with the run's token; empty turns the run-state API off for runs
	StateURL string
}

type activeRun struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cancelled atomic.Bool // stopped by a person
	shutdown  atomic.Bool // stopped because the service is exiting
	done      chan struct{}
	// rec is set once the run is executing, so the run-state API can add events to its stream
	rec atomic.Pointer[recorder]
}

// Manager starts queued runs up to the concurrency limit in Settings and supervises them.
type Manager struct {
	store  *store.Store
	broker *Broker
	proc   ProcessConfig
	files  storage.Store
	log    *slog.Logger

	// runEnded (OnRunEnd) is called with each run's id when it finishes
	runEnded func(runID string)

	mu     sync.Mutex
	active map[string]*activeRun
	wake   chan struct{}
	wg     sync.WaitGroup
	base   context.Context
}

// NewManager builds the run manager. files may be nil (no attachments or artifacts are stored).
func NewManager(st *store.Store, broker *Broker, proc ProcessConfig, files storage.Store, log *slog.Logger) *Manager {
	return &Manager{
		store:  st,
		broker: broker,
		proc:   proc,
		files:  files,
		log:    log,
		active: map[string]*activeRun{},
		wake:   make(chan struct{}, 1),
		base:   context.Background(),
	}
}

// OnRunEnd sets fn to be called with a run's id when it finishes (succeeded, failed or stopped), so services that keep
// per-run state can drop it (memory's per-run vector cache). Call it before Run.
func (m *Manager) OnRunEnd(fn func(runID string)) { m.runEnded = fn }

// Notify asks the dispatcher to look for queued runs now.
func (m *Manager) Notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Run recovers from a previous crash, then dispatches queued runs until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.base = ctx
	m.mu.Unlock()
	m.failOrphans(ctx)

	ticker := time.NewTicker(dispatchInterval)
	defer ticker.Stop()
	for {
		m.dispatch(ctx)
		select {
		case <-ctx.Done():
			m.shutdown()
			return
		case <-m.wake:
		case <-ticker.C:
		}
	}
}

func (m *Manager) failOrphans(ctx context.Context) {
	ids, err := m.store.FailOrphanedRuns(ctx)
	if err != nil {
		m.log.Error("could not fail orphaned runs", "err", err)
		return
	}
	for _, id := range ids {
		m.log.Warn("run was in progress when the service stopped; marked failed", "run_id", id)
		m.removeRunFolders(m.pathsFor(id))
		m.expireRunToken(id, 0)
		seq, err := m.store.MaxEventSeq(ctx, id)
		if err != nil {
			continue
		}
		event := model.RunEvent{Seq: seq + 1, Type: "run_status", CreatedAt: time.Now(),
			Payload: map[string]any{"status": model.RunFailed, "error": "The Crew Studio service restarted while this run was in progress."}}
		if err := m.store.AppendEvent(ctx, id, event); err != nil {
			m.log.Error("could not record orphaned run status", "run_id", id, "err", err)
		}
	}
}

func (m *Manager) dispatch(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	settings, err := m.store.GetSettings(ctx)
	if err != nil {
		m.log.Error("dispatch: read settings", "err", err)
		return
	}
	m.mu.Lock()
	free := settings.MaxConcurrentRuns - len(m.active)
	m.mu.Unlock()
	if free <= 0 {
		return
	}
	ids, err := m.store.QueuedRunIDs(ctx, free)
	if err != nil {
		m.log.Error("dispatch: list queued runs", "err", err)
		return
	}
	for _, id := range ids {
		m.launch(ctx, id)
	}
}

func (m *Manager) launch(ctx context.Context, id string) {
	runCtx, cancel := context.WithCancel(ctx)
	entry := &activeRun{ctx: runCtx, cancel: cancel, done: make(chan struct{})}

	// Register before marking the run started, so a concurrent Cancel always finds it
	m.mu.Lock()
	if _, exists := m.active[id]; exists {
		m.mu.Unlock()
		cancel()
		return
	}
	m.active[id] = entry
	m.mu.Unlock()

	started, err := m.store.StartRun(ctx, id)
	if err != nil || !started {
		if err != nil {
			m.log.Error("start run", "run_id", id, "err", err)
		}
		m.forget(id, entry)
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.forget(id, entry)
		m.execute(id, entry)
	}()
}

func (m *Manager) forget(id string, entry *activeRun) {
	entry.cancel()
	m.mu.Lock()
	if m.active[id] == entry {
		delete(m.active, id)
	}
	m.mu.Unlock()
	select {
	case <-entry.done:
	default:
		close(entry.done)
	}
	m.Notify()
}

// Cancel stops a queued or running run and returns it once it has stopped (or the wait times out).
func (m *Manager) Cancel(ctx context.Context, id string) (model.Run, error) {
	m.mu.Lock()
	entry := m.active[id]
	m.mu.Unlock()

	if entry != nil {
		entry.cancelled.Store(true)
		entry.cancel()
		select {
		case <-entry.done:
		case <-time.After(cancelWaitTimeout):
		case <-ctx.Done():
		}
		return m.store.GetRun(ctx, id)
	}

	msg := "Cancelled before it started."
	// Only succeeds while the run is still queued, so it cannot clobber a run the dispatcher just started
	finished, cancelled, err := m.store.CancelQueuedRun(ctx, id, msg)
	if err != nil {
		return model.Run{}, err
	}
	if !cancelled {
		m.mu.Lock()
		started := m.active[id]
		m.mu.Unlock()
		if started != nil {
			// It was launched between the two checks: stop it the normal way
			return m.Cancel(ctx, id)
		}
		return m.store.GetRun(ctx, id)
	}
	rec := newRecorder(m, id, 0)
	if seq, err := m.store.MaxEventSeq(ctx, id); err == nil {
		rec.seq = seq
	}
	rec.record(model.RunEvent{Type: "run_status", CreatedAt: time.Now(),
		Payload: map[string]any{"status": model.RunCancelled, "error": msg}}, nil)
	m.broker.CloseRun(id)
	return finished, nil
}

// shuttingDown reports whether the service is stopping.
func (m *Manager) shuttingDown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.base.Err() != nil
}

// ActiveCount reports how many runs are executing now.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

func (m *Manager) shutdown() {
	m.mu.Lock()
	for _, entry := range m.active {
		entry.shutdown.Store(true)
		entry.cancel()
	}
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownWaitTimeout):
		m.log.Warn("some runs did not stop before shutdown timeout")
	}
}

// recorder assigns sequence numbers, stores events, and publishes them live.
type recorder struct {
	// mu orders events from the run and from the run-state API
	mu    sync.Mutex
	m     *Manager
	runID string
	seq   int
	// files, when set, are uploaded by finish before the final status
	files *runPaths
	// tasks is how many tasks the run has, so a finished task's position can be checked
	tasks int
}

func newRecorder(m *Manager, runID string, seq int) *recorder {
	return &recorder{m: m, runID: runID, seq: seq}
}

func (r *recorder) record(event model.RunEvent, call *model.LLMCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	event.Seq = r.seq
	// Use a fresh context: events must be saved even while the run is being cancelled
	ctx, cancel := context.WithTimeout(context.Background(), dbWriteTimeout)
	defer cancel()
	if err := r.m.store.AppendEvent(ctx, r.runID, event); err != nil {
		r.m.log.Error("save run event", "run_id", r.runID, "type", event.Type, "err", err)
	}
	if call != nil {
		call.Seq = r.seq
		if err := r.m.store.AppendLLMCall(ctx, r.runID, *call); err != nil {
			r.m.log.Error("save llm call", "run_id", r.runID, "err", err)
		}
	}
	r.m.broker.Publish(r.runID, event)
}
