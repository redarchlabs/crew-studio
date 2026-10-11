package api

import (
	"context"
	"encoding/json"
	"log/slog"

	"crewstudio/internal/duereport"
	"crewstudio/internal/reports"
)

// reportSnapshots gives delivered reports their data snapshot (internal/duereport), read through the tracking list
// like a watch reads it. Any failure, including a slow tracker, leaves the report without a snapshot and says why.
type reportSnapshots struct {
	builder *duereport.Builder
	log     *slog.Logger
}

func (r reportSnapshots) Snapshot(ctx context.Context, req reports.SnapshotRequest) (json.RawMessage, string) {
	raw, err := r.builder.Snapshot(ctx, duereport.Request{CrewID: req.CrewID, ScheduleID: req.ScheduleID, Project: req.Project,
		Cadence: req.Cadence, Location: req.Location, Now: req.Now})
	if err != nil {
		r.log.Info("report delivered without a data snapshot", "crew_id", req.CrewID, "project", req.Project, "err", err)
		return nil, duereport.Reason(err)
	}
	return raw, ""
}
