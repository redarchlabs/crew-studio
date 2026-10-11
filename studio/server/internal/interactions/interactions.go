// Package interactions turns a run's spec and events into a graph of who did what:
// the crew runs tasks, tasks are assigned to agents, agents call tools and models and delegate to each other.
package interactions

import (
	"strings"
	"time"
	"unicode"

	"crewstudio/internal/model"
)

const (
	KindRuns       = "runs"
	KindAssigned   = "assigned"
	KindContext    = "context"
	KindToolCall   = "tool_call"
	KindLLMCall    = "llm_call"
	KindDelegation = "delegation"

	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

type Stats struct {
	LLMCalls         int     `json:"llm_calls"`
	ToolCalls        int     `json:"tool_calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	DurationMS       int64   `json:"duration_ms"`
}

type Node struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
	Stats  Stats  `json:"stats"`
}

type Edge struct {
	ID      string  `json:"id"`
	Source  string  `json:"source"`
	Target  string  `json:"target"`
	Kind    string  `json:"kind"`
	Count   int     `json:"count"`
	CostUSD float64 `json:"cost_usd"`
}

type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// builder accumulates the graph while keeping first-seen order stable for layout.
type builder struct {
	nodes     map[string]*Node
	nodeOrder []string
	edges     map[string]*Edge
	edgeOrder []string
	started   map[string]time.Time
	// task node id -> role of the agent last seen working on it
	taskAgent map[string]string
	// toolKey(name) -> tool node id, so "Google Search" and "google_search" are one tool
	toolIDs map[string]string
}

func newBuilder() *builder {
	return &builder{nodes: map[string]*Node{}, edges: map[string]*Edge{}, started: map[string]time.Time{}, taskAgent: map[string]string{}, toolIDs: map[string]string{}}
}

func (b *builder) node(id, kind, label string) *Node {
	if n, ok := b.nodes[id]; ok {
		return n
	}
	n := &Node{ID: id, Kind: kind, Label: label}
	if kind == "task" || kind == "agent" {
		n.Status = StatusPending
	}
	b.nodes[id] = n
	b.nodeOrder = append(b.nodeOrder, id)
	return n
}

func (b *builder) edge(source, target, kind string) *Edge {
	id := kind + ":" + source + "->" + target
	if e, ok := b.edges[id]; ok {
		return e
	}
	e := &Edge{ID: id, Source: source, Target: target, Kind: kind}
	b.edges[id] = e
	b.edgeOrder = append(b.edgeOrder, id)
	return e
}

func (b *builder) task(name string) *Node {
	return b.node("task:"+name, "task", name)
}

func (b *builder) agent(role string) *Node {
	return b.node("agent:"+role, "agent", role)
}

// tool returns the node for a tool however it is spelled. CrewAI reports sanitized names ("google_search")
// while the runner's cost events use display names ("Google Search"); the display name wins as the label.
func (b *builder) tool(name string) *Node {
	key := toolKey(name)
	if id, ok := b.toolIDs[key]; ok {
		n := b.nodes[id]
		if !strings.Contains(n.Label, " ") && strings.Contains(name, " ") {
			n.Label = name
		}
		return n
	}
	b.toolIDs[key] = "tool:" + name
	return b.node("tool:"+name, "tool", name)
}

// toolKey folds a tool name to lower snake case: "Google Search" and "google_search" both give "google_search".
func toolKey(name string) string {
	var sb strings.Builder
	gap := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if gap && sb.Len() > 0 {
				sb.WriteByte('_')
			}
			gap = false
			sb.WriteRune(r)
		} else {
			gap = true
		}
	}
	return sb.String()
}

func (b *builder) graph() Graph {
	g := Graph{Nodes: make([]Node, 0, len(b.nodeOrder)), Edges: make([]Edge, 0, len(b.edgeOrder))}
	for _, id := range b.nodeOrder {
		g.Nodes = append(g.Nodes, *b.nodes[id])
	}
	for _, id := range b.edgeOrder {
		g.Edges = append(g.Edges, *b.edges[id])
	}
	return g
}

// Build returns the interaction graph for a run. With no events it shows the planned structure.
func Build(spec model.RunSpec, events []model.RunEvent) Graph {
	b := newBuilder()
	crew := b.node("crew", "crew", spec.Crew.Name)

	roles := map[string]string{}
	for _, agent := range spec.Agents {
		role := model.Interpolate(agent.Role, spec.Inputs)
		roles[agent.ID] = role
		b.agent(role)
	}
	taskNames := map[string]string{}
	for _, task := range spec.Tasks {
		taskNames[task.ID] = task.Name
		b.task(task.Name)
		b.edge("crew", "task:"+task.Name, KindRuns)
		if task.AgentID != nil {
			if role, ok := roles[*task.AgentID]; ok {
				b.edge("task:"+task.Name, "agent:"+role, KindAssigned)
			}
		}
	}
	for _, task := range spec.Tasks {
		for _, ctxID := range task.ContextTaskIDs {
			if ctxName, ok := taskNames[ctxID]; ok {
				b.edge("task:"+ctxName, "task:"+task.Name, KindContext)
			}
		}
	}

	for _, event := range events {
		b.apply(event, crew)
	}
	return b.graph()
}

func (b *builder) apply(event model.RunEvent, crew *Node) {
	p := event.Payload
	taskName, role := str(p, "task_name"), str(p, "agent_role")

	switch event.Type {
	case "task_started":
		task := b.task(taskName)
		task.Status = StatusRunning
		b.started[task.ID] = event.CreatedAt
	case "task_completed", "task_failed":
		task := b.task(taskName)
		task.Status = StatusDone
		if event.Type == "task_failed" {
			task.Status = StatusFailed
		}
		if start, ok := b.started[task.ID]; ok {
			task.Stats.DurationMS += event.CreatedAt.Sub(start).Milliseconds()
		}
		if role == "" {
			role = b.taskAgent[task.ID]
		}
		if role != "" {
			agent := b.agent(role)
			agent.Status = task.Status
			if start, ok := b.started[task.ID]; ok {
				agent.Stats.DurationMS += event.CreatedAt.Sub(start).Milliseconds()
			}
		}
	case "agent_started":
		agent := b.agent(role)
		agent.Status = StatusRunning
		if taskName != "" {
			task := b.task(taskName)
			b.taskAgent[task.ID] = role
			b.edge(task.ID, agent.ID, KindAssigned)
		}
	case "crew_completed", "crew_failed":
		b.settle(event.Type == "crew_failed")
	case "run_status":
		// Cancelled, timed-out and crashed runs have no crew_* event; their final status settles the graph
		if status := model.RunStatus(str(p, "status")); status.Terminal() {
			b.settle(status != model.RunSucceeded)
		}
	case "tool_started":
		agent := b.agent(role)
		tool := b.tool(str(p, "tool_name"))
		b.edge(agent.ID, tool.ID, KindToolCall).Count++
		for _, n := range []*Node{agent, tool, crew, b.taskIfNamed(taskName)} {
			if n != nil {
				n.Stats.ToolCalls++
			}
		}
	case "search", "video":
		cost := num(p, "cost_usd")
		agent := b.agent(role)
		if toolName := str(p, "tool_name"); toolName != "" {
			tool := b.tool(toolName)
			b.edge(agent.ID, tool.ID, KindToolCall).CostUSD += cost
			tool.Stats.CostUSD += cost
		}
		for _, n := range []*Node{agent, crew, b.taskIfNamed(taskName)} {
			if n != nil {
				n.Stats.CostUSD += cost
			}
		}
	case "llm_call":
		b.applyLLMCall(p, role, taskName, crew)
	case "delegation":
		from, to := b.agent(str(p, "from_agent")), b.agent(str(p, "to_agent"))
		b.edge(from.ID, to.ID, KindDelegation).Count++
	}
}

// settle closes out nodes still marked running once the crew has finished.
func (b *builder) settle(failed bool) {
	final := StatusDone
	if failed {
		final = StatusFailed
	}
	for _, n := range b.nodes {
		if n.Status == StatusRunning {
			n.Status = final
		}
	}
}

func (b *builder) applyLLMCall(p map[string]any, role, taskName string, crew *Node) {
	modelID := str(p, "model")
	cost := num(p, "cost_usd")
	usage, _ := p["usage"].(map[string]any)
	prompt, completion := int(num(usage, "prompt_tokens")), int(num(usage, "completion_tokens"))

	modelNode := b.node("model:"+modelID, "model", modelID)
	targets := []*Node{modelNode, crew, b.taskIfNamed(taskName)}
	if role != "" {
		agent := b.agent(role)
		edge := b.edge(agent.ID, modelNode.ID, KindLLMCall)
		edge.Count++
		edge.CostUSD += cost
		targets = append(targets, agent)
	}
	for _, n := range targets {
		if n == nil {
			continue
		}
		n.Stats.LLMCalls++
		n.Stats.PromptTokens += prompt
		n.Stats.CompletionTokens += completion
		n.Stats.CostUSD += cost
	}
}

func (b *builder) taskIfNamed(name string) *Node {
	if name == "" {
		return nil
	}
	return b.task(name)
}

func str(p map[string]any, key string) string {
	s, _ := p[key].(string)
	return s
}

func num(p map[string]any, key string) float64 {
	switch v := p[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}
