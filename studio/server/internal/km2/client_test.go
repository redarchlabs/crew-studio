package km2_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/km2"
	"crewstudio/internal/km2/km2test"
	"crewstudio/internal/model"
)

const testKey = "km2_test-key-0123456789"

func conn(base string, config map[string]string) model.Connection {
	cfg := map[string]string{"base_url": base}
	for k, v := range config {
		cfg[k] = v
	}
	return model.Connection{ID: "c1", Name: "KM2 Finance", Kind: model.KindKM2, Config: cfg}
}

// newClient is a client of a fake KM2 that never sleeps between retries (it records the waits instead).
func newClient(t *testing.T, d *km2test.Data) (*km2.Client, *[]time.Duration) {
	t.Helper()
	srv := km2test.Server(t, d)
	var waits []time.Duration
	c, err := km2.New(conn(srv.URL+"/", nil), map[string]string{"api_key": testKey},
		km2.Options{HTTP: srv.Client(), Sleep: func(d time.Duration) error { waits = append(waits, d); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return c, &waits
}

func TestNewChecksTheConnection(t *testing.T) {
	cases := map[string]struct {
		conn    model.Connection
		secrets map[string]string
	}{
		"not a KM2 one":      {model.Connection{Name: "x", Kind: "jira"}, map[string]string{"api_key": testKey}},
		"not a usable":       {conn("ftp://km2.example", nil), map[string]string{"api_key": testKey}},
		"has no API key":     {conn("https://km2.example", nil), map[string]string{}},
		"not a KM2 key":      {conn("https://km2.example", nil), map[string]string{"api_key": "sk-other"}},
		"query not usable":   {conn("https://km2.example?x=1", nil), map[string]string{"api_key": testKey}},
		"user is not usable": {conn("https://u:p@km2.example", nil), map[string]string{"api_key": testKey}},
	}
	for name, tc := range cases {
		_, err := km2.New(tc.conn, tc.secrets, km2.Options{})
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), testKey) {
			t.Errorf("%s: the key leaked: %v", name, err)
		}
	}
}

func TestReadsAndSaves(t *testing.T) {
	d := km2test.NewData(testKey)
	c, _ := newClient(t, d)
	ctx := context.Background()

	folders, err := c.Folders(ctx)
	if err != nil || len(folders) != 3 || folders[1].Path != "Projects.Atlas" || folders[1].ParentID == nil {
		t.Fatalf("folders = %+v, %v", folders, err)
	}
	found, err := c.Search(ctx, "atlas", 5)
	if err != nil || found.Total != 1 || found.Hits[0].DocumentTitle != "Atlas charter" {
		t.Fatalf("search = %+v, %v", found, err)
	}
	answer, err := c.Ask(ctx, "Who owns Atlas?")
	if err != nil || !strings.Contains(answer.Answer, "Dana") || len(answer.Sources) != 1 || answer.Sources[0]["nested"] != nil ||
		answer.Sources[0]["document_title"] != "Atlas charter" {
		t.Fatalf("ask = %+v, %v", answer, err)
	}

	doc := km2.Document{FolderID: strings.ToUpper(km2test.FolderAtlas), Title: "W41", Content: "# Status", ExternalRef: "atlas-w41",
		Metadata: map[string]any{"source": "crew-studio"}}
	saved, err := c.Save(ctx, doc)
	if err != nil || saved.Outcome != "created" || saved.ExternalRef == nil || *saved.ExternalRef != "atlas-w41" {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	if again, _ := c.Save(ctx, doc); again.Outcome != "unchanged" || again.ID != saved.ID {
		t.Errorf("again = %+v", again)
	}
	doc.Content = "# Status v2"
	if again, _ := c.Save(ctx, doc); again.Outcome != "updated" {
		t.Errorf("updated = %+v", again)
	}
	status, err := c.Status(ctx, saved.ID)
	if err != nil || status.ProcessingStatus != "SUCCESS" {
		t.Errorf("status = %+v, %v", status, err)
	}
	if _, err := c.Status(ctx, "../admin"); err == nil {
		t.Error("a bad document id was sent")
	}
	if _, err := c.Save(ctx, km2.Document{FolderID: "nope", Title: "x", Content: "y"}); err == nil {
		t.Error("a bad folder id was sent")
	}
	reqs := d.Requests()
	last := reqs[len(reqs)-2]
	if last.Path != "/api/v1/knowledge/documents" || last.Body["folder_id"] != km2test.FolderAtlas {
		t.Errorf("save request = %+v", last)
	}
	for _, r := range reqs {
		for k, v := range r.Body {
			if s, ok := v.(string); ok && strings.Contains(s, testKey) {
				t.Errorf("the key was sent in the body (%s)", k)
			}
		}
	}
}

func TestErrorsNeverHoldTheKey(t *testing.T) {
	d := km2test.NewData(testKey)
	c, waits := newClient(t, d)
	ctx := context.Background()

	d.Fail("/api/v1/knowledge/documents", km2test.Failure{Status: http.StatusUnprocessableEntity,
		Detail: []map[string]any{{"loc": []any{"body", "metadata"}, "msg": "bad value " + testKey}}})
	_, err := c.Save(ctx, km2.Document{FolderID: km2test.FolderAtlas, Title: "t", Content: "c"})
	var ke *km2.Error
	if !errors.As(err, &ke) || ke.Status != 422 || ke.Detail != "metadata: bad value [key]" || strings.Contains(err.Error(), testKey) {
		t.Fatalf("422 = %#v", err)
	}
	d.Fail("/api/v1/knowledge/documents", km2test.Failure{Status: http.StatusConflict, Detail: "in use; key km2_partial123 echoed"})
	_, err = c.Save(ctx, km2.Document{FolderID: km2test.FolderAtlas, Title: "t", Content: "c"})
	if !errors.As(err, &ke) || ke.Detail != "in use; key [key] echoed" {
		t.Errorf("partial key = %v", err)
	}
	d.Fail("/api/v1/knowledge/documents", km2test.Failure{Status: http.StatusConflict, Detail: "external_ref is already in use in this folder"})
	_, err = c.Save(ctx, km2.Document{FolderID: km2test.FolderAtlas, Title: "t", Content: "c"})
	if km2.StatusOf(err) != 409 || !strings.Contains(km2.SafeMessage(err), "already in use") {
		t.Errorf("409 = %v", err)
	}

	// Rate limited: retried with Retry-After, then given up when KM2 asks for longer than Studio waits
	d.Fail("/api/v1/search", km2test.Failure{Status: http.StatusTooManyRequests, RetryAfter: "2", Times: 2})
	if _, err := c.Search(ctx, "x", 1); err != nil || len(*waits) != 2 || (*waits)[0] != 2*time.Second {
		t.Errorf("retried = %v, waits %v", err, *waits)
	}
	d.Fail("/api/v1/search", km2test.Failure{Status: http.StatusTooManyRequests, RetryAfter: "3600"})
	_, err = c.Search(ctx, "x", 1)
	if !errors.As(err, &ke) || !ke.RateLimited || !strings.Contains(ke.Message, "retry after 3600 s") {
		t.Errorf("daily cap = %v", err)
	}
	d.Fail("/api/v1/search", km2test.Failure{Status: http.StatusTooManyRequests, Times: 3})
	if _, err := c.Search(ctx, "x", 1); km2.StatusOf(err) != 429 {
		t.Errorf("backoff gave up = %v", err)
	}
	d.Fail("/api/v1/search/chat", km2test.Failure{Status: http.StatusServiceUnavailable, Detail: map[string]any{"message": "brain down"}})
	if _, err := c.Ask(ctx, "x"); km2.StatusOf(err) != 503 || !strings.Contains(err.Error(), "brain down") {
		t.Errorf("503 = %v", err)
	}
	for status, want := range map[int]string{401: "refused the key", 403: "lacks the scope", 404: "no such folder", 413: "larger than", 500: "HTTP 500", 308: "redirect"} {
		d.Fail("/api/v1/knowledge/folders", km2test.Failure{Status: status})
		if _, err := c.Folders(ctx); !strings.Contains(km2.SafeMessage(err), want) {
			t.Errorf("%d = %v", status, err)
		}
	}

	d.SetScopes("knowledge:read")
	if _, err := c.Search(ctx, "x", 1); km2.StatusOf(err) != 403 {
		t.Errorf("no scope = %v", err)
	}
	if km2.SafeMessage(errors.New("db password=hunter2")) != "the request failed on the server; see the service log" || km2.StatusOf(errors.New("x")) != 0 {
		t.Error("plain errors are not shown")
	}
}

func TestAnswersAreBoundedAndNotFollowed(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/api/v1/knowledge/folders":
			http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
		case "/api/v1/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hits":[{"text":"` + strings.Repeat("x", 5000) + `","document_title":"a\nb"}],"total":1}`))
		case "/api/v1/search/chat":
			_, _ = w.Write([]byte(strings.Repeat("y", 5<<20)))
		case "/api/v1/knowledge/documents":
			if r.URL.Query().Get("bad") == "" && hits%2 == 1 {
				_, _ = w.Write([]byte(`{"id":"../x","outcome":"created"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"7B2E0000-0000-4000-8000-000000000001","outcome":"<script>","processing_status":"PENDING; drop","title":"a\nb"}`))
		default:
			_, _ = w.Write([]byte("not json"))
		}
	}))
	t.Cleanup(srv.Close)
	c, err := km2.New(conn(srv.URL, nil), map[string]string{"api_key": testKey}, km2.Options{HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.Folders(ctx); km2.StatusOf(err) != 302 {
		t.Errorf("redirect = %v", err)
	}
	found, err := c.Search(ctx, "x", 1)
	if err != nil || len([]rune(found.Hits[0].Text)) != 4001 || found.Hits[0].DocumentTitle != "a b" {
		t.Errorf("hit = %v", err)
	}
	if _, err := c.Ask(ctx, "x"); err == nil || !strings.Contains(err.Error(), "more than 4 MB") {
		t.Errorf("big = %v", err)
	}
	if _, err := c.Status(ctx, km2test.FolderAtlas); err == nil || !strings.Contains(err.Error(), "not the expected JSON") {
		t.Errorf("not json = %v", err)
	}
	doc := km2.Document{FolderID: km2test.FolderAtlas, Title: "t", Content: "c"}
	if _, err := c.Save(ctx, doc); err == nil || !strings.Contains(err.Error(), "without a document id") {
		t.Errorf("bad id = %v", err)
	}
	saved, err := c.Save(ctx, doc)
	if err != nil || saved.ID != "7b2e0000-0000-4000-8000-000000000001" || saved.Outcome != "saved" || saved.ProcessingStatus != "" || saved.Title != "a b" {
		t.Errorf("cleaned = %+v, %v", saved, err)
	}
	if hits != 6 {
		t.Errorf("hits = %d", hits)
	}
	if !strings.Contains(fmt.Sprint(c), "KM2 client for ") || strings.Contains(fmt.Sprint(c), testKey) || strings.Contains(c.LogValue().String(), testKey) {
		t.Error("the client prints its key")
	}

	down, _ := km2.New(conn("http://127.0.0.1:1", nil), map[string]string{"api_key": testKey}, km2.Options{})
	_, err = down.Folders(ctx)
	if err == nil || !strings.Contains(err.Error(), "could not reach KM2") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("down = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Folders(cancelled); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("cancelled = %v", err)
	}
}
