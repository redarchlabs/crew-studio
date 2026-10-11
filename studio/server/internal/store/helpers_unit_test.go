package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/testdb"
)

var schemaSeq atomic.Int64

// newUnitStore returns a store on a freshly migrated schema of its own. The name includes the process id so
// concurrent runs of this package (for example a repo-wide `go test ./...`) cannot drop each other's schema.
func newUnitStore(t *testing.T) *Store {
	t.Helper()
	schema := fmt.Sprintf("store_unit_%d_%d", os.Getpid(), schemaSeq.Add(1))
	pool := testdb.New(t, schema)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
	return New(pool)
}

func bg() context.Context { return context.Background() }

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantNotFound(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func sortedIDs[T any](items []T, id func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, id(it))
	}
	slices.Sort(out)
	return out
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func (tc *fixture) area(name string) string {
	tc.t.Helper()
	a, err := tc.s.CreateArea(bg(), model.Area{Name: name})
	mustOK(tc.t, err)
	return a.ID
}

// fixture bundles a store and helpers to create library items quickly.
type fixture struct {
	t *testing.T
	s *Store
}

func newFixture(t *testing.T) *fixture { return &fixture{t: t, s: newUnitStore(t)} }

func (tc *fixture) llm(name string) model.LLM {
	tc.t.Helper()
	l, err := tc.s.CreateLLM(bg(), model.LLM{Name: name, Model: "openai/" + name})
	mustOK(tc.t, err)
	return l
}

func (tc *fixture) agent(name string, llmID *string, areas ...string) model.Agent {
	tc.t.Helper()
	a, err := tc.s.CreateAgent(bg(), model.Agent{Name: name, Role: "r", Goal: "g", Backstory: "b", LLMID: llmID, MaxIter: 5, Tools: []string{}, AreaIDs: areas})
	mustOK(tc.t, err)
	return a
}

func (tc *fixture) task(name string, agentID *string, areas ...string) model.Task {
	tc.t.Helper()
	tk, err := tc.s.CreateTask(bg(), model.Task{Name: name, Description: "d", ExpectedOutput: "e", AgentID: agentID, AreaIDs: areas})
	mustOK(tc.t, err)
	return tk
}

func (tc *fixture) crew(name string, agentIDs, taskIDs []string, areas ...string) model.Crew {
	tc.t.Helper()
	c, err := tc.s.CreateCrew(bg(), model.Crew{Name: name, Process: model.ProcessSequential, BudgetUSD: 2,
		AgentIDs: agentIDs, TaskIDs: taskIDs, AreaIDs: areas})
	mustOK(tc.t, err)
	return c
}

// simpleCrew creates agent+task+crew in the given areas.
func (tc *fixture) simpleCrew(name string, areas ...string) model.Crew {
	tc.t.Helper()
	a := tc.agent(name+"-agent", nil, areas...)
	tk := tc.task(name+"-task", &a.ID, areas...)
	return tc.crew(name, []string{a.ID}, []string{tk.ID}, areas...)
}

func (tc *fixture) admin() string {
	tc.t.Helper()
	id, err := tc.s.CreateFirstAdmin(bg(), "Admin@Example.com", "Admin", "hash")
	mustOK(tc.t, err)
	return id
}

func (tc *fixture) human(email string, in model.UserInput) model.User {
	tc.t.Helper()
	in.Kind, in.Email = auth.KindHuman, email
	if in.Name == "" {
		in.Name = email
	}
	if in.Status == "" {
		in.Status = model.UserActive
	}
	u, err := tc.s.CreateUser(bg(), in)
	mustOK(tc.t, err)
	return u
}

func (tc *fixture) service(name string, in model.UserInput) model.User {
	tc.t.Helper()
	in.Kind, in.Name = auth.KindService, name
	if in.Status == "" {
		in.Status = model.UserActive
	}
	u, err := tc.s.CreateUser(bg(), in)
	mustOK(tc.t, err)
	return u
}

func (tc *fixture) spec(crew model.Crew) model.RunSpec {
	return model.RunSpec{Crew: crew, Inputs: map[string]string{"k": "v"}, BudgetUSD: 1}
}

func (tc *fixture) run(crew model.Crew, meta RunMeta) model.Run {
	tc.t.Helper()
	r, _, err := tc.s.CreateRun(bg(), tc.spec(crew), meta)
	mustOK(tc.t, err)
	return r
}

func (tc *fixture) finish(runID string, status model.RunStatus) {
	tc.t.Helper()
	_, err := tc.s.FinishRun(bg(), runID, RunResult{Status: status})
	mustOK(tc.t, err)
}

func (tc *fixture) workOrder(title string, crew model.Crew) model.WorkOrder {
	tc.t.Helper()
	wo, err := tc.s.CreateWorkOrder(bg(), model.WorkOrderInput{Title: title, Brief: "b", CrewID: &crew.ID},
		WorkOrderMeta{TargetName: crew.Name, AreaIDs: crew.AreaIDs})
	mustOK(tc.t, err)
	return wo
}

func (tc *fixture) exec(sql string, args ...any) {
	tc.t.Helper()
	if _, err := tc.s.Pool().Exec(bg(), sql, args...); err != nil {
		tc.t.Fatalf("%s: %v", strings.Fields(sql)[0], err)
	}
}

// parallel runs fn n times concurrently and collects results.
func parallel[T any](n int, fn func(i int) T) []T {
	out := make([]T, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			out[i] = fn(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return out
}
