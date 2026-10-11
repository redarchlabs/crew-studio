package tracker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"crewstudio/internal/connections"
)

const (
	requestTimeout = 30 * time.Second
	// maxResponseBytes caps one answer; a search page is far smaller
	maxResponseBytes = 8 << 20
	// maxRetries is how often a rate-limited request is tried again
	maxRetries   = 3
	firstBackoff = 2 * time.Second
	maxBackoff   = 30 * time.Second
	userAgent    = "CrewStudio/1.0 (tracker)"
)

// httpClient sends requests with retries on rate limits. It never follows redirects, so credentials are only ever
// sent to the connection's own host.
type httpClient struct {
	client *http.Client
	sleep  func(ctx context.Context, d time.Duration) error
}

func newHTTP(opts Options) *httpClient {
	client := opts.HTTP
	if client == nil {
		client = &http.Client{Transport: connections.Transport(), Timeout: requestTimeout}
	}
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	return &httpClient{client: &safe, sleep: sleep}
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

// request describes one call; build makes a fresh *http.Request for every attempt.
type request struct {
	method  string
	url     string
	body    []byte
	headers map[string]string
	// rateLimited reports answers that mean "slow down" besides HTTP 429 (Linear answers 400 RATELIMITED)
	rateLimited func(r response) bool
}

// do sends the request, waiting and retrying when the tracker says to slow down.
func (h *httpClient) do(ctx context.Context, name string, req request) (response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := h.once(ctx, name, req)
		if err != nil {
			return resp, err
		}
		limited := resp.status == http.StatusTooManyRequests || (resp.status == http.StatusServiceUnavailable && resp.header.Get("Retry-After") != "") ||
			(req.rateLimited != nil && req.rateLimited(resp))
		if !limited {
			return resp, nil
		}
		wait := retryWait(resp.header, attempt, time.Now())
		if attempt >= maxRetries || wait > 2*maxBackoff {
			return resp, &Error{Message: name + " is rate limiting Crew Studio; try again later", Status: resp.status, RateLimited: true, Rejected: true}
		}
		if err := h.sleep(ctx, wait); err != nil {
			return resp, unreachable("could not reach %s: %s", name, netProblem(err))
		}
	}
}

func (h *httpClient) once(ctx context.Context, name string, req request) (response, error) {
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.method, req.url, body)
	if err != nil {
		return response{}, errorf("the request to %s could not be built", name)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)
	if req.body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range req.headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := h.client.Do(httpReq)
	if err != nil {
		return response{}, unreachable("could not reach %s: %s", name, netProblem(err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return response{}, unreachable("could not read the answer from %s: %s", name, netProblem(err))
	}
	if len(data) > maxResponseBytes {
		return response{}, unreachable("%s answered with more than %d MB", name, maxResponseBytes>>20)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// netProblem says why a request failed in general terms, so tracker calls cannot be used to probe a network
// (and no URL, which a *url.Error would quote, ends up in a message).
func netProblem(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "the request was cancelled"
	}
	return "the connection failed (the server is down, or the address is not allowed)"
}

// retryWait is how long to wait before trying again: Retry-After (seconds), Linear's reset time (epoch ms), or an
// exponential backoff from 2 s with jitter.
func retryWait(header http.Header, attempt int, now time.Time) time.Duration {
	if s, err := strconv.Atoi(header.Get("Retry-After")); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	if ms, err := strconv.ParseInt(header.Get("X-RateLimit-Requests-Reset"), 10, 64); err == nil {
		if d := time.UnixMilli(ms).Sub(now); d > 0 {
			return d
		}
	}
	d := firstBackoff << attempt
	if d > maxBackoff {
		d = maxBackoff
	}
	jitter := 0.7 + 0.6*rand.Float64()
	return time.Duration(float64(d) * jitter)
}
