package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/help"
)

const (
	// Each semantic search can call the embedding API, so searches get their own limit per principal
	helpSearchesPerMinute = 60
	helpSearchBurst       = 20
	maxHelpQueryRunes     = 200
	defaultHelpResults    = 8
	maxHelpResults        = 20
)

var (
	helpSlugPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	helpImagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}\.(png|jpg|webp)$`)
	helpImageTypes   = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".webp": "image/webp"}
)

// helpRoutes are open to everyone signed in; what each person gets is filtered by their permissions.
func (s *Server) helpRoutes(r chi.Router) {
	r.Get("/help", s.helpContents)
	r.Get("/help/articles/{slug}", s.helpArticle)
	r.Get("/help/search", s.helpSearch)
	r.Get("/help/images/{name}", s.helpImage)
}

func (s *Server) helpContents(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"categories": s.help.Library().Contents(principal(r))})
}

func (s *Server) helpArticle(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !helpSlugPattern.MatchString(slug) {
		fail(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	// Hidden and missing articles answer the same, so titles of hidden ones cannot be probed
	view, found := s.help.Library().Article(principal(r), slug)
	if !found {
		fail(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	ok(w, view)
}

func (s *Server) helpSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > maxHelpQueryRunes {
		fail(w, http.StatusUnprocessableEntity, "validation_failed", "Search for 2 to 200 characters.",
			"q: must be 2 to 200 characters")
		return
	}
	limit := defaultHelpResults
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxHelpResults {
			fail(w, http.StatusUnprocessableEntity, "validation_failed", "limit must be 1 to 20.", "limit: must be 1 to 20")
			return
		}
		limit = n
	}
	p := principal(r)
	if !s.helpSearches.allow(p.UserID) {
		tooMany(w, "Searching too quickly; wait a moment.")
		return
	}
	ok(w, s.help.Search(r.Context(), p, q, limit))
}

func (s *Server) helpImage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	lib := s.help.Library()
	if !helpImagePattern.MatchString(name) || !lib.ImageVisible(principal(r), name) {
		fail(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	data, err := lib.Image(name)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	sum := sha256.Sum256(data)
	w.Header().Set("Content-Type", helpImageTypes[path.Ext(name)])
	// The answer depends on who is asking, so the browser revalidates every time (cheap with the ETag) and a
	// screenshot stops showing as soon as the person loses access
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// defaultHelp is the keyword-only help index from the compiled-in articles, for servers built without one.
func defaultHelp() *help.Index {
	lib, err := help.LoadEmbedded()
	if err != nil {
		// The articles are compiled in and checked by tests, so this is a build defect
		panic(err)
	}
	return help.NewIndex(lib, nil, nil, nil)
}
