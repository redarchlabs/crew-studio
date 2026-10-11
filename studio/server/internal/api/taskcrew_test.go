package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

func TestTaskCrewDraft(t *testing.T) {
	e := newEnv(t)
	profile := e.taskByName(t, "Corporate Profile")
	path := "/api/tasks/" + profile.ID + "/crew-draft"

	r := e.do(t, "GET", path, nil)
	wantStatus(t, r, http.StatusOK)
	draft := decodeData[model.Crew](t, r)
	if draft.ID != "" || draft.Name != "Corporate Profile" || !slices.Equal(draft.TaskIDs, []string{profile.ID}) ||
		!slices.Equal(draft.AgentIDs, []string{*profile.AgentID}) || draft.Process != model.ProcessSequential ||
		!slices.Equal(draft.AreaIDs, []string{financeArea}) || !strings.Contains(draft.Description, "Corporate Profile") {
		t.Fatalf("draft = %+v", draft)
	}
	// The PE crew lends its input definitions
	pe := e.crewByName(t, "PE Due Diligence Crew")
	for _, in := range draft.Inputs {
		if i := slices.IndexFunc(pe.Inputs, func(x model.CrewInput) bool { return x.Key == in.Key }); i < 0 || pe.Inputs[i] != in {
			t.Errorf("input %+v not borrowed from %+v", in, pe.Inputs)
		}
	}

	// Saved as is, it is a crew that can run; a second draft needs another name
	saved := decodeData[model.Crew](t, e.do(t, "POST", "/api/crews", draft))
	v := decodeData[validation](t, e.do(t, "GET", "/api/crews/"+saved.ID+"/validate", nil))
	if !v.Valid {
		t.Errorf("saved draft cannot run: %v", v.Errors)
	}
	if again := decodeData[model.Crew](t, e.do(t, "GET", path, nil)); again.Name != "Corporate Profile (task)" {
		t.Errorf("second draft name = %q", again.Name)
	}

	// It takes permission to see tasks and to make crews, and the task must be in your areas
	_, viewer := e.person(t, "dv@example.com", nil, []string{financeArea}, []string{auth.TasksView})
	wantStatus(t, e.as(viewer).do(t, "GET", path, nil), http.StatusForbidden)
	_, itMaker := e.person(t, "dit@example.com", nil, []string{itArea}, []string{auth.TasksView, auth.CrewsCreate})
	wantStatus(t, e.as(itMaker).do(t, "GET", path, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/tasks/"+missingID+"/crew-draft", nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "GET", "/api/tasks/nope/crew-draft", nil), http.StatusNotFound)

	// Input definitions come only from crews the caller can see
	ctx := context.Background()
	if _, err := e.st.CreateCrew(ctx, model.Crew{Name: "Aaa HR crew", Process: model.ProcessSequential, BudgetUSD: 1,
		AgentIDs: []string{*profile.AgentID}, TaskIDs: []string{profile.ID}, AreaIDs: []string{hrArea},
		Inputs: []model.CrewInput{{Key: "company", Label: "HR-ONLY LABEL", Default: "secret default"}, {Key: "location", Label: "HR location"}}}); err != nil {
		t.Fatal(err)
	}
	_, financeMaker := e.person(t, "dfin@example.com", nil, []string{financeArea}, []string{auth.TasksView, auth.CrewsCreate})
	for _, in := range decodeData[model.Crew](t, e.as(financeMaker).do(t, "GET", path, nil)).Inputs {
		if strings.Contains(in.Label, "HR") || in.Default == "secret default" {
			t.Errorf("input borrowed from a crew outside the caller's areas: %+v", in)
		}
	}

	// A task that takes context from a task outside your areas cannot become your crew
	research := e.taskByName(t, "AI Research Task")
	mixed, err := e.st.CreateTask(ctx, model.Task{Name: "IT view of the profile", Description: "Summarise for IT.",
		ExpectedOutput: "A summary.", AgentID: research.AgentID, ContextTaskIDs: []string{profile.ID}, AreaIDs: []string{itArea}})
	if err != nil {
		t.Fatal(err)
	}
	r = e.as(itMaker).do(t, "GET", "/api/tasks/"+mixed.ID+"/crew-draft", nil)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "outside your functional areas") {
		t.Errorf("error = %+v", r.body.Error)
	}
	// ... while someone who sees both gets the context task first
	both := decodeData[model.Crew](t, e.do(t, "GET", "/api/tasks/"+mixed.ID+"/crew-draft", nil))
	if !slices.Equal(both.TaskIDs, []string{profile.ID, mixed.ID}) {
		t.Errorf("tasks = %v", both.TaskIDs)
	}
}

func TestUniqueCrewName(t *testing.T) {
	crews := func(names ...string) []model.Crew {
		out := make([]model.Crew, 0, len(names))
		for _, n := range names {
			out = append(out, model.Crew{Name: n})
		}
		return out
	}
	tests := []struct {
		name  string
		crews []model.Crew
		want  string
	}{
		{"free", crews("Other"), "Memo"},
		{"taken", crews("Memo"), "Memo (task)"},
		{"taken twice", crews("Memo", "Memo (task)"), "Memo (task 2)"},
		{"gap", crews("Memo", "Memo (task)", "Memo (task 3)"), "Memo (task 2)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uniqueCrewName("Memo", tt.crews); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	// A long name is cut short before the suffix, so it still fits the 120-character limit
	long := strings.Repeat("é", 120)
	if got := uniqueCrewName(long, crews(long)); got != strings.Repeat("é", 100)+" (task)" || len([]rune(got)) > 120 {
		t.Errorf("long name = %q (%d runes)", got, len([]rune(got)))
	}
	if got := uniqueCrewName(long, crews("Other")); got != long {
		t.Errorf("a free long name was changed")
	}
}
