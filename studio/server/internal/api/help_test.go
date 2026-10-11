package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"crewstudio/internal/help"
)

const helpTestArticle = `---
title: Crews
category: Build
permissions: crews.view
summary: Crews.
---
Crews run tasks. ![List.](crews.png)

## Run a crew
<!-- permissions: crews.run -->
Press Run and set a budget. ![Dialog.](run-crew-modal.png)
`

// helpEnv is newEnv with a small help library whose screenshots are known.
func helpEnv(t *testing.T) testEnv {
	t.Helper()
	e := newEnv(t)
	lib, err := help.Load(fstest.MapFS{
		"crews.md":                  {Data: []byte(helpTestArticle)},
		"start.md":                  {Data: []byte("---\ntitle: Start\ncategory: Getting started\nsummary: s\n---\nSee [crews](/help/crews).\n")},
		"images/crews.png":          {Data: []byte("\x89PNG-crews")},
		"images/run-crew-modal.png": {Data: []byte("\x89PNG-modal")},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(Deps{
		Store: e.st, SSO: &fakeSSO{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Help: help.NewIndex(lib, nil, nil, nil),
	}, Options{PublicURL: "http://127.0.0.1"}).Handler())
	t.Cleanup(srv.Close)
	e.srv = srv
	return e
}

func (e testEnv) rawGet(t *testing.T, path string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	if e.token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(body)
}

func TestHelpIsFilteredByPermission(t *testing.T) {
	e := helpEnv(t)
	_, nobodyTok := e.person(t, "nobody@example.com", nil, nil, nil)
	_, viewerTok := e.person(t, "viewer@example.com", nil, nil, []string{"crews.view"})
	_, runnerTok := e.person(t, "runner@example.com", nil, nil, []string{"crews.view", "crews.run"})
	nobody, viewer, runner := e.as(nobodyTok), e.as(viewerTok), e.as(runnerTok)

	wantStatus(t, e.as("").do(t, "GET", "/api/help", nil), http.StatusUnauthorized)

	type contents struct {
		Categories []help.ContentsCategory `json:"categories"`
	}
	c := decodeData[contents](t, nobody.do(t, "GET", "/api/help", nil))
	if len(c.Categories) != 1 || c.Categories[0].Articles[0].Slug != "start" {
		t.Errorf("someone with no permissions sees only general help: %+v", c)
	}
	c = decodeData[contents](t, viewer.do(t, "GET", "/api/help", nil))
	if len(c.Categories) != 2 || len(c.Categories[1].Articles[0].Headings) != 0 {
		t.Errorf("viewer contents = %+v", c)
	}
	c = decodeData[contents](t, runner.do(t, "GET", "/api/help", nil))
	if len(c.Categories[1].Articles[0].Headings) != 1 {
		t.Errorf("runner contents = %+v", c)
	}

	// Hidden and missing articles are both 404
	wantStatus(t, nobody.do(t, "GET", "/api/help/articles/crews", nil), http.StatusNotFound)
	wantStatus(t, viewer.do(t, "GET", "/api/help/articles/missing", nil), http.StatusNotFound)
	wantStatus(t, viewer.do(t, "GET", "/api/help/articles/..%2Fcrews", nil), http.StatusNotFound)
	start := decodeData[help.ArticleView](t, nobody.do(t, "GET", "/api/help/articles/start", nil))
	if strings.Contains(start.Sections[0].Markdown, "/help/crews") {
		t.Errorf("link to a hidden article must be plain text: %q", start.Sections[0].Markdown)
	}
	art := decodeData[help.ArticleView](t, viewer.do(t, "GET", "/api/help/articles/crews", nil))
	if len(art.Sections) != 1 || !strings.Contains(art.Sections[0].Markdown, "/api/help/images/crews.png") {
		t.Errorf("viewer article = %+v", art)
	}
	art = decodeData[help.ArticleView](t, runner.do(t, "GET", "/api/help/articles/crews", nil))
	if len(art.Sections) != 2 {
		t.Errorf("runner article = %+v", art)
	}
}

func TestHelpImages(t *testing.T) {
	e := helpEnv(t)
	_, viewerTok := e.person(t, "viewer@example.com", nil, nil, []string{"crews.view"})
	viewer := e.as(viewerTok)

	status, header, body := viewer.rawGet(t, "/api/help/images/crews.png")
	if status != http.StatusOK || header.Get("Content-Type") != "image/png" || body != "\x89PNG-crews" ||
		header.Get("Cache-Control") != "private, no-cache" || header.Get("ETag") == "" {
		t.Errorf("image: %d %v %q", status, header, body)
	}
	req, _ := http.NewRequest("GET", viewer.srv.URL+"/api/help/images/crews.png", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: viewerTok})
	req.Header.Set("If-None-Match", header.Get("ETag"))
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusNotModified {
		t.Errorf("revalidation: %v %v", res.StatusCode, err)
	}
	for _, path := range []string{
		"/api/help/images/run-crew-modal.png", // only in the crews.run section
		"/api/help/images/missing.png",
		"/api/help/images/crews.md",
		"/api/help/images/..%2Fcrews.md",
	} {
		if status, _, _ := viewer.rawGet(t, path); status != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, status)
		}
	}
	if status, _, _ := e.as("").rawGet(t, "/api/help/images/crews.png"); status != http.StatusUnauthorized {
		t.Errorf("signed-out image request: %d", status)
	}
	if status, _, _ := e.rawGet(t, "/api/help/images/run-crew-modal.png"); status != http.StatusOK {
		t.Errorf("admin should see every screenshot: %d", status)
	}
}

func TestHelpSearch(t *testing.T) {
	e := helpEnv(t)
	_, viewerTok := e.person(t, "viewer@example.com", nil, nil, []string{"crews.view"})
	viewer := e.as(viewerTok)

	res := decodeData[help.Results](t, e.do(t, "GET", "/api/help/search?q=budget", nil))
	if res.Mode != help.ModeKeyword || len(res.Results) != 1 || res.Results[0].SectionID != "run-a-crew" {
		t.Errorf("admin search = %+v", res)
	}
	res = decodeData[help.Results](t, viewer.do(t, "GET", "/api/help/search?q=budget", nil))
	if len(res.Results) != 0 {
		t.Errorf("viewer must not find the crews.run section: %+v", res)
	}
	for _, q := range []string{"q=", "q=a", "q=" + strings.Repeat("x", 201), "q=crews&limit=0", "q=crews&limit=21", "q=crews&limit=x"} {
		wantStatus(t, viewer.do(t, "GET", "/api/help/search?"+q, nil), http.StatusUnprocessableEntity)
	}
	res = decodeData[help.Results](t, viewer.do(t, "GET", "/api/help/search?q=crews&limit=1", nil))
	if len(res.Results) != 1 {
		t.Errorf("limit: %+v", res)
	}
}

func TestHelpSearchIsRateLimited(t *testing.T) {
	e := helpEnv(t)
	limited := 0
	for range helpSearchBurst + 5 {
		if r := e.do(t, "GET", "/api/help/search?q=crews", nil); r.status == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Error("searches beyond the burst must be limited")
	}
}

func TestDefaultHelpUsesCompiledArticles(t *testing.T) {
	ix := defaultHelp()
	if len(ix.Library().Articles()) == 0 {
		t.Fatal("no compiled-in articles")
	}
	if got := ix.Search(context.Background(), nil, "crews", 5); got.Mode != help.ModeKeyword || len(got.Results) != 0 {
		t.Errorf("nobody signed in finds nothing: %+v", got)
	}
}
