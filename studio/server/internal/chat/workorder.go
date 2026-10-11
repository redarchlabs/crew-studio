package chat

import (
	"context"
	"fmt"
	"slices"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Work order conversations: each person can talk with a work order's crew about it, one conversation per person and
// work order. They work whether or not the crew has chat on: the crew's front desk answers when chat is set up,
// otherwise the agent of the crew's last task (the one that wrote the final output). Each turn is given the work
// order's brief and its latest attempt's output.

// WorkOrderConversation returns the caller's conversation about a work order; store.ErrNotFound when they have none.
func (s *Service) WorkOrderConversation(ctx context.Context, p *auth.Principal, wo model.WorkOrder) (model.Conversation, error) {
	if err := checkChatter(p); err != nil {
		return model.Conversation{}, err
	}
	return s.store.WorkOrderConversation(ctx, wo.ID, p.UserID)
}

// SendWorkOrderMessage adds the person's message to their conversation about the work order (starting it if need be)
// and starts the turn that answers it.
func (s *Service) SendWorkOrderMessage(ctx context.Context, p *auth.Principal, wo model.WorkOrder, content string, files ...model.Attachment) (Turn, error) {
	if err := checkChatter(p); err != nil {
		return Turn{}, err
	}
	text, err := model.NormalizeChatMessage(content)
	if err != nil {
		return Turn{}, err
	}
	if wo.CrewID == nil {
		return Turn{}, problem("The crew this work order ran has been deleted.")
	}
	crew, err := s.visibleCrew(ctx, p, *wo.CrewID)
	if err != nil {
		return Turn{}, err
	}
	base, settings, err := s.workOrderBase(ctx, p, crew)
	if err != nil {
		return Turn{}, err
	}
	conv, err := s.store.EnsureWorkOrderConversation(ctx, crew.ID, p.UserID, wo.ID, model.ChatTitle(fmt.Sprintf("WO-%d: %s", wo.Number, wo.Title)))
	if err != nil {
		return Turn{}, err
	}
	return s.start(ctx, p, conv, base, settings, text, files)
}

// workOrderBase freezes the agent that answers questions about the crew's work: the front desk when chat is set up,
// otherwise the agent of the crew's last task.
func (s *Service) workOrderBase(ctx context.Context, p *auth.Principal, crew model.Crew) (model.RunSpec, model.ChatSettings, error) {
	settings, err := s.store.ChatSettings(ctx, crew.ID)
	if err != nil {
		return model.RunSpec{}, settings, err
	}
	lib, err := s.launcher.Library(ctx)
	if err != nil {
		return model.RunSpec{}, settings, err
	}
	agentID, chatLLM := "", (*string)(nil)
	if settings.Enabled && settings.FrontDeskAgentID != nil && slices.Contains(crew.AgentIDs, *settings.FrontDeskAgentID) {
		agentID, chatLLM = *settings.FrontDeskAgentID, settings.LLMID
	} else {
		agentID = finalAgent(crew, lib.Tasks)
	}
	if agentID == "" {
		return model.RunSpec{}, settings, problem("The crew has no agents to talk to.")
	}
	spec, err := s.deskSpec(ctx, p, crew, lib, agentID, chatLLM, settings)
	return spec, settings, err
}

// finalAgent is the agent of the crew's last task that has one in the crew, else the crew's first agent.
func finalAgent(crew model.Crew, tasks map[string]model.Task) string {
	for i := len(crew.TaskIDs) - 1; i >= 0; i-- {
		if t, ok := tasks[crew.TaskIDs[i]]; ok && t.AgentID != nil && slices.Contains(crew.AgentIDs, *t.AgentID) {
			return *t.AgentID
		}
	}
	if len(crew.AgentIDs) > 0 {
		return crew.AgentIDs[0]
	}
	return ""
}

// chatWorkOrder is what a turn is told about the work order: its brief and its latest attempt's output (cut).
func (s *Service) chatWorkOrder(ctx context.Context, workOrderID string) (*model.ChatWorkOrder, error) {
	wo, err := s.store.GetWorkOrder(ctx, workOrderID)
	if err != nil {
		return nil, err
	}
	out := &model.ChatWorkOrder{Number: wo.Number, Title: wo.Title, Brief: wo.Brief, Attempt: wo.RunCount}
	if wo.CurrentRunID == nil {
		return out, nil
	}
	run, err := s.store.GetRun(ctx, *wo.CurrentRunID)
	if err != nil {
		return nil, err
	}
	out.Status = string(run.Status)
	if run.Output != nil {
		out.Output = model.CutText(*run.Output, model.MaxRevisionOutputChars)
	}
	return out, nil
}

func (s *Service) visibleWorkOrder(ctx context.Context, p *auth.Principal, id string) (model.WorkOrder, error) {
	wo, err := s.store.GetWorkOrder(ctx, id)
	if err == nil && !p.SeesAreas(wo.AreaIDs) {
		err = store.ErrNotFound
	}
	return wo, err
}
