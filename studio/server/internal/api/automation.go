package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/scheduler"
	"crewstudio/internal/store"
)

const (
	maxHookBodyBytes    = 1 << 20
	maxPayloadInBrief   = 50_000
	schedulePreviewRuns = 5
)

func (s *Server) scheduleRoutes(r chi.Router) {
	r.Route("/schedules", func(r chi.Router) {
		manage := s.need(auth.SchedulesManage, auth.WorkOrdersCreate)
		r.With(s.need(auth.SchedulesView)).Get("/", s.listSchedules)
		r.With(manage).Post("/", s.saveSchedule)
		r.With(s.needAny(auth.SchedulesView, auth.SchedulesManage)).Post("/preview", s.previewSchedule)
		r.With(s.need(auth.SchedulesView)).Get("/{id}", s.getSchedule)
		r.With(manage).Put("/{id}", s.saveSchedule)
		r.With(s.need(auth.SchedulesManage)).Delete("/{id}", s.deleteSchedule)
		r.With(manage).Post("/{id}/run-now", s.runScheduleNow)
		s.watchRoutes(r)
		s.reportDestinationRoutes(r)
	})
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSchedules(r.Context(), scopeOf(principal(r)))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, items)
}

func (s *Server) loadSchedule(w http.ResponseWriter, r *http.Request) (model.Schedule, bool) {
	id, valid := pathID(w, r)
	if !valid {
		return model.Schedule{}, false
	}
	sc, err := s.store.GetSchedule(r.Context(), id)
	if err == nil && !principal(r).SeesAreas(sc.AreaIDs) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return sc, false
	}
	return sc, true
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	if sc, found := s.loadSchedule(w, r); found {
		ok(w, sc)
	}
}

// saveSchedule creates (POST) or updates (PUT) a schedule. Whoever saves it becomes the person it runs as.
func (s *Server) saveSchedule(w http.ResponseWriter, r *http.Request) {
	id := ""
	var old model.Schedule
	if chi.URLParam(r, "id") != "" {
		var found bool
		if old, found = s.loadSchedule(w, r); !found {
			return
		}
		id = old.ID
	}
	var in model.Schedule
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeSchedule(in)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	p := principal(r)
	if _, err := s.launcher.ResolveTarget(r.Context(), p, clean.CrewID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	var next *time.Time
	next, err = scheduler.Next(clean.Cron, clean.Timezone, time.Now())
	if err != nil {
		failErr(w, r, s.log, invalid(err.Error()))
		return
	}
	switch {
	case !clean.Enabled:
		next = nil
	case id != "" && old.Enabled && old.NextRunAt != nil && old.Cron == clean.Cron && old.Timezone == clean.Timezone:
		// Editing anything else keeps the upcoming run where it was
		next = old.NextRunAt
	}
	out, err := s.store.SaveSchedule(r.Context(), id, clean, next)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.scheduler.Notify()
	if id == "" {
		created(w, out)
		return
	}
	ok(w, out)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	if err := s.store.DeleteSchedule(r.Context(), sc.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

// runScheduleNow creates one work order from the schedule straight away, as the person pressing the button.
func (s *Server) runScheduleNow(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	p := principal(r)
	if !s.runStarts.allow(p.UserID) {
		tooMany(w, "Work orders are being submitted too quickly; wait a moment.")
		return
	}
	wo, err := s.scheduler.Launch(r.Context(), p, sc, time.Now())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	created(w, wo)
}

type previewRequest struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}

func (s *Server) previewSchedule(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if !decode(w, r, &req) {
		return
	}
	tz := strings.TrimSpace(req.Timezone)
	if tz == "" {
		tz = "UTC"
	}
	if len(req.Cron) > 120 || len(tz) > 64 {
		failErr(w, r, s.log, invalid("cron: too long"))
		return
	}
	times, err := scheduler.NextTimes(strings.Join(strings.Fields(req.Cron), " "), tz, time.Now(), schedulePreviewRuns)
	if err != nil {
		failErr(w, r, s.log, invalid(err.Error()))
		return
	}
	out := make([]string, len(times))
	for i, t := range times {
		out[i] = t.Format(time.RFC3339)
	}
	ok(w, map[string][]string{"next": out})
}

// Triggers

func (s *Server) triggerRoutes(r chi.Router) {
	r.Route("/triggers", func(r chi.Router) {
		manage := s.need(auth.TriggersManage, auth.WorkOrdersCreate)
		r.With(s.need(auth.TriggersView)).Get("/", s.listTriggers)
		r.With(manage).Post("/", s.saveTrigger)
		r.With(s.need(auth.TriggersView)).Get("/{id}", s.getTrigger)
		r.With(manage).Put("/{id}", s.saveTrigger)
		r.With(s.need(auth.TriggersManage)).Delete("/{id}", s.deleteTrigger)
		r.With(manage).Post("/{id}/rotate", s.rotateTrigger)
	})
}

// forViewer hides the signing secret from people who cannot manage triggers.
func forViewer(p *auth.Principal, t model.Trigger) model.Trigger {
	if !p.Can(auth.TriggersManage) {
		t.SigningSecret = ""
	}
	return t
}

func (s *Server) listTriggers(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	items, err := s.store.ListTriggers(r.Context(), scopeOf(p))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	for i := range items {
		items[i] = forViewer(p, items[i])
	}
	ok(w, items)
}

func (s *Server) loadTrigger(w http.ResponseWriter, r *http.Request) (model.Trigger, bool) {
	id, valid := pathID(w, r)
	if !valid {
		return model.Trigger{}, false
	}
	t, err := s.store.GetTrigger(r.Context(), id)
	if err == nil && !principal(r).SeesAreas(t.AreaIDs) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return t, false
	}
	return t, true
}

func (s *Server) getTrigger(w http.ResponseWriter, r *http.Request) {
	if t, found := s.loadTrigger(w, r); found {
		ok(w, forViewer(principal(r), t))
	}
}

func (s *Server) saveTrigger(w http.ResponseWriter, r *http.Request) {
	id := ""
	if chi.URLParam(r, "id") != "" {
		old, found := s.loadTrigger(w, r)
		if !found {
			return
		}
		id = old.ID
	}
	var in model.Trigger
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeTrigger(in)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if _, err := s.launcher.ResolveTarget(r.Context(), principal(r), clean.CrewID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	out, err := s.store.SaveTrigger(r.Context(), id, clean)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if id == "" {
		created(w, out)
		return
	}
	ok(w, out)
}

func (s *Server) rotateTrigger(w http.ResponseWriter, r *http.Request) {
	t, found := s.loadTrigger(w, r)
	if !found {
		return
	}
	out, err := s.store.RotateTriggerSecrets(r.Context(), t.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

func (s *Server) deleteTrigger(w http.ResponseWriter, r *http.Request) {
	t, found := s.loadTrigger(w, r)
	if !found {
		return
	}
	if err := s.store.DeleteTrigger(r.Context(), t.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

// Webhook calls

type hookAccepted struct {
	WorkOrderID string `json:"work_order_id"`
	Number      int64  `json:"number"`
	RunID       string `json:"run_id"`
	Status      string `json:"status"`
	StatusURL   string `json:"status_url"`
}

func hookToken(r *http.Request) string {
	if token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found {
		return strings.TrimSpace(token)
	}
	return strings.TrimSpace(r.Header.Get("X-CrewStudio-Token"))
}

// fireTrigger is called by other systems. It authenticates with the trigger's own token, maps the JSON
// payload to inputs, and creates and submits a work order as the person who set the trigger up.
func (s *Server) fireTrigger(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	trigger, err := s.store.TriggerByToken(r.Context(), id, hookToken(r))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusUnauthorized, "unauthenticated", "Missing or wrong trigger token.")
		return
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if !s.hooks.allow(trigger.ID) {
		tooMany(w, "This trigger is being called too often; slow down.")
		return
	}
	if !trigger.Enabled {
		fail(w, http.StatusConflict, "disabled", "This trigger is turned off.")
		return
	}
	if active, err := s.store.ActiveWorkOrders(r.Context(), model.SourceWebhook, trigger.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	} else if active >= maxActivePerTrigger {
		tooMany(w, fmt.Sprintf("This trigger already has %d work orders queued or running; try again when they finish.", active))
		return
	}
	key, valid := idempotencyKey(w, r)
	if !valid {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHookBodyBytes))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("The payload is over %d bytes.", maxHookBodyBytes))
		return
	}
	var payload any
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			fail(w, http.StatusBadRequest, "bad_json", "The payload is not valid JSON.")
			return
		}
	}
	p, err := s.launcher.RunAs(r.Context(), trigger.RunAs)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ctx := store.WithActor(r.Context(), store.Actor{ID: p.UserID, Name: p.Name + " (trigger " + trigger.Name + ")"})
	var callback *launch.CallbackRequest
	if trigger.CallbackURL != nil {
		callback = &launch.CallbackRequest{URL: *trigger.CallbackURL, Source: store.CallbackFromTrigger, Owner: trigger.ID}
	}
	in := model.WorkOrderInput{
		Title: trigger.Name + " · " + time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		Brief: hookBrief(trigger, raw), CrewID: trigger.CrewID,
		Inputs: MapInputs(trigger.InputMap, payload), BudgetUSD: trigger.BudgetUSD, Submit: true,
	}
	clean, err := model.NormalizeWorkOrder(in)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	in = clean
	// Retries are matched per trigger, not per person it runs as
	idem := launch.Idempotency{Scope: "trigger:" + trigger.ID, Key: key}
	result, err := s.launcher.CreateWorkOrder(ctx, p, in, launch.WorkOrderSource{Kind: model.SourceWebhook, ID: trigger.ID}, idem, callback)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	wo, run := result.WorkOrder, result.Run
	if !result.Replayed {
		if rerr := s.store.RecordTriggerFired(r.Context(), trigger.ID); rerr != nil {
			s.log.Warn("record trigger fired", "trigger_id", trigger.ID, "err", rerr)
		}
	}
	accepted := hookAccepted{WorkOrderID: wo.ID, Number: wo.Number, Status: wo.Status, StatusURL: "/api/work-orders/" + wo.ID}
	if run != nil {
		accepted.RunID = run.ID
		accepted.Status = string(run.Status)
	}
	writeJSON(w, http.StatusAccepted, envelope{Success: true, Data: accepted})
}

func hookBrief(t model.Trigger, raw []byte) string {
	brief := t.Brief
	if !t.IncludePayload || len(raw) == 0 {
		return brief
	}
	var pretty []byte
	var v any
	if json.Unmarshal(raw, &v) == nil {
		pretty, _ = json.MarshalIndent(v, "", "  ")
	}
	text := cleanText(string(pretty))
	if len(text) > maxPayloadInBrief {
		text = strings.ToValidUTF8(text[:maxPayloadInBrief], "") + "\n… [payload cut]"
	}
	if brief != "" {
		brief += "\n\n"
	}
	return brief + "## Payload\n\n```json\n" + text + "\n```"
}

// MapInputs reads each input from a dotted path in the payload ("customer.name", "items.0.sku").
// Strings are used as they are; numbers and booleans are formatted; objects and lists become JSON.
func MapInputs(inputMap map[string]string, payload any) map[string]string {
	out := map[string]string{}
	for key, p := range inputMap {
		v, found := lookup(payload, p)
		if !found || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			out[key] = cleanText(t)
		case float64:
			out[key] = strconv.FormatFloat(t, 'f', -1, 64)
		case bool:
			out[key] = strconv.FormatBool(t)
		default:
			data, _ := json.Marshal(t)
			out[key] = cleanText(string(data))
		}
	}
	return out
}

// cleanText removes NUL characters (Postgres rejects them) and invalid UTF-8.
func cleanText(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "")
}

func lookup(v any, dotted string) (any, bool) {
	for _, part := range strings.Split(dotted, ".") {
		switch node := v.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, false
			}
			v = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			v = node[i]
		default:
			return nil, false
		}
	}
	return v, true
}
