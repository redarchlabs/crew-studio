// Package journal turns a run's events into a readable record of what each agent did on each task.
package journal

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"crewstudio/internal/model"
)

const (
	// MaxEntries bounds one journal; the full detail stays in the events and the transcript
	MaxEntries  = 5000
	maxBodyLen  = 4000
	maxTitleLen = 160

	TaskPending = "pending"
	TaskRunning = "running"
	TaskDone    = "done"
	TaskFailed  = "failed"
)

type Entry struct {
	Seq       int       `json:"seq"`
	At        time.Time `json:"at"`
	AgentRole string    `json:"agent_role"`
	TaskName  string    `json:"task_name"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Model     string    `json:"model"`
	CostUSD   float64   `json:"cost_usd"`
	Tokens    int       `json:"tokens"`
}

type Agent struct {
	Role             string     `json:"role"`
	Tasks            []string   `json:"tasks"`
	Entries          int        `json:"entries"`
	LLMCalls         int        `json:"llm_calls"`
	ToolCalls        int        `json:"tool_calls"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	CostUSD          float64    `json:"cost_usd"`
	FirstAt          *time.Time `json:"first_at"`
	LastAt           *time.Time `json:"last_at"`
}

type Task struct {
	Name       string     `json:"name"`
	AgentRole  string     `json:"agent_role"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Output     *string    `json:"output"`
}

type Journal struct {
	RunID     string          `json:"run_id"`
	Status    model.RunStatus `json:"status"`
	Agents    []Agent         `json:"agents"`
	Tasks     []Task          `json:"tasks"`
	Entries   []Entry         `json:"entries"`
	Truncated bool            `json:"truncated"`
}

// Build makes the journal. responses maps an llm_call event's seq to the model's reply text.
func Build(run model.Run, spec model.RunSpec, events []model.RunEvent, responses map[int]string) Journal {
	b := newBuilder(spec)
	j := Journal{RunID: run.ID, Status: run.Status, Entries: []Entry{}}
	for _, ev := range events {
		entry, ok := b.entry(ev, responses)
		if !ok {
			continue
		}
		b.count(ev, entry)
		if len(j.Entries) >= MaxEntries {
			j.Truncated = true
			continue
		}
		j.Entries = append(j.Entries, entry)
	}
	j.Agents = b.agentList()
	j.Tasks = b.taskList()
	return j
}

type builder struct {
	agents     map[string]*Agent
	agentOrder []string
	tasks      map[string]*Task
	taskOrder  []string
}

func newBuilder(spec model.RunSpec) *builder {
	b := &builder{agents: map[string]*Agent{}, tasks: map[string]*Task{}}
	roles := map[string]string{}
	for _, a := range spec.Agents {
		roles[a.ID] = model.Interpolate(a.Role, spec.Inputs)
		b.agent(roles[a.ID])
	}
	for _, t := range spec.Tasks {
		role := ""
		if t.AgentID != nil {
			role = roles[*t.AgentID]
		}
		b.task(t.Name).AgentRole = role
		if role != "" {
			b.addTask(role, t.Name)
		}
	}
	return b
}

func (b *builder) agent(role string) *Agent {
	if a, ok := b.agents[role]; ok {
		return a
	}
	a := &Agent{Role: role, Tasks: []string{}}
	b.agents[role] = a
	b.agentOrder = append(b.agentOrder, role)
	return a
}

func (b *builder) task(name string) *Task {
	if t, ok := b.tasks[name]; ok {
		return t
	}
	t := &Task{Name: name, Status: TaskPending}
	b.tasks[name] = t
	b.taskOrder = append(b.taskOrder, name)
	return t
}

func (b *builder) addTask(role, task string) {
	a := b.agent(role)
	if task != "" && !slices.Contains(a.Tasks, task) {
		a.Tasks = append(a.Tasks, task)
	}
}

func (b *builder) agentList() []Agent {
	out := make([]Agent, 0, len(b.agentOrder))
	for _, role := range b.agentOrder {
		out = append(out, *b.agents[role])
	}
	return out
}

func (b *builder) taskList() []Task {
	out := make([]Task, 0, len(b.taskOrder))
	for _, name := range b.taskOrder {
		out = append(out, *b.tasks[name])
	}
	return out
}

func str(p map[string]any, key string) string {
	switch v := p[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func num(p map[string]any, key string) float64 {
	f, _ := p[key].(float64)
	return f
}

func usage(p map[string]any) (prompt, completion int) {
	u, _ := p["usage"].(map[string]any)
	return int(num(u, "prompt_tokens")), int(num(u, "completion_tokens"))
}

func clip(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n… [cut; full text in the LLM calls and the run transcript]"
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxTitleLen {
		r := []rune(s)
		return string(r[:maxTitleLen]) + "…"
	}
	return s
}

// consultationEntry describes a specialist's question to the lead (docs/agent-chat-handoffs-design.md, section 6): who
// it belongs to, its title and its body. The runner records one event when the specialist asks and one with the outcome.
func consultationEntry(p map[string]any) (role, title, body string) {
	from, to := str(p, "from_agent"), str(p, "to_agent")
	question, answer := clip(str(p, "question"), maxBodyLen), clip(str(p, "answer"), maxBodyLen)
	switch str(p, "outcome") {
	case "asked":
		return from, from + " asked the lead: " + oneLine(clip(str(p, "question"), 160)), question
	case "answered":
		return to, to + " answered " + from, answer
	case "needs_person":
		return to, to + " passed " + from + "'s question to the person", answer
	case "declined":
		return to, to + " could not be asked again", question
	case "stopped":
		return to, to + " stopped: nobody answered in time", answer
	}
	return to, to + " could not answer " + from, answer
}

// compactionBody says why an agent's context was compacted.
func compactionBody(p map[string]any) string {
	why := "Someone asked to compact the context."
	if str(p, "reason") == "auto" {
		why = fmt.Sprintf("The context passed the %s-token limit in Settings.", thousands(int(num(p, "limit_tokens"))))
	}
	return fmt.Sprintf("%s %d messages were replaced by a summary; the task and the latest step were kept.",
		why, int(num(p, "messages_summarised")))
}

// thousands writes n with comma separators, such as 48,210.
func thousands(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + thousands(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func compactJSON(v any) string {
	if v == nil {
		return ""
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

// entry describes one event, or returns false for events that do not belong in the journal (raw logs).
func (b *builder) entry(ev model.RunEvent, responses map[int]string) (Entry, bool) {
	p := ev.Payload
	e := Entry{Seq: ev.Seq, At: ev.CreatedAt, AgentRole: str(p, "agent_role"), TaskName: str(p, "task_name")}
	switch ev.Type {
	case "run_status":
		e.Kind, e.Title, e.Body = "run", "Run "+str(p, "status"), clip(str(p, "error"), maxBodyLen)
	case "run_resumed":
		e.Kind, e.Title, e.Body = "run", resumedTitle(p), resumedBody(p)
	case "crew_started":
		e.Kind, e.Title = "run", "Crew started: "+str(p, "crew_name")
	case "crew_completed":
		e.Kind, e.Title, e.Body = "run", "Crew finished", clip(str(p, "output"), maxBodyLen)
		e.CostUSD = num(p, "cost_usd")
	case "crew_failed":
		e.Kind, e.Title, e.Body = "run", "Crew failed", clip(str(p, "error"), maxBodyLen)
	case "task_started":
		e.Kind, e.Title = "task_started", "Started "+e.TaskName
	case "agent_started":
		e.Kind, e.Title = "agent_started", e.AgentRole+" picked up "+e.TaskName
	case "task_completed":
		e.Kind, e.Title, e.Body = "task_completed", "Finished "+e.TaskName, clip(str(p, "output"), maxBodyLen)
	case "task_failed":
		e.Kind, e.Title, e.Body = "task_failed", e.TaskName+" failed", clip(str(p, "error"), maxBodyLen)
	case "llm_call":
		prompt, completion := usage(p)
		e.Kind, e.Model, e.CostUSD, e.Tokens = "llm_call", str(p, "model"), num(p, "cost_usd"), prompt+completion
		e.Title = "Asked " + e.Model
		e.Body = clip(responses[ev.Seq], maxBodyLen)
	case "tool_started":
		e.Kind, e.Title, e.Body = "tool_call", "Used "+str(p, "tool_name"), clip(compactJSON(p["tool_args"]), maxBodyLen)
	case "tool_finished":
		e.Kind, e.Title, e.Body = "tool_result", "Result from "+str(p, "tool_name"), clip(str(p, "output"), maxBodyLen)
	case "tool_error":
		e.Kind, e.Title, e.Body = "tool_error", str(p, "tool_name")+" failed", clip(str(p, "error"), maxBodyLen)
	case "search":
		e.Kind, e.Model, e.CostUSD = "search", str(p, "model"), num(p, "cost_usd")
		e.Title = "Searched: " + oneLine(str(p, "query"))
		e.Tokens = int(num(p, "input_tokens") + num(p, "output_tokens"))
	case "video":
		e.Kind, e.Model, e.CostUSD = "video", str(p, "model"), num(p, "cost_usd")
		e.Title = "Made video " + str(p, "file")
		if made, _ := p["made"].(bool); !made {
			e.Title = "Video blocked: " + str(p, "file")
		}
		e.Body = clip(str(p, "prompt"), maxBodyLen)
		e.Tokens = int(num(p, "input_tokens") + num(p, "output_tokens"))
	case "delegation":
		e.AgentRole = str(p, "from_agent")
		e.Kind, e.Title, e.Body = "delegation", "Delegated to "+str(p, "to_agent"), clip(str(p, "question"), maxBodyLen)
	case "consultation":
		e.Kind = "consultation"
		e.AgentRole, e.Title, e.Body = consultationEntry(p)
	case "budget":
		e.Kind = "budget"
		e.Title = fmt.Sprintf("Budget %s at $%.4f of $%.2f", strings.ReplaceAll(str(p, "state"), "_", " "), num(p, "spent_usd"), num(p, "budget_usd"))
	case "context_compacted":
		e.Kind = "compaction"
		if failed, _ := p["failed"].(bool); failed {
			e.Title = "Could not compact " + e.AgentRole + "'s context"
			e.Body = clip(str(p, "error"), maxBodyLen) + "\nThe context was left as it was."
			break
		}
		e.Title = fmt.Sprintf("Compacted %s's context: about %s → %s tokens", e.AgentRole,
			thousands(int(num(p, "tokens_before"))), thousands(int(num(p, "tokens_after"))))
		e.Body = compactionBody(p)
	case "artifacts":
		e.Kind, e.Title, e.Body = "artifacts", fmt.Sprintf("Saved %d files", int(num(p, "files"))), str(p, "error")
	case "state":
		// Recorded by the run-state API for each notes or action log call, such as "Notes: read weekly-report v12"
		e.Kind, e.Title, e.Body = "state", str(p, "message"), clip(str(p, "detail"), maxBodyLen)
	default:
		return Entry{}, false
	}
	e.Title = oneLine(e.Title)
	return e, true
}

// count updates the per-agent and per-task summaries.
func (b *builder) count(ev model.RunEvent, e Entry) {
	p := ev.Payload
	if e.TaskName != "" {
		t := b.task(e.TaskName)
		switch ev.Type {
		case "task_started":
			at := e.At
			t.StartedAt, t.Status = &at, TaskRunning
			if e.AgentRole != "" {
				t.AgentRole = e.AgentRole
			}
		case "task_completed":
			at := e.At
			out := str(p, "output")
			t.FinishedAt, t.Status, t.Output = &at, TaskDone, &out
		case "task_failed":
			at := e.At
			t.FinishedAt, t.Status = &at, TaskFailed
		}
	}
	if e.AgentRole == "" {
		return
	}
	a := b.agent(e.AgentRole)
	b.addTask(e.AgentRole, e.TaskName)
	a.Entries++
	at := e.At
	if a.FirstAt == nil {
		a.FirstAt = &at
	}
	a.LastAt = &at
	switch ev.Type {
	case "llm_call":
		prompt, completion := usage(p)
		a.LLMCalls++
		a.PromptTokens += prompt
		a.CompletionTokens += completion
		a.CostUSD += e.CostUSD
	case "tool_started":
		a.ToolCalls++
	case "search", "video":
		a.CostUSD += e.CostUSD
	}
}

// resumedTitle and resumedBody describe a run_resumed event: which tasks the run reused from the run it resumed.
func resumedTitle(p map[string]any) string {
	start := int(num(p, "start_position"))
	if start == 1 {
		return "Resumed: reused task 1 from the earlier run"
	}
	return fmt.Sprintf("Resumed: reused tasks 1–%d from the earlier run", start)
}

func resumedBody(p map[string]any) string {
	var lines []string
	if names, ok := p["reused_tasks"].([]any); ok {
		for _, n := range names {
			if s, ok := n.(string); ok {
				lines = append(lines, "Reused: "+s)
			}
		}
	}
	if files := int(num(p, "restored_files")); files > 0 {
		lines = append(lines, fmt.Sprintf("Restored %d files from the earlier run's workspace", files))
	}
	if msg := str(p, "error"); msg != "" {
		lines = append(lines, msg)
	}
	return clip(strings.Join(lines, "\n"), maxBodyLen)
}
