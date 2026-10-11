package help

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	OpenAIModel      = "text-embedding-3-small"
	openAIBaseURL    = "https://api.openai.com/v1"
	openAIDimensions = 768
	openAIKeyEnvVar  = "OPENAI_API_KEY"
	// openAIBaseURLEnvVar points at an OpenAI-compatible server (a proxy, or the e2e fake), as it does for the runner
	openAIBaseURLEnvVar = "OPENAI_BASE_URL"
	openAIMinSimilarity = 0.28
	// openAINearDuplicate: text-embedding-3 scores a reworded lesson around 0.90 to 0.95 and the same lesson with
	// other punctuation or case at 0.97 or more (not calibrated against live memories)
	openAINearDuplicate = 0.95
)

// OpenAIEmbedder calls the OpenAI embeddings API. Like GeminiEmbedder it looks the key up on every call and
// never logs it.
type OpenAIEmbedder struct {
	Key     func() string
	BaseURL string
	Client  *http.Client
}

// NewOpenAIEmbedder uses key to find OPENAI_API_KEY, and OPENAI_BASE_URL when it is set.
func NewOpenAIEmbedder(key func(name string) string) *OpenAIEmbedder {
	base := strings.TrimSpace(key(openAIBaseURLEnvVar))
	if base == "" {
		base = openAIBaseURL
	}
	return &OpenAIEmbedder{
		Key:     func() string { return key(openAIKeyEnvVar) },
		BaseURL: base,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Model includes the vector size, so changing it never reuses cached vectors of another size.
func (o *OpenAIEmbedder) Model() string    { return fmt.Sprintf("%s:%d", OpenAIModel, openAIDimensions) }
func (o *OpenAIEmbedder) Configured() bool { return o.Key() != "" }

// MinSimilarity was calibrated on the help articles: real questions scored 0.33 to 0.64 at best, nonsense and
// off-topic queries 0.12 to 0.24.
func (o *OpenAIEmbedder) MinSimilarity() float64 { return openAIMinSimilarity }

// NearDuplicateSimilarity is the score from which crew memory treats two memories in one scope as the same lesson.
func (o *OpenAIEmbedder) NearDuplicateSimilarity() float64 { return openAINearDuplicate }

// EmbedDocuments embeds each section with its title in front; this model needs no task instructions.
func (o *OpenAIEmbedder) EmbedDocuments(ctx context.Context, docs []Document) ([][]float32, error) {
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.Title + "\n\n" + d.Text
	}
	return o.embed(ctx, texts)
}

func (o *OpenAIEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	out, err := o.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// EmbedTexts embeds plain texts (memories and memory queries); this model embeds both the same way.
func (o *OpenAIEmbedder) EmbedTexts(ctx context.Context, texts []string, _ bool) ([][]float32, error) {
	return o.embed(ctx, texts)
}

func (o *OpenAIEmbedder) embed(ctx context.Context, texts []string) ([][]float32, error) {
	key := o.Key()
	if key == "" {
		return nil, errors.New(openAIKeyEnvVar + " is not set")
	}
	input := make([]string, len(texts))
	for i, t := range texts {
		if len(t) > maxEmbedChars {
			t = t[:maxEmbedChars]
		}
		input[i] = strings.ToValidUTF8(t, "")
	}
	body, err := json.Marshal(map[string]any{"model": OpenAIModel, "input": input, "dimensions": openAIDimensions})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: read response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		msg := string(raw)
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		return nil, fmt.Errorf("openai embeddings: HTTP %d: %s", res.StatusCode, msg)
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("openai embeddings: parse response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("openai embeddings: got %d vectors for %d texts", len(parsed.Data), len(texts))
	}
	slices.SortFunc(parsed.Data, func(a, b struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}) int {
		return a.Index - b.Index
	})
	out := make([][]float32, len(texts))
	for i, d := range parsed.Data {
		if d.Index != i || len(d.Embedding) == 0 {
			return nil, fmt.Errorf("openai embeddings: vector %d is missing or empty", i)
		}
		out[i] = d.Embedding
	}
	return out, nil
}
