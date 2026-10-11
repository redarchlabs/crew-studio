package km2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"crewstudio/internal/connections"
)

const (
	// maxResponseBytes caps one answer; a search page or chat answer is far smaller
	maxResponseBytes = 4 << 20
	// maxRetries is how often a rate-limited request is tried again
	maxRetries   = 2
	firstBackoff = time.Second
	maxBackoff   = 10 * time.Second
	// maxRetryWait is the longest Retry-After waited for; a longer one (KM2's daily write cap) is reported instead
	maxRetryWait   = 20 * time.Second
	maxDetailChars = 500
	userAgent      = "CrewStudio/1.0 (km2)"
)

// caller sends requests with retries on rate limits. It never follows redirects, so the key is only ever sent to
// the connection's own base URL.
type caller struct {
	client *http.Client
	sleep  func(ctx context.Context, d time.Duration) error
}

// sharedClient is the client of every KM2 call that brings none: one transport with the protections of every
// connection (connections.Transport), so connections to KM2 are reused, and idle ones are closed after a while.
var sharedClient = sync.OnceValue(func() *http.Client {
	t := connections.Transport()
	t.IdleConnTimeout = 90 * time.Second
	t.MaxIdleConnsPerHost = 4
	t.ResponseHeaderTimeout = askTimeout
	return &http.Client{Transport: t}
})

func newCaller(opts Options) *caller {
	client := opts.HTTP
	if client == nil {
		client = sharedClient()
	}
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	sleep := sleepCtx
	if opts.Sleep != nil {
		sleep = func(_ context.Context, d time.Duration) error { return opts.Sleep(d) }
	}
	return &caller{client: &safe, sleep: sleep}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// response is a whole answer, read up to maxResponseBytes.
type response struct {
	status int
	header http.Header
	body   []byte
}

// call is one request to KM2: method, full URL, optional JSON body and the key.
type call struct {
	method string
	url    string
	body   []byte
	key    string
}

// do sends the request, waiting and retrying when KM2 says to slow down. Answers other than 2xx become *Error.
func (c *caller) do(ctx context.Context, req call) (response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.once(ctx, req)
		if err != nil {
			return resp, err
		}
		limited := resp.status == http.StatusTooManyRequests ||
			(resp.status == http.StatusServiceUnavailable && resp.header.Get("Retry-After") != "")
		if !limited {
			if resp.status < 200 || resp.status > 299 {
				return resp, statusError(resp, req.key)
			}
			return resp, nil
		}
		wait := retryWait(resp.header, attempt)
		if attempt >= maxRetries || wait > maxRetryWait {
			return resp, statusError(resp, req.key)
		}
		if err := c.sleep(ctx, wait); err != nil {
			return resp, errorf("could not reach KM2: %s", netProblem(err))
		}
	}
}

func (c *caller) once(ctx context.Context, req call) (response, error) {
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.method, req.url, body)
	if err != nil {
		return response{}, errorf("the request to KM2 could not be built")
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)
	httpReq.Header.Set("Authorization", "Bearer "+req.key)
	if req.body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return response{}, errorf("could not reach KM2: %s", netProblem(err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return response{}, errorf("could not read KM2's answer: %s", netProblem(err))
	}
	if len(data) > maxResponseBytes {
		return response{}, errorf("KM2 answered with more than %d MB", maxResponseBytes>>20)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// netProblem says why a request failed in general terms, so KM2 calls cannot be used to probe a network (and no
// URL, which a *url.Error would quote, ends up in a message).
func netProblem(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "the request was cancelled"
	}
	return "the connection failed (KM2 is down, or the address is not allowed)"
}

// retryWait is Retry-After (seconds) or an exponential backoff from 1 s with jitter.
func retryWait(header http.Header, attempt int) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After"))); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	d := firstBackoff << attempt
	if d > maxBackoff {
		d = maxBackoff
	}
	return time.Duration(float64(d) * (0.7 + 0.6*rand.Float64()))
}

// statusError explains a non-2xx answer without ever repeating the key.
func statusError(resp response, key string) *Error {
	e := &Error{Status: resp.status}
	switch resp.status {
	case http.StatusUnauthorized:
		e.Message = "KM2 refused the key (it is wrong, revoked or expired); an administrator must replace the connection's secret"
	case http.StatusForbidden:
		e.Message = "KM2 refused: the key lacks the scope for this, or its profile may not use that folder"
	case http.StatusNotFound:
		e.Message = "KM2 found no such folder or document, or the key's profile cannot see it"
	case http.StatusConflict:
		e.Message, e.Detail = "KM2 refused the save", detailOf(resp.body, key)
	case http.StatusRequestEntityTooLarge:
		e.Message = "the document is larger than KM2 accepts"
	case http.StatusUnprocessableEntity, http.StatusBadRequest:
		e.Message, e.Detail = "KM2 refused the request as invalid", detailOf(resp.body, key)
	case http.StatusTooManyRequests:
		e.RateLimited = true
		e.Message = "KM2 is rate limiting this key, or its daily document cap is used up; try again later"
		if after, err := strconv.Atoi(strings.TrimSpace(resp.header.Get("Retry-After"))); err == nil && after >= 0 {
			e.Message += fmt.Sprintf(" (KM2 says retry after %d s)", after)
		}
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		e.Message = fmt.Sprintf("KM2 answered with a redirect (HTTP %d); use the final address as the connection's base URL", resp.status)
	default:
		e.Message = fmt.Sprintf("KM2 answered HTTP %d; try again later", resp.status)
		if resp.status == http.StatusServiceUnavailable {
			e.Detail = detailOf(resp.body, key)
		}
	}
	return e
}

// keyLike is anything that looks like a KM2 key (or a long part of one), removed from KM2's text as well
var keyLike = regexp.MustCompile(`km2_[A-Za-z0-9_-]{6,}`)

// detailOf reads FastAPI's {"detail": ...} (a string, a list of validation errors, or an object with a message),
// removes the key, and cuts it to one short line.
func detailOf(body []byte, key string) string {
	var env struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &env) != nil || len(env.Detail) == 0 {
		return ""
	}
	var text string
	var list []struct {
		Msg string `json:"msg"`
		Loc []any  `json:"loc"`
	}
	var obj struct {
		Message string `json:"message"`
	}
	switch {
	case json.Unmarshal(env.Detail, &text) == nil:
	case json.Unmarshal(env.Detail, &list) == nil:
		parts := make([]string, 0, len(list))
		for _, item := range list {
			parts = append(parts, locText(item.Loc)+item.Msg)
		}
		text = strings.Join(parts, "; ")
	case json.Unmarshal(env.Detail, &obj) == nil:
		text = obj.Message
	}
	if key != "" {
		text = strings.ReplaceAll(text, key, "[key]")
	}
	text = keyLike.ReplaceAllString(text, "[key]")
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > maxDetailChars {
		text = string(r[:maxDetailChars]) + "…"
	}
	return text
}

// locText names the field of a validation error, such as "metadata: ".
func locText(loc []any) string {
	if len(loc) == 0 {
		return ""
	}
	if s, ok := loc[len(loc)-1].(string); ok && s != "body" {
		return s + ": "
	}
	return ""
}
