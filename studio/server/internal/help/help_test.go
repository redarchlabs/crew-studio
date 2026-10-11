package help

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/time/rate"
)

type perms map[string]bool

func (p perms) Can(k string) bool { return p[k] }

const crewsArticle = `---
title: Crews
category: Build
order: 10
permissions: crews.view
summary: Build and inspect crews.
keywords: team, pipeline
---
A crew is a team of agents working through tasks.

![The crews list.](crews.png)

## Read the graph
The graph shows tasks and agents. See [agents](/help/agents) and [settings](/help/settings#budget).

## Run a crew
<!-- permissions: crews.run -->
Press Run and fill in the inputs. Set a budget in dollars.

![The run dialog.](run-crew-modal.png)

` + "```" + `
## not a heading inside a fence
` + "```" + `

## Run a crew
<!-- permissions: crews.edit, crews.create -->
A second heading with the same title gets a numbered id.
`

const agentsArticle = `---
title: Agents
category: Build
order: 20
permissions: agents.view
summary: Agents.
---
## Roles and goals
Each agent has a role, a goal and a backstory.
`

const startArticle = `---
title: Getting started
category: Getting started
summary: Start here.
---
Sign in with Microsoft or Clerk. Open [crews](/help/crews) when you can.
`

const settingsArticle = `---
title: Settings
category: Configure
permissions: settings.view
summary: Budgets.
---
## Budget
The default budget caps spending per run.
`

func testLibrary(t *testing.T) *Library {
	t.Helper()
	lib, err := Load(fstest.MapFS{
		"crews.md":                  {Data: []byte(crewsArticle)},
		"agents.md":                 {Data: []byte(agentsArticle)},
		"getting-started.md":        {Data: []byte(startArticle)},
		"settings.md":               {Data: []byte(settingsArticle)},
		"notes.txt":                 {Data: []byte("ignored")},
		"images/crews.png":          {Data: []byte("png-crews")},
		"images/run-crew-modal.png": {Data: []byte("png-modal")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func TestParseArticle(t *testing.T) {
	lib := testLibrary(t)
	a, ok := lib.article("crews")
	if !ok {
		t.Fatal("crews not loaded")
	}
	if a.Title != "Crews" || a.Category != "Build" || a.Order != 10 || !slices.Equal(a.Permissions, []string{"crews.view"}) ||
		!slices.Equal(a.Keywords, []string{"team", "pipeline"}) {
		t.Fatalf("front matter = %+v", a)
	}
	var ids []string
	for _, s := range a.Sections {
		ids = append(ids, s.ID)
	}
	if want := []string{"intro", "read-the-graph", "run-a-crew", "run-a-crew-2"}; !slices.Equal(ids, want) {
		t.Fatalf("section ids = %v, want %v", ids, want)
	}
	run := a.Sections[2]
	if !slices.Equal(run.Permissions, []string{"crews.run"}) || strings.Contains(run.Body, "<!--") {
		t.Errorf("run section perms %v body %q", run.Permissions, run.Body)
	}
	if !strings.Contains(run.Body, "## not a heading inside a fence") {
		t.Error("a ## line inside a code fence must stay in the body")
	}
	if !slices.Equal(a.Sections[0].Images, []string{"crews.png"}) || !slices.Equal(run.Images, []string{"run-crew-modal.png"}) {
		t.Errorf("images: intro %v run %v", a.Sections[0].Images, run.Images)
	}
	if got := lib.ReferencedImages(); !slices.Equal(got, []string{"crews.png", "run-crew-modal.png"}) {
		t.Errorf("referenced images = %v", got)
	}
}

func TestLoadOrdersByCategoryThenOrder(t *testing.T) {
	var slugs []string
	for _, a := range testLibrary(t).Articles() {
		slugs = append(slugs, a.Slug)
	}
	if want := []string{"getting-started", "crews", "agents", "settings"}; !slices.Equal(slugs, want) {
		t.Errorf("order = %v, want %v", slugs, want)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no front matter":      "# Title\n",
		"unclosed":             "---\ntitle: x\ncategory: y\n",
		"bad line":             "---\ntitle x\n---\nbody",
		"missing title":        "---\ncategory: Build\n---\nbody",
		"bad order":            "---\ntitle: x\ncategory: y\norder: first\n---\nbody",
		"unknown permission":   "---\ntitle: x\ncategory: y\npermissions: crews.fly\n---\nbody",
		"unknown section perm": "---\ntitle: x\ncategory: y\n---\n## A\n<!-- permissions: nope.nope -->\ntext",
		"stray comment":        "---\ntitle: x\ncategory: y\n---\n## A\ntext\n<!-- permissions: crews.view -->",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(fstest.MapFS{"a.md": {Data: []byte(raw)}}); err == nil {
				t.Error("expected an error")
			}
		})
	}
	if _, err := Load(fstest.MapFS{"Bad_Name.md": {Data: []byte(startArticle)}}); err == nil {
		t.Error("expected an error for a file name that is not a slug")
	}
}

func TestContentsFiltersByPermission(t *testing.T) {
	lib := testLibrary(t)
	cats := lib.Contents(perms{"crews.view": true})
	if len(cats) != 2 || cats[0].Title != "Getting started" || cats[1].Title != "Build" || len(cats[1].Articles) != 1 {
		t.Fatalf("contents = %+v", cats)
	}
	var headings []string
	for _, h := range cats[1].Articles[0].Headings {
		headings = append(headings, h.ID)
	}
	if !slices.Equal(headings, []string{"read-the-graph"}) {
		t.Errorf("a viewer without crews.run/edit must only see the ungated heading, got %v", headings)
	}

	cats = lib.Contents(perms{"crews.view": true, "crews.run": true, "agents.view": true})
	if len(cats[1].Articles) != 2 || len(cats[1].Articles[0].Headings) != 2 {
		t.Errorf("contents with run+agents = %+v", cats)
	}
	if got := lib.Contents(nil); len(got) != 0 {
		t.Errorf("nobody signed in sees nothing, got %+v", got)
	}
}

func TestArticleFiltersSectionsAndLinks(t *testing.T) {
	lib := testLibrary(t)
	if _, ok := lib.Article(perms{}, "crews"); ok {
		t.Error("crews needs crews.view")
	}
	if _, ok := lib.Article(perms{"crews.view": true}, "missing"); ok {
		t.Error("missing article must not be found")
	}
	view, ok := lib.Article(perms{"crews.view": true}, "crews")
	if !ok || len(view.Sections) != 2 {
		t.Fatalf("view = %+v", view)
	}
	if !strings.Contains(view.Sections[0].Markdown, "![The crews list.](/api/help/images/crews.png)") {
		t.Errorf("image not rewritten: %q", view.Sections[0].Markdown)
	}
	graph := view.Sections[1].Markdown
	if strings.Contains(graph, "/help/agents") || !strings.Contains(graph, "See agents and settings.") {
		t.Errorf("links to hidden articles must become text: %q", graph)
	}

	view, _ = lib.Article(perms{"crews.view": true, "agents.view": true, "settings.view": true}, "crews")
	if !strings.Contains(view.Sections[1].Markdown, "[agents](/help/agents)") || !strings.Contains(view.Sections[1].Markdown, "[settings](/help/settings#budget)") {
		t.Errorf("links to visible articles must stay: %q", view.Sections[1].Markdown)
	}
}

func TestImages(t *testing.T) {
	lib := testLibrary(t)
	viewer := perms{"crews.view": true}
	if !lib.ImageVisible(viewer, "crews.png") || lib.ImageVisible(viewer, "run-crew-modal.png") {
		t.Error("the run dialog screenshot is only in the crews.run section")
	}
	if !lib.ImageVisible(perms{"crews.view": true, "crews.run": true}, "run-crew-modal.png") {
		t.Error("runners see the run dialog screenshot")
	}
	if data, err := lib.Image("crews.png"); err != nil || string(data) != "png-crews" {
		t.Errorf("image = %q, %v", data, err)
	}
	for _, bad := range []string{"../crews.md", "missing.png", "crews.md"} {
		if _, err := lib.Image(bad); err == nil {
			t.Errorf("Image(%q) must fail", bad)
		}
	}
	if _, err := (&Library{}).Image("crews.png"); err == nil {
		t.Error("a library without images has none")
	}
}

func TestKeywordSearch(t *testing.T) {
	ix := NewIndex(testLibrary(t), nil, nil, nil)
	got := ix.Search(context.Background(), perms{"crews.view": true, "crews.run": true}, "how do I set the budget?", 5)
	if got.Mode != ModeKeyword || len(got.Results) == 0 {
		t.Fatalf("results = %+v", got)
	}
	if r := got.Results[0]; r.Slug != "crews" || r.SectionID != "run-a-crew" || !strings.Contains(r.Snippet, "budget") {
		t.Errorf("top result = %+v", r)
	}
	for _, r := range got.Results {
		if r.Slug == "settings" {
			t.Error("settings needs settings.view")
		}
	}

	// Without crews.run the section that matches is hidden from search too
	got = ix.Search(context.Background(), perms{"crews.view": true}, "budget", 5)
	for _, r := range got.Results {
		if r.SectionID == "run-a-crew" {
			t.Errorf("hidden section returned: %+v", r)
		}
	}
	if got := ix.Search(context.Background(), perms{"crews.view": true}, "zzzz", 5); len(got.Results) != 0 {
		t.Errorf("no match should return nothing, got %+v", got.Results)
	}
	if got := ix.Search(context.Background(), perms{"crews.view": true}, "agents", 1); len(got.Results) != 1 {
		t.Errorf("limit not applied: %+v", got.Results)
	}
}

func TestTokenizeAndSnippet(t *testing.T) {
	for _, group := range [][]string{
		{"schedule", "schedules", "scheduled", "scheduling"},
		{"run", "runs", "running"},
		{"image", "images"},
		{"policy", "policies"},
		{"use", "uses"},
	} {
		for _, w := range group[1:] {
			if stem(w) != stem(group[0]) {
				t.Errorf("stem(%q) = %q, stem(%q) = %q; want equal", w, stem(w), group[0], stem(group[0]))
			}
		}
	}
	if stem("called") != "call" || stem("pass") != "pass" {
		t.Errorf("stem(called) = %q, stem(pass) = %q", stem("called"), stem("pass"))
	}
	// Markdown punctuation must not glue words together; snake_case splits the same way in text and queries
	if got := tokenize(plainText("set work_order and STUDIO_HELP_SEARCH |a|b| **bold**text")); !slices.Contains(got, "work") ||
		!slices.Contains(got, "order") || !slices.Contains(got, "studio") || !slices.Contains(got, "bold") {
		t.Errorf("tokens = %v", got)
	}
	long := strings.Repeat("word ", 100)
	if s := snippet(long, nil); !strings.HasSuffix(s, "…") || len([]rune(s)) > snippetRunes+1 {
		t.Errorf("snippet not trimmed: %d runes", len([]rune(s)))
	}
	if s := snippet("First line. The budget line.", tokenize("budget")); s != "The budget line." {
		t.Errorf("snippet = %q", s)
	}
}

// fakeEmbedder maps text onto a small bag-of-words vector, so related words land near each other.
type fakeEmbedder struct {
	mu         sync.Mutex
	configured bool
	failDocs   bool
	failQuery  bool
	docCalls   int
	queryCalls int
	floor      float64
	synonyms   map[string]string
}

func (f *fakeEmbedder) Model() string          { return "fake-embed" }
func (f *fakeEmbedder) Configured() bool       { return f.configured }
func (f *fakeEmbedder) MinSimilarity() float64 { return f.floor }

func (f *fakeEmbedder) vector(text string) []float32 {
	v := make([]float32, 64)
	for _, t := range tokenize(text) {
		if s, ok := f.synonyms[t]; ok {
			t = s
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(t))
		v[h.Sum32()%64]++
	}
	return v
}

func (f *fakeEmbedder) EmbedDocuments(_ context.Context, docs []Document) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docCalls++
	if f.failDocs {
		return nil, errors.New("boom")
	}
	out := make([][]float32, len(docs))
	for i, d := range docs {
		out[i] = f.vector(d.Title + " " + d.Text)
	}
	return out, nil
}

func (f *fakeEmbedder) EmbedQuery(_ context.Context, q string) ([]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryCalls++
	if f.failQuery {
		return nil, errors.New("boom")
	}
	return f.vector(q), nil
}

type memoryCache struct {
	mu      sync.Mutex
	vectors map[string][]float32
	loads   int
	pruned  []string
}

func (m *memoryCache) LoadHelpVectors(_ context.Context, _ string, hashes []string) (map[string][]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loads++
	out := map[string][]float32{}
	for _, h := range hashes {
		if v, ok := m.vectors[h]; ok {
			out[h] = v
		}
	}
	return out, nil
}

func (m *memoryCache) SaveHelpVectors(_ context.Context, _ string, vectors map[string][]float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, v := range vectors {
		m.vectors[h] = v
	}
	return nil
}

func (m *memoryCache) PruneHelpVectors(_ context.Context, _ string, keep []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruned = keep
	return nil
}

func TestSemanticSearchFindsMeaningAndCaches(t *testing.T) {
	lib := testLibrary(t)
	// "spending" means the same as "budget" to this embedder but shares no keyword with the article
	emb := &fakeEmbedder{configured: true, synonyms: map[string]string{"spending": "budget", "limit": "cap"}}
	cache := &memoryCache{vectors: map[string][]float32{}}
	ix := NewIndex(lib, emb, cache, nil)
	if !ix.Prepare(context.Background()) {
		t.Fatal("prepare failed")
	}
	if emb.docCalls != 1 || len(cache.vectors) != len(ix.chunks) || len(cache.pruned) != len(ix.chunks) {
		t.Fatalf("prepare: %d doc calls, %d cached, %d kept", emb.docCalls, len(cache.vectors), len(cache.pruned))
	}
	viewer := perms{"crews.view": true, "crews.run": true, "settings.view": true}
	got := ix.Search(context.Background(), viewer, "spending limit", 3)
	if got.Mode != ModeSemantic || len(got.Results) == 0 || got.Results[0].Slug != "settings" {
		t.Fatalf("semantic results = %+v", got)
	}
	ix.Search(context.Background(), viewer, "Spending limit", 3)
	if emb.queryCalls != 1 {
		t.Errorf("repeated query should be cached, got %d calls", emb.queryCalls)
	}

	// A new index (a restart) reuses the cached vectors instead of embedding again
	emb2 := &fakeEmbedder{configured: true}
	ix2 := NewIndex(lib, emb2, cache, nil)
	ix2.Prepare(context.Background())
	if emb2.docCalls != 0 || ix2.vectors == nil {
		t.Errorf("restart re-embedded: %d calls", emb2.docCalls)
	}
	if !ix2.Prepare(context.Background()) || cache.loads != 2 {
		t.Errorf("prepare after ready must be a no-op, loads = %d", cache.loads)
	}
}

func TestSemanticSearchFallsBack(t *testing.T) {
	lib := testLibrary(t)
	viewer := perms{"crews.view": true, "crews.run": true}

	unconfigured := NewIndex(lib, &fakeEmbedder{}, nil, nil)
	unconfigured.Prepare(context.Background())
	if got := unconfigured.Search(context.Background(), viewer, "budget", 3); got.Mode != ModeKeyword || len(got.Results) == 0 {
		t.Errorf("no key: %+v", got)
	}

	failing := &fakeEmbedder{configured: true, failDocs: true}
	ix := NewIndex(lib, failing, nil, nil)
	ix.Prepare(context.Background())
	if ix.vectors != nil || failing.docCalls != 1 {
		t.Fatal("failed prepare must leave keyword search")
	}
	if got := ix.Search(context.Background(), viewer, "budget", 3); got.Mode != ModeKeyword || len(got.Results) == 0 {
		t.Errorf("failed embeddings: %+v", got)
	}

	queryFails := &fakeEmbedder{configured: true, failQuery: true}
	ix = NewIndex(lib, queryFails, nil, nil)
	ix.Prepare(context.Background())
	if got := ix.Search(context.Background(), viewer, "budget", 3); got.Mode != ModeKeyword || len(got.Results) == 0 {
		t.Errorf("failed query embedding: %+v", got)
	}
}

func TestQueryCacheIsBounded(t *testing.T) {
	emb := &fakeEmbedder{configured: true}
	ix := NewIndex(testLibrary(t), emb, nil, nil)
	ix.queryLimiter.SetLimit(rate.Inf)
	ix.Prepare(context.Background())
	for i := range queryCacheLimit + 5 {
		if _, err := ix.queryVector(context.Background(), "query "+strings.Repeat("x", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(ix.queryCache) != queryCacheLimit || len(ix.queryOrder) != queryCacheLimit {
		t.Errorf("cache size %d/%d", len(ix.queryCache), len(ix.queryOrder))
	}
}

func TestVectorHelpers(t *testing.T) {
	if n := normalize([]float32{3, 4}); n[0] != 0.6 || n[1] != 0.8 {
		t.Errorf("normalize = %v", n)
	}
	if n := normalize([]float32{0, 0}); n[0] != 0 {
		t.Errorf("zero vector = %v", n)
	}
	if d := dot([]float32{1}, []float32{1, 2}); d > -1e300 {
		t.Errorf("mismatched lengths must rank last, got %v", d)
	}
}

func TestParseRejectsOpenByMistake(t *testing.T) {
	cases := map[string]string{
		"misspelt key":         "---\ntitle: x\ncategory: y\npermision: admin.users\n---\nbody",
		"empty permissions":    "---\ntitle: x\ncategory: y\npermissions:\n---\nbody",
		"empty section perms":  "---\ntitle: x\ncategory: y\n---\n## A\n<!-- permissions: -->\ntext",
		"heading with no text": "---\ntitle: x\ncategory: y\n---\n##\ntext",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(fstest.MapFS{"a.md": {Data: []byte(raw)}}); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestSectionsFencesAndIDs(t *testing.T) {
	raw := "---\ntitle: x\ncategory: y\n---\n## A\n~~~\n## inside tildes\n<!-- not a gate -->\n~~~\n## A\n## A 2\n````\n```\n## inside a longer fence\n````\n"
	lib, err := Load(fstest.MapFS{"a.md": {Data: []byte(raw)}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range lib.Articles()[0].Sections {
		ids = append(ids, s.ID)
	}
	if want := []string{"a", "a-2", "a-2-2"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if body := lib.Articles()[0].Sections[2].Body; !strings.Contains(body, "## inside a longer fence") {
		t.Errorf("longer fence was closed early: %q", body)
	}
}

func TestLinksIntoHiddenSectionsLoseTheirAnchor(t *testing.T) {
	lib, err := Load(fstest.MapFS{
		"crews.md": {Data: []byte(crewsArticle)},
		"start.md": {Data: []byte("---\ntitle: S\ncategory: Getting started\n---\nSee [running](/help/crews#run-a-crew) and [graph](/help/crews#read-the-graph).\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := lib.Article(perms{"crews.view": true}, "start")
	md := view.Sections[0].Markdown
	if strings.Contains(md, "#run-a-crew") || !strings.Contains(md, "[running](/help/crews)") || !strings.Contains(md, "(/help/crews#read-the-graph)") {
		t.Errorf("markdown = %q", md)
	}
}

func TestSemanticFloorAndBackoff(t *testing.T) {
	lib := testLibrary(t)
	viewer := perms{"crews.view": true, "crews.run": true, "settings.view": true}
	emb := &fakeEmbedder{configured: true, floor: 0.99}
	ix := NewIndex(lib, emb, nil, nil)
	ix.Prepare(context.Background())
	if got := ix.Search(context.Background(), viewer, "qwerty", 5); got.Mode != ModeSemantic || len(got.Results) != 0 {
		t.Errorf("a query unrelated to everything must return nothing: %+v", got)
	}

	now := time.Now()
	emb = &fakeEmbedder{configured: true, failQuery: true}
	ix = NewIndex(lib, emb, nil, nil)
	ix.now = func() time.Time { return now }
	ix.Prepare(context.Background())
	ix.Search(context.Background(), viewer, "budget", 5)
	ix.Search(context.Background(), viewer, "other words", 5)
	if emb.queryCalls != 1 {
		t.Errorf("after a failure searches must not call the provider for a while, got %d calls", emb.queryCalls)
	}
	now = now.Add(queryBackoff + time.Second)
	emb.failQuery = false
	if got := ix.Search(context.Background(), viewer, "budget", 5); got.Mode != ModeSemantic || emb.queryCalls != 2 {
		t.Errorf("after the backoff semantic search resumes: %+v, %d calls", got.Mode, emb.queryCalls)
	}
}

func TestQueryEmbeddingsAreLimitedGlobally(t *testing.T) {
	emb := &fakeEmbedder{configured: true}
	ix := NewIndex(testLibrary(t), emb, nil, nil)
	ix.Prepare(context.Background())
	keyword := 0
	for i := range queryEmbedBurst + 10 {
		if ix.Search(context.Background(), perms{"crews.view": true}, fmt.Sprintf("crews %d", i), 3).Mode == ModeKeyword {
			keyword++
		}
	}
	if keyword == 0 || emb.queryCalls > queryEmbedBurst+1 {
		t.Errorf("%d keyword fallbacks, %d provider calls", keyword, emb.queryCalls)
	}
}

func TestRunRetriesUntilReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	NewIndex(testLibrary(t), nil, nil, nil).Run(ctx) // no embedder: returns at once

	emb := &fakeEmbedder{configured: true}
	ix := NewIndex(testLibrary(t), emb, nil, nil)
	ix.Run(ctx)
	if ix.semanticVectors() == nil {
		t.Error("Run should leave semantic search ready")
	}

	failing := NewIndex(testLibrary(t), &fakeEmbedder{configured: true, failDocs: true}, nil, nil)
	done := make(chan struct{})
	go func() { failing.Run(ctx); close(done) }()
	cancel()
	<-done
}
