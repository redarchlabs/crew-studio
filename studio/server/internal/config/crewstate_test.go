package config

import "testing"

func TestSecretsKeyIsRead(t *testing.T) {
	dir := studio(t, "STUDIO_DATABASE_URL=postgres://x\nSTUDIO_PYTHON=py\nSTUDIO_SECRETS_KEY=abc\n")
	cfg, err := Load(dir, noEnv)
	if err != nil || cfg.SecretsKey != "abc" {
		t.Fatalf("cfg = %+v, %v", cfg.SecretsKey, err)
	}
}

func TestInternalURL(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:8088": "http://127.0.0.1:8088",
		"0.0.0.0:9000":   "http://127.0.0.1:9000",
		":8088":          "http://127.0.0.1:8088",
		"[::]:8088":      "http://127.0.0.1:8088",
		"[::1]:8088":     "http://[::1]:8088",
		"localhost:8091": "http://localhost:8091",
		"bad":            "http://bad",
	} {
		if got := (Config{Addr: addr}).InternalURL(); got != want {
			t.Errorf("%s = %s, want %s", addr, got, want)
		}
	}
}

func TestTrackerSettings(t *testing.T) {
	env := map[string]string{"STUDIO_JIRA_SITES": " https://jira.example.com/ , http://127.0.0.1:9000",
		"STUDIO_LINEAR_API_URL": "http://127.0.0.1:9001/graphql", "STUDIO_KM2_TRACKER_SITES": "https://KM2.example.com/"}
	dir := studio(t, "STUDIO_DATABASE_URL=postgres://x\nSTUDIO_PYTHON=py\n")
	cfg, err := Load(dir, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.JiraSites) != 2 || cfg.JiraSites[0] != "https://jira.example.com" || cfg.LinearAPIURL != "http://127.0.0.1:9001/graphql" {
		t.Errorf("cfg = %v %q", cfg.JiraSites, cfg.LinearAPIURL)
	}
	if len(cfg.KM2TrackerSites) != 1 || cfg.KM2TrackerSites[0] != "https://KM2.example.com" {
		t.Errorf("km2 sites = %v", cfg.KM2TrackerSites)
	}
	for _, bad := range []map[string]string{{"STUDIO_JIRA_SITES": "ftp://x"}, {"STUDIO_JIRA_SITES": "https://x/path"}, {"STUDIO_LINEAR_API_URL": "nope"},
		{"STUDIO_JIRA_SITES": "http://jira.example.com"}, {"STUDIO_LINEAR_API_URL": "http://10.0.0.5/graphql"},
		{"STUDIO_KM2_TRACKER_SITES": "http://km2.example.com"}, {"STUDIO_KM2_TRACKER_SITES": "https://km2.example.com/api"}} {
		if _, err := Load(dir, func(k string) string { return bad[k] }); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
