package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"crewstudio/internal/model"
)

// fakeEmbedder returns [len(text), 1] (or [len, 2] for queries) and counts the texts it embedded.
type fakeEmbedder struct {
	mu         sync.Mutex
	configured bool
	model      string
	texts      []string
	err        error
	short      bool
	bad        bool
}

func (f *fakeEmbedder) Model() string    { return f.model }
func (f *fakeEmbedder) Configured() bool { return f.configured }
func (f *fakeEmbedder) EmbedTexts(_ context.Context, texts []string, query bool) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.texts = append(f.texts, texts...)
	out := make([][]float32, len(texts))
	for i, t := range texts {
		second := float32(1)
		if query {
			second = 2
		}
		out[i] = []float32{float32(len(t)), second}
		if f.bad {
			out[i] = []float32{}
		}
	}
	if f.short {
		out = out[:len(out)-1]
	}
	return out, nil
}

func newFake() *fakeEmbedder { return &fakeEmbedder{configured: true, model: "fake:2"} }

func TestAvailable(t *testing.T) {
	var nilSvc *Service
	if ok, reason := nilSvc.Available(); ok || !strings.Contains(reason, "STUDIO_MEMORY_EMBEDDINGS") {
		t.Errorf("nil = %v %q", ok, reason)
	}
	if nilSvc.Model() != "" {
		t.Error("nil service has no model")
	}
	nilSvc.Forget("x")
	if ok, reason := NewService(nil, Options{Off: "off here"}, nil, nil).Available(); ok || reason != "off here" {
		t.Errorf("off = %v %q", ok, reason)
	}
	f := newFake()
	f.configured = false
	s := NewService(f, Options{Unconfigured: "needs OPENAI_API_KEY"}, nil, nil)
	if ok, reason := s.Available(); ok || reason != "needs OPENAI_API_KEY" {
		t.Errorf("unconfigured = %v %q", ok, reason)
	}
	if _, err := s.Embed(context.Background(), "r", []string{"x"}, false, nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
	if n, err := s.ReembedOnce(context.Background()); n != 0 || err != nil {
		t.Errorf("reembed unavailable = %d %v", n, err)
	}
	if ok, _ := NewService(newFake(), Options{}, nil, nil).Available(); !ok {
		t.Error("configured embedder should be available")
	}
}

func TestEmbedCachesPerRunAndReserves(t *testing.T) {
	f := newFake()
	s := NewService(f, Options{}, nil, nil)
	ctx := context.Background()
	var reserved []int
	reserve := func(n int) error { reserved = append(reserved, n); return nil }

	v, err := s.Embed(ctx, "run1", []string{"aa", "bbb", "aa"}, false, reserve)
	if err != nil || len(v) != 3 || v[0][0] != 2 || v[1][0] != 3 || v[2][0] != 2 {
		t.Fatalf("v = %v, %v", v, err)
	}
	// The same texts again come from the cache; a query of the same text is a different vector
	v, err = s.Embed(ctx, "run1", []string{"bbb", "aa"}, false, reserve)
	if err != nil || v[0][0] != 3 {
		t.Fatal(v, err)
	}
	q, _ := s.Embed(ctx, "run1", []string{"aa"}, true, reserve)
	if q[0][1] != 2 {
		t.Errorf("query vector = %v", q)
	}
	if strings.Join(f.texts, ",") != "aa,bbb,aa" || len(reserved) != 3 || reserved[0] != 2 || reserved[1] != 0 || reserved[2] != 1 {
		t.Errorf("embedded %v, reserved %v", f.texts, reserved)
	}
	// Another run, and people (no run), do not share the cache
	_, _ = s.Embed(ctx, "run2", []string{"aa"}, false, nil)
	_, _ = s.Embed(ctx, "", []string{"aa"}, false, nil)
	_, _ = s.Embed(ctx, "", []string{"aa"}, false, nil)
	if len(f.texts) != 6 {
		t.Errorf("embedded %v", f.texts)
	}
	s.Forget("run1")
	_, _ = s.Embed(ctx, "run1", []string{"aa"}, false, nil)
	if len(f.texts) != 7 {
		t.Errorf("forgotten run should embed again: %v", f.texts)
	}

	// A refused reservation stops the call
	stop := errors.New("cap")
	if _, err := s.Embed(ctx, "run3", []string{"new"}, false, func(int) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("err = %v", err)
	}
}

func TestEmbedProviderFailures(t *testing.T) {
	ctx := context.Background()
	for name, f := range map[string]*fakeEmbedder{
		"error": {configured: true, model: "m", err: errors.New("HTTP 500")},
		"short": {configured: true, model: "m", short: true},
		"empty": {configured: true, model: "m", bad: true},
	} {
		_, err := NewService(f, Options{}, nil, nil).Embed(ctx, "r", []string{"a", "b"}, false, nil)
		var pe *ProviderError
		if !errors.As(err, &pe) || pe.Unwrap() == nil || !strings.HasPrefix(err.Error(), "memory embeddings:") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A cancelled caller waiting on the rate limit is told Studio is busy
	s := NewService(newFake(), Options{}, nil, nil)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	for i := 0; i < embedsPerSecond; i++ {
		s.limiter.Allow()
	}
	if _, err := s.Embed(cctx, "r", []string{"x"}, false, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v", err)
	}
}

func TestCacheBounds(t *testing.T) {
	c := newCache()
	now := time.Now()
	c.now = func() time.Time { return now }
	for i := 0; i < cachePerRun+5; i++ {
		c.put("r", string(rune('a'+i%26))+strings.Repeat("x", i), []float32{1})
	}
	if len(c.runs["r"].vectors) != cachePerRun {
		t.Errorf("per run = %d", len(c.runs["r"].vectors))
	}
	for i := 0; i < cacheRuns+3; i++ {
		now = now.Add(time.Second)
		c.put(strings.Repeat("r", i+2), "k", []float32{1})
	}
	if len(c.runs) > cacheRuns {
		t.Errorf("runs = %d", len(c.runs))
	}
	// Idle runs expire
	now = now.Add(cacheIdleExpiry + time.Minute)
	c.put("fresh", "k", []float32{1})
	if len(c.runs) != 1 {
		t.Errorf("after idle = %d", len(c.runs))
	}
	if _, ok := c.get("missing", "k"); ok {
		t.Error("missing run hit")
	}
}

type fakeReembedStore struct {
	rows    []model.MemoryRecord
	set     map[string][]float32
	listErr error
	setErr  error
	asked   string
}

func (f *fakeReembedStore) MemoriesToReembed(_ context.Context, embedModel string, limit int) ([]model.MemoryRecord, error) {
	f.asked = embedModel
	if len(f.rows) > limit {
		return f.rows[:limit], f.listErr
	}
	return f.rows, f.listErr
}

func (f *fakeReembedStore) SetMemoryVector(_ context.Context, id, content, embedModel string, v []float32) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.set[id+"|"+content+"|"+embedModel] = v
	return nil
}

func TestReembedOnce(t *testing.T) {
	st := &fakeReembedStore{set: map[string][]float32{}}
	for i := 0; i < 40; i++ {
		st.rows = append(st.rows, model.MemoryRecord{ID: string(rune('A' + i)), Content: strings.Repeat("y", i+1)})
	}
	f := newFake()
	s := NewService(f, Options{}, st, nil)
	n, err := s.ReembedOnce(context.Background())
	if err != nil || n != 40 || st.asked != "fake:2" || len(st.set) != 40 {
		t.Fatalf("n = %d, err = %v, set = %d", n, err, len(st.set))
	}
	if v := st.set["A|y|fake:2"]; len(v) != 2 || v[1] != 1 {
		t.Errorf("document vector = %v", v)
	}
	st.setErr = errors.New("db down")
	if _, err := s.ReembedOnce(context.Background()); err == nil {
		t.Error("store error should surface")
	}
	f.err = errors.New("provider down")
	if _, err := s.ReembedOnce(context.Background()); err == nil {
		t.Error("provider error should surface")
	}
	st.rows = nil
	if n, err := s.ReembedOnce(context.Background()); n != 0 || err != nil {
		t.Errorf("nothing to do = %d %v", n, err)
	}

	// Run does one pass straight away and stops with its context
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	st.listErr = errors.New("db down")
	go func() { s.Run(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
}

type calibrated struct{ fakeEmbedder }

func (*calibrated) MinSimilarity() float64           { return 0.42 }
func (*calibrated) NearDuplicateSimilarity() float64 { return 0.95 }

func TestMinSimilarity(t *testing.T) {
	var nilSvc *Service
	if nilSvc.MinSimilarity() != defaultMinSimilarity || NewService(newFake(), Options{}, nil, nil).MinSimilarity() != defaultMinSimilarity {
		t.Error("default floor")
	}
	if got := NewService(&calibrated{}, Options{}, nil, nil).MinSimilarity(); got != 0.42 {
		t.Errorf("calibrated = %v", got)
	}
	if nilSvc.NearDuplicateSimilarity() != NearDuplicateThreshold || NewService(newFake(), Options{}, nil, nil).NearDuplicateSimilarity() != NearDuplicateThreshold {
		t.Error("default near-duplicate threshold")
	}
	if got := NewService(&calibrated{}, Options{}, nil, nil).NearDuplicateSimilarity(); got != 0.95 {
		t.Errorf("calibrated near-duplicate = %v", got)
	}
}
