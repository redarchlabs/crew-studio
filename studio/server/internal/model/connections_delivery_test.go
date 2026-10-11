package model

import (
	"strings"
	"testing"
)

func TestDeliveryConnectionKinds(t *testing.T) {
	email := ConnectionInput{Name: "Mail", Kind: "email", AreaIDs: []string{"55555555-0000-4000-8000-000000000001"},
		Config: map[string]string{"host": "smtp.example.com", "port": "587", "security": " STARTTLS ", "from_address": "pmo@example.com"}}
	out, err := NormalizeConnection(email)
	if err != nil || out.Config["security"] != "starttls" {
		t.Fatalf("email = %+v, %v", out.Config, err)
	}
	email.Config = map[string]string{"host": "smtp.example.com", "port": "587", "security": "ssl3", "from_address": "pmo@example.com"}
	if _, err := NormalizeConnection(email); err == nil || !strings.Contains(err.Error(), "config.security: must be one of starttls, tls, none") {
		t.Errorf("bad security: %v", err)
	}
	for _, kind := range []string{"slack", "teams", "email"} {
		if k, _ := ConnectionKindOf(kind); !k.Testable {
			t.Errorf("%s is not testable", kind)
		}
	}
	// Webhook URLs: https anywhere, plain http only to this machine
	for url, ok := range map[string]bool{
		"https://hooks.slack.com/services/T/B/x": true, "http://127.0.0.1:8795/hook": true, "http://localhost/hook": true,
		"http://hooks.slack.com/services/x": false, "ftp://127.0.0.1/x": false, "http://10.0.0.5/hook": false,
	} {
		_, err := NormalizeSecrets("teams", map[string]string{"post_url": url})
		if (err == nil) != ok {
			t.Errorf("%s: %v", url, err)
		}
	}
	if !DestinationChanged("email", map[string]string{"security": "starttls"}, map[string]string{"security": "none"}) {
		t.Error("changing email security must clear the password")
	}
}
