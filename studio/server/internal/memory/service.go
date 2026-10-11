package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"crewstudio/internal/model"
)

const (
	// embedsPerSecond bounds embedding calls across all of Studio (every run and person together)
	embedsPerSecond = 20
	// MaxEmbedBatch is the most texts one embed request may carry
	MaxEmbedBatch = 32
	// reembedPerTick is how many memories of an old embedder are re-embedded each reembedEvery (7 provider calls of
	// MaxEmbedBatch, well within embedsPerSecond)
	reembedPerTick = 200
	reembedEvery   = time.Minute
	// cache bounds: runs remembered, vectors per run, and how long a quiet run's vectors are kept
	cacheRuns       = 256
	cachePerRun     = 600
	cacheIdleExpiry = 6 * time.Hour
)

var (
	// ErrUnavailable means the server has no embedder for memory (off, or the provider key is missing)
	ErrUnavailable = errors.New("memory embeddings are not available")
	// ErrBusy means too many embedding calls are waiting across Studio
	ErrBusy = errors.New("too many memory embedding calls just now")
)

// ProviderError is a failed call to the embedding provider. Its message is for the service log, not for runs.
type ProviderError struct{ Err error }

func (e *ProviderError) Error() string { return "memory embeddings: " + e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

// Embedder turns texts into vectors. query is true for a search query, false for a memory to store (Gemini
// embeds the two differently). The help embedders (internal/help) implement it.
type Embedder interface {
	// Model names the model and vector size, such as text-embedding-3-small:768
	Model() string
	// Configured reports whether the embedder has its API key
	Configured() bool
	EmbedTexts(ctx context.Context, texts []string, query bool) ([][]float32, error)
}

// ReembedStore is what the re-embed worker needs from the store.
type ReembedStore interface {
	MemoriesToReembed(ctx context.Context, embedModel string, limit int) ([]model.MemoryRecord, error)
	SetMemoryVector(ctx context.Context, id, content, embedModel string, vector []float32) error
}

const (
	defaultOff          = "Memory is off on this server. An administrator can set STUDIO_MEMORY_EMBEDDINGS to openai or gemini."
	defaultUnconfigured = "Memory needs the embedding provider's API key in the provider .env file."
)

// Options say why memory is unavailable, for the Memory tab.
type Options struct {
	// Off is shown when there is no embedder (STUDIO_MEMORY_EMBEDDINGS=off)
	Off string
	// Unconfigured is shown when the embedder has no API key
	Unconfigured string
}

// Service embeds memories and queries for the run-state API and the Memory tab, with a per-run cache and one rate
// limit for all of Studio, and re-embeds old memories in the background (Run).
type Service struct {
	embedder Embedder
	opts     Options
	store    ReembedStore
	log      *slog.Logger
	limiter  *rate.Limiter
	cache    *cache
}

// NewService wraps e, which may be nil (memory unavailable). st and log may be nil when Run is not used.
func NewService(e Embedder, opts Options, st ReembedStore, log *slog.Logger) *Service {
	if opts.Off == "" {
		opts.Off = defaultOff
	}
	if opts.Unconfigured == "" {
		opts.Unconfigured = defaultUnconfigured
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{embedder: e, opts: opts, store: st, log: log,
		limiter: rate.NewLimiter(rate.Limit(embedsPerSecond), embedsPerSecond), cache: newCache()}
}

// Available reports whether memory can embed, and why not. A nil Service is unavailable.
func (s *Service) Available() (bool, string) {
	switch {
	case s == nil:
		return false, defaultOff
	case s.embedder == nil:
		return false, s.opts.Off
	case !s.embedder.Configured():
		return false, s.opts.Unconfigured
	}
	return true, ""
}

// Model is the embedder's model, or "" without one.
func (s *Service) Model() string {
	if s == nil || s.embedder == nil {
		return ""
	}
	return s.embedder.Model()
}

// defaultMinSimilarity is the search floor for embedders that do not say their own
const defaultMinSimilarity = 0.3

// MinSimilarity is the cosine score below which a memory is unrelated to a person's search on the Memory tab: the
// embedder's own calibration (help search) when it has one.
func (s *Service) MinSimilarity() float64 {
	if s != nil {
		if m, ok := s.embedder.(interface{ MinSimilarity() float64 }); ok {
			return m.MinSimilarity()
		}
	}
	return defaultMinSimilarity
}

// NearDuplicateSimilarity is the cosine score from which a new memory says the same as an existing one in its scope:
// the embedder's own (embedders score paraphrases differently) or NearDuplicateThreshold.
func (s *Service) NearDuplicateSimilarity() float64 {
	if s != nil {
		if m, ok := s.embedder.(interface{ NearDuplicateSimilarity() float64 }); ok {
			return m.NearDuplicateSimilarity()
		}
	}
	return NearDuplicateThreshold
}

// Embed returns one vector per text. With a runID, vectors are cached for that run, and reserve (when not nil) is
// told how many texts need the provider before it is called; its error stops the call.
func (s *Service) Embed(ctx context.Context, runID string, texts []string, query bool, reserve func(misses int) error) ([][]float32, error) {
	if ok, _ := s.Available(); !ok {
		return nil, ErrUnavailable
	}
	embedModel := s.embedder.Model()
	keys := make([]string, len(texts))
	out := make([][]float32, len(texts))
	var missing []string
	seen := map[string]bool{}
	for i, t := range texts {
		keys[i] = cacheKey(embedModel, query, t)
		if v, hit := s.cache.get(runID, keys[i]); hit {
			out[i] = v
			continue
		}
		if !seen[keys[i]] {
			seen[keys[i]] = true
			missing = append(missing, t)
		}
	}
	if reserve != nil {
		if err := reserve(len(missing)); err != nil {
			return nil, err
		}
	}
	if len(missing) > 0 {
		fresh, err := s.call(ctx, missing, query)
		if err != nil {
			return nil, err
		}
		for i, t := range missing {
			s.cache.put(runID, cacheKey(embedModel, query, t), fresh[i])
		}
		byKey := map[string][]float32{}
		for i, t := range missing {
			byKey[cacheKey(embedModel, query, t)] = fresh[i]
		}
		for i := range texts {
			if out[i] == nil {
				out[i] = byKey[keys[i]]
			}
		}
	}
	return out, nil
}

// call asks the provider, within the rate limit, and checks what came back.
func (s *Service) call(ctx context.Context, texts []string, query bool) ([][]float32, error) {
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.limiter.Wait(wait); err != nil {
		return nil, ErrBusy
	}
	vectors, err := s.embedder.EmbedTexts(ctx, texts, query)
	if err != nil {
		return nil, &ProviderError{Err: err}
	}
	if len(vectors) != len(texts) {
		return nil, &ProviderError{Err: fmt.Errorf("got %d vectors for %d texts", len(vectors), len(texts))}
	}
	for i, v := range vectors {
		if !ValidVector(v) {
			return nil, &ProviderError{Err: fmt.Errorf("vector %d is empty or not finite", i)}
		}
	}
	return vectors, nil
}

// Forget drops a finished run's cached vectors.
func (s *Service) Forget(runID string) {
	if s != nil {
		s.cache.forget(runID)
	}
}

// Run re-embeds memories stored with an older embedder, reembedPerTick a minute, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(reembedEvery)
	defer ticker.Stop()
	for {
		if _, err := s.ReembedOnce(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("re-embed memories", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReembedOnce re-embeds up to reembedPerTick memories of another embedder and returns how many it updated.
func (s *Service) ReembedOnce(ctx context.Context) (int, error) {
	if ok, _ := s.Available(); !ok || s.store == nil {
		return 0, nil
	}
	embedModel := s.embedder.Model()
	rows, err := s.store.MemoriesToReembed(ctx, embedModel, reembedPerTick)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	done := 0
	for start := 0; start < len(rows); start += MaxEmbedBatch {
		batch := rows[start:min(start+MaxEmbedBatch, len(rows))]
		texts := make([]string, len(batch))
		for i, r := range batch {
			texts[i] = r.Content
		}
		vectors, err := s.call(ctx, texts, false)
		if err != nil {
			return done, err
		}
		for i, r := range batch {
			if err := s.store.SetMemoryVector(ctx, r.ID, r.Content, embedModel, vectors[i]); err != nil {
				return done, err
			}
			done++
		}
	}
	return done, nil
}

func cacheKey(embedModel string, query bool, text string) string {
	kind := "d"
	if query {
		kind = "q"
	}
	sum := sha256.Sum256([]byte(text))
	return embedModel + "\x00" + kind + "\x00" + hex.EncodeToString(sum[:])
}

// cache keeps each run's vectors so the same text is embedded (and counted against the run's cap) once.
type cache struct {
	mu   sync.Mutex
	runs map[string]*runVectors
	now  func() time.Time
}

type runVectors struct {
	vectors map[string][]float32
	used    time.Time
}

func newCache() *cache {
	return &cache{runs: map[string]*runVectors{}, now: time.Now}
}

func (c *cache) get(runID, key string) ([]float32, bool) {
	if runID == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rv := c.runs[runID]
	if rv == nil {
		return nil, false
	}
	rv.used = c.now()
	v, ok := rv.vectors[key]
	return v, ok
}

func (c *cache) put(runID, key string, v []float32) {
	if runID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	rv := c.runs[runID]
	if rv == nil {
		c.evict(now)
		rv = &runVectors{vectors: map[string][]float32{}}
		c.runs[runID] = rv
	}
	rv.used = now
	if len(rv.vectors) < cachePerRun {
		rv.vectors[key] = v
	}
}

// evict drops idle runs, then the least recently used one while the cache is full. The caller holds mu.
func (c *cache) evict(now time.Time) {
	var oldest string
	for id, rv := range c.runs {
		if now.Sub(rv.used) > cacheIdleExpiry {
			delete(c.runs, id)
			continue
		}
		if oldest == "" || rv.used.Before(c.runs[oldest].used) {
			oldest = id
		}
	}
	if len(c.runs) >= cacheRuns && oldest != "" {
		delete(c.runs, oldest)
	}
}

func (c *cache) forget(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.runs, runID)
}
