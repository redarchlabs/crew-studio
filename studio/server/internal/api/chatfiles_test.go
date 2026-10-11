package api

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"testing"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

// postMessage sends a chat message as multipart/form-data with files.
func (e testEnv) postMessage(t *testing.T, path string, fields map[string]string, files map[string][]byte) response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for name, data := range files {
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write(data)
	}
	_ = mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "crewstudio")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	return e.send(t, req)
}

func TestWorkOrderMessageWithAScreenshot(t *testing.T) {
	e := newEnv(t)
	wo := e.finishedWorkOrder(t, "Gemini is cheapest.")
	path := "/api/work-orders/" + wo.ID + "/conversation/messages"

	r := e.postMessage(t, path, map[string]string{"content": " "}, map[string][]byte{"pasted 1.png": pngBytes})
	wantStatus(t, r, http.StatusCreated)
	view := decodeData[conversationView](t, r)
	sent := view.Messages[0]
	if sent.Content != "(See the attached files: pasted 1.png.)" || len(sent.Attachments) != 1 || sent.Attachments[0].ContentType != "image/png" ||
		sent.Attachments[0].Filename != "pasted 1.png" || e.files.Len() != 1 {
		t.Fatalf("message = %+v, stored %d", sent, e.files.Len())
	}
	run, err := e.st.GetRunDetail(context.Background(), *view.Messages[1].RunID)
	if err != nil {
		t.Fatal(err)
	}
	if files := run.Spec.Chat.Files; len(files) != 1 || files[0].ID != sent.Attachments[0].ID || files[0].Path != "attachments/pasted 1.png" {
		t.Fatalf("spec files = %+v", files)
	}
	// Reading the conversation again lists the file with its message
	again := decodeData[conversationView](t, e.do(t, "GET", "/api/work-orders/"+wo.ID+"/conversation", nil))
	if len(again.Messages[0].Attachments) != 1 {
		t.Errorf("listed = %+v", again.Messages[0])
	}

	// Only the person whose conversation it is can open the file
	content := "/api/attachments/" + sent.Attachments[0].ID + "/content"
	if status, header, _ := e.fetch(t, content); status != http.StatusOK || header.Get("Content-Type") != "image/png" {
		t.Errorf("owner fetch = %d %s", status, header.Get("Content-Type"))
	}
	_, other := e.person(t, "bo@example.com", []string{builderRole}, wo.AreaIDs, nil)
	if status, _, _ := e.as(other).fetch(t, content); status != http.StatusNotFound {
		t.Errorf("other person fetch = %d", status)
	}

	// While the reply is written, another message is refused and its file is not kept
	r = e.postMessage(t, path, map[string]string{"content": "and this"}, map[string][]byte{"b.png": pngBytes})
	wantStatus(t, r, http.StatusConflict)
	if e.files.Len() != 1 {
		t.Errorf("stored %d after a refused turn", e.files.Len())
	}
}

func TestChatMessageFilesAreChecked(t *testing.T) {
	e := newEnv(t)
	crew := e.chatCrew(t, 1)
	path := "/api/chat/conversations"
	cases := []struct {
		name   string
		files  map[string][]byte
		status int
	}{
		{"not an image", map[string][]byte{"evil.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`)}, http.StatusUnsupportedMediaType},
		{"text", map[string][]byte{"notes.txt": []byte("hello")}, http.StatusUnsupportedMediaType},
		{"too many", map[string][]byte{"1.png": pngBytes, "2.png": pngBytes, "3.png": pngBytes, "4.png": pngBytes, "5.png": pngBytes}, http.StatusUnprocessableEntity},
		{"too large", map[string][]byte{"big.png": append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{0}, maxChatFileBytes)...)}, http.StatusRequestEntityTooLarge},
		{"empty", map[string][]byte{"empty.png": {}}, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		r := e.postMessage(t, path, map[string]string{"crew_id": crew.ID, "content": "look"}, c.files)
		if r.status != c.status {
			t.Errorf("%s: status = %d, want %d (%+v)", c.name, r.status, c.status, r.body.Error)
		}
		if e.files.Len() != 0 {
			t.Fatalf("%s: %d files left in storage", c.name, e.files.Len())
		}
	}
	// A bad crew id is refused after the file was stored, and the file goes again
	r := e.postMessage(t, path, map[string]string{"crew_id": "nope", "content": "look"}, map[string][]byte{"a.png": pngBytes})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if e.files.Len() != 0 {
		t.Errorf("stored %d after a refused message", e.files.Len())
	}

	// Starting a conversation with a file, then deleting it, removes the file
	r = e.postMessage(t, path, map[string]string{"crew_id": crew.ID, "content": "what is this?"}, map[string][]byte{"a.pdf": []byte("%PDF-1.4\n%%EOF")})
	wantStatus(t, r, http.StatusCreated)
	view := decodeData[conversationView](t, r)
	if len(view.Messages[0].Attachments) != 1 || view.Messages[0].Attachments[0].ContentType != "application/pdf" {
		t.Fatalf("message = %+v", view.Messages[0])
	}
	e.finishTurn(t, view.Messages[1], "A PDF.", 0.001)
	wantStatus(t, e.do(t, "DELETE", path+"/"+view.Conversation.ID, nil), http.StatusNoContent)
	if status, _, _ := e.fetch(t, "/api/attachments/"+view.Messages[0].Attachments[0].ID+"/content"); status != http.StatusNotFound {
		t.Errorf("file of a deleted conversation = %d", status)
	}

	// JSON messages still work and carry no files
	plain := e.startChat(t, crew.ID, "hi")
	if plain.Messages[0].Attachments == nil || len(plain.Messages[0].Attachments) != 0 {
		t.Errorf("plain message attachments = %#v", plain.Messages[0].Attachments)
	}
}

func TestChatFilesNeedChatRightsBeforeTheyAreRead(t *testing.T) {
	e := newEnv(t)
	wo := e.finishedWorkOrder(t, "out")
	_, viewer := e.person(t, "vi@example.com", []string{viewerRole}, wo.AreaIDs, nil)
	r := e.as(viewer).postMessage(t, "/api/work-orders/"+wo.ID+"/conversation/messages", map[string]string{"content": "x"},
		map[string][]byte{"a.png": pngBytes})
	wantStatus(t, r, http.StatusForbidden)
	if e.files.Len() != 0 {
		t.Errorf("stored %d files for someone who may not chat", e.files.Len())
	}
}
