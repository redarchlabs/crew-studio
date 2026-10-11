// Package storage keeps work order attachments, files agents write and run transcripts in object storage:
// Azure Blob Storage (or the Azurite emulator locally) or Google Cloud Storage.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrNotFound means there is no object with that key.
var ErrNotFound = errors.New("object not found")

// Store is an object store. Keys are plain ASCII paths such as "attachments/<uuid>".
type Store interface {
	// Put writes size bytes from r under key, replacing any existing object.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Get opens an object; the caller closes the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes an object; a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// Ready checks the container or bucket exists (creating it where that is safe, e.g. the emulator).
	Ready(ctx context.Context) error
	// Describe names the backend for logs and the health page, without secrets.
	Describe() string
}

// Large files take as long as they take; callers bound transfers with their context. Only waiting for the
// store to start answering is limited here.
const responseHeaderTimeout = time.Minute

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return &http.Client{
		Transport: transport,
		// Object stores never need redirects; refusing them keeps signed requests where they were sent
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// statusError builds an error from an unexpected response, including a short piece of the body.
func statusError(op string, res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	return fmt.Errorf("%s: HTTP %d: %s", op, res.StatusCode, strings.TrimSpace(string(body)))
}

// ValidKey reports whether key is safe to use as an object name.
func ValidKey(key string) bool {
	if key == "" || len(key) > 512 || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return false
	}
	for _, r := range key {
		ok := r == '/' || r == '-' || r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// Memory is an in-process store for tests.
type Memory struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemory() *Memory { return &Memory{objects: map[string][]byte{}} }

func (m *Memory) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	if !ValidKey(key) {
		return fmt.Errorf("invalid object key %q", key)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) Ready(context.Context) error { return nil }
func (m *Memory) Describe() string            { return "memory" }

// Len reports how many objects are stored (tests).
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}
