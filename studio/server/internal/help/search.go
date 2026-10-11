package help

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

const (
	// Results with the same rank in both lists score highest; 60 is the usual reciprocal-rank-fusion constant
	rrfK = 60
	// Vector matches further than this below the best cosine score are dropped as unrelated
	vectorGap        = 0.12
	vectorCandidates = 20
	snippetRunes     = 220
	// A failed embedding of the articles is retried after this, so a missing key added later starts working
	retryAfter      = 5 * time.Minute
	queryTimeout    = 6 * time.Second
	embedBatchSize  = 50
	queryCacheLimit = 256
	// After a failed query embedding, searches use keywords for this long instead of each waiting on the provider
	queryBackoff = 30 * time.Second
	// Query embeddings across everyone, on top of the per-person API limit, so many accounts cannot run up a bill
	queryEmbedsPerSecond = 10
	queryEmbedBurst      = 30
	bm25K1               = 1.2
	bm25B                = 0.75

	ModeSemantic = "semantic"
	ModeKeyword  = "keyword"
)

var (
	wordPattern = regexp.MustCompile(`[\p{L}\p{N}]+`)
	// Markdown punctuation becomes a space so "**bold**text" or "|a|b|" do not fuse into one word
	markdownNoise   = regexp.MustCompile("[`*_>#|~]+")
	whitespace      = regexp.MustCompile(`\s+`)
	markdownLink    = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	sentenceBoundry = regexp.MustCompile(`(?:[.!?]\s+|\n+)`)
	stopWords       = map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true, "can": true,
		"do": true, "does": true, "for": true, "from": true, "how": true, "i": true, "in": true, "is": true, "it": true,
		"me": true, "my": true, "of": true, "on": true, "or": true, "the": true, "to": true, "what": true, "when": true,
		"where": true, "which": true, "who": true, "why": true, "with": true, "you": true, "your": true,
	}
)

// Embedder turns text into vectors. Documents and queries are embedded differently for asymmetric retrieval.
type Embedder interface {
	// Model names the model and vector size; cached vectors are keyed by it
	Model() string
	// MinSimilarity is the cosine score below which even the best match is treated as unrelated
	MinSimilarity() float64
	// Configured reports whether the embedder has what it needs (an API key) to be tried at all
	Configured() bool
	EmbedDocuments(ctx context.Context, docs []Document) ([][]float32, error)
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// Document is one section to embed.
type Document struct {
	Title string
	Text  string
}

// VectorCache keeps document vectors between restarts, keyed by model and content hash.
type VectorCache interface {
	LoadHelpVectors(ctx context.Context, model string, hashes []string) (map[string][]float32, error)
	SaveHelpVectors(ctx context.Context, model string, vectors map[string][]float32) error
	PruneHelpVectors(ctx context.Context, model string, keep []string) error
}

// Result is one search hit: a section of an article.
type Result struct {
	Slug         string  `json:"slug"`
	ArticleTitle string  `json:"article_title"`
	Category     string  `json:"category"`
	SectionID    string  `json:"section_id"`
	SectionTitle string  `json:"section_title"`
	Snippet      string  `json:"snippet"`
	Score        float64 `json:"score"`
}

// Results is a search response.
type Results struct {
	Query   string   `json:"query"`
	Mode    string   `json:"mode"`
	Results []Result `json:"results"`
}

type chunk struct {
	article *Article
	section *Section
	plain   string
	terms   map[string]int
	length  int
	hash    string
}

// Index searches a Library by keyword (BM25) and, when an embedder is available, by meaning (cosine over
// embeddings), merging the two rankings. Only sections the viewer may see are ranked.
type Index struct {
	lib      *Library
	embedder Embedder
	cache    VectorCache
	log      *slog.Logger

	chunks  []chunk
	docFreq map[string]int
	avgLen  float64

	queryLimiter *rate.Limiter
	flight       singleflight.Group
	now          func() time.Time

	mu         sync.Mutex
	vectors    [][]float32
	preparing  bool
	failUntil  time.Time
	queryCache map[string][]float32
	queryOrder []string
}

// NewIndex builds the keyword index; embedder and cache may be nil for keyword-only search.
func NewIndex(lib *Library, embedder Embedder, cache VectorCache, log *slog.Logger) *Index {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ix := &Index{
		lib: lib, embedder: embedder, cache: cache, log: log, docFreq: map[string]int{}, queryCache: map[string][]float32{},
		queryLimiter: rate.NewLimiter(queryEmbedsPerSecond, queryEmbedBurst), now: time.Now,
	}
	model := ""
	if embedder != nil {
		model = embedder.Model()
	}
	total := 0
	for i := range lib.articles {
		a := &lib.articles[i]
		for j := range a.Sections {
			s := &a.Sections[j]
			plain := plainText(s.Body)
			indexed := strings.Join([]string{a.Title, a.Title, s.Title, s.Title, plain}, "\n")
			if s.ID == IntroID {
				indexed += "\n" + a.Summary + "\n" + strings.Join(a.Keywords, " ")
			}
			terms := map[string]int{}
			n := 0
			for _, t := range tokenize(indexed) {
				terms[t]++
				n++
			}
			for t := range terms {
				ix.docFreq[t]++
			}
			total += n
			sum := sha256.Sum256([]byte(model + "\x00" + documentTitle(a, s) + "\x00" + plain))
			ix.chunks = append(ix.chunks, chunk{article: a, section: s, plain: plain, terms: terms, length: n, hash: hex.EncodeToString(sum[:])})
		}
	}
	if len(ix.chunks) > 0 {
		ix.avgLen = float64(total) / float64(len(ix.chunks))
	}
	return ix
}

// Library returns the articles the index searches.
func (ix *Index) Library() *Library { return ix.lib }

func documentTitle(a *Article, s *Section) string {
	if s.Title == "" {
		return a.Title
	}
	return a.Title + " — " + s.Title
}

func plainText(md string) string {
	md = markdownLink.ReplaceAllString(md, "$1")
	md = markdownNoise.ReplaceAllString(md, " ")
	// Keep line breaks (snippets split sentences on them) but collapse runs of spaces
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(whitespace.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func tokenize(s string) []string {
	words := wordPattern.FindAllString(strings.ToLower(s), -1)
	out := words[:0]
	for _, w := range words {
		if stopWords[w] {
			continue
		}
		out = append(out, stem(w))
	}
	return out
}

// stem is a deliberately light suffix strip so "schedule", "schedules", "scheduled" and "scheduling" meet,
// as do "run", "runs" and "running". Documents and queries go through the same steps, so it only has to be
// consistent, not linguistically right.
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		w = w[:len(w)-3] + "y"
	case len(w) > 5 && strings.HasSuffix(w, "ing"):
		w = undouble(w[:len(w)-3])
	case len(w) > 4 && strings.HasSuffix(w, "ed"):
		w = undouble(w[:len(w)-2])
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		w = w[:len(w)-1]
	}
	if len(w) > 3 && strings.HasSuffix(w, "e") {
		w = w[:len(w)-1]
	}
	return w
}

// undouble turns "runn" (from "running") back into "run"; "ll", "ss" and "zz" stay ("called", "passed").
func undouble(w string) string {
	n := len(w)
	if n >= 3 && w[n-1] == w[n-2] && !strings.ContainsRune("lsz", rune(w[n-1])) {
		return w[:n-1]
	}
	return w
}

// Prepare embeds every section (reusing cached vectors) so semantic search can start, and reports whether
// it is ready. It is safe to call repeatedly.
func (ix *Index) Prepare(ctx context.Context) bool {
	if ix.embedder == nil || !ix.embedder.Configured() {
		return false
	}
	ix.mu.Lock()
	if ix.vectors != nil || ix.preparing {
		ready := ix.vectors != nil
		ix.mu.Unlock()
		return ready
	}
	ix.preparing = true
	ix.mu.Unlock()

	vectors, err := ix.embedAll(ctx)
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.preparing = false
	if err != nil {
		ix.log.Warn("help search: embedding the articles failed; searching by keyword until it works", "err", err)
		return false
	}
	ix.vectors = vectors
	ix.log.Info("help search: semantic search ready", "model", ix.embedder.Model(), "sections", len(vectors))
	return true
}

// Run prepares semantic search and, until that works (no key yet, provider down), retries every few minutes.
// It returns when search is ready, there is no embedder, or ctx ends.
func (ix *Index) Run(ctx context.Context) {
	if ix.embedder == nil {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ix.Prepare(ctx) {
			return
		}
		timer.Reset(retryAfter)
	}
}

func (ix *Index) embedAll(ctx context.Context) ([][]float32, error) {
	model := ix.embedder.Model()
	hashes := make([]string, len(ix.chunks))
	for i, c := range ix.chunks {
		hashes[i] = c.hash
	}
	cached := map[string][]float32{}
	if ix.cache != nil {
		var err error
		if cached, err = ix.cache.LoadHelpVectors(ctx, model, hashes); err != nil {
			ix.log.Warn("help search: reading cached vectors failed", "err", err)
			cached = map[string][]float32{}
		}
	}
	vectors := make([][]float32, len(ix.chunks))
	var missing []int
	for i, c := range ix.chunks {
		if v, ok := cached[c.hash]; ok {
			vectors[i] = v
		} else {
			missing = append(missing, i)
		}
	}
	fresh := map[string][]float32{}
	for start := 0; start < len(missing); start += embedBatchSize {
		batch := missing[start:min(start+embedBatchSize, len(missing))]
		docs := make([]Document, len(batch))
		for k, i := range batch {
			c := ix.chunks[i]
			docs[k] = Document{Title: documentTitle(c.article, c.section), Text: c.plain}
		}
		got, err := ix.embedder.EmbedDocuments(ctx, docs)
		if err != nil {
			return nil, err
		}
		if len(got) != len(batch) {
			return nil, errors.New("embedding response has the wrong number of vectors")
		}
		for k, i := range batch {
			vectors[i] = normalize(got[k])
			fresh[ix.chunks[i].hash] = vectors[i]
		}
	}
	if len(fresh) > 0 && ix.cache != nil {
		if err := ix.cache.SaveHelpVectors(ctx, model, fresh); err != nil {
			ix.log.Warn("help search: caching vectors failed", "err", err)
		} else if err := ix.cache.PruneHelpVectors(ctx, model, hashes); err != nil {
			ix.log.Warn("help search: pruning old vectors failed", "err", err)
		}
	}
	return vectors, nil
}

// semanticVectors returns the section vectors, or nil while semantic search is unavailable or backing off.
func (ix *Index) semanticVectors() [][]float32 {
	if ix.embedder == nil || !ix.embedder.Configured() {
		return nil
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.now().Before(ix.failUntil) {
		return nil
	}
	return ix.vectors
}

func queryKey(q string) string {
	return strings.ToLower(strings.TrimSpace(whitespace.ReplaceAllString(q, " ")))
}

// queryVector embeds a query once: repeats come from a small cache and simultaneous identical queries share
// one call. Failures switch semantic search off briefly so searches do not each wait on a failing provider.
func (ix *Index) queryVector(ctx context.Context, q string) ([]float32, error) {
	key := queryKey(q)
	ix.mu.Lock()
	if v, ok := ix.queryCache[key]; ok {
		ix.mu.Unlock()
		return v, nil
	}
	ix.mu.Unlock()
	v, err, _ := ix.flight.Do(key, func() (any, error) {
		if !ix.queryLimiter.Allow() {
			return nil, errors.New("query embedding limit reached")
		}
		// Not the request's context: a caller giving up must not fail the others sharing this call
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
		defer cancel()
		v, err := ix.embedder.EmbedQuery(cctx, key)
		if err != nil {
			ix.mu.Lock()
			ix.failUntil = ix.now().Add(queryBackoff)
			ix.mu.Unlock()
			return nil, err
		}
		v = normalize(v)
		ix.mu.Lock()
		defer ix.mu.Unlock()
		if _, ok := ix.queryCache[key]; !ok {
			if len(ix.queryOrder) >= queryCacheLimit {
				delete(ix.queryCache, ix.queryOrder[0])
				ix.queryOrder = ix.queryOrder[1:]
			}
			ix.queryOrder = append(ix.queryOrder, key)
		}
		ix.queryCache[key] = v
		return v, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]float32), nil
}

// Search ranks the sections the viewer may see. It never fails: if the embedder is unavailable the
// keyword ranking is returned and Mode says so.
func (ix *Index) Search(ctx context.Context, v Viewer, query string, limit int) Results {
	out := Results{Query: query, Mode: ModeKeyword, Results: []Result{}}
	visible := make([]int, 0, len(ix.chunks))
	for i, c := range ix.chunks {
		if c.article.sectionVisible(v, c.section) {
			visible = append(visible, i)
		}
	}
	terms := tokenize(query)
	keyword := ix.rankKeyword(visible, terms)
	fused := map[int]float64{}
	for rank, i := range keyword {
		fused[i] += 1.0 / float64(rrfK+rank+1)
	}
	if vectors := ix.semanticVectors(); vectors != nil {
		qv, err := ix.queryVector(ctx, query)
		if err != nil {
			ix.log.Warn("help search: embedding the query failed; using keyword search", "err", err)
		} else {
			out.Mode = ModeSemantic
			for rank, i := range rankVector(visible, vectors, qv, ix.embedder.MinSimilarity()) {
				fused[i] += 1.0 / float64(rrfK+rank+1)
			}
		}
	}
	ranked := make([]int, 0, len(fused))
	for i := range fused {
		ranked = append(ranked, i)
	}
	slices.SortFunc(ranked, func(a, b int) int {
		if fused[a] != fused[b] {
			if fused[a] > fused[b] {
				return -1
			}
			return 1
		}
		return a - b
	})
	for _, i := range ranked[:min(limit, len(ranked))] {
		c := ix.chunks[i]
		out.Results = append(out.Results, Result{
			Slug: c.article.Slug, ArticleTitle: c.article.Title, Category: c.article.Category,
			SectionID: c.section.ID, SectionTitle: c.section.Title,
			Snippet: snippet(c.plain, terms), Score: math.Round(fused[i]*1e5) / 1e5,
		})
	}
	return out
}

// rankKeyword returns the visible chunks that match any term, best BM25 score first.
func (ix *Index) rankKeyword(visible []int, terms []string) []int {
	n := float64(len(ix.chunks))
	scores := map[int]float64{}
	for _, i := range visible {
		c := ix.chunks[i]
		score := 0.0
		for _, t := range terms {
			tf := float64(c.terms[t])
			if tf == 0 {
				continue
			}
			df := float64(ix.docFreq[t])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			score += idf * tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*float64(c.length)/ix.avgLen))
		}
		if score > 0 {
			scores[i] = score
		}
	}
	return sortByScore(scores)
}

// rankVector returns the closest visible chunks by cosine similarity. Nothing is returned when even the best
// match is below floor (the query is unrelated to anything), and matches far below the best are dropped.
func rankVector(visible []int, vectors [][]float32, q []float32, floor float64) []int {
	scores := map[int]float64{}
	best := math.Inf(-1)
	for _, i := range visible {
		if vectors[i] == nil {
			continue
		}
		s := dot(vectors[i], q)
		if math.IsInf(s, -1) {
			continue
		}
		scores[i] = s
		best = math.Max(best, s)
	}
	if best < floor {
		return nil
	}
	for i, s := range scores {
		if s < best-vectorGap || s < floor {
			delete(scores, i)
		}
	}
	ranked := sortByScore(scores)
	return ranked[:min(vectorCandidates, len(ranked))]
}

func sortByScore(scores map[int]float64) []int {
	out := make([]int, 0, len(scores))
	for i := range scores {
		out = append(out, i)
	}
	slices.SortFunc(out, func(a, b int) int {
		if scores[a] > scores[b] {
			return -1
		}
		if scores[a] < scores[b] {
			return 1
		}
		return a - b
	})
	return out
}

func dot(a, b []float32) float64 {
	if len(a) != len(b) {
		return math.Inf(-1)
	}
	s := 0.0
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func normalize(v []float32) []float32 {
	sum := 0.0
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	norm := math.Sqrt(sum)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}

// snippet picks the sentence with the most query terms (or the opening) and trims it to length.
func snippet(plain string, terms []string) string {
	sentences := sentenceBoundry.Split(plain, -1)
	best, bestHits := "", -1
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		hits := 0
		words := map[string]bool{}
		for _, t := range tokenize(s) {
			words[t] = true
		}
		for _, t := range terms {
			if words[t] {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = s, hits
		}
	}
	runes := []rune(best)
	if len(runes) > snippetRunes {
		return strings.TrimSpace(string(runes[:snippetRunes])) + "…"
	}
	return best
}
