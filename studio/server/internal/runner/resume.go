package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// resumeWorkspace starts a resumed run where the earlier one left off: the files that run's agents wrote are put back
// in the new workspace (later tasks may read them), and a run_resumed event records what was reused. What could not
// be restored is said in the event, by file name only; the run still goes ahead.
func (m *Manager) resumeWorkspace(ctx context.Context, spec model.RunSpec, workspace string, rec *recorder) {
	res := spec.Resume
	names := make([]string, 0, len(res.Outputs))
	for _, o := range res.Outputs {
		names = append(names, o.TaskName)
	}
	restored, notes := m.restoreArtifacts(ctx, rec.runID, res.FromRunID, workspace)
	payload := map[string]any{"from_run_id": res.FromRunID, "start_position": res.StartPosition,
		"reused_tasks": names, "restored_files": restored}
	if len(notes) > 0 {
		payload["error"] = strings.Join(notes, " ")
	}
	rec.record(model.RunEvent{Type: "run_resumed", CreatedAt: time.Now(), Payload: payload}, nil)
}

// restoreArtifacts copies the earlier run's files into the workspace, within the limits a run's own files have. It
// returns how many it restored and notes for the people reading the run (details go to the service log).
func (m *Manager) restoreArtifacts(ctx context.Context, runID, fromRunID, workspace string) (int, []string) {
	if m.files == nil {
		return 0, nil // without file storage the earlier run kept no files either
	}
	if _, err := m.store.GetRun(ctx, fromRunID); errors.Is(err, store.ErrNotFound) {
		return 0, []string{"The earlier run was deleted, so the files its agents wrote could not be restored."}
	}
	var notes []string
	if saved, err := m.store.LastEventOfType(ctx, fromRunID, "artifacts"); err == nil && saved != nil && str(saved.Payload, "error") != "" {
		notes = append(notes, "The earlier run could not keep all the files its agents wrote, so some are missing.")
	}
	artifacts, err := m.store.ListAttachments(ctx, store.AttachmentFilter{RunID: fromRunID, Kind: model.AttachmentArtifact})
	if err != nil {
		m.log.Error("list files to restore", "run_id", runID, "from_run_id", fromRunID, "err", err)
		return 0, append(notes, "The earlier run's files could not be listed, so none were restored.")
	}
	ctx, cancel := context.WithTimeout(ctx, fileTransferTimeout)
	defer cancel()
	restored := 0
	var total int64
	var failed, skipped []string
	for _, a := range artifacts {
		rel := path.Clean(a.Filename)
		if rel == "." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) || rel == inputsDir || strings.HasPrefix(rel, inputsDir+"/") {
			continue
		}
		if restored >= maxArtifactFiles || a.SizeBytes > maxArtifactFileBytes || total+a.SizeBytes > maxArtifactTotalBytes {
			skipped = append(skipped, a.Filename)
			continue
		}
		if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(rel))); err == nil {
			continue // the run's own input file has that name
		}
		spec := model.SpecAttachment{ID: a.ID, Filename: a.Filename, ContentType: a.ContentType, SizeBytes: a.SizeBytes, Path: rel}
		if err := m.downloadInput(ctx, workspace, spec); err != nil {
			m.log.Error("restore file", "run_id", runID, "from_run_id", fromRunID, "file", a.Filename, "err", err)
			failed = append(failed, a.Filename)
			continue
		}
		restored++
		total += a.SizeBytes
	}
	if len(skipped) > 0 {
		notes = append(notes, fmt.Sprintf("%d files were over the limits for a run's files and were not restored: %s.",
			len(skipped), someNames(skipped)))
	}
	if len(failed) > 0 {
		notes = append(notes, fmt.Sprintf("%d files could not be restored: %s.", len(failed), someNames(failed)))
	}
	return restored, notes
}

// maxNamedFiles is how many file names a note lists before saying how many more there are
const maxNamedFiles = 10

func someNames(names []string) string {
	if len(names) <= maxNamedFiles {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:maxNamedFiles], ", "), len(names)-maxNamedFiles)
}
