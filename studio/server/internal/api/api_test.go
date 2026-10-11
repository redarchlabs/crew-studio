package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runner"
	"crewstudio/internal/scheduler"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

type fakeRuns struct {
	notified atomic.Int32
	st       *store.Store
}

func (f *fakeRuns) Notify() { f.notified.Add(1) }

func (f *fakeRuns) Cancel(ctx context.Context, id string) (model.Run, error) {
	msg := "Cancelled."
	return f.st.FinishRun(ctx, id, store.RunResult{Status: model.RunCancelled, Error: &msg})
}

type fakeTools struct{}

func (fakeTools) Tools() ([]model.Tool, error) {
	return []model.Tool{
		{Key: "google_search", Name: "Google Search", RequiresEnv: []string{"GEMINI_API_KEY"}, Available: true, Effect: model.EffectReads, Needs: []string{}},
		{Key: "tracker_get_issue", Name: "Get Tracker Issue", RequiresEnv: []string{}, Available: true, Effect: model.EffectReads,
			Needs: []string{model.NeedTracker}},
		{Key: "tracker_comment", Name: "Comment on Tracker Issue", RequiresEnv: []string{}, Available: true, Effect: model.EffectChanges,
			Needs: []string{model.NeedTracker}},
		{Key: "km_search", Name: "Search Knowledge (KM2)", RequiresEnv: []string{}, Available: true, Effect: model.EffectReads,
			Needs: []string{model.NeedKM2}},
	}, nil
}

// EnvSet says only OPENAI_API_KEY is set in the provider env file.
func (fakeTools) EnvSet(keys ...string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = k == "OPENAI_API_KEY"
	}
	return out, nil
}

func (f fakeTools) ByKey() (map[string]model.Tool, error) {
	tools, _ := f.Tools()
	out := map[string]model.Tool{}
	for _, t := range tools {
		out[t.Key] = t
	}
	return out, nil
}

// fakeSSO stands in for Clerk/Microsoft: Begin returns a URL on the provider, Complete returns identity.
type fakeSSO struct {
	identity auth.Identity
	err      error
}

func (f *fakeSSO) Providers() []auth.ProviderInfo {
	return []auth.ProviderInfo{{Key: "fake", Label: "Fake"}}
}
func (f *fakeSSO) Has(key string) bool { return key == "fake" }
func (f *fakeSSO) Begin(_ context.Context, _ string) (auth.LoginStart, error) {
	return auth.LoginStart{URL: "https://idp.example/authorize", State: auth.RandomToken(16), Nonce: "n", CodeVerifier: "v"}, nil
}
func (f *fakeSSO) Complete(_ context.Context, _, code, verifier, nonce string) (auth.Identity, error) {
	if code != "good" || verifier != "v" || nonce != "n" {
		return auth.Identity{}, errors.New("bad exchange")
	}
	return f.identity, f.err
}

type testEnv struct {
	srv    *httptest.Server
	st     *store.Store
	runs   *fakeRuns
	broker *runner.Broker
	files  *storage.Memory
	sso    *fakeSSO
	sched  *scheduler.Scheduler
	// token is the admin's session; requests use it unless they set their own credentials
	token   string
	adminID string
}

const (
	testSetupToken = "setup-token-for-tests"
	// Seeded by migration 0004
	financeArea = "55555555-0000-4000-8000-000000000001"
	itArea      = "55555555-0000-4000-8000-000000000002"
	hrArea      = "55555555-0000-4000-8000-000000000004"
	builderRole = "66666666-0000-4000-8000-000000000002"
	viewerRole  = "66666666-0000-4000-8000-000000000004"
	adminRole   = "66666666-0000-4000-8000-000000000001"
)

func newEnv(t *testing.T) testEnv {
	t.Helper()
	e := newBareEnv(t)
	id, err := e.st.CreateFirstAdmin(context.Background(), "admin@example.com", "Ada Admin", mustHash(t, "correct horse battery"))
	if err != nil {
		t.Fatal(err)
	}
	e.adminID = id
	e.token = e.session(t, id)
	return e
}

// newBareEnv has no accounts yet (first-run setup is pending).
func newBareEnv(t *testing.T) testEnv {
	t.Helper()
	st := store.New(testdb.New(t, "api_test"))
	runs := &fakeRuns{st: st}
	broker := runner.NewBroker()
	files := storage.NewMemory()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	launcher := launch.New(st, fakeTools{}, runs)
	sched := scheduler.New(st, launcher, log)
	sso := &fakeSSO{}
	webDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>studio</html>"), 0o600)
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	t.Cleanup(srv.Close)
	srv.Config.Handler = NewServer(Deps{
		Store: st, Runs: runs, Broker: broker, Tools: fakeTools{}, Launcher: launcher, Scheduler: sched,
		Files: files, SSO: sso, Log: log,
	}, Options{
		AllowedOrigins: []string{"http://127.0.0.1:5174"}, WebDir: webDir, PublicURL: srv.URL, LocalLogin: true,
		SetupToken: testSetupToken,
	}).Handler()
	return testEnv{srv: srv, st: st, runs: runs, broker: broker, files: files, sso: sso, sched: sched}
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	h, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (e testEnv) session(t *testing.T, userID string) string {
	t.Helper()
	token, err := e.st.CreateSession(context.Background(), userID, "test", "127.0.0.1", "go-test")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// as returns the env acting with another session token ("" = anonymous).
func (e testEnv) as(token string) testEnv {
	e.token = token
	return e
}

type response struct {
	status  int
	header  http.Header
	cookies []*http.Cookie
	body    struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   *apiError       `json:"error"`
		Meta    *model.Page     `json:"meta"`
	}
}

var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (e testEnv) do(t *testing.T, method, path string, body any, headers ...string) response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	if e.token != "" && req.Header.Get("Authorization") == "" && req.Header.Get("Cookie") == "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	}
	return e.send(t, req)
}

func (e testEnv) send(t *testing.T, req *http.Request) response {
	t.Helper()
	res, err := noRedirects.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out := response{status: res.StatusCode, header: res.Header, cookies: res.Cookies()}
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusFound {
		raw, _ := io.ReadAll(res.Body)
		if err := json.Unmarshal(raw, &out.body); err != nil {
			t.Fatalf("%s %s: response is not an envelope: %s", req.Method, req.URL.Path, raw)
		}
	}
	return out
}

func decodeData[T any](t *testing.T, r response) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.body.Data, &v); err != nil {
		t.Fatalf("decode data: %v (%s)", err, r.body.Data)
	}
	return v
}

func wantStatus(t *testing.T, r response, status int) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status = %d, want %d; error = %+v", r.status, status, r.body.Error)
	}
}

func (e testEnv) crewByName(t *testing.T, name string) model.Crew {
	t.Helper()
	crews := decodeData[[]model.Crew](t, e.do(t, "GET", "/api/crews", nil))
	for _, c := range crews {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("crew %q not found", name)
	return model.Crew{}
}

func TestAgentCRUDAndErrors(t *testing.T) {
	e := newEnv(t)

	r := e.do(t, "POST", "/api/agents", map[string]any{"name": "Scout", "role": "Scout", "goal": "g", "backstory": "b", "max_iter": 5,
		"tools": []string{"google_search"}, "area_ids": []string{financeArea}})
	wantStatus(t, r, http.StatusCreated)
	agent := decodeData[model.Agent](t, r)
	if !r.body.Success || agent.ID == "" || len(agent.Tools) != 1 {
		t.Fatalf("created = %+v", agent)
	}

	r = e.do(t, "POST", "/api/agents", map[string]any{"name": "Bad", "role": "", "goal": "g", "backstory": "b", "max_iter": 0, "tools": []string{"nope"}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if r.body.Error.Code != "validation_failed" || len(r.body.Error.Details) != 3 {
		t.Errorf("error = %+v", r.body.Error)
	}

	r = e.do(t, "POST", "/api/agents", map[string]any{"name": "Scout", "role": "r", "goal": "g", "backstory": "b", "max_iter": 5,
		"area_ids": []string{financeArea}})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "conflict" {
		t.Errorf("a conflict without its own code should report %q, got %q", "conflict", r.body.Error.Code)
	}

	agent.Goal = "new goal"
	r = e.do(t, "PUT", "/api/agents/"+agent.ID, agent)
	wantStatus(t, r, http.StatusOK)
	if decodeData[model.Agent](t, r).Goal != "new goal" {
		t.Error("update not applied")
	}

	wantStatus(t, e.do(t, "GET", "/api/agents/not-a-uuid", nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/agents/99999999-0000-4000-8000-000000000000", nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", "/api/agents/"+agent.ID, nil), http.StatusNoContent)

	r = e.do(t, "GET", "/api/history?entity_type=agent&entity_id="+agent.ID, nil)
	wantStatus(t, r, http.StatusOK)
	if r.body.Meta == nil || r.body.Meta.Total != 3 {
		t.Errorf("history meta = %+v", r.body.Meta)
	}
	wantStatus(t, e.do(t, "GET", "/api/history?entity_type=bogus", nil), http.StatusUnprocessableEntity)
}

func TestDeleteInUseReturnsConflict(t *testing.T) {
	e := newEnv(t)
	llms := decodeData[[]model.LLM](t, e.do(t, "GET", "/api/llms", nil))
	var used model.LLM
	for _, l := range llms {
		if l.Name == "GPT-6 Luna (research)" {
			used = l
		}
	}
	r := e.do(t, "DELETE", "/api/llms/"+used.ID, nil)
	wantStatus(t, r, http.StatusConflict)
	if !strings.Contains(r.body.Error.Message, "still used by") {
		t.Errorf("message = %q", r.body.Error.Message)
	}
}

func TestRequestGuards(t *testing.T) {
	e := newEnv(t)

	req, _ := http.NewRequest("POST", e.srv.URL+"/api/llms", strings.NewReader(`{"name":"x","model":"m"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST = %d, want 415", res.StatusCode)
	}
	res.Body.Close()

	r := e.do(t, "GET", "/api/crews", nil, "Origin", "https://evil.example")
	wantStatus(t, r, http.StatusForbidden)
	r = e.do(t, "GET", "/api/crews", nil, "Origin", "http://127.0.0.1:5174")
	wantStatus(t, r, http.StatusOK)

	req, _ = http.NewRequest("POST", e.srv.URL+"/api/llms", strings.NewReader(`{"name": `))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("bad JSON = %d, want 400", res.StatusCode)
	}
	res.Body.Close()

	wantStatus(t, e.do(t, "GET", "/api/nope", nil), http.StatusNotFound)
}

func TestSettingsAndTools(t *testing.T) {
	e := newEnv(t)
	settings := decodeData[model.Settings](t, e.do(t, "GET", "/api/settings", nil))
	settings.MaxConcurrentRuns = 3
	wantStatus(t, e.do(t, "PUT", "/api/settings", settings), http.StatusOK)
	if e.runs.notified.Load() == 0 {
		t.Error("changing settings should wake the dispatcher")
	}
	settings.HardStopMultiplier = 0
	wantStatus(t, e.do(t, "PUT", "/api/settings", settings), http.StatusUnprocessableEntity)

	// Each tool says what it does outside the run and what it needs, for the tool picker's chips
	tools := decodeData[[]model.Tool](t, e.do(t, "GET", "/api/tools", nil))
	if len(tools) != 4 || tools[0].Key != "google_search" || tools[0].Effect != model.EffectReads || tools[0].Needs == nil ||
		tools[2].Effect != model.EffectChanges || !slices.Equal(tools[1].Needs, []string{model.NeedTracker}) {
		t.Errorf("tools = %+v", tools)
	}
	overview := decodeData[model.Overview](t, e.do(t, "GET", "/api/overview", nil))
	// The two imported crews and the PMO template (migration 0016)
	if overview.Counts.Crews != 3 || overview.Counts.Pricing != 9 || overview.Counts.LLMs != 9 {
		t.Errorf("overview = %+v", overview.Counts)
	}
}

func TestValidateAndStartRun(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")

	v := decodeData[validation](t, e.do(t, "GET", "/api/crews/"+crew.ID+"/validate", nil))
	if !v.Valid {
		t.Fatalf("seeded crew should be runnable: %v", v.Errors)
	}

	r := e.do(t, "POST", "/api/crews/"+crew.ID+"/runs", map[string]any{"inputs": map[string]string{"rogue": "x"}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	r = e.do(t, "POST", "/api/crews/"+crew.ID+"/runs", map[string]any{"inputs": map[string]string{"company": "Acme"}, "budget_usd": 5000})
	wantStatus(t, r, http.StatusUnprocessableEntity)

	r = e.do(t, "POST", "/api/crews/"+crew.ID+"/runs", map[string]any{"inputs": map[string]string{"company": "Acme", "location": "Ohio"}, "budget_usd": 1.5})
	wantStatus(t, r, http.StatusCreated)
	run := decodeData[model.Run](t, r)
	if run.Status != model.RunQueued || run.BudgetUSD != 1.5 || run.Inputs["location"] != "Ohio" || e.runs.notified.Load() == 0 {
		t.Errorf("run = %+v", run)
	}

	detail := decodeData[model.Run](t, e.do(t, "GET", "/api/runs/"+run.ID, nil))
	if detail.Spec == nil || len(detail.Spec.Tasks) != 5 || len(detail.Spec.Pricing) != 9 || detail.Spec.Settings.SearchModel == "" {
		t.Errorf("spec not frozen: %+v", detail.Spec)
	}

	r = e.do(t, "GET", "/api/runs?crew_id="+crew.ID+"&limit=1", nil)
	if r.body.Meta == nil || r.body.Meta.Total != 1 || r.body.Meta.Limit != 1 {
		t.Errorf("runs meta = %+v", r.body.Meta)
	}

	graph := decodeData[struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}](t, e.do(t, "GET", "/api/runs/"+run.ID+"/interactions", nil))
	if len(graph.Nodes) != 1+5+5 || len(graph.Edges) == 0 {
		t.Errorf("planned graph = %d nodes, %d edges", len(graph.Nodes), len(graph.Edges))
	}

	wantStatus(t, e.do(t, "DELETE", "/api/runs/"+run.ID, nil), http.StatusConflict)
	cancelled := decodeData[model.Run](t, e.do(t, "POST", "/api/runs/"+run.ID+"/cancel", map[string]any{}))
	if cancelled.Status != model.RunCancelled {
		t.Errorf("cancel = %+v", cancelled)
	}
	wantStatus(t, e.do(t, "GET", "/api/runs/"+run.ID+"/llm-calls", nil), http.StatusOK)
	wantStatus(t, e.do(t, "DELETE", "/api/runs/"+run.ID, nil), http.StatusNoContent)
}

func TestStartRunRefusesBrokenCrew(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "AI Research Crew")
	crew.TaskIDs = []string{}
	wantStatus(t, e.do(t, "PUT", "/api/crews/"+crew.ID, crew), http.StatusOK)
	r := e.do(t, "POST", "/api/crews/"+crew.ID+"/runs", map[string]any{})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "no tasks") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
}

func readSSE(t *testing.T, body io.Reader, until func(model.RunEvent) bool) []model.RunEvent {
	t.Helper()
	var events []model.RunEvent
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev model.RunEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("bad event: %s", line)
		}
		events = append(events, ev)
		if until(ev) {
			break
		}
	}
	return events
}

func TestStreamReplaysThenFollowsLive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	crew := e.crewByName(t, "AI Research Crew")
	run, _, _ := e.st.CreateRun(ctx, model.RunSpec{Crew: crew, BudgetUSD: 1}, store.RunMeta{AreaIDs: crew.AreaIDs})
	_, _ = e.st.StartRun(ctx, run.ID)
	now := time.Now()
	_ = e.st.AppendEvent(ctx, run.ID, model.RunEvent{Seq: 1, Type: "run_status", Payload: map[string]any{"status": "running"}, CreatedAt: now})
	_ = e.st.AppendEvent(ctx, run.ID, model.RunEvent{Seq: 2, Type: "task_started", Payload: map[string]any{"task_name": "x"}, CreatedAt: now})

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, "GET", e.srv.URL+"/api/runs/"+run.ID+"/stream?after=1", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}

	go func() {
		// Wait for the handler to subscribe, then publish live events like the runner would
		for i := 0; i < 100; i++ {
			time.Sleep(20 * time.Millisecond)
			live := model.RunEvent{Seq: 3, Type: "llm_call", Payload: map[string]any{}, CreatedAt: time.Now()}
			_ = e.st.AppendEvent(ctx, run.ID, live)
			e.broker.Publish(run.ID, live)
			final := model.RunEvent{Seq: 4, Type: "run_status", Payload: map[string]any{"status": "succeeded"}, CreatedAt: time.Now()}
			_ = e.st.AppendEvent(ctx, run.ID, final)
			_, _ = e.st.FinishRun(ctx, run.ID, store.RunResult{Status: model.RunSucceeded})
			e.broker.Publish(run.ID, final)
			e.broker.CloseRun(run.ID)
			return
		}
	}()

	events := readSSE(t, res.Body, func(ev model.RunEvent) bool { return ev.Type == "run_status" && ev.Payload["status"] == "succeeded" })
	seqs := make([]int, len(events))
	for i, ev := range events {
		seqs[i] = ev.Seq
	}
	if len(seqs) != 3 || seqs[0] != 2 || seqs[2] != 4 {
		t.Errorf("seqs = %v, want [2 3 4]", seqs)
	}

	// A finished run replays its stored events and closes
	req2, _ := http.NewRequest("GET", e.srv.URL+"/api/runs/"+run.ID+"/stream", nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	replay := readSSE(t, res2.Body, func(model.RunEvent) bool { return false })
	if len(replay) != 4 {
		t.Errorf("replay = %d events, want 4", len(replay))
	}

	events2 := decodeData[[]model.RunEvent](t, e.do(t, "GET", "/api/runs/"+run.ID+"/events?after=2", nil))
	if len(events2) != 2 {
		t.Errorf("events after 2 = %d", len(events2))
	}
}

func TestSPAFallbackAndHealth(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/", "/crews/123"} {
		res, err := http.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !strings.Contains(string(body), "studio") {
			t.Errorf("%s served %q", path, body)
		}
	}
	wantStatus(t, e.do(t, "GET", "/healthz", nil), http.StatusOK)
}

func TestHostAllowListBlocksDNSRebinding(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/crews", nil)
	req.Host = "evil.example:8088"
	req.Header.Set("Origin", "http://evil.example:8088")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("rebinding host = %d, want 421", res.StatusCode)
	}

	for _, host := range []string{"localhost:8088", "127.0.0.1:5174", "[::1]:8088"} {
		req, _ := http.NewRequest("GET", e.srv.URL+"/api/crews", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
		req.Host = host
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("host %s = %d, want 200", host, res.StatusCode)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	res, err := http.Get(e.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q", csp)
	}
}
