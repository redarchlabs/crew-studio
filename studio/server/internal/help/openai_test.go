package help

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbedder(t *testing.T) {
	var got struct {
		Model      string   `json:"model"`
		Input      []string `json:"input"`
		Dimensions int      `json:"dimensions"`
	}
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		data := []map[string]any{}
		// Out of order on purpose: results are matched by index
		for i := len(got.Input) - 1; i >= 0; i-- {
			data = append(data, map[string]any{"index": i, "embedding": []float32{float32(i), 1}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	o := NewOpenAIEmbedder(func(name string) string {
		if name == "OPENAI_API_KEY" {
			return "sk-test"
		}
		return ""
	})
	o.BaseURL = srv.URL
	if !o.Configured() || o.Model() != "text-embedding-3-small:768" || o.MinSimilarity() <= 0 ||
		o.NearDuplicateSimilarity() <= o.MinSimilarity() || o.NearDuplicateSimilarity() >= 1 {
		t.Fatal("embedder should be configured")
	}
	vecs, err := o.EmbedDocuments(context.Background(), []Document{{Title: "Crews", Text: "Build"}, {Title: "Runs", Text: strings.Repeat("x", maxEmbedChars+5)}})
	if err != nil || len(vecs) != 2 || vecs[1][0] != 1 {
		t.Fatalf("documents: %v %v", vecs, err)
	}
	if auth != "Bearer sk-test" || path != "/embeddings" || got.Model != OpenAIModel || got.Dimensions != openAIDimensions ||
		got.Input[0] != "Crews\n\nBuild" || len(got.Input[1]) != maxEmbedChars {
		t.Errorf("request: auth %q path %q body %+v", auth, path, got.Model)
	}
	if _, err := o.EmbedQuery(context.Background(), "budget"); err != nil || got.Input[0] != "budget" {
		t.Errorf("query: %v %q", err, got.Input)
	}
}

func TestOpenAIEmbedderErrors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(strings.Repeat("nope ", 100)))
		},
		"bad json":    func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{")) },
		"wrong count": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) },
		"empty vector": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[]}]}`))
		},
		"bad index": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"index":3,"embedding":[1]}]}`))
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			o := NewOpenAIEmbedder(func(string) string { return "sk-secret" })
			o.BaseURL = srv.URL
			_, err := o.EmbedQuery(context.Background(), "q")
			if err == nil || strings.Contains(err.Error(), "sk-secret") || len(err.Error()) > maxErrorBody+100 {
				t.Errorf("err = %v", err)
			}
		})
	}
	o := NewOpenAIEmbedder(func(string) string { return "" })
	if _, err := o.EmbedQuery(context.Background(), "q"); err == nil || o.Configured() {
		t.Error("missing key must fail")
	}
	o = NewOpenAIEmbedder(func(string) string { return "k" })
	o.BaseURL = "http://127.0.0.1:1"
	if _, err := o.EmbedQuery(context.Background(), "q"); err == nil {
		t.Error("unreachable server must fail")
	}
}

func TestOpenAIEmbedTextsAndBaseURL(t *testing.T) {
	var input []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		input = body.Input
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": []float32{1, 2}}}})
	}))
	defer srv.Close()
	// OPENAI_BASE_URL from the provider .env points the embedder elsewhere, as it does for the runner
	o := NewOpenAIEmbedder(func(name string) string {
		return map[string]string{"OPENAI_API_KEY": "sk-test", "OPENAI_BASE_URL": srv.URL + "/v1/"}[name]
	})
	if o.BaseURL != srv.URL+"/v1/" {
		t.Fatalf("base url = %q", o.BaseURL)
	}
	vecs, err := o.EmbedTexts(context.Background(), []string{"Sam replies late"}, true)
	if err != nil || len(vecs) != 1 || input[0] != "Sam replies late" {
		t.Fatalf("vecs %v err %v input %v", vecs, err, input)
	}
	if NewOpenAIEmbedder(func(string) string { return "" }).BaseURL != openAIBaseURL {
		t.Error("default base url")
	}
}
