package km2_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"crewstudio/internal/connections"
	"crewstudio/internal/km2"
	"crewstudio/internal/km2/km2test"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

var schemaSeq atomic.Int64

type svcEnv struct {
	st   *store.Store
	svc  *km2.Service
	data *km2test.Data
	crew model.Crew
	conn model.Connection
	area string
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// newSvcEnv is a crew in one area with a KM2 connection (fake server) in the same area, not linked yet.
func newSvcEnv(t *testing.T, config map[string]string) svcEnv {
	t.Helper()
	ctx := context.Background()
	schema := fmt.Sprintf("km2_svc_%d_%d", os.Getpid(), schemaSeq.Add(1))
	pool := testdb.New(t, schema)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
	st := store.New(pool)
	area, err := st.CreateArea(ctx, model.Area{Name: "Alpha"})
	must(t, err)
	llm, err := st.CreateLLM(ctx, model.LLM{Name: "L", Model: "openai/m"})
	must(t, err)
	agent, err := st.CreateAgent(ctx, model.Agent{Name: "A", Role: "r", Goal: "g", Backstory: "b", LLMID: &llm.ID, MaxIter: 3, Tools: []string{}, AreaIDs: []string{area.ID}})
	must(t, err)
	task, err := st.CreateTask(ctx, model.Task{Name: "T", Description: "d", ExpectedOutput: "e", AgentID: &agent.ID, AreaIDs: []string{area.ID}})
	must(t, err)
	crew, err := st.CreateCrew(ctx, model.Crew{Name: "C", Process: model.ProcessSequential, BudgetUSD: 1, AgentIDs: []string{agent.ID},
		TaskIDs: []string{task.ID}, AreaIDs: []string{area.ID}})
	must(t, err)
	box, err := secrets.NewBox(secrets.GenerateKey())
	must(t, err)
	conns := connections.New(st, box)
	d := km2test.NewData(testKey)
	srv := km2test.Server(t, d)
	cfg := map[string]string{"base_url": srv.URL}
	for k, v := range config {
		cfg[k] = v
	}
	in, err := model.NormalizeConnection(model.ConnectionInput{Name: "KM2 Alpha", Kind: model.KindKM2, AreaIDs: []string{area.ID},
		Config: cfg, Secrets: map[string]string{"api_key": testKey}})
	must(t, err)
	conn, err := conns.Create(ctx, in)
	must(t, err)
	svc := km2.NewService(st, conns, km2.Options{HTTP: srv.Client()})
	return svcEnv{st: st, svc: svc, data: d, crew: crew, conn: conn, area: area.ID}
}

func TestServiceForCrew(t *testing.T) {
	e := newSvcEnv(t, nil)
	ctx := context.Background()
	if _, _, err := e.svc.ForCrew(ctx, e.conn.ID, e.crew.ID); err == nil || !strings.Contains(err.Error(), "may not use") {
		t.Fatalf("unlinked: %v", err)
	}
	must(t, e.st.SetCrewConnections(ctx, e.crew.ID, []string{e.conn.ID}, store.AllAreas))
	conn, client, err := e.svc.ForCrew(ctx, e.conn.ID, e.crew.ID)
	if err != nil || conn.ID != e.conn.ID || client == nil {
		t.Fatalf("linked: %v", err)
	}
	if _, _, err := e.svc.ForCrew(ctx, "99999999-0000-4000-8000-000000000000", e.crew.ID); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("missing: %v", err)
	}
	for _, use := range []store.CrewConnectionUse{
		{Connection: model.Connection{Name: "j", Kind: "jira"}},
		{Connection: model.Connection{Name: "k", Kind: model.KindKM2}},
		{Connection: model.Connection{Name: "k", Kind: model.KindKM2, Enabled: true}, Linked: true},
	} {
		if km2.UseProblem(use) == "" {
			t.Errorf("no problem for %+v", use)
		}
	}
	if _, err := e.svc.Client(ctx, model.Connection{Name: "j", Kind: "jira"}); err == nil {
		t.Error("a jira connection got a KM2 client")
	}
	allowed := 0
	for i := 0; i < 60; i++ {
		if e.svc.Allow(e.conn.ID) {
			allowed++
		}
	}
	if allowed < 40 || allowed >= 60 {
		t.Errorf("allowed %d of 60 calls at once", allowed)
	}
}

func TestServiceTest(t *testing.T) {
	e := newSvcEnv(t, map[string]string{model.ConfigKM2DefaultFolder: km2test.FolderAtlas,
		model.ConfigKM2WriteFolders: "6a1f0000-0000-4000-8000-0000000000ff"})
	ctx := context.Background()
	res := e.svc.Test(ctx, e.conn)
	if !res.OK || !res.Supported || res.FolderCount != 3 || len(res.Folders) != 3 || res.Message != "Connected: the key sees 3 folders." {
		t.Fatalf("test = %+v", res)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "cannot see folder 6a1f0000-0000-4000-8000-0000000000ff") {
		t.Errorf("warnings = %v", res.Warnings)
	}
	e.data.SetScopes("search:read")
	res = e.svc.Test(ctx, e.conn)
	if res.OK || res.StatusCode != http.StatusForbidden || !strings.Contains(res.Message, "knowledge:read") {
		t.Errorf("no knowledge:read = %+v", res)
	}
	e.data.Fail("/api/v1/knowledge/folders", km2test.Failure{Status: http.StatusUnauthorized})
	e.data.SetScopes("knowledge:read")
	res = e.svc.Test(ctx, e.conn)
	if res.OK || !strings.Contains(res.Message, "refused the key") || strings.Contains(res.Message, testKey) {
		t.Errorf("revoked = %+v", res)
	}

	plain := newSvcEnv(t, nil)
	res = plain.svc.Test(ctx, plain.conn)
	if !res.OK || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "No folders are set") {
		t.Errorf("no folders = %+v", res)
	}
	remote := plain.conn
	remote.Config = map[string]string{"base_url": strings.Replace(remote.Config["base_url"], "127.0.0.1", "localhost", 1)}
	if res = plain.svc.Test(ctx, remote); len(res.Warnings) != 1 {
		t.Errorf("localhost over http = %+v", res)
	}
	listOnly := newSvcEnv(t, map[string]string{model.ConfigKM2WriteFolders: km2test.FolderAtlas})
	res = listOnly.svc.Test(ctx, listOnly.conn)
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "No default folder") {
		t.Errorf("no default = %+v", res)
	}
}

type fakeLister struct {
	folders []km2.Folder
	err     error
}

func (f fakeLister) Folders(context.Context) ([]km2.Folder, error) { return f.folders, f.err }

func TestResolveFolder(t *testing.T) {
	ctx := context.Background()
	c := conn("https://km2.example", map[string]string{model.ConfigKM2DefaultFolder: km2test.FolderProjects,
		model.ConfigKM2WriteFolders: km2test.FolderAtlas + "," + km2test.FolderHR})
	lister := fakeLister{folders: []km2.Folder{
		{ID: km2test.FolderProjects, Name: "Projects", Path: "Projects"},
		{ID: km2test.FolderAtlas, Name: "Atlas", Path: "Projects.Atlas"},
		{ID: km2test.FolderHR, Name: "Atlas", Path: "HR.Atlas"},
		{ID: "6a1f0000-0000-4000-8000-0000000000ee", Name: "Secret", Path: "Secret"},
	}}
	for wanted, id := range map[string]string{"": km2test.FolderProjects, strings.ToUpper(km2test.FolderAtlas): km2test.FolderAtlas,
		"projects / atlas": km2test.FolderAtlas, "Projects>Atlas": km2test.FolderAtlas, "hr.atlas": km2test.FolderHR} {
		f, err := km2.ResolveFolder(ctx, c, lister, wanted)
		if err != nil || f.ID != id {
			t.Errorf("%q = %+v, %v", wanted, f, err)
		}
	}
	for wanted, code := range map[string]string{"Atlas": "km2_folder_ambiguous", "Secret": "km2_folder_not_allowed",
		"6a1f0000-0000-4000-8000-0000000000ee": "km2_folder_not_allowed"} {
		_, err := km2.ResolveFolder(ctx, c, lister, wanted)
		fe, ok := err.(*km2.FolderError)
		if !ok || fe.Code != code || fe.Error() == "" {
			t.Errorf("%q = %v", wanted, err)
		}
	}
	_, err := km2.ResolveFolder(ctx, c, fakeLister{err: &km2.Error{Status: 403, Message: "no"}}, "Atlas")
	if fe, ok := err.(*km2.FolderError); !ok || fe.Code != "km2_folder_names" {
		t.Errorf("names off = %v", err)
	}
	_, err = km2.ResolveFolder(ctx, c, fakeLister{err: &km2.Error{Status: 500, Message: "down"}}, "Atlas")
	if km2.StatusOf(err) != 500 {
		t.Errorf("down = %v", err)
	}
	_, err = km2.ResolveFolder(ctx, conn("https://km2.example", nil), lister, "")
	if fe, ok := err.(*km2.FolderError); !ok || fe.Status != http.StatusConflict {
		t.Errorf("saving off = %v", err)
	}
	_, err = km2.ResolveFolder(ctx, conn("https://km2.example", map[string]string{model.ConfigKM2WriteFolders: km2test.FolderAtlas}), lister, "")
	if fe, ok := err.(*km2.FolderError); !ok || fe.Code != "km2_no_folder" || !strings.Contains(fe.Message, km2test.FolderAtlas) {
		t.Errorf("no default = %v", err)
	}
}

// An agent-chat turn's KM2 connection is loaded without the crew checks, but must still be an enabled KM2 one.
func TestServiceForAgentChat(t *testing.T) {
	e := newSvcEnv(t, nil)
	ctx := context.Background()
	conn, client, err := e.svc.ForAgentChat(ctx, e.conn.ID)
	if err != nil || conn.ID != e.conn.ID || client == nil {
		t.Fatalf("agent chat: %v", err)
	}
	if _, _, err := e.svc.ForAgentChat(ctx, "99999999-0000-4000-8000-000000000000"); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("missing: %v", err)
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE connections SET enabled = false WHERE id = $1`, e.conn.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.ForAgentChat(ctx, e.conn.ID); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled: %v", err)
	}
}
