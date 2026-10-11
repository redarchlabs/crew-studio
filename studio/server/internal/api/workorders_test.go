package api

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// A 1x1 PNG.
var tinyPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 0x49, 0x48, 0x44, 0x52, 0, 0, 0, 1, 0, 0, 0, 1,
	8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0, 1, 0, 0, 5, 0, 1, 0x0d,
	0x0a, 0x2d, 0xb4, 0, 0, 0, 0, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}

func (e testEnv) taskByName(t *testing.T, name string) model.Task {
	t.Helper()
	for _, task := range decodeData[[]model.Task](t, e.do(t, "GET", "/api/tasks", nil)) {
		if task.Name == name {
			return task
		}
	}
	t.Fatalf("task %q not found", name)
	return model.Task{}
}

func (e testEnv) upload(t *testing.T, workOrderID string, files map[string][]byte, headers ...string) response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, data := range files {
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write(data)
	}
	_ = mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/work-orders/"+workOrderID+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "crewstudio")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	return e.send(t, req)
}

// oneTaskCrew saves the crew drafted for a task, the way the task page's Make a crew button does.
func (e testEnv) oneTaskCrew(t *testing.T, taskName string) model.Crew {
	t.Helper()
	r := e.do(t, "GET", "/api/tasks/"+e.taskByName(t, taskName).ID+"/crew-draft", nil)
	wantStatus(t, r, http.StatusOK)
	r = e.do(t, "POST", "/api/crews", decodeData[model.Crew](t, r))
	wantStatus(t, r, http.StatusCreated)
	return decodeData[model.Crew](t, r)
}

func TestWorkOrderLifecycleForAOneTaskCrew(t *testing.T) {
	e := newEnv(t)
	memo := e.oneTaskCrew(t, "Investment Memo") // takes context from the four research tasks

	// A task is not a target any more; the error says how to run one
	r := e.do(t, "POST", "/api/work-orders", map[string]any{"title": "t", "target_type": "task",
		"task_id": e.taskByName(t, "Investment Memo").ID})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "make a crew with just that task") {
		t.Errorf("task target error = %+v", r.body.Error)
	}

	r = e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "Explain {this}", Brief: "Use the {pasted} screenshot.",
		CrewID: &memo.ID, Inputs: map[string]string{"company": "Acme", "location": "Ohio"}})
	wantStatus(t, r, http.StatusCreated)
	wo := decodeData[model.WorkOrder](t, r)
	if wo.Status != model.WorkOrderDraft || wo.Number == 0 || wo.TargetName != "Investment Memo" || len(wo.AreaIDs) != 1 || wo.AreaIDs[0] != financeArea {
		t.Fatalf("draft = %+v", wo)
	}

	r = e.upload(t, wo.ID, map[string][]byte{"pasted-20261003.png": tinyPNG, "notes.md": []byte("# Notes\nhello")})
	wantStatus(t, r, http.StatusCreated)
	files := decodeData[[]model.Attachment](t, r)
	if len(files) != 2 || e.files.Len() != 2 {
		t.Fatalf("files = %+v, stored %d", files, e.files.Len())
	}
	wantStatus(t, e.upload(t, wo.ID, map[string][]byte{"evil.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`)}),
		http.StatusUnsupportedMediaType)
	wantStatus(t, e.upload(t, wo.ID, map[string][]byte{"tool.exe": {0x4d, 0x5a, 0x90, 0, 3, 0, 0, 0}}), http.StatusUnsupportedMediaType)
	// A multipart post without the custom header is refused (cross-site form protection)
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/work-orders/"+wo.ID+"/attachments", strings.NewReader("--x--"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	wantStatus(t, e.send(t, req), http.StatusUnsupportedMediaType)

	// Images are served inline with a sandboxing CSP; text downloads as plain text
	var png, md model.Attachment
	for _, f := range files {
		if f.ContentType == "image/png" {
			png = f
		} else {
			md = f
		}
	}
	res := e.raw(t, "/api/attachments/"+png.ID+"/content")
	if res.Header.Get("Content-Disposition")[:6] != "inline" || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("png headers = %v", res.Header)
	}
	res = e.raw(t, "/api/attachments/"+md.ID+"/content")
	if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") || res.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("md headers = %v", res.Header)
	}

	r = e.do(t, "POST", "/api/work-orders/"+wo.ID+"/submit", map[string]any{"budget_usd": 0.5})
	wantStatus(t, r, http.StatusCreated)
	run := decodeData[model.Run](t, r)
	if run.WorkOrderID == nil || *run.WorkOrderID != wo.ID || run.TriggerType != "work_order" || run.CrewName != "Investment Memo" {
		t.Fatalf("run = %+v", run)
	}
	detail := decodeData[model.Run](t, e.do(t, "GET", "/api/runs/"+run.ID, nil))
	spec := detail.Spec
	if len(spec.Tasks) != 5 || spec.Tasks[4].Name != "Investment Memo" || len(spec.Agents) != 5 || spec.WorkOrder == nil ||
		len(spec.WorkOrder.Attachments) != 2 || spec.Inputs["company"] != "Acme" {
		t.Fatalf("spec = %+v", spec)
	}
	if !strings.HasPrefix(spec.WorkOrder.Attachments[0].Path, "attachments/") {
		t.Errorf("attachment path = %q", spec.WorkOrder.Attachments[0].Path)
	}
	wantStatus(t, e.do(t, "POST", "/api/work-orders/"+wo.ID+"/submit", map[string]any{}), http.StatusConflict)
	wantStatus(t, e.do(t, "PUT", "/api/work-orders/"+wo.ID, model.WorkOrderInput{Title: "x", CrewID: &memo.ID}),
		http.StatusConflict)

	got := decodeData[model.WorkOrder](t, e.do(t, "GET", "/api/work-orders/"+wo.ID, nil))
	if got.Status != "queued" || got.RunCount != 1 || got.Attachments == nil || len(*got.Attachments) != 2 {
		t.Errorf("detail = %+v", got)
	}

	// The journal follows the events
	ctx := context.Background()
	now := time.Now()
	_ = e.st.AppendEvent(ctx, run.ID, model.RunEvent{Seq: 1, Type: "task_started", CreatedAt: now,
		Payload: map[string]any{"task_name": "Corporate Profile", "agent_role": "Corporate Investigator"}})
	_ = e.st.AppendEvent(ctx, run.ID, model.RunEvent{Seq: 2, Type: "delegation", CreatedAt: now,
		Payload: map[string]any{"from_agent": "Corporate Investigator", "to_agent": "Writer", "question": "draft it"}})
	j := decodeData[struct {
		Entries []map[string]any `json:"entries"`
		Tasks   []map[string]any `json:"tasks"`
	}](t, e.do(t, "GET", "/api/work-orders/"+wo.ID+"/journal", nil))
	if len(j.Entries) != 2 || j.Entries[1]["title"] != "Delegated to Writer" || j.Tasks[0]["status"] != "running" {
		t.Errorf("journal = %+v", j)
	}

	cancelled := decodeData[model.Run](t, e.do(t, "POST", "/api/work-orders/"+wo.ID+"/cancel", map[string]any{}))
	if cancelled.Status != model.RunCancelled {
		t.Errorf("cancel = %+v", cancelled)
	}
	list := e.do(t, "GET", "/api/work-orders?q=WO-"+strconv.FormatInt(wo.Number, 10), nil)
	if list.body.Meta.Total != 1 {
		t.Errorf("search by number = %+v", list.body.Meta)
	}
	wantStatus(t, e.do(t, "DELETE", "/api/work-orders/"+wo.ID+"/attachments/"+md.ID, nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "DELETE", "/api/work-orders/"+wo.ID, nil), http.StatusNoContent)
	pending, _ := e.st.PendingStorageDeletions(ctx, 10)
	if len(pending) != 2 {
		t.Errorf("deleting a work order should queue its stored files for deletion, got %v", pending)
	}
}

func (e testEnv) raw(t *testing.T, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", path, res.StatusCode)
	}
	return res
}

func TestWorkOrderSubmitProblemsKeepTheDraft(t *testing.T) {
	e := newEnv(t)
	empty := decodeData[model.Crew](t, e.do(t, "POST", "/api/crews", map[string]any{"name": "Empty", "process": "sequential",
		"budget_usd": 1, "area_ids": []string{financeArea}}))
	r := e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "Nothing to do", CrewID: &empty.ID, Submit: true})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(r.body.Error.Message, "saved as a draft") || !strings.Contains(strings.Join(r.body.Error.Details, " "), "no tasks") {
		t.Errorf("error = %+v", r.body.Error)
	}
	pe := e.crewByName(t, "PE Due Diligence Crew")
	r = e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "Diligence", CrewID: &pe.ID, Submit: true,
		Inputs: map[string]string{"company": "Acme"}}, "Idempotency-Key", "abc")
	wantStatus(t, r, http.StatusCreated)
	first := decodeData[model.WorkOrder](t, r)
	again := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "Diligence",
		CrewID: &pe.ID, Submit: true, Inputs: map[string]string{"company": "Acme"}}, "Idempotency-Key", "abc"))
	if first.ID != again.ID || first.Status != "queued" {
		t.Errorf("idempotent create: %s vs %s (%s)", first.ID, again.ID, first.Status)
	}

	_, viewer := e.person(t, "v@example.com", []string{viewerRole}, []string{itArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "GET", "/api/work-orders/"+first.ID, nil), http.StatusNotFound)
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "x", CrewID: &pe.ID}),
		http.StatusForbidden)
}

func TestSchedules(t *testing.T) {
	e := newEnv(t)
	preview := e.do(t, "POST", "/api/schedules/preview", map[string]string{"cron": "0 9 * * 1-5", "timezone": "Europe/London"})
	wantStatus(t, preview, http.StatusOK)
	if next := decodeData[map[string][]string](t, preview)["next"]; len(next) != 5 {
		t.Errorf("next = %v", next)
	}
	wantStatus(t, e.do(t, "POST", "/api/schedules/preview", map[string]string{"cron": "* * * * *"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/schedules/preview", map[string]string{"cron": "0 9 * * *", "timezone": "Mars/Base"}),
		http.StatusUnprocessableEntity)

	pe := e.crewByName(t, "PE Due Diligence Crew")
	r := e.do(t, "POST", "/api/schedules", model.Schedule{Name: "Daily diligence", CrewID: &pe.ID, Cron: "0 9 * * *",
		Timezone: "America/New_York", Enabled: true, Inputs: map[string]string{"company": "Acme"}, Brief: "Refresh."})
	wantStatus(t, r, http.StatusCreated)
	sc := decodeData[model.Schedule](t, r)
	if sc.NextRunAt == nil || sc.RunAsName != "Ada Admin" || sc.TargetName != pe.Name || len(sc.AreaIDs) != 1 {
		t.Fatalf("schedule = %+v", sc)
	}

	wo := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/schedules/"+sc.ID+"/run-now", map[string]any{}))
	if wo.Source != "schedule" || wo.SourceID == nil || *wo.SourceID != sc.ID || wo.Status != "queued" {
		t.Errorf("run now = %+v", wo)
	}

	// Make it due and let the scheduler fire it; overlap "skip" holds it back while the first is still queued
	ctx := context.Background()
	if _, err := e.st.Pool().Exec(ctx, `UPDATE schedules SET next_run_at = now() - interval '1 minute' WHERE id = $1`, sc.ID); err != nil {
		t.Fatal(err)
	}
	e.sched.Tick(ctx)
	got := decodeData[model.Schedule](t, e.do(t, "GET", "/api/schedules/"+sc.ID, nil))
	if got.LastError == nil || !strings.Contains(*got.LastError, "Skipped") || !got.NextRunAt.After(time.Now()) {
		t.Errorf("after overlapping tick = %+v", got)
	}
	_, _ = e.runs.Cancel(ctx, *wo.CurrentRunID)
	if _, err := e.st.Pool().Exec(ctx, `UPDATE schedules SET next_run_at = now() - interval '1 minute' WHERE id = $1`, sc.ID); err != nil {
		t.Fatal(err)
	}
	e.sched.Tick(ctx)
	list := e.do(t, "GET", "/api/work-orders?source=schedule&source_id="+sc.ID, nil)
	if list.body.Meta.Total != 2 {
		t.Errorf("work orders from schedule = %d", list.body.Meta.Total)
	}

	// When the person it runs as loses access, the schedule pauses itself
	_, builder := e.person(t, "b@example.com", []string{builderRole}, []string{financeArea}, nil)
	sc.Enabled = true
	saved := decodeData[model.Schedule](t, e.as(builder).do(t, "PUT", "/api/schedules/"+sc.ID, sc))
	if saved.RunAsName != "b@example.com" {
		t.Errorf("run as = %q", saved.RunAsName)
	}
	users := decodeData[[]model.User](t, e.do(t, "GET", "/api/admin/users", nil))
	for _, u := range users {
		if u.Email != nil && *u.Email == "b@example.com" {
			wantStatus(t, e.do(t, "PUT", "/api/admin/users/"+u.ID, model.UserInput{Kind: "human", Email: *u.Email, Name: u.Name,
				Status: model.UserDisabled, AreaIDs: u.AreaIDs, RoleIDs: u.RoleIDs}), http.StatusOK)
		}
	}
	if _, err := e.st.Pool().Exec(ctx, `UPDATE schedules SET next_run_at = now() - interval '1 minute' WHERE id = $1`, sc.ID); err != nil {
		t.Fatal(err)
	}
	e.sched.Tick(ctx)
	paused := decodeData[model.Schedule](t, e.do(t, "GET", "/api/schedules/"+sc.ID, nil))
	if paused.Enabled || paused.LastError == nil || !strings.HasPrefix(*paused.LastError, "Paused") {
		t.Errorf("paused = %+v", paused)
	}

	wantStatus(t, e.do(t, "DELETE", "/api/crews/"+pe.ID, nil), http.StatusConflict)
	wantStatus(t, e.do(t, "DELETE", "/api/schedules/"+sc.ID, nil), http.StatusNoContent)
}

func TestTriggers(t *testing.T) {
	e := newEnv(t)
	pe := e.crewByName(t, "PE Due Diligence Crew")
	cb := "https://hooks.example.com/done"
	r := e.do(t, "POST", "/api/triggers", model.Trigger{Name: "CRM deal", CrewID: &pe.ID, Enabled: true,
		InputMap: map[string]string{"company": "deal.company", "location": "deal.sites.0"}, Brief: "New deal.", IncludePayload: true,
		CallbackURL: &cb})
	wantStatus(t, r, http.StatusCreated)
	trig := decodeData[model.Trigger](t, r)
	if !strings.HasPrefix(trig.Token, "cst_") || trig.SigningSecret == "" {
		t.Fatalf("trigger = %+v", trig)
	}
	hook := func(token string, body string, headers ...string) response {
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/hooks/"+trig.ID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		return e.send(t, req)
	}
	wantStatus(t, hook("", `{}`), http.StatusUnauthorized)
	wantStatus(t, hook("cst_aaaaaaaa_"+strings.Repeat("b", 43), `{}`), http.StatusUnauthorized)

	payload := `{"deal": {"company": "Acme Corp", "sites": ["Ohio", "Texas"]}}`
	r = hook(trig.Token, payload, "Idempotency-Key", "deal-42")
	wantStatus(t, r, http.StatusAccepted)
	accepted := decodeData[hookAccepted](t, r)
	if accepted.Status != "queued" || accepted.RunID == "" {
		t.Fatalf("accepted = %+v", accepted)
	}
	again := decodeData[hookAccepted](t, hook(trig.Token, payload, "Idempotency-Key", "deal-42"))
	if again.WorkOrderID != accepted.WorkOrderID {
		t.Error("repeating an Idempotency-Key must not create a second work order")
	}
	run := decodeData[model.Run](t, e.do(t, "GET", "/api/runs/"+accepted.RunID, nil))
	if run.Inputs["company"] != "Acme Corp" || run.Inputs["location"] != "Ohio" || !strings.Contains(run.Spec.WorkOrder.Brief, `"Acme Corp"`) {
		t.Errorf("run inputs %v brief %q", run.Inputs, run.Spec.WorkOrder.Brief)
	}
	deliveries := decodeData[[]store.Delivery](t, e.do(t, "GET", "/api/runs/"+run.ID+"/deliveries", nil))
	if len(deliveries) != 1 || deliveries[0].URL != cb {
		t.Errorf("deliveries = %+v", deliveries)
	}

	// Viewers see the trigger without its signing secret
	_, viewer := e.person(t, "v@example.com", []string{viewerRole}, []string{financeArea}, nil)
	seen := decodeData[model.Trigger](t, e.as(viewer).do(t, "GET", "/api/triggers/"+trig.ID, nil))
	if seen.SigningSecret != "" || seen.Token != "" {
		t.Errorf("viewer sees secrets: %+v", seen)
	}

	rotated := decodeData[model.Trigger](t, e.do(t, "POST", "/api/triggers/"+trig.ID+"/rotate", map[string]any{}))
	wantStatus(t, hook(trig.Token, payload), http.StatusUnauthorized)
	trig.Enabled = false
	trig.Token, trig.SigningSecret = "", ""
	wantStatus(t, e.do(t, "PUT", "/api/triggers/"+trig.ID, trig), http.StatusOK)
	wantStatus(t, hook(rotated.Token, payload), http.StatusConflict)
	wantStatus(t, e.do(t, "DELETE", "/api/triggers/"+trig.ID, nil), http.StatusNoContent)
}

func TestMapInputs(t *testing.T) {
	var payload any = map[string]any{"a": map[string]any{"b": "x", "n": 3.5, "ok": true, "list": []any{"p", "q"}, "obj": map[string]any{"k": 1.0}}}
	got := MapInputs(map[string]string{"s": "a.b", "n": "a.n", "ok": "a.ok", "first": "a.list.1", "obj": "a.obj", "missing": "a.z", "bad": "a.list.9"}, payload)
	want := map[string]string{"s": "x", "n": "3.5", "ok": "true", "first": "q", "obj": `{"k":1}`}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestHardeningGuards(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo := e.crewByName(t, "PE Due Diligence Crew")

	// NUL characters are refused up front instead of failing in Postgres
	r := e.do(t, "POST", "/api/work-orders", map[string]any{"title": "a\u0000b", "crew_id": memo.ID})
	wantStatus(t, r, http.StatusBadRequest)

	wo := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "t",
		CrewID: &memo.ID, Inputs: map[string]string{"company": "Acme"}}))
	files := decodeData[[]model.Attachment](t, e.upload(t, wo.ID, map[string][]byte{"a.png": tinyPNG}))
	wantStatus(t, e.do(t, "POST", "/api/work-orders/"+wo.ID+"/submit", map[string]any{}), http.StatusCreated)
	// While it runs, its inputs cannot change
	wantStatus(t, e.upload(t, wo.ID, map[string][]byte{"b.png": tinyPNG}), http.StatusConflict)
	wantStatus(t, e.do(t, "DELETE", "/api/work-orders/"+wo.ID+"/attachments/"+files[0].ID, nil), http.StatusConflict)

	// A trigger cannot pile up more than a handful of active work orders
	pe := e.crewByName(t, "PE Due Diligence Crew")
	trig := decodeData[model.Trigger](t, e.do(t, "POST", "/api/triggers", model.Trigger{Name: "Flood", CrewID: &pe.ID,
		Enabled: true, InputMap: map[string]string{}}))
	statuses := map[int]int{}
	for i := 0; i < maxActivePerTrigger+2; i++ {
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/hooks/"+trig.ID, strings.NewReader(`{"n": 1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+trig.Token)
		statuses[e.send(t, req).status]++
	}
	if statuses[http.StatusAccepted] != maxActivePerTrigger || statuses[http.StatusTooManyRequests] != 2 {
		t.Errorf("statuses = %v", statuses)
	}

	// Editing a schedule's description keeps its next run time
	sc := decodeData[model.Schedule](t, e.do(t, "POST", "/api/schedules", model.Schedule{Name: "Weekly", CrewID: &pe.ID,
		Cron: "0 9 * * 1", Timezone: "UTC", Enabled: true}))
	if _, err := e.st.Pool().Exec(ctx, `UPDATE schedules SET next_run_at = now() + interval '1 hour' WHERE id = $1`, sc.ID); err != nil {
		t.Fatal(err)
	}
	before := decodeData[model.Schedule](t, e.do(t, "GET", "/api/schedules/"+sc.ID, nil))
	before.Description = "changed"
	after := decodeData[model.Schedule](t, e.do(t, "PUT", "/api/schedules/"+sc.ID, before))
	if !after.NextRunAt.Equal(*before.NextRunAt) {
		t.Errorf("next run moved from %v to %v", before.NextRunAt, after.NextRunAt)
	}
}
