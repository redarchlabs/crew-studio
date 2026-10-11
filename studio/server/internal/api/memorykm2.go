package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Promoting memories to KM2: a person turns one or more of a crew's memories into a KM2 document through the crew's
// one KM2 connection, which must be in their areas. It goes through the same save path as km_save_document
// (km2SaveDocument): the connection's folders, a km2:save action log reservation (with no run), the per-connection
// daily cap, and a cap per person per day. Runs never promote memories themselves.

const (
	maxPromoteMemories        = 50
	maxPromotionsPerPersonDay = 20
)

type memoryPromoteRequest struct {
	MemoryIDs   []string `json:"memory_ids"`
	Title       string   `json:"title"`
	Folder      string   `json:"folder"`
	ExternalRef string   `json:"external_ref"`
}

type memoryPromoteResponse struct {
	Outcome     string `json:"outcome"`
	DocumentID  string `json:"document_id"`
	ExternalRef string `json:"external_ref"`
	FolderID    string `json:"folder_id"`
}

func (s *Server) promoteMemories(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	var in memoryPromoteRequest
	if !decode(w, r, &in) {
		return
	}
	ids, mems, err := s.promotedMemories(r, crew.ID, in.MemoryIDs)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ref := strings.TrimSpace(in.ExternalRef)
	if ref == "" {
		sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
		ref = "crew-memory-" + crew.ID[:8] + "-" + hex.EncodeToString(sum[:6])
	}
	who := principal(r)
	meta := map[string]any{"source": "crew-studio", "crew": crew.Name, "kind": "crew-memory", "promoted_by": who.Name, "memory_ids": anyList(ids)}
	doc, err := model.NormalizeKM2Save(model.KM2Save{Title: in.Title, Content: promotedDocument(in.Title, crew, mems), ExternalRef: ref,
		Folder: in.Folder, Metadata: meta})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	k, found := s.promotionKM2(w, r, crew)
	if !found {
		return
	}
	req := km2SaveRequest{scope: store.StateScope{CrewID: crew.ID}, owner: store.PersonKM2Owner(who.UserID), perOwner: maxPromotionsPerPersonDay,
		k: k, in: doc, meta: doc.Metadata, key: km2SaveKey("promote:"+who.UserID, doc), tool: "memory_promote",
		extra: map[string]any{"promoted_by": who.UserID, "memories": len(ids)}}
	out, err := s.km2SaveDocument(r.Context(), req)
	if err != nil {
		s.km2Failure(w, r, err)
		return
	}
	res := memoryPromoteResponse{ExternalRef: doc.ExternalRef, FolderID: out.folder.ID}
	switch out.status {
	case km2StatusInFlight:
		fail(w, http.StatusConflict, "km2_in_flight", "These memories are being saved to KM2 right now; wait for it.")
		return
	case km2StatusRunCap:
		fail(w, http.StatusTooManyRequests, "km2_promote_cap", fmt.Sprintf("You have promoted %d documents to KM2 in the last 24 hours, the most one person may.", out.saves))
		return
	case km2StatusDayCap:
		fail(w, http.StatusTooManyRequests, "km2_day_cap", fmt.Sprintf("%s has saved %d documents to KM2 in the last 24 hours, the most Crew Studio allows; try tomorrow.",
			k.conn.Name, maxKM2SavesPerConnectionDay))
		return
	case km2StatusRepeated:
		res.Outcome, res.DocumentID = "already_saved", stringOf(out.entry.Details["document_id"])
	default:
		res.Outcome, res.DocumentID = out.saved.Outcome, out.saved.ID
	}
	if res.DocumentID != "" {
		if err := s.store.SetMemoriesKM2Document(r.Context(), crew.ID, ids, res.DocumentID); err != nil {
			failErr(w, r, s.log, err)
			return
		}
	}
	s.audit(r, "memories_promoted", who.UserID, who.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name, "memory_ids": ids,
		"connection_id": k.conn.ID, "document_id": res.DocumentID, "external_ref": res.ExternalRef, "outcome": res.Outcome})
	ok(w, res)
}

// promotedMemories checks memory_ids and loads them, in the order given.
func (s *Server) promotedMemories(r *http.Request, crewID string, raw []string) ([]string, []model.MemoryRecord, error) {
	if len(raw) == 0 || len(raw) > maxPromoteMemories {
		return nil, nil, invalid(fmt.Sprintf("memory_ids: pick 1 to %d memories", maxPromoteMemories))
	}
	var ids []string
	for _, id := range raw {
		if !uuidPattern.MatchString(id) {
			return nil, nil, invalid(fmt.Sprintf("memory_ids: %q is not a memory id", id))
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	mems, _, err := s.store.ListMemories(r.Context(), store.MemoryFilter{CrewID: crewID, IDs: ids}, maxPromoteMemories, 0)
	if err != nil {
		return nil, nil, err
	}
	if len(mems) != len(ids) {
		return nil, nil, invalid("memory_ids: some of these memories are not in this crew (deleted since?)")
	}
	// A pending memory is what a run saved and nobody vouched for yet; it must not reach shared knowledge first
	if slices.ContainsFunc(mems, func(m model.MemoryRecord) bool { return m.Status == model.MemoryPending }) {
		return nil, nil, invalid("memory_ids: approve these memories first")
	}
	slices.SortFunc(mems, func(a, b model.MemoryRecord) int { return slices.Index(ids, a.ID) - slices.Index(ids, b.ID) })
	return ids, mems, nil
}

// promotedDocument is the KM2 document made from memories: one bullet each, with where it came from.
func promotedDocument(title string, crew model.Crew, mems []model.MemoryRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nLessons the crew %q learned, promoted from its memory in Crew Studio.\n\n", strings.TrimSpace(title), crew.Name)
	for _, m := range mems {
		fmt.Fprintf(&b, "- %s\n  (scope %s; saved %s by %s", m.Content, m.Scope, m.CreatedAt.UTC().Format("2006-01-02"), m.WrittenBy)
		if m.TaskName != "" {
			fmt.Fprintf(&b, ", task %q", m.TaskName)
		}
		b.WriteString(")\n")
	}
	return b.String()
}

// promotionKM2 picks the crew's one enabled KM2 connection, which must be in the person's areas. It answers itself
// on failure.
func (s *Server) promotionKM2(w http.ResponseWriter, r *http.Request, crew model.Crew) (runKM2, bool) {
	c, found := s.promotionConnection(w, r, crew)
	if !found {
		return runKM2{}, false
	}
	if !s.km2.Allow(c.ID) {
		tooMany(w, "Too many KM2 calls through "+c.Name+" just now; try again in a minute.")
		return runKM2{}, false
	}
	conn, client, err := s.km2.ForCrew(r.Context(), c.ID, crew.ID)
	if err != nil {
		s.km2Failure(w, r, err)
		return runKM2{}, false
	}
	return runKM2{conn: conn, client: client}, true
}

// promotionConnection is the one enabled KM2 connection the crew may use, in the person's areas; it answers itself
// when there is none or more than one.
func (s *Server) promotionConnection(w http.ResponseWriter, r *http.Request, crew model.Crew) (model.Connection, bool) {
	conns, err := s.store.CrewConnections(r.Context(), crew.ID, store.AllAreas)
	if err != nil {
		failErr(w, r, s.log, err)
		return model.Connection{}, false
	}
	var km2s []model.Connection
	for _, c := range conns {
		if c.Enabled && c.Kind == model.KindKM2 {
			km2s = append(km2s, c)
		}
	}
	switch {
	case len(km2s) == 0 || (len(km2s) == 1 && !principal(r).SeesAreas(km2s[0].AreaIDs)):
		fail(w, http.StatusConflict, "no_km2", "This crew has no KM2 connection that you can use. Ask an administrator to give it one in your areas.")
		return model.Connection{}, false
	case len(km2s) > 1:
		fail(w, http.StatusConflict, "km2_ambiguous", "This crew may use more than one KM2 connection, so Crew Studio cannot tell which to save to. "+
			"Ask an administrator to leave it only one.")
		return model.Connection{}, false
	}
	return km2s[0], true
}

func anyList(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
