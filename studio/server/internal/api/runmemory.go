package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/memory"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Memory during a run (docs/crew-state-and-pmo-design.md, section 19). The runner's StudioMemoryStorage and
// StudioEmbedder call these with the run token; they map onto CrewAI's StorageBackend. The run's frozen memory
// settings (RunSpec.Memory, read with the token) decide what it may do. Scopes are paths relative to the crew.
// Searches, saves, updates, deletes and refusals are journalled; embeds, reads, counts and touches are not.

const (
	// memoryEmbedCapPerRun bounds the texts one run may have embedded (cache misses only)
	memoryEmbedCapPerRun   = 300
	maxMemoryEmbedText     = 20_000
	maxMemorySearchLimit   = 50
	defaultMemorySearch    = 10
	maxMemoryListLimit     = 200
	maxMemoryRecordsInSave = 20
	maxMemoryIDs           = 200
	memoryLineRole         = 200
)

var errMemoryEmbedCap = errors.New("memory embed cap")

// memoryRunRoutes hangs off /run-state (see runStateRoutes).
func (s *Server) memoryRunRoutes(r chi.Router) {
	r.Route("/memory", func(r chi.Router) {
		r.Use(s.requireRunMemory)
		writes := s.memoryWrites
		r.Post("/embed", s.runMemoryEmbed)
		r.Post("/search", s.runMemorySearch)
		r.With(writes).Post("/records", s.runMemorySave)
		r.Get("/records", s.runMemoryList)
		r.Get("/records/{mid}", s.runMemoryGet)
		r.With(writes).Patch("/records/{mid}", s.runMemoryUpdate)
		r.Get("/scopes", s.runMemoryScopes)
		r.Get("/scope-info", s.runMemoryScopeInfo)
		r.Get("/categories", s.runMemoryCategories)
		r.Get("/count", s.runMemoryCount)
		r.With(writes).Post("/delete", s.runMemoryDelete)
		r.With(writes).Post("/touch", s.runMemoryTouch)
	})
}

// runMemory is the run's frozen memory settings; requireRunMemory guarantees it is set.
func runMemory(r *http.Request) model.MemorySpec {
	if m := runCallOf(r).scope.Memory; m != nil {
		return *m
	}
	return model.MemorySpec{Mode: model.MemoryOff}
}

// requireRunMemory answers 403 memory_off unless memory is on for the run.
func (s *Server) requireRunMemory(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if runMemory(r).Mode == model.MemoryOff {
			s.journal(r, "memory", "Memory: refused, memory is off for this run", "")
			fail(w, http.StatusForbidden, "memory_off", "Memory is off for this run.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// memoryWrites answers 403 memory_read_only for a run whose crew only recalls memories.
func (s *Server) memoryWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if runMemory(r).Mode == model.MemoryRecallOnly {
			s.journal(r, "memory", "Memory: write refused, this crew only recalls memories", "")
			fail(w, http.StatusForbidden, "memory_read_only", "This crew only recalls memories; it does not save or change them.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// memoryFailure answers an embedding failure: unavailable, busy, the run's cap, or the provider.
func (s *Server) memoryFailure(w http.ResponseWriter, r *http.Request, journal bool, err error) {
	var pe *memory.ProviderError
	switch {
	case errors.Is(err, memory.ErrUnavailable):
		_, reason := s.memory.Available()
		fail(w, http.StatusServiceUnavailable, "memory_unavailable", "Crew Studio cannot embed memories right now. "+reason)
	case errors.Is(err, memory.ErrBusy):
		tooMany(w, "Too many memory embeddings across Crew Studio just now; try again shortly.")
	case errors.Is(err, errMemoryEmbedCap):
		if journal {
			s.journal(r, "memory", fmt.Sprintf("Memory: refused, the run already embedded %d texts", memoryEmbedCapPerRun), "")
		}
		fail(w, http.StatusTooManyRequests, "memory_embed_cap", fmt.Sprintf("This run has already embedded %d texts for memory, the most one run may.", memoryEmbedCapPerRun))
	case errors.As(err, &pe):
		s.log.Warn("memory embeddings failed", "err", pe.Err)
		fail(w, http.StatusServiceUnavailable, "memory_unavailable", "The embedding provider failed, so memory is unavailable for now.")
	default:
		failErr(w, r, s.log, err)
	}
}

// embedForRun embeds texts for the calling run, counting cache misses against its cap. The count is reserved before
// the provider is called (so concurrent calls cannot pass the cap) and given back when the call fails.
func (s *Server) embedForRun(r *http.Request, texts []string, query bool) ([][]float32, error) {
	runID := runCallOf(r).scope.RunID
	reserved := 0
	vectors, err := s.memory.Embed(r.Context(), runID, texts, query, func(n int) error {
		allowed, err := s.store.AddMemoryEmbeds(r.Context(), runID, n, memoryEmbedCapPerRun)
		if err == nil && !allowed {
			err = errMemoryEmbedCap
		}
		if err == nil {
			reserved = n
		}
		return err
	})
	if err != nil && reserved > 0 {
		if rerr := s.store.RefundMemoryEmbeds(context.WithoutCancel(r.Context()), runID, reserved); rerr != nil {
			s.log.Warn("could not give back memory embeddings", "run_id", runID, "err", rerr)
		}
	}
	return vectors, err
}

type memoryEmbedRequest struct {
	Texts []string `json:"texts"`
	// Kind is "query" while recalling, otherwise "document"
	Kind string `json:"kind"`
}

type memoryEmbedResponse struct {
	Model      string      `json:"model"`
	Dimensions int         `json:"dimensions"`
	Vectors    [][]float32 `json:"vectors"`
}

func (s *Server) runMemoryEmbed(w http.ResponseWriter, r *http.Request) {
	var in memoryEmbedRequest
	if !decode(w, r, &in) {
		return
	}
	var p memory.Problems
	if len(in.Texts) == 0 || len(in.Texts) > memory.MaxEmbedBatch {
		p.Add("texts", "send 1 to %d texts", memory.MaxEmbedBatch)
	}
	for i, t := range in.Texts {
		if strings.TrimSpace(t) == "" || len([]rune(t)) > maxMemoryEmbedText {
			p.Add(fmt.Sprintf("texts[%d]", i), "must be 1 to %d characters", maxMemoryEmbedText)
		}
	}
	if in.Kind != "" && in.Kind != "query" && in.Kind != "document" {
		p.Add("kind", "must be query or document")
	}
	if err := p.Err(); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	vectors, err := s.embedForRun(r, in.Texts, in.Kind == "query")
	if err != nil {
		s.memoryFailure(w, r, true, err)
		return
	}
	ok(w, memoryEmbedResponse{Model: s.memory.Model(), Dimensions: len(vectors[0]), Vectors: vectors})
}

// memoryQuery reads the filters a run sends with search and delete.
type memoryQuery struct {
	ScopePrefix    *string        `json:"scope_prefix"`
	Categories     []string       `json:"categories"`
	MetadataFilter map[string]any `json:"metadata_filter"`
}

// filter checks the query and returns a filter of the run's crew.
func (q memoryQuery) filter(p *memory.Problems, crewID string) store.MemoryFilter {
	f := store.MemoryFilter{CrewID: crewID}
	if q.ScopePrefix != nil {
		scope, err := memory.NormalizeScope("scope_prefix", *q.ScopePrefix, crewID)
		if err != nil {
			*p = append(*p, problemText(err))
		}
		f.ScopePrefix = scope
	}
	if len(q.Categories) > 0 {
		f.Categories = memory.CleanCategories(p, "categories", q.Categories)
	}
	if len(q.MetadataFilter) > 0 {
		f.Metadata = memory.CleanMetadata(p, "metadata_filter", q.MetadataFilter)
	}
	return f
}

type memorySearchRequest struct {
	memoryQuery
	Embedding []float32 `json:"embedding"`
	Limit     int       `json:"limit"`
	MinScore  float64   `json:"min_score"`
}

type memorySearchResponse struct {
	Matches []memory.Scored `json:"matches"`
}

func (s *Server) runMemorySearch(w http.ResponseWriter, r *http.Request) {
	var in memorySearchRequest
	if !decode(w, r, &in) {
		return
	}
	call := runCallOf(r)
	var p memory.Problems
	f := in.filter(&p, call.scope.CrewID)
	if !memory.ValidVector(in.Embedding) {
		p.Add("embedding", "must be 1 to %d finite numbers", memory.MaxVectorLen)
	}
	if in.Limit == 0 {
		in.Limit = defaultMemorySearch
	}
	if in.Limit < 1 || in.Limit > maxMemorySearchLimit {
		p.Add("limit", "must be between 1 and %d", maxMemorySearchLimit)
	}
	if in.MinScore < 0 || in.MinScore > 1 {
		p.Add("min_score", "must be between 0 and 1")
	}
	if err := p.Err(); err != nil {
		s.journal(r, "memory_search", "Memory: search refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	if ok, _ := s.memory.Available(); !ok {
		s.memoryFailure(w, r, true, memory.ErrUnavailable)
		return
	}
	f.VisibleToRun, f.EmbedModel = call.scope.RunID, s.memory.Model()
	rows, err := s.store.MemoryRows(r.Context(), f)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	// CrewAI asks with min_score 0; the embedder's relevance floor keeps unrelated memories out of the prompt
	matches := memory.Rank(in.Embedding, rows, max(in.MinScore, s.memory.MinSimilarity()), in.Limit)
	s.journal(r, "memory_search", fmt.Sprintf("Memory: recalled %d of %d", len(matches), len(rows)), "")
	ok(w, memorySearchResponse{Matches: matches})
}

type memorySaveRequest struct {
	Records []memory.Input `json:"records"`
}

type memorySaveResponse struct {
	Saved          []store.MemorySaved `json:"saved"`
	Skipped        int                 `json:"skipped"`
	RemainingSaves int                 `json:"remaining_saves"`
}

func (s *Server) runMemorySave(w http.ResponseWriter, r *http.Request) {
	const tool = "memory_save"
	var in memorySaveRequest
	if !decode(w, r, &in) {
		return
	}
	call, spec := runCallOf(r), runMemory(r)
	var p memory.Problems
	if len(in.Records) == 0 || len(in.Records) > maxMemoryRecordsInSave {
		p.Add("records", "send 1 to %d memories", maxMemoryRecordsInSave)
	}
	clean := make([]memory.Clean, len(in.Records))
	for i, rec := range in.Records {
		clean[i] = memory.CleanInput(&p, fmt.Sprintf("records[%d]", i), rec, call.scope.CrewID)
	}
	if err := p.Err(); err != nil {
		s.journal(r, tool, "Memory: save refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	used, err := s.store.RunMemorySaves(r.Context(), call.scope.RunID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if used >= spec.SavesPerRun {
		s.journal(r, tool, fmt.Sprintf("Memory: save refused, the run already saved %d memories", used), "")
		fail(w, http.StatusTooManyRequests, "memory_save_cap", fmt.Sprintf("This run has already saved %d memories, the most this crew allows per run.", used))
		return
	}
	writes, fine := s.memoryWritesFor(w, r, clean)
	if !fine {
		return
	}
	res, err := s.store.SaveMemories(r.Context(), call.scope.CrewID, writes, store.MemoryCaps{RunID: call.scope.RunID,
		PerRun: spec.SavesPerRun, PerCrew: model.MaxMemoriesPerCrew, PendingPerCrew: model.MaxPendingMemoriesPerCrew})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if res.Skipped == len(res.Saved) && (res.CrewFull || res.PendingFull) {
		if res.CrewFull {
			s.journal(r, tool, "Memory: save refused, the crew holds the most memories it may", "")
			fail(w, http.StatusTooManyRequests, "memory_crew_full", fmt.Sprintf("This crew already holds %d memories, the most it may; people can delete some on its Memory tab.", model.MaxMemoriesPerCrew))
			return
		}
		s.journal(r, tool, "Memory: save refused, too many memories wait for review", "")
		fail(w, http.StatusTooManyRequests, "memory_pending_full", fmt.Sprintf("%d of this crew's memories are waiting for a person to review them, the most it may have; "+
			"people can approve or delete them on its Memory tab.", model.MaxPendingMemoriesPerCrew))
		return
	}
	s.journal(r, tool, memorySaveLine(res, writes), "")
	created(w, memorySaveResponse{Saved: orEmpty(res.Saved), Skipped: res.Skipped, RemainingSaves: res.Remaining})
}

func memorySaveLine(res store.MemorySaveResult, writes []store.MemoryWrite) string {
	dup := 0
	for _, sv := range res.Saved {
		if sv.Duplicate {
			dup++
		}
	}
	line := fmt.Sprintf("Memory: saved %d", len(res.Inserted))
	var extra []string
	if dup > 0 {
		extra = append(extra, fmt.Sprintf("%d already known", dup))
	}
	skips := map[string]int{}
	for _, sv := range res.Saved {
		skips[sv.Skipped]++
	}
	for _, sk := range []struct{ reason, text string }{{store.SkipSaveCap, "skipped over the cap"}, {store.SkipCrewFull, "skipped, the crew is full"},
		{store.SkipPendingFull, "skipped, too many wait for review"}, {store.SkipNotSaved, "not saved"}} {
		if n := skips[sk.reason]; n > 0 {
			extra = append(extra, fmt.Sprintf("%d %s", n, sk.text))
		}
	}
	if len(extra) > 0 {
		line += " (" + strings.Join(extra, ", ") + ")"
	}
	if len(res.Inserted) > 0 && writes[0].Status == model.MemoryPending {
		line += ", waiting for review"
	}
	return line
}

// memoryWritesFor embeds cleaned memories for the calling run and turns them into writes, marking near-duplicates
// among the memories the run may see in the same scope (another run's pending memory is never revealed). Every
// write is pending while the run's saves are reviewed. It answers itself on failure.
func (s *Server) memoryWritesFor(w http.ResponseWriter, r *http.Request, clean []memory.Clean) ([]store.MemoryWrite, bool) {
	call, spec := runCallOf(r), runMemory(r)
	texts := make([]string, len(clean))
	for i, c := range clean {
		texts[i] = c.Content
	}
	vectors, err := s.embedForRun(r, texts, false)
	if err != nil {
		s.memoryFailure(w, r, true, err)
		return nil, false
	}
	embedModel := s.memory.Model()
	existing, err := s.store.MemoryRows(r.Context(), store.MemoryFilter{CrewID: call.scope.CrewID, EmbedModel: embedModel, VisibleToRun: call.scope.RunID})
	if err != nil {
		failErr(w, r, s.log, err)
		return nil, false
	}
	byScope := map[string][]model.MemoryRecord{}
	for _, row := range existing {
		byScope[row.Scope] = append(byScope[row.Scope], row)
	}
	threshold, runID := s.memory.NearDuplicateSimilarity(), call.scope.RunID
	writes := make([]store.MemoryWrite, len(clean))
	for i, c := range clean {
		role := memory.StripInvisible(cleanAgentRole(stringOf(c.Metadata["agent"])))
		if role == "" {
			role = memory.StripInvisible(call.agentRole)
		}
		writes[i] = store.MemoryWrite{ID: c.ID, CrewID: call.scope.CrewID, Scope: c.Scope, Content: c.Content, ContentHash: c.Hash,
			Categories: c.Categories, Metadata: c.Metadata, Importance: c.Importance, Status: spec.SavedStatus(), AgentRole: role,
			TaskName: km2Clip(memory.StripInvisible(cleanAgentRole(stringOf(c.Metadata["task"]))), memoryLineRole), Source: "run:" + runID,
			WrittenBy: "run", RunID: &runID, EmbedModel: embedModel, Vector: vectors[i]}
		if dup, found := memory.NearDuplicate(vectors[i], byScope[c.Scope], threshold); found {
			writes[i].DuplicateOf = dup.ID
		}
	}
	return writes, true
}

type memoryRecordsResponse struct {
	Records []model.MemoryRecord `json:"records"`
}

// scopeParam reads a scope query parameter ("" when absent).
func scopeParam(r *http.Request, name, crewID string) (string, error) {
	if !r.URL.Query().Has(name) {
		return "", nil
	}
	return memory.NormalizeScope(name, r.URL.Query().Get(name), crewID)
}

func (s *Server) runMemoryList(w http.ResponseWriter, r *http.Request) {
	call := runCallOf(r)
	scope, err := scopeParam(r, "scope_prefix", call.scope.CrewID)
	limit, offset := maxMemoryListLimit, 0
	q := r.URL.Query()
	if err == nil && q.Has("limit") {
		if limit, err = strconv.Atoi(q.Get("limit")); err != nil || limit < 1 || limit > maxMemoryListLimit {
			err = invalid(fmt.Sprintf("limit: must be between 1 and %d", maxMemoryListLimit))
		}
	}
	if err == nil && q.Has("offset") {
		if offset, err = strconv.Atoi(q.Get("offset")); err != nil || offset < 0 {
			err = invalid("offset: must be 0 or more")
		}
	}
	var items []model.MemoryRecord
	if err == nil {
		items, _, err = s.store.ListMemories(r.Context(), store.MemoryFilter{CrewID: call.scope.CrewID, VisibleToRun: call.scope.RunID, ScopePrefix: scope}, limit, offset)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, memoryRecordsResponse{Records: items})
}

// runMemoryRecord reads {mid} as a memory the run may see; it answers 404 itself otherwise.
func (s *Server) runMemoryRecord(w http.ResponseWriter, r *http.Request) (model.MemoryRecord, bool) {
	id, valid := pathUUID(w, r, "mid")
	if !valid {
		return model.MemoryRecord{}, false
	}
	call := runCallOf(r)
	rec, err := s.store.GetMemory(r.Context(), call.scope.CrewID, id)
	if err == nil && rec.Status != model.MemoryActive && (rec.RunID == nil || *rec.RunID != call.scope.RunID) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return rec, false
	}
	return rec, true
}

func (s *Server) runMemoryGet(w http.ResponseWriter, r *http.Request) {
	if rec, found := s.runMemoryRecord(w, r); found {
		ok(w, rec)
	}
}

// memoryPatch changes a memory. Runs send content, scope, categories, metadata and importance; people may also pin
// and approve.
type memoryPatch struct {
	Content    *string        `json:"content"`
	Scope      *string        `json:"scope"`
	Categories *[]string      `json:"categories"`
	Metadata   map[string]any `json:"metadata"`
	Importance *float64       `json:"importance"`
	Pinned     *bool          `json:"pinned"`
	Status     *string        `json:"status"`
}

// update checks the patch against the memory as it is and returns the store update; a new content still needs
// its vector.
func (in memoryPatch) update(p *memory.Problems, old model.MemoryRecord, crewID string) store.MemoryUpdate {
	var u store.MemoryUpdate
	if in.Content != nil {
		content := memory.CleanContent(p, "content", *in.Content)
		if content != old.Content {
			u.Content, u.ContentHash = &content, memory.ContentHash(content)
		}
	}
	if in.Scope != nil {
		scope, err := memory.NormalizeScope("scope", *in.Scope, crewID)
		if err != nil {
			*p = append(*p, problemText(err))
		}
		u.Scope = &scope
	}
	if in.Categories != nil {
		cats := memory.CleanCategories(p, "categories", *in.Categories)
		u.Categories = &cats
	}
	if in.Metadata != nil {
		u.Metadata = memory.CleanMetadata(p, "metadata", in.Metadata)
	}
	if in.Importance != nil {
		imp := memory.CleanImportance(p, "importance", in.Importance)
		u.Importance = &imp
	}
	return u
}

func (s *Server) runMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	const tool = "memory_update"
	var in memoryPatch
	if !decode(w, r, &in) {
		return
	}
	old, found := s.runMemoryRecord(w, r)
	if !found {
		return
	}
	call, spec := runCallOf(r), runMemory(r)
	if old.RunID == nil || *old.RunID != call.scope.RunID {
		s.memoryNotOwned(w, r)
		return
	}
	if memoryLocked(old, spec) {
		s.memoryLocked(w, r)
		return
	}
	var p memory.Problems
	if in.Pinned != nil || in.Status != nil {
		p.Add("pinned", "runs cannot pin or approve memories")
	}
	u := in.update(&p, old, call.scope.CrewID)
	if err := p.Err(); err != nil {
		s.journal(r, tool, "Memory: change refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	if u.Content != nil {
		vectors, err := s.embedForRun(r, []string{*u.Content}, false)
		if err != nil {
			s.memoryFailure(w, r, true, err)
			return
		}
		u.Vector, u.EmbedModel = vectors[0], s.memory.Model()
	}
	u.OnlyRunID, u.RunReview = call.scope.RunID, spec.ReviewRunSaves
	rec, err := s.store.UpdateMemory(r.Context(), call.scope.CrewID, old.ID, u)
	switch {
	case errors.Is(err, store.ErrMemoryNotOwned):
		s.memoryNotOwned(w, r)
		return
	case errors.Is(err, store.ErrMemoryLocked):
		s.memoryLocked(w, r)
		return
	case err != nil:
		s.journal(r, tool, "Memory: change refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	s.journal(r, tool, "Memory: changed a memory in "+rec.Scope, "")
	ok(w, rec)
}

// memoryLocked reports whether a person vouched for a memory, so runs may no longer change or delete it: they pinned
// it, or approved it while the run's saves are reviewed (an active memory then was approved).
func memoryLocked(rec model.MemoryRecord, spec model.MemorySpec) bool {
	return rec.Pinned || (spec.ReviewRunSaves && rec.Status == model.MemoryActive)
}

func (s *Server) memoryNotOwned(w http.ResponseWriter, r *http.Request) {
	s.journal(r, "memory_update", "Memory: change refused, the memory was not written by this run", "")
	fail(w, http.StatusForbidden, "memory_not_owned", "This run may change only the memories it saved itself.")
}

func (s *Server) memoryLocked(w http.ResponseWriter, r *http.Request) {
	s.journal(r, "memory_update", "Memory: change refused, a person approved or pinned it", "")
	fail(w, http.StatusForbidden, "memory_locked", "A person approved or pinned this memory, so runs can no longer change it.")
}

type memoryScopesResponse struct {
	Scopes []string `json:"scopes"`
}

// visibleScopes are the scopes of the memories the run may see.
func (s *Server) visibleScopes(r *http.Request) ([]model.MemoryScopeCount, error) {
	call := runCallOf(r)
	return s.store.MemoryScopes(r.Context(), store.MemoryFilter{CrewID: call.scope.CrewID, VisibleToRun: call.scope.RunID})
}

func childScopes(scopes []model.MemoryScopeCount, parent string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, sc := range scopes {
		if child := memory.ChildScope(sc.Scope, parent); child != "" && !seen[child] {
			seen[child] = true
			out = append(out, child)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Server) runMemoryScopes(w http.ResponseWriter, r *http.Request) {
	parent, err := scopeParam(r, "parent", runCallOf(r).scope.CrewID)
	var scopes []model.MemoryScopeCount
	if err == nil {
		scopes, err = s.visibleScopes(r)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, memoryScopesResponse{Scopes: childScopes(scopes, parent)})
}

func (s *Server) runMemoryScopeInfo(w http.ResponseWriter, r *http.Request) {
	call := runCallOf(r)
	scope, err := scopeParam(r, "scope", call.scope.CrewID)
	if scope == "" {
		scope = "/"
	}
	f := store.MemoryFilter{CrewID: call.scope.CrewID, VisibleToRun: call.scope.RunID, ScopePrefix: scope}
	info := model.MemoryScopeInfo{Path: scope, Categories: []string{}}
	var cats map[string]int
	var scopes []model.MemoryScopeCount
	if err == nil {
		info.RecordCount, err = s.store.CountMemories(r.Context(), f)
	}
	if err == nil {
		info.OldestRecord, info.NewestRecord, err = s.store.MemoryAge(r.Context(), f)
	}
	if err == nil {
		cats, err = s.store.MemoryCategories(r.Context(), f)
	}
	if err == nil {
		scopes, err = s.visibleScopes(r)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	for c := range cats {
		info.Categories = append(info.Categories, c)
	}
	sort.Strings(info.Categories)
	info.ChildScopes = childScopes(scopes, scope)
	ok(w, info)
}

type memoryCategoriesResponse struct {
	Categories map[string]int `json:"categories"`
}

func (s *Server) runMemoryCategories(w http.ResponseWriter, r *http.Request) {
	call := runCallOf(r)
	scope, err := scopeParam(r, "scope_prefix", call.scope.CrewID)
	var cats map[string]int
	if err == nil {
		cats, err = s.store.MemoryCategories(r.Context(), store.MemoryFilter{CrewID: call.scope.CrewID, VisibleToRun: call.scope.RunID, ScopePrefix: scope})
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, memoryCategoriesResponse{Categories: cats})
}

type memoryCountResponse struct {
	Count int `json:"count"`
}

func (s *Server) runMemoryCount(w http.ResponseWriter, r *http.Request) {
	call := runCallOf(r)
	scope, err := scopeParam(r, "scope_prefix", call.scope.CrewID)
	var n int
	if err == nil {
		n, err = s.store.CountMemories(r.Context(), store.MemoryFilter{CrewID: call.scope.CrewID, VisibleToRun: call.scope.RunID, ScopePrefix: scope})
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, memoryCountResponse{Count: n})
}

type memoryDeleteRequest struct {
	memoryQuery
	RecordIDs []string   `json:"record_ids"`
	OlderThan *time.Time `json:"older_than"`
}

type memoryDeleteResponse struct {
	Deleted int `json:"deleted"`
}

// memoryIDs checks a list of memory ids.
func memoryIDs(p *memory.Problems, field string, ids []string) []string {
	if len(ids) > maxMemoryIDs {
		p.Add(field, "at most %d", maxMemoryIDs)
		return nil
	}
	for _, id := range ids {
		if !uuidPattern.MatchString(id) {
			p.Add(field, "%q is not a memory id", id)
			return nil
		}
	}
	return ids
}

func (s *Server) runMemoryDelete(w http.ResponseWriter, r *http.Request) {
	const tool = "memory_delete"
	var in memoryDeleteRequest
	if !decode(w, r, &in) {
		return
	}
	call := runCallOf(r)
	var p memory.Problems
	f := in.filter(&p, call.scope.CrewID)
	if in.RecordIDs != nil {
		f.IDs = memoryIDs(&p, "record_ids", in.RecordIDs)
	}
	if err := p.Err(); err != nil {
		s.journal(r, tool, "Memory: delete refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	// A run deletes only what it wrote, and never what a person pinned or (while its saves are reviewed) approved
	f.WrittenByRun, f.Unpinned, f.OlderThan = call.scope.RunID, true, in.OlderThan
	if runMemory(r).ReviewRunSaves {
		f.Status = model.MemoryPending
	}
	n, err := s.store.DeleteMemories(r.Context(), f, false)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.journal(r, tool, fmt.Sprintf("Memory: deleted %d of the run's memories", n), "")
	ok(w, memoryDeleteResponse{Deleted: n})
}

type memoryTouchRequest struct {
	RecordIDs []string `json:"record_ids"`
}

type memoryTouchResponse struct {
	Touched int `json:"touched"`
}

func (s *Server) runMemoryTouch(w http.ResponseWriter, r *http.Request) {
	var in memoryTouchRequest
	if !decode(w, r, &in) {
		return
	}
	var p memory.Problems
	ids := memoryIDs(&p, "record_ids", in.RecordIDs)
	if err := p.Err(); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	call := runCallOf(r)
	n := 0
	var err error
	if len(ids) > 0 {
		n, err = s.store.TouchMemories(r.Context(), store.MemoryFilter{CrewID: call.scope.CrewID, VisibleToRun: call.scope.RunID, IDs: ids})
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, memoryTouchResponse{Touched: n})
}
