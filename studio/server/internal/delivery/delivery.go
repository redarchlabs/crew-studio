// Package delivery sends finished reports to Slack, Microsoft Teams and email (docs/delivery.md). It holds no state:
// callers decide what to send and record the outcome. Secrets are opened on the server and never leave it; errors
// say what went wrong in general terms only, never a webhook URL, token or response body.
package delivery

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
)

const (
	// DefaultTimeout bounds one send (connect, upload, answer)
	DefaultTimeout = 30 * time.Second
	// maxAnswerBytes is read from an answer; only short error codes are ever shown
	maxAnswerBytes  = 16 << 10
	defaultSlackAPI = "https://slack.com/api"
)

// SecretSource opens a connection's secrets on the server.
type SecretSource interface {
	Secrets(ctx context.Context, id string) (map[string]string, error)
}

// Options configure the senders. The zero value is right for production.
type Options struct {
	// HTTP is the client for Slack and Teams; nil uses the connection transport (no proxy, no cloud metadata
	// addresses) and follows no redirects
	HTTP *http.Client
	// SlackAPIURL replaces https://slack.com/api (STUDIO_SLACK_API_URL; tests only)
	SlackAPIURL string
	// Origins are extra webhook origins an administrator allowed (STUDIO_DELIVERY_ORIGINS), such as
	// http://127.0.0.1:8795; plain http is only ever accepted for these
	Origins []string
	// Dial connects to SMTP servers; nil uses the connection dialer (no cloud metadata addresses)
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// TLS is the base TLS configuration for SMTP (tests trust a test certificate with it); nil uses the system roots
	TLS *tls.Config
	// Timeout bounds one send; 0 means DefaultTimeout
	Timeout time.Duration
}

// Message is what is sent: a title (the email subject and the heading of a chat message), Markdown, and a footer.
type Message struct {
	Title    string
	Markdown string
	// Footer says where the report came from, such as "Sent by Crew Studio, work order #12"
	Footer string
	// HTML, when set, is a self-contained styled page sent as the HTML part of an email beside the plain text (Slack
	// and Teams ignore it)
	HTML string
}

// Target is where one delivery goes besides its connection: the email recipients.
type Target struct {
	Recipients []string
}

// Error is a failed send, with a message that is safe to show and store.
type Error struct {
	Message string
	// Retryable is false when trying again cannot help until someone changes the connection
	Retryable bool
}

func (e *Error) Error() string { return e.Message }

func failf(retryable bool, format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...), Retryable: retryable}
}

// SafeMessage is an error's text when it is a delivery Error, or a general one.
func SafeMessage(err error) string {
	var de *Error
	if errors.As(err, &de) {
		return de.Message
	}
	return "the message could not be sent"
}

// Sender sends messages through slack, teams and email connections.
type Sender struct {
	secrets SecretSource
	opts    Options
	http    *http.Client
}

// New builds a sender.
func New(secrets SecretSource, opts Options) *Sender {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.SlackAPIURL == "" {
		opts.SlackAPIURL = defaultSlackAPI
	}
	opts.SlackAPIURL = strings.TrimRight(opts.SlackAPIURL, "/")
	client := opts.HTTP
	if client == nil {
		client = &http.Client{Transport: connections.Transport()}
	}
	// Never follow a redirect: a webhook's secret URL must not lead a request (and its body) somewhere else
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copied.Timeout = opts.Timeout
	return &Sender{secrets: secrets, opts: opts, http: &copied}
}

// Kinds are the connection kinds Send handles.
var Kinds = []string{model.DestinationSlack, model.DestinationTeams, model.DestinationEmail}

// Send delivers msg through conn. It answers an *Error on failure.
func (s *Sender) Send(ctx context.Context, conn model.Connection, to Target, msg Message) error {
	if !conn.Enabled {
		return failf(false, "the connection %s is turned off", conn.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	secrets, err := s.secrets.Secrets(ctx, conn.ID)
	if err != nil {
		return failf(false, "the secrets of %s cannot be read on the server", conn.Name)
	}
	switch conn.Kind {
	case model.DestinationSlack:
		return s.sendSlack(ctx, conn, secrets, msg)
	case model.DestinationTeams:
		return s.sendTeams(ctx, secrets, msg)
	case model.DestinationEmail:
		return s.sendEmail(ctx, conn, secrets, to, msg)
	}
	return failf(false, "%s is a %s connection, which cannot receive reports", conn.Name, conn.Kind)
}

// TestMessage is what "Test" on a connection sends.
func TestMessage(connName string) Message {
	return Message{
		Title:    "Crew Studio test message",
		Markdown: fmt.Sprintf("This is a test from the Crew Studio connection %q. If you can read it, reports can be delivered here.", oneLine(connName)),
		Footer:   "Sent by Crew Studio when someone tested this connection.",
	}
}
