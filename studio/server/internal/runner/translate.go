package runner

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"crewstudio/internal/model"
)

const maxLogLineLen = 10_000

// Event types the runner may send. Anything else on stdout is kept as a log line.
var runnerEventTypes = map[string]bool{
	"crew_started": true, "task_started": true, "agent_started": true, "tool_started": true,
	"tool_finished": true, "tool_error": true, "llm_call": true, "search": true, "video": true, "delegation": true,
	"task_completed": true, "task_failed": true, "crew_completed": true, "crew_failed": true, "budget": true,
	"context_compacted": true, "consultation": true,
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07`)

// Provider key shapes (OpenAI, Anthropic, Gemini API, Vertex AI). A library error that echoes a key
// must not store it in the run log.
var secretPattern = regexp.MustCompile(`\b(sk-(?:ant-|proj-)?[A-Za-z0-9_\-]{16,}|AIza[0-9A-Za-z_\-]{30,}|AQ\.[A-Za-z0-9_\-]{20,})`)

const maxPayloadStringLen = 1 << 20

func redactSecrets(s string) string {
	return secretPattern.ReplaceAllString(s, "[redacted]")
}

// capStrings truncates oversized strings anywhere in a payload and strips NUL bytes.
func capStrings(v any) any {
	switch t := v.(type) {
	case string:
		t = strings.ReplaceAll(t, "\x00", "")
		if len(t) > maxPayloadStringLen {
			return t[:maxPayloadStringLen] + "\n...[truncated by Crew Studio]"
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = capStrings(item)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = capStrings(item)
		}
		return out
	}
	return v
}

type runnerLine struct {
	Type    string         `json:"type"`
	At      string         `json:"at"`
	Payload map[string]any `json:"payload"`
}

// translated is one stdout/stderr line turned into what gets stored.
type translated struct {
	Event   model.RunEvent
	LLMCall *model.LLMCall // set for llm_call; its messages/response are moved out of the event
}

// cleanLogLine strips terminal colour codes and caps the length. It returns "" for blank lines.
func cleanLogLine(line string) string {
	line = strings.TrimRight(ansiPattern.ReplaceAllString(redactSecrets(line), ""), " \r\n\t")
	line = strings.ReplaceAll(line, "\x00", "")
	if strings.TrimSpace(line) == "" {
		return ""
	}
	if len(line) > maxLogLineLen {
		line = line[:maxLogLineLen] + " ...[truncated]"
	}
	return line
}

func logEvent(line string, now time.Time) model.RunEvent {
	return model.RunEvent{Type: "log", Payload: map[string]any{"line": line}, CreatedAt: now}
}

// translateStdout parses one runner JSON line. Lines that are not runner events become logs.
func translateStdout(raw string, now time.Time) (translated, bool) {
	var line runnerLine
	if err := json.Unmarshal([]byte(raw), &line); err != nil || !runnerEventTypes[line.Type] {
		if cleaned := cleanLogLine(raw); cleaned != "" {
			return translated{Event: logEvent(cleaned, now)}, true
		}
		return translated{}, false
	}
	if line.Payload == nil {
		line.Payload = map[string]any{}
	}
	line.Payload, _ = capStrings(line.Payload).(map[string]any)
	at := now
	// Trust the runner's timestamp only when it is sane; it orders events CrewAI emitted from a thread pool
	if parsed, err := time.Parse(time.RFC3339Nano, line.At); err == nil && parsed.Before(now.Add(time.Minute)) && parsed.After(now.Add(-24*time.Hour)) {
		at = parsed
	}
	out := translated{Event: model.RunEvent{Type: line.Type, Payload: line.Payload, CreatedAt: at}}

	if line.Type == "llm_call" {
		call := model.LLMCall{
			AgentRole: str(line.Payload, "agent_role"),
			TaskName:  str(line.Payload, "task_name"),
			Model:     str(line.Payload, "model"),
			CostUSD:   num(line.Payload, "cost_usd"),
			Messages:  line.Payload["messages"],
			Response:  str(line.Payload, "response"),
			CreatedAt: at,
		}
		if usage, ok := line.Payload["usage"].(map[string]any); ok {
			call.Usage = model.TokenUsage{
				PromptTokens:       int(num(usage, "prompt_tokens")),
				CompletionTokens:   int(num(usage, "completion_tokens")),
				CachedPromptTokens: int(num(usage, "cached_prompt_tokens")),
				CacheWriteTokens:   int(num(usage, "cache_write_tokens")),
				SuccessfulRequests: 1,
			}
		}
		if call.Messages == nil {
			call.Messages = []any{}
		}
		slim := make(map[string]any, len(line.Payload))
		for k, v := range line.Payload {
			if k != "messages" && k != "response" {
				slim[k] = v
			}
		}
		out.Event.Payload = slim
		out.LLMCall = &call
	}
	return out, true
}

func str(p map[string]any, key string) string {
	s, _ := p[key].(string)
	return s
}

func num(p map[string]any, key string) float64 {
	f, _ := p[key].(float64)
	return f
}

// outcome is what the runner's final event says about the run.
type outcome struct {
	completed   bool
	failed      bool
	output      string
	errMsg      string
	costUSD     *float64
	usage       *model.TokenUsage
	taskOutputs []model.TaskOutput
}

// readOutcome extracts the result from a crew_completed or crew_failed payload.
func readOutcome(eventType string, payload map[string]any) outcome {
	o := outcome{completed: eventType == "crew_completed", failed: eventType == "crew_failed"}
	o.output = str(payload, "output")
	o.errMsg = str(payload, "error")
	if c, ok := payload["cost_usd"].(float64); ok {
		o.costUSD = &c
	}
	if raw, ok := payload["usage"]; ok {
		var usage model.TokenUsage
		if data, err := json.Marshal(raw); err == nil && json.Unmarshal(data, &usage) == nil {
			o.usage = &usage
		}
	}
	if raw, ok := payload["task_outputs"]; ok {
		var outputs []model.TaskOutput
		if data, err := json.Marshal(raw); err == nil && json.Unmarshal(data, &outputs) == nil {
			o.taskOutputs = outputs
		}
	}
	return o
}
