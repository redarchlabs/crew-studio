package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/memory"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

// A crew's memory as people see it on the crew's Memory tab: settings, the memories themselves, review of pending
// ones, bulk deletes and promotion to KM2 (memorykm2.go). Crews outside the caller's areas answer 404. Audit entries
// record ids, scopes and counts, never a memory's text.

// crewMemoryRoutes hangs off /crews (see crewStateRoutes).
func (s *Server) crewMemoryRoutes(r chi.Router) {
	r.Route("/{id}/memory", func(r chi.Router) {
		view, manage := s.need(auth.StateView), s.need(auth.StateManage)
		r.With(view).Get("/settings", s.getMemorySettings)
		// The settings decide what runs spend, so changing them needs crews.edit as well
		r.With(s.need(auth.CrewsEdit, auth.StateManage)).Put("/settings", s.putMemorySettings)
		r.With(view).Get("/", s.listCrewMemories)
		r.With(view).Get("/scopes", s.listMemoryScopes)
		r.With(manage).Post("/", s.createMemory)
		r.With(manage).Post("/delete", s.deleteMemories)
		r.With(manage).Post("/promote", s.promoteMemories)
		r.With(manage).Get("/promote/folders", s.promoteFolders)
		r.With(manage).Patch("/{mid}", s.updateMemory)
		r.With(manage).Delete("/{mid}", s.deleteMemory)
	})
}

// memorySettingsView answers GET and PUT /crews/{id}/memory/settings.
type memorySettingsView struct {
	Settings model.MemorySettings `json:"settings"`
	// Available is false when the server cannot embed (memory can then only be off)
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason"`
	EmbedModel        string `json:"embed_model"`
	// SuggestedLLMID is the crew's cheapest priced agent model, used when settings.llm_id is null
	SuggestedLLMID *string `json:"suggested_llm_id"`
	// ReembedPending counts the crew's memories still embedded with another model than EmbedModel; the background
	// worker re-embeds them, and until then recall cannot find them
	ReembedPending int `json:"reembed_pending"`
}

func (s *Server) memorySettingsView(r *http.Request, crew model.Crew, settings model.MemorySettings) (memorySettingsView, error) {
	available, reason := s.memory.Available()
	view := memorySettingsView{Settings: settings, Available: available, UnavailableReason: reason, EmbedModel: s.memory.Model()}
	if view.EmbedModel != "" {
		n, err := s.store.CountMemories(r.Context(), store.MemoryFilter{CrewID: crew.ID, OtherEmbedModel: view.EmbedModel})
		if err != nil {
			return view, err
		}
		view.ReembedPending = n
	}
	lib, err := s.launcher.Library(r.Context())
	if err != nil {
		return view, err
	}
	if id := runspec.CheapestAgentLLM(crew, lib); id != "" {
		view.SuggestedLLMID = &id
	}
	return view, nil
}

func (s *Server) getMemorySettings(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	settings, err := s.store.MemorySettings(r.Context(), crew.ID)
	var view memorySettingsView
	if err == nil {
		view, err = s.memorySettingsView(r, crew, settings)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, view)
}

func (s *Server) putMemorySettings(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	var in model.MemorySettings
	if !decode(w, r, &in) {
		return
	}
	in.CrewID = crew.ID
	clean, err := model.NormalizeMemorySettings(in)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	// Turning memory off always works; turning it on needs an embedder
	if available, reason := s.memory.Available(); !available && clean.Mode != model.MemoryOff {
		fail(w, http.StatusConflict, "memory_unavailable", reason)
		return
	}
	lib, err := s.launcher.Library(r.Context())
	if err == nil {
		err = memoryLLMProblems(crew, lib, clean)
	}
	var saved model.MemorySettings
	if err == nil {
		saved, err = s.store.PutMemorySettings(r.Context(), clean, principal(r).UserID)
	}
	var view memorySettingsView
	if err == nil {
		view, err = s.memorySettingsView(r, crew, saved)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	p := principal(r)
	s.audit(r, "memory_settings_changed", p.UserID, p.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name,
		"mode": saved.Mode, "llm_id": saved.LLMID, "recall_depth": saved.RecallDepth, "recall_limit": saved.RecallLimit,
		"saves_per_run": saved.SavesPerRun, "retention_days": saved.RetentionDays, "review_run_saves": saved.ReviewRunSaves})
	ok(w, view)
}

// memoryLLMProblems checks the memory model exists and has a price, or that the crew has a priced model to fall back on.
// LLMs are library-wide (they have no areas), so, as with an agent's llm_id, any priced LLM may be picked.
func memoryLLMProblems(crew model.Crew, lib runspec.Library, in model.MemorySettings) error {
	if in.LLMID != nil {
		llm, found := lib.LLMs[*in.LLMID]
		switch {
		case !found:
			return invalid("llm_id: no such LLM")
		case runspec.MatchPrice(llm.Model, lib.Pricing) == nil:
			return invalid("llm_id: " + llm.Name + " has no price; add one on the Pricing page")
		}
		return nil
	}
	if in.NeedsLLM() && runspec.CheapestAgentLLM(crew, lib) == "" {
		return invalid("llm_id: none of the crew's agents has a priced model to fall back on; pick one")
	}
	return nil
}

// adminMemoryFilter reads the Memory tab's filters: scope (a prefix), status, pinned and run_id.
func adminMemoryFilter(r *http.Request, crewID string) (store.MemoryFilter, error) {
	q := r.URL.Query()
	f := store.MemoryFilter{CrewID: crewID}
	var p memory.Problems
	if q.Get("scope") != "" {
		scope, err := memory.NormalizeScope("scope", q.Get("scope"), crewID)
		if err != nil {
			p = append(p, problemText(err))
		}
		f.ScopePrefix = scope
	}
	switch status := q.Get("status"); status {
	case "", model.MemoryActive, model.MemoryPending:
		f.Status = status
	default:
		p.Add("status", "must be active or pending")
	}
	switch q.Get("pinned") {
	case "":
	case "true":
		yes := true
		f.Pinned = &yes
	case "false":
		no := false
		f.Pinned = &no
	default:
		p.Add("pinned", "must be true or false")
	}
	if runID := q.Get("run_id"); runID != "" {
		if !uuidPattern.MatchString(runID) {
			p.Add("run_id", "must be a run's id")
		}
		f.RunID = runID
	}
	return f, p.Err()
}

func (s *Server) listCrewMemories(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	f, err := adminMemoryFilter(r, crew.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	limit, offset := pageParams(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var items []model.MemoryRecord
	var total int
	if q != "" {
		items, total, err = s.searchMemories(r, f, q, limit, offset)
	} else {
		items, total, err = s.store.ListMemories(r.Context(), f, limit, offset)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	page(w, items, model.Page{Total: total, Limit: limit, Offset: offset})
}

// searchMemories finds memories by meaning, best first, keeping any whose text contains q; without an embedder
// (or when it fails) it falls back to the text match alone, newest first.
func (s *Server) searchMemories(r *http.Request, f store.MemoryFilter, q string, limit, offset int) ([]model.MemoryRecord, int, error) {
	vectors, err := s.memory.Embed(r.Context(), "", []string{q}, true, nil)
	if err != nil {
		if !errors.Is(err, memory.ErrUnavailable) {
			s.log.Warn("memory search fell back to text", "err", err)
		}
		f.Contains = q
		return s.store.ListMemories(r.Context(), f, limit, offset)
	}
	rows, err := s.store.MemoryRows(r.Context(), f)
	if err != nil {
		return nil, 0, err
	}
	embedModel, floor, lower := s.memory.Model(), s.memory.MinSimilarity(), strings.ToLower(q)
	type hit struct {
		rec   model.MemoryRecord
		score float64
	}
	var hits []hit
	for _, row := range rows {
		score := 0.0
		if row.EmbedModel == embedModel {
			score = memory.Cosine(vectors[0], row.Vector)
		}
		if score >= floor || strings.Contains(strings.ToLower(row.Content), lower) {
			row.Vector = nil
			hits = append(hits, hit{rec: row, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := []model.MemoryRecord{}
	for i := offset; i < len(hits) && len(out) < limit; i++ {
		out = append(out, hits[i].rec)
	}
	return out, len(hits), nil
}

func (s *Server) listMemoryScopes(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	scopes, err := s.store.MemoryScopes(r.Context(), store.MemoryFilter{CrewID: crew.ID})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, scopes)
}

// memoryInput is a memory a person adds by hand.
type memoryInput struct {
	Content    string         `json:"content"`
	Scope      string         `json:"scope"`
	Categories []string       `json:"categories"`
	Metadata   map[string]any `json:"metadata"`
	Importance *float64       `json:"importance"`
	Pinned     bool           `json:"pinned"`
}

// memoryUnavailable answers 409 itself when the server cannot embed.
func (s *Server) memoryUnavailable(w http.ResponseWriter) bool {
	if available, reason := s.memory.Available(); !available {
		fail(w, http.StatusConflict, "memory_unavailable", reason)
		return true
	}
	return false
}

// embedForPerson embeds one memory's text for a person (no run cache, no run cap).
func (s *Server) embedForPerson(w http.ResponseWriter, r *http.Request, content string) ([]float32, bool) {
	vectors, err := s.memory.Embed(r.Context(), "", []string{content}, false, nil)
	if err != nil {
		s.memoryFailure(w, r, false, err)
		return nil, false
	}
	return vectors[0], true
}

func (s *Server) createMemory(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	var in memoryInput
	if !decode(w, r, &in) {
		return
	}
	var p memory.Problems
	clean := memory.CleanInput(&p, "", memory.Input{Content: in.Content, Scope: in.Scope, Categories: in.Categories,
		Metadata: in.Metadata, Importance: in.Importance}, crew.ID)
	if err := p.Err(); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if s.memoryUnavailable(w) {
		return
	}
	vector, fine := s.embedForPerson(w, r, clean.Content)
	if !fine {
		return
	}
	who := principal(r)
	res, err := s.store.SaveMemories(r.Context(), crew.ID, []store.MemoryWrite{{CrewID: crew.ID, Scope: clean.Scope, Content: clean.Content,
		ContentHash: clean.Hash, Categories: clean.Categories, Metadata: clean.Metadata, Importance: clean.Importance,
		Status: model.MemoryActive, Pinned: in.Pinned, Source: "user:" + who.Name, WrittenBy: who.Name, EmbedModel: s.memory.Model(),
		Vector: vector}}, store.MemoryCaps{PerCrew: model.MaxMemoriesPerCrew})
	switch {
	case err != nil:
		failErr(w, r, s.log, err)
		return
	case res.CrewFull:
		fail(w, http.StatusTooManyRequests, "memory_crew_full", "This crew already holds the most memories it may; delete some first.")
		return
	case res.Saved[0].Duplicate:
		fail(w, http.StatusConflict, "memory_exists", "A memory in this scope already says the same thing.")
		return
	}
	rec, err := s.store.GetMemory(r.Context(), crew.ID, res.Saved[0].ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.audit(r, "memory_created", who.UserID, who.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name, "memory_id": rec.ID,
		"scope": rec.Scope})
	created(w, rec)
}

// loadMemory reads {mid} of the crew, answering 404 itself.
func (s *Server) loadMemory(w http.ResponseWriter, r *http.Request, crewID string) (model.MemoryRecord, bool) {
	id, valid := pathUUID(w, r, "mid")
	if !valid {
		return model.MemoryRecord{}, false
	}
	rec, err := s.store.GetMemory(r.Context(), crewID, id)
	if err != nil {
		failErr(w, r, s.log, err)
		return rec, false
	}
	return rec, true
}

func (s *Server) updateMemory(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	var in memoryPatch
	if !decode(w, r, &in) {
		return
	}
	old, found := s.loadMemory(w, r, crew.ID)
	if !found {
		return
	}
	var p memory.Problems
	u := in.update(&p, old, crew.ID)
	if in.Status != nil && *in.Status != model.MemoryActive {
		p.Add("status", `can only be "active", which approves a pending memory`)
	}
	u.Status, u.Pinned = in.Status, in.Pinned
	if err := p.Err(); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if u.Content != nil {
		if s.memoryUnavailable(w) {
			return
		}
		vector, fine := s.embedForPerson(w, r, *u.Content)
		if !fine {
			return
		}
		u.Vector, u.EmbedModel = vector, s.memory.Model()
	}
	rec, err := s.store.UpdateMemory(r.Context(), crew.ID, old.ID, u)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	who := principal(r)
	s.audit(r, "memory_updated", who.UserID, who.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name, "memory_id": rec.ID,
		"scope": rec.Scope, "content_changed": u.Content != nil, "approved": old.Status == model.MemoryPending && rec.Status == model.MemoryActive,
		"pinned": rec.Pinned})
	ok(w, rec)
}

func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	id, valid := pathUUID(w, r, "mid")
	if !valid {
		return
	}
	rec, err := s.store.DeleteMemory(r.Context(), crew.ID, id)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	who := principal(r)
	s.audit(r, "memory_deleted", who.UserID, who.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name, "memory_id": rec.ID,
		"scope": rec.Scope})
	noContent(w)
}

// memoriesDeleteRequest deletes many memories at once (pinned ones included): a scope and everything below it,
// those whose text contains something (such as a person's name), and/or a list of ids. dry_run only counts.
type memoriesDeleteRequest struct {
	ScopePrefix *string  `json:"scope_prefix"`
	Contains    string   `json:"contains"`
	IDs         []string `json:"ids"`
	DryRun      bool     `json:"dry_run"`
}

type memoriesDeleteResponse struct {
	Matched int `json:"matched"`
	Deleted int `json:"deleted"`
}

func (s *Server) deleteMemories(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	var in memoriesDeleteRequest
	if !decode(w, r, &in) {
		return
	}
	f := store.MemoryFilter{CrewID: crew.ID, Contains: strings.TrimSpace(in.Contains)}
	var p memory.Problems
	if in.ScopePrefix != nil {
		scope, err := memory.NormalizeScope("scope_prefix", *in.ScopePrefix, crew.ID)
		if err != nil {
			p = append(p, problemText(err))
		}
		f.ScopePrefix = scope
	}
	if in.IDs != nil {
		f.IDs = memoryIDs(&p, "ids", in.IDs)
	}
	if in.ScopePrefix == nil && f.Contains == "" && len(in.IDs) == 0 {
		p.Add("scope_prefix", "give a scope, some text or memory ids to delete")
	}
	if err := p.Err(); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	matched, err := s.store.DeleteMemories(r.Context(), f, in.DryRun)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	out := memoriesDeleteResponse{Matched: matched}
	if !in.DryRun {
		out.Deleted = matched
		who := principal(r)
		s.audit(r, "memories_deleted", who.UserID, who.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name,
			"scope_prefix": f.ScopePrefix, "by_text": f.Contains != "", "ids": len(in.IDs), "deleted": matched})
	}
	ok(w, out)
}
