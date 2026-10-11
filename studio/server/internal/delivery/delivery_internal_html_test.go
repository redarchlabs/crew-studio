package delivery

import (
	"strings"
	"testing"
	"time"
)

func TestEmailWithHTMLIsMultipartAlternative(t *testing.T) {
	msg := Message{Title: "ERP daily report, 2026-10-06", Markdown: "Overdue 2\n\nAll fine.", Footer: "Sent by Crew Studio <PMO>.",
		HTML: "<!DOCTYPE html><html><body><h1>ERP</h1></body></html>"}
	raw, err := buildEmail("pmo@example.com", msg, time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`Content-Type: multipart/alternative; boundary="crewstudio-`, "Content-Type: text/plain; charset=utf-8",
		"Content-Type: text/html; charset=utf-8", "Overdue 2", "<h1>ERP</h1>", "Sent by Crew Studio &lt;PMO&gt;.</p></body>"} {
		if !strings.Contains(strings.ReplaceAll(text, "=\r\n", ""), want) {
			t.Errorf("email lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "text/plain; charset") > strings.Index(text, "text/html; charset") {
		t.Error("the plain part must come first")
	}
	// Too large with its page: sent as plain text alone
	msg.HTML = "<html><body>" + strings.Repeat("<p>x</p>", maxEmailBytes/7) + "</body></html>"
	raw, err = buildEmail("pmo@example.com", msg, time.Now())
	if err != nil || strings.Contains(string(raw), "multipart") || !strings.Contains(string(raw), "Content-Type: text/plain; charset=utf-8") {
		t.Errorf("oversized html: %v", err)
	}
}
