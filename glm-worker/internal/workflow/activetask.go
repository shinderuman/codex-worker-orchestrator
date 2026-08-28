package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const activeTaskStateKey = "active-task"

func resolveActiveTaskPath(repoRoot string) (string, bool, error) {
	planPath := filepath.Join(repoRoot, implementationPlanFile)
	content, err := os.ReadFile(planPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", true, fmt.Errorf("read %s: %w", implementationPlanFile, err)
	}
	entries, err := taskcontract.ActiveSectionEntries(string(content))
	if err != nil {
		return "", true, err
	}
	if len(entries) == 0 {
		return "", true, fmt.Errorf("%sのACTIVE欄にtask fileがありません", implementationPlanFile)
	}
	if len(entries) > 1 {
		return "", true, fmt.Errorf("%sのACTIVE欄が一意ではありません(%d件)", implementationPlanFile, len(entries))
	}
	path := entries[0]
	if err := taskcontract.ValidateActiveTaskPath(path); err != nil {
		return "", true, err
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
	if w.activeTaskStateSet() {
		return w.readActiveTaskState(), nil
	}
	activeTaskPath, wired, err := resolveActiveTaskPath(w.config.RepoRoot)
	if err != nil {
		return "", err
	}
	if !wired {
		activeTaskPath = ""
	}
	if err := w.state.Write(activeTaskStateKey, activeTaskPath); err != nil {
		return "", err
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
