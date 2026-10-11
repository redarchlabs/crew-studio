package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/connections"
	"crewstudio/internal/km2"
	"crewstudio/internal/km2/km2test"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
)

const km2TestKey = "km2_api-test-key-secret"

type km2Env struct {
	testEnv
	data *km2test.Data
	url  string
}

// newKM2Env is newEnv with connections turned on and a fake KM2 the KM2 client may reach.
func newKM2Env(t *testing.T) km2Env {
	t.Helper()
	e := newEnv(t)
	box, err := secrets.NewBox(secrets.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	d := km2test.NewData(km2TestKey)
	srv := km2test.Server(t, d)
	conns := connections.New(e.st, box)
	e.srv.Config.Handler = NewServer(Deps{
		Store: e.st, Runs: e.runs, Broker: e.broker, Tools: fakeTools{}, Launcher: launch.New(e.st, fakeTools{}, e.runs),
		Scheduler: e.sched, Files: e.files, SSO: e.sso, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Connections: conns, KM2: km2.NewService(e.st, conns, km2.Options{HTTP: srv.Client()}),
	}, Options{AllowedOrigins: []string{"http://127.0.0.1:5174"}, PublicURL: e.srv.URL, LocalLogin: true}).Handler()
	return km2Env{testEnv: e, data: d, url: srv.URL}
}

// km2Connection creates a KM2 connection in the finance area with the Atlas folder as default and Projects listed.
func (e km2Env) km2Connection(t *testing.T, name string) model.Connection {
	t.Helper()
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": name, "kind": "km2", "area_ids": []string{financeArea},
		"config": map[string]string{"base_url": e.url, model.ConfigKM2DefaultFolder: km2test.FolderAtlas,
			model.ConfigKM2WriteFolders: km2test.FolderProjects},
		"secrets": map[string]string{"api_key": km2TestKey}})
	wantStatus(t, r, http.StatusCreated)
	return decodeData[model.Connection](t, r)
}

// km2Run is a scheduled run whose crew may use one KM2 connection.
func (e km2Env) km2Run(t *testing.T) (stateRun, model.Connection) {
	t.Helper()
	sr := e.scheduledRun(t)
	conn := e.km2Connection(t, "KM2 Finance")
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	return sr, conn
}

func (e testEnv) journalText(t *testing.T, runID string) string {
	t.Helper()
	events, err := e.st.ListEvents(context.Background(), runID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, ev := range events {
		if msg, ok := ev.Payload["message"].(string); ok {
			b.WriteString(msg + "\n")
		}
	}
	return b.String()
}

func TestKM2ConnectionTestListsFolders(t *testing.T) {
	e := newKM2Env(t)
	conn := e.km2Connection(t, "KM2 Finance")
	if !conn.Enabled || len(conn.MissingSecrets) != 0 || conn.Config[model.ConfigKM2DefaultFolder] != km2test.FolderAtlas {
		t.Fatalf("conn = %+v", conn)
	}
	r := e.do(t, "POST", "/api/connections/"+conn.ID+"/test", map[string]any{})
	wantStatus(t, r, http.StatusOK)
	res := decodeData[km2.TestResult](t, r)
	if !res.OK || res.FolderCount != 3 || len(res.Folders) != 3 || len(res.Warnings) != 0 || strings.Contains(string(r.body.Data), km2TestKey) {
		t.Errorf("test = %+v", res)
	}
	if reqs := e.data.Requests(); len(reqs) != 1 || reqs[0].Path != "/api/v1/knowledge/folders" {
		t.Errorf("requests = %+v", reqs)
	}

	// New folders keep the key; a new base URL drops it
	up := map[string]any{"name": conn.Name, "area_ids": []string{financeArea},
		"config": map[string]string{"base_url": e.url, model.ConfigKM2DefaultFolder: km2test.FolderProjects}}
	if got := decodeData[model.Connection](t, e.do(t, "PUT", "/api/connections/"+conn.ID, up)); len(got.SecretKeys) != 1 {
		t.Errorf("folder change dropped the key: %+v", got)
	}
	bad := map[string]any{"name": conn.Name, "area_ids": []string{financeArea}, "config": map[string]string{"base_url": e.url, model.ConfigKM2WriteFolders: "x"}}
	wantStatus(t, e.do(t, "PUT", "/api/connections/"+conn.ID, bad), http.StatusUnprocessableEntity)
	up["config"] = map[string]string{"base_url": "https://elsewhere.example"}
	if got := decodeData[model.Connection](t, e.do(t, "PUT", "/api/connections/"+conn.ID, up)); len(got.SecretKeys) != 0 {
		t.Errorf("a new base URL kept the key: %+v", got)
	}
	res = decodeData[km2.TestResult](t, e.do(t, "POST", "/api/connections/"+conn.ID+"/test", map[string]any{}))
	if res.OK || !strings.Contains(res.Message, "no API key") {
		t.Errorf("no key = %+v", res)
	}
}

func TestRunKM2SearchAndAsk(t *testing.T) {
	e := newKM2Env(t)
	sr, _ := e.km2Run(t)

	r := e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas", "top_k": 3})
	wantStatus(t, r, http.StatusOK)
	found := decodeData[km2SearchView](t, r)
	if found.Connection != "KM2 Finance" || found.Total != 1 || found.Hits[0].DocumentTitle != "Atlas charter" {
		t.Errorf("search = %+v", found)
	}
	wantStatus(t, e.call(t, sr, "POST", "/km2/search", map[string]any{"query": " "}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "x", "top_k": 99}), http.StatusUnprocessableEntity)

	r = e.call(t, sr, "POST", "/km2/ask", map[string]any{"question": "Who owns Atlas?"})
	wantStatus(t, r, http.StatusOK)
	answer := decodeData[km2AskView](t, r)
	if !strings.Contains(answer.Answer, "Dana") || len(answer.Sources) != 1 {
		t.Errorf("ask = %+v", answer)
	}
	wantStatus(t, e.call(t, sr, "POST", "/km2/ask", map[string]any{"question": ""}), http.StatusUnprocessableEntity)

	// KM2's failures reach the agent without the key
	e.data.Fail("/api/v1/search", km2test.Failure{Status: http.StatusUnauthorized})
	r = e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"})
	wantStatus(t, r, http.StatusBadGateway)
	if r.body.Error.Code != "km2_failed" || strings.Contains(toJSONString(r.body), km2TestKey) {
		t.Errorf("401 = %+v", r.body.Error)
	}
	e.data.Fail("/api/v1/search/chat", km2test.Failure{Status: http.StatusTooManyRequests, RetryAfter: "86400"})
	r = e.call(t, sr, "POST", "/km2/ask", map[string]any{"question": "x"})
	wantStatus(t, r, http.StatusTooManyRequests)
	if r.body.Error.Code != "km2_rate_limited" {
		t.Errorf("429 = %+v", r.body.Error)
	}
	e.data.SetScopes("knowledge:write")
	wantStatus(t, e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"}), http.StatusForbidden)

	journal := e.journalText(t, sr.run.ID)
	for _, want := range []string{"KM2: searched KM2 Finance, 1 passages", "KM2: asked KM2 Finance, answer with 1 sources",
		"KM2: search in KM2 Finance failed: KM2 refused the key", "KM2: search refused: query: is required"} {
		if !strings.Contains(journal, want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
	if strings.Contains(journal, km2TestKey) {
		t.Error("the key is in the journal")
	}
}

func TestRunKM2NeedsExactlyOneConnection(t *testing.T) {
	e := newKM2Env(t)
	sr := e.scheduledRun(t)
	r := e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "no_km2" {
		t.Errorf("none = %+v", r.body.Error)
	}
	a, b := e.km2Connection(t, "KM2 A"), e.km2Connection(t, "KM2 B")
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{a.ID, b.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	r = e.call(t, sr, "POST", "/km2/documents", map[string]any{"title": "t", "content": "c", "external_ref": "r"})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "km2_ambiguous" || !strings.Contains(r.body.Error.Message, "KM2 A, KM2 B") {
		t.Errorf("two = %+v", r.body.Error)
	}
	if len(e.data.Requests()) != 0 {
		t.Error("KM2 was called")
	}
}

func TestRunKM2SaveIsLoggedAndCapped(t *testing.T) {
	e := newKM2Env(t)
	sr, conn := e.km2Run(t)
	save := map[string]any{"title": "Atlas weekly W41", "content": "# Status\nOn track.", "external_ref": "atlas-weekly-2026-W41",
		"metadata": map[string]any{"project": "atlas", "source": "agent-cannot-override"}, "source_file": "reports/w41.md"}

	r := e.call(t, sr, "POST", "/km2/documents", save)
	wantStatus(t, r, http.StatusCreated)
	res := decodeData[km2SaveResult](t, r)
	if !res.Saved || res.Outcome != "created" || res.FolderID != km2test.FolderAtlas || res.Logged == nil ||
		res.Logged.Action != model.ActionKM2Save || res.Logged.Subject != "km2:atlas-weekly-2026-W41" || res.Logged.DryRun {
		t.Fatalf("save = %+v", res)
	}
	details := res.Logged.Details
	if details["outcome"] != "created" || details["external_ref"] != "atlas-weekly-2026-W41" || details["connection_id"] != conn.ID ||
		details["source_file"] != "reports/w41.md" || details["document_id"] != res.DocumentID {
		t.Errorf("details = %+v", details)
	}
	reqs := e.data.Requests()
	meta, _ := reqs[len(reqs)-1].Body["metadata"].(map[string]any)
	if meta["source"] != "crew-studio" || meta["run_id"] != sr.run.ID || meta["project"] != "atlas" || meta["schedule"] != "atlas sweep" {
		t.Errorf("metadata sent = %+v", meta)
	}

	// The same document again in this run is not sent again
	r = e.call(t, sr, "POST", "/km2/documents", save)
	wantStatus(t, r, http.StatusOK)
	if res := decodeData[km2SaveResult](t, r); !res.Saved || !res.Repeated || res.Outcome != "created" || !strings.Contains(res.Message, "already saved") || len(e.data.Saved()) != 1 {
		t.Errorf("repeat = %+v", res)
	}
	// A new version, into a folder named by path
	save["content"], save["folder"] = "# Status\nSlipping.", "Projects"
	res = decodeData[km2SaveResult](t, e.call(t, sr, "POST", "/km2/documents", save))
	if res.Outcome != "created" || res.FolderID != km2test.FolderProjects {
		t.Errorf("projects = %+v", res)
	}
	save["folder"] = "projects/atlas"
	if res = decodeData[km2SaveResult](t, e.call(t, sr, "POST", "/km2/documents", save)); res.Outcome != "updated" {
		t.Errorf("new version = %+v", res)
	}

	// Folders outside the connection's list, reserved metadata, and KM2's own refusals
	save["folder"] = "HR"
	r = e.call(t, sr, "POST", "/km2/documents", save)
	wantStatus(t, r, http.StatusForbidden)
	if r.body.Error.Code != "km2_folder_not_allowed" {
		t.Errorf("hr = %+v", r.body.Error)
	}
	delete(save, "folder")
	save["metadata"] = map[string]any{"access_keys": []any{1.0}}
	wantStatus(t, e.call(t, sr, "POST", "/km2/documents", save), http.StatusUnprocessableEntity)
	save["metadata"], save["content"] = map[string]any{}, "third"
	e.data.Fail("/api/v1/knowledge/documents", km2test.Failure{Status: http.StatusConflict, Detail: "the previous version is still processing"})
	r = e.call(t, sr, "POST", "/km2/documents", save)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "km2_conflict" || len(r.body.Error.Details) != 1 || r.body.Error.Details[0] != "the previous version is still processing" {
		t.Errorf("409 = %+v", r.body.Error)
	}
	wantStatus(t, e.call(t, sr, "POST", "/km2/documents", map[string]any{"title": "t", "content": "c"}), http.StatusUnprocessableEntity)

	// The per-run cap counts saves, not refusals: 3 so far
	for i := 3; i < maxKM2SavesPerRun; i++ {
		save["content"] = "version " + string(rune('a'+i))
		wantStatus(t, e.call(t, sr, "POST", "/km2/documents", save), http.StatusOK)
	}
	save["content"] = "one too many"
	r = e.call(t, sr, "POST", "/km2/documents", save)
	wantStatus(t, r, http.StatusTooManyRequests)
	if r.body.Error.Code != "km2_save_cap" {
		t.Errorf("cap = %+v", r.body.Error)
	}
	journal := e.journalText(t, sr.run.ID)
	for _, want := range []string{"KM2: saved atlas-weekly-2026-W41 to KM2 Finance (created)", "was already saved in this run",
		"KM2: save refused, the run already saved 10 documents"} {
		if !strings.Contains(journal, want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
}

func TestRunKM2DryRunNeverSends(t *testing.T) {
	e := newKM2Env(t)
	sr, _ := e.km2Run(t)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET inputs = '{"dry_run": "true"}' WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, sr, "POST", "/km2/documents", map[string]any{"title": "W41", "content": "# Status", "external_ref": "atlas-w41"})
	wantStatus(t, r, http.StatusOK)
	res := decodeData[km2SaveResult](t, r)
	if res.Saved || !res.DryRun || res.WouldSave == nil || res.WouldSave.Bytes != 8 || res.Logged == nil || !res.Logged.DryRun ||
		res.Logged.Details["would_save"] != true {
		t.Fatalf("dry run = %+v", res)
	}
	if len(e.data.Saved()) != 0 {
		t.Error("a dry run sent a document")
	}
	if !strings.Contains(e.journalText(t, sr.run.ID), "KM2: dry run, would save atlas-w41 to KM2 Finance (not sent)") {
		t.Error("journal")
	}
	// Reads still work in a dry run
	wantStatus(t, e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"}), http.StatusOK)
}

func TestChatTurnsMayOnlyReadKM2(t *testing.T) {
	e := newKM2Env(t)
	crew := e.chatCrew(t, 1)
	conn := e.km2Connection(t, "KM2 Finance")
	if err := e.st.SetCrewConnections(context.Background(), crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	_, sr := e.chatTurn(t, crew, "What do we know about Atlas?")
	spec, err := e.st.GetRunSpec(context.Background(), sr.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(spec.Chat.Tools); !strings.Contains(string(raw), "km_search") || !strings.Contains(string(raw), "km_ask") {
		t.Fatalf("chat tools = %s", raw)
	}
	wantStatus(t, e.call(t, sr, "POST", "/km2/search", map[string]any{"query": "atlas"}), http.StatusOK)
	r := e.call(t, sr, "POST", "/km2/documents", map[string]any{"title": "t", "content": "c", "external_ref": "r"})
	wantStatus(t, r, http.StatusForbidden)
	if len(e.data.Saved()) != 0 {
		t.Error("a chat turn saved to KM2")
	}
}

func TestRunKM2SaveOutcomesAndForgery(t *testing.T) {
	e := newKM2Env(t)
	sr, _ := e.km2Run(t)

	// Agents cannot record km2:save themselves (or take a save's idempotency key)
	r := e.call(t, sr, "POST", "/log", map[string]any{"subject": "km2:x", "action": model.ActionKM2Save})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/log", map[string]any{"subject": "x", "action": "report:weekly", "idempotency_key": "km2:" + sr.run.ID}),
		http.StatusUnprocessableEntity)

	// KM2 failed after the document was sent: the save is kept as unknown (it counts), and sending it again is allowed
	save := map[string]any{"title": "W41", "content": "# Status", "external_ref": "atlas-w41"}
	e.data.Fail("/api/v1/knowledge/documents", km2test.Failure{Status: http.StatusInternalServerError})
	wantStatus(t, e.call(t, sr, "POST", "/km2/documents", save), http.StatusBadGateway)
	saves, err := e.st.RunKM2Saves(context.Background(), sr.run.ID)
	if err != nil || saves != 1 {
		t.Fatalf("unknown save counted = %d, %v", saves, err)
	}
	r = e.call(t, sr, "POST", "/km2/documents", save)
	wantStatus(t, r, http.StatusCreated)
	res := decodeData[km2SaveResult](t, r)
	if res.Logged == nil || res.Logged.Details["outcome"] != "created" || len(e.data.Saved()) != 1 {
		t.Errorf("resent = %+v", res)
	}
	if saves, _ = e.st.RunKM2Saves(context.Background(), sr.run.ID); saves != 1 {
		t.Errorf("saves after the resend = %d", saves)
	}
}

func TestRunKM2AsksAreLimitedPerRun(t *testing.T) {
	e := newKM2Env(t)
	sr, _ := e.km2Run(t)
	for i := 0; i < km2AskBurst; i++ {
		wantStatus(t, e.call(t, sr, "POST", "/km2/ask", map[string]any{"question": "Who owns Atlas?"}), http.StatusOK)
	}
	r := e.call(t, sr, "POST", "/km2/ask", map[string]any{"question": "Again?"})
	wantStatus(t, r, http.StatusTooManyRequests)
	if !strings.Contains(r.body.Error.Message, "km_ask too often") {
		t.Errorf("limit = %+v", r.body.Error)
	}
	// Another run is not affected, and one run cannot hold more than two questions open
	run, _, err := e.st.CreateRun(context.Background(), model.RunSpec{Crew: sr.crew, BudgetUSD: 1}, store.RunMeta{AreaIDs: sr.crew.AreaIDs})
	if err != nil {
		t.Fatal(err)
	}
	other := e.scheduledRunOfNewToken(t, stateRun{run: run, crew: sr.crew})
	for i := 0; i < maxOpenKM2Asks; i++ {
		if !km2AsksOpen.acquire(other.run.ID) {
			t.Fatal("could not open a question")
		}
	}
	r = e.call(t, other, "POST", "/km2/ask", map[string]any{"question": "x"})
	wantStatus(t, r, http.StatusTooManyRequests)
	for i := 0; i < maxOpenKM2Asks; i++ {
		km2AsksOpen.release(other.run.ID)
	}
	wantStatus(t, e.call(t, other, "POST", "/km2/ask", map[string]any{"question": "x"}), http.StatusOK)
}
