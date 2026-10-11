package km2

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"crewstudio/internal/model"
)

// The KM2 v1 endpoints Studio uses (KM2 docs/API.md, "Enterprise API — /api/v1").
const (
	pathSearch    = "/api/v1/search"
	pathChat      = "/api/v1/search/chat"
	pathFolders   = "/api/v1/knowledge/folders"
	pathDocuments = "/api/v1/knowledge/documents"
	// keyPrefix starts every KM2 API key; KM2 ignores other credentials
	keyPrefix = "km2_"
)

const (
	readTimeout = 30 * time.Second
	// Chat answers and document writes wait for KM2's model or ingest queue, so they get longer
	askTimeout  = 90 * time.Second
	saveTimeout = 90 * time.Second
	// Limits on what reaches an agent from KM2
	maxHitChars     = 4_000
	maxAnswerChars  = 20_000
	maxSources      = 10
	maxSourceFields = 12
	maxSourceChars  = 600
	maxFolders      = 5_000
)

var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Client calls one KM2 connection with its key. It never prints the key, even through fmt or slog.
type Client struct {
	base string
	key  string
	http *caller
}

// String names the client by its base URL only.
func (c *Client) String() string { return "KM2 client for " + c.base }

// LogValue keeps the key out of structured logs.
func (c *Client) LogValue() slog.Value { return slog.StringValue(c.String()) }

// New builds a client for a km2 connection from its opened secrets. The base URL is the only host it ever calls.
func New(conn model.Connection, secrets map[string]string, opts Options) (*Client, error) {
	if conn.Kind != model.KindKM2 {
		return nil, errorf("%s is a %s connection, not a KM2 one", conn.Name, conn.Kind)
	}
	base := strings.TrimRight(conn.Config["base_url"], "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errorf("the base URL of %s is not a usable http or https address", conn.Name)
	}
	key := strings.TrimSpace(secrets["api_key"])
	if key == "" {
		return nil, errorf("%s has no API key saved; replace its secret", conn.Name)
	}
	if !strings.HasPrefix(key, keyPrefix) {
		return nil, errorf("the API key of %s is not a KM2 key (KM2 keys start with %s); replace its secret", conn.Name, keyPrefix)
	}
	return &Client{base: base, key: key, http: newCaller(opts)}, nil
}

func (c *Client) send(ctx context.Context, method, path string, body any, timeout time.Duration, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req := call{method: method, url: c.base + path, key: c.key}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return errorf("the request to KM2 could not be built")
		}
		req.body = data
	}
	resp, err := c.http.do(ctx, req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return &Error{Message: "KM2 answered with something that is not the expected JSON", Status: resp.status}
	}
	return nil
}

type folderWire struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	DotPath  string  `json:"dot_path"`
	ParentID *string `json:"parent_id"`
}

// Folders lists the folders the key can see (KM2 needs knowledge:read for this).
func (c *Client) Folders(ctx context.Context) ([]Folder, error) {
	var wire []folderWire
	if err := c.send(ctx, http.MethodGet, pathFolders, nil, readTimeout, &wire); err != nil {
		return nil, err
	}
	if len(wire) > maxFolders {
		wire = wire[:maxFolders]
	}
	out := make([]Folder, 0, len(wire))
	for _, f := range wire {
		out = append(out, Folder{ID: strings.ToLower(f.ID), Name: oneLine(f.Name, 255), Path: oneLine(f.DotPath, 1000), ParentID: f.ParentID})
	}
	return out, nil
}

// Search finds the passages most like the query (search:read).
func (c *Client) Search(ctx context.Context, query string, topK int) (SearchResult, error) {
	var out SearchResult
	body := map[string]any{"query": query, "limit": topK}
	if err := c.send(ctx, http.MethodPost, pathSearch, body, readTimeout, &out); err != nil {
		return SearchResult{}, err
	}
	hits := make([]Hit, 0, min(len(out.Hits), topK))
	for _, h := range out.Hits[:min(len(out.Hits), topK)] {
		hits = append(hits, Hit{DocumentID: oneLine(h.DocumentID, 100), DocumentTitle: oneLine(h.DocumentTitle, 255),
			Text: cutRunes(h.Text, maxHitChars), Score: h.Score, ChunkOrder: h.ChunkOrder})
	}
	return SearchResult{Hits: hits, Total: out.Total}, nil
}

// Ask gets an answer grounded in the knowledge the key can see, with its sources (search:read).
func (c *Client) Ask(ctx context.Context, question string) (Answer, error) {
	var wire struct {
		Answer  string           `json:"answer"`
		Sources []map[string]any `json:"sources"`
	}
	body := map[string]any{"query": question, "chat_history": []any{}, "use_knowledge_graph": true}
	if err := c.send(ctx, http.MethodPost, pathChat, body, askTimeout, &wire); err != nil {
		return Answer{}, err
	}
	return Answer{Answer: cutRunes(wire.Answer, maxAnswerChars), Sources: trimSources(wire.Sources)}, nil
}

// Save adds a document, or a new version of the one with the same external_ref in that folder (knowledge:write).
func (c *Client) Save(ctx context.Context, doc Document) (Saved, error) {
	if !idPattern.MatchString(doc.FolderID) {
		return Saved{}, errorf("the folder id %q is not a KM2 folder id", oneLine(doc.FolderID, 60))
	}
	body := map[string]any{"folder_id": strings.ToLower(doc.FolderID), "title": doc.Title, "content": doc.Content}
	if doc.ExternalRef != "" {
		body["external_ref"] = doc.ExternalRef
	}
	if len(doc.Metadata) > 0 {
		body["metadata"] = doc.Metadata
	}
	var out Saved
	if err := c.send(ctx, http.MethodPost, pathDocuments, body, saveTimeout, &out); err != nil {
		return Saved{}, err
	}
	return cleanSaved(out)
}

// outcomes are the write outcomes KM2 documents.
var outcomes = []string{"created", "updated", "reprocessed", "metadata_updated", "unchanged"}

var statusPattern = regexp.MustCompile(`^[A-Za-z_]{1,30}$`)

// cleanSaved checks the fields of KM2's answer that reach agents, the journal and the action log.
func cleanSaved(s Saved) (Saved, error) {
	if !idPattern.MatchString(s.ID) {
		return Saved{}, &Error{Message: "KM2 answered the save without a document id; check the document in KM2", Status: http.StatusOK}
	}
	s.ID = strings.ToLower(s.ID)
	if !slices.Contains(outcomes, s.Outcome) {
		s.Outcome = "saved"
	}
	if !statusPattern.MatchString(s.ProcessingStatus) {
		s.ProcessingStatus = ""
	}
	s.Title = oneLine(s.Title, 255)
	s.DocumentKey = oneLine(s.DocumentKey, 255)
	return s, nil
}

// Status reads a document's processing state (knowledge:read).
func (c *Client) Status(ctx context.Context, documentID string) (DocumentStatus, error) {
	if !idPattern.MatchString(documentID) {
		return DocumentStatus{}, errorf("%q is not a KM2 document id", oneLine(documentID, 60))
	}
	var out DocumentStatus
	err := c.send(ctx, http.MethodGet, pathDocuments+"/"+url.PathEscape(strings.ToLower(documentID)), nil, readTimeout, &out)
	return out, err
}

// trimSources keeps the scalar fields of the first sources, cut short, so a source cannot flood the agent.
func trimSources(in []map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, src := range in {
		if len(out) == maxSources {
			break
		}
		clean := map[string]any{}
		keys := make([]string, 0, len(src))
		for k := range src {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if len(clean) == maxSourceFields {
				break
			}
			switch val := src[k].(type) {
			case string:
				clean[oneLine(k, 60)] = cutRunes(val, maxSourceChars)
			case float64, bool:
				clean[oneLine(k, 60)] = val
			}
		}
		out = append(out, clean)
	}
	return out
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func oneLine(s string, n int) string {
	return cutRunes(strings.Join(strings.Fields(s), " "), n)
}
