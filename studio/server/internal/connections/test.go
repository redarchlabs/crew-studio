package connections

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"syscall"
	"time"

	"crewstudio/internal/model"
)

// maxTestBodyBytes is read from a test response and thrown away; bodies are never shown to anyone.
const maxTestBodyBytes = 64 << 10

// TestResult says whether a connection could be reached. It never includes response bodies or secrets.
type TestResult struct {
	// Supported is false for kinds whose client does not exist yet
	Supported  bool   `json:"supported"`
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	StatusCode int    `json:"status_code,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// Test tries an http connection. Jira and Linear connections are tested by the tracker client (internal/tracker);
// other kinds report "not supported yet".
func (s *Service) Test(ctx context.Context, c model.Connection) TestResult {
	if model.IsTrackerKind(c.Kind) {
		return TestResult{Message: fmt.Sprintf("%s connections are tested through the tracker client.", c.Kind)}
	}
	if c.Kind == model.KindKM2 {
		return TestResult{Message: "KM2 connections are tested through the KM2 client."}
	}
	if c.Kind == "slack" || c.Kind == "teams" || c.Kind == "email" {
		return TestResult{Message: fmt.Sprintf("%s connections are tested by sending a test message through the delivery client.", c.Kind)}
	}
	if kind, _ := model.ConnectionKindOf(c.Kind); !kind.Testable {
		return TestResult{Message: fmt.Sprintf("Testing %s connections is not supported yet; it arrives with the %s client.", c.Kind, c.Kind)}
	}
	return s.testHTTP(ctx, c)
}

// testHTTP sends a GET to the base URL with the auth header. Redirects are reported, not followed.
func (s *Service) testHTTP(ctx context.Context, c model.Connection) TestResult {
	res := TestResult{Supported: true}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Config["base_url"], nil)
	if err != nil {
		res.Message = "The base URL is not usable: " + err.Error()
		return res
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CrewStudio/1.0 (connection test)")
	if header := c.Config["auth_header"]; header != "" {
		if !slices.Contains(c.SecretKeys, "auth_value") && c.ID != "" {
			res.Message = "An auth header name is set but no auth header value is saved. Replace the secret first."
			return res
		}
		secrets, err := s.Secrets(ctx, c.ID)
		if err != nil {
			res.Message = "Could not read the saved secret: " + err.Error()
			return res
		}
		if secrets["auth_value"] == "" {
			res.Message = "An auth header name is set but no auth header value is saved. Replace the secret first."
			return res
		}
		req.Header.Set(header, secrets["auth_value"])
	}
	client := &http.Client{
		Transport:     testTransport(),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	start := time.Now()
	resp, err := client.Do(req)
	res.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Message = "Could not reach " + c.Config["base_url"] + ": " + unwrapURLError(err)
		return res
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxTestBodyBytes))
	res.StatusCode = resp.StatusCode
	res.OK, res.Message = describeStatus(resp.StatusCode)
	return res
}

func describeStatus(code int) (bool, string) {
	switch {
	case code >= 200 && code < 300:
		return true, fmt.Sprintf("Connected: the server answered HTTP %d.", code)
	case code >= 300 && code < 400:
		return false, fmt.Sprintf("The server answered with a redirect (HTTP %d). Use the final address as the base URL.", code)
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return false, fmt.Sprintf("The server refused the credentials (HTTP %d). Check the auth header and replace the secret.", code)
	default:
		return false, fmt.Sprintf("The server was reached but answered HTTP %d.", code)
	}
}

// errBlockedAddress is returned for addresses a test must never reach.
var errBlockedAddress = errors.New("address not allowed")

// testTransport ignores proxy settings and refuses link-local (cloud metadata), multicast and unspecified
// addresses at connect time, so DNS tricks cannot reach them. Loopback and private networks stay allowed:
// connections to internal APIs are expected, and only connections.manage holders can run a test.
func testTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: defaultTestTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errBlockedAddress
		}
		if !allowedAddress(host) {
			return errBlockedAddress
		}
		return nil
	}}
	return &http.Transport{Proxy: nil, DialContext: dialer.DialContext, TLSHandshakeTimeout: defaultTestTimeout}
}

// metadataPrefixes are cloud metadata and platform services that are not link-local: the AWS IPv6 metadata
// endpoint, Alibaba's metadata address, Azure's wire server, and NAT64 (which can reach any IPv4 address).
var metadataPrefixes = []netip.Prefix{
	netip.MustParsePrefix("fd00:ec2::/64"), netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("168.63.129.16/32"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
}

// allowedAddress refuses link-local (cloud metadata), multicast, unspecified and known metadata addresses.
func allowedAddress(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, p := range metadataPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// unwrapURLError says why a request failed in general terms only, so the test is not a port scanner.
func unwrapURLError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, errBlockedAddress):
		return "that address is not allowed"
	default:
		return "could not connect"
	}
}
