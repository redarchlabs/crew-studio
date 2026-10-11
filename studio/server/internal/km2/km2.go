// Package km2 talks to KM2 (Knowledge Manager) over its public v1 HTTP API only (docs/crew-state-and-pmo-design.md,
// section 9): semantic search, RAG chat, the folder list and document writes by external_ref.
//
// The client is small and written by hand. It sends the connection's key only to the connection's own base URL
// (fixed paths, ids checked and escaped, redirects never followed), uses the same transport protections as other
// connections, caps response sizes, backs off on 429 and Retry-After, and keeps the key out of every error.
package km2

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Folder is a KM2 folder the key can see.
type Folder struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	ParentID *string `json:"parent_id"`
}

// Hit is one passage found by a search.
type Hit struct {
	DocumentID    string  `json:"document_id"`
	DocumentTitle string  `json:"document_title"`
	Text          string  `json:"text"`
	Score         float64 `json:"score"`
	ChunkOrder    int     `json:"chunk_order"`
}

// SearchResult is what a search found.
type SearchResult struct {
	Hits  []Hit `json:"hits"`
	Total int   `json:"total"`
}

// Answer is a RAG chat answer with its sources (scalar fields only, cut short).
type Answer struct {
	Answer  string           `json:"answer"`
	Sources []map[string]any `json:"sources"`
}

// Document is what to save: text content filed in a folder under a stable external_ref.
type Document struct {
	FolderID    string
	Title       string
	Content     string
	ExternalRef string
	Metadata    map[string]any
}

// Saved is KM2's answer to a document write.
type Saved struct {
	ID               string  `json:"id"`
	DocumentKey      string  `json:"document_key"`
	ExternalRef      *string `json:"external_ref"`
	FolderID         *string `json:"folder_id"`
	Title            string  `json:"title"`
	ProcessingStatus string  `json:"processing_status"`
	ContentHash      *string `json:"content_hash"`
	// Outcome is created, updated, reprocessed, metadata_updated or unchanged
	Outcome string `json:"outcome"`
}

// DocumentStatus is a document's processing state.
type DocumentStatus struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	ProcessingStatus string  `json:"processing_status"`
	FolderID         *string `json:"folder_id"`
	ExternalRef      *string `json:"external_ref"`
}

// Options tune the client; tests replace the HTTP client and the sleep between retries.
type Options struct {
	HTTP  *http.Client
	Sleep func(d time.Duration) error
}

// Error is a failed KM2 call. Message is safe to show to people and agents: it never holds the key, and Detail
// (KM2's own explanation, for validation and conflict answers) has had the key removed.
type Error struct {
	Message string
	// Status is KM2's HTTP status, 0 when KM2 was not reached
	Status int
	// Detail is KM2's reason for a 409, 413 or 422, cut short
	Detail      string
	RateLimited bool
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Message + ": " + e.Detail
	}
	return e.Message
}

func errorf(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// SafeMessage is what people and agents are told about an error: KM2 errors as they are, anything else in general
// terms.
func SafeMessage(err error) string {
	var ke *Error
	if errors.As(err, &ke) {
		return ke.Error()
	}
	return "the request failed on the server; see the service log"
}

// StatusOf is the KM2 status of an error, or 0.
func StatusOf(err error) int {
	var ke *Error
	if errors.As(err, &ke) {
		return ke.Status
	}
	return 0
}
