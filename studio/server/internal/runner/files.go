package runner

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

const (
	// Files agents write are kept, within reason: a runaway agent must not fill the bucket
	maxArtifactFiles      = 200
	maxArtifactFileBytes  = 100 << 20
	maxArtifactTotalBytes = 500 << 20
	// The transcript and console log are bounded by the per-run output limit; this is a backstop
	maxTranscriptBytes  = 1 << 30
	fileTransferTimeout = 10 * time.Minute

	transcriptFile = "transcript.jsonl"
	consoleFile    = "console.log"
	inputsDir      = "attachments"
)

// runPaths are one run's folders: the workspace its agents may use, and a private folder agents cannot reach.
type runPaths struct {
	workspace  string
	private    string
	transcript string
	console    string
}

func (m *Manager) pathsFor(runID string) runPaths {
	private := filepath.Join(m.proc.DataDir, "runs", runID)
	return runPaths{
		workspace:  filepath.Join(m.proc.Workspace, "runs", runID),
		private:    private,
		transcript: filepath.Join(private, transcriptFile),
		console:    filepath.Join(private, consoleFile),
	}
}

// prepareFiles creates the run's folders and copies the run's input files (a work order's, or those sent with a
// chat message) into its workspace.
func (m *Manager) prepareFiles(ctx context.Context, runID string, spec model.RunSpec) (runPaths, error) {
	p := m.pathsFor(runID)
	for _, dir := range []string{p.workspace, p.private} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return p, fmt.Errorf("Could not create the run folder: %v", err)
		}
	}
	inputs := inputFiles(spec)
	if len(inputs) == 0 {
		return p, nil
	}
	if m.files == nil {
		return p, errors.New("This run has input files, but no file storage is configured.")
	}
	ctx, cancel := context.WithTimeout(ctx, fileTransferTimeout)
	defer cancel()
	for _, a := range inputs {
		if err := m.downloadInput(ctx, p.workspace, a); err != nil {
			return p, fmt.Errorf("Could not load the file %q: %v", a.Filename, err)
		}
	}
	return p, nil
}

func inputFiles(spec model.RunSpec) []model.SpecAttachment {
	var out []model.SpecAttachment
	if spec.WorkOrder != nil {
		out = append(out, spec.WorkOrder.Attachments...)
	}
	if spec.Chat != nil {
		out = append(out, spec.Chat.Files...)
	}
	return out
}

func (m *Manager) downloadInput(ctx context.Context, workspace string, a model.SpecAttachment) error {
	target := filepath.Join(workspace, filepath.FromSlash(a.Path))
	if !within(workspace, target) {
		return errors.New("unsafe path")
	}
	row, err := m.store.GetAttachment(ctx, a.ID)
	if err != nil {
		return err
	}
	body, err := m.files.Get(ctx, row.StorageKey)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, io.LimitReader(body, a.SizeBytes+1))
	closeErr := f.Close()
	return errors.Join(copyErr, closeErr)
}

func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// consoleWriter keeps every runner stderr line, uncut but with ANSI codes and provider keys removed.
type consoleWriter struct {
	f *os.File
	w *bufio.Writer
}

func openConsole(path string) *consoleWriter {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o640)
	if err != nil {
		return nil
	}
	return &consoleWriter{f: f, w: bufio.NewWriterSize(f, 64<<10)}
}

func (c *consoleWriter) line(text string) {
	if c == nil {
		return
	}
	_, _ = c.w.WriteString(ansiPattern.ReplaceAllString(redactSecrets(text), "") + "\n")
}

func (c *consoleWriter) close() {
	if c == nil {
		return
	}
	_ = c.w.Flush()
	_ = c.f.Close()
}

// saveFiles uploads what the agents wrote and the full transcript and console log, records an "artifacts"
// event, and removes the local folders (unless configured to keep them).
func (m *Manager) saveFiles(runID string, p runPaths, rec *recorder) {
	defer m.removeRunFolders(p)
	if m.files == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	defer cancel()

	var saved int
	var bytes int64
	var problems []string
	upload := func(path, name, kind string) {
		size, err := m.uploadFile(ctx, runID, path, name, kind)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			return
		}
		saved++
		bytes += size
	}

	for _, name := range []string{transcriptFile, consoleFile} {
		path := filepath.Join(p.private, name)
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			if info.Size() > maxTranscriptBytes {
				problems = append(problems, fmt.Sprintf("%s is over %d MB and was not kept", name, maxTranscriptBytes>>20))
				continue
			}
			upload(path, name, model.AttachmentTranscript)
		}
	}
	artifacts, skipped := collectArtifacts(p.workspace)
	for _, a := range artifacts {
		upload(a.path, a.name, model.AttachmentArtifact)
	}
	payload := map[string]any{"files": saved, "bytes": bytes}
	if skipped > 0 {
		problems = append(problems, fmt.Sprintf("%d files were not kept (over %d files, %d MB each or %d MB in total, or not regular files)",
			skipped, maxArtifactFiles, maxArtifactFileBytes>>20, maxArtifactTotalBytes>>20))
	}
	if len(problems) > 0 {
		payload["error"] = strings.Join(problems, "\n")
		m.log.Warn("some run files were not saved", "run_id", runID, "problems", len(problems))
	}
	rec.record(model.RunEvent{Type: "artifacts", CreatedAt: time.Now(), Payload: payload}, nil)
}

type artifactFile struct {
	path string
	name string
}

// collectArtifacts lists regular files the agents left in the workspace, skipping the input files.
func collectArtifacts(workspace string) ([]artifactFile, int) {
	var out []artifactFile
	var total int64
	skipped := 0
	_ = filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(workspace, path)
		if relErr != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			if rel == inputsDir {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		// Hard links could smuggle a file from elsewhere on the machine into the upload, so only files with
		// a single link (written by the agents themselves) are kept
		if err != nil || !info.Mode().IsRegular() || linkCount(info) != 1 {
			skipped++
			return nil
		}
		if len(out) >= maxArtifactFiles || info.Size() > maxArtifactFileBytes || total+info.Size() > maxArtifactTotalBytes {
			skipped++
			return nil
		}
		total += info.Size()
		out = append(out, artifactFile{path: path, name: filepath.ToSlash(rel)})
		return nil
	})
	return out, skipped
}

// contentTypeFor prefers the extension (so .md and .csv are recognised) and falls back to sniffing.
func contentTypeFor(name string, head []byte) string {
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); ct != "" {
		return ct
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".jsonl", ".log":
		return "text/plain; charset=utf-8"
	}
	return http.DetectContentType(head)
}

func (m *Manager) uploadFile(ctx context.Context, runID, path, name, kind string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return 0, err
	}
	head := make([]byte, 512)
	n, _ := f.ReadAt(head, 0)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	id := auth.NewUUID()
	key := "runs/" + runID + "/" + id
	contentType := contentTypeFor(name, head[:n])
	if err := m.files.Put(ctx, key, f, info.Size(), contentType); err != nil {
		return 0, err
	}
	_, err = m.store.AddAttachment(ctx, model.Attachment{
		ID: id, RunID: &runID, Kind: kind, Filename: name, ContentType: contentType, SizeBytes: info.Size(),
		SHA256: hex.EncodeToString(hash.Sum(nil)), StorageKey: key,
	})
	if err != nil {
		_ = m.files.Delete(ctx, key)
		return 0, err
	}
	return info.Size(), nil
}

func (m *Manager) removeRunFolders(p runPaths) {
	if m.proc.KeepRunFiles {
		return
	}
	for _, dir := range []string{p.workspace, p.private} {
		if err := os.RemoveAll(dir); err != nil {
			m.log.Warn("could not remove run folder", "dir", dir, "err", err)
		}
	}
}

func linkCount(info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
