package api

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestOpenAPICoversEveryRoute keeps the API explorer complete: every route the server registers under /api
// must appear in openapi.yaml.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	spec := string(openAPISpec)
	pathBlock := spec[strings.Index(spec, "\npaths:\n"):]
	documented := map[string]map[string]bool{}
	var current string
	for _, line := range strings.Split(pathBlock, "\n") {
		if m := regexp.MustCompile(`^  (/\S+):$`).FindStringSubmatch(line); m != nil {
			current = m[1]
			documented[current] = map[string]bool{}
			continue
		}
		if m := regexp.MustCompile(`^    (get|post|put|patch|delete):`).FindStringSubmatch(line); m != nil && current != "" {
			documented[current][strings.ToUpper(m[1])] = true
		}
	}

	s := NewServer(Deps{}, Options{})
	router := chi.NewRouter()
	s.authRoutes(router)
	router.Post("/hooks/{id}", s.fireTrigger)
	router.Get("/runs/{id}/stream", s.streamRun)
	router.Post("/work-orders/{id}/attachments", s.uploadAttachments)
	router.Get("/attachments/{id}/content", s.attachmentContent)
	s.routes(router)

	// Browser-only sign-in steps are not part of the API explorer
	skip := map[string]bool{"/auth/{provider}/login": true, "/auth/{provider}/callback": true, "/auth/setup": true,
		"/auth/login": true, "/runs/{id}/stream": true}
	var missing []string
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if route == "" || skip[route] || method == http.MethodOptions {
			return nil
		}
		if !documented[route][method] {
			missing = append(missing, method+" "+route)
		}
		return nil
	})
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("routes missing from openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
}
