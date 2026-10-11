package api

import (
	"context"
	"fmt"

	"crewstudio/internal/model"
	"crewstudio/internal/reports"
	"crewstudio/internal/store"
)

// SaveReport saves a delivered report to KM2 through the same path as km_save_document (km2SaveDocument): the
// connection's allowed folder, a km2:save action log reservation with the per-run and per-connection caps, then KM2's
// document endpoint. As for km_save_document, the crew must have exactly one KM2 connection, so a run always acts as
// one KM2 identity. It implements reports.KM2.
func (s *Server) SaveReport(ctx context.Context, req reports.KM2Request) (reports.KM2Saved, error) {
	if err := s.onlyKM2Connection(ctx, req.CrewID); err != nil {
		return reports.KM2Saved{}, err
	}
	if !s.km2.Allow(req.ConnectionID) {
		return reports.KM2Saved{}, &reports.SafeError{Message: "Too many KM2 calls through this connection just now; retry in a minute."}
	}
	conn, client, err := s.km2.ForCrew(ctx, req.ConnectionID, req.CrewID)
	if err != nil {
		return reports.KM2Saved{}, err
	}
	in, err := model.NormalizeKM2Save(model.KM2Save{Title: req.Title, Content: req.Content, ExternalRef: req.ExternalRef, Folder: req.Folder,
		Metadata: req.Metadata})
	if err != nil {
		return reports.KM2Saved{}, &reports.SafeError{Message: "The report cannot be saved to KM2: " + problemText(err) + "."}
	}
	meta := km2Metadata(store.RunScope{CrewName: req.CrewName, RunID: req.RunID, ScheduleName: req.ScheduleName}, in.Metadata)
	out, err := s.km2SaveDocument(ctx, km2SaveRequest{scope: store.StateScope{CrewID: req.CrewID, ScheduleID: req.ScheduleID, DryRun: req.DryRun},
		owner: store.RunKM2Owner(req.RunID), perOwner: maxKM2SavesPerRun, k: runKM2{conn: conn, client: client}, in: in, meta: meta, key: km2SaveKey(req.RunID, in), tool: "deliver_report"})
	if err != nil {
		return reports.KM2Saved{}, err
	}
	switch out.status {
	case km2StatusSaved:
		return reports.KM2Saved{DocumentID: out.saved.ID, Outcome: out.saved.Outcome}, nil
	case km2StatusWouldSave:
		return reports.KM2Saved{Outcome: "would_save"}, nil
	case km2StatusRepeated:
		return reports.KM2Saved{DocumentID: stringOf(out.entry.Details["document_id"]), Outcome: "already_saved"}, nil
	case km2StatusInFlight:
		return reports.KM2Saved{}, &reports.SafeError{Message: "This report is being saved to KM2 by another call right now; retry later."}
	case km2StatusRunCap:
		return reports.KM2Saved{}, &reports.SafeError{Message: fmt.Sprintf("The run already saved %d documents to KM2, the most one run may.", out.saves)}
	}
	return reports.KM2Saved{}, &reports.SafeError{Message: fmt.Sprintf("%s has saved %d documents to KM2 in the last 24 hours, the most Crew Studio allows; retry tomorrow.",
		conn.Name, maxKM2SavesPerConnectionDay)}
}

// onlyKM2Connection refuses a save when the crew may use more than one enabled KM2 connection (as km2Of does for runs).
func (s *Server) onlyKM2Connection(ctx context.Context, crewID string) error {
	conns, err := s.store.CrewConnections(ctx, crewID, store.AllAreas)
	if err != nil {
		return err
	}
	n := 0
	for _, c := range conns {
		if c.Enabled && c.Kind == model.KindKM2 {
			n++
		}
	}
	if n > 1 {
		return &reports.SafeError{Message: "The crew may use more than one KM2 connection, so Crew Studio cannot tell which KM2 identity to act as; " +
			"an administrator must leave it only one."}
	}
	return nil
}
