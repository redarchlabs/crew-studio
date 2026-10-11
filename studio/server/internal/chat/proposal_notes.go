package chat

import (
	"context"
	"errors"
	"fmt"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// noteChange is what the front desk proposes: the whole new text of one of the crew's crew-wide notes.
// BaseVersion is the version it read (0 for a new note); the change is refused if the note moved on since.
type noteChange struct {
	Name        string `json:"name"`
	Subject     string `json:"subject"`
	Content     string `json:"content"`
	BaseVersion *int   `json:"base_version"`
}

func notesPutKind() Kind {
	return Kind{Type: TypeNotesPut, Normalize: normalizeNoteChange, Apply: applyNoteChange, Undo: undoNoteChange,
		Needs: []string{auth.StateView, auth.StateManage}}
}

func normalizeNoteChange(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[noteChange](payload)
	if err != nil {
		return nil, "", err
	}
	clean, err := model.NormalizeNotePut(model.NotePut{Name: in.Name, Subject: in.Subject, Content: in.Content, ExpectedVersion: in.BaseVersion})
	if err == nil && in.BaseVersion == nil {
		err = &model.ValidationError{Details: []string{"base_version: give the version you read (0 for a new note)"}}
	}
	if err != nil {
		return nil, "", err
	}
	out := noteChange{Name: clean.Name, Subject: clean.Subject, Content: clean.Content, BaseVersion: in.BaseVersion}
	summary := "Create the note " + clean.Name
	if *in.BaseVersion > 0 {
		summary = fmt.Sprintf("Change the note %s (version %d)", clean.Name, *in.BaseVersion)
	}
	if clean.Subject != "" {
		summary += " about " + clean.Subject
	}
	m, err := asMap(out)
	return m, summary, err
}

// noteKey is the crew-wide note a change is about; chat never reaches a schedule's notes.
func noteKey(crewID string, c noteChange) store.NoteKey {
	return store.NoteKey{StateScope: store.StateScope{CrewID: crewID}, Subject: c.Subject, Name: c.Name}
}

func applyNoteChange(ctx context.Context, env Env, req Request) (map[string]any, error) {
	in, err := decodePayload[noteChange](req.Payload)
	if err != nil {
		return nil, err
	}
	if in.BaseVersion == nil {
		return nil, &model.ValidationError{Details: []string{"base_version: missing"}}
	}
	key := noteKey(req.Crew.ID, in)
	note, err := env.Store.PutNote(ctx, key, in.Content, in.BaseVersion, store.NoteWriter{Name: req.Principal.Name})
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		// A confirm whose response was lost already saved it: the next version is this text, by this person
		if latest, lerr := env.Store.LatestNote(ctx, key); lerr == nil && latest.Version == *in.BaseVersion+1 &&
			latest.Content == in.Content && latest.WrittenBy == req.Principal.Name {
			note, err = latest, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": note.Name, "subject": note.Subject, "version": note.Version, "previous_version": *in.BaseVersion,
		"undoable": true}, nil
}

func undoNoteChange(ctx context.Context, env Env, req Request) error {
	in, err := decodePayload[noteChange](req.Payload)
	if err != nil {
		return err
	}
	version, ok := intOf(req.Result, "version")
	if !ok || version < 1 {
		return &store.ConflictError{Code: "not_undoable", Message: "This change has no saved version to undo."}
	}
	_, err = env.Store.UndoNoteWrite(ctx, noteKey(req.Crew.ID, in), version, store.NoteWriter{Name: req.Principal.Name})
	return err
}
