// Package deliverytest has in-process fakes for report delivery tests: an SMTP server and a recorder for Slack and
// Teams webhooks. Nothing here contacts a real service.
package deliverytest

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Mail is one message the fake SMTP server accepted.
type Mail struct {
	From string
	To   []string
	Data string
	// TLS is true when the message arrived over TLS (STARTTLS or implicit)
	TLS bool
	// User is who signed in with AUTH PLAIN ("" when nobody did)
	User string
}

// SMTPServer is a small SMTP server for tests: EHLO, STARTTLS, AUTH PLAIN, MAIL, RCPT, DATA, RSET, NOOP, QUIT.
type SMTPServer struct {
	Addr string
	// Pool trusts the server's certificate
	Pool *tls.Config

	ln        net.Listener
	tlsConfig *tls.Config
	implicit  bool
	starttls  bool
	user      string
	password  string
	// RejectRcpt makes RCPT TO for this address fail with 550
	RejectRcpt string

	mu    sync.Mutex
	mails []Mail
}

// SMTPOptions choose how the fake behaves.
type SMTPOptions struct {
	// Implicit makes the server speak TLS from the first byte (port 465 style)
	Implicit bool
	// NoStartTLS hides the STARTTLS extension
	NoStartTLS bool
	// User and Password, when set, are the only credentials AUTH PLAIN accepts (and AUTH is then required)
	User, Password string
}

// NewSMTP starts a fake SMTP server on 127.0.0.1 for the test.
func NewSMTP(t testing.TB, opts SMTPOptions) *SMTPServer {
	t.Helper()
	cert, pool := selfSigned(t)
	s := &SMTPServer{tlsConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		implicit: opts.Implicit, starttls: !opts.NoStartTLS && !opts.Implicit, user: opts.User, password: opts.Password,
		Pool: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if s.implicit {
		ln = tls.NewListener(ln, s.tlsConfig)
	}
	s.ln, s.Addr = ln, ln.Addr().String()
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

// Mails returns the accepted messages.
func (s *SMTPServer) Mails() []Mail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mail(nil), s.mails...)
}

// HostPort splits Addr.
func (s *SMTPServer) HostPort() (string, string) {
	host, port, _ := net.SplitHostPort(s.Addr)
	return host, port
}

func (s *SMTPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.session(conn)
	}
}

type session struct {
	srv  *SMTPServer
	conn net.Conn
	r    *bufio.Reader
	mail Mail
	tls  bool
	auth string
}

func (s *SMTPServer) session(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	ss := &session{srv: s, conn: conn, r: bufio.NewReader(conn), tls: s.implicit}
	ss.reply("220 fake.smtp ESMTP ready")
	for {
		line, err := ss.r.ReadString('\n')
		if err != nil {
			return
		}
		if !ss.handle(strings.TrimRight(line, "\r\n")) {
			return
		}
	}
}

func (ss *session) reply(lines ...string) {
	_, _ = ss.conn.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
}

func (ss *session) handle(line string) bool {
	verb, arg, _ := strings.Cut(line, " ")
	switch strings.ToUpper(verb) {
	case "EHLO", "HELO":
		ext := []string{"250-fake.smtp", "250-8BITMIME"}
		if ss.srv.starttls && !ss.tls {
			ext = append(ext, "250-STARTTLS")
		}
		ext = append(ext, "250 AUTH PLAIN")
		ss.reply(ext...)
	case "STARTTLS":
		ss.reply("220 go ahead")
		tc := tls.Server(ss.conn, ss.srv.tlsConfig)
		if tc.Handshake() != nil {
			return false
		}
		ss.conn, ss.r, ss.tls = tc, bufio.NewReader(tc), true
	case "AUTH":
		ss.authPlain(arg)
	case "MAIL":
		if ss.srv.user != "" && ss.auth == "" {
			ss.reply("530 authentication required")
			return true
		}
		ss.mail = Mail{From: addrOf(arg), TLS: ss.tls, User: ss.auth}
		ss.reply("250 ok")
	case "RCPT":
		to := addrOf(arg)
		if to == ss.srv.RejectRcpt {
			ss.reply("550 no such user")
			return true
		}
		ss.mail.To = append(ss.mail.To, to)
		ss.reply("250 ok")
	case "DATA":
		ss.reply("354 end with .")
		ss.data()
	case "RSET", "NOOP":
		ss.reply("250 ok")
	case "QUIT":
		ss.reply("221 bye")
		return false
	default:
		ss.reply("502 not implemented")
	}
	return true
}

func (ss *session) authPlain(arg string) {
	mech, b64, _ := strings.Cut(arg, " ")
	raw, err := base64.StdEncoding.DecodeString(b64)
	parts := strings.Split(string(raw), "\x00")
	if !strings.EqualFold(mech, "PLAIN") || err != nil || len(parts) != 3 ||
		(ss.srv.user != "" && (parts[1] != ss.srv.user || parts[2] != ss.srv.password)) {
		ss.reply("535 authentication failed")
		return
	}
	ss.auth = parts[1]
	ss.reply("235 ok")
}

func (ss *session) data() {
	var b strings.Builder
	for {
		line, err := ss.r.ReadString('\n')
		if err != nil {
			return
		}
		if line == ".\r\n" {
			break
		}
		b.WriteString(strings.TrimPrefix(line, "."))
	}
	ss.mail.Data = b.String()
	ss.srv.mu.Lock()
	ss.srv.mails = append(ss.srv.mails, ss.mail)
	ss.srv.mu.Unlock()
	ss.reply("250 queued")
}

func addrOf(arg string) string {
	_, v, _ := strings.Cut(arg, ":")
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "<") {
		v, _, _ = strings.Cut(v[1:], ">")
	}
	return v
}

func selfSigned(t testing.TB) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, IsCA: true,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}
