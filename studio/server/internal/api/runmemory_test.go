package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode"

	"crewstudio/internal/connections"
	"crewstudio/internal/km2"
	"crewstudio/internal/launch"
	"crewstudio/internal/memory"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
)

// letterEmbedder makes a 26-number vector of letter counts, so texts with the same letters are near-identical.
type letterEmbedder struct {
	configured bool
	fail       bool
	calls      int
}

func (l *letterEmbedder) Model() string    { return "letters:26" }
func (l *letterEmbedder) Configured() bool { return l.configured }
func (l *letterEmbedder) EmbedTexts(_ context.Context, texts []string, _ bool) ([][]float32, error) {
	l.calls++
	if l.fail {
		return nil, errors.New("HTTP 500 from provider")
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, 26)
		v[0] = 0.01
		for _, r := range strings.ToLower(t) {
			if r >= 'a' && r <= 'z' {
				v[r-'a']++
			} else if unicode.IsDigit(r) {
				v[25] += 3
			}
		}
		out[i] = v
	}
	return out, nil
}

type memoryEnv struct {
	km2Env
	emb *letterEmbedder
}

// newMemoryEnv is newKM2Env with a memory service on a letter-counting embedder.
func newMemoryEnv(t *testing.T) memoryEnv {
	t.Helper()
	ke := newKM2Env(t)
	emb := &letterEmbedder{configured: true}
	box, err := secrets.NewBox(secrets.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	conns := connections.New(ke.st, box)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ke.srv.Config.Handler = NewServer(Deps{
		Store: ke.st, Runs: ke.runs, Broker: ke.broker, Tools: fakeTools{}, Launcher: launch.New(ke.st, fakeTools{}, ke.runs),
		Scheduler: ke.sched, Files: ke.files, SSO: ke.sso, Log: log, Connections: conns,
		KM2:    km2.NewService(ke.st, conns, km2.Options{HTTP: http.DefaultClient}),
		Memory: memory.NewService(emb, memory.Options{Unconfigured: "needs a key"}, ke.st, log),
	}, Options{AllowedOrigins: []string{"http://127.0.0.1:5174"}, PublicURL: ke.srv.URL, LocalLogin: true}).Handler()
	return memoryEnv{km2Env: ke, emb: emb}
}

// memoryRun is a run of the PE crew whose frozen spec has memory in mode, with review of its saves off.
func (e memoryEnv) memoryRun(t *testing.T, mode string, savesPerRun int) stateRun {
	t.Helper()
	return e.memoryRunWith(t, mode, savesPerRun, false)
}

// reviewedRun is memoryRun with review on: everything the run saves waits as pending.
func (e memoryEnv) reviewedRun(t *testing.T, mode string, savesPerRun int) stateRun {
	t.Helper()
	return e.memoryRunWith(t, mode, savesPerRun, true)
}

func (e memoryEnv) memoryRunWith(t *testing.T, mode string, savesPerRun int, review bool) stateRun {
	t.Helper()
	ctx := context.Background()
	crew := e.crewByName(t, "PE Due Diligence Crew")
	spec := model.RunSpec{Crew: crew, BudgetUSD: 1, Memory: &model.MemorySpec{Mode: mode, RecallDepth: model.MemoryShallow, RecallLimit: 5,
		SavesPerRun: savesPerRun, ReviewRunSaves: review, EmbedModel: "letters:26"}}
	run, _, err := e.st.CreateRun(ctx, spec, store.RunMeta{AreaIDs: crew.AreaIDs})
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.st.CreateRunToken(ctx, run.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return stateRun{run: run, crew: crew, token: token}
}

func wantCode(t *testing.T, r response, status int, code string) {
	t.Helper()
	wantStatus(t, r, status)
	if r.body.Error == nil || r.body.Error.Code != code {
		t.Fatalf("error = %+v, want %s", r.body.Error, code)
	}
}

type saveAnswer struct {
	Saved          []store.MemorySaved `json:"saved"`
	Skipped        int                 `json:"skipped"`
	RemainingSaves int                 `json:"remaining_saves"`
}

type searchAnswer struct {
	Matches []struct {
		Record model.MemoryRecord `json:"record"`
		Score  float64            `json:"score"`
	} `json:"matches"`
}

func TestRunMemoryModes(t *testing.T) {
	e := newMemoryEnv(t)
	// No memory in the spec: off
	off := e.scheduledRun(t)
	wantCode(t, e.call(t, off, "POST", "/memory/embed", map[string]any{"texts": []string{"x"}}), http.StatusForbidden, "memory_off")

	recall := e.memoryRun(t, model.MemoryRecallOnly, 20)
	r := e.call(t, recall, "POST", "/memory/embed", map[string]any{"texts": []string{"Sam is late", "Sam is late"}, "kind": "query"})
	wantStatus(t, r, http.StatusOK)
	emb := decodeData[memoryEmbedResponse](t, r)
	if emb.Model != "letters:26" || emb.Dimensions != 26 || len(emb.Vectors) != 2 {
		t.Errorf("embed = %+v", emb)
	}
	for _, path := range []string{"/memory/records", "/memory/delete", "/memory/touch"} {
		wantCode(t, e.call(t, recall, "POST", path, map[string]any{"records": []map[string]any{{"content": "x"}}}), http.StatusForbidden, "memory_read_only")
	}
	wantCode(t, e.call(t, recall, "PATCH", "/memory/records/"+missingID, map[string]any{}), http.StatusForbidden, "memory_read_only")
	for _, body := range []map[string]any{{"texts": []string{}}, {"texts": []string{" "}}, {"texts": []string{"a"}, "kind": "other"}} {
		wantStatus(t, e.call(t, recall, "POST", "/memory/embed", body), http.StatusUnprocessableEntity)
	}

	// The run's embed cap counts texts sent to the provider; cached texts are free
	if _, err := e.st.AddMemoryEmbeds(context.Background(), recall.run.ID, memoryEmbedCapPerRun-1, memoryEmbedCapPerRun); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.call(t, recall, "POST", "/memory/embed", map[string]any{"texts": []string{"Sam is late"}, "kind": "query"}), http.StatusOK)
	wantCode(t, e.call(t, recall, "POST", "/memory/embed", map[string]any{"texts": []string{"new one", "and another"}}), http.StatusTooManyRequests, "memory_embed_cap")

	// The provider failing, or no embedder at all, is 503
	e.emb.fail = true
	wantCode(t, e.call(t, e.memoryRun(t, model.MemoryExplicit, 5), "POST", "/memory/embed", map[string]any{"texts": []string{"zz"}}), http.StatusServiceUnavailable, "memory_unavailable")
	e.emb.fail, e.emb.configured = false, false
	wantCode(t, e.call(t, recall, "POST", "/memory/search", map[string]any{"embedding": emb.Vectors[0]}), http.StatusServiceUnavailable, "memory_unavailable")
	e.emb.configured = true
}

func TestRunMemorySaveSearchAndOwnership(t *testing.T) {
	e := newMemoryEnv(t)
	explicit := e.memoryRun(t, model.MemoryExplicit, 3)
	r := e.call(t, explicit, "POST", "/memory/records", map[string]any{"records": []map[string]any{
		{"content": "Sam usually replies late on Fridays", "scope": "/crew/" + explicit.crew.ID + "/People/Sam", "categories": []string{"People"},
			"metadata": map[string]any{"task": "Daily sweep", "agent": "PMO Coordinator"}, "importance": 0.8},
		{"content": "The vendor status page lies about partial outages"},
	}})
	wantStatus(t, r, http.StatusCreated)
	saved := decodeData[saveAnswer](t, r)
	if len(saved.Saved) != 2 || saved.Saved[0].Status != model.MemoryActive || saved.Saved[0].Duplicate || saved.RemainingSaves != 1 {
		t.Fatalf("saved = %+v", saved)
	}
	samID := saved.Saved[0].ID
	rec := decodeData[model.MemoryRecord](t, e.call(t, explicit, "GET", "/memory/records/"+samID, nil))
	if rec.Scope != "/people/sam" || rec.TaskName != "Daily sweep" || rec.AgentRole != "PMO Coordinator" || rec.Source != "run:"+explicit.run.ID ||
		rec.WrittenBy != "run" || rec.Importance != 0.8 || rec.Categories[0] != "people" {
		t.Errorf("record = %+v", rec)
	}

	// The same lesson again (exactly, or nearly) is not saved twice and costs nothing
	r = e.call(t, explicit, "POST", "/memory/records", map[string]any{"records": []map[string]any{
		{"content": "sam usually replies LATE on fridays", "scope": "/people/sam"},
		{"content": "Sam usually replies late on Fridays!", "scope": "/people/sam"},
	}})
	wantStatus(t, r, http.StatusCreated)
	if again := decodeData[saveAnswer](t, r); !again.Saved[0].Duplicate || !again.Saved[1].Duplicate || again.Saved[1].ID != samID || again.RemainingSaves != 1 {
		t.Errorf("again = %+v", again)
	}
	// Past the cap: skipped, then refused
	r = e.call(t, explicit, "POST", "/memory/records", map[string]any{"records": []map[string]any{{"content": "Budget reviews run on Mondays"}, {"content": "Quarter closes in 2026"}}})
	if capped := decodeData[saveAnswer](t, r); len(capped.Saved) != 2 || capped.Saved[1].Skipped != store.SkipSaveCap || capped.Saved[1].ID != "" ||
		capped.Skipped != 1 || capped.RemainingSaves != 0 {
		t.Errorf("capped = %+v", capped)
	}
	wantCode(t, e.call(t, explicit, "POST", "/memory/records", map[string]any{"records": []map[string]any{{"content": "one more"}}}), http.StatusTooManyRequests, "memory_save_cap")
	r = e.call(t, explicit, "POST", "/memory/records", map[string]any{"records": []map[string]any{{"content": ""}}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, ";"), "records[0].content") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	wantStatus(t, e.call(t, explicit, "POST", "/memory/records", map[string]any{"records": []map[string]any{}}), http.StatusUnprocessableEntity)

	// With review: saved pending, recalled only by the run that saved it
	auto := e.reviewedRun(t, model.MemoryAutomatic, 20)
	r = e.call(t, auto, "POST", "/memory/records", map[string]any{"records": []map[string]any{{"content": "PRJ-15 dates slipped three times", "scope": "/projects"}}})
	pendingID := decodeData[saveAnswer](t, r).Saved[0].ID
	if decodeData[saveAnswer](t, r).Saved[0].Status != model.MemoryPending {
		t.Error("automatic memory should be pending")
	}
	query := decodeData[memoryEmbedResponse](t, e.call(t, auto, "POST", "/memory/embed", map[string]any{"texts": []string{"PRJ-15 dates slipped"}, "kind": "query"})).Vectors[0]
	found := decodeData[searchAnswer](t, e.call(t, auto, "POST", "/memory/search", map[string]any{"embedding": query, "limit": 2}))
	if len(found.Matches) != 2 || found.Matches[0].Record.ID != pendingID || found.Matches[0].Score <= found.Matches[1].Score {
		t.Errorf("own search = %+v", found)
	}
	other := decodeData[searchAnswer](t, e.call(t, explicit, "POST", "/memory/search", map[string]any{"embedding": query, "scope_prefix": "/projects"}))
	if len(other.Matches) != 0 {
		t.Errorf("another run recalled a pending memory: %+v", other)
	}
	wantStatus(t, e.call(t, explicit, "GET", "/memory/records/"+pendingID, nil), http.StatusNotFound)
	for _, body := range []map[string]any{{"embedding": []float32{}}, {"embedding": query, "limit": 99}, {"embedding": query, "min_score": 2},
		{"embedding": query, "scope_prefix": "/../x"}, {"embedding": query, "categories": []string{"Bad/Cat"}}} {
		wantStatus(t, e.call(t, explicit, "POST", "/memory/search", body), http.StatusUnprocessableEntity)
	}

	// A run changes only what it saved
	wantCode(t, e.call(t, auto, "PATCH", "/memory/records/"+samID, map[string]any{"content": "x"}), http.StatusForbidden, "memory_not_owned")
	wantStatus(t, e.call(t, auto, "PATCH", "/memory/records/"+pendingID, map[string]any{"pinned": true}), http.StatusUnprocessableEntity)
	calls := e.emb.calls
	upd := decodeData[model.MemoryRecord](t, e.call(t, auto, "PATCH", "/memory/records/"+pendingID,
		map[string]any{"content": "PRJ-15 dates slipped four times", "scope": "/projects/prj-15", "categories": []string{"dates"}, "importance": 0.9}))
	if upd.Content != "PRJ-15 dates slipped four times" || upd.Scope != "/projects/prj-15" || upd.Status != model.MemoryPending || e.emb.calls != calls+1 {
		t.Errorf("update = %+v (calls %d -> %d)", upd, calls, e.emb.calls)
	}

	// Listing, scopes, info, categories, count (what this run may see)
	list := decodeData[memoryRecordsResponse](t, e.call(t, auto, "GET", "/memory/records?scope_prefix=/projects&limit=10", nil))
	if len(list.Records) != 1 {
		t.Errorf("list = %+v", list)
	}
	wantStatus(t, e.call(t, auto, "GET", "/memory/records?limit=500", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, auto, "GET", "/memory/records?offset=-1", nil), http.StatusUnprocessableEntity)
	scopes := decodeData[memoryScopesResponse](t, e.call(t, auto, "GET", "/memory/scopes?parent=/", nil))
	if strings.Join(scopes.Scopes, ",") != "/people,/projects" {
		t.Errorf("scopes = %v", scopes.Scopes)
	}
	info := decodeData[model.MemoryScopeInfo](t, e.call(t, auto, "GET", "/memory/scope-info?scope=/projects", nil))
	if info.RecordCount != 1 || info.Categories[0] != "dates" || strings.Join(info.ChildScopes, ",") != "/projects/prj-15" || info.OldestRecord == nil {
		t.Errorf("info = %+v", info)
	}
	if root := decodeData[model.MemoryScopeInfo](t, e.call(t, auto, "GET", "/memory/scope-info", nil)); root.Path != "/" || root.RecordCount != 4 {
		t.Errorf("root info = %+v", root)
	}
	cats := decodeData[memoryCategoriesResponse](t, e.call(t, explicit, "GET", "/memory/categories", nil))
	if cats.Categories["people"] != 1 || cats.Categories["dates"] != 0 {
		t.Errorf("categories = %v", cats)
	}
	if n := decodeData[memoryCountResponse](t, e.call(t, explicit, "GET", "/memory/count?scope_prefix=/people", nil)); n.Count != 1 {
		t.Errorf("count = %+v", n)
	}
	for _, path := range []string{"/memory/count?scope_prefix=/a/../b", "/memory/categories?scope_prefix=/a%20b", "/memory/scopes?parent=/..", "/memory/scope-info?scope=/.."} {
		wantStatus(t, e.call(t, explicit, "GET", path, nil), http.StatusUnprocessableEntity)
	}
	if touched := decodeData[memoryTouchResponse](t, e.call(t, explicit, "POST", "/memory/touch", map[string]any{"record_ids": []string{samID, pendingID}})); touched.Touched != 1 {
		t.Errorf("touched = %+v", touched)
	}
	wantStatus(t, e.call(t, explicit, "POST", "/memory/touch", map[string]any{"record_ids": []string{"nope"}}), http.StatusUnprocessableEntity)

	// Deleting: only the run's own memories
	if d := decodeData[memoryDeleteResponse](t, e.call(t, auto, "POST", "/memory/delete", map[string]any{"record_ids": []string{samID}})); d.Deleted != 0 {
		t.Errorf("deleted another run's memory: %+v", d)
	}
	if d := decodeData[memoryDeleteResponse](t, e.call(t, auto, "POST", "/memory/delete", map[string]any{"scope_prefix": "/projects", "categories": []string{"dates"}})); d.Deleted != 1 {
		t.Errorf("delete own = %+v", d)
	}
	wantStatus(t, e.call(t, auto, "POST", "/memory/delete", map[string]any{"record_ids": []string{"x"}}), http.StatusUnprocessableEntity)

	lines := strings.Join(e.stateLines(t, auto.run.ID), "\n")
	for _, want := range []string{"Memory: saved 1, waiting for review", "Memory: recalled 2 of 4", "Memory: change refused", "Memory: changed a memory in /projects/prj-15", "Memory: deleted 1"} {
		if !strings.Contains(lines, want) {
			t.Errorf("journal lacks %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "embed") {
		t.Errorf("embeds should not be journalled:\n%s", lines)
	}
	if lines := strings.Join(e.stateLines(t, explicit.run.ID), "\n"); !strings.Contains(lines, "Memory: saved 0 (2 already known)") ||
		!strings.Contains(lines, "1 skipped over the cap") {
		t.Errorf("explicit journal:\n%s", lines)
	}
}

func TestRunMemoryCrewFull(t *testing.T) {
	e := newMemoryEnv(t)
	run := e.memoryRun(t, model.MemoryExplicit, 20)
	if _, err := e.st.Pool().Exec(context.Background(), `INSERT INTO memories (crew_id, scope, content, content_hash, embed_model, vector, written_by)
		SELECT $1, '/', 'm' || g, 'h' || g, 'other:1', '{1}', 't' FROM generate_series(1, $2) g`, run.crew.ID, model.MaxMemoriesPerCrew); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.call(t, run, "POST", "/memory/records", map[string]any{"records": []map[string]any{{"content": "one too many"}}}), http.StatusTooManyRequests, "memory_crew_full")
}
