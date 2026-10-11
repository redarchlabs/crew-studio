// Package api is the Crew Studio HTTP API. Every JSON response uses one envelope (see docs/api.md).
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

const (
	maxBodyBytes     = 2 << 20
	defaultPageLimit = 50
	maxPageLimit     = 200
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type apiError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

type envelope struct {
	Success bool        `json:"success"`
	Data    any         `json:"data"`
	Error   *apiError   `json:"error"`
	Meta    *model.Page `json:"meta,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func ok(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: data})
}

func created(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusCreated, envelope{Success: true, Data: data})
}

func page(w http.ResponseWriter, data any, meta model.Page) {
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: data, Meta: &meta})
}

func noContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func fail(w http.ResponseWriter, status int, code, message string, details ...string) {
	writeJSON(w, status, envelope{Error: &apiError{Code: code, Message: message, Details: details}})
}

// failErr maps domain errors to responses. Unexpected errors are logged in full and reported generically,
// so database details never reach the browser.
func failErr(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var verr *model.ValidationError
	var conflict *store.ConflictError
	var problems *launch.ProblemsError
	var denied *launch.ForbiddenError
	switch {
	case errors.As(err, &verr):
		fail(w, http.StatusUnprocessableEntity, "validation_failed", "Please fix the highlighted fields.", verr.Details...)
	case errors.As(err, &problems):
		fail(w, http.StatusUnprocessableEntity, "validation_failed", problems.Message, problems.Details...)
	case errors.As(err, &denied):
		forbidden(w, denied.Permission)
	case errors.As(err, &conflict):
		code := conflict.Code
		if code == "" {
			code = "conflict"
		}
		fail(w, http.StatusConflict, code, conflict.Message)
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", "Not found.")
	default:
		log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		fail(w, http.StatusInternalServerError, "internal", "Something went wrong on the server. Check the service log.")
	}
}

// invalid is a 422 from a list of "field: problem" strings.
func invalid(details ...string) error {
	return &model.ValidationError{Details: details}
}

// decode reads a JSON body into dst, rejecting oversized bodies and trailing data.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("Request body is over %d bytes.", maxBodyBytes))
			return false
		}
		fail(w, http.StatusBadRequest, "bad_json", "Could not read the request body.")
		return false
	}
	// Postgres text cannot hold NUL characters; refuse them here rather than fail later
	if bytes.Contains(raw, []byte(`\u0000`)) {
		fail(w, http.StatusBadRequest, "bad_json", "The request body contains a NUL (\\u0000) character, which is not allowed.")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("Request body is over %d bytes.", maxBodyBytes))
			return false
		}
		fail(w, http.StatusBadRequest, "bad_json", "The request body is not valid JSON: "+err.Error())
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "bad_json", "The request body must contain a single JSON object.")
		return false
	}
	return true
}

// pageParams reads limit/offset query parameters with sane bounds.
func pageParams(r *http.Request) (limit, offset int) {
	limit = defaultPageLimit
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, maxPageLimit)
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	return limit, offset
}
