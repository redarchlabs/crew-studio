package delivery

import (
	"context"
	"errors"
	"io"
	"mime/quotedprintable"
	"net/http"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/delivery/deliverytest"
	"crewstudio/internal/model"
)

type secretMap map[string]map[string]string

func (m secretMap) Secrets(_ context.Context, id string) (map[string]string, error) {
	s, ok := m[id]
	if !ok {
		return nil, errors.New("no such secret")
	}
	return s, nil
}

var report = Message{Title: "Atlas weekly status, 2026-W41", Markdown: "# Status\nOn **track** & <fine>\n- [PRJ-10](https://jira.example.com/browse/PRJ-10)",
	Footer: "Sent by Crew Studio, work order #7"}

func conn(id, kind string, config map[string]string) model.Connection {
	return model.Connection{ID: id, Name: "conn " + id, Kind: kind, Config: config, Enabled: true}
}

func TestSlackWebhookAndBot(t *testing.T) {
	hooks := deliverytest.NewHooks(t)
	secrets := secretMap{
		"hook":   {"webhook_url": hooks.URL + "/slack/hook/T1/B1/x"},
		"bot":    {"bot_token": hooks.BotToken},
		"badbot": {"bot_token": "xoxb-wrong"},
		"remote": {"webhook_url": "https://example.com/services/x"},
		"none":   {},
	}
	s := New(secrets, Options{Origins: []string{hooks.URL}, SlackAPIURL: hooks.URL + "/slack/api"})
	ctx := context.Background()
	if err := s.Send(ctx, conn("hook", "slack", nil), Target{}, report); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(ctx, conn("bot", "slack", map[string]string{"channel": "C0123"}), Target{}, report); err != nil {
		t.Fatal(err)
	}
	reqs := hooks.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	text, _ := reqs[0].Body["text"].(string)
	for _, want := range []string{"*Atlas weekly status, 2026-W41*", "*Status*", "On *track* &amp; &lt;fine&gt;", "• <https://jira.example.com/browse/PRJ-10|PRJ-10>", "_Sent by Crew Studio, work order #7_"} {
		if !strings.Contains(text, want) {
			t.Errorf("slack text lacks %q:\n%s", want, text)
		}
	}
	if reqs[1].Path != "/slack/api/chat.postMessage" || reqs[1].Body["channel"] != "C0123" || reqs[1].Header.Get("Authorization") != "Bearer "+hooks.BotToken {
		t.Errorf("bot request = %+v", reqs[1])
	}

	for name, tc := range map[string]struct {
		c    model.Connection
		want string
	}{
		"no channel": {conn("bot", "slack", nil), "has a bot token but no channel"},
		"bad token":  {conn("badbot", "slack", map[string]string{"channel": "C1"}), "refused the bot token (invalid_auth)"},
		"other host": {conn("remote", "slack", nil), "must start with https://hooks.slack.com/"},
		"no secret":  {conn("none", "slack", nil), "has no webhook URL or bot token"},
		"wrong kind": {conn("hook", "jira", nil), "cannot receive reports"},
		"no secrets": {conn("missing", "slack", nil), "cannot be read on the server"},
		"disabled":   {model.Connection{ID: "hook", Name: "off", Kind: "slack"}, "is turned off"},
	} {
		err := s.Send(ctx, tc.c, Target{}, report)
		if err == nil || !strings.Contains(SafeMessage(err), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// An unlisted plain-http webhook is refused before anything is sent
	strict := New(secrets, Options{})
	if err := strict.Send(ctx, conn("hook", "slack", nil), Target{}, report); err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Errorf("unlisted http: %v", err)
	}

	hooks.Fail(http.StatusNotFound, "not_in_channel")
	err := s.Send(ctx, conn("hook", "slack", nil), Target{}, report)
	var de *Error
	if !errors.As(err, &de) || de.Retryable || !strings.Contains(de.Message, "HTTP 404: channel_not_found") {
		t.Errorf("404: %v", err)
	}
	err = s.Send(ctx, conn("bot", "slack", map[string]string{"channel": "C9"}), Target{}, report)
	if err == nil || !strings.Contains(err.Error(), "not in C9; invite it") {
		t.Errorf("not in channel: %v", err)
	}
	hooks.Fail(http.StatusTooManyRequests, "")
	if err := s.Send(ctx, conn("hook", "slack", nil), Target{}, report); !errors.As(err, &de) || !de.Retryable {
		t.Errorf("429: %v", err)
	}
}

func TestTeamsWorkflowCard(t *testing.T) {
	hooks := deliverytest.NewHooks(t)
	secrets := secretMap{
		"wf":        {"post_url": hooks.URL + "/teams/workflows/abc/triggers/manual/paths/invoke?sig=secret"},
		"connector": {"post_url": "https://acme.webhook.office.com/webhookb2/x"},
		"logic":     {"post_url": "https://prod-01.westus.logic.azure.com/workflows/x"},
		"empty":     {},
	}
	s := New(secrets, Options{Origins: []string{hooks.URL + "/"}})
	ctx := context.Background()
	big := report
	big.Markdown = strings.Repeat("A long line of status text.\n", 2000)
	if err := s.Send(ctx, conn("wf", "teams", nil), Target{}, big); err != nil {
		t.Fatal(err)
	}
	req := hooks.Requests()[0]
	att := req.Body["attachments"].([]any)[0].(map[string]any)
	card := att["content"].(map[string]any)
	if req.Body["type"] != "message" || att["contentType"] != adaptiveCardType || att["contentUrl"] != nil || card["version"] != "1.2" {
		t.Errorf("card = %+v", req.Body)
	}
	blocks := card["body"].([]any)
	text := blocks[1].(map[string]any)["text"].(string)
	if len(text) > maxTeamsBytes || !strings.HasSuffix(text, teamsCutNote) || len(blocks) != 3 {
		t.Errorf("body text %d bytes, %d blocks", len(text), len(blocks))
	}
	if got := toTeams("## Risks\nplain"); got != "**Risks**\nplain" {
		t.Errorf("toTeams = %q", got)
	}
	secrets["internal"] = map[string]string{"post_url": "https://intranet.example.com/hook"}
	for id, want := range map[string]string{"connector": "connectors were retired", "logic": "stopped working on 30 November 2025", "empty": "no workflow URL",
		"internal": "must be on a Power Platform host"} {
		if err := s.Send(ctx, conn(id, "teams", nil), Target{}, report); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", id, err)
		}
	}
	hooks.Fail(http.StatusBadGateway, "")
	var de *Error
	if err := s.Send(ctx, conn("wf", "teams", nil), Target{}, report); !errors.As(err, &de) || !de.Retryable || strings.Contains(de.Message, "sig=") {
		t.Errorf("502: %v", err)
	}
	hooks.Fail(http.StatusMovedPermanently, "")
	if err := s.Send(ctx, conn("wf", "teams", nil), Target{}, report); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("redirect: %v", err)
	}
}

func emailConn(id, host, port, security string, extra map[string]string) model.Connection {
	cfg := map[string]string{"host": host, "port": port, "security": security, "from_address": "pmo@example.com"}
	for k, v := range extra {
		cfg[k] = v
	}
	return conn(id, "email", cfg)
}

func TestEmailOverSTARTTLSAndTLS(t *testing.T) {
	starttls := deliverytest.NewSMTP(t, deliverytest.SMTPOptions{User: "pmo", Password: "pw"})
	implicit := deliverytest.NewSMTP(t, deliverytest.SMTPOptions{Implicit: true})
	secrets := secretMap{"mail": {"password": "pw"}, "none": {}}
	s := New(secrets, Options{TLS: starttls.Pool})
	ctx := context.Background()
	host, port := starttls.HostPort()
	msg := report
	msg.Title = "Atlas report\r\nBcc: victim@example.com"
	to := Target{Recipients: []string{"sam@example.com", "dana@Example.com"}}
	if err := s.Send(ctx, emailConn("mail", host, port, "", map[string]string{"username": "pmo"}), to, msg); err != nil {
		t.Fatal(err)
	}
	mails := starttls.Mails()
	if len(mails) != 1 || !mails[0].TLS || mails[0].User != "pmo" || mails[0].From != "pmo@example.com" || len(mails[0].To) != 2 || mails[0].To[1] != "dana@example.com" {
		t.Fatalf("mails = %+v", mails)
	}
	head, body, _ := strings.Cut(mails[0].Data, "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") || !strings.Contains(head, "Subject: Atlas report Bcc: victim@example.com") ||
		!strings.Contains(head, "Content-Transfer-Encoding: quoted-printable") || !strings.Contains(head, "To: undisclosed-recipients:;") ||
		strings.Contains(head, "sam@example.com") {
		t.Errorf("headers:\n%s", head)
	}
	raw, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if decoded := string(raw); !strings.Contains(decoded, "On **track** & <fine>") || !strings.Contains(decoded, "-- \r\nSent by Crew Studio") {
		t.Errorf("body:\n%s", decoded)
	}

	ih, ip := implicit.HostPort()
	tlsSender := New(secrets, Options{TLS: implicit.Pool})
	if err := tlsSender.Send(ctx, emailConn("none", ih, ip, "tls", nil), Target{Recipients: []string{"sam@example.com"}}, report); err != nil {
		t.Fatal(err)
	}
	if m := implicit.Mails(); len(m) != 1 || !m[0].TLS {
		t.Errorf("implicit = %+v", m)
	}
	// Without the test certificate the server is not trusted
	if err := New(secrets, Options{}).Send(ctx, emailConn("none", ih, ip, "tls", nil), Target{Recipients: []string{"a@example.com"}}, report); err == nil ||
		!strings.Contains(err.Error(), "TLS handshake") {
		t.Errorf("untrusted: %v", err)
	}
}

func TestEmailRefusals(t *testing.T) {
	plain := deliverytest.NewSMTP(t, deliverytest.SMTPOptions{NoStartTLS: true})
	plain.RejectRcpt = "gone@example.com"
	host, port := plain.HostPort()
	secrets := secretMap{"m": {}, "pw": {"password": "secret"}}
	s := New(secrets, Options{TLS: plain.Pool, Timeout: 5 * time.Second})
	ctx := context.Background()
	to := Target{Recipients: []string{"sam@example.com"}}
	// No encryption is fine to a relay on this machine, without a password
	if err := s.Send(ctx, emailConn("m", "127.0.0.1", port, "none", nil), to, report); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		c    model.Connection
		to   Target
		want string
	}{
		"no starttls":     {emailConn("m", host, port, "starttls", nil), to, "does not offer STARTTLS"},
		"clear remote":    {emailConn("m", "smtp.example.com", "587", "none", nil), to, "only allowed to a relay on this machine"},
		"odd port":        {emailConn("m", "smtp.example.com", "6379", "starttls", nil), to, "must use an SMTP port"},
		"other domain":    {emailConn("m", host, port, "none", map[string]string{"allowed_domains": "example.org"}), to, "may only send to example.org"},
		"user no pw":      {emailConn("m", host, port, "none", map[string]string{"username": "u"}), to, "has a user name but no password"},
		"rejected rcpt":   {emailConn("m", host, port, "none", nil), Target{Recipients: []string{"gone@example.com"}}, "refused the recipient gone@example.com (code 550)"},
		"bad rcpt":        {emailConn("m", host, port, "none", nil), Target{Recipients: []string{"x@y.z\r\nBcc: e@x.io"}}, "not a plain email address"},
		"no rcpt":         {emailConn("m", host, port, "none", nil), Target{}, "has no recipients"},
		"bad from":        {emailConn("m", host, port, "none", map[string]string{"from_address": "Evil <e@x.io>"}), to, "from address"},
		"no host":         {emailConn("m", "", port, "none", nil), to, "needs an SMTP host and port"},
		"blocked address": {emailConn("m", "169.254.169.254", "25", "starttls", nil), to, "not allowed"},
	} {
		err := s.Send(ctx, tc.c, tc.to, report)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := len(plain.Mails()); n != 1 {
		t.Errorf("mails = %d", n)
	}
}

func TestMarkdownHelpers(t *testing.T) {
	if got := toSlack("```\n**not bold** <x>\n```\n__b__ [t|x](https://e.com/?a=1&b=2)"); got != "```\n**not bold** &lt;x&gt;\n```\n*b* <https://e.com/?a=1&b=2|t/x>" {
		t.Errorf("toSlack = %q", got)
	}
	if got := clip("héllo wörld", 9, "…"); len(got) > 9 || !strings.HasSuffix(got, "…") {
		t.Errorf("clip = %q", got)
	}
	if got := oneLine(" a\r\n\tb "); got != "a b" {
		t.Errorf("oneLine = %q", got)
	}
	if SafeMessage(errors.New("boom")) != "the message could not be sent" {
		t.Error("SafeMessage")
	}
	if !teamsHost("default123.environment.api.powerplatform.com") || teamsHost("powerplatform.com.evil.io") {
		t.Error("teamsHost")
	}
	if m := TestMessage("PMO"); !strings.Contains(m.Markdown, `"PMO"`) {
		t.Errorf("test message = %+v", m)
	}
}
