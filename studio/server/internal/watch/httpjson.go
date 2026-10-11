package watch

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crewstudio/internal/model"
)

const httpFetchTimeout = 30 * time.Second

// HTTPJSON reads a JSON API through an http connection: GET base URL + path, with the connection's auth header.
type HTTPJSON struct {
	// Transport is the connection transport (connections.Transport); tests may swap it
	Transport http.RoundTripper
	Timeout   time.Duration
}

// requestURL joins the connection's base URL and the watch's path, refusing anything that leaves the base host.
func requestURL(base, path string) (string, error) {
	if problem := model.CheckRequestPath(path); problem != "" {
		return "", sourceErrorf("the path %s", problem)
	}
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Host == "" {
		return "", sourceErrorf("the connection's base URL is not usable")
	}
	joined := strings.TrimRight(base, "/") + path
	u, err := url.Parse(joined)
	if err != nil || u.Host != baseURL.Host || u.Scheme != baseURL.Scheme || u.User != nil {
		return "", sourceErrorf("the path does not stay on the connection's host")
	}
	return u.String(), nil
}

func (h HTTPJSON) Fetch(ctx context.Context, req Request) (Fetched, error) {
	if req.Connection == nil {
		return Fetched{}, sourceErrorf("this watch needs an http connection")
	}
	conn := req.Connection
	target, err := requestURL(conn.Config["base_url"], req.Def.Source.Path)
	if err != nil {
		return Fetched{}, err
	}
	timeout := h.Timeout
	if timeout == 0 {
		timeout = httpFetchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Fetched{}, sourceErrorf("the request could not be built")
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "CrewStudio/1.0 (watch)")
	if header := conn.Config["auth_header"]; header != "" {
		value := req.Secrets["auth_value"]
		if value == "" {
			return Fetched{}, sourceErrorf("the connection %s has an auth header name but no saved value; replace its secret", conn.Name)
		}
		httpReq.Header.Set(header, value)
	}
	client := &http.Client{Transport: h.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Fetched{}, sourceErrorf("could not reach %s: %s", conn.Name, unwrapNetError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Fetched{}, sourceErrorf("%s answered HTTP %d%s", conn.Name, resp.StatusCode, statusHint(resp.StatusCode))
	}
	body, err := readCapped(resp.Body, maxSourceBytes, "the response")
	if err != nil {
		return Fetched{}, err
	}
	doc, err := decodeJSON(body)
	if err != nil {
		return Fetched{}, err
	}
	d := req.Def
	items, err := itemsFromJSON(doc, d.Source.ItemsPath, d.IDField, d.TrackedFields, d.MaxItems)
	if err != nil {
		return Fetched{}, err
	}
	return Fetched{Label: fmt.Sprintf("%s: %s", conn.Name, d.Source.Path), Items: items}, nil
}

func statusHint(code int) string {
	switch {
	case code >= 300 && code < 400:
		return " (a redirect; use the final address in the connection or path)"
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return " (check the connection's credentials)"
	}
	return ""
}
