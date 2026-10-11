package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeAzure answers every request with a fixed status and records what it saw.
type fakeAzure struct {
	status int
	mu     sync.Mutex
	seen   []string
	auth   string
}

func (f *fakeAzure) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen = append(f.seen, r.Method+" "+r.URL.RequestURI())
	f.auth = r.Header.Get("Authorization")
	f.mu.Unlock()
	w.WriteHeader(f.status)
	_, _ = w.Write([]byte("<Error>boom</Error>"))
}

func azureAgainst(t *testing.T, status int) (*Azure, *fakeAzure) {
	t.Helper()
	f := &fakeAzure{status: status}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	key := base64.StdEncoding.EncodeToString([]byte("not-a-real-key"))
	a, err := NewAzure("AccountName=acct;AccountKey="+key+";BlobEndpoint="+srv.URL, "box")
	if err != nil {
		t.Fatal(err)
	}
	return a, f
}

func TestAzureErrorStatuses(t *testing.T) {
	ctx := context.Background()
	a, f := azureAgainst(t, http.StatusInternalServerError)
	if err := a.Ready(ctx); err == nil || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("ready = %v", err)
	}
	if _, err := a.Get(ctx, "k/a.txt"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("get = %v", err)
	}
	if err := a.Delete(ctx, "k/a.txt"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("delete = %v", err)
	}
	if err := a.Put(ctx, "k/a.txt", strings.NewReader("x"), 1, "text/plain"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("put = %v", err)
	}
	if !strings.HasPrefix(f.auth, "SharedKey acct:") {
		t.Errorf("authorization = %q", f.auth)
	}
	if !strings.Contains(a.Describe(), "box") {
		t.Errorf("describe = %q", a.Describe())
	}
}

func TestAzureSuccessAndNotFoundStatuses(t *testing.T) {
	ctx := context.Background()
	a, _ := azureAgainst(t, http.StatusNotFound)
	if _, err := a.Get(ctx, "k/a.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get 404 = %v", err)
	}
	if err := a.Delete(ctx, "k/a.txt"); err != nil {
		t.Errorf("delete 404 = %v", err)
	}
	if err := a.Ready(ctx); err == nil {
		t.Error("ready 404 should fail")
	}
	a, _ = azureAgainst(t, http.StatusConflict)
	if err := a.Ready(ctx); err != nil {
		t.Errorf("ready with existing container = %v", err)
	}
	a, _ = azureAgainst(t, http.StatusAccepted)
	if err := a.Delete(ctx, "k/a.txt"); err != nil {
		t.Errorf("delete 202 = %v", err)
	}
	a, _ = azureAgainst(t, http.StatusCreated)
	if err := a.Put(ctx, "k/empty.txt", bytes.NewReader(nil), 0, "text/plain"); err != nil {
		t.Errorf("put empty = %v", err)
	}
	// Invalid keys never reach the network
	if _, err := a.Get(ctx, "../x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get invalid = %v", err)
	}
	if err := a.Delete(ctx, "../x"); err != nil {
		t.Errorf("delete invalid = %v", err)
	}
}

func TestAzureUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	key := base64.StdEncoding.EncodeToString([]byte("k"))
	a, err := NewAzure("AccountName=acct;AccountKey="+key+";BlobEndpoint="+url, "box")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := a.Ready(ctx); err == nil {
		t.Error("ready")
	}
	if _, err := a.Get(ctx, "k"); err == nil {
		t.Error("get")
	}
	if err := a.Delete(ctx, "k"); err == nil {
		t.Error("delete")
	}
	if err := a.Put(ctx, "k", strings.NewReader("x"), 1, ""); err == nil {
		t.Error("put")
	}
}

func TestNewAzureRejectsBadConfig(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("k"))
	for name, tc := range map[string]struct{ conn, container string }{
		"no container":     {"AccountName=a;AccountKey=" + key, ""},
		"no key":           {"AccountName=a", "c"},
		"bad base64":       {"AccountName=a;AccountKey=@@@", "c"},
		"http remote":      {"AccountName=a;AccountKey=" + key + ";BlobEndpoint=http://example.com", "c"},
		"endpoint no host": {"AccountName=a;AccountKey=" + key + ";BlobEndpoint=nonsense", "c"},
	} {
		if _, err := NewAzure(tc.conn, tc.container); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	a, err := NewAzure("AccountName=a;AccountKey="+key+";DefaultEndpointsProtocol=https;EndpointSuffix=core.example.net", "c")
	if err != nil || !strings.Contains(a.Describe(), "a.blob.core.example.net") {
		t.Errorf("derived endpoint = %v, %v", a, err)
	}
	a, err = NewAzure("AccountName=a;AccountKey="+key, "c")
	if err != nil || !strings.Contains(a.Describe(), "a.blob.core.windows.net") {
		t.Errorf("default endpoint = %v, %v", a, err)
	}
	if a, err = NewAzure("UseDevelopmentStorage=true", "c"); err != nil || !a.emulator {
		t.Errorf("emulator = %v, %v", a, err)
	}
}

func TestNewGCSCredentialErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := NewGCS(ctx, GCSConfig{}); err == nil {
		t.Error("empty bucket should fail")
	}
	if _, err := NewGCS(ctx, GCSConfig{Bucket: "b", CredentialsFile: filepath.Join(t.TempDir(), "missing.json")}); err == nil ||
		!strings.Contains(err.Error(), "read credentials") {
		t.Errorf("missing file = %v", err)
	}
	bad := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewGCS(ctx, GCSConfig{Bucket: "b", CredentialsFile: bad}); err == nil || !strings.Contains(err.Error(), "parse credentials") {
		t.Errorf("bad file = %v", err)
	}
	g, err := NewGCS(ctx, GCSConfig{Bucket: "b", Endpoint: "http://127.0.0.1:1/"})
	if err != nil || !strings.Contains(g.Describe(), "gcs b") {
		t.Errorf("emulator = %v, %v", g, err)
	}
}

func TestDescribeMemory(t *testing.T) {
	if NewMemory().Describe() == "" {
		t.Error("describe is empty")
	}
}
