package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAHandler(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<html>app</html>", "assets/page-abc.js": "export {}"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := spaHandler(dir)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	asset := get("/assets/page-abc.js")
	if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d %q", asset.Code, asset.Header().Get("Cache-Control"))
	}

	// A chunk from an older build is a plain 404, never the app's HTML
	stale := get("/assets/page-old.js")
	if stale.Code != http.StatusNotFound || strings.Contains(stale.Body.String(), "<html>") {
		t.Errorf("stale asset: %d %q", stale.Code, stale.Body.String())
	}
	if cc := stale.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("stale asset must not be cached, got %q", cc)
	}
	if traversal := get("/assets/../assets/missing.js"); traversal.Code != http.StatusNotFound {
		t.Errorf("cleaned asset path: %d", traversal.Code)
	}

	// Client-side routes still get the app
	for _, route := range []string{"/", "/work-orders/b987dc2e-67af-460a-97bf-bd3e7c7add31", "/crews"} {
		rec := get(route)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html>app</html>") {
			t.Errorf("route %s: %d %q", route, rec.Code, rec.Body.String())
		}
	}
}
