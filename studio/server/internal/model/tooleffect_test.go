package model

import "testing"

func TestCheckToolEffect(t *testing.T) {
	cases := []struct {
		name string
		tool Tool
		ok   bool
	}{
		{"reads with no needs", Tool{Key: "a", Effect: EffectReads, Needs: []string{}}, true},
		{"needs left out", Tool{Key: "a", Effect: EffectReads}, false},
		{"changes with needs", Tool{Key: "a", Effect: EffectChanges, Needs: []string{NeedCrew, NeedSchedule}}, true},
		{"proposes", Tool{Key: "a", Effect: EffectProposes, Needs: []string{NeedTracker, NeedKM2}}, true},
		{"missing effect", Tool{Key: "a", Needs: []string{}}, false},
		{"unknown effect", Tool{Key: "a", Effect: "writes", Needs: []string{}}, false},
		{"unknown need", Tool{Key: "a", Effect: EffectReads, Needs: []string{"mcp"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckToolEffect(tc.tool); (err == nil) != tc.ok {
				t.Errorf("CheckToolEffect(%+v) = %v", tc.tool, err)
			}
		})
	}
}

func TestChatNeedOf(t *testing.T) {
	for kind, want := range map[string]string{TrackerJira: NeedTracker, TrackerLinear: NeedTracker, TrackerKM2: NeedTracker,
		KindKM2: NeedKM2, "smtp": "", "": ""} {
		if got := ChatNeedOf(kind); got != want {
			t.Errorf("ChatNeedOf(%q) = %q, want %q", kind, got, want)
		}
	}
}
