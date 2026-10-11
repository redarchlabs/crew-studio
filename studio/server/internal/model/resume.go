package model

// ResumeSpec is frozen into a run that resumes another: the outputs of the tasks before StartPosition, which the runner
// hands on as context instead of running those tasks again.
type ResumeSpec struct {
	FromRunID     string       `json:"from_run_id"`
	StartPosition int          `json:"start_position"`
	Outputs       []TaskOutput `json:"outputs"`
}

// ResumeHint says where a finished run could be resumed: the first task that would run, and how many tasks (and how
// much of their cost) would be reused.
type ResumeHint struct {
	StartPosition int    `json:"start_position"`
	TaskName      string `json:"task_name"`
	ReusedTasks   int    `json:"reused_tasks"`
	// ReusedCostUSD is what the reused tasks cost when they ran, when that was recorded
	ReusedCostUSD float64 `json:"reused_cost_usd"`
	// SpentOnTaskUSD is what the run already spent on the task that will run again
	SpentOnTaskUSD float64 `json:"spent_on_task_usd"`
	// Warnings are reasons the remaining tasks are likely to fail as they were, such as a provider key that is missing
	Warnings []string `json:"warnings"`
}

// ResumePoint reports where a crew run that failed or was cancelled can pick up: after the unbroken run of task
// outputs from the first task. It needs run detail (spec and task outputs), at least one finished task and at least
// one task left. Chat turns and hand-offs are never resumed.
func ResumePoint(run Run) (ResumeHint, bool) {
	if run.Kind != RunKindCrew || (run.Status != RunFailed && run.Status != RunCancelled) ||
		run.Spec == nil || run.Spec.Chat != nil || run.TaskOutputs == nil {
		return ResumeHint{}, false
	}
	byPosition := make(map[int]TaskOutput, len(*run.TaskOutputs))
	for _, o := range *run.TaskOutputs {
		byPosition[o.Position] = o
	}
	hint := ResumeHint{}
	for {
		o, done := byPosition[hint.StartPosition]
		if !done {
			break
		}
		if o.CostUSD != nil {
			hint.ReusedCostUSD += *o.CostUSD
		}
		hint.StartPosition++
	}
	hint.ReusedTasks = hint.StartPosition
	if hint.StartPosition == 0 || hint.StartPosition >= len(run.Spec.Tasks) {
		return ResumeHint{}, false
	}
	hint.TaskName = run.Spec.Tasks[hint.StartPosition].Name
	hint.SpentOnTaskUSD = run.TaskCosts[hint.TaskName]
	hint.Warnings = []string{}
	return hint, true
}

// ReusedOutputs are the outputs a resumed run copies: every task before start, each still naming the run that
// produced it (so resuming a resumed run points at the original work).
func ReusedOutputs(run Run, start int) []TaskOutput {
	if run.TaskOutputs == nil {
		return nil
	}
	out := make([]TaskOutput, 0, start)
	for _, o := range *run.TaskOutputs {
		if o.Position >= start {
			continue
		}
		if o.ReusedFromRunID == nil {
			from := run.ID
			o.ReusedFromRunID = &from
		}
		o.ReusedFromUnavailable = false // about the reader of run detail, not the output
		out = append(out, o)
	}
	return out
}
