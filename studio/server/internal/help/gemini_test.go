package help

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiEmbedder(t *testing.T) {
	var got struct {
		Requests []geminiRequest `json:"requests"`
	}
	var gotKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath = r.Header.Get("x-goog-api-key"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		out := map[string]any{"embeddings": []map[string]any{}}
		for range got.Requests {
			out["embeddings"] = append(out["embeddings"].([]map[string]any), map[string]any{"values": []float32{0.1, 0.2}})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	g := NewGeminiEmbedder(func(name string) string {
		if name == "GEMINI_API_KEY" {
			return "test-key"
		}
		return ""
	})
	g.BaseURL = srv.URL
	if !g.Configured() || g.Model() != "gemini-embedding-2:768" || g.MinSimilarity() <= 0 ||
		g.NearDuplicateSimilarity() <= g.MinSimilarity() || g.NearDuplicateSimilarity() >= 1 {
		t.Fatal("embedder should be configured")
	}
	vecs, err := g.EmbedDocuments(context.Background(), []Document{{Title: "Crews", Text: "Build crews"}, {Title: "Runs", Text: strings.Repeat("x", maxEmbedChars+10)}})
	if err != nil || len(vecs) != 2 {
		t.Fatalf("documents: %v %v", vecs, err)
	}
	if gotKey != "test-key" || gotPath != "/models/"+GeminiModel+":batchEmbedContents" {
		t.Errorf("key %q path %q", gotKey, gotPath)
	}
	first := got.Requests[0]
	if first.Model != "models/"+GeminiModel || first.OutputDimensionality != geminiDimensions ||
		first.Content.Parts[0].Text != "title: Crews | text: Build crews" {
		t.Errorf("request = %+v", first)
	}
	if n := len(got.Requests[1].Content.Parts[0].Text); n != maxEmbedChars {
		t.Errorf("long text not capped: %d", n)
	}
	if _, err := g.EmbedQuery(context.Background(), "budget"); err != nil {
		t.Fatal(err)
	}
	if text := got.Requests[0].Content.Parts[0].Text; text != "task: search result | query: budget" {
		t.Errorf("query text = %q", text)
	}
}

func TestGeminiEmbedderErrors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(strings.Repeat("denied ", 100)))
		},
		"bad json":    func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{")) },
		"wrong count": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"embeddings":[]}`)) },
		"empty vector": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"embeddings":[{"values":[]}]}`))
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			g := NewGeminiEmbedder(func(string) string { return "secret-key" })
			g.BaseURL = srv.URL
			_, err := g.EmbedQuery(context.Background(), "q")
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), "secret-key") || len(err.Error()) > maxErrorBody+100 {
				t.Errorf("error leaks the key or is too long: %v", err)
			}
		})
	}

	g := NewGeminiEmbedder(func(string) string { return "" })
	if g.Configured() {
		t.Error("no key means not configured")
	}
	if _, err := g.EmbedQuery(context.Background(), "q"); err == nil {
		t.Error("missing key must fail")
	}
	g = NewGeminiEmbedder(func(string) string { return "k" })
	g.BaseURL = "http://127.0.0.1:1"
	if _, err := g.EmbedQuery(context.Background(), "q"); err == nil {
		t.Error("unreachable server must fail")
	}
}

func TestGeminiEmbedTexts(t *testing.T) {
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got struct {
			Requests []geminiRequest `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		out := []map[string]any{}
		for _, req := range got.Requests {
			texts = append(texts, req.Content.Parts[0].Text)
			out = append(out, map[string]any{"values": []float32{0.1}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	}))
	defer srv.Close()
	g := NewGeminiEmbedder(func(string) string { return "k" })
	g.BaseURL = srv.URL
	if _, err := g.EmbedTexts(context.Background(), []string{"a"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := g.EmbedTexts(context.Background(), []string{"b"}, true); err != nil {
		t.Fatal(err)
	}
	if strings.Join(texts, "|") != "title: none | text: a|task: search result | query: b" {
		t.Errorf("texts = %q", texts)
	}
}
