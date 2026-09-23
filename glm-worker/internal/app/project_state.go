package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type projectStateOutput struct {
	Version      int                            `json:"version"`
	PlanPresent  bool                           `json:"plan_present"`
	Goal         *projectStateGoal              `json:"goal,omitempty"`
	Schedule     *projectStateSchedule          `json:"schedule,omitempty"`
	Dependencies []repositoryproject.Dependency `json:"dependencies"`
	NextRunnable *string                        `json:"next_runnable"`
	Blockers     []repositoryproject.Blocker    `json:"blockers"`
	Completion   *projectStateCompletion        `json:"completion,omitempty"`
	Continuation projectContinuationProjection  `json:"continuation"`
}

type projectStateGoal struct {
	Present bool   `json:"present"`
	Status  string `json:"status,omitempty"`
}

type projectStateSchedule struct {
	Active  []string `json:"active"`
	Next    []string `json:"next"`
	Blocked []string `json:"blocked"`
}

type projectStateCompletion struct {
	Ready          bool                      `json:"ready"`
	Unmet          []string                  `json:"unmet,omitempty"`
	ActiveTask     string                    `json:"active_task,omitempty"`
	TaskStatus     string                    `json:"task_status,omitempty"`
	RequiredAction string                    `json:"required_action,omitempty"`
	Validations    []parentHandoffValidation `json:"validations"`
	TreeClean      *bool                     `json:"tree_clean,omitempty"`
}

const projectStateVersion = 2

func printProjectState(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	output, err := buildProjectState(cfg, st)
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, output)
}

func executeStatelessProjection(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	if cmd.Mode == ModeProjectState {
		return printProjectState(cfg, state.AttachStateStore(cfg), stdout)
	}
	return printPacketCheck(cmd, stdout)
}

func buildProjectState(cfg config.AppConfig, st *state.StateStore) (projectStateOutput, error) {
	output := projectStateOutput{
		Version:      projectStateVersion,
		Dependencies: []repositoryproject.Dependency{},
		Blockers:     []repositoryproject.Blocker{},
		Continuation: unknownProjectContinuation(repositoryproject.ReasonPlanAbsent),
	}
	loaded, err := repositoryprojecttree.LoadProjectState(cfg.RepoRoot)
	if err != nil {
		return output, err
	}
	if !loaded.PlanPresent {
		return output, nil
	}
	prepared := loaded.Plan
	output.PlanPresent = true
	output.Goal = &projectStateGoal{Present: prepared.Goal.Present, Status: prepared.Goal.Status}
	output.Schedule = &projectStateSchedule{Active: prepared.Active, Next: prepared.Next, Blocked: prepared.Blocked}
	output.Dependencies = loaded.Graph.Dependencies()
	output.NextRunnable = loaded.Graph.NextRunnable(prepared.Next)
	output.Blockers = loaded.Graph.Blockers(prepared.Next, prepared.Blocked)

	continuation, completion, err := parentcontinuation.BuildProjectContinuation(cfg.RepoRoot, st, loaded)
	if err != nil {
		return output, err
	}
	if completion != nil {
		output.Completion = projectStateCompletionFromFocused(completion)
	}
	output.Continuation = projectContinuationFromPolicy(continuation)
	return output, nil
}

func projectStateCompletionFromFocused(evidence *parentcontinuation.CompletionEvidence) *projectStateCompletion {
	validations := make([]parentHandoffValidation, 0, len(evidence.Validations))
	for _, record := range evidence.Validations {
		validations = append(validations, parentHandoffValidationFromRun(record))
	}
	return &projectStateCompletion{
		Ready:          evidence.View.Ready,
		Unmet:          append([]string(nil), evidence.View.Unmet...),
		ActiveTask:     evidence.ActiveTask,
		TaskStatus:     string(evidence.TaskStatus),
		RequiredAction: string(evidence.RequiredAction),
		Validations:    validations,
		TreeClean:      evidence.TreeClean,
	}
}
