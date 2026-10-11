package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
)

const (
	maxAttachmentBytes      = 25 << 20
	maxAttachmentsPerOrder  = 20
	maxAttachmentTotalBytes = 100 << 20
	uploadField             = "file"
)

// Allowed input files, decided from the content (sniffed) and, for text and Office formats, the extension.
var (
	imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}
	textTypes  = map[string]string{
		".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8", ".markdown": "text/markdown; charset=utf-8",
		".csv": "text/csv; charset=utf-8", ".json": "application/json", ".xml": "application/xml",
		".yaml": "application/x-yaml", ".yml": "application/x-yaml", ".html": "text/html; charset=utf-8",
		".htm": "text/html; charset=utf-8", ".log": "text/plain; charset=utf-8", ".tsv": "text/tab-separated-values; charset=utf-8",
	}
	officeTypes = map[string]string{
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
)

// classify returns the content type to store for an upload, or "" when the file is not allowed.
func classify(filename string, head []byte) string {
	sniffed := http.DetectContentType(head)
	base, _, _ := mime.ParseMediaType(sniffed)
	ext := strings.ToLower(path.Ext(filename))
	switch {
	case imageTypes[base]:
		return base
	case base == "application/pdf":
		return base
	case base == "application/zip" && officeTypes[ext] != "":
		return officeTypes[ext]
	case strings.HasPrefix(base, "text/") && textTypes[ext] != "":
		return textTypes[ext]
	case strings.HasPrefix(base, "text/") && ext == "":
		return "text/plain; charset=utf-8"
	}
	return ""
}

// uploadAttachments stores one or more multipart "file" parts as a work order's input files.
func (s *Server) uploadAttachments(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		fail(w, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured on this server.")
		return
	}
	p := principal(r)
	if !p.Can(auth.WorkOrdersCreate) {
		forbidden(w, auth.WorkOrdersCreate)
		return
	}
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	if err := s.store.EnsureWorkOrderIdle(r.Context(), wo.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	count, total, err := s.store.InputAttachmentTotals(r.Context(), wo.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentTotalBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_upload", "Send the files as multipart/form-data.")
		return
	}
	out := []model.Attachment{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.uploadFailed(w, r, err, out)
			return
		}
		if part.FormName() != uploadField || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		if count >= maxAttachmentsPerOrder {
			_ = part.Close()
			fail(w, http.StatusUnprocessableEntity, "validation_failed", fmt.Sprintf("A work order can have at most %d files.", maxAttachmentsPerOrder))
			return
		}
		a, err := s.storeUpload(r, wo.ID, part.FileName(), part, maxAttachmentTotalBytes-total)
		_ = part.Close()
		if err != nil {
			s.uploadFailed(w, r, err, out)
			return
		}
		count++
		total += a.SizeBytes
		out = append(out, a)
	}
	if len(out) == 0 {
		fail(w, http.StatusBadRequest, "bad_upload", `No files were sent; use the form field "file".`)
		return
	}
	created(w, out)
}

type uploadError struct {
	status int
	code   string
	msg    string
}

func (e *uploadError) Error() string { return e.msg }

func (s *Server) uploadFailed(w http.ResponseWriter, r *http.Request, err error, saved []model.Attachment) {
	var tooBig *http.MaxBytesError
	var ue *uploadError
	note := ""
	if len(saved) > 0 {
		note = fmt.Sprintf(" (%d earlier files in this upload were saved)", len(saved))
	}
	switch {
	case errors.As(err, &ue):
		fail(w, ue.status, ue.code, ue.msg+note)
	case errors.As(err, &tooBig):
		fail(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("A work order's files may total at most %d MB.", maxAttachmentTotalBytes>>20)+note)
	default:
		failErr(w, r, s.log, err)
	}
}

// errUploadTooLarge means a file was over the limit spoolUpload was given.
var errUploadTooLarge = errors.New("upload too large")

// spooled is an upload copied to a temporary file, with its size, hash and allowed content type ("" when the type
// is not allowed). Close removes the file.
type spooled struct {
	file        *os.File
	size        int64
	sha256      string
	contentType string
}

func (sp *spooled) Close() {
	_ = sp.file.Close()
	_ = os.Remove(sp.file.Name())
}

// spoolUpload copies at most limit bytes of body to a temporary file, hashing it and deciding its type.
func spoolUpload(filename string, body io.Reader, limit int64) (*spooled, error) {
	tmp, err := os.CreateTemp("", "crewstudio-upload-*")
	if err != nil {
		return nil, fmt.Errorf("spool upload: %w", err)
	}
	sp := &spooled{file: tmp}
	hash := sha256.New()
	if sp.size, err = io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(body, limit+1)); err != nil {
		sp.Close()
		return nil, err
	}
	if sp.size > limit {
		sp.Close()
		return nil, errUploadTooLarge
	}
	if sp.size == 0 {
		sp.Close()
		return nil, &uploadError{http.StatusUnprocessableEntity, "validation_failed", filename + " is empty."}
	}
	head := make([]byte, 512)
	n, _ := tmp.ReadAt(head, 0)
	sp.contentType = classify(filename, head[:n])
	sp.sha256 = hex.EncodeToString(hash.Sum(nil))
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		sp.Close()
		return nil, err
	}
	return sp, nil
}

// storeUpload spools one file to disk (checking size and type), uploads it, and records it.
func (s *Server) storeUpload(r *http.Request, workOrderID, filename string, body io.Reader, remaining int64) (model.Attachment, error) {
	filename = launch.SafeFilename(filename)
	limit := min(int64(maxAttachmentBytes), remaining)
	sp, err := spoolUpload(filename, body, limit)
	if errors.Is(err, errUploadTooLarge) {
		if limit < maxAttachmentBytes {
			return model.Attachment{}, &uploadError{http.StatusRequestEntityTooLarge, "too_large",
				fmt.Sprintf("%s would take this work order over %d MB of files.", filename, maxAttachmentTotalBytes>>20)}
		}
		return model.Attachment{}, &uploadError{http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("%s is larger than %d MB.", filename, maxAttachmentBytes>>20)}
	}
	if err != nil {
		return model.Attachment{}, err
	}
	defer sp.Close()
	tmp, size, contentType := sp.file, sp.size, sp.contentType
	if contentType == "" {
		return model.Attachment{}, &uploadError{http.StatusUnsupportedMediaType, "unsupported_file",
			filename + " is not an allowed file type (images, PDF, text, CSV, JSON, Word, Excel, PowerPoint)."}
	}
	id := auth.NewUUID()
	key := "work-orders/" + workOrderID + "/" + id
	if err := s.files.Put(r.Context(), key, tmp, size, contentType); err != nil {
		return model.Attachment{}, fmt.Errorf("store %s: %w", filename, err)
	}
	a, err := s.store.AddInputAttachment(r.Context(), model.Attachment{
		ID: id, WorkOrderID: &workOrderID, Filename: filename, ContentType: contentType,
		SizeBytes: size, SHA256: sp.sha256, StorageKey: key,
	}, maxAttachmentsPerOrder, maxAttachmentTotalBytes)
	if err != nil {
		// Not tracked by any row yet, so remove it now (with its own deadline: the request may be gone)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
		defer cancel()
		_ = s.files.Delete(cleanup, key)
		if errors.Is(err, store.ErrInputQuota) {
			return model.Attachment{}, &uploadError{http.StatusRequestEntityTooLarge, "too_large",
				fmt.Sprintf("A work order can have at most %d files and %d MB.", maxAttachmentsPerOrder, maxAttachmentTotalBytes>>20)}
		}
		return model.Attachment{}, err
	}
	return a, nil
}

func (s *Server) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	attachmentID, valid := pathUUID(w, r, "attachmentId")
	if !valid {
		return
	}
	a, err := s.store.GetAttachment(r.Context(), attachmentID)
	if err == nil && (a.WorkOrderID == nil || *a.WorkOrderID != wo.ID || a.Kind != model.AttachmentInput) {
		err = store.ErrNotFound
	}
	if err == nil {
		// The running attempt reads its inputs; they cannot be removed underneath it
		err = s.store.EnsureWorkOrderIdle(r.Context(), wo.ID)
	}
	if err == nil {
		err = s.store.DeleteAttachment(r.Context(), attachmentID)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

// canSeeAttachment: input files follow their work order; run files follow their run; a chat message's files are
// only for the person whose conversation it is.
func (s *Server) canSeeAttachment(r *http.Request, a model.Attachment) bool {
	p := principal(r)
	if a.MessageID != nil {
		_, conv, err := s.store.GetMessage(r.Context(), *a.MessageID)
		if err != nil || p == nil || conv.UserID != p.UserID {
			return false
		}
		_, _, err = s.chat.OwnConversation(r.Context(), p, conv.ID)
		return err == nil
	}
	if a.RunID != nil {
		run, err := s.store.GetRun(r.Context(), *a.RunID)
		return err == nil && canViewRun(p, run)
	}
	if a.WorkOrderID != nil && p.Can(auth.WorkOrdersView) {
		wo, err := s.store.GetWorkOrder(r.Context(), *a.WorkOrderID)
		return err == nil && p.SeesAreas(wo.AreaIDs)
	}
	return false
}

// attachmentContent streams a stored file. Only plain images are shown inline; everything else downloads,
// and a sandboxing CSP stops an uploaded HTML or SVG file running script on this origin.
func (s *Server) attachmentContent(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	if s.files == nil {
		fail(w, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured on this server.")
		return
	}
	a, err := s.store.GetAttachment(r.Context(), id)
	if err == nil && !s.canSeeAttachment(r, a) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	body, err := s.files.Get(r.Context(), a.StorageKey)
	if errors.Is(err, storage.ErrNotFound) {
		fail(w, http.StatusNotFound, "not_found", "The stored file is missing.")
		return
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	defer body.Close()

	base, _, _ := mime.ParseMediaType(a.ContentType)
	inline := imageTypes[base] && r.URL.Query().Get("download") == ""
	contentType := a.ContentType
	if !inline && (strings.HasPrefix(base, "text/") || base == "application/xml" || base == "application/json") {
		contentType = "text/plain; charset=utf-8" // never rendered as HTML, even if fetched directly
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", fmt.Sprint(a.SizeBytes))
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": path.Base(a.Filename)}))
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=300")
	h.Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, body); err != nil {
		s.log.Warn("stream attachment", "attachment_id", a.ID, "err", err)
	}
}
