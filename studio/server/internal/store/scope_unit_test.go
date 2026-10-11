package store

import (
	"slices"
	"testing"

	"crewstudio/internal/model"
)

func TestScopeAreaArgs(t *testing.T) {
	all, ids := Scope{}.areaArgs()
	if all || ids == nil || len(ids) != 0 {
		t.Errorf("zero scope = %v %#v", all, ids)
	}
	all, ids = AllAreas.areaArgs()
	if !all || ids == nil {
		t.Errorf("AllAreas = %v %#v", all, ids)
	}
	_, ids = Scope{AreaIDs: []string{"a"}}.areaArgs()
	if len(ids) != 1 {
		t.Error("ids lost")
	}
}

func TestAreaScopingOfLibraryItems(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	baseAgents := len(must(f.s.ListAgents(bg(), AllAreas)))
	baseTasks := len(must(f.s.ListTasks(bg(), AllAreas)))
	baseCrews := len(must(f.s.ListCrews(bg(), AllAreas)))

	agentA, agentB, agentNone := f.agent("agent-a", nil, a), f.agent("agent-b", nil, b), f.agent("agent-none", nil)
	agentAB := f.agent("agent-ab", nil, a, b)
	taskA, taskB, taskNone := f.task("task-a", nil, a), f.task("task-b", nil, b), f.task("task-none", nil)
	crewA := f.crew("crew-a", []string{agentA.ID}, []string{taskA.ID}, a)
	crewB := f.crew("crew-b", []string{agentB.ID}, []string{taskB.ID}, b)

	idOf := func(x model.Agent) string { return x.ID }
	ctx := bg()

	agents := func(s Scope) []string { return sortedIDs(must(f.s.ListAgents(ctx, s)), idOf) }
	if got := agents(Scope{AreaIDs: []string{a}}); !sameSet(got, []string{agentA.ID, agentAB.ID}) {
		t.Errorf("area A agents = %v", got)
	}
	if got := agents(Scope{AreaIDs: []string{b}}); !sameSet(got, []string{agentB.ID, agentAB.ID}) {
		t.Errorf("area B agents = %v", got)
	}
	if got := agents(Scope{AreaIDs: []string{a, b}}); len(got) != 3 {
		t.Errorf("A+B agents = %v", got)
	}
	if got := agents(Scope{}); len(got) != 0 {
		t.Errorf("empty scope must see nothing, got %v", got)
	}
	allAgents := agents(AllAreas)
	if len(allAgents) != baseAgents+4 {
		t.Errorf("all agents = %d, want %d", len(allAgents), baseAgents+4)
	}
	for _, id := range []string{agentA.ID, agentB.ID, agentNone.ID, agentAB.ID} {
		if !slices.Contains(allAgents, id) {
			t.Errorf("all-scope lacks %s", id)
		}
	}
	// All wins over AreaIDs
	if got := agents(Scope{All: true, AreaIDs: []string{a}}); len(got) != baseAgents+4 {
		t.Errorf("All must override area list, got %d", len(got))
	}

	tasks := sortedIDs(must(f.s.ListTasks(ctx, Scope{AreaIDs: []string{a}})), func(x model.Task) string { return x.ID })
	if !sameSet(tasks, []string{taskA.ID}) {
		t.Errorf("area A tasks = %v", tasks)
	}
	if n := len(must(f.s.ListTasks(ctx, AllAreas))); n != baseTasks+3 {
		t.Errorf("all tasks = %d (task-none %s)", n, taskNone.ID)
	}
	// the 2 seeded crews have no area: invisible to area scopes
	crews := must(f.s.ListCrews(ctx, Scope{AreaIDs: []string{b}}))
	if len(crews) != 1 || crews[0].ID != crewB.ID {
		t.Errorf("area B crews = %+v", crews)
	}
	if n := len(must(f.s.ListCrews(ctx, AllAreas))); n != baseCrews+2 {
		t.Errorf("all crews = %d", n)
	}
	_ = crewA
}

func TestVisibleIDs(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	agentA, agentB, agentNone := f.agent("agent-a", nil, a), f.agent("agent-b", nil, b), f.agent("agent-none", nil)
	taskA, taskB := f.task("task-a", nil, a), f.task("task-b", nil, b)
	crewA := f.simpleCrew("crew-a", a)
	missing := "99999999-0000-4000-8000-000000000000"
	ctx := bg()

	got := must(f.s.VisibleIDs(ctx, "agent", []string{agentA.ID, agentB.ID, agentNone.ID, missing}, Scope{AreaIDs: []string{a}}))
	if len(got) != 1 || !got[agentA.ID] {
		t.Errorf("agents visible in A = %v", got)
	}
	got = must(f.s.VisibleIDs(ctx, "agent", []string{agentA.ID, agentB.ID, agentNone.ID, missing}, AllAreas))
	if len(got) != 3 || got[missing] {
		t.Errorf("all-scope must still drop missing ids: %v", got)
	}
	got = must(f.s.VisibleIDs(ctx, "task", []string{taskA.ID, taskB.ID}, Scope{AreaIDs: []string{b}}))
	if len(got) != 1 || !got[taskB.ID] {
		t.Errorf("tasks visible in B = %v", got)
	}
	got = must(f.s.VisibleIDs(ctx, "crew", []string{crewA.ID}, Scope{AreaIDs: []string{b}}))
	if len(got) != 0 {
		t.Errorf("crew in A visible from B: %v", got)
	}
	got = must(f.s.VisibleIDs(ctx, "crew", []string{crewA.ID}, Scope{AreaIDs: []string{a}}))
	if !got[crewA.ID] {
		t.Error("crew should be visible in own area")
	}
	if got := must(f.s.VisibleIDs(ctx, "agent", nil, AllAreas)); len(got) != 0 {
		t.Errorf("nil ids -> %v", got)
	}
	if _, err := f.s.VisibleIDs(ctx, "llm", []string{agentA.ID}, AllAreas); err == nil {
		t.Error("unknown kind must error")
	}
}

func TestAgentTaskCrewCRUDWithAreasAndRevisions(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	ctx := WithActor(bg(), Actor{ID: f.admin(), Name: "Admin"})

	llm := f.llm("l")
	agent, err := f.s.CreateAgent(ctx, model.Agent{Name: "Ag", Role: "r", Goal: "g", Backstory: "b", LLMID: &llm.ID, MaxIter: 3,
		Tools: []string{"google_search"}, AllowDelegation: true, Verbose: true, AreaIDs: []string{a}})
	mustOK(t, err)
	if !agent.AllowDelegation || !agent.Verbose || len(agent.AreaIDs) != 1 || agent.Tools[0] != "google_search" {
		t.Errorf("agent = %+v", agent)
	}
	// duplicate name conflicts
	_, err = f.s.CreateAgent(ctx, model.Agent{Name: "Ag", Role: "r", Goal: "g", Backstory: "b", MaxIter: 1, Tools: []string{}})
	wantConflict(t, err, "already exists")
	// unknown area => conflict, nothing half-created
	_, err = f.s.CreateAgent(ctx, model.Agent{Name: "Orphan", Role: "r", Goal: "g", Backstory: "b", MaxIter: 1, Tools: []string{},
		AreaIDs: []string{"99999999-0000-4000-8000-000000000000"}})
	wantConflict(t, err, "does not exist")
	for _, ag := range must(f.s.ListAgents(ctx, AllAreas)) {
		if ag.Name == "Orphan" {
			t.Error("failed create must roll back")
		}
	}

	// moving between areas replaces the join rows
	moved, err := f.s.UpdateAgent(ctx, agent.ID, model.Agent{Name: "Ag2", Role: "r", Goal: "g", Backstory: "b", MaxIter: 3, Tools: []string{}, AreaIDs: []string{b}})
	mustOK(t, err)
	if moved.Name != "Ag2" || len(moved.AreaIDs) != 1 || moved.AreaIDs[0] != b || moved.AllowDelegation {
		t.Errorf("moved = %+v", moved)
	}
	if n := len(must(f.s.ListAgents(ctx, Scope{AreaIDs: []string{a}}))); n != 0 {
		t.Errorf("agent still visible in old area: %d", n)
	}
	_, err = f.s.UpdateAgent(ctx, "99999999-0000-4000-8000-000000000000", model.Agent{Name: "x", Role: "r", Goal: "g", Backstory: "b", MaxIter: 1, Tools: []string{}})
	wantNotFound(t, err)
	_, err = f.s.GetAgent(ctx, "99999999-0000-4000-8000-000000000000")
	wantNotFound(t, err)

	// revisions carry the actor and areas, and are scoped
	revs, total, err := f.s.ListRevisions(ctx, RevisionFilter{EntityType: "agent", EntityID: agent.ID, Types: []string{"agent"}, Scope: AllAreas, Limit: 10})
	mustOK(t, err)
	if total != 2 || revs[0].Action != "update" || revs[1].Action != "create" || revs[0].ActorName != "Admin" || revs[0].ActorID == nil {
		t.Errorf("revisions = %+v (total %d)", revs, total)
	}
	_, total, _ = f.s.ListRevisions(ctx, RevisionFilter{EntityType: "agent", EntityID: agent.ID, Types: []string{"agent"}, Scope: Scope{AreaIDs: []string{a}}, Limit: 10})
	// create revision was in area a, update in area b: scope A sees only the create
	if total != 1 {
		t.Errorf("scoped revisions total = %d, want 1", total)
	}
	_, total, _ = f.s.ListRevisions(ctx, RevisionFilter{EntityType: "agent", EntityID: agent.ID, Types: []string{"crew"}, Scope: AllAreas, Limit: 10})
	if total != 0 {
		t.Errorf("type filter ignored: %d", total)
	}
	_, total, _ = f.s.ListRevisions(ctx, RevisionFilter{Scope: AllAreas, Limit: 10})
	if total != 0 {
		t.Errorf("no Types means no revisions, got %d", total)
	}
	page, total, _ := f.s.ListRevisions(ctx, RevisionFilter{EntityType: "agent", Types: []string{"agent"}, Scope: AllAreas, Limit: 1, Offset: 1})
	if len(page) != 1 || total < 2 {
		t.Errorf("paging: %d of %d", len(page), total)
	}

	mustOK(t, f.s.DeleteAgent(ctx, agent.ID))
	wantNotFound(t, f.s.DeleteAgent(ctx, agent.ID))
}

func TestTaskContextOrderAndCrewAreas(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	t1, t2, t3 := f.task("t1", nil), f.task("t2", nil), f.task("t3", nil)
	got, err := f.s.CreateTask(bg(), model.Task{Name: "t4", Description: "d", ExpectedOutput: "e", Markdown: true,
		ContextTaskIDs: []string{t3.ID, t1.ID, t2.ID}, AreaIDs: []string{a}})
	mustOK(t, err)
	if want := []string{t3.ID, t1.ID, t2.ID}; !slicesEqual(got.ContextTaskIDs, want) || !got.Markdown {
		t.Errorf("context order lost: %v", got.ContextTaskIDs)
	}
	upd, err := f.s.UpdateTask(bg(), got.ID, model.Task{Name: "t4", Description: "d", ExpectedOutput: "e", ContextTaskIDs: []string{t2.ID}})
	mustOK(t, err)
	if len(upd.ContextTaskIDs) != 1 || upd.Markdown || len(upd.AreaIDs) != 0 {
		t.Errorf("update = %+v", upd)
	}
	_, err = f.s.UpdateTask(bg(), "99999999-0000-4000-8000-000000000000", model.Task{Name: "x", Description: "d", ExpectedOutput: "e"})
	wantNotFound(t, err)
	_, err = f.s.UpdateTask(bg(), t1.ID, model.Task{Name: "t2", Description: "d", ExpectedOutput: "e"})
	wantConflict(t, err, "already exists")
	// context to a missing task
	_, err = f.s.CreateTask(bg(), model.Task{Name: "bad", Description: "d", ExpectedOutput: "e", ContextTaskIDs: []string{"99999999-0000-4000-8000-000000000000"}})
	wantConflict(t, err, "does not exist")

	crew := f.crew("c", nil, []string{t1.ID}, a)
	if len(crew.AreaIDs) != 1 {
		t.Errorf("crew areas = %v", crew.AreaIDs)
	}
	up, err := f.s.UpdateCrew(bg(), crew.ID, model.Crew{Name: "c2", Process: model.ProcessSequential, BudgetUSD: 3, TaskIDs: []string{t2.ID, t1.ID},
		Inputs: []model.CrewInput{{Key: "k", Label: "K", Default: "d", Required: true}}})
	mustOK(t, err)
	if up.Name != "c2" || len(up.AreaIDs) != 0 || len(up.Inputs) != 1 || !up.Inputs[0].Required || !slicesEqual(up.TaskIDs, []string{t2.ID, t1.ID}) {
		t.Errorf("updated crew = %+v", up)
	}
	_, err = f.s.UpdateCrew(bg(), "99999999-0000-4000-8000-000000000000", model.Crew{Name: "z", Process: model.ProcessSequential, BudgetUSD: 1})
	wantNotFound(t, err)
	_, err = f.s.GetCrew(bg(), "99999999-0000-4000-8000-000000000000")
	wantNotFound(t, err)
	wantNotFound(t, f.s.DeleteCrew(bg(), "99999999-0000-4000-8000-000000000000"))
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInUseChecksIncludeSchedulesAndTriggers(t *testing.T) {
	f := newFixture(t)
	f.admin()
	crew, other := f.simpleCrew("c"), f.simpleCrew("other")

	sched, err := f.s.SaveSchedule(bg(), "", model.Schedule{Name: "sched", CrewID: &crew.ID, Cron: "* * * * *", Timezone: "UTC",
		OverlapPolicy: model.OverlapSkip, Enabled: true}, nil)
	mustOK(t, err)
	trig, err := f.s.SaveTrigger(bg(), "", model.Trigger{Name: "trig", CrewID: &other.ID, Enabled: true})
	mustOK(t, err)

	wantConflict(t, f.s.DeleteCrew(bg(), crew.ID), `schedule "sched"`)
	wantConflict(t, f.s.DeleteCrew(bg(), other.ID), `trigger "trig"`)
	// The crews' tasks are held by the crews, not by the schedule or trigger
	wantConflict(t, f.s.DeleteTask(bg(), crew.TaskIDs[0]), `crew "c"`)

	mustOK(t, f.s.DeleteSchedule(bg(), sched.ID))
	mustOK(t, f.s.DeleteTrigger(bg(), trig.ID))
	mustOK(t, f.s.DeleteCrew(bg(), crew.ID))
	mustOK(t, f.s.DeleteCrew(bg(), other.ID))
}

func TestDeleteCrewKeepsRunsAndWorkOrders(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	f.finish(run.ID, model.RunSucceeded)
	wo := f.workOrder("wo", crew)
	mustOK(t, f.s.DeleteCrew(bg(), crew.ID))
	got := must(f.s.GetRun(bg(), run.ID))
	if got.CrewID != nil || got.CrewName != crew.Name {
		t.Errorf("run should be detached but keep the name: %+v", got)
	}
	gotWO := must(f.s.GetWorkOrder(bg(), wo.ID))
	if gotWO.CrewID != nil || gotWO.TargetName != crew.Name {
		t.Errorf("work order = %+v", gotWO)
	}
}

func TestAreasCRUDAndDeleteRefusesNonEmpty(t *testing.T) {
	f := newFixture(t)
	ctx := bg()
	a := must(f.s.CreateArea(ctx, model.Area{Name: "Ops", Description: "d"}))
	if a.Usage == nil || a.Usage.Crews != 0 {
		t.Errorf("usage = %+v", a.Usage)
	}
	_, err := f.s.CreateArea(ctx, model.Area{Name: "Ops"})
	wantConflict(t, err, "already exists")
	upd, err := f.s.UpdateArea(ctx, a.ID, model.Area{Name: "Operations", Description: "new"})
	mustOK(t, err)
	if upd.Name != "Operations" || upd.Description != "new" {
		t.Errorf("upd = %+v", upd)
	}
	other := must(f.s.CreateArea(ctx, model.Area{Name: "Other"}))
	_, err = f.s.UpdateArea(ctx, a.ID, model.Area{Name: "Other"})
	wantConflict(t, err, "Other")
	_, err = f.s.UpdateArea(ctx, "99999999-0000-4000-8000-000000000000", model.Area{Name: "x"})
	wantNotFound(t, err)
	_, err = f.s.GetArea(ctx, "99999999-0000-4000-8000-000000000000")
	wantNotFound(t, err)
	wantNotFound(t, f.s.DeleteArea(ctx, "99999999-0000-4000-8000-000000000000"))

	// each kind of content blocks deletion on its own
	ag := f.agent("ag", nil, a.ID)
	wantConflict(t, f.s.DeleteArea(ctx, a.ID), "1 agents")
	tk := f.task("tk", nil, a.ID)
	cr := f.crew("cr", nil, nil, a.ID)
	if u := must(f.s.GetArea(ctx, a.ID)).Usage; u.Agents != 1 || u.Tasks != 1 || u.Crews != 1 {
		t.Errorf("usage = %+v", u)
	}
	mustOK(t, f.s.DeleteCrew(ctx, cr.ID))
	wantConflict(t, f.s.DeleteArea(ctx, a.ID), "Operations")
	mustOK(t, f.s.DeleteTask(ctx, tk.ID))
	mustOK(t, f.s.DeleteAgent(ctx, ag.ID))

	// users lose membership instead of blocking
	f.admin()
	u := f.human("p@example.com", model.UserInput{AreaIDs: []string{a.ID}})
	if got := must(f.s.GetArea(ctx, a.ID)).Usage.Users; got != 1 {
		t.Errorf("user usage = %d", got)
	}
	mustOK(t, f.s.DeleteArea(ctx, a.ID))
	if got := must(f.s.GetUser(ctx, u.ID)); len(got.AreaIDs) != 0 {
		t.Errorf("user keeps deleted area: %v", got.AreaIDs)
	}

	list := must(f.s.ListAreas(ctx))
	ids := sortedIDs(list, func(x model.Area) string { return x.ID })
	if !slices.Contains(ids, other.ID) || slices.Contains(ids, a.ID) {
		t.Errorf("areas = %v", ids)
	}
	found := must(f.s.ExistingAreaIDs(ctx, []string{other.ID, a.ID, "99999999-0000-4000-8000-000000000000"}))
	if len(found) != 1 || !found[other.ID] {
		t.Errorf("existing = %v", found)
	}
	if got := must(f.s.ExistingAreaIDs(ctx, nil)); len(got) != 0 {
		t.Errorf("nil ids = %v", got)
	}
}
