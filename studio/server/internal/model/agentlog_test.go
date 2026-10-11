package model

import (
	"strings"
	"testing"
)

func TestAgentsCannotForgeStudioEntries(t *testing.T) {
	for _, ok := range []LogRecord{
		{Subject: "PRJ-1", Action: "followup:due_today"}, // a follow-up of a non-tracker watch item
		{Subject: "person:sam", Action: "reminder:sent", IdempotencyKey: "w41-sam"},
		{Subject: "project:atlas", Action: "risk:raised"},
	} {
		if err := CheckAgentLogRecord(ok); err != nil {
			t.Errorf("%+v refused: %v", ok, err)
		}
	}
	for want, bad := range map[string]LogRecord{
		"tracker: actions":            {Subject: "jira:PRJ-1", Action: "tracker:comment"},
		"TRACKER: actions":            {Subject: "x", Action: "TRACKER:suggest"},
		"report: actions":             {Subject: "project:atlas", Action: "report:slack"},
		"follow-ups of tracker":       {Subject: "jira:PRJ-15", Action: "followup:due_soon"},
		"follow-ups of tracker ":      {Subject: " Linear:ENG-4", Action: "followup:overdue_escalate"},
		"must not start with run:":    {Subject: "x", Action: "note", IdempotencyKey: "run:abc:jira:PRJ-1:tracker:comment"},
		"must not start with report:": {Subject: "x", Action: "note", IdempotencyKey: "Report:r1:2"},
	} {
		err := CheckAgentLogRecord(bad)
		if err == nil || !strings.Contains(err.Error(), strings.TrimSpace(want)) && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(strings.TrimSpace(want))) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
