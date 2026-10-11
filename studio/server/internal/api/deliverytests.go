package api

import (
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"crewstudio/internal/connections"
	"crewstudio/internal/delivery"
	"crewstudio/internal/model"
)

// "Test" on a slack, teams or email connection sends a real test message: to the channel, or by email to the
// person testing (the connection's from address when they have no email). Nobody can choose other recipients here,
// so Test cannot be used to send mail to arbitrary people.

const (
	deliveryTestsPerMinute = 6
	deliveryTestBurst      = 3
)

var deliveryTests = newKeyedLimiter(rate.Every(time.Minute/deliveryTestsPerMinute), deliveryTestBurst)

// deliveryTestResult is a connection test that sent a message.
type deliveryTestResult struct {
	connections.TestResult
	// SentTo says where the test message went, such as an email address
	SentTo string `json:"sent_to,omitempty"`
}

func (s *Server) testDeliveryConnection(w http.ResponseWriter, r *http.Request, c model.Connection) {
	p := principal(r)
	if !deliveryTests.allow(p.UserID) {
		tooMany(w, "Test messages are being sent too quickly; wait a moment.")
		return
	}
	res := deliveryTestResult{TestResult: connections.TestResult{Supported: true}}
	to := delivery.Target{}
	if c.Kind == model.DestinationEmail {
		addr := p.Email
		if _, err := model.NormalizeEmailAddress(addr); err != nil {
			addr = c.Config["from_address"]
		}
		to.Recipients, res.SentTo = []string{addr}, addr
	}
	start := time.Now()
	err := s.sender.Send(r.Context(), c, to, delivery.TestMessage(c.Name))
	res.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Message = "The test message was not sent: " + delivery.SafeMessage(err) + "."
		ok(w, res)
		return
	}
	res.OK = true
	res.Message = "Sent a test message. Check that it arrived."
	if res.SentTo != "" {
		res.Message = "Sent a test email to " + res.SentTo + ". Check that it arrived."
	}
	s.auditConnection(r, "connection_test_message_sent", c, map[string]any{"sent_to": res.SentTo})
	ok(w, res)
}
