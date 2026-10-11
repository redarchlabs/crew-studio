package connections

import (
	"net"
	"net/http"
	"syscall"
	"time"
)

// Transport is the HTTP transport for calls made through http connections (tests and watch fetches): no proxy,
// and no link-local (cloud metadata), multicast or unspecified addresses, checked when connecting. Loopback and
// private networks stay allowed, because connections to internal APIs are expected and only connections.manage
// holders choose a connection's base URL.
func Transport() *http.Transport { return testTransport() }

// Dialer is a dialer with the same address checks as Transport, for clients that are not HTTP (SMTP).
func Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil || !allowedAddress(host) {
			return errBlockedAddress
		}
		return nil
	}}
}

// ErrBlockedAddress is what a dial through Dialer or Transport fails with for an address that is not allowed.
var ErrBlockedAddress = errBlockedAddress
