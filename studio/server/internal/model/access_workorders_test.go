package model

import (
	"slices"
	"strings"
	"testing"
)

func knownPerm(p string) bool { return p == "runs.read" || p == "crews.write" }

func TestNormalizeArea(t *testing.T) {
	tests := []struct {
		name    string
		in      Area
		wantErr string
	}{
		{"valid trimmed", Area{ID: uuidA, Name: "  Ops ", Description: " d "}, ""},
		{"empty name", Area{Name: "   "}, "name:"},
		{"name at max", Area{Name: strings.Repeat("a", 120)}, ""},
		{"name over max", Area{Name: strings.Repeat("a", 121)}, "name:"},
		{"unicode counted by rune", Area{Name: strings.Repeat("é", 120)}, ""},
		{"description over max", Area{Name: "x", Description: strings.Repeat("d", 5001)}, "description:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeArea(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if got.Name != strings.TrimSpace(tt.in.Name) || got.ID != tt.in.ID {
					t.Errorf("got %+v", got)
				}
				return
			}
			if !hasDetail(detailsOf(t, err), tt.wantErr) {
				t.Errorf("want %q", tt.wantErr)
			}
		})
	}
}

func TestNormalizeRole(t *testing.T) {
	got, err := NormalizeRole(Role{Name: " R ", Permissions: []string{"runs.read", " runs.read", "crews.write"}}, knownPerm)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Permissions, []string{"runs.read", "crews.write"}) || got.Name != "R" {
		t.Errorf("got %+v", got)
	}
	_, err = NormalizeRole(Role{Name: "", Permissions: []string{"nope"}}, knownPerm)
	d := detailsOf(t, err)
	if !hasDetail(d, "name:") || !hasDetail(d, "permissions:") {
		t.Errorf("details %v", d)
	}
	empty, err := NormalizeRole(Role{Name: "x"}, knownPerm)
	if err != nil || empty.Permissions == nil {
		t.Errorf("nil permissions must become empty slice: %+v %v", empty, err)
	}
}

func TestNormalizeUser(t *testing.T) {
	tests := []struct {
		name    string
		in      UserInput
		wantErr []string
		check   func(t *testing.T, u UserInput)
	}{
		{"human lowercases email, defaults name and status", UserInput{Kind: "human", Email: "  Ada@Example.COM "}, nil,
			func(t *testing.T, u UserInput) {
				if u.Email != "ada@example.com" || u.Name != "ada@example.com" || u.Status != UserActive {
					t.Errorf("got %+v", u)
				}
			}},
		{"human invalid email", UserInput{Kind: "human", Email: "nope"}, []string{"email:"}, nil},
		{"human email with display name rejected", UserInput{Kind: "human", Email: "Ada <ada@example.com>"}, []string{"email:"}, nil},
		{"human email too long", UserInput{Kind: "human", Email: strings.Repeat("a", 250) + "@x.io"}, []string{"email:"}, nil},
		{"service clears email", UserInput{Kind: "service", Email: "x@y.io", Name: "bot"}, nil,
			func(t *testing.T, u UserInput) {
				if u.Email != "" {
					t.Errorf("email = %q", u.Email)
				}
			}},
		{"service without name", UserInput{Kind: "service"}, []string{"name:"}, nil},
		{"bad kind", UserInput{Kind: "robot", Name: "x"}, []string{"kind:"}, nil},
		{"bad status", UserInput{Kind: "service", Name: "x", Status: "paused"}, []string{"status:"}, nil},
		{"disabled ok", UserInput{Kind: "service", Name: "x", Status: UserDisabled}, nil, nil},
		{"bad uuids and perms", UserInput{Kind: "service", Name: "x", AreaIDs: []string{"z"}, RoleIDs: []string{uuidA, uuidA}, Permissions: []string{"x"}},
			[]string{"area_ids:", "role_ids:", "permissions:"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeUser(tt.in, knownPerm)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatal(err)
				}
				if tt.check != nil {
					tt.check(t, got)
				}
				return
			}
			d := detailsOf(t, err)
			for _, w := range tt.wantErr {
				if !hasDetail(d, w) {
					t.Errorf("missing %q in %v", w, d)
				}
			}
		})
	}
}

func TestNormalizeAccessSettings(t *testing.T) {
	role := "  " + uuidA + " "
	got, err := NormalizeAccessSettings(AccessSettings{AutoProvisionRoleID: &role,
		CallbackAllowedHosts: []string{" Example.COM ", "", "example.com", "10.0.0.1"}})
	if err != nil {
		t.Fatal(err)
	}
	if *got.AutoProvisionRoleID != uuidA || !slices.Equal(got.CallbackAllowedHosts, []string{"example.com", "10.0.0.1"}) {
		t.Errorf("got %+v", got)
	}
	blank := "  "
	got, err = NormalizeAccessSettings(AccessSettings{AutoProvisionRoleID: &blank})
	if err != nil || got.AutoProvisionRoleID != nil || got.CallbackAllowedHosts == nil {
		t.Errorf("blank role should be nil, hosts non-nil: %+v %v", got, err)
	}
	bad := []string{"http://x", "a/b", "a:80", "u@h", "a b", "*.x.com", "h?q", "h#f", strings.Repeat("a", 254)}
	for _, h := range bad {
		if _, err := NormalizeAccessSettings(AccessSettings{CallbackAllowedHosts: []string{h}}); err == nil {
			t.Errorf("host %q accepted", h)
		}
	}
	notUUID := "abc"
	if _, err := NormalizeAccessSettings(AccessSettings{AutoProvisionRoleID: &notUUID}); err == nil {
		t.Error("bad role id accepted")
	}
	many := make([]string, 51)
	for i := range many {
		many[i] = "h" + strings.Repeat("a", i)
	}
	if _, err := NormalizeAccessSettings(AccessSettings{CallbackAllowedHosts: many}); err == nil {
		t.Error("51 hosts accepted")
	}
	if _, err := NormalizeAccessSettings(AccessSettings{CallbackAllowedHosts: many[:50]}); err != nil {
		t.Errorf("50 hosts rejected: %v", err)
	}
}

func TestNormalizeWorkOrder(t *testing.T) {
	base := WorkOrderInput{Title: " T ", Brief: " b ", CrewID: ptr(" " + uuidB + " "),
		Inputs: map[string]string{"k": " v "}, BudgetUSD: ptr(1.0)}
	got, err := NormalizeWorkOrder(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "T" || got.Brief != "b" || *got.CrewID != uuidB || got.Inputs["k"] != "v" {
		t.Errorf("got %+v", got)
	}
	if base.Inputs["k"] != " v " {
		t.Error("input map mutated")
	}
	explicit := base
	explicit.TargetType = "crew"
	if got, err = NormalizeWorkOrder(explicit); err != nil || got.TargetType != "" {
		t.Errorf("target_type crew: %+v %v", got, err)
	}

	tests := []struct {
		name   string
		mutate func(w WorkOrderInput) WorkOrderInput
		want   string
	}{
		{"no title", func(w WorkOrderInput) WorkOrderInput { w.Title = " "; return w }, "title:"},
		{"title too long", func(w WorkOrderInput) WorkOrderInput { w.Title = strings.Repeat("t", 201); return w }, "title:"},
		{"brief too long", func(w WorkOrderInput) WorkOrderInput { w.Brief = strings.Repeat("t", 100_001); return w }, "brief:"},
		{"task target refused", func(w WorkOrderInput) WorkOrderInput { w.TaskID = ptr(uuidA); return w }, "task_id: work orders, schedules and triggers run crews"},
		{"task target type refused", func(w WorkOrderInput) WorkOrderInput { w.TargetType = "task"; return w }, "target_type:"},
		{"needs crew", func(w WorkOrderInput) WorkOrderInput { w.CrewID = nil; return w }, "crew_id: is required"},
		{"blank crew id", func(w WorkOrderInput) WorkOrderInput { w.CrewID = ptr("  "); return w }, "crew_id:"},
		{"crew id not uuid", func(w WorkOrderInput) WorkOrderInput { w.CrewID = ptr("x"); return w }, "crew_id:"},
		{"bad input key", func(w WorkOrderInput) WorkOrderInput { w.Inputs = map[string]string{"Bad": "x"}; return w }, "inputs:"},
		{"zero budget", func(w WorkOrderInput) WorkOrderInput { w.BudgetUSD = ptr(0.0); return w }, "budget_usd:"},
		{"budget over max", func(w WorkOrderInput) WorkOrderInput { w.BudgetUSD = ptr(1000.01); return w }, "budget_usd:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeWorkOrder(tt.mutate(base))
			if d := detailsOf(t, err); !hasDetail(d, tt.want) {
				t.Errorf("want %q in %v", tt.want, d)
			}
		})
	}
	if _, err := NormalizeWorkOrder(func() WorkOrderInput { w := base; w.BudgetUSD = ptr(1000.0); return w }()); err != nil {
		t.Errorf("budget at max rejected: %v", err)
	}
	tooMany := base
	tooMany.Inputs = map[string]string{}
	for i := 0; i < 51; i++ {
		tooMany.Inputs["k"+strings.Repeat("a", i)] = "v"
	}
	_, err = NormalizeWorkOrder(tooMany)
	if !hasDetail(detailsOf(t, err), "inputs:") {
		t.Error("51 inputs accepted")
	}
}

func TestNormalizeSchedule(t *testing.T) {
	base := Schedule{Name: " Nightly ", Cron: "  0   2 * * * ", CrewID: ptr(uuidA)}
	got, err := NormalizeSchedule(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cron != "0 2 * * *" || got.Timezone != "UTC" || got.OverlapPolicy != OverlapSkip || got.Name != "Nightly" {
		t.Errorf("got %+v", got)
	}
	got, err = NormalizeSchedule(func() Schedule { s := base; s.Timezone = "Europe/London"; s.OverlapPolicy = OverlapAllow; return s }())
	if err != nil || got.Timezone != "Europe/London" || got.OverlapPolicy != OverlapAllow {
		t.Errorf("got %+v %v", got, err)
	}
	tests := []struct {
		name   string
		mutate func(s Schedule) Schedule
		want   string
	}{
		{"no name", func(s Schedule) Schedule { s.Name = ""; return s }, "name:"},
		{"no cron", func(s Schedule) Schedule { s.Cron = " "; return s }, "cron:"},
		{"bad tz", func(s Schedule) Schedule { s.Timezone = "Mars/Base"; return s }, "timezone:"},
		{"bad overlap", func(s Schedule) Schedule { s.OverlapPolicy = "queue"; return s }, "overlap_policy:"},
		{"no crew", func(s Schedule) Schedule { s.CrewID = nil; return s }, "crew_id:"},
		{"task target refused", func(s Schedule) Schedule { s.TaskID = ptr(uuidB); return s }, "task_id:"},
		{"bad budget", func(s Schedule) Schedule { s.BudgetUSD = ptr(-1.0); return s }, "budget_usd:"},
		{"bad input", func(s Schedule) Schedule { s.Inputs = map[string]string{"A B": "x"}; return s }, "inputs:"},
		{"long brief", func(s Schedule) Schedule { s.Brief = strings.Repeat("b", 100_001); return s }, "brief:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeSchedule(tt.mutate(base))
			if d := detailsOf(t, err); !hasDetail(d, tt.want) {
				t.Errorf("want %q in %v", tt.want, d)
			}
		})
	}
}

func TestNormalizeTrigger(t *testing.T) {
	base := Trigger{Name: " T ", CrewID: ptr(uuidA), CallbackURL: ptr(" https://example.com/hook "),
		InputMap: map[string]string{"company": " customer.name "}}
	got, err := NormalizeTrigger(base)
	if err != nil {
		t.Fatal(err)
	}
	if *got.CallbackURL != "https://example.com/hook" || got.InputMap["company"] != "customer.name" || got.Name != "T" {
		t.Errorf("got %+v", got)
	}
	blank := base
	blank.CallbackURL = ptr("  ")
	got, err = NormalizeTrigger(blank)
	if err != nil || got.CallbackURL != nil {
		t.Errorf("blank callback should be nil: %+v %v", got, err)
	}
	tests := []struct {
		name   string
		mutate func(s Trigger) Trigger
		want   string
	}{
		{"no name", func(s Trigger) Trigger { s.Name = ""; return s }, "name:"},
		{"bad key", func(s Trigger) Trigger { s.InputMap = map[string]string{"Bad": "a.b"}; return s }, "input_map:"},
		{"empty path", func(s Trigger) Trigger { s.InputMap = map[string]string{"a": " "}; return s }, "input_map:"},
		{"path with space", func(s Trigger) Trigger { s.InputMap = map[string]string{"a": "x y"}; return s }, "input_map:"},
		{"path too long", func(s Trigger) Trigger { s.InputMap = map[string]string{"a": strings.Repeat("p", 201)}; return s }, "input_map:"},
		{"bad callback", func(s Trigger) Trigger { s.CallbackURL = ptr("ftp://x"); return s }, "callback_url:"},
		{"bad budget", func(s Trigger) Trigger { s.BudgetUSD = ptr(0.0); return s }, "budget_usd:"},
		{"no crew", func(s Trigger) Trigger { s.CrewID = nil; return s }, "crew_id:"},
		{"task target refused", func(s Trigger) Trigger { s.TargetType = "task"; s.TaskID = ptr(uuidB); return s }, "task_id:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeTrigger(tt.mutate(base))
			if d := detailsOf(t, err); !hasDetail(d, tt.want) {
				t.Errorf("want %q in %v", tt.want, d)
			}
		})
	}
	big := base
	big.InputMap = map[string]string{}
	for i := 0; i < 51; i++ {
		big.InputMap["k"+strings.Repeat("a", i)] = "a.b"
	}
	_, err = NormalizeTrigger(big)
	if !hasDetail(detailsOf(t, err), "input_map:") {
		t.Error("51 entries accepted")
	}
}

func TestCheckCallbackURL(t *testing.T) {
	tests := []struct {
		url string
		bad bool
	}{
		{"https://example.com/a?b=1", false},
		{"http://internal.local:8080/x", false},
		{"https://[2001:db8::1]/x", false},
		{"", true},
		{"/relative", true},
		{"example.com/x", true},
		{"ftp://example.com", true},
		{"javascript://x", true},
		{"https://user:pw@example.com", true},
		{"https://example.com/#frag", true},
		{"https://0.0.0.0/x", true},
		{"https://[::]/x", true},
		{"https://224.0.0.1/x", true},
		{"https://example.com/" + strings.Repeat("a", 2000), true},
		{"https://exa mple.com", true},
	}
	for _, tt := range tests {
		got := CheckCallbackURL(tt.url)
		if (got != "") != tt.bad {
			t.Errorf("CheckCallbackURL(%.40q) = %q, bad=%v", tt.url, got, tt.bad)
		}
	}
}

func TestRunStatusTerminal(t *testing.T) {
	for _, s := range []RunStatus{RunSucceeded, RunFailed, RunCancelled} {
		if !s.Terminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	for _, s := range []RunStatus{RunQueued, RunRunning, ""} {
		if s.Terminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func TestValidationErrorMessage(t *testing.T) {
	e := &ValidationError{Details: []string{"a: x", "b: y"}}
	if e.Error() != "validation failed: a: x; b: y" {
		t.Error(e.Error())
	}
}
