package api

import (
	"context"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// crud wires list/get/create/update/delete endpoints for one entity type.
type crud[T any] struct {
	// Permissions for reading, creating, changing and deleting
	view, create, edit, del string

	list   func(ctx context.Context, scope store.Scope) ([]T, error)
	get    func(ctx context.Context, id string) (T, error)
	insert func(ctx context.Context, in T) (T, error)
	update func(ctx context.Context, id string, in T) (T, error)
	remove func(ctx context.Context, id string) error
	// normalize validates input and returns a cleaned copy; id is "" when creating
	normalize func(ctx context.Context, in T, id string) (T, error)
	// areas, when set, makes the entity area-scoped
	areas *areaRules[T]
	// extra registers additional routes under the same prefix
	extra func(r chi.Router)
}

// areaRules says how an entity carries functional areas and what it references.
type areaRules[T any] struct {
	of   func(T) []string
	with func(T, []string) T
	// refs checks the items it references are visible (or already referenced before the change)
	refs func(ctx context.Context, p *auth.Principal, in T, old *T) error
}

func unscoped[T any](list func(ctx context.Context) ([]T, error)) func(context.Context, store.Scope) ([]T, error) {
	return func(ctx context.Context, _ store.Scope) ([]T, error) { return list(ctx) }
}

// mergeAreas applies a change to an item's areas: people may add or remove only their own areas; areas they
// are not in stay as they were. The result must not be empty.
func mergeAreas(p *auth.Principal, old, requested []string) ([]string, error) {
	var problems []string
	for _, id := range requested {
		if !p.HasArea(id) && !slices.Contains(old, id) {
			problems = append(problems, "area_ids: you can only add functional areas you belong to")
			break
		}
	}
	out := []string{}
	for _, id := range old {
		if !p.HasArea(id) {
			out = append(out, id)
		}
	}
	for _, id := range requested {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		problems = append(problems, "area_ids: pick at least one functional area")
	}
	if len(problems) > 0 {
		return nil, invalid(problems...)
	}
	return out, nil
}

func (c crud[T]) visible(p *auth.Principal, item T) bool {
	return c.areas == nil || p.SeesAreas(c.areas.of(item))
}

// load fetches an item and hides it (404) when it is outside the principal's areas.
func (c crud[T]) load(ctx context.Context, p *auth.Principal, id string) (T, error) {
	item, err := c.get(ctx, id)
	if err != nil {
		return item, err
	}
	if !c.visible(p, item) {
		var zero T
		return zero, store.ErrNotFound
	}
	return item, nil
}

// prepare validates input and applies the area and reference rules. old is nil when creating.
func (c crud[T]) prepare(ctx context.Context, p *auth.Principal, in T, id string, old *T) (T, error) {
	clean, err := c.normalize(ctx, in, id)
	if err != nil || c.areas == nil {
		return clean, err
	}
	var before []string
	if old != nil {
		before = c.areas.of(*old)
	}
	areas, err := mergeAreas(p, before, c.areas.of(clean))
	if err != nil {
		return clean, err
	}
	clean = c.areas.with(clean, areas)
	if c.areas.refs != nil {
		if err := c.areas.refs(ctx, p, clean, old); err != nil {
			return clean, err
		}
	}
	return clean, nil
}

func mount[T any](r chi.Router, s *Server, path string, c crud[T]) {
	r.Route(path, func(r chi.Router) {
		if c.extra != nil {
			c.extra(r)
		}
		r.With(s.need(c.view)).Get("/", func(w http.ResponseWriter, r *http.Request) {
			items, err := c.list(r.Context(), scopeOf(principal(r)))
			if err != nil {
				failErr(w, r, s.log, err)
				return
			}
			ok(w, items)
		})
		r.With(s.need(c.create)).Post("/", func(w http.ResponseWriter, r *http.Request) {
			var in T
			if !decode(w, r, &in) {
				return
			}
			clean, err := c.prepare(r.Context(), principal(r), in, "", nil)
			if err != nil {
				failErr(w, r, s.log, err)
				return
			}
			out, err := c.insert(r.Context(), clean)
			if err != nil {
				failErr(w, r, s.log, err)
				return
			}
			created(w, out)
		})
		r.With(s.need(c.view)).Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, valid := pathID(w, r)
			if !valid {
				return
			}
			out, err := c.load(r.Context(), principal(r), id)
			if err != nil {
				failErr(w, r, s.log, err)
				return
			}
			ok(w, out)
		})
		r.With(s.need(c.edit)).Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, valid := pathID(w, r)
			if !valid {
				return
			}
			var in T
			if !decode(w, r, &in) {
				return
			}
			p := principal(r)
			old, err := c.load(r.Context(), p, id)
			if err != nil {
				failErr(w, r, s.log, err)
				return
			}
			clean, err := c.prepare(r.Context(), p, in, id, &old)
			if err != nil {
				failErr(w, r, s.log, err)
				return
			}
			out, err := c.update(r.Context(), id, clean)
			if err != nil {
				failErr(w, r, s.log, err)
				return
			}
			ok(w, out)
		})
		r.With(s.need(c.del)).Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, valid := pathID(w, r)
			if !valid {
				return
			}
			if _, err := c.load(r.Context(), principal(r), id); err != nil {
				failErr(w, r, s.log, err)
				return
			}
			if err := c.remove(r.Context(), id); err != nil {
				failErr(w, r, s.log, err)
				return
			}
			noContent(w)
		})
	})
}

func (s *Server) knownTools() (map[string]bool, error) {
	tools, err := s.tools.ByKey()
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(tools))
	for key := range tools {
		known[key] = true
	}
	return known, nil
}

// checkRefs reports referenced ids the principal cannot see, unless the item already referenced them.
func (s *Server) checkRefs(ctx context.Context, p *auth.Principal, kind, field string, ids, before []string) error {
	if p.AllAreas || len(ids) == 0 {
		return nil
	}
	visible, err := s.store.VisibleIDs(ctx, kind, ids, scopeOf(p))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !visible[id] && !slices.Contains(before, id) {
			return invalid(field + ": " + id + " does not exist or is outside your functional areas")
		}
	}
	return nil
}

func ptrList(p *string) []string {
	if p == nil {
		return nil
	}
	return []string{*p}
}

func (s *Server) mountLibrary(r chi.Router) {
	st := s.store
	mount(r, s, "/llms", crud[model.LLM]{
		view: auth.LLMsView, create: auth.LLMsManage, edit: auth.LLMsManage, del: auth.LLMsManage,
		list: unscoped(st.ListLLMs), get: st.GetLLM, insert: st.CreateLLM, update: st.UpdateLLM, remove: st.DeleteLLM,
		normalize: func(_ context.Context, in model.LLM, _ string) (model.LLM, error) { return model.NormalizeLLM(in) },
	})
	mount(r, s, "/pricing", crud[model.ModelPrice]{
		view: auth.PricingView, create: auth.PricingManage, edit: auth.PricingManage, del: auth.PricingManage,
		list: unscoped(st.ListPrices), get: st.GetPrice, insert: st.CreatePrice, update: st.UpdatePrice, remove: st.DeletePrice,
		normalize: func(_ context.Context, in model.ModelPrice, _ string) (model.ModelPrice, error) {
			return model.NormalizePrice(in)
		},
	})
	mount(r, s, "/agents", crud[model.Agent]{
		view: auth.AgentsView, create: auth.AgentsCreate, edit: auth.AgentsEdit, del: auth.AgentsDelete,
		list: st.ListAgents, get: st.GetAgent, insert: st.CreateAgent, update: st.UpdateAgent, remove: st.DeleteAgent,
		normalize: func(_ context.Context, in model.Agent, _ string) (model.Agent, error) {
			known, err := s.knownTools()
			if err != nil {
				return in, err
			}
			return model.NormalizeAgent(in, known)
		},
		areas: &areaRules[model.Agent]{
			of:   func(a model.Agent) []string { return a.AreaIDs },
			with: func(a model.Agent, ids []string) model.Agent { a.AreaIDs = ids; return a },
		},
		extra: s.agentChatRoutes,
	})
	mount(r, s, "/tasks", crud[model.Task]{
		view: auth.TasksView, create: auth.TasksCreate, edit: auth.TasksEdit, del: auth.TasksDelete,
		list: st.ListTasks, get: st.GetTask, insert: st.CreateTask, update: st.UpdateTask, remove: st.DeleteTask,
		normalize: func(_ context.Context, in model.Task, id string) (model.Task, error) {
			in.ID = id // lets validation reject a task that lists itself as context
			return model.NormalizeTask(in)
		},
		areas: &areaRules[model.Task]{
			of:   func(t model.Task) []string { return t.AreaIDs },
			with: func(t model.Task, ids []string) model.Task { t.AreaIDs = ids; return t },
			refs: func(ctx context.Context, p *auth.Principal, in model.Task, old *model.Task) error {
				var oldAgent, oldContext []string
				if old != nil {
					oldAgent, oldContext = ptrList(old.AgentID), old.ContextTaskIDs
				}
				if err := s.checkRefs(ctx, p, "agent", "agent_id", ptrList(in.AgentID), oldAgent); err != nil {
					return err
				}
				return s.checkRefs(ctx, p, "task", "context_task_ids", in.ContextTaskIDs, oldContext)
			},
		},
		extra: func(r chi.Router) {
			r.With(s.need(auth.TasksView, auth.CrewsCreate)).Get("/{id}/crew-draft", s.taskCrewDraft)
		},
	})
	mount(r, s, "/crews", crud[model.Crew]{
		view: auth.CrewsView, create: auth.CrewsCreate, edit: auth.CrewsEdit, del: auth.CrewsDelete,
		list: st.ListCrews, get: st.GetCrew, insert: st.CreateCrew, update: st.UpdateCrew, remove: st.DeleteCrew,
		normalize: func(_ context.Context, in model.Crew, _ string) (model.Crew, error) { return model.NormalizeCrew(in) },
		areas: &areaRules[model.Crew]{
			of:   func(c model.Crew) []string { return c.AreaIDs },
			with: func(c model.Crew, ids []string) model.Crew { c.AreaIDs = ids; return c },
			refs: func(ctx context.Context, p *auth.Principal, in model.Crew, old *model.Crew) error {
				var oldAgents, oldTasks []string
				if old != nil {
					oldAgents, oldTasks = old.AgentIDs, old.TaskIDs
				}
				if err := s.checkRefs(ctx, p, "agent", "agent_ids", in.AgentIDs, oldAgents); err != nil {
					return err
				}
				return s.checkRefs(ctx, p, "task", "task_ids", in.TaskIDs, oldTasks)
			},
		},
		extra: func(r chi.Router) {
			r.With(s.need(auth.CrewsView)).Get("/{id}/validate", s.validateCrew)
			r.With(s.need(auth.CrewsRun)).Post("/{id}/runs", s.startRun)
			s.crewStateRoutes(r)
			s.chatSettingsRoutes(r)
			s.escalationSettingsRoutes(r)
		},
	})
}
