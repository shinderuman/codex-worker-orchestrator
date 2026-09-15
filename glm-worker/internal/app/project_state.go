package app

import (
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"io"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type projectStateOutput struct {
	Version      int                           `json:"version"`
	PlanPresent  bool                          `json:"plan_present"`
	Goal         *projectStateGoal             `json:"goal,omitempty"`
	Schedule     *projectStateSchedule         `json:"schedule,omitempty"`
	Dependencies []projectStateDependency      `json:"dependencies"`
	NextRunnable *string                       `json:"next_runnable"`
	Blockers     []projectStateBlocker         `json:"blockers"`
	Completion   *projectStateCompletion       `json:"completion,omitempty"`
	Continuation projectContinuationObligation `json:"continuation"`
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

type projectStateDependency = repositoryproject.Dependency
type projectStateBlocker = repositoryproject.Blocker

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
		Dependencies: []projectStateDependency{},
		Blockers:     []projectStateBlocker{},
		Continuation: unknownProjectContinuation(projectContinuationReasonPlanAbsent),
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
	if prepared.Goal.Present && prepared.Goal.Status == taskcontract.GoalStatusActive {
		completion, err := buildProjectStateCompletion(cfg, st, prepared.Active, prepared.Next, prepared.Blocked, loaded.Graph)
		if err != nil {
			return output, err
		}
		output.Completion = completion
	}
	output.Continuation = deriveProjectContinuation(output, st)
	return output, nil
}

func buildProjectStateCompletion(cfg config.AppConfig, st *state.StateStore, active, next, blocked []string, graph *repositoryproject.TaskGraph) (*projectStateCompletion, error) {
	if len(active) != 1 {
		return nil, fmt.Errorf("Goal進行中のcompletion評価には単一ACTIVE taskが必要です(active=%d)", len(active))
	}
	activeTask := active[0]
	content, ok := graph.TaskContent(activeTask)
	if !ok {
		return nil, fmt.Errorf("ACTIVE task %sのdependency状態を解決できません", activeTask)
	}
	completion := &projectStateCompletion{Ready: false, ActiveTask: activeTask, Validations: []parentHandoffValidation{}}
	unmet, err := repositoryproject.CompletionScheduleUnmet(next, blocked, activeTask, content)
	if err != nil {
		return nil, err
	}
	unmet = append(unmet, projectStateLifecycleUnmet(st, activeTask, completion)...)
	unmet = append(unmet, projectStateEvidenceUnmet(cfg, st, completion)...)
	completion.Unmet = unmet
	completion.Ready = len(unmet) == 0
	return completion, nil
}

func projectStateLifecycleUnmet(st *state.StateStore, activeTask string, completion *projectStateCompletion) []string {
	unmet := []string{}
	completion.TaskStatus = string(st.TaskStatus())
	if st.TaskStatus() != state.TaskStatusComplete {
		unmet = append(unmet, "task_not_complete")
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil {
		return append(unmet, "lifecycle_inconsistent")
	}
	completion.RequiredAction = string(plan.RequiredAction)
	if plan.RequiredAction != state.ParentActionNone {
		unmet = append(unmet, "pending_parent_action")
	}
	if pinned := st.ReadOr("active-task", ""); pinned != activeTask {
		unmet = append(unmet, "active_task_mismatch")
	}
	return unmet
}

func projectStateEvidenceUnmet(cfg config.AppConfig, st *state.StateStore, completion *projectStateCompletion) []string {
	unmet := []string{}
	snapshot, snapshotErr := state.CaptureGitSnapshot(cfg.RepoRoot)
	if snapshotErr != nil {
		unmet = append(unmet, "snapshot_unavailable")
	} else {
		completion.Validations = currentParentValidations(st, cfg.RepoRoot, &state.SnapshotDigest{
			Head:           snapshot.Head,
			IndexDigest:    snapshot.IndexDigest,
			WorktreeDigest: snapshot.WorktreeDigest,
		})
		if !projectStateValidationPass(completion.Validations) {
			unmet = append(unmet, "validation_not_current")
		}
	}
	clean, cleanErr := projectStateTreeClean(cfg.RepoRoot)
	if cleanErr != nil {
		return append(unmet, "tree_status_unavailable")
	}
	completion.TreeClean = &clean
	if !clean {
		unmet = append(unmet, "tree_not_clean")
	}
	return unmet
}

func projectStateValidationPass(validations []parentHandoffValidation) bool {
	for _, validation := range validations {
		if validation.Status == qualityGateStatusPass {
			return true
		}
	}
	return false
}

func projectStateTreeClean(repoRoot string) (bool, error) {
	command := exec.Command("git", "-C", repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(output)) == "", nil
}
