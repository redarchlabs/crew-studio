package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// askingRun starts (in the database only) a manual work order run created by userID, with escalation esc frozen in.
func (e testEnv) askingRun(t *testing.T, userID string, esc *model.EscalationSpec) stateRun {
	t.Helper()
	ctx := store.WithActor(context.Background(), store.Actor{ID: e.adminID, Name: "Ada Admin"})
	crew := e.crewByName(t, "PE Due Diligence Crew")
	wo, err := e.st.CreateWorkOrder(ctx, model.WorkOrderInput{Title: "pricing", CrewID: &crew.ID},
		store.WorkOrderMeta{TargetName: crew.Name, AreaIDs: crew.AreaIDs, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := e.st.CreateRun(ctx, model.RunSpec{Crew: crew, BudgetUSD: 1, Escalation: esc},
		store.RunMeta{AreaIDs: crew.AreaIDs, WorkOrderID: wo.ID, CreatedBy: userID})
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.st.CreateRunToken(ctx, run.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return stateRun{run: run, crew: crew, token: token}
}

type runQuestion struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Answer    string `json:"answer"`
	OnTimeout string `json:"on_timeout"`
}

var escalationOn = &model.EscalationSpec{Rules: "Ask before assuming a budget.", TimeoutMinutes: 30, OnTimeout: model.EscalationProceed, MaxQuestions: 2}

func TestEscalationSettingsRoundTrip(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	path := "/api/crews/" + crew.ID + "/escalation"
	got := decodeData[model.EscalationSettings](t, e.do(t, "GET", path, nil))
	if got.Enabled || got.TimeoutMinutes != model.DefaultEscalationTimeoutMinutes || got.OnTimeout != model.EscalationProceed {
		t.Errorf("default = %+v", got)
	}
	wantStatus(t, e.do(t, "PUT", path, map[string]any{"enabled": true, "rules": " ", "timeout_minutes": 30, "max_questions": 5}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "PUT", path, map[string]any{"enabled": true, "rules": "x", "timeout_minutes": 999, "max_questions": 5}), http.StatusUnprocessableEntity)
	saved := decodeData[model.EscalationSettings](t, e.do(t, "PUT", path,
		map[string]any{"enabled": true, "rules": "  Ask when sources conflict.  ", "timeout_minutes": 15, "on_timeout": "fail", "max_questions": 3}))
	if !saved.Enabled || saved.Rules != "Ask when sources conflict." || saved.OnTimeout != "fail" || saved.UpdatedByName == "" {
		t.Errorf("saved = %+v", saved)
	}
}

func TestAskAndAnswerAQuestion(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	samID, sam := e.person(t, "sam@example.com", []string{builderRole}, crew.AreaIDs, nil)
	_, viewer := e.person(t, "vic@example.com", []string{viewerRole}, crew.AreaIDs, nil)
	sr := e.askingRun(t, samID, escalationOn)

	asked := e.call(t, sr, "POST", "/questions", map[string]any{"question": "What budget should I assume?", "context": "Brief has none", "task_name": "Research"})
	wantStatus(t, asked, http.StatusCreated)
	q := decodeData[runQuestion](t, asked)
	if q.Status != model.QuestionOpen || q.OnTimeout != model.EscalationProceed {
		t.Fatalf("asked = %+v", q)
	}

	mine := decodeData[[]model.HumanQuestion](t, e.as(sam).do(t, "GET", "/api/questions", nil))
	if len(mine) != 1 || mine[0].Question != "What budget should I assume?" || mine[0].AgentRole != "PMO\tCoordinator" && mine[0].AgentRole != "PMO Coordinator" || mine[0].WorkOrderTitle != "pricing" {
		t.Errorf("sam's questions = %+v", mine)
	}
	if others := decodeData[[]model.HumanQuestion](t, e.as(viewer).do(t, "GET", "/api/questions", nil)); len(others) != 0 {
		t.Errorf("viewer sees %d questions", len(others))
	}

	answerPath := "/api/questions/" + q.ID + "/answer"
	wantStatus(t, e.as(viewer).do(t, "POST", answerPath, map[string]any{"answer": "$5k"}), http.StatusForbidden)
	wantStatus(t, e.as(sam).do(t, "POST", answerPath, map[string]any{"answer": "  "}), http.StatusUnprocessableEntity)
	wantStatus(t, e.as(sam).do(t, "POST", answerPath, map[string]any{"answer": "Assume $5k a month."}), http.StatusOK)
	wantStatus(t, e.as(sam).do(t, "POST", answerPath, map[string]any{"answer": "again"}), http.StatusConflict)

	polled := decodeData[runQuestion](t, e.call(t, sr, "GET", "/questions/"+q.ID, nil))
	if polled.Status != model.QuestionAnswered || polled.Answer != "Assume $5k a month." {
		t.Errorf("polled = %+v", polled)
	}

	listed := decodeData[[]model.HumanQuestion](t, e.do(t, "GET", "/api/work-orders/"+*sr.run.WorkOrderID+"/questions", nil))
	if len(listed) != 1 || listed[0].AnsweredByName != "sam@example.com" {
		t.Errorf("work order questions = %+v", listed)
	}
	events, err := e.st.ListEvents(context.Background(), sr.run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, ev := range events {
		if ev.Payload["tool"] == askHumanTool {
			lines = append(lines, ev.Payload["message"].(string))
		}
	}
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "Asked a human: What budget") || !strings.HasPrefix(lines[1], "sam@example.com answered: Assume") {
		t.Errorf("journal lines = %q", lines)
	}
}

func TestAskingIsRefusedWithoutEscalationOrPastTheLimit(t *testing.T) {
	e := newEnv(t)
	off := e.askingRun(t, e.adminID, nil)
	r := e.call(t, off, "POST", "/questions", map[string]any{"question": "?"})
	wantStatus(t, r, http.StatusForbidden)
	if r.body.Error.Code != "escalation_off" {
		t.Errorf("code = %s", r.body.Error.Code)
	}

	on := e.askingRun(t, e.adminID, escalationOn)
	wantStatus(t, e.call(t, on, "POST", "/questions", map[string]any{"question": ""}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, on, "POST", "/questions", map[string]any{"question": "one"}), http.StatusCreated)
	wantStatus(t, e.call(t, on, "POST", "/questions", map[string]any{"question": "two"}), http.StatusCreated)
	wantStatus(t, e.call(t, on, "POST", "/questions", map[string]any{"question": "three"}), http.StatusTooManyRequests)
}

func TestQuestionsTimeOutAndCloseWithTheirRun(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sr := e.askingRun(t, e.adminID, &model.EscalationSpec{Rules: "x", TimeoutMinutes: 5, OnTimeout: model.EscalationFail, MaxQuestions: 5})
	late := decodeData[runQuestion](t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "late?"}))
	if _, err := e.st.Pool().Exec(ctx, `UPDATE human_questions SET deadline_at = now() - interval '1 second' WHERE id = $1`, late.ID); err != nil {
		t.Fatal(err)
	}
	polled := decodeData[runQuestion](t, e.call(t, sr, "GET", "/questions/"+late.ID, nil))
	if polled.Status != model.QuestionTimedOut || polled.OnTimeout != model.EscalationFail {
		t.Errorf("polled = %+v", polled)
	}
	wantStatus(t, e.do(t, "POST", "/api/questions/"+late.ID+"/answer", map[string]any{"answer": "too late"}), http.StatusConflict)

	open := decodeData[runQuestion](t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "still there?"}))
	if _, err := e.st.Pool().Exec(ctx, `UPDATE runs SET status = 'cancelled' WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	listed := decodeData[[]model.HumanQuestion](t, e.do(t, "GET", "/api/work-orders/"+*sr.run.WorkOrderID+"/questions", nil))
	for _, q := range listed {
		if q.ID == open.ID && q.Status != model.QuestionClosed {
			t.Errorf("question of a cancelled run is %q", q.Status)
		}
	}
	if mine := decodeData[[]model.HumanQuestion](t, e.do(t, "GET", "/api/questions", nil)); len(mine) != 0 {
		t.Errorf("open questions after the run ended = %+v", mine)
	}
}

func TestUnaddressedQuestionsGoToWorkOrderOperators(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	_, viewer := e.person(t, "vic@example.com", []string{viewerRole}, crew.AreaIDs, nil)
	// No creator: a trigger or schedule started the run
	sr := e.askingRun(t, "", escalationOn)
	q := decodeData[runQuestion](t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "Which region?"}))
	if mine := decodeData[[]model.HumanQuestion](t, e.do(t, "GET", "/api/questions", nil)); len(mine) != 1 {
		t.Errorf("admin sees %d unaddressed questions", len(mine))
	}
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/questions/"+q.ID+"/answer", map[string]any{"answer": "EU"}), http.StatusForbidden)
	wantStatus(t, e.do(t, "POST", "/api/questions/"+q.ID+"/answer", map[string]any{"answer": "EU"}), http.StatusOK)
}

func TestAWaitingRunHearsTheAnswerAtOnce(t *testing.T) {
	e := newEnv(t)
	sr := e.askingRun(t, e.adminID, escalationOn)
	q := decodeData[runQuestion](t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "Which region?"}))

	done := make(chan runQuestion, 1)
	started := time.Now()
	go func() {
		done <- decodeData[runQuestion](t, e.call(t, sr, "GET", "/questions/"+q.ID+"?wait=20", nil))
	}()
	time.Sleep(200 * time.Millisecond)
	wantStatus(t, e.do(t, "POST", "/api/questions/"+q.ID+"/answer", map[string]any{"answer": "EU"}), http.StatusOK)
	select {
	case got := <-done:
		if got.Status != model.QuestionAnswered || got.Answer != "EU" {
			t.Errorf("woken with %+v", got)
		}
		if waited := time.Since(started); waited > 5*time.Second {
			t.Errorf("waited %v for the answer", waited)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the waiting request was not woken by the answer")
	}

	// Without wait, an open question answers at once
	open := decodeData[runQuestion](t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "Which currency?"}))
	started = time.Now()
	if got := decodeData[runQuestion](t, e.call(t, sr, "GET", "/questions/"+open.ID, nil)); got.Status != model.QuestionOpen || time.Since(started) > 2*time.Second {
		t.Errorf("no-wait poll = %+v after %v", got, time.Since(started))
	}
}

// questionEvents reads "questions" events from an open question stream.
func questionEvents(t *testing.T, body io.Reader) <-chan []model.HumanQuestion {
	t.Helper()
	out := make(chan []model.HumanQuestion, 8)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		event := ""
		for scanner.Scan() {
			line := scanner.Text()
			if name, found := strings.CutPrefix(line, "event: "); found {
				event = name
			} else if data, found := strings.CutPrefix(line, "data: "); found && event == "questions" {
				var list []model.HumanQuestion
				if err := json.Unmarshal([]byte(data), &list); err == nil {
					out <- list
				}
			}
		}
	}()
	return out
}

func TestQuestionStreamPushesNewAndAnsweredQuestions(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/questions/stream", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	events := questionEvents(t, res.Body)
	next := func() []model.HumanQuestion {
		select {
		case list := <-events:
			return list
		case <-ctx.Done():
			t.Fatal("no questions event")
			return nil
		}
	}
	if first := next(); len(first) != 0 {
		t.Fatalf("first = %+v", first)
	}
	sr := e.askingRun(t, e.adminID, escalationOn)
	q := decodeData[runQuestion](t, e.call(t, sr, "POST", "/questions", map[string]any{"question": "Which region?"}))
	if asked := next(); len(asked) != 1 || asked[0].ID != q.ID {
		t.Fatalf("after asking = %+v", asked)
	}
	wantStatus(t, e.do(t, "POST", "/api/questions/"+q.ID+"/answer", map[string]any{"answer": "EU"}), http.StatusOK)
	if answered := next(); len(answered) != 0 {
		t.Fatalf("after answering = %+v", answered)
	}
}
