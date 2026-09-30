package parentactioncmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentfix"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

const sameTaskScopeRegistrationHint = "same-task scope is not proven; preserve current ACTIVE and register the independent or ambiguous finding with glm-parent-action record-defect-finding --task <IMPLEMENTATION_TASKS/...md>"

func executeScopedFixAction(cfg config.AppConfig, descriptor parentaction.PayloadAction, args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 {
		return executeStagedPayloadAction(cfg, descriptor, args, stdout, stderr)
	}
	if err := validateSameTaskFixAdmission(cfg, args[2:]); err != nil {
		return err
	}
	return executeStagedPayloadAction(cfg, descriptor, args, stdout, stderr)
}

func validateSameTaskFixAdmission(cfg config.AppConfig, optionArgs []string) error {
	_, remaining, err := parentfix.Extract(optionArgs)
	if err != nil || len(remaining) != 0 {
		return fmt.Errorf("invalid fix options")
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	if err := validateSameTaskScopeAdmission(cfg.RepoRoot, st); err != nil {
		return fmt.Errorf("same-task fix rejected: %w", err)
	}
	return nil
}

func validateSameTaskScopeAdmission(repoRoot string, st *state.StateStore) error {
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		return fmt.Errorf("current review binding is unavailable: %w; %s", err, sameTaskScopeRegistrationHint)
	}
	if binding == nil || len(binding.Targets) == 0 || binding.Proof == nil {
		return fmt.Errorf("current machine-owned review evidence is unavailable; %s", sameTaskScopeRegistrationHint)
	}
	ready, err := st.ParentReviewAcceptReady()
	if err != nil {
		return fmt.Errorf("current review proof cannot be validated: %w; %s", err, sameTaskScopeRegistrationHint)
	}
	if !ready {
		return fmt.Errorf("current review proof is not bound to the exact current snapshot; %s", sameTaskScopeRegistrationHint)
	}

	activeTask := filepath.ToSlash(strings.TrimSpace(st.ReadOr("active-task", "")))
	if activeTask == "" {
		return fmt.Errorf("current ACTIVE task binding is missing; %s", sameTaskScopeRegistrationHint)
	}
	if err := taskcontract.ValidateActiveTaskPath(activeTask); err != nil {
		return fmt.Errorf("current ACTIVE task binding is invalid: %w", err)
	}

	diff, available, err := taskdiff.Capture(repoRoot, st)
	if err != nil {
		return fmt.Errorf("current task diff cannot be captured: %w; %s", err, sameTaskScopeRegistrationHint)
	}
	for _, target := range binding.Targets {
		path, locator, err := reviewtarget.Parse(target)
		if err != nil {
			return fmt.Errorf("current review target is invalid: %w", err)
		}
		path = filepath.ToSlash(path)
		if path == activeTask {
			if err := validateMachineBoundTaskAuthorityTarget(repoRoot, activeTask, locator); err != nil {
				return fmt.Errorf("current review target is not a concrete Task Contract/Acceptance locus: %w; %s", err, sameTaskScopeRegistrationHint)
			}
			continue
		}
		if !available || !taskDiffCoversReviewTarget(target, diff) {
			return fmt.Errorf("current review target %q is not covered by the current Task-produced diff; %s", target, sameTaskScopeRegistrationHint)
		}
	}
	return nil
}

func taskDiffCoversReviewTarget(target string, diff []byte) bool {
	path, _, err := reviewtarget.Parse(target)
	if err != nil || len(diff) == 0 {
		return false
	}
	section := parentevidence.ReviewDiffFileSection(string(diff), path)
	if section == "" {
		return false
	}
	digest := sha256.Sum256([]byte(section))
	return parentevidence.ReviewDiffCoversTarget(target, parentevidence.DiffBody{
		Files: []parentevidence.DiffFile{{
			Path:        path,
			Status:      "M",
			WorktreeSHA: hex.EncodeToString(digest[:]),
		}},
		Body: string(diff),
	})
}

func validateMachineBoundTaskAuthorityTarget(repoRoot, activeTask, locator string) error {
	start, end, ok := parentevidence.NumericRange(locator)
	if !ok {
		return fmt.Errorf("Task authority target must use a concrete line or line range")
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(activeTask)))
	if err != nil {
		return fmt.Errorf("read current ACTIVE task: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	if start < 1 || end > len(lines) {
		return fmt.Errorf("Task authority target %s is outside current ACTIVE task", locator)
	}
	sections := taskSectionByLine(lines)
	substantive := false
	for line := start; line <= end; line++ {
		section := sections[line-1]
		if section != "## Contract" && section != "## Acceptance criteria" && section != "## Acceptance" {
			return fmt.Errorf("Task authority target %s is outside Contract/Acceptance", locator)
		}
		trimmed := strings.TrimSpace(lines[line-1])
		if trimmed != "" && !strings.HasPrefix(trimmed, "## ") {
			substantive = true
		}
	}
	if !substantive {
		return fmt.Errorf("Task authority target %s does not identify a concrete Contract/Acceptance line", locator)
	}
	return nil
}

func taskSectionByLine(lines []string) []string {
	sections := make([]string, len(lines))
	section := ""
	fence := 0
	for index, line := range lines {
		backticks := leadingTaskFenceBackticks(line)
		if fence != 0 {
			sections[index] = section
			if backticks >= fence {
				fence = 0
			}
			continue
		}
		if backticks >= 3 {
			fence = backticks
			sections[index] = section
			continue
		}
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(line)
		}
		sections[index] = section
	}
	return sections
}

func leadingTaskFenceBackticks(line string) int {
	line = strings.TrimLeft(line, " \t")
	count := 0
	for count < len(line) && line[count] == '`' {
		count++
	}
	return count
}
