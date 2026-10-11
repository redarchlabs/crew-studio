package model

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmailAddress(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"sam@Example.COM", "sam@example.com", true},
		{"  dana@example.org ", "dana@example.org", true},
		{"ops@localhost", "ops@localhost", true},
		{"Sam <sam@example.com>", "", false},
		{"sam@example.com\r\nBcc: x@evil.test", "", false},
		{"sam@example.com, x@evil.test", "", false},
		{"sam example@example.com", "", false},
		{"\"quoted\"@example.com", "", false},
		{"sam@nodot", "", false},
		{"@example.com", "", false},
		{"", "", false},
		{strings.Repeat("a", 250) + "@example.com", "", false},
	} {
		got, err := NormalizeEmailAddress(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("%q -> %q, %v", tc.in, got, err)
		}
	}
}

func TestNormalizeDestinations(t *testing.T) {
	conn := "11111111-0000-4000-8000-0000000000aa"
	folder := "AAAAAAAA-0000-4000-8000-000000000001"
	out, err := NormalizeDestinations([]ReportDestinationInput{
		{Kind: " slack ", ConnectionID: conn},
		{Kind: "email", ConnectionID: conn, Recipients: []string{"a@example.com", "B@Example.com"}},
		{Kind: "km2", ConnectionID: conn, Folder: folder},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Kind != "slack" || len(out[0].Recipients) != 0 || out[1].Recipients[1] != "B@example.com" || out[2].Folder != strings.ToLower(folder) {
		t.Errorf("out = %+v", out)
	}

	bad := []ReportDestinationInput{
		{Kind: "fax", ConnectionID: conn},
		{Kind: "slack", ConnectionID: "x"},
		{Kind: "email", ConnectionID: conn},
		{Kind: "email", ConnectionID: conn, Recipients: []string{"a@example.com", "a@example.com", "Evil <e@x.io>"}},
		{Kind: "teams", ConnectionID: conn, Recipients: []string{"a@example.com"}},
		{Kind: "slack", ConnectionID: conn, Folder: folder},
		{Kind: "km2", ConnectionID: conn, Folder: "not-a-uuid"},
		{Kind: "teams", ConnectionID: conn},
		{Kind: "teams", ConnectionID: conn},
	}
	_, err = NormalizeDestinations(bad)
	details := strings.Join(err.(*ValidationError).Details, "\n")
	for _, want := range []string{"destinations[0].kind", "destinations[1].connection_id", "destinations[2].recipients: give at least one",
		"lists a@example.com more than once", "is not a plain email address", "destinations[4].recipients: only email",
		"destinations[5].folder", "destinations[6].folder", "destinations[8]: is the same as an earlier destination"} {
		if !strings.Contains(details, want) {
			t.Errorf("missing %q in:\n%s", want, details)
		}
	}

	many := make([]ReportDestinationInput, MaxDestinations+1)
	for i := range many {
		many[i] = ReportDestinationInput{Kind: "email", ConnectionID: conn, Recipients: []string{strings.Repeat("x", i+1) + "@example.com"}}
	}
	if _, err := NormalizeDestinations(many); err == nil {
		t.Error("too many destinations accepted")
	}
	tooMany := make([]string, MaxEmailRecipients+1)
	for i := range tooMany {
		tooMany[i] = "p@example.com"
	}
	if _, err := NormalizeDestinations([]ReportDestinationInput{{Kind: "email", ConnectionID: conn, Recipients: tooMany}}); err == nil {
		t.Error("too many recipients accepted")
	}
}

func TestNormalizeReport(t *testing.T) {
	out, err := NormalizeReport(ReportRequest{Project: "  Project   Atlas ", Markdown: "\n# Status\nOn track\n"})
	if err != nil || out.Project != "Project Atlas" || out.Markdown != "# Status\nOn track" {
		t.Fatalf("out = %+v, %v", out, err)
	}
	for _, in := range []ReportRequest{
		{Project: "", Markdown: "x"},
		{Project: "Atlas", Markdown: "  "},
		{Project: "Atlas", Markdown: strings.Repeat("x", MaxReportBytes+1)},
		{Project: "Atlas", Markdown: "bad \x00 byte"},
		{Project: "Atlas", Markdown: "bad \xff utf8"},
		{Project: "Atlas\x07", Markdown: "x"},
		{Project: "!!!", Markdown: "x"},
		{Project: strings.Repeat("p", 101), Markdown: "x"},
	} {
		if _, err := NormalizeReport(in); err == nil {
			t.Errorf("%q accepted", in.Project)
		}
	}
}

func TestProjectSlugWeekAndKeys(t *testing.T) {
	for in, want := range map[string]string{
		"Atlas": "atlas", "Project Atlas (EU)": "project-atlas-eu", "  --Birch__2--  ": "birch-2", "Ünïcode": "n-code", "!!!": "",
		strings.Repeat("ab-", 40): strings.TrimRight(strings.Repeat("ab-", 20), "-"),
	} {
		if got := ProjectSlug(in); got != want {
			t.Errorf("ProjectSlug(%q) = %q, want %q", in, got, want)
		}
	}
	// Monday 2026-10-05 is in week 41; late Sunday in UTC is already Monday in Tokyo
	monday := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	if got := ISOWeek(monday, nil); got != "2026-W41" {
		t.Errorf("week = %s", got)
	}
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	if got := ISOWeek(time.Date(2026, 10, 4, 20, 0, 0, 0, time.UTC), tokyo); got != "2026-W41" {
		t.Errorf("tokyo week = %s", got)
	}
	if got := ReportExternalRef("atlas", CadenceWeekly, "2026-W41"); got != "atlas-weekly-2026-W41" {
		t.Errorf("ref = %s", got)
	}
	if ActionReport("teams") != "report:teams" || ProjectSubject("atlas") != "project:atlas" || DeliveryKey("r1", 2) != "report:r1:2" {
		t.Error("keys")
	}
	if !ValidAction(ActionReport(DestinationWorkOrder)) {
		t.Error("report action is not a valid action")
	}
	targets := map[string]ReportDelivery{
		"the work order output": {Kind: DestinationWorkOrder},
		"Mail (2 recipients)":   {Kind: DestinationEmail, ConnectionName: "Mail", Recipients: []string{"a", "b"}},
		"PMO channel":           {Kind: DestinationTeams, ConnectionName: "PMO channel"},
	}
	for want, d := range targets {
		if got := d.Target(); got != want {
			t.Errorf("target = %q, want %q", got, want)
		}
	}
}

func TestEmailDomains(t *testing.T) {
	cfg := map[string]string{ConfigEmailDomains: " Example.com, @example.org  "}
	if got := EmailDomains(cfg); len(got) != 2 || got[0] != "example.com" || got[1] != "example.org" {
		t.Errorf("domains = %v", got)
	}
	if out := RecipientsOutside(cfg, []string{"a@example.com", "b@EXAMPLE.org", "c@evil.io"}); len(out) != 1 || out[0] != "c@evil.io" {
		t.Errorf("outside = %v", out)
	}
	if out := RecipientsOutside(map[string]string{}, []string{"c@evil.io"}); len(out) != 0 {
		t.Errorf("no list = %v", out)
	}
}
