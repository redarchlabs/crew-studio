package watch

import (
	"context"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	webFetchTimeout = 20 * time.Second
	maxPageBytes    = 2_000_000
	maxRedirects    = 5
)

// WebPage reads a public web page and compares its text (or the text of a CSS selector). Like the Scrape Website
// tool it reaches public addresses only: every connection, including after redirects, is checked as it is made,
// so DNS tricks cannot reach private, loopback or link-local addresses.
type WebPage struct {
	// Transport may be replaced by tests; nil uses PublicTransport
	Transport http.RoundTripper
	Timeout   time.Duration
}

// blockedPrefixes are special-purpose ranges that are not public even though Go calls them global unicast.
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "240.0.0.0/4", "192.88.99.0/24", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23",
		"2001:db8::/32", "2002::/16", "::/8", "fec0::/10", "3fff::/20",
	} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

// IsPublic reports whether an address is a public (globally routable) one.
func IsPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errBlockedAddress
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !IsPublic(addr) {
		return errBlockedAddress
	}
	return nil
}

// PublicTransport ignores proxy settings and connects to public addresses only.
func PublicTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: webFetchTimeout, Control: publicOnly}
	return &http.Transport{Proxy: nil, DialContext: dialer.DialContext, TLSHandshakeTimeout: webFetchTimeout,
		ResponseHeaderTimeout: webFetchTimeout, IdleConnTimeout: 90 * time.Second, MaxIdleConnsPerHost: 2}
}

// publicTransport is shared by every web page fetch, so idle connections are reused and then closed.
var publicTransport = PublicTransport()

func (p WebPage) Fetch(ctx context.Context, req Request) (Fetched, error) {
	src := req.Def.Source
	timeout := p.Timeout
	if timeout == 0 {
		timeout = webFetchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	text, err := p.fetchText(ctx, src.URL, src.Selector)
	if err != nil {
		return Fetched{}, err
	}
	// The query string can carry tokens, so it stays out of the change list and the crew's prompt
	page := withoutQuery(src.URL)
	label := page
	if src.Selector != "" {
		label += " (" + src.Selector + ")"
	}
	id, err := checkItemID(page)
	if err != nil {
		id = "page"
	}
	return Fetched{Label: label, Items: []Item{{ID: id, Fields: map[string]any{"text": normalizeValue(text)}}}}, nil
}

func (p WebPage) client() *http.Client {
	transport := p.Transport
	if transport == nil {
		transport = publicTransport
	}
	return &http.Client{Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
			return errBlockedAddress
		}
		return nil
	}}
}

func (p WebPage) fetchText(ctx context.Context, pageURL, selector string) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", sourceErrorf("the page URL is not usable")
	}
	httpReq.Header.Set("User-Agent", "CrewStudio/1.0 (watch)")
	httpReq.Header.Set("Accept", "text/html, text/plain;q=0.9")
	resp, err := p.client().Do(httpReq)
	if err != nil {
		return "", sourceErrorf("could not fetch the page: %s", unwrapNetError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", sourceErrorf("the page answered HTTP %d", resp.StatusCode)
	}
	body, err := readCapped(resp.Body, maxPageBytes, "the page")
	if err != nil {
		return "", err
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch {
	case strings.Contains(mediaType, "html") || (mediaType == "" && looksLikeHTML(body)):
		return PageText(body, selector)
	case strings.HasPrefix(mediaType, "text/"):
		if selector != "" {
			return "", sourceErrorf("the page is plain text, so a CSS selector cannot apply")
		}
		return cleanLines(string(body)), nil
	}
	return "", sourceErrorf("the page is %q, not HTML or text", mediaType)
}

func withoutQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
	return u.String()
}

func looksLikeHTML(body []byte) bool {
	head := strings.ToLower(string(body[:min(len(body), 512)]))
	return strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html")
}
