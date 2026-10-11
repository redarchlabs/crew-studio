package model

import (
	"strings"
	"testing"
)

const areaA = "55555555-0000-4000-8000-000000000001"

// joinedDetails is every validation problem in one string, for "mentions" checks.
func joinedDetails(t *testing.T, err error) string {
	t.Helper()
	return strings.Join(detailsOf(t, err), "; ")
}

func TestNormalizeConnectionCleansAndDefaults(t *testing.T) {
	in := ConnectionInput{
		Name: "  Acme Jira ", Kind: "jira", AreaIDs: []string{areaA},
		Config: map[string]string{"site_url": " https://acme.atlassian.net/ ", "email": "bot@acme.example"},
	}
	out, err := NormalizeConnection(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "Acme Jira" || out.Config["site_url"] != "https://acme.atlassian.net" || out.Enabled == nil || !*out.Enabled {
		t.Errorf("out = %+v", out)
	}
	if in.Config["site_url"] != " https://acme.atlassian.net/ " {
		t.Error("input config was changed in place")
	}
}

func TestNormalizeConnectionRejectsBadInput(t *testing.T) {
	cases := map[string]struct {
		in   ConnectionInput
		want string
	}{
		"no name":       {ConnectionInput{Kind: "linear", AreaIDs: []string{areaA}}, "name: is required"},
		"bad kind":      {ConnectionInput{Name: "x", Kind: "ftp", AreaIDs: []string{areaA}}, "kind: must be one of"},
		"no areas":      {ConnectionInput{Name: "x", Kind: "linear"}, "area_ids: pick at least one"},
		"unknown field": {ConnectionInput{Name: "x", Kind: "linear", AreaIDs: []string{areaA}, Config: map[string]string{"color": "red"}}, `config: unknown setting "color"`},
		"secret in config": {ConnectionInput{Name: "x", Kind: "linear", AreaIDs: []string{areaA}, Config: map[string]string{"api_key": "k"}},
			`config: "api_key" is a secret`},
		"missing required": {ConnectionInput{Name: "x", Kind: "jira", AreaIDs: []string{areaA}, Config: map[string]string{"email": "a@b.example"}},
			"config.site_url: is required"},
		"bad url": {ConnectionInput{Name: "x", Kind: "http", AreaIDs: []string{areaA}, Config: map[string]string{"base_url": "ftp://x"}},
			"config.base_url: must be an http or https URL"},
		"url with password": {ConnectionInput{Name: "x", Kind: "http", AreaIDs: []string{areaA}, Config: map[string]string{"base_url": "https://u:p@x.example"}},
			"must not contain a user name or password"},
		"bad port": {ConnectionInput{Name: "x", Kind: "email", AreaIDs: []string{areaA},
			Config: map[string]string{"host": "smtp.example", "port": "99999", "from_address": "a@b.example"}}, "config.port: must be a port"},
		"bad email": {ConnectionInput{Name: "x", Kind: "email", AreaIDs: []string{areaA},
			Config: map[string]string{"host": "smtp.example", "port": "587", "from_address": "nope"}}, "config.from_address: must be an email"},
		"bad header": {ConnectionInput{Name: "x", Kind: "http", AreaIDs: []string{areaA},
			Config: map[string]string{"base_url": "https://x.example", "auth_header": "Bad Header:"}}, "config.auth_header: must be an HTTP header name"},
		"secrets on input": {ConnectionInput{Name: "x", Kind: "linear", AreaIDs: []string{areaA}, Secrets: map[string]string{"nope": "1"}},
			`secrets: unknown secret "nope"`},
	}
	for name, tc := range cases {
		_, err := NormalizeConnection(tc.in)
		if got := joinedDetails(t, err); !strings.Contains(got, tc.want) {
			t.Errorf("%s: details %q do not mention %q", name, got, tc.want)
		}
	}
}

func TestNormalizeSecrets(t *testing.T) {
	out, err := NormalizeSecrets("jira", map[string]string{"api_token": "  tok "})
	if err != nil || out["api_token"] != "tok" {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	for name, tc := range map[string]struct {
		kind    string
		secrets map[string]string
		want    string
	}{
		"empty value":    {"jira", map[string]string{"api_token": " "}, "secrets.api_token: must not be empty"},
		"newline":        {"jira", map[string]string{"api_token": "a\nb"}, "secrets.api_token: must be one line"},
		"too long":       {"jira", map[string]string{"api_token": strings.Repeat("x", maxSecretLen+1)}, "must be at most"},
		"not a secret":   {"jira", map[string]string{"site_url": "https://x.example"}, `secrets: "site_url" is not a secret`},
		"http webhook":   {"teams", map[string]string{"post_url": "http://x.example/hook"}, "secrets.post_url: must be an https URL"},
		"unknown kind":   {"ftp", map[string]string{"x": "y"}, "kind: must be one of"},
		"none for kind?": {"slack", map[string]string{}, "secrets: give at least one secret"},
	} {
		_, err := NormalizeSecrets(tc.kind, tc.secrets)
		if got := joinedDetails(t, err); !strings.Contains(got, tc.want) {
			t.Errorf("%s: details %q do not mention %q", name, got, tc.want)
		}
	}
}

func TestMissingSecrets(t *testing.T) {
	if got := MissingSecrets("jira", nil); len(got) != 1 || got[0] != "api_token" {
		t.Errorf("jira = %v", got)
	}
	if got := MissingSecrets("jira", []string{"api_token"}); len(got) != 0 {
		t.Errorf("jira set = %v", got)
	}
	if got := MissingSecrets("slack", nil); len(got) != 1 || got[0] != "webhook_url or bot_token" {
		t.Errorf("slack = %v", got)
	}
	if got := MissingSecrets("slack", []string{"bot_token"}); len(got) != 0 {
		t.Errorf("slack bot = %v", got)
	}
	if got := MissingSecrets("http", nil); len(got) != 0 {
		t.Errorf("http needs no secret: %v", got)
	}
}

func TestEveryKindIsDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range ConnectionKinds {
		seen[k.Kind] = true
		if k.Label == "" || len(k.Fields) == 0 && k.Kind != "linear" {
			t.Errorf("%s is not fully described", k.Kind)
		}
	}
	for _, kind := range []string{"jira", "linear", "km2", "http", "slack", "teams", "email"} {
		if !seen[kind] {
			t.Errorf("kind %s missing", kind)
		}
	}
	if k, ok := ConnectionKindOf("http"); !ok || !k.Testable {
		t.Error("http connections can be tested")
	}
	for _, kind := range []string{"jira", "linear"} {
		if k, _ := ConnectionKindOf(kind); !k.Testable {
			t.Errorf("%s connections can be tested (phase 4)", kind)
		}
	}
	if k, _ := ConnectionKindOf("km2"); !k.Testable {
		t.Error("km2 connections can be tested (phase 6)")
	}
}

func TestDestinationChanged(t *testing.T) {
	base := map[string]string{"base_url": "https://a.example", "auth_header": "X-Key"}
	if DestinationChanged("http", base, map[string]string{"base_url": "https://a.example", "auth_header": "X-Key"}) {
		t.Error("same settings")
	}
	if !DestinationChanged("http", base, map[string]string{"base_url": "https://evil.example", "auth_header": "X-Key"}) {
		t.Error("new URL")
	}
	if !DestinationChanged("http", base, map[string]string{"base_url": "https://a.example", "auth_header": "Referer"}) {
		t.Error("new header")
	}
	if !DestinationChanged("email", map[string]string{"host": "a", "port": "587"}, map[string]string{"host": "a", "port": "25"}) {
		t.Error("new port")
	}
	if DestinationChanged("email", map[string]string{"host": "a", "from_address": "x@a.example"}, map[string]string{"host": "a", "from_address": "y@a.example"}) {
		t.Error("from address is not a destination")
	}
	if DestinationChanged("ftp", nil, map[string]string{"x": "y"}) {
		t.Error("unknown kind")
	}
}
