package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// KM2 save cards from agent chat (slice 3d). Confirming saves the document through the agent's KM2 connection, to its
// default folder or one of the folders an administrator allowed, as the person: the metadata says who confirmed it
// and which agent drafted it. A card's document id always starts with chatRefPrefix and the conversation's own prefix
// (conversationRef), so a card can only ever replace a document an earlier card of the same conversation saved, never
// one a crew, a person or another conversation saved. It cannot be undone from chat.

const (
	// chatRefPrefix starts the external id of every document a chat card saves
	chatRefPrefix = "agent-chat-"
	// maxProposedKM2Content caps a proposed document: the card shows it in full before the person confirms
	maxProposedKM2Content = 100_000
)

type km2SaveProposal struct {
	proposedVia
	Title       string `json:"title"`
	ExternalRef string `json:"external_ref"`
	Content     string `json:"content"`
	Folder      string `json:"folder"`
}

// convRefLen is how much of the conversation's id goes into its documents' ids
const convRefLen = 12

// conversationPrefix starts the id of every document the conversation's cards save.
func conversationPrefix(conversationID string) string {
	id := strings.ReplaceAll(conversationID, "-", "")
	if len(id) > convRefLen {
		id = id[:convRefLen]
	}
	return chatRefPrefix + id + "-"
}

// conversationRef is the document id a card of the conversation saves under: the model's id behind the conversation's
// prefix (whatever prefix it already wrote is kept inside it), refused when the whole would be too long.
func conversationRef(conversationID string, raw any) (string, error) {
	ref, _ := raw.(string)
	ref = strings.TrimSpace(ref)
	prefix := conversationPrefix(conversationID)
	if ref == "" || strings.HasPrefix(ref, prefix) {
		return ref, nil
	}
	if room := model.MaxKM2ExternalRefLen - len(prefix); len(ref) > room {
		return "", &model.ValidationError{Details: []string{fmt.Sprintf("external_ref: at most %d characters in a chat card", room)}}
	}
	return prefix + ref, nil
}

func km2SaveKind() Kind {
	return Kind{Type: TypeKM2Save, Normalize: normalizeKM2Save, Apply: applyKM2Save, Needs: counterpartNeeds}
}

func normalizeKM2Save(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[km2SaveProposal](payload)
	if err != nil {
		return nil, "", err
	}
	via, problems := in.clean()
	ref := strings.TrimSpace(in.ExternalRef)
	if ref != "" && !strings.HasPrefix(ref, chatRefPrefix) {
		ref = chatRefPrefix + ref
	}
	c, err := model.NormalizeKM2Save(model.KM2Save{Title: in.Title, Content: in.Content, ExternalRef: ref, Folder: in.Folder})
	details, err := detailsOf(err)
	if err != nil {
		return nil, "", err
	}
	problems = slices.Concat(problems, details)
	if len(c.Content) > maxProposedKM2Content {
		problems = append(problems, fmt.Sprintf("content: must be at most %d bytes in a chat card; save a shorter document", maxProposedKM2Content))
	}
	out := km2SaveProposal{proposedVia: via, Title: c.Title, ExternalRef: c.ExternalRef, Content: c.Content, Folder: c.Folder}
	return finish(problems, out, "Save to KM2: "+c.Title)
}

func applyKM2Save(ctx context.Context, env Env, req Request) (map[string]any, error) {
	in, err := decodePayload[km2SaveProposal](req.Payload)
	if err != nil {
		return nil, err
	}
	if env.KM2 == nil {
		return nil, &DeniedError{Message: "KM2 is not set up on this server."}
	}
	if !strings.HasPrefix(in.ExternalRef, conversationPrefix(req.Conversation.ID)) {
		return nil, &model.ValidationError{Details: []string{"external_ref: a card may only save under its own conversation's ids"}}
	}
	conn, err := counterpartConnection(ctx, env, req, TypeKM2Save, in.proposedVia)
	if err != nil {
		return nil, err
	}
	if !env.KM2.Allow(conn.ID) {
		return nil, &store.ConflictError{Code: "km2_busy", Message: "Too many KM2 calls through " + conn.Name + " just now; try again in a minute."}
	}
	_, client, err := env.KM2.ForAgentChat(ctx, conn.ID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	folder, err := km2.ResolveFolder(ctx, conn, client, in.Folder)
	if err != nil {
		return nil, err
	}
	meta := map[string]any{"source": "crew-studio", "kind": "agent-chat", "saved_by": oneLine(req.Principal.Name),
		"conversation_id": req.Conversation.ID}
	if in.ProposedBy != "" {
		meta["drafted_by"] = in.ProposedBy
	}
	saved, err := client.Save(ctx, km2.Document{FolderID: folder.ID, Title: in.Title, Content: in.Content, ExternalRef: in.ExternalRef, Metadata: meta})
	where := map[string]any{"connection": conn.Name, "external_ref": in.ExternalRef, "folder_id": folder.ID}
	if err != nil {
		return written(nil, err, where)
	}
	return map[string]any{"connection": conn.Name, "document_id": saved.ID, "outcome": saved.Outcome, "external_ref": in.ExternalRef,
		"folder_id": folder.ID}, nil
}
