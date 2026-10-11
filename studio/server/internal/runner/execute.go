package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

const (
	maxRunnerLineBytes = 16 << 20 // final outputs can be large
	killGracePeriod    = 5 * time.Second
	tailLogLines       = 15
	// A looping or prompt-injected agent must not be able to fill the database
	maxEventsPerRun = 100_000
	maxBytesPerRun  = 256 << 20
)

type outputLine struct {
	stream string
	text   string
}

// execute runs one crew to completion. The run is already marked running.
func (m *Manager) execute(runID string, entry *activeRun) {
	ctx := context.Background()
	seq, err := m.store.MaxEventSeq(ctx, runID)
	if err != nil {
		m.finishWithError(runID, newRecorder(m, runID, 0), fmt.Sprintf("Could not read run events: %v", err), nil)
		return
	}
	rec := newRecorder(m, runID, seq)
	entry.rec.Store(rec)
	rec.record(model.RunEvent{Type: "run_status", CreatedAt: time.Now(), Payload: map[string]any{"status": model.RunRunning}}, nil)

	if entry.cancelled.Load() {
		m.finish(runID, rec, store.RunResult{Status: model.RunCancelled, Error: strPtr("Cancelled before it started.")})
		return
	}
	request, paths, spec, err := m.buildRequest(entry.ctx, runID)
	// Whatever happens next, finish() keeps the files the run produced before reporting the final status
	rec.files = &paths
	if err != nil {
		switch {
		case entry.shutdown.Load() || m.shuttingDown():
			m.finishWithError(runID, rec, "The Crew Studio service stopped while this run was starting.", nil)
		case entry.cancelled.Load():
			m.finish(runID, rec, store.RunResult{Status: model.RunCancelled, Error: strPtr("Cancelled before it started.")})
		default:
			m.finishWithError(runID, rec, err.Error(), nil)
		}
		return
	}

	rec.tasks = len(spec.Tasks)
	if spec.Resume != nil {
		m.resumeWorkspace(entry.ctx, spec, paths.workspace, rec)
	}

	limit, overrun := runLimit(spec, m.proc.MaxDuration, time.Now())
	runCtx, cancelTimeout := context.WithTimeout(entry.ctx, limit)
	defer cancelTimeout()
	console := openConsole(paths.console)
	result, tail, procErr := m.runProcess(runCtx, request, paths.workspace, console, rec)
	console.close()

	switch {
	case entry.shutdown.Load() || m.shuttingDown():
		m.finishWithError(runID, rec, "The Crew Studio service stopped while this run was in progress.", &result)
	case entry.cancelled.Load():
		m.finish(runID, rec, withOutcome(store.RunResult{Status: model.RunCancelled, Error: strPtr("Cancelled.")}, result))
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		m.finishWithError(runID, rec, overrun, &result)
	case result.completed && procErr == nil:
		m.finish(runID, rec, withOutcome(store.RunResult{Status: model.RunSucceeded, Output: strPtr(result.output)}, result))
	default:
		msg := result.errMsg
		if msg == "" {
			msg = fmt.Sprintf("The runner exited unexpectedly (%v).", procErr)
			if len(tail) > 0 {
				msg += "\nLast output:\n" + strings.Join(tail, "\n")
			}
		}
		m.finishWithError(runID, rec, msg, &result)
	}
}

func strPtr(s string) *string { return &s }

func withOutcome(res store.RunResult, o outcome) store.RunResult {
	res.CostUSD = o.costUSD
	res.Usage = o.usage
	res.TaskOutputs = o.taskOutputs
	return res
}

func (m *Manager) finishWithError(runID string, rec *recorder, msg string, o *outcome) {
	res := store.RunResult{Status: model.RunFailed, Error: &msg}
	if o != nil {
		res = withOutcome(res, *o)
	}
	m.finish(runID, rec, res)
}

func (m *Manager) finish(runID string, rec *recorder, res store.RunResult) {
	if rec.files != nil {
		m.saveFiles(runID, *rec.files, rec)
		rec.files = nil
	}
	res = sanitizeResult(res)
	// State tools may still finish a call they started; after the grace period the token stops working
	m.expireRunToken(runID, runTokenGrace)
	if m.runEnded != nil {
		m.runEnded(runID)
	}
	if err := m.saveResult(runID, res); err != nil {
		// Never leave a run stuck as "running": record at least its status
		reason := fmt.Sprintf("The run ended (%s) but its result could not be saved: %v", res.Status, err)
		res = store.RunResult{Status: res.Status, Error: &reason}
		ctx, cancel := context.WithTimeout(context.Background(), dbWriteTimeout)
		if ferr := m.store.ForceFinishRun(ctx, runID, res.Status, reason); ferr != nil {
			m.log.Error("could not record run status", "run_id", runID, "err", ferr)
		}
		cancel()
	}
	payload := map[string]any{"status": res.Status}
	if res.Error != nil {
		payload["error"] = *res.Error
	}
	rec.record(model.RunEvent{Type: "run_status", CreatedAt: time.Now(), Payload: payload}, nil)
	m.broker.CloseRun(runID)
	m.log.Info("run finished", "run_id", runID, "status", res.Status)
}

const saveAttempts = 3

// saveTaskOutput keeps a finished task's output straight away, so the work survives a run that is killed or stopped
// before it reports its final result. A failure here is logged: the final result saves the output again.
func (m *Manager) saveTaskOutput(runID string, o model.TaskOutput) {
	clean := sanitizeResult(store.RunResult{TaskOutputs: []model.TaskOutput{o}}).TaskOutputs[0]
	ctx, cancel := context.WithTimeout(context.Background(), dbWriteTimeout)
	defer cancel()
	if err := m.store.SaveTaskOutput(ctx, runID, clean); err != nil {
		m.log.Error("save task output", "run_id", runID, "position", o.Position, "err", err)
	}
}

func (m *Manager) saveResult(runID string, res store.RunResult) error {
	var err error
	for attempt := 1; attempt <= saveAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), dbWriteTimeout)
		_, err = m.store.FinishRun(ctx, runID, res)
		cancel()
		if err == nil || errors.Is(err, store.ErrNotFound) {
			return nil // ErrNotFound: it was already finished (for example, cancelled)
		}
		m.log.Error("save run result", "run_id", runID, "attempt", attempt, "err", err)
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return err
}

// sanitizeResult removes NUL bytes, which Postgres text columns reject.
func sanitizeResult(res store.RunResult) store.RunResult {
	clean := func(p *string) *string {
		if p == nil {
			return nil
		}
		s := strings.ReplaceAll(*p, "\x00", "")
		return &s
	}
	res.Output = clean(res.Output)
	res.Error = clean(res.Error)
	outputs := make([]model.TaskOutput, len(res.TaskOutputs))
	for i, o := range res.TaskOutputs {
		o.Raw = strings.ReplaceAll(o.Raw, "\x00", "")
		o.TaskName = strings.ReplaceAll(o.TaskName, "\x00", "")
		o.AgentRole = strings.ReplaceAll(o.AgentRole, "\x00", "")
		outputs[i] = o
	}
	res.TaskOutputs = outputs
	return res
}

// handoffGrace is how long past its deadline a hand-off's runner process may live: the runner stops it at the
// deadline itself (its listeners refuse further LLM calls), and this stops a process that got stuck.
const handoffGrace = 2 * time.Minute

// runLimit is how long a run may take and what its error says when it takes longer: the server's maximum, or for a
// hand-off until its deadline plus handoffGrace when that comes sooner.
func runLimit(spec model.RunSpec, maxDuration time.Duration, now time.Time) (time.Duration, string) {
	limit := maxDuration
	message := fmt.Sprintf("The run took longer than %s and was stopped.", maxDuration)
	if spec.Chat == nil || spec.Chat.Handoff == nil {
		return limit, message
	}
	h := spec.Chat.Handoff
	if until := max(h.DeadlineAt.Sub(now), 0) + handoffGrace; until < limit {
		return until, fmt.Sprintf("The hand-off ran past its %d-minute limit and was stopped.", h.TimeoutMinutes)
	}
	return limit, message
}

// buildRequest prepares the run's folders and files and returns what the runner reads on stdin, with the frozen spec.
func (m *Manager) buildRequest(ctx context.Context, runID string) ([]byte, runPaths, model.RunSpec, error) {
	paths := m.pathsFor(runID)
	spec, err := m.store.GetRunSpec(ctx, runID)
	if err != nil {
		return nil, paths, spec, fmt.Errorf("Could not load the run spec: %v", err)
	}
	period := spec.Settings.GroundingFreePeriod
	if period == "" {
		period = model.GroundingPeriodDay // specs frozen before the period setting existed
	}
	grounded, err := m.store.GroundedSearchesInPeriod(ctx, period)
	if err != nil {
		return nil, paths, spec, fmt.Errorf("Could not count searches in the free-allowance period: %v", err)
	}
	if paths, err = m.prepareFiles(ctx, runID, spec); err != nil {
		return nil, paths, spec, err
	}
	req := map[string]any{
		"spec":                        spec,
		"grounded_searches_in_period": grounded,
		"workspace":                   paths.workspace,
		"transcript_path":             paths.transcript,
	}
	// The token goes on stdin, never in the environment, so tools and anything they start cannot read it there
	stateAPI, err := m.stateAPI(ctx, runID)
	if err != nil {
		return nil, paths, spec, err
	}
	if stateAPI != nil {
		req["state_api"] = stateAPI
	}
	request, err := json.Marshal(req)
	return request, paths, spec, err
}

// Environment variables the runner may inherit. Everything else (database URLs, cloud credentials,
// tokens in the shell) stays out of the process that runs model-directed tools.
var childEnvExact = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LANG": true, "TZ": true, "TMPDIR": true,
	"VIRTUAL_ENV": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "REQUESTS_CA_BUNDLE": true,
}

// Provider settings a person may export in their shell instead of the provider .env file.
var childEnvPrefixes = []string{"LC_", "OPENAI_", "ANTHROPIC_", "GEMINI_", "GOOGLE_API_", "LITELLM_"}

func allowedInChild(key string) bool {
	if childEnvExact[key] {
		return true
	}
	for _, prefix := range childEnvPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// childEnv passes an allow-listed slice of the parent environment plus switches that keep CrewAI quiet and
// local. Provider keys normally come from the provider .env file, which the runner loads itself.
func (m *Manager) childEnv() []string {
	env := make([]string, 0, 32)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if allowedInChild(key) {
			env = append(env, kv)
		}
	}
	return append(env,
		"STUDIO_PROVIDER_ENV_FILE="+m.proc.ProviderEnvFile,
		"PYTHONUNBUFFERED=1",
		"PYTHONIOENCODING=utf-8",
		"NO_COLOR=1",
		"TERM=dumb",
		"CREWAI_DISABLE_TELEMETRY=true",
		"CREWAI_TRACING_ENABLED=false",
		"OTEL_SDK_DISABLED=true",
	)
}

// runProcess starts the runner, feeds it the request, and records every line it prints.
func (m *Manager) runProcess(ctx context.Context, request []byte, workspace string, console *consoleWriter, rec *recorder) (outcome, []string, error) {
	cmd := exec.Command(m.proc.Python, m.proc.Script)
	cmd.Dir = workspace
	cmd.Env = m.childEnv()
	// Own process group, so cancelling also stops anything the runner spawned
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = strings.NewReader(string(request))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return outcome{}, nil, fmt.Errorf("open runner stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return outcome{}, nil, fmt.Errorf("open runner stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return outcome{errMsg: fmt.Sprintf("Could not start the Python runner: %v", err)}, nil, err
	}

	// limitCtx also stops the runner when it exceeds the per-run event limits
	limitCtx, stopForLimit := context.WithCancel(ctx)
	defer stopForLimit()
	exited := make(chan struct{})
	go stopOnCancel(limitCtx, cmd.Process.Pid, exited)
	var events, bytes int
	var limitHit string

	lines := make(chan outputLine, 256)
	var readers sync.WaitGroup
	readers.Add(2)
	go scanLines(stdout, "stdout", lines, &readers)
	go scanLines(stderr, "stderr", lines, &readers)
	go func() {
		readers.Wait()
		close(lines)
	}()

	var result outcome
	var tail []string
	for line := range lines {
		if limitHit != "" {
			continue // keep draining the pipes until the runner exits
		}
		events++
		bytes += len(line.text)
		if events > maxEventsPerRun || bytes > maxBytesPerRun {
			limitHit = fmt.Sprintf("The run produced more than %d events or %d MB of output and was stopped.",
				maxEventsPerRun, maxBytesPerRun>>20)
			stopForLimit()
			continue
		}
		now := time.Now()
		if line.stream == "stderr" {
			console.line(line.text)
			cleaned := cleanLogLine(line.text)
			if cleaned == "" {
				continue
			}
			tail = append(tail, cleaned)
			if len(tail) > tailLogLines {
				tail = tail[1:]
			}
			rec.record(logEvent(cleaned, now), nil)
			continue
		}
		t, ok := translateStdout(line.text, now)
		if !ok {
			continue
		}
		if t.Event.Type == "crew_completed" || t.Event.Type == "crew_failed" {
			result = readOutcome(t.Event.Type, t.Event.Payload)
			// The runner is done. If something it started keeps the output pipes open, stop the whole group
			// after a grace period instead of waiting for the run's time limit.
			go stopAfter(limitCtx, cmd.Process.Pid, exited, finishedGrace)
		}
		rec.record(t.Event, t.LLMCall)
		if o, ok := finishedTask(t.Event, rec.tasks); ok {
			m.saveTaskOutput(rec.runID, o)
		}
	}
	waitErr := cmd.Wait()
	close(exited)
	if limitHit != "" {
		result.completed = false
		result.errMsg = limitHit
	}
	return result, tail, waitErr
}

func scanLines(r io.Reader, stream string, out chan<- outputLine, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxRunnerLineBytes)
	for scanner.Scan() {
		out <- outputLine{stream: stream, text: scanner.Text()}
	}
	if err := scanner.Err(); err != nil {
		out <- outputLine{stream: "stderr", text: fmt.Sprintf("[crew studio] could not read runner %s: %v", stream, err)}
		// Drain so the runner never blocks on a full pipe
		_, _ = io.Copy(io.Discard, r)
	}
}

const finishedGrace = 30 * time.Second

// stopAfter kills the runner's process group if it is still around grace after reporting its result.
func stopAfter(ctx context.Context, pid int, exited <-chan struct{}, grace time.Duration) {
	select {
	case <-exited:
	case <-ctx.Done():
	case <-time.After(grace):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

// stopOnCancel sends SIGTERM to the runner's process group when ctx ends, then SIGKILL if needed.
func stopOnCancel(ctx context.Context, pid int, exited <-chan struct{}) {
	select {
	case <-exited:
		return
	case <-ctx.Done():
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(killGracePeriod):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
