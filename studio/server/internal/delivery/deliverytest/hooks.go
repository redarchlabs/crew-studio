package deliverytest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Request is one request a fake webhook received.
type Request struct {
	Path   string
	Header http.Header
	Body   map[string]any
}

// Hooks is a fake Slack and Teams in one server:
//   - POST /slack/hook... is an incoming webhook (200 "ok")
//   - POST /slack/api/chat.postMessage needs "Authorization: Bearer <BotToken>" and answers {"ok": true}
//   - POST /teams/... is a Teams workflow (202)
//
// Set Status to make every webhook answer that HTTP status instead (and SlackError to make chat.postMessage
// answer {"ok": false, "error": SlackError}).
type Hooks struct {
	*httptest.Server
	BotToken string

	mu         sync.Mutex
	requests   []Request
	status     int
	slackError string
}

// NewHooks starts the fake for the test.
func NewHooks(t testing.TB) *Hooks {
	t.Helper()
	h := &Hooks{BotToken: "xoxb-test-token"}
	h.Server = httptest.NewServer(http.HandlerFunc(h.serve))
	t.Cleanup(h.Close)
	return h
}

// Fail makes the webhooks answer status (0 to succeed again) and chat.postMessage answer slackError.
func (h *Hooks) Fail(status int, slackError string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status, h.slackError = status, slackError
}

// Requests returns what the fake received.
func (h *Hooks) Requests() []Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Request(nil), h.requests...)
}

func (h *Hooks) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	h.mu.Lock()
	h.requests = append(h.requests, Request{Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
	status, slackErr := h.status, h.slackError
	h.mu.Unlock()
	switch {
	case r.URL.Path == "/slack/api/chat.postMessage":
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("Authorization") != "Bearer "+h.BotToken:
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
		case slackErr != "":
			_, _ = w.Write([]byte(`{"ok":false,"error":"` + slackErr + `"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"channel":"C1","ts":"1.2"}`))
		}
	case status != 0:
		w.WriteHeader(status)
		_, _ = w.Write([]byte("channel_not_found"))
	case len(r.URL.Path) > 7 && r.URL.Path[:7] == "/teams/":
		w.WriteHeader(http.StatusAccepted)
	default:
		_, _ = w.Write([]byte("ok"))
	}
}
