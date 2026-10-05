package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const activeTaskStateKey = "active-task"

func resolvePlanActiveTaskPath(repoRoot string) (string, bool, error) {
	planPath := filepath.Join(repoRoot, implementationPlanFile)
	content, err := os.ReadFile(planPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", true, fmt.Errorf("read %s: %w", implementationPlanFile, err)
	}
	schedule := taskcontract.ParsePlanSchedule(string(content))
	path, err := schedule.ActiveTask()
	if err != nil {
		return "", true, err
	}
	return path, true, nil
}

func resolveActiveTaskPath(repoRoot string) (string, bool, error) {
	path, wired, err := resolvePlanActiveTaskPath(repoRoot)
	if err != nil || !wired {
		return path, wired, err
	}
	info, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(path)))
	if err != nil {
		return "", true, fmt.Errorf("ACTIVE task file %sを確認できません: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", true, fmt.Errorf("ACTIVE task file %sはregular fileではありません(%s)", path, info.Mode().Type())
	}
	return path, true, nil
}

func (w *Workflow) readActiveTaskState() string {
	return w.state.ReadOr(activeTaskStateKey, "")
}

func (w *Workflow) activeTaskStateSet() bool {
	return w.state.Exists(activeTaskStateKey)
}

func (w *Workflow) resolveAndPinActiveTask() (string, error) {
	if path, wired, err := w.resolveControllerExecutionTask(); err != nil || wired {
		return path, err
	}
	return w.resolveAndPinPlanTask()
}

func (w *Workflow) resolveControllerExecutionTask() (string, bool, error) {
	task, canonical, err := controller.WorkflowExecutionTask(w.config)
	if err != nil || !canonical {
		return "", canonical, err
	}
	binding, err := w.state.LoadControllerRuntimeBinding()
	if err != nil {
		return "", true, fmt.Errorf("read controller runtime binding: %w", err)
	}
	if binding.TaskPath != task.TaskPath || binding.TaskContractDigest != task.ContractDigest {
		return "", true, fmt.Errorf("controller runtime binding differs from canonical controller execution task")
	}
	if !activeTaskFileExists(w.config.RepoRoot, task.TaskPath) {
		return "", true, fmt.Errorf("controller execution task file is missing: %s", task.TaskPath)
	}
	if err := w.recordInitialExecutionUnitDisposition(task.TaskPath); err != nil {
		return "", true, err
	}
	return task.TaskPath, true, nil
}

func (w *Workflow) resolveAndPinPlanTask() (string, error) {
	harnessActive, err := w.repositoryHarnessActive()
	if err != nil {
		return "", err
	}
	if w.activeTaskStateSet() {
		activeTaskPath, err := w.resolvePinnedActiveTask(harnessActive)
		if err != nil {
			return "", err
		}
		if err := w.recordInitialExecutionUnitDisposition(activeTaskPath); err != nil {
			return "", err
		}
		return activeTaskPath, nil
	}
	if !harnessActive {
		if err := w.state.Write(activeTaskStateKey, ""); err != nil {
			return "", err
		}
		return "", nil
	}
	activeTaskPath, err := w.currentPlanActiveTask()
	if err != nil {
		return "", err
	}
	if err := w.state.Write(activeTaskStateKey, activeTaskPath); err != nil {
		return "", err
	}
	if err := w.recordInitialExecutionUnitDisposition(activeTaskPath); err != nil {
		return "", err
	}
	return activeTaskPath, nil
}

func (w *Workflow) recordInitialExecutionUnitDisposition(activeTaskPath string) error {
	executionUnit := os.Getenv(executionunit.DispositionEnv)
	if executionUnit == "" || activeTaskPath == "" {
		return nil
	}
	existing, err := executionunit.CurrentDisposition(w.state)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.ExecutionUnit != executionUnit {
			return fmt.Errorf("execution-unit disposition changed during task start: current=%q requested=%q", existing.ExecutionUnit, executionUnit)
		}
		return nil
	}
	return executionunit.RecordDisposition(w.state, activeTaskPath, executionUnit, w.now().UTC())
}

func (w *Workflow) resolvePinnedActiveTask(harnessActive bool) (string, error) {
	pinned := w.readActiveTaskState()
	if !harnessActive {
		return pinned, nil
	}
	activeTaskPath, _, err := resolvePlanActiveTaskPath(w.config.RepoRoot)
	if err != nil {
		return "", err
	}
	if pinned != activeTaskPath {
		return "", fmt.Errorf("pinned ACTIVE task %q no longer matches Plan ACTIVE %q", pinned, activeTaskPath)
	}
	return pinned, nil
}

func (w *Workflow) currentPlanActiveTask() (string, error) {
	activeTaskPath, wired, err := resolveActiveTaskPath(w.config.RepoRoot)
	if err != nil {
		return "", err
	}
	if !wired {
		return "", nil
	}
	return activeTaskPath, nil
}

func (w *Workflow) ensureActiveTaskPath(phase string) (string, error) {
	activeTaskPath, err := w.resolveAndPinActiveTask()
	if err != nil {
		return "", w.failClosedActiveTaskResolution(phase, err)
	}
	return activeTaskPath, nil
}

func (w *Workflow) gateDecisionActiveTask() (string, error) {
	activeTaskPath, err := w.resolveAndPinActiveTask()
	if err != nil {
		return "", w.failClosedDecisionRejection("worker-decision", parentMetadataGuardSurface.activeUnresolvableOutcome(), "PlanのACTIVE欄からACTIVE task fileを一意に解決できなかったためdecisionを消費していません", err)
	}
	if activeTaskPath != "" && !activeTaskFileExists(w.config.RepoRoot, activeTaskPath) {
		return "", w.failClosedDecisionRejection("worker-decision", parentMetadataGuardSurface.missingOutcome(), "ACTIVE task file "+activeTaskPath+"がworking treeへ存在しないためdecisionを消費していません", nil)
	}
	return activeTaskPath, nil
}

func activeTaskFileExists(repoRoot string, activeTaskPath string) bool {
	if activeTaskPath == "" {
		return true
	}
	info, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(activeTaskPath)))
	return err == nil && info.Mode().IsRegular()
}
