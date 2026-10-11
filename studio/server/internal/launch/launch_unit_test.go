package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

const missingID = "99999999-0000-4000-8000-000000000000"

type fakeTools struct {
	tools map[string]model.Tool
	err   error
}

func (f fakeTools) ByKey() (map[string]model.Tool, error) { return f.tools, f.err }

type countingNotifier struct{ n atomic.Int64 }

func (c *countingNotifier) Notify() { c.n.Add(1) }

var schemaSeq atomic.Int64

type env struct {
	t        *testing.T
	st       *store.Store
	l        *Launcher
	notifier *countingNotifier
	tools    *fakeTools
	area     string
	crew     model.Crew
	task     model.Task
	agent    model.Agent
	llm      model.LLM
}

func bg() context.Context { return context.Background() }

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

// newEnv builds a store on a private schema with one runnable crew (agent -> task, one required input) in one area.
func newEnv(t *testing.T) *env {
	t.Helper()
	schema := fmt.Sprintf("launch_unit_%d_%d", os.Getpid(), schemaSeq.Add(1))
	pool := testdb.New(t, schema)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
	st := store.New(pool)
	e := &env{t: t, st: st, notifier: &countingNotifier{}, tools: &fakeTools{tools: map[string]model.Tool{}}}
	e.l = New(st, e.tools, e.notifier)

	a, err := st.CreateArea(bg(), model.Area{Name: "Alpha"})
	ok(t, err)
	e.area = a.ID
	e.llm, err = st.CreateLLM(bg(), model.LLM{Name: "L", Model: "openai/unit-model"})
	ok(t, err)
	_, err = st.CreatePrice(bg(), model.ModelPrice{Model: "unit-model", InputPerMTok: 1, OutputPerMTok: 2})
	ok(t, err)
	e.agent, err = st.CreateAgent(bg(), model.Agent{Name: "Ag", Role: "Analyst of {company}", Goal: "g", Backstory: "b", LLMID: &e.llm.ID,
		MaxIter: 5, Tools: []string{}, AreaIDs: []string{a.ID}})
	ok(t, err)
	e.task, err = st.CreateTask(bg(), model.Task{Name: "Tk", Description: "Research {company}", ExpectedOutput: "memo", AgentID: &e.agent.ID, AreaIDs: []string{a.ID}})
	ok(t, err)
	e.crew, err = st.CreateCrew(bg(), model.Crew{Name: "Cr", Process: model.ProcessSequential, BudgetUSD: 2, AgentIDs: []string{e.agent.ID},
		TaskIDs: []string{e.task.ID}, AreaIDs: []string{a.ID}, Inputs: []model.CrewInput{{Key: "company", Label: "Company", Required: true}}})
	ok(t, err)
	return e
}

type pOpts struct {
	perms    []string
	allAreas bool
	areas    []string
	service  bool
	via      string
}

func (e *env) principal(name string, o pOpts) *auth.Principal {
	e.t.Helper()
	in := model.UserInput{Kind: auth.KindHuman, Email: name + "@example.com", Name: name, Status: model.UserActive,
		AllAreas: o.allAreas, AreaIDs: o.areas, Permissions: o.perms}
	if o.service {
		in = model.UserInput{Kind: auth.KindService, Name: name, Status: model.UserActive, AllAreas: o.allAreas, AreaIDs: o.areas, Permissions: o.perms}
	}
	u, err := e.st.CreateUser(bg(), in)
	ok(e.t, err)
	p, err := e.st.LoadPrincipal(bg(), u.ID)
	ok(e.t, err)
	p.Via = o.via
	if p.Via == "" {
		p.Via = auth.ViaSession
	}
	return p
}

// full is a principal that may do everything in the test area.
func (e *env) full(name string) *auth.Principal {
	return e.principal(name, pOpts{perms: []string{auth.CrewsRun, auth.WorkOrdersCreate}, areas: []string{e.area}})
}

func wantProblems(t *testing.T, err error, fragments ...string) *ProblemsError {
	t.Helper()
	var pe *ProblemsError
	if !errors.As(err, &pe) {
		t.Fatalf("want *ProblemsError, got %T %v", err, err)
	}
	all := strings.Join(pe.Details, "\n")
	for _, f := range fragments {
		if !strings.Contains(all, f) {
			t.Errorf("details %q lack %q", all, f)
		}
	}
	return pe
}

func wantForbidden(t *testing.T, err error, perm string) {
	t.Helper()
	var fe *ForbiddenError
	if !errors.As(err, &fe) || fe.Permission != perm {
		t.Fatalf("want forbidden %s, got %v", perm, err)
	}
}

func TestErrorTypes(t *testing.T) {
	pe := &ProblemsError{Message: "Nope", Details: []string{"a", "b"}}
	if pe.Error() != "Nope: a; b" {
		t.Error(pe.Error())
	}
	if (&ForbiddenError{Permission: "x.y"}).Error() != "missing permission x.y" {
		t.Error("forbidden message")
	}
	if err := require(nil, auth.CrewsRun); err == nil {
		t.Error("nil principal must be denied")
	}
	if err := require(&auth.Principal{Permissions: map[string]bool{"a": true, "b": true}}, "a", "b"); err != nil {
		t.Error(err)
	}
	wantForbidden(t, require(&auth.Principal{Permissions: map[string]bool{"a": true}}, "a", "b"), "b")
	if require(&auth.Principal{}) != nil {
		t.Error("no required permissions means allowed")
	}
}

func TestCallbackFor(t *testing.T) {
	human := &auth.Principal{UserID: "h", Kind: auth.KindHuman}
	svc := &auth.Principal{UserID: "s", Kind: auth.KindService}
	tests := []struct {
		name    string
		p       *auth.Principal
		url     string
		wantNil bool
		problem string
	}{
		{"blank is no callback", human, "   ", true, ""},
		{"empty for service", svc, "", true, ""},
		{"human refused", human, "https://example.com/x", false, "only API keys of service accounts"},
		{"service with good url", svc, "  https://example.com/x ", false, ""},
		{"service with bad scheme", svc, "ftp://example.com", false, "callback_url:"},
		{"service with credentials", svc, "https://u:p@example.com", false, "credentials"},
		{"service with relative", svc, "/x", false, "callback_url:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb, err := callbackFor(tt.p, tt.url)
			if tt.problem != "" {
				wantProblems(t, err, tt.problem)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantNil {
				if cb != nil {
					t.Error("want nil")
				}
				return
			}
			if cb.URL != "https://example.com/x" || cb.SecretSource != store.CallbackFromServiceAccount || cb.SecretOwner != "s" {
				t.Errorf("cb = %+v", cb)
			}
		})
	}
}

func TestSafeFilenameTable(t *testing.T) {
	long := strings.Repeat("a", 300)
	tests := []struct {
		in, want string
	}{
		{"report.pdf", "report.pdf"},
		{"", "file"},
		{".", "file"},
		{"..", "file"},
		{"...", "file"},
		{"/", "_"},
		{"   ", "file"},
		{"../../etc/passwd", "passwd"},
		{`..\..\windows\system32\cmd.exe`, "cmd.exe"},
		{"/abs/path/file.txt", "file.txt"},
		{"dir/", "dir"},
		{"a b c.txt", "a b c.txt"},
		{"  padded.txt  ", "padded.txt"},
		{".hidden", "hidden"},
		{"trailing.", "trailing"},
		{"name\x00.txt", "name_.txt"},
		{"emoji😀.png", "emoji_.png"},
		{"semi;colon&amp.txt", "semi_colon_amp.txt"},
		{"tab\tname", "tab_name"},
		{"naïve résumé.docx", "na_ve r_sum_.docx"},
		{"日本語.txt", "___.txt"},
		{"UPPER_lower-123.TXT", "UPPER_lower-123.TXT"},
		{"$(rm -rf).sh", "__rm -rf_.sh"},
		{"a\nb", "a_b"},
	}
	for _, tt := range tests {
		if got := SafeFilename(tt.in); got != tt.want {
			t.Errorf("SafeFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	// length cap keeps a short extension
	got := SafeFilename(long + ".png")
	if len(got) != 120 || !strings.HasSuffix(got, ".png") {
		t.Errorf("long = %d %q", len(got), got[len(got)-10:])
	}
	// a huge "extension" is cut off with the rest
	got = SafeFilename("a." + strings.Repeat("b", 300))
	if len(got) != 120 {
		t.Errorf("long ext = %d", len(got))
	}
	if got := SafeFilename(long); len(got) != 120 {
		t.Errorf("no ext = %d", len(got))
	}
	// boundary
	if got := SafeFilename(strings.Repeat("x", 120)); len(got) != 120 {
		t.Errorf("exactly 120 changed to %d", len(got))
	}
	// never contains path separators or leading dots
	for _, in := range []string{"a/b", `a\b`, "../x", "..a", "a/..", "/.."} {
		g := SafeFilename(in)
		if strings.ContainsAny(g, `/\`) || strings.HasPrefix(g, ".") || g == "" {
			t.Errorf("unsafe result %q for %q", g, in)
		}
	}
}

func TestUniqueName(t *testing.T) {
	used := map[string]bool{}
	got := []string{
		uniqueName("a.txt", used), uniqueName("A.TXT", used), uniqueName("a.txt", used), uniqueName("noext", used), uniqueName("noext", used),
		uniqueName("a-2.txt", used),
	}
	want := []string{"a.txt", "A-2.TXT", "a-3.txt", "noext", "noext-2", "a-2.txt"}
	// "A-2.TXT" is not "a-2.txt" case-insensitively? it is: so the last call must move on
	want[5] = "a-2-2.txt"
	if !slices.Equal(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestWorkOrderSpecPaths(t *testing.T) {
	spec := workOrderSpec(model.WorkOrder{ID: "w", Number: 3, Title: "T", Brief: "B"}, nil)
	if spec.Attachments == nil || len(spec.Attachments) != 0 {
		t.Error("attachments must be an empty list, not nil")
	}
	atts := []model.Attachment{
		{ID: "1", Filename: "../../evil.sh", ContentType: "text/x", SizeBytes: 5},
		{ID: "2", Filename: "evil.sh"},
		{ID: "3", Filename: ""},
		{ID: "4", Filename: "file"},
	}
	spec = workOrderSpec(model.WorkOrder{ID: "w"}, atts)
	var paths []string
	for _, a := range spec.Attachments {
		paths = append(paths, a.Path)
		if !strings.HasPrefix(a.Path, AttachmentsDir+"/") || strings.Contains(strings.TrimPrefix(a.Path, AttachmentsDir+"/"), "/") || strings.Contains(a.Path, "..") {
			t.Errorf("path escapes the attachments dir: %q", a.Path)
		}
	}
	if !slices.Equal(paths, []string{"attachments/evil.sh", "attachments/evil-2.sh", "attachments/file", "attachments/file-2"}) {
		t.Errorf("paths = %v", paths)
	}
	if spec.Attachments[0].ContentType != "text/x" || spec.Attachments[0].SizeBytes != 5 || spec.Attachments[0].Filename != "../../evil.sh" {
		t.Errorf("metadata lost: %+v", spec.Attachments[0])
	}
}

func TestFreezeBudgets(t *testing.T) {
	crew := model.Crew{ID: "c", Name: "n", Process: model.ProcessSequential}
	// An empty crew has its own problems; budget problems are reported alongside them
	tests := []struct {
		name     string
		budget   *float64
		fallback float64
		problem  bool
		want     float64
	}{
		{"fallback used", nil, 2, false, 2},
		{"explicit wins", ptr(5.0), 2, false, 5},
		{"explicit zero", ptr(0.0), 2, true, 0},
		{"negative", ptr(-1.0), 2, true, 0},
		{"at max", ptr(1000.0), 2, false, 1000},
		{"over max", ptr(1000.01), 2, true, 0},
		{"zero fallback", nil, 0, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// make the crew otherwise valid via an in-memory library
			lib, c := minimalLibrary(crew)
			spec, err := freeze(c, lib, nil, tt.budget, tt.fallback, false)
			if tt.problem {
				wantProblems(t, err, "budget_usd:")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if spec.BudgetUSD != tt.want {
				t.Errorf("budget = %v", spec.BudgetUSD)
			}
		})
	}
	// problems from the crew and from the budget come together
	_, err := freeze(crew, emptyLib(), map[string]string{"x": "y"}, ptr(0.0), 1, false)
	pe := wantProblems(t, err, "budget_usd:", "no tasks", "inputs.x")
	if pe.Message == "" {
		t.Error("message")
	}
}

func TestLibraryLoadsEverything(t *testing.T) {
	e := newEnv(t)
	e.tools.tools = map[string]model.Tool{"google_search": {Key: "google_search", Available: true}}
	lib, err := e.l.Library(bg())
	ok(t, err)
	if _, found := lib.Agents[e.agent.ID]; !found {
		t.Error("agent missing")
	}
	if _, found := lib.Tasks[e.task.ID]; !found {
		t.Error("task missing")
	}
	if _, found := lib.LLMs[e.llm.ID]; !found {
		t.Error("llm missing")
	}
	if len(lib.Pricing) == 0 || lib.Settings.MaxConcurrentRuns == 0 || len(lib.Tools) != 1 {
		t.Errorf("pricing/settings/tools not loaded: %d %+v %d", len(lib.Pricing), lib.Settings, len(lib.Tools))
	}
	crewFound := false
	for _, c := range lib.Crews {
		crewFound = crewFound || c.ID == e.crew.ID
	}
	if !crewFound {
		t.Error("crew missing")
	}
	// library ignores area scope: it is the service's view
	e.tools.err = errors.New("tool registry down")
	if _, err := e.l.Library(bg()); err == nil || !strings.Contains(err.Error(), "registry down") {
		t.Errorf("tool errors must propagate: %v", err)
	}
	ctx, cancel := context.WithCancel(bg())
	cancel()
	if _, err := e.l.Library(ctx); err == nil {
		t.Error("canceled context must fail")
	}
}

func TestStartCrewRunPermissionsAndAreas(t *testing.T) {
	e := newEnv(t)
	req := CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}}

	noPerm := e.principal("noperm", pOpts{areas: []string{e.area}})
	_, _, err := e.l.StartCrewRun(bg(), noPerm, req)
	wantForbidden(t, err, auth.CrewsRun)

	_, _, err = e.l.StartCrewRun(bg(), nil, req)
	wantForbidden(t, err, auth.CrewsRun)

	other, err := e.st.CreateArea(bg(), model.Area{Name: "Beta"})
	ok(t, err)
	outsider := e.principal("outsider", pOpts{perms: []string{auth.CrewsRun}, areas: []string{other.ID}})
	_, _, err = e.l.StartCrewRun(bg(), outsider, req)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("out-of-area crew must look missing, got %v", err)
	}
	noAreas := e.principal("noareas", pOpts{perms: []string{auth.CrewsRun}})
	if _, _, err = e.l.StartCrewRun(bg(), noAreas, req); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no areas: %v", err)
	}
	if _, _, err = e.l.StartCrewRun(bg(), e.full("insider"), CrewRunRequest{CrewID: missingID}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing crew: %v", err)
	}
	admin := e.principal("admin", pOpts{perms: []string{auth.CrewsRun}, allAreas: true})
	if _, _, err = e.l.StartCrewRun(bg(), admin, req); err != nil {
		t.Errorf("all-areas principal: %v", err)
	}
	if e.notifier.n.Load() != 1 {
		t.Errorf("notified %d times", e.notifier.n.Load())
	}
}

func TestStartCrewRunQueuesFrozenRun(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	run, existing, err := e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "  Acme  "}, TriggerType: "api"})
	ok(t, err)
	if existing || run.Status != model.RunQueued || run.TriggerType != "api" || run.CreatedBy == nil || *run.CreatedBy != p.UserID ||
		run.CreatedByName != "pat" || run.BudgetUSD != 2 || run.Inputs["company"] != "Acme" || !slices.Equal(run.AreaIDs, []string{e.area}) {
		t.Errorf("run = %+v", run)
	}
	spec, err := e.st.GetRunSpec(bg(), run.ID)
	ok(t, err)
	if len(spec.Agents) != 1 || len(spec.Tasks) != 1 || len(spec.LLMs) != 1 || len(spec.Pricing) == 0 || spec.Crew.ID != e.crew.ID {
		t.Errorf("spec not frozen with everything: %d agents %d tasks %d llms", len(spec.Agents), len(spec.Tasks), len(spec.LLMs))
	}
	if spec.WorkOrder != nil {
		t.Error("a direct run has no work order")
	}
	if e.notifier.n.Load() != 1 {
		t.Errorf("notifications = %d", e.notifier.n.Load())
	}
	// default trigger is manual
	run2, _, err := e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "B"}, BudgetUSD: ptr(7.5)})
	ok(t, err)
	if run2.TriggerType != "manual" || run2.BudgetUSD != 7.5 {
		t.Errorf("run2 = %+v", run2)
	}
}

func TestStartCrewRunProblems(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	tests := []struct {
		name string
		req  CrewRunRequest
		want []string
	}{
		{"missing required input", CrewRunRequest{CrewID: e.crew.ID}, []string{"inputs.company: is required"}},
		{"blank required input", CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "   "}}, []string{"inputs.company: is required"}},
		{"unknown input", CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "x", "bogus": "y"}}, []string{"inputs.bogus"}},
		{"oversized input", CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": strings.Repeat("x", 20_001)}}, []string{"at most 20000"}},
		{"zero budget", CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "x"}, BudgetUSD: ptr(0.0)}, []string{"budget_usd:"}},
		{"huge budget", CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "x"}, BudgetUSD: ptr(1001.0)}, []string{"budget_usd:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := e.l.StartCrewRun(bg(), p, tt.req)
			wantProblems(t, err, tt.want...)
		})
	}
	if e.notifier.n.Load() != 0 {
		t.Error("a refused run must not wake the dispatcher")
	}
	// budget at the maximum is allowed
	if _, _, err := e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "x"}, BudgetUSD: ptr(1000.0)}); err != nil {
		t.Errorf("budget 1000: %v", err)
	}

	// a crew that cannot run (agent without an LLM)
	bare, err := e.st.CreateAgent(bg(), model.Agent{Name: "Bare", Role: "r", Goal: "g", Backstory: "b", MaxIter: 3, Tools: []string{}, AreaIDs: []string{e.area}})
	ok(t, err)
	task, err := e.st.CreateTask(bg(), model.Task{Name: "BareTask", Description: "d", ExpectedOutput: "e", AgentID: &bare.ID, AreaIDs: []string{e.area}})
	ok(t, err)
	broken, err := e.st.CreateCrew(bg(), model.Crew{Name: "Broken", Process: model.ProcessSequential, BudgetUSD: 1, AgentIDs: []string{bare.ID}, TaskIDs: []string{task.ID}, AreaIDs: []string{e.area}})
	ok(t, err)
	_, _, err = e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: broken.ID})
	wantProblems(t, err, "has no LLM")
	// nothing was created
	runs, total, _ := e.st.ListRuns(bg(), store.RunFilter{Scope: store.AllAreas, Limit: 50})
	for _, r := range runs {
		if r.CrewID != nil && *r.CrewID == broken.ID {
			t.Error("run created for a broken crew")
		}
	}
	_ = total
}

func TestStartCrewRunIdempotency(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	q := e.full("quinn")
	req := CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}, IdempotencyKey: "k1"}

	first, existing, err := e.l.StartCrewRun(bg(), p, req)
	ok(t, err)
	if existing {
		t.Fatal("first call is not a replay")
	}
	again, existing, err := e.l.StartCrewRun(bg(), p, req)
	ok(t, err)
	if !existing || again.ID != first.ID {
		t.Errorf("replay = %v %v", again.ID, existing)
	}
	if e.notifier.n.Load() != 1 {
		t.Errorf("replay must not notify again: %d", e.notifier.n.Load())
	}
	// the same key from someone else is separate
	other, existing, err := e.l.StartCrewRun(bg(), q, req)
	ok(t, err)
	if existing || other.ID == first.ID {
		t.Error("keys must be scoped per principal")
	}

	// the same key for a different crew is refused
	crew2, err := e.st.CreateCrew(bg(), model.Crew{Name: "Cr2", Process: model.ProcessSequential, BudgetUSD: 2, AgentIDs: []string{e.agent.ID},
		TaskIDs: []string{e.task.ID}, AreaIDs: []string{e.area}, Inputs: []model.CrewInput{{Key: "company", Required: true}}})
	ok(t, err)
	_, _, err = e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: crew2.ID, Inputs: map[string]string{"company": "Acme"}, IdempotencyKey: "k1"})
	wantProblems(t, err, "Idempotency-Key")
	if !errors.Is(err, errKeyReused) {
		var pe *ProblemsError
		if !errors.As(err, &pe) || pe.Message != errKeyReused.Message {
			t.Errorf("not errKeyReused: %v", err)
		}
	}
	// the key used by a work order's run cannot be reused for a direct run
	wo := e.draftWorkOrder(p)
	_, _, err = e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{IdempotencyKey: "wo-key"})
	ok(t, err)
	_, _, err = e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}, IdempotencyKey: "wo-key"})
	wantProblems(t, err, "Idempotency-Key")
	// no key: never replayed
	a, _, _ := e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}})
	b, _, _ := e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}})
	if a.ID == b.ID {
		t.Error("runs without a key must be independent")
	}
}

func TestStartCrewRunCallbacks(t *testing.T) {
	e := newEnv(t)
	human := e.full("pat")
	svc := e.principal("bot", pOpts{perms: []string{auth.CrewsRun, auth.WorkOrdersCreate}, areas: []string{e.area}, service: true, via: auth.ViaAPIKey})
	req := CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}, CallbackURL: "https://example.com/hook"}

	_, _, err := e.l.StartCrewRun(bg(), human, req)
	wantProblems(t, err, "service accounts")
	bad := req
	bad.CallbackURL = "gopher://x"
	_, _, err = e.l.StartCrewRun(bg(), svc, bad)
	wantProblems(t, err, "callback_url:")

	run, _, err := e.l.StartCrewRun(bg(), svc, req)
	ok(t, err)
	ds, err := e.st.ListDeliveries(bg(), run.ID)
	ok(t, err)
	if len(ds) != 1 || ds[0].URL != "https://example.com/hook" || ds[0].SecretSource != store.CallbackFromServiceAccount || ds[0].SecretOwnerID != svc.UserID {
		t.Errorf("deliveries = %+v", ds)
	}
}

func TestResolveTarget(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	beta, _ := e.st.CreateArea(bg(), model.Area{Name: "Beta"})
	outsider := e.principal("out", pOpts{areas: []string{beta.ID}})

	tgt, err := e.l.ResolveTarget(bg(), p, &e.crew.ID)
	ok(t, err)
	if tgt.Name != "Cr" || !slices.Equal(tgt.AreaIDs, e.crew.AreaIDs) {
		t.Errorf("crew target = %+v", tgt)
	}

	tests := []struct {
		name string
		p    *auth.Principal
		crew *string
		want string
	}{
		{"no crew", p, nil, "crew_id: pick a crew"},
		{"unknown crew", p, ptr(missingID), "crew_id: no such crew"},
		{"a task id is no crew", p, &e.task.ID, "crew_id: no such crew"},
		{"crew out of area looks missing", outsider, &e.crew.ID, "crew_id: no such crew"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.l.ResolveTarget(bg(), tt.p, tt.crew)
			wantProblems(t, err, tt.want)
			if errors.Is(err, store.ErrNotFound) {
				t.Error("must not leak ErrNotFound for targets")
			}
		})
	}
	// other failures are not disguised as validation problems
	ctx, cancel := context.WithCancel(bg())
	cancel()
	_, err = e.l.ResolveTarget(ctx, p, &e.crew.ID)
	var pe *ProblemsError
	if err == nil || errors.As(err, &pe) {
		t.Errorf("infrastructure error must pass through, got %v", err)
	}
}

func (e *env) draftWorkOrder(p *auth.Principal) model.WorkOrder {
	e.t.Helper()
	c, err := e.l.CreateWorkOrder(bg(), p, model.WorkOrderInput{Title: "WO", Brief: "brief", CrewID: &e.crew.ID,
		Inputs: map[string]string{"company": "Acme"}}, WorkOrderSource{}, Idempotency{}, nil)
	ok(e.t, err)
	return c.WorkOrder
}

func TestRunAs(t *testing.T) {
	e := newEnv(t)
	_, err := e.l.RunAs(bg(), nil)
	wantProblems(t, err, "no longer exists")

	_, err = e.l.RunAs(bg(), ptr(missingID))
	wantProblems(t, err, "disabled or deleted")

	p := e.full("pat")
	got, err := e.l.RunAs(bg(), &p.UserID)
	ok(t, err)
	if got.Via != auth.ViaSystem || got.UserID != p.UserID || !got.Can(auth.WorkOrdersCreate) || !got.SeesAreas([]string{e.area}) {
		t.Errorf("principal = %+v", got)
	}
	// disabled users cannot be acted for (someone else must keep Manage users)
	e.principal("root", pOpts{perms: []string{auth.AdminUsers}})
	_, err = e.st.UpdateUser(bg(), p.UserID, model.UserInput{Kind: auth.KindHuman, Email: "pat@example.com", Name: "pat", Status: model.UserDisabled})
	ok(t, err)
	_, err = e.l.RunAs(bg(), &p.UserID)
	wantProblems(t, err, "disabled or deleted")

	// infrastructure errors are not reported as "nobody to run as"
	ctx, cancel := context.WithCancel(bg())
	cancel()
	q := e.full("quinn")
	_, err = e.l.RunAs(ctx, &q.UserID)
	var pe *ProblemsError
	if err == nil || errors.As(err, &pe) {
		t.Errorf("want raw error, got %v", err)
	}
}
