package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Saving documents to KM2 from a run. Everything a save must respect is enforced here in code, not by the prompt:
//   - dry run: nothing is saved; the run gets a "would save" answer and a dry-run action log entry
//   - every save is reserved in the action log as km2:save before anything is sent, and finished with KM2's outcome
//   - a run may save at most maxKM2SavesPerRun documents, and a connection at most maxKM2SavesPerConnectionDay a day
//   - only the connection's default folder and its listed folders can be written to
//   - the same document saved twice in one run is sent once
// The reservation takes a short per-run database lock; no lock is held while KM2 works.

const (
	maxKM2SavesPerRun = 10
	// maxKM2SavesPerConnectionDay keeps crews sharing a key well under KM2's own daily write cap (500 by default)
	maxKM2SavesPerConnectionDay = 200
	// km2SaveTimeout bounds a save and its log entry once started, independent of the caller
	km2SaveTimeout = 100 * time.Second
	km2LoggedTitle = 255
)

type km2WouldSave struct {
	Connection  string `json:"connection"`
	FolderID    string `json:"folder_id"`
	Title       string `json:"title"`
	ExternalRef string `json:"external_ref"`
	Bytes       int    `json:"bytes"`
}

// km2SaveResult answers km_save_document.
type km2SaveResult struct {
	Saved  bool `json:"saved"`
	DryRun bool `json:"dry_run"`
	// Repeated is true when this document was already saved (or would have been) in this run; nothing was sent
	Repeated         bool               `json:"repeated,omitempty"`
	Outcome          string             `json:"outcome,omitempty"`
	DocumentID       string             `json:"document_id,omitempty"`
	ProcessingStatus string             `json:"processing_status,omitempty"`
	ExternalRef      string             `json:"external_ref"`
	FolderID         string             `json:"folder_id"`
	WouldSave        *km2WouldSave      `json:"would_save,omitempty"`
	Logged           *model.ActionEntry `json:"logged"`
	Message          string             `json:"message"`
}

// km2Save is one save on its way: what the agent asked for, where it goes, and its log entry.
type km2Save struct {
	k       runKM2
	in      model.KM2Save
	folder  km2.Folder
	meta    map[string]any
	key     string
	details map[string]any
}

func (s *Server) runKM2Save(w http.ResponseWriter, r *http.Request) {
	const tool = "km_save_document"
	var in model.KM2Save
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeKM2Save(in)
	if err != nil {
		s.journal(r, tool, "KM2: save refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	call := runCallOf(r)
	key := km2SaveKey(call.scope.RunID, clean)
	if !s.withinWriteQuota(w, r, tool) || !s.km2SaveAllowed(w, r, key, clean) {
		return
	}
	meta := km2Metadata(call.scope, clean.Metadata)
	if problems := model.KM2MetadataProblems(meta); len(problems) > 0 {
		s.journal(r, tool, "KM2: save refused: "+problems[0], "")
		failErr(w, r, s.log, invalid(problems...))
		return
	}
	k, found := s.km2Of(w, r, tool)
	if !found {
		return
	}
	req := km2SaveRequest{scope: call.stateScope(), owner: store.RunKM2Owner(call.scope.RunID), perOwner: maxKM2SavesPerRun,
		k: k, in: clean, meta: meta, key: key, tool: tool}
	out, err := s.km2SaveDocument(r.Context(), req)
	if err != nil {
		what := "save to " + k.conn.Name
		if out.attempted {
			what = "save " + clean.ExternalRef + " to " + k.conn.Name
		}
		s.km2CallFailed(w, r, tool, what, err)
		return
	}
	s.km2SaveAnswer(w, r, req, out)
}

// km2SaveAllowed answers itself when the document was already saved in this run or the run reached its cap, before
// KM2 is called at all (ReserveKM2Save checks both again, under the lock).
func (s *Server) km2SaveAllowed(w http.ResponseWriter, r *http.Request, key string, in model.KM2Save) bool {
	call := runCallOf(r)
	prior, err := s.store.ActionByKey(r.Context(), call.scope.CrewID, key)
	if err == nil && prior != nil && !km2Pending(prior) {
		s.km2Repeated(w, r, in, prior)
		return false
	}
	var n int
	if err == nil {
		n, err = s.store.RunKM2Saves(r.Context(), call.scope.RunID)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return false
	}
	if n >= maxKM2SavesPerRun {
		s.km2CapReached(w, r, n)
		return false
	}
	return true
}

func km2Pending(e *model.ActionEntry) bool {
	o := stringOf(e.Details["outcome"])
	return o == store.KM2Sending || o == store.KM2Unknown
}

// km2Metadata is the agent's metadata with Studio's own fields, which the agent cannot override.
func km2Metadata(scope store.RunScope, extra map[string]any) map[string]any {
	out := maps.Clone(extra)
	if out == nil {
		out = map[string]any{}
	}
	out["source"], out["crew"], out["run_id"] = "crew-studio", scope.CrewName, scope.RunID
	if scope.ScheduleName != "" {
		out["schedule"] = scope.ScheduleName
	}
	return out
}

// km2SaveKey makes a save idempotent within a run: the same document (folder as named, ref, title, content and
// metadata) is sent once, so a tool call retried after a timeout does not save twice or count twice against the cap.
func km2SaveKey(runID string, in model.KM2Save) string {
	meta, _ := json.Marshal(in.Metadata)
	folder := strings.ToLower(in.Folder)
	sum := sha256.Sum256([]byte(folder + "\x00" + in.ExternalRef + "\x00" + in.Title + "\x00" + in.Content + "\x00" + string(meta)))
	return "km2:" + runID + ":" + hex.EncodeToString(sum[:16])
}

func km2SaveDetails(sv km2Save) map[string]any {
	sum := sha256.Sum256([]byte(sv.in.Content))
	d := map[string]any{"tool": "km_save_document", "connection": sv.k.conn.Name, "connection_id": sv.k.conn.ID,
		"folder_id": sv.folder.ID, "external_ref": sv.in.ExternalRef, "title": km2Clip(sv.in.Title, km2LoggedTitle),
		"bytes": len(sv.in.Content), "content_sha256": hex.EncodeToString(sum[:])}
	if sv.folder.Path != "" {
		d["folder"] = sv.folder.Path
	}
	if sv.in.SourceFile != "" {
		d["source_file"] = sv.in.SourceFile
	}
	return d
}

// km2SaveRequest is one save through the KM2 save path: a run's km_save_document, or a report delivered to KM2
// (deliver_report and its retries).
type km2SaveRequest struct {
	scope store.StateScope
	// owner is who the save counts against (a run, or a person promoting memories), with its cap
	owner    store.KM2Owner
	perOwner int
	k        runKM2
	in       model.KM2Save
	meta     map[string]any
	key      string
	tool     string
	// extra details for the action log entry (such as promoted_by for a person's save)
	extra map[string]any
}

type km2SaveStatus int

const (
	km2StatusSaved km2SaveStatus = iota
	km2StatusWouldSave
	km2StatusRepeated
	km2StatusInFlight
	km2StatusRunCap
	km2StatusDayCap
)

// km2SaveOutcome is how a save went. attempted is true once KM2 was asked to save (so a failure may be KM2's).
type km2SaveOutcome struct {
	status    km2SaveStatus
	folder    km2.Folder
	entry     *model.ActionEntry
	saved     km2.Saved
	saves     int
	attempted bool
}

// km2SaveDocument resolves the folder, reserves the save in the action log (with the per-run and per-connection caps),
// and sends it, or in a dry run only reserves a dry-run entry. Errors are the folder's or KM2's.
func (s *Server) km2SaveDocument(ctx context.Context, req km2SaveRequest) (km2SaveOutcome, error) {
	var out km2SaveOutcome
	folder, err := km2.ResolveFolder(ctx, req.k.conn, req.k.client, req.in.Folder)
	if err != nil {
		return out, err
	}
	out.folder = folder
	sv := km2Save{k: req.k, in: req.in, folder: folder, meta: req.meta, key: req.key}
	sv.details = km2SaveDetails(sv)
	sv.details["tool"] = req.tool
	maps.Copy(sv.details, req.extra)
	if req.scope.DryRun {
		sv.details["would_save"] = true
	}
	rec := model.LogRecord{Subject: model.KM2Subject(req.in.ExternalRef), Action: model.ActionKM2Save, Details: sv.details, IdempotencyKey: req.key}
	if _, err := model.NormalizeLogRecord(rec); err != nil {
		return out, err
	}
	res, err := s.store.ReserveKM2Save(ctx, req.scope, req.owner, rec,
		store.KM2Limits{PerOwner: req.perOwner, PerConnectionDay: maxKM2SavesPerConnectionDay, ConnectionID: req.k.conn.ID})
	switch {
	case err != nil:
		return out, err
	case res.Prior != nil && km2Pending(res.Prior):
		out.status, out.entry = km2StatusInFlight, res.Prior
	case res.Prior != nil:
		out.status, out.entry = km2StatusRepeated, res.Prior
	case res.CapReached:
		out.status, out.saves = km2StatusRunCap, res.Saves
	case res.DayCapReached:
		out.status = km2StatusDayCap
	case req.scope.DryRun:
		out.status, out.entry = km2StatusWouldSave, res.Entry
	default:
		return s.km2Send(ctx, sv, res.Entry, out)
	}
	return out, nil
}

// km2Send sends a reserved save and finishes its log entry: with KM2's outcome, removed when KM2 refused it, or
// "unknown" when KM2's answer never came (it still counts, and may be sent again).
func (s *Server) km2Send(ctx context.Context, sv km2Save, entry *model.ActionEntry, out km2SaveOutcome) (km2SaveOutcome, error) {
	// Once the document is sent it must be logged, even if the caller gave up waiting: a detached context with its own limit
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), km2SaveTimeout)
	defer cancel()
	out.attempted = true
	saved, err := sv.k.client.Save(ctx, km2.Document{FolderID: sv.folder.ID, Title: sv.in.Title, Content: sv.in.Content,
		ExternalRef: sv.in.ExternalRef, Metadata: sv.meta})
	if err != nil {
		s.km2SaveFailed(ctx, entry, sv, err)
		return out, err
	}
	done := maps.Clone(sv.details)
	done["outcome"], done["document_id"], done["processing_status"] = saved.Outcome, saved.ID, saved.ProcessingStatus
	out.status, out.saved, out.entry = km2StatusSaved, saved, s.finishKM2Save(ctx, entry, done)
	return out, nil
}

// km2SaveAnswer answers km_save_document from a save's outcome.
func (s *Server) km2SaveAnswer(w http.ResponseWriter, r *http.Request, req km2SaveRequest, out km2SaveOutcome) {
	const tool = "km_save_document"
	sv := req.in
	switch out.status {
	case km2StatusInFlight:
		s.journal(r, tool, "KM2: "+sv.ExternalRef+" is being saved by another call of this run", "")
		fail(w, http.StatusConflict, "km2_in_flight", "This document is being saved by another call right now; wait for it.")
	case km2StatusRepeated:
		s.km2Repeated(w, r, sv, out.entry)
	case km2StatusRunCap:
		s.km2CapReached(w, r, out.saves)
	case km2StatusDayCap:
		s.journal(r, tool, "KM2: save refused, "+req.k.conn.Name+" reached its daily save limit", "")
		fail(w, http.StatusTooManyRequests, "km2_day_cap", fmt.Sprintf("%s has saved %d documents to KM2 in the last 24 hours, the most "+
			"Crew Studio allows; try tomorrow or leave the document in your output.", req.k.conn.Name, maxKM2SavesPerConnectionDay))
	case km2StatusWouldSave:
		s.journal(r, tool, fmt.Sprintf("KM2: dry run, would save %s to %s (not sent)", sv.ExternalRef, req.k.conn.Name), "")
		ok(w, km2SaveResult{DryRun: true, ExternalRef: sv.ExternalRef, FolderID: out.folder.ID, Logged: out.entry,
			WouldSave: &km2WouldSave{Connection: req.k.conn.Name, FolderID: out.folder.ID, Title: sv.Title, ExternalRef: sv.ExternalRef, Bytes: len(sv.Content)},
			Message:   "Dry run: nothing was saved to KM2. Add this to the \"Would save\" section of your output."})
	default:
		s.journal(r, tool, fmt.Sprintf("KM2: saved %s to %s (%s)", sv.ExternalRef, req.k.conn.Name, out.saved.Outcome), "Title: "+km2Clip(sv.Title, 200))
		res := km2SaveResult{Saved: true, Outcome: out.saved.Outcome, DocumentID: out.saved.ID, ProcessingStatus: out.saved.ProcessingStatus,
			ExternalRef: sv.ExternalRef, FolderID: out.folder.ID, Logged: out.entry, Message: km2OutcomeText(out.saved.Outcome)}
		if out.saved.Outcome == "created" {
			created(w, res)
			return
		}
		ok(w, res)
	}
}

func (s *Server) km2CapReached(w http.ResponseWriter, r *http.Request, n int) {
	s.journal(r, "km_save_document", fmt.Sprintf("KM2: save refused, the run already saved %d documents", n), "")
	fail(w, http.StatusTooManyRequests, "km2_save_cap", fmt.Sprintf("This run has already saved %d documents to KM2, the most one run may. "+
		"Combine the rest into fewer documents, or leave them in your output.", n))
}

func (s *Server) km2Repeated(w http.ResponseWriter, r *http.Request, in model.KM2Save, prior *model.ActionEntry) {
	s.journal(r, "km_save_document", "KM2: "+in.ExternalRef+" was already saved in this run", "")
	ok(w, km2SaveResult{Saved: !prior.DryRun, DryRun: prior.DryRun, Repeated: true, ExternalRef: in.ExternalRef,
		FolderID: stringOf(prior.Details["folder_id"]), DocumentID: stringOf(prior.Details["document_id"]), Logged: prior,
		Outcome: stringOf(prior.Details["outcome"]), Message: "This document was already saved in this run; nothing new was sent."})
}

// km2SaveFailed drops the reservation of a save KM2 refused (nothing was written), or marks it unknown.
func (s *Server) km2SaveFailed(ctx context.Context, entry *model.ActionEntry, sv km2Save, err error) {
	if km2Refused(err) {
		if derr := s.store.DropKM2Save(ctx, entry.ID); derr != nil {
			s.log.Error("drop KM2 save", "entry", entry.ID, "err", derr)
		}
		return
	}
	failed := maps.Clone(sv.details)
	failed["outcome"] = store.KM2Unknown
	s.finishKM2Save(ctx, entry, failed)
}

// km2Refused reports whether KM2 answered that it wrote nothing (a 4xx other than a timeout).
func km2Refused(err error) bool {
	var fe *km2.FolderError
	var ke *km2.Error
	if errors.As(err, &fe) {
		return true
	}
	return errors.As(err, &ke) && ke.Status >= 400 && ke.Status < 500 && ke.Status != http.StatusRequestTimeout
}

// finishKM2Save stores a save's final details, trying twice. A failure is logged; the save itself stands.
func (s *Server) finishKM2Save(ctx context.Context, entry *model.ActionEntry, details map[string]any) *model.ActionEntry {
	var out model.ActionEntry
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if out, err = s.store.FinishKM2Save(ctx, entry.ID, details); err == nil {
			return &out
		}
	}
	s.log.Error("record KM2 save", "entry", entry.ID, "err", err)
	return entry
}

func km2OutcomeText(outcome string) string {
	switch outcome {
	case "created":
		return "Saved a new document to KM2. KM2 now extracts its knowledge in the background."
	case "updated":
		return "Saved a new version of the document with this external_ref. KM2 re-reads it in the background."
	case "unchanged":
		return "KM2 already had exactly this document; nothing changed."
	case "metadata_updated":
		return "The content was unchanged; KM2 updated its title and metadata."
	case "reprocessed":
		return "The content was unchanged; KM2 is reading it again because its last attempt did not finish."
	}
	return "Saved to KM2."
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func km2Clip(text string, n int) string {
	if r := []rune(text); len(r) > n {
		return string(r[:n]) + "…"
	}
	return text
}
