package runner

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

func TestTranslateStdoutLLMCallMovesBulkOut(t *testing.T) {
	now := time.Now()
	line := `{"type":"llm_call","at":"` + now.Add(-time.Second).Format(time.RFC3339Nano) + `","payload":{"agent_role":"A","task_name":"T","model":"m",` +
		`"usage":{"prompt_tokens":10,"completion_tokens":2,"cached_prompt_tokens":1,"cache_write_tokens":4},"cost_usd":0.5,"messages":[{"role":"user","content":"hi"}],"response":"yo"}}`
	got, ok := translateStdout(line, now)
	if !ok || got.LLMCall == nil {
		t.Fatalf("got %+v", got)
	}
	if _, has := got.Event.Payload["messages"]; has {
		t.Error("messages should not stay in the event payload")
	}
	if got.LLMCall.Response != "yo" || got.LLMCall.Usage.PromptTokens != 10 || got.LLMCall.Usage.CachedPromptTokens != 1 || got.LLMCall.Usage.CacheWriteTokens != 4 || got.LLMCall.CostUSD != 0.5 {
		t.Errorf("call = %+v", got.LLMCall)
	}
	if !got.Event.CreatedAt.Before(now) {
		t.Error("runner timestamp should be used when sane")
	}
}

func TestTranslateStdoutFallsBackToLog(t *testing.T) {
	now := time.Now()
	got, ok := translateStdout("\x1b[31mplain text\x1b[0m", now)
	if !ok || got.Event.Type != "log" || got.Event.Payload["line"] != "plain text" {
		t.Errorf("got %+v", got)
	}
	got, ok = translateStdout(`{"type":"rm_rf","payload":{}}`, now)
	if !ok || got.Event.Type != "log" {
		t.Errorf("unknown types must become logs, got %+v", got)
	}
	if _, ok := translateStdout("   ", now); ok {
		t.Error("blank lines should be dropped")
	}
	got, _ = translateStdout(`{"type":"task_started","at":"2001-01-01T00:00:00Z"}`, now)
	if !got.Event.CreatedAt.Equal(now) || got.Event.Payload == nil {
		t.Errorf("implausible timestamps must be replaced, got %+v", got.Event)
	}
}

func TestCleanLogLineCapsLength(t *testing.T) {
	long := strings.Repeat("x", maxLogLineLen+50)
	if got := cleanLogLine(long); !strings.HasSuffix(got, "[truncated]") || len(got) > maxLogLineLen+20 {
		t.Errorf("len = %d", len(got))
	}
}

func TestReadOutcome(t *testing.T) {
	o := readOutcome("crew_completed", map[string]any{
		"output": "memo", "cost_usd": 0.25,
		"usage":        map[string]any{"prompt_tokens": 5.0, "successful_requests": 2.0},
		"task_outputs": []any{map[string]any{"position": 0.0, "task_name": "T", "agent_role": "A", "raw": "r"}},
	})
	if !o.completed || o.output != "memo" || *o.costUSD != 0.25 || o.usage.PromptTokens != 5 || len(o.taskOutputs) != 1 {
		t.Errorf("outcome = %+v", o)
	}
	if f := readOutcome("crew_failed", map[string]any{"error": "boom"}); !f.failed || f.errMsg != "boom" || f.costUSD != nil {
		t.Errorf("failed outcome = %+v", f)
	}
}

func TestBrokerDeliversAndDropsSlowSubscribers(t *testing.T) {
	b := NewBroker()
	ch, stop := b.Subscribe("r1")
	b.Publish("r1", model.RunEvent{Seq: 1})
	if ev := <-ch; ev.Seq != 1 {
		t.Errorf("got %+v", ev)
	}
	b.Publish("other", model.RunEvent{Seq: 9})

	for i := 0; i <= subscriberBuffer; i++ {
		b.Publish("r1", model.RunEvent{Seq: i})
	}
	if b.subscriberCount("r1") != 0 {
		t.Error("a full subscriber should be dropped")
	}
	stop() // removing an already-dropped subscriber must not panic

	ch2, _ := b.Subscribe("r2")
	b.CloseRun("r2")
	if _, open := <-ch2; open {
		t.Error("CloseRun should close the channel")
	}
}

func TestCatalogAvailability(t *testing.T) {
	dir := t.TempDir()
	catalog := filepath.Join(dir, "tools.json")
	envFile := filepath.Join(dir, ".env")
	_ = os.WriteFile(catalog, []byte(`[{"key":"a","name":"A","description":"d","requires_env":["KEY_A"],"effect":"reads","needs":[]},`+
		`{"key":"b","name":"B","description":"d","effect":"changes","needs":["crew"]}]`), 0o600)
	_ = os.WriteFile(envFile, []byte("KEY_A=\n"), 0o600)

	c := NewCatalog(catalog, envFile, func(string) string { return "" })
	tools, err := c.ByKey()
	if err != nil {
		t.Fatal(err)
	}
	if tools["a"].Available || !tools["b"].Available || tools["b"].RequiresEnv == nil || tools["a"].Effect != model.EffectReads ||
		tools["a"].Needs == nil || !slices.Equal(tools["b"].Needs, []string{model.NeedCrew}) {
		t.Errorf("tools = %+v", tools)
	}

	_ = os.WriteFile(envFile, []byte("KEY_A=secret\n"), 0o600)
	tools, _ = c.ByKey()
	if !tools["a"].Available {
		t.Error("tool should become available once its key is set, without restart")
	}

	if _, err := NewCatalog(filepath.Join(dir, "missing.json"), envFile, os.Getenv).Tools(); err == nil {
		t.Error("missing catalog should error")
	}

	_ = os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=\nOPENAI_API_KEY=sk-x\n"), 0o600)
	fromEnv := NewCatalog(catalog, envFile, func(k string) string { return map[string]string{"GEMINI_API_KEY": "g"}[k] })
	set, err := fromEnv.EnvSet("ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY")
	if err != nil || set["ANTHROPIC_API_KEY"] || !set["OPENAI_API_KEY"] || !set["GEMINI_API_KEY"] {
		t.Errorf("env set = %v %v", set, err)
	}
	if set, err := NewCatalog(catalog, filepath.Join(dir, "none.env"), func(string) string { return "" }).EnvSet("X"); err != nil || set["X"] {
		t.Errorf("missing env file = %v %v", set, err)
	}
}

// The catalog fails closed: an entry without an effect, or with an unknown effect or need, stops the server (it is
// read at startup) rather than letting a tool into agent chat unclassified.
func TestCatalogRefusesUnclassifiedTools(t *testing.T) {
	dir := t.TempDir()
	for name, entry := range map[string]string{
		"no effect":      `{"key":"a","name":"A","description":"d","needs":[]}`,
		"unknown effect": `{"key":"a","name":"A","description":"d","effect":"writes","needs":[]}`,
		"unknown need":   `{"key":"a","name":"A","description":"d","effect":"reads","needs":["mcp"]}`,
		"no needs":       `{"key":"a","name":"A","description":"d","effect":"reads"}`,
	} {
		catalog := filepath.Join(dir, "tools.json")
		_ = os.WriteFile(catalog, []byte("["+entry+"]"), 0o600)
		_, err := NewCatalog(catalog, filepath.Join(dir, ".env"), func(string) string { return "" }).Tools()
		if err == nil || !strings.Contains(err.Error(), `tool "a"`) {
			t.Errorf("%s: %v", name, err)
		}
		if err := CheckCatalog(catalog); err == nil || !strings.Contains(err.Error(), `tool "a"`) {
			t.Errorf("startup check, %s: %v", name, err)
		}
	}
}

// The startup check reads only the catalog's classification: a provider env file it cannot read does not stop the
// server (the tools' availability is worked out later, per request).
func TestCheckCatalogIgnoresTheProviderEnv(t *testing.T) {
	dir := t.TempDir()
	catalog := filepath.Join(dir, "tools.json")
	_ = os.WriteFile(catalog, []byte(`[{"key":"a","name":"A","description":"d","effect":"reads","needs":[]}]`), 0o600)
	if err := CheckCatalog(catalog); err != nil {
		t.Errorf("valid catalog: %v", err)
	}
	unreadable := filepath.Join(dir, "env-dir")
	_ = os.Mkdir(unreadable, 0o700)
	if _, err := NewCatalog(catalog, unreadable, func(string) string { return "" }).Tools(); err == nil {
		t.Fatal("expected Tools to fail on an unreadable provider env")
	}
	if err := CheckCatalog(catalog); err != nil {
		t.Errorf("startup check coupled to the provider env: %v", err)
	}
	if err := CheckCatalog(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("a missing catalog passed the startup check")
	}
	_ = os.WriteFile(catalog, []byte(`{`), 0o600)
	if err := CheckCatalog(catalog); err == nil {
		t.Error("a broken catalog passed the startup check")
	}
}

// The shipped catalog classifies every tool.
func TestShippedCatalogIsClassified(t *testing.T) {
	tools, err := NewCatalog("../../../runner/tools_catalog.json", filepath.Join(t.TempDir(), ".env"), func(string) string { return "" }).Tools()
	if err != nil || len(tools) == 0 {
		t.Fatalf("catalog = %d tools, %v", len(tools), err)
	}
	if err := CheckCatalog("../../../runner/tools_catalog.json"); err != nil {
		t.Errorf("startup check: %v", err)
	}
}

func TestLogLinesRedactProviderKeys(t *testing.T) {
	for _, secret := range []string{
		"sk-proj-abcdefghijklmnopqrstuvwx", "sk-ant-api03-abcdefghijklmnopqrstu",
		"AIzaSyA1234567890abcdefghijklmnopqrstu", "AQ.Ab8RN6abcdefghijklmnopqrstuv",
	} {
		got := cleanLogLine("Error: invalid key " + secret + " rejected")
		if strings.Contains(got, secret) || !strings.Contains(got, "[redacted]") {
			t.Errorf("secret not redacted: %q", got)
		}
	}
	if got := cleanLogLine("a\x00b"); got != "ab" {
		t.Errorf("NUL not stripped: %q", got)
	}
}

func TestPayloadStringsAreCapped(t *testing.T) {
	huge := strings.Repeat("y", maxPayloadStringLen+10)
	got, _ := translateStdout(`{"type":"task_completed","payload":{"output":"`+huge+`","nested":["a\u0000b"]}}`, time.Now())
	if out := got.Event.Payload["output"].(string); len(out) > maxPayloadStringLen+64 {
		t.Errorf("output not capped: %d", len(out))
	}
	if nested := got.Event.Payload["nested"].([]any); nested[0] != "ab" {
		t.Errorf("nested = %v", nested)
	}
}

func TestChildEnvIsAllowListed(t *testing.T) {
	t.Setenv("STUDIO_DATABASE_URL", "postgres://secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws")
	t.Setenv("OPENAI_API_KEY", "sk-from-shell")
	m := &Manager{proc: ProcessConfig{ProviderEnvFile: "/x/.env"}}
	env := strings.Join(m.childEnv(), "\n")
	if strings.Contains(env, "postgres://secret") || strings.Contains(env, "AWS_SECRET") {
		t.Errorf("leaked: %s", env)
	}
	if !strings.Contains(env, "OPENAI_API_KEY=sk-from-shell") || !strings.Contains(env, "STUDIO_PROVIDER_ENV_FILE=/x/.env") {
		t.Errorf("missing expected vars: %s", env)
	}
}

func TestSanitizeResultStripsNUL(t *testing.T) {
	out := "memo\x00"
	res := sanitizeResult(store.RunResult{Output: &out, TaskOutputs: []model.TaskOutput{{Raw: "r\x00"}}})
	if *res.Output != "memo" || res.TaskOutputs[0].Raw != "r" || out != "memo\x00" {
		t.Errorf("res = %+v (input %q)", res, out)
	}
}
