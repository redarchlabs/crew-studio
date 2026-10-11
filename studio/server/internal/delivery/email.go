package delivery

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"html/template"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"slices"
	"strings"
	"time"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
)

// Email over SMTP (docs/delivery.md): STARTTLS (the default, usually port 587), implicit TLS (usually 465), or no
// encryption only to a relay on this machine. Every address is checked again before sending and must be a bare
// address, and the subject has no line breaks, so nothing a report or a setting holds can add mail headers.

const maxEmailBytes = 512 << 10

// smtpPorts are the ports mail servers listen on; any other port is refused (except on this machine), so an email
// connection cannot be used to probe other services.
var smtpPorts = []string{"25", "465", "587", "2525"}

func (s *Sender) sendEmail(ctx context.Context, conn model.Connection, secrets map[string]string, to Target, msg Message) error {
	cfg, err := smtpConfigOf(conn, secrets)
	if err != nil {
		return err
	}
	if len(to.Recipients) == 0 {
		return failf(false, "the email has no recipients")
	}
	rcpts := make([]string, 0, len(to.Recipients))
	for _, r := range to.Recipients {
		addr, err := model.NormalizeEmailAddress(r)
		if err != nil {
			return failf(false, "a recipient is not a plain email address")
		}
		rcpts = append(rcpts, addr)
	}
	if outside := model.RecipientsOutside(conn.Config, rcpts); len(outside) > 0 {
		return failf(false, "%s may only send to %s; %d recipients are outside them", conn.Name,
			strings.Join(model.EmailDomains(conn.Config), ", "), len(outside))
	}
	data, err := buildEmail(cfg.from, msg, time.Now())
	if err != nil {
		return err
	}
	return s.smtpSend(ctx, cfg, rcpts, data)
}

type smtpConfig struct {
	host, port, security, user, password, from string
}

func smtpConfigOf(conn model.Connection, secrets map[string]string) (smtpConfig, error) {
	cfg := smtpConfig{host: strings.TrimSpace(conn.Config["host"]), port: strings.TrimSpace(conn.Config["port"]),
		security: conn.Config[model.ConfigSMTPSecurity], user: conn.Config["username"], password: secrets["password"]}
	if cfg.security == "" {
		cfg.security = model.SMTPStartTLS
	}
	from, err := model.NormalizeEmailAddress(conn.Config["from_address"])
	if err != nil {
		return cfg, failf(false, "%s's from address is not a plain email address", conn.Name)
	}
	cfg.from = from
	if cfg.host == "" || cfg.port == "" || strings.ContainsAny(cfg.host, "/ \r\n") {
		return cfg, failf(false, "%s needs an SMTP host and port", conn.Name)
	}
	if !loopbackHost(cfg.host) && !slices.Contains(smtpPorts, cfg.port) {
		return cfg, failf(false, "%s must use an SMTP port (%s); other ports are only allowed to a relay on this machine", conn.Name, strings.Join(smtpPorts, ", "))
	}
	if cfg.security == model.SMTPNone && !loopbackHost(cfg.host) {
		return cfg, failf(false, "%s sends without encryption, which is only allowed to a relay on this machine (localhost); use starttls or tls", conn.Name)
	}
	if cfg.user != "" && cfg.password == "" {
		return cfg, failf(false, "%s has a user name but no password; replace its secret", conn.Name)
	}
	return cfg, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// buildEmail writes the message: plain UTF-8 text (the Markdown as written), quoted-printable, with a Q-encoded
// subject. Every header value is checked to be one line.
func buildEmail(from string, msg Message, now time.Time) ([]byte, error) {
	subject := oneLine(msg.Title)
	if subject == "" {
		subject = "Crew Studio report"
	}
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	_, domain, _ := strings.Cut(from, "@")
	headers := [][2]string{
		{"From", from},
		// Recipients are only in the envelope, so they do not see each other's addresses
		{"To", "undisclosed-recipients:;"},
		{"Subject", mime.QEncoding.Encode("utf-8", subject)},
		{"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", "<" + hex.EncodeToString(id) + "@" + domain + ">"},
		{"MIME-Version", "1.0"},
		{"Auto-Submitted", "auto-generated"},
	}
	boundary := ""
	if msg.HTML != "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		boundary = "crewstudio-" + hex.EncodeToString(b)
		headers = append(headers, [2]string{"Content-Type", `multipart/alternative; boundary="` + boundary + `"`})
	} else {
		headers = append(headers, [2]string{"Content-Type", "text/plain; charset=utf-8"}, [2]string{"Content-Transfer-Encoding", "quoted-printable"})
	}
	var buf bytes.Buffer
	for _, h := range headers {
		if strings.ContainsAny(h[1], "\r\n\x00") {
			return nil, failf(false, "the email could not be written safely")
		}
		buf.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	buf.WriteString("\r\n")
	body := msg.Markdown
	if msg.Footer != "" {
		body += "\n\n-- \n" + oneLine(msg.Footer)
	}
	if boundary == "" {
		if err := writeQP(&buf, body); err != nil {
			return nil, err
		}
	} else {
		// multipart/alternative: the plain text first, then the styled HTML that mail clients prefer
		html := msg.HTML
		if msg.Footer != "" {
			html = strings.Replace(html, "</body>", "<p style=\"color:#8a94a1;font-size:12px\">"+template.HTMLEscapeString(oneLine(msg.Footer))+"</p></body>", 1)
		}
		for _, part := range []struct{ ctype, text string }{{"text/plain", body}, {"text/html", html}} {
			buf.WriteString("--" + boundary + "\r\nContent-Type: " + part.ctype + "; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
			if err := writeQP(&buf, part.text); err != nil {
				return nil, err
			}
			buf.WriteString("\r\n")
		}
		buf.WriteString("--" + boundary + "--\r\n")
	}
	if buf.Len() > maxEmailBytes && msg.HTML != "" {
		// A report too large with its styled page still goes out as plain text
		msg.HTML = ""
		return buildEmail(from, msg, now)
	}
	if buf.Len() > maxEmailBytes {
		return nil, failf(false, "the email is too large")
	}
	return buf.Bytes(), nil
}

func writeQP(buf *bytes.Buffer, text string) error {
	qp := quotedprintable.NewWriter(buf)
	if _, err := qp.Write([]byte(strings.ReplaceAll(text, "\n", "\r\n"))); err != nil {
		return failf(false, "the email could not be written")
	}
	if err := qp.Close(); err != nil {
		return failf(false, "the email could not be written")
	}
	return nil
}

func (s *Sender) dial(ctx context.Context, address string) (net.Conn, error) {
	if s.opts.Dial != nil {
		return s.opts.Dial(ctx, "tcp", address)
	}
	return connections.Dialer(s.opts.Timeout).DialContext(ctx, "tcp", address)
}

func (s *Sender) tlsConfig(host string) *tls.Config {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if s.opts.TLS != nil {
		cfg = s.opts.TLS.Clone()
	}
	cfg.ServerName = host
	return cfg
}

// smtpSend talks to the server: connect (TLS straight away for "tls"), STARTTLS when asked (refusing a server
// that does not offer it), authenticate when a user is set, then send.
func (s *Sender) smtpSend(ctx context.Context, cfg smtpConfig, rcpts []string, data []byte) error {
	address := net.JoinHostPort(cfg.host, cfg.port)
	raw, err := s.dial(ctx, address)
	if err != nil {
		return failf(true, "could not reach the SMTP server %s: %s", address, transportProblem(err))
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	conn := raw
	if cfg.security == model.SMTPTLS {
		tc := tls.Client(raw, s.tlsConfig(cfg.host))
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return failf(false, "the TLS handshake with %s failed; check the port and security setting", address)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, cfg.host)
	if err != nil {
		_ = conn.Close()
		return smtpProblem(address, "greeting", err)
	}
	defer c.Close()
	if err := c.Hello("crew-studio"); err != nil {
		return smtpProblem(address, "greeting", err)
	}
	if cfg.security == model.SMTPStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return failf(false, "the SMTP server %s does not offer STARTTLS; choose tls or another server", address)
		}
		if err := c.StartTLS(s.tlsConfig(cfg.host)); err != nil {
			return failf(false, "STARTTLS with %s failed; check the server's certificate and security setting", address)
		}
	}
	if cfg.user != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.user, cfg.password, cfg.host)); err != nil {
			return smtpProblem(address, "sign-in", err)
		}
	}
	return sendData(c, address, cfg.from, rcpts, data)
}

func sendData(c *smtp.Client, address, from string, rcpts []string, data []byte) error {
	if err := c.Mail(from); err != nil {
		return smtpProblem(address, "sender", err)
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return smtpProblem(address, "recipient "+r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return smtpProblem(address, "message", err)
	}
	if _, err := w.Write(data); err != nil {
		return smtpProblem(address, "message", err)
	}
	if err := w.Close(); err != nil {
		return smtpProblem(address, "message", err)
	}
	_ = c.Quit()
	return nil
}

// smtpProblem reports an SMTP failure with the server's reply code only (never its text, which can echo data).
func smtpProblem(address, step string, err error) error {
	var tp *textproto.Error
	if errors.As(err, &tp) {
		retry := tp.Code >= 400 && tp.Code < 500
		return failf(retry, "the SMTP server %s refused the %s (code %d)", address, step, tp.Code)
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return failf(true, "the SMTP server %s timed out during the %s", address, step)
	}
	if strings.Contains(err.Error(), "unencrypted connection") {
		return failf(false, "the password would be sent unencrypted to %s; use starttls or tls", address)
	}
	return failf(true, "the SMTP %s with %s failed", step, address)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
