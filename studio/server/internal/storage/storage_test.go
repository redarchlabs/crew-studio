package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
)

func TestValidKey(t *testing.T) {
	for key, want := range map[string]bool{
		"attachments/0f5c.png": true, "runs/a/b-c_d.txt": true,
		"": false, "/abs": false, "a/../b": false, "space key": false, "ünï": false, strings.Repeat("a", 513): false,
	} {
		if got := ValidKey(key); got != want {
			t.Errorf("ValidKey(%q) = %v", key, got)
		}
	}
}

func roundTrip(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.Ready(ctx); err != nil {
		t.Fatalf("ready: %v", err)
	}
	key := "test/" + strings.ToLower(auth.RandomToken(8)) + ".txt"
	key = strings.NewReplacer("-", "x", "_", "y").Replace(key)
	body := []byte("hello attachments")
	if err := s.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatalf("put: %v", err)
	}
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, body) {
		t.Errorf("got %q", got)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Errorf("deleting a missing object should succeed: %v", err)
	}
	if _, err := s.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete = %v", err)
	}
	if err := s.Put(ctx, "../escape", bytes.NewReader(nil), 0, "text/plain"); err == nil {
		t.Error("invalid key must be refused")
	}
}

func TestMemoryRoundTrip(t *testing.T) {
	roundTrip(t, NewMemory())
}

func TestAzureConnectionStrings(t *testing.T) {
	a, err := NewAzure("UseDevelopmentStorage=true", "c")
	if err != nil || a.account != azuriteAccount || a.endpoint.Host != "127.0.0.1:10000" || !a.emulator {
		t.Fatalf("dev storage: %+v %v", a, err)
	}
	a, err = NewAzure("DefaultEndpointsProtocol=https;AccountName=acme;AccountKey=a2V5;EndpointSuffix=core.windows.net", "files")
	if err != nil || a.endpoint.String() != "https://acme.blob.core.windows.net" || a.emulator {
		t.Fatalf("real account: %+v %v", a, err)
	}
	for _, bad := range []string{
		"AccountName=acme", "AccountName=acme;AccountKey=!!notbase64", "AccountName=acme;AccountKey=a2V5;BlobEndpoint=http://example.com",
	} {
		if _, err := NewAzure(bad, "c"); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if _, err := NewAzure("UseDevelopmentStorage=true", ""); err == nil {
		t.Error("empty container should be refused")
	}
}

// TestAzuriteRoundTrip runs against the docker-compose Azurite container when it is up.
func TestAzuriteRoundTrip(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:10000", 300*time.Millisecond)
	if err != nil {
		if os.Getenv("STUDIO_REQUIRE_AZURITE") != "" {
			t.Fatalf("azurite not reachable: %v", err)
		}
		t.Skip("Azurite is not running on 127.0.0.1:10000")
	}
	conn.Close()
	a, err := NewAzure("UseDevelopmentStorage=true", "crewstudio-test")
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, a)
}

// fakeGCS implements the few JSON API calls the client makes.
func fakeGCS(t *testing.T) *httptest.Server {
	objects := map[string][]byte{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/storage/v1/b/bkt":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/upload/storage/v1/b/bkt/o":
			if r.URL.Query().Get("uploadType") != "media" {
				t.Errorf("upload type = %q", r.URL.Query().Get("uploadType"))
			}
			body, _ := io.ReadAll(r.Body)
			objects[r.URL.Query().Get("name")] = body
			_, _ = w.Write([]byte(`{}`))
		case strings.HasPrefix(r.URL.Path, "/storage/v1/b/bkt/o/"):
			name := strings.TrimPrefix(r.URL.Path, "/storage/v1/b/bkt/o/")
			data, ok := objects[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if r.Method == http.MethodDelete {
				delete(objects, name)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGCSRoundTripAgainstFake(t *testing.T) {
	srv := fakeGCS(t)
	defer srv.Close()
	g, err := NewGCS(context.Background(), GCSConfig{Bucket: "bkt", Endpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, g)
	if _, err := NewGCS(context.Background(), GCSConfig{}); err == nil {
		t.Error("empty bucket should be refused")
	}
	missing, _ := NewGCS(context.Background(), GCSConfig{Bucket: "nope", Endpoint: srv.URL})
	if err := missing.Ready(context.Background()); err == nil {
		t.Error("a missing bucket should fail Ready")
	}
}
