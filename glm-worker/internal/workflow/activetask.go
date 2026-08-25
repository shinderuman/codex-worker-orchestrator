package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const activeTaskStateKey = "active-task"

const activeTaskPathPrefix = state.ParentTasksDir + "/"

const activeTaskPathExt = ".md"

func resolveActiveTaskPath(repoRoot string) (string, bool, error) {
	planPath := filepath.Join(repoRoot, implementationPlanFile)
	content, err := os.ReadFile(planPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", true, fmt.Errorf("read %s: %w", implementationPlanFile, err)
	}
	entries, err := activeSectionEntries(string(content))
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
	if err := validateActiveTaskPath(path); err != nil {
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

func activeSectionEntries(planContent string) ([]string, error) {
	lines := strings.Split(planContent, "\n")
	inSection := false
	var entries []string
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			inSection = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == "ACTIVE"
			continue
		}
		if !inSection {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			return nil, fmt.Errorf("%sのACTIVE欄の行 %qがschedule list記法(`- `bulletとblank行のみ)へ違反しています", implementationPlanFile, trimmed)
		}
		path, err := activeEntryPath(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		if err != nil {
			return nil, err
		}
		entries = append(entries, path)
	}
	return entries, nil
}

func activeEntryPath(item string) (string, error) {
	switch strings.Count(item, "`") {
	case 0:
		return item, nil
	case 2:
		if strings.HasPrefix(item, "`") && strings.HasSuffix(item, "`") {
			return item[1 : len(item)-1], nil
		}
	}
	return "", fmt.Errorf("ACTIVE欄の項目 %qがbullet構文(逆引用符1組で囲まれた単一task path、または逆引用符なしの直書き)へ違反しています", item)
}

func validateActiveTaskPath(path string) error {
	if !strings.HasPrefix(path, activeTaskPathPrefix) {
		return fmt.Errorf("ACTIVE task path %qは%s配下である必要があります", path, state.ParentTasksDir)
	}
	rest := strings.TrimPrefix(path, activeTaskPathPrefix)
	if rest == "" || strings.Contains(path, `\`) || strings.Contains(rest, "//") {
		return fmt.Errorf("ACTIVE task path %qが配置契約に違反しています", path)
	}
	if !strings.HasSuffix(path, activeTaskPathExt) {
		return fmt.Errorf("ACTIVE task path %qが配置契約に違反しています(%s fileに限定)", path, activeTaskPathExt)
	}
	for _, segment := range strings.Split(rest, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("ACTIVE task path %qが配置契約に違反しています", path)
		}
	}
	return nil
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
