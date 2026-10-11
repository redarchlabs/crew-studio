package model

import (
	"strings"
	"testing"
)

func TestNormalizeChatSettings(t *testing.T) {
	agent := "22222222-0000-4000-8000-000000000001"
	blank := "  "
	got, err := NormalizeChatSettings(ChatSettings{Enabled: true, FrontDeskAgentID: &agent, LLMID: &blank, DailyBudgetUSD: 2})
	if err != nil || got.LLMID != nil || *got.FrontDeskAgentID != agent {
		t.Errorf("got %+v, %v", got, err)
	}
	bad := "nope"
	cases := []ChatSettings{
		{Enabled: true, DailyBudgetUSD: 1},
		{FrontDeskAgentID: &bad, DailyBudgetUSD: 1},
		{LLMID: &bad, DailyBudgetUSD: 1},
		{DailyBudgetUSD: 0},
		{DailyBudgetUSD: MaxChatDailyBudgetUSD + 1},
	}
	for i, c := range cases {
		if _, err := NormalizeChatSettings(c); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	if d := DefaultChatSettings("c1"); d.Enabled || d.DailyBudgetUSD != DefaultChatDailyBudgetUSD || d.CrewID != "c1" {
		t.Errorf("default = %+v", d)
	}
}

func TestNormalizeChatMessage(t *testing.T) {
	if got, err := NormalizeChatMessage("  hi  "); err != nil || got != "hi" {
		t.Errorf("got %q %v", got, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", MaxChatMessageLen+1), "a\x00b"} {
		if _, err := NormalizeChatMessage(bad); err == nil {
			t.Errorf("%q accepted", bad[:min(len(bad), 10)])
		}
	}
}

func TestChatTitle(t *testing.T) {
	if got := ChatTitle("What is\n\tslipping?"); got != "What is slipping?" {
		t.Errorf("got %q", got)
	}
	long := ChatTitle(strings.Repeat("word ", 100))
	if r := []rune(long); len(r) > 80 || !strings.HasSuffix(long, "…") {
		t.Errorf("long = %q", long)
	}
}
