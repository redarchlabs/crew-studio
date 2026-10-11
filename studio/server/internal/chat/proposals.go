package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"crewstudio/internal/auth"
	"crewstudio/internal/km2"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// Proposal types.
const (
	TypeStartWorkOrder = "start_work_order"
	TypeNotesPut       = "notes_put"
	// Tracking-list changes (phase 4)
	TypeTrackingAdd    = "tracking_add"
	TypeTrackingRemove = "tracking_remove"
)

// Env is what applying a proposal may use.
type Env struct {
	Store    *store.Store
	Launcher *launch.Launcher
	// Trackers checks tracking-list items in Jira or Linear; nil when trackers are not set up
	Trackers *tracker.Service
	// Chat is the chat service itself, for proposals that start chat work (hand-offs)
	Chat *Service
	// KM2 saves documents for KM2 save cards from agent chat; nil when KM2 is not set up
	KM2 *km2.Service
}

// Request is one proposal being applied or undone by the person who confirmed it.
type Request struct {
	Principal    *auth.Principal
	Crew         model.Crew
	Conversation model.Conversation
	MessageID    string
	Payload      map[string]any
	// Result is what Apply returned (Undo only)
	Result map[string]any
}

// Kind is one type of proposal: how to check it when the front desk makes it, apply it when confirmed and, when
// that makes sense, undo it.
type Kind struct {
	Type string
	// Normalize checks a proposed payload (untrusted: it comes from the model) and returns a clean copy and a
	// one-line summary for the card
	Normalize func(payload map[string]any) (map[string]any, string, error)
	Apply     func(ctx context.Context, env Env, req Request) (map[string]any, error)
	// Undo reverses Apply; nil when the change cannot be undone from chat
	Undo func(ctx context.Context, env Env, req Request) error
	// Needs lists permissions the confirming person needs besides crews.chat
	Needs []string
}

var registry = map[string]Kind{}

// Register adds a proposal kind. It panics on a duplicate, which is a programming error.
func Register(k Kind) {
	if _, dup := registry[k.Type]; dup {
		panic("chat: proposal type registered twice: " + k.Type)
	}
	registry[k.Type] = k
}

// Lookup returns the kind of a proposal type.
func Lookup(typ string) (Kind, bool) {
	k, ok := registry[typ]
	return k, ok
}

// Types lists the registered proposal types, sorted.
func Types() []string {
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func init() {
	Register(startWorkOrderKind())
	Register(trackingAddKind())
	Register(trackingRemoveKind())
	Register(notesPutKind())
	Register(startHandoffKind())
	Register(trackerCommentKind())
	Register(trackerSuggestionKind())
	Register(trackerIssueKind())
	Register(km2SaveKind())
}

// decodePayload turns a payload into T, refusing fields T does not have.
func decodePayload[T any](payload map[string]any) (T, error) {
	var out T
	raw, err := json.Marshal(payload)
	if err != nil {
		return out, &model.ValidationError{Details: []string{"payload: is not valid JSON"}}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, &model.ValidationError{Details: []string{"payload: " + err.Error()}}
	}
	return out, nil
}

// asMap turns a typed payload back into the map stored on the proposal.
func asMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w", err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	return out, nil
}

// intOf reads a whole number from a JSON-decoded map value.
func intOf(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case float64:
		return int(v), v == float64(int(v))
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	}
	return 0, false
}
