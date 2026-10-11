package help

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// GeminiModel embeds text, images and documents into one space; text-only use needs no task_type parameter,
	// the task goes in the prompt instead (see GeminiEmbedder.EmbedQuery)
	GeminiModel      = "gemini-embedding-2"
	geminiBaseURL    = "https://generativelanguage.googleapis.com/v1beta"
	geminiDimensions = 768
	// 8,192 input tokens is the model limit; sections are far shorter, but cap the bytes sent anyway
	maxEmbedChars       = 24000
	maxErrorBody        = 300
	geminiKeyEnvVar     = "GEMINI_API_KEY"
	geminiMinSimilarity = 0.5
	// geminiNearDuplicate is higher than OpenAI's because Gemini scores related but different text higher (not
	// calibrated against live memories)
	geminiNearDuplicate = 0.98
)

// GeminiEmbedder calls the Gemini API's batchEmbedContents. The key is looked up on every call (from the
// provider .env, like the runner does) so adding it takes effect without a restart; it is never logged.
type GeminiEmbedder struct {
	Key     func() string
	BaseURL string
	Client  *http.Client
}

// NewGeminiEmbedder uses key to find GEMINI_API_KEY.
func NewGeminiEmbedder(key func(name string) string) *GeminiEmbedder {
	return &GeminiEmbedder{
		Key:     func() string { return key(geminiKeyEnvVar) },
		BaseURL: geminiBaseURL,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Model includes the vector size, so changing it never reuses cached vectors of another size.
func (g *GeminiEmbedder) Model() string    { return fmt.Sprintf("%s:%d", GeminiModel, geminiDimensions) }
func (g *GeminiEmbedder) Configured() bool { return g.Key() != "" }

// MinSimilarity: Gemini embeddings score unrelated text higher than OpenAI's do. Not calibrated against live
// data yet (see the README); raise it if nonsense queries return results.
func (g *GeminiEmbedder) MinSimilarity() float64 { return geminiMinSimilarity }

// NearDuplicateSimilarity is the score from which crew memory treats two memories in one scope as the same lesson.
func (g *GeminiEmbedder) NearDuplicateSimilarity() float64 { return geminiNearDuplicate }

// EmbedDocuments uses the model's document format for asymmetric retrieval.
func (g *GeminiEmbedder) EmbedDocuments(ctx context.Context, docs []Document) ([][]float32, error) {
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = "title: " + d.Title + " | text: " + d.Text
	}
	return g.embed(ctx, texts)
}

// EmbedQuery uses the model's search-query format.
func (g *GeminiEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	out, err := g.embed(ctx, []string{"task: search result | query: " + query})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// EmbedTexts embeds memories (title-less documents) or memory queries in the model's retrieval formats.
func (g *GeminiEmbedder) EmbedTexts(ctx context.Context, texts []string, query bool) ([][]float32, error) {
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		if query {
			prefixed[i] = "task: search result | query: " + t
		} else {
			prefixed[i] = "title: none | text: " + t
		}
	}
	return g.embed(ctx, prefixed)
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiRequest struct {
	Model   string `json:"model"`
	Content struct {
		Parts []geminiPart `json:"parts"`
	} `json:"content"`
	OutputDimensionality int `json:"output_dimensionality"`
}

func (g *GeminiEmbedder) embed(ctx context.Context, texts []string) ([][]float32, error) {
	key := g.Key()
	if key == "" {
		return nil, errors.New(geminiKeyEnvVar + " is not set")
	}
	reqs := make([]geminiRequest, len(texts))
	for i, t := range texts {
		if len(t) > maxEmbedChars {
			t = t[:maxEmbedChars]
		}
		reqs[i].Model = "models/" + GeminiModel
		reqs[i].Content.Parts = []geminiPart{{Text: strings.ToValidUTF8(t, "")}}
		reqs[i].OutputDimensionality = geminiDimensions
	}
	body, err := json.Marshal(map[string]any{"requests": reqs})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(g.BaseURL, "/") + "/models/" + GeminiModel + ":batchEmbedContents"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", key)
	res, err := g.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embeddings: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("gemini embeddings: read response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		msg := string(raw)
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		return nil, fmt.Errorf("gemini embeddings: HTTP %d: %s", res.StatusCode, msg)
	}
	var parsed struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("gemini embeddings: parse response: %w", err)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini embeddings: got %d vectors for %d texts", len(parsed.Embeddings), len(texts))
	}
	out := make([][]float32, len(texts))
	for i, e := range parsed.Embeddings {
		if len(e.Values) == 0 {
			return nil, fmt.Errorf("gemini embeddings: vector %d is empty", i)
		}
		out[i] = e.Values
	}
	return out, nil
}
