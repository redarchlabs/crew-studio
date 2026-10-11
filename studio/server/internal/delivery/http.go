package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"crewstudio/internal/connections"
)

// errorCodePattern is a short machine-readable code such as channel_not_found, the only part of an answer shown.
var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// answer is what a webhook or API answered: the status and up to maxAnswerBytes of the body.
type answer struct {
	status     int
	body       []byte
	retryAfter string
}

// postJSON sends body to target. Transport failures come back as safe *Errors (the URL holds a secret, so it is
// never part of one).
func (s *Sender) postJSON(ctx context.Context, service, target string, body any, headers map[string]string) (answer, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return answer{}, failf(false, "the %s message could not be encoded", service)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return answer{}, failf(false, "the %s address is not usable", service)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "CrewStudio/1.0 (report delivery)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return answer{}, failf(true, "could not reach %s: %s", service, transportProblem(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes))
	return answer{status: resp.StatusCode, body: raw, retryAfter: resp.Header.Get("Retry-After")}, nil
}

// transportProblem says why a request failed in general terms only.
func transportProblem(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "it timed out"
	case errors.Is(err, connections.ErrBlockedAddress):
		return "that address is not allowed"
	default:
		return "the connection failed"
	}
}

// statusProblem turns an unsuccessful answer into an *Error; code is the service's short error code, if any.
func statusProblem(service string, a answer, code string) error {
	detail := "HTTP " + strconv.Itoa(a.status)
	if code != "" {
		detail += ": " + code
	}
	switch {
	case a.status == http.StatusTooManyRequests:
		wait := ""
		if n, err := strconv.Atoi(strings.TrimSpace(a.retryAfter)); err == nil && n > 0 && n < 86400 {
			wait = " (it asked to wait " + strconv.Itoa(n) + " seconds)"
		}
		return failf(true, "%s is limiting how fast messages are sent%s; retry later", service, wait)
	case a.status >= 300 && a.status < 400:
		return failf(false, "%s answered with a redirect (%s); check the URL", service, detail)
	case a.status == http.StatusUnauthorized || a.status == http.StatusForbidden:
		return failf(false, "%s refused the credentials or the message (%s)", service, detail)
	case a.status == http.StatusNotFound || a.status == http.StatusGone:
		return failf(false, "%s does not know this destination (%s); it may have been removed", service, detail)
	case a.status == http.StatusRequestEntityTooLarge:
		return failf(false, "%s refused the message as too large (%s)", service, detail)
	case a.status >= 500:
		return failf(true, "%s had a problem (%s); retry later", service, detail)
	}
	return failf(false, "%s refused the message (%s)", service, detail)
}

// plainCode is a body that is only a short error code (Slack webhooks answer like that), or "".
func plainCode(body []byte) string {
	text := strings.TrimSpace(string(body))
	if errorCodePattern.MatchString(text) {
		return text
	}
	return ""
}

// originOf is scheme://host[:port] of u, lower case.
func originOf(u *url.URL) string {
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

// listedOrigin reports whether an administrator allowed u's origin (STUDIO_DELIVERY_ORIGINS).
func (s *Sender) listedOrigin(u *url.URL) bool {
	origin := originOf(u)
	return slices.ContainsFunc(s.opts.Origins, func(o string) bool { return strings.EqualFold(strings.TrimRight(o, "/"), origin) })
}

// webhookURL parses a secret webhook URL: https, no credentials in it, and plain http only for an allowed origin.
func (s *Sender) webhookURL(service, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, failf(false, "the %s webhook URL is not valid; replace the secret", service)
	}
	if u.Scheme == "http" && !s.listedOrigin(u) {
		return nil, failf(false, "the %s webhook URL must use https", service)
	}
	return u, nil
}
