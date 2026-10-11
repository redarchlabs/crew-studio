package chat

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	maxProposedBriefLen  = 10_000
	maxProposedTitleLen  = 200
	maxProposedInputs    = 50
	defaultTitleFromWord = 60
)

var proposedInputKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// startWorkOrder is what the front desk proposes: a full run of this crew with a brief. In agent chat it names the
// crew (one of those the agent may start; see proposedCrew).
type startWorkOrder struct {
	Title    string            `json:"title"`
	Brief    string            `json:"brief"`
	Inputs   map[string]string `json:"inputs,omitempty"`
	CrewID   string            `json:"crew_id,omitempty"`
	CrewName string            `json:"crew_name,omitempty"`
}

// proposedCrew settles which crew a start-work-order proposal is for. A crew chat starts its own crew, so the
// payload may not name one. An agent-chat turn must name one of its crews (by name or id) in "crew"; the payload
// returned carries that crew's id and name instead.
func proposedCrew(chat *model.ChatSpec, payload map[string]any) (map[string]any, error) {
	_, named := payload["crew"]
	_, byID := payload["crew_id"]
	_, byName := payload["crew_name"]
	if chat.Agent == nil {
		if named || byID || byName {
			return nil, &model.ValidationError{Details: []string{"crew: this chat starts its own crew; do not name one"}}
		}
		return payload, nil
	}
	if byID || byName {
		return nil, &model.ValidationError{Details: []string{"crew: name the crew in crew"}}
	}
	want, _ := payload["crew"].(string)
	want = strings.TrimSpace(want)
	names := make([]string, 0, len(chat.Agent.Crews))
	for _, c := range chat.Agent.Crews {
		names = append(names, c.Name)
		if want != "" && (c.ID == want || strings.EqualFold(c.Name, want)) {
			out := make(map[string]any, len(payload)+1)
			for k, v := range payload {
				if k != "crew" {
					out[k] = v
				}
			}
			out["crew_id"], out["crew_name"] = c.ID, c.Name
			return out, nil
		}
	}
	return nil, &model.ValidationError{Details: []string{"crew: must be one of " + strings.Join(names, ", ")}}
}

// checkProposedInputs refuses a cleaned work order proposal the crew could not run as proposed: a required input
// without a default left out, or an input the crew does not have. The crew is the one the proposal names (agent
// chat) or the run's own (crew chat), read now so the check follows its current inputs. The error names the keys,
// so the agent can fix them and propose again.
func (s *Service) checkProposedInputs(ctx context.Context, scope store.RunScope, payload map[string]any) error {
	crewID, _ := payload["crew_id"].(string)
	if crewID == "" {
		crewID = scope.CrewID
	}
	if crewID == "" {
		return &model.ValidationError{Details: []string{"crew: the crew of this chat no longer exists"}}
	}
	crew, err := s.store.GetCrew(ctx, crewID)
	if err != nil {
		return err
	}
	inputs := map[string]string{}
	if given, ok := payload["inputs"].(map[string]any); ok {
		for key, value := range given {
			inputs[key], _ = value.(string)
		}
	}
	if problems := proposedInputProblems(crew, inputs); len(problems) > 0 {
		return &model.ValidationError{Details: problems}
	}
	return nil
}

// proposedInputProblems lists what stops the crew running with these inputs, as a work order would check them (see
// runspec.ResolveInputs), and the crew's input keys when an unknown one was given.
func proposedInputProblems(crew model.Crew, inputs map[string]string) []string {
	_, problems := runspec.ResolveInputs(crew, inputs)
	if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasSuffix(p, "the crew has no input with this key") }) {
		return problems
	}
	if len(crew.Inputs) == 0 {
		return append(problems, "inputs: this crew has no inputs")
	}
	keys := make([]string, 0, len(crew.Inputs))
	for _, in := range crew.Inputs {
		keys = append(keys, in.Key)
	}
	return append(problems, "inputs: this crew's input keys are "+strings.Join(keys, ", "))
}

func startWorkOrderKind() Kind {
	return Kind{
		Type:      TypeStartWorkOrder,
		Normalize: normalizeStartWorkOrder,
		Apply:     applyStartWorkOrder,
		Needs:     []string{auth.WorkOrdersCreate},
	}
}

func normalizeStartWorkOrder(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[startWorkOrder](payload)
	if err != nil {
		return nil, "", err
	}
	out := startWorkOrder{Title: oneLine(in.Title), Brief: strings.TrimSpace(in.Brief), Inputs: map[string]string{},
		CrewID: in.CrewID, CrewName: oneLine(in.CrewName)}
	var problems []string
	if out.CrewID != "" && !uuidPattern.MatchString(out.CrewID) {
		problems = append(problems, "crew_id: must be a crew's id")
	}
	switch n := len([]rune(out.Brief)); {
	case n == 0:
		problems = append(problems, "brief: say what the run should do")
	case n > maxProposedBriefLen:
		problems = append(problems, fmt.Sprintf("brief: must be at most %d characters", maxProposedBriefLen))
	}
	if out.Title == "" {
		out.Title = defaultTitle(out.Brief)
	}
	if len([]rune(out.Title)) > maxProposedTitleLen {
		problems = append(problems, fmt.Sprintf("title: must be at most %d characters", maxProposedTitleLen))
	}
	if len(in.Inputs) > maxProposedInputs {
		problems = append(problems, fmt.Sprintf("inputs: at most %d", maxProposedInputs))
	}
	for key, value := range in.Inputs {
		switch {
		case !proposedInputKey.MatchString(key):
			problems = append(problems, fmt.Sprintf("inputs.%s: not a valid input key", oneLine(key)))
		case len(value) > runspec.MaxInputValueLen:
			problems = append(problems, fmt.Sprintf("inputs.%s: must be at most %d characters", key, runspec.MaxInputValueLen))
		default:
			out.Inputs[key] = strings.TrimSpace(value)
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, "", &model.ValidationError{Details: problems}
	}
	clean, err := asMap(out)
	if out.CrewName != "" {
		return clean, fmt.Sprintf("Start a work order on %s: %s", out.CrewName, out.Title), err
	}
	return clean, "Start a work order: " + out.Title, err
}

// workOrderCrew is the crew a confirmed proposal starts: the conversation's crew, or in agent chat the crew the
// proposal names, which must still be one the agent may start and the person can see.
func workOrderCrew(ctx context.Context, env Env, req Request, in startWorkOrder) (string, error) {
	if req.Conversation.AgentID == nil {
		if in.CrewID != "" || req.Crew.ID == "" {
			return "", &model.ValidationError{Details: []string{"crew_id: this chat starts its own crew"}}
		}
		return req.Crew.ID, nil
	}
	settings, err := env.Store.AgentChatSettings(ctx, *req.Conversation.AgentID)
	if err != nil {
		return "", err
	}
	if !settings.Enabled {
		return "", errChatOff
	}
	if in.CrewID == "" || !slices.Contains(settings.CrewIDs, in.CrewID) {
		return "", &store.ConflictError{Code: "crew_not_offered",
			Message: "The agent may no longer start this crew; ask someone who can edit the agent, or ask the agent again."}
	}
	crew, err := env.Store.GetCrew(ctx, in.CrewID)
	if err == nil && !req.Principal.SeesAreas(crew.AreaIDs) {
		err = store.ErrNotFound
	}
	return crew.ID, err
}

// defaultTitle is the first words of the brief.
func defaultTitle(brief string) string {
	line := oneLine(brief)
	if r := []rune(line); len(r) > defaultTitleFromWord {
		line = strings.TrimSpace(string(r[:defaultTitleFromWord])) + "…"
	}
	return line
}

// oneLine collapses whitespace and control characters into single spaces, and drops hidden characters (model.IsHidden).
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(model.StripHidden(s), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
}

func applyStartWorkOrder(ctx context.Context, env Env, req Request) (map[string]any, error) {
	in, err := decodePayload[startWorkOrder](req.Payload)
	if err != nil {
		return nil, err
	}
	crewID, err := workOrderCrew(ctx, env, req, in)
	if err != nil {
		return nil, err
	}
	wo, err := model.NormalizeWorkOrder(model.WorkOrderInput{Title: in.Title, Brief: in.Brief, CrewID: &crewID, Inputs: in.Inputs, Submit: true})
	if err != nil {
		return nil, err
	}
	// The key makes a repeated confirm (a retry after a lost response) return the work order made the first time
	created, err := env.Launcher.CreateWorkOrder(ctx, req.Principal, wo,
		launch.WorkOrderSource{Kind: model.SourceChat, ID: req.Conversation.ID},
		launch.Idempotency{Key: "chat-proposal:" + req.MessageID}, nil)
	if err != nil {
		// Submitting failed after the draft was made; do not leave a draft behind for a proposal still pending
		if created.WorkOrder.ID != "" && created.Run == nil {
			if derr := env.Store.DeleteWorkOrder(ctx, created.WorkOrder.ID); derr != nil {
				return nil, errors.Join(err, derr)
			}
		}
		return nil, err
	}
	result := map[string]any{"work_order_id": created.WorkOrder.ID, "work_order_number": created.WorkOrder.Number}
	if created.Run != nil {
		result["run_id"] = created.Run.ID
	}
	return result, nil
}
