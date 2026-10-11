package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
)

// Files sent with a chat message: pasted screenshots, images and PDFs, which models that can see images look at.
// A message with files comes as multipart/form-data: a "content" field (and "crew_id" or "agent_id" to start a
// conversation) and
// one "file" part per file. The files are stored before the turn starts and removed again if it does not.

const (
	maxChatFiles      = 4
	maxChatFileBytes  = 10 << 20
	maxChatFilesBytes = 20 << 20
	// maxChatField bounds a text field of the form (content is checked again by NormalizeChatMessage)
	maxChatField = 4 * model.MaxChatMessageLen
	// filesOnlyMessage is the text of a message that is only files
	filesOnlyMessage = "(See the attached files: %s.)"
)

var chatFileTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "application/pdf": true}

// chatMessageForm is a chat message as sent, with its files already in object storage.
type chatMessageForm struct {
	CrewID  string
	AgentID string
	Content string
	Files   []model.Attachment
}

// readChatMessage reads a message sent as JSON ({ content, crew_id or agent_id }) or as multipart/form-data with files. It
// answers the error itself and returns false; on success the caller must start the turn or call dropChatFiles.
func (s *Server) readChatMessage(w http.ResponseWriter, r *http.Request) (chatMessageForm, bool) {
	// The cheap checks come before any file is read: who may chat, and how fast
	if !s.mayChat(w, r) || !s.allowTurn(w, r) {
		return chatMessageForm{}, false
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "multipart/form-data" {
		var in startConversationRequest
		if !decode(w, r, &in) {
			return chatMessageForm{}, false
		}
		return chatMessageForm{CrewID: in.CrewID, AgentID: in.AgentID, Content: in.Content}, true
	}
	if s.files == nil {
		fail(w, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured on this server, so messages cannot carry files.")
		return chatMessageForm{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatFilesBytes+(1<<20))
	form, err := s.readChatForm(r)
	if err != nil {
		s.dropChatFiles(r.Context(), form.Files)
		s.chatUploadFailed(w, r, err)
		return chatMessageForm{}, false
	}
	if strings.TrimSpace(form.Content) == "" && len(form.Files) > 0 {
		form.Content = filesOnlyText(form.Files)
	}
	return form, true
}

// mayChat answers 403 itself unless the caller is a person who may chat (the chat service checks again).
func (s *Server) mayChat(w http.ResponseWriter, r *http.Request) bool {
	p := principal(r)
	if !p.Can(auth.CrewsChat) {
		forbidden(w, auth.CrewsChat)
		return false
	}
	if p.Kind != auth.KindHuman {
		fail(w, http.StatusForbidden, "forbidden", "Chat is for people; service accounts cannot chat with crews.")
		return false
	}
	return true
}

// filesOnlyText is the text of a message that is only files; it names them, so the title and later turns say what was sent.
func filesOnlyText(files []model.Attachment) string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Filename
	}
	return fmt.Sprintf(filesOnlyMessage, strings.Join(names, ", "))
}

func (s *Server) readChatForm(r *http.Request) (chatMessageForm, error) {
	var form chatMessageForm
	reader, err := r.MultipartReader()
	if err != nil {
		return form, &uploadError{http.StatusBadRequest, "bad_upload", "Send the message as multipart/form-data."}
	}
	var total int64
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return form, nil
		}
		if err != nil {
			return form, err
		}
		switch {
		case part.FormName() == "content" || part.FormName() == "crew_id" || part.FormName() == "agent_id":
			value, err := io.ReadAll(io.LimitReader(part, maxChatField+1))
			_ = part.Close()
			if err != nil {
				return form, err
			}
			if len(value) > maxChatField {
				return form, &uploadError{http.StatusUnprocessableEntity, "validation_failed", "The message is too long."}
			}
			switch part.FormName() {
			case "content":
				form.Content = string(value)
			case "crew_id":
				form.CrewID = strings.TrimSpace(string(value))
			default:
				form.AgentID = strings.TrimSpace(string(value))
			}
		case part.FormName() == uploadField && part.FileName() != "":
			if len(form.Files) >= maxChatFiles {
				_ = part.Close()
				return form, &uploadError{http.StatusUnprocessableEntity, "validation_failed", fmt.Sprintf("A message can carry at most %d files.", maxChatFiles)}
			}
			a, err := s.storeChatFile(r, part.FileName(), part, maxChatFilesBytes-total)
			_ = part.Close()
			if err != nil {
				return form, err
			}
			total += a.SizeBytes
			form.Files = append(form.Files, a)
		default:
			_ = part.Close()
		}
	}
}

// storeChatFile checks one file (an image or PDF within the limits) and puts it in object storage.
func (s *Server) storeChatFile(r *http.Request, filename string, body io.Reader, remaining int64) (model.Attachment, error) {
	filename = launch.SafeFilename(filename)
	sp, err := spoolUpload(filename, body, min(int64(maxChatFileBytes), remaining))
	if errors.Is(err, errUploadTooLarge) {
		return model.Attachment{}, &uploadError{http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("%s is too large: each file may be at most %d MB, and a message's files %d MB.", filename, maxChatFileBytes>>20, maxChatFilesBytes>>20)}
	}
	if err != nil {
		return model.Attachment{}, err
	}
	defer sp.Close()
	if !chatFileTypes[sp.contentType] {
		return model.Attachment{}, &uploadError{http.StatusUnsupportedMediaType, "unsupported_file",
			filename + " cannot go with a chat message: send images (PNG, JPEG, GIF, WebP) or PDFs."}
	}
	id := auth.NewUUID()
	key := "chat/" + principal(r).UserID + "/" + id
	if err := s.files.Put(r.Context(), key, sp.file, sp.size, sp.contentType); err != nil {
		return model.Attachment{}, fmt.Errorf("store %s: %w", filename, err)
	}
	return model.Attachment{ID: id, Kind: model.AttachmentChat, Filename: filename, ContentType: sp.contentType,
		SizeBytes: sp.size, SHA256: sp.sha256, StorageKey: key}, nil
}

// dropChatFiles removes stored files of a message whose turn did not start (no row tracks them yet).
func (s *Server) dropChatFiles(ctx context.Context, files []model.Attachment) {
	if len(files) == 0 || s.files == nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, f := range files {
		if err := s.files.Delete(cleanup, f.StorageKey); err != nil {
			s.log.Warn("remove chat file of a turn that did not start", "key", f.StorageKey, "err", err)
		}
	}
}

func (s *Server) chatUploadFailed(w http.ResponseWriter, r *http.Request, err error) {
	var tooBig *http.MaxBytesError
	var ue *uploadError
	switch {
	case errors.As(err, &ue):
		fail(w, ue.status, ue.code, ue.msg)
	case errors.As(err, &tooBig):
		fail(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("A message's files may total at most %d MB.", maxChatFilesBytes>>20))
	default:
		failErr(w, r, s.log, err)
	}
}
