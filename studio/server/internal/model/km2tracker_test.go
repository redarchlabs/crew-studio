package model

import (
	"strings"
	"testing"
)

func km2TrackerInput(config map[string]string, secrets map[string]string) ConnectionInput {
	return ConnectionInput{Name: "KM2 PMO", Kind: TrackerKM2, AreaIDs: []string{areaA}, Config: config, Secrets: secrets}
}

func TestKM2TrackerIsATracker(t *testing.T) {
	if !IsTrackerKind(TrackerKM2) || !ConnectionFits(ConnectionTracker, TrackerKM2) || IsTrackerKind(KindKM2) {
		t.Error("km2_tracker is a tracker kind and km2 is not")
	}
	if got, ok := NormalizeItemRef(" km2_tracker:erp-12 "); !ok || got != "ERP-12" {
		t.Errorf("ref = %q %v", got, ok)
	}
	if TrackerSubject(TrackerKM2, "ERP-1") != "km2_tracker:ERP-1" {
		t.Error("subject")
	}
	if err := CheckAgentLogRecord(LogRecord{Subject: "KM2_TRACKER:ERP-1", Action: "followup:due_soon"}); err == nil {
		t.Error("an agent recorded a follow-up of a KM2 tracker issue")
	}
	if k, ok := ConnectionKindOf(TrackerKM2); !ok || !k.Testable || k.Label == "" {
		t.Errorf("kind = %+v", k)
	}
}

func TestTrackerCapabilities(t *testing.T) {
	for kind, want := range map[string][2]bool{TrackerJira: {true, true}, TrackerLinear: {false, false}, TrackerKM2: {true, false}, "http": {false, false}} {
		if got := TrackerHasProjectLeads(kind); got != want[0] {
			t.Errorf("%s project leads = %v", kind, got)
		}
		if got := TrackerCommentMovesUpdated(kind); got != want[1] {
			t.Errorf("%s comment moves updated = %v", kind, got)
		}
	}
}

func TestNormalizeKM2TrackerConnection(t *testing.T) {
	out, err := NormalizeConnection(km2TrackerInput(map[string]string{"base_url": " https://km2.example.com/ ", "self_email": "bot@example.com",
		"entity_prefix": " pm_ ", "activity_field": "activity_at", "issue_url_template": "https://km2.example.com/r/trk_issue?record_id={id}",
		"allow_create": "true"}, map[string]string{"api_key": " km2_abc "}))
	if err != nil {
		t.Fatal(err)
	}
	if out.Config["base_url"] != "https://km2.example.com" || out.Config["entity_prefix"] != "pm_" || out.Secrets["api_key"] != "km2_abc" ||
		out.Config["allow_create"] != "true" {
		t.Errorf("out = %+v", out)
	}
	if _, err := NormalizeConnection(km2TrackerInput(map[string]string{"base_url": "http://127.0.0.1:8125", "self_email": "bot@example.com"}, nil)); err != nil {
		t.Errorf("loopback http refused: %v", err)
	}
	cases := map[string]struct {
		config  map[string]string
		secrets map[string]string
		want    string
	}{
		"plain http":     {map[string]string{"base_url": "http://km2.example.com", "self_email": "a@b.example"}, nil, "config.base_url: must be https"},
		"no self email":  {map[string]string{"base_url": "https://km2.example.com"}, nil, "config.self_email: is required"},
		"bad prefix":     {map[string]string{"base_url": "https://k.example", "self_email": "a@b.example", "entity_prefix": "Trk-"}, nil, "config.entity_prefix:"},
		"bad field":      {map[string]string{"base_url": "https://k.example", "self_email": "a@b.example", "activity_field": "x;drop"}, nil, "config.activity_field:"},
		"bad template":   {map[string]string{"base_url": "https://k.example", "self_email": "a@b.example", "issue_url_template": "javascript:alert({id})"}, nil, "config.issue_url_template:"},
		"no placeholder": {map[string]string{"base_url": "https://k.example", "self_email": "a@b.example", "issue_url_template": "https://k.example/x"}, nil, "{id} or {key}"},
		"not a km2 key":  {map[string]string{"base_url": "https://k.example", "self_email": "a@b.example"}, map[string]string{"api_key": "lin_123"}, "secrets.api_key: must start with km2_"},
	}
	for name, tc := range cases {
		_, err := NormalizeConnection(km2TrackerInput(tc.config, tc.secrets))
		if got := joinedDetails(t, err); !strings.Contains(got, tc.want) {
			t.Errorf("%s: details %q do not mention %q", name, got, tc.want)
		}
	}
	if _, err := NormalizeSecrets(TrackerKM2, map[string]string{"api_key": "nope"}); err == nil || !strings.Contains(err.Error(), "km2_") {
		t.Errorf("replace with a non-KM2 key: %v", err)
	}
	if !DestinationChanged(TrackerKM2, map[string]string{"base_url": "https://a.example"}, map[string]string{"base_url": "https://b.example"}) {
		t.Error("a new base URL must drop the saved key")
	}
}

func TestKM2TrackerSettingsDefaults(t *testing.T) {
	s := KM2TrackerSettingsOf(map[string]string{"base_url": "https://k.example", "self_email": "Bot@Example.com"})
	if s.EntityPrefix != "trk_" || s.ActivityField != "updated_at" || s.SelfEmail != "bot@example.com" || s.BaseURL != "https://k.example" {
		t.Errorf("settings = %+v", s)
	}
	if s.Entity("issue") != "trk_issue" {
		t.Errorf("entity = %s", s.Entity("issue"))
	}
	if got := s.IssueURL("rec-1", "ERP-1"); got != "" {
		t.Errorf("no template, url = %q", got)
	}
	s = KM2TrackerSettingsOf(map[string]string{"entity_prefix": "pm_", "activity_field": "activity_at", "issue_url_template": "https://k.example/r?id={id}&k={key}"})
	if s.Entity("comment") != "pm_comment" || s.ActivityField != "activity_at" {
		t.Errorf("settings = %+v", s)
	}
	if got := s.IssueURL("a b", "ERP-1"); got != "https://k.example/r?id=a+b&k=ERP-1" {
		t.Errorf("url = %q", got)
	}
}

func TestIssueURLEscapesByPlace(t *testing.T) {
	s := KM2TrackerSettingsOf(map[string]string{"issue_url_template": "https://k.example/issues/{key}/view?id={id}&k={key}#c-{id}"})
	if got := s.IssueURL("a&b c/d", "ERP 1/2"); got != "https://k.example/issues/ERP%201%2F2/view?id=a%26b+c%2Fd&k=ERP+1%2F2#c-a%26b+c%2Fd" {
		t.Errorf("url = %q", got)
	}
	for _, bad := range []string{"https://{id}.k.example/x", "https://k.example{key}/x", "{id}://k.example/x", "https://u{id}@k.example/x", "no-scheme/{id}"} {
		if got := KM2TrackerSettingsOf(map[string]string{"issue_url_template": bad}).IssueURL("1", "ERP-1"); got != "" {
			t.Errorf("%s gave %q", bad, got)
		}
	}
	if got := KM2TrackerSettingsOf(map[string]string{"issue_url_template": "https://k.example?id={id}"}).IssueURL("1 2", "K"); got != "https://k.example?id=1+2" {
		t.Errorf("query without a path = %q", got)
	}
}

func TestKM2TrackerSettingsAreChecked(t *testing.T) {
	for _, bad := range []string{"https://{id}.k.example/x", "https://k{id}.example/x"} {
		_, err := NormalizeConnection(km2TrackerInput(map[string]string{"base_url": "https://k.example", "self_email": "a@b.example", "issue_url_template": bad}, nil))
		if got := joinedDetails(t, err); !strings.Contains(got, "out of the scheme and host") {
			t.Errorf("%s: %q", bad, got)
		}
	}
	out, err := NormalizeConnection(km2TrackerInput(map[string]string{"base_url": "https://k.example", "self_email": "Crew.Studio@Example.COM"}, nil))
	if err != nil || out.Config["self_email"] != "crew.studio@example.com" {
		t.Errorf("self email = %q %v", out.Config["self_email"], err)
	}
	if !DestinationChanged(TrackerKM2, map[string]string{"base_url": "https://a.example", "entity_prefix": "trk_"},
		map[string]string{"base_url": "https://a.example", "entity_prefix": "hr_"}) {
		t.Error("a new entity prefix must drop the saved key")
	}
}
