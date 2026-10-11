package model

import (
	"strings"
	"testing"
	"time"
)

func TestNoteNamesAndSubjects(t *testing.T) {
	for _, ok := range []string{"weekly-report", "risk_register", "a", "v1.2", "atlas:summary"} {
		if !ValidNoteName(ok) {
			t.Errorf("%q should be a valid note name", ok)
		}
	}
	for _, bad := range []string{"", "-lead", "has space", "slash/name", strings.Repeat("n", 101), "naïve"} {
		if ValidNoteName(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
	if s, err := NormalizeSubject("  jira:PRJ-15 "); err != nil || s != "jira:PRJ-15" {
		t.Errorf("subject = %q, %v", s, err)
	}
	if s, err := NormalizeSubject(""); err != nil || s != "" {
		t.Errorf("empty subject = %q, %v", s, err)
	}
	for _, bad := range []string{"tab\there", "line\nbreak", strings.Repeat("s", 201)} {
		if _, err := NormalizeSubject(bad); err == nil {
			t.Errorf("subject %q should be refused", bad)
		}
	}
}

func TestNormalizeNotePut(t *testing.T) {
	v := 3
	out, err := NormalizeNotePut(NotePut{Name: "weekly-report", Subject: " project:atlas ", Content: "hello", ExpectedVersion: &v})
	if err != nil || out.Subject != "project:atlas" || *out.ExpectedVersion != 3 {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	neg := -1
	for name, tc := range map[string]struct {
		in   NotePut
		want string
	}{
		"bad name":     {NotePut{Name: "bad name", Content: "x"}, "name: use letters"},
		"too big":      {NotePut{Name: "n", Content: strings.Repeat("x", MaxNoteBytes+1)}, "content: is over the 65536-byte limit"},
		"neg version":  {NotePut{Name: "n", Content: "x", ExpectedVersion: &neg}, "expected_version: must be 0 or more"},
		"nul in text":  {NotePut{Name: "n", Content: "a\x00b"}, "content: must not contain NUL"},
		"long subject": {NotePut{Name: "n", Content: "x", Subject: strings.Repeat("s", 300)}, "subject: must be at most"},
	} {
		_, err := NormalizeNotePut(tc.in)
		if got := joinedDetails(t, err); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q does not mention %q", name, got, tc.want)
		}
	}
}

func TestNormalizeLogRecord(t *testing.T) {
	out, err := NormalizeLogRecord(LogRecord{Subject: " jira:PRJ-15", Action: "followup:due_soon", IdempotencyKey: " k1 "})
	if err != nil || out.Subject != "jira:PRJ-15" || out.IdempotencyKey != "k1" || out.Details == nil {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	big := map[string]any{"x": strings.Repeat("y", MaxLogDetailsBytes)}
	for name, tc := range map[string]struct {
		in   LogRecord
		want string
	}{
		"no subject":  {LogRecord{Action: "a"}, "subject: is required"},
		"bad action":  {LogRecord{Subject: "s", Action: "has space"}, "action: use letters"},
		"big details": {LogRecord{Subject: "s", Action: "a", Details: big}, "details: is over"},
		"long key":    {LogRecord{Subject: "s", Action: "a", IdempotencyKey: strings.Repeat("k", 201)}, "idempotency_key: must be at most"},
	} {
		_, err := NormalizeLogRecord(tc.in)
		if got := joinedDetails(t, err); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q does not mention %q", name, got, tc.want)
		}
	}
}

func TestParseWithin(t *testing.T) {
	for text, want := range map[string]time.Duration{
		"":    0,
		"30m": 30 * time.Minute,
		"24h": 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"2w":  14 * 24 * time.Hour,
	} {
		got, err := ParseWithin(text)
		if err != nil || got != want {
			t.Errorf("%q = %v, %v", text, got, err)
		}
	}
	for _, bad := range []string{"7", "d", "-1d", "0d", "1y", "99999d", "1.5d"} {
		if _, err := ParseWithin(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}
