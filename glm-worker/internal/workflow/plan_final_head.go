package workflow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const parentCompletionHeadVerified = "plan completion head: verified"

type finalHeadPlanSnapshot struct {
	Plan    string
	Head    string
	Status  string
	Present bool
}

func CheckFinalHeadPlan(root string) (string, error) {
	snapshot, err := finalHeadPlan(root)
	if err != nil {
		return "", err
	}
	if !snapshot.Present {
		return "plan final head: " + snapshot.Status, nil
	}
	if err := validateFinalHeadPlan(root, snapshot.Head, snapshot.Plan); err != nil {
		return "", err
	}
	return "plan final head: verified", nil
}

func CheckParentCompletionHead(root string) (string, error) {
	snapshot, err := finalHeadPlan(root)
	if err != nil {
		return "", err
	}
	if !snapshot.Present {
		return "plan completion head: " + snapshot.Status, nil
	}
	goal, err := taskcontract.ParsePlanGoal(snapshot.Plan)
	if err != nil {
		return "", err
	}
	if goal.Present && goal.Status == taskcontract.GoalStatusCompleted {
		if err := validateGoalTerminalFinalHeadPlan(root, snapshot.Head, snapshot.Plan); err != nil {
			return "", err
		}
		return parentCompletionHeadVerified, nil
	}
	if goal.Present && goal.Status == taskcontract.GoalStatusActive {
		blockedOnly, err := validateBlockedOnlyFinalHeadPlan(root, snapshot.Head, snapshot.Plan)
		if err != nil {
			return "", err
		}
		if blockedOnly {
			return parentCompletionHeadVerified, nil
		}
	}
	if err := validateFinalHeadPlan(root, snapshot.Head, snapshot.Plan); err != nil {
		return "", err
	}
	return parentCompletionHeadVerified, nil
}

func validateGoalTerminalFinalHeadPlan(root, head, plan string) error {
	schedule := taskcontract.ParsePlanSchedule(plan)
	active, activeErr := schedule.ActiveEntries()
	next, blocked, nonActiveErr := schedule.NonActiveEntries()
	if activeErr != nil {
		return activeErr
	}
	if nonActiveErr != nil {
		return nonActiveErr
	}
	if len(active) > 0 || len(next) > 0 || len(blocked) > 0 {
		return fmt.Errorf("completed GOALのHEAD planはACTIVE/NEXT/BLOCKEDを空にする必要があります(active=%d next=%d blocked=%d)", len(active), len(next), len(blocked))
	}
	return validateFinalHeadScheduleClosure(root, head, schedule)
}

func validateBlockedOnlyFinalHeadPlan(root, head, plan string) (bool, error) {
	schedule := taskcontract.ParsePlanSchedule(plan)
	active, err := schedule.ActiveEntries()
	if err != nil {
		return false, err
	}
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return false, err
	}
	if len(active) != 0 || len(next) != 0 || len(blocked) == 0 {
		return false, nil
	}
	for _, path := range blocked {
		if err := taskcontract.ValidateActiveTaskPath(path); err != nil {
			return true, err
		}
		if err := validateFinalHeadTask(root, head, path); err != nil {
			return true, err
		}
	}
	if err := validateFinalHeadScheduleClosure(root, head, schedule); err != nil {
		return true, err
	}
	return true, nil
}

func finalHeadPlan(root string) (finalHeadPlanSnapshot, error) {
	repository, err := finalHeadRepository(root)
	if err != nil {
		return finalHeadPlanSnapshot{}, err
	}
	if !repository {
		return finalHeadPlanSnapshot{Status: "skipped (not a git repository)"}, nil
	}

	head, err := state.ResolveGitHeadAuthority("git", root)
	if err != nil {
		return finalHeadPlanSnapshot{}, fmt.Errorf("final HEAD authorityを確認できません: %w", err)
	}
	if head.Unborn {
		return finalHeadPlanSnapshot{Status: "skipped (no commits)"}, nil
	}

	if _, err := finalHeadGitOutput(root, "ls-files", "--error-unmatch", "--", implementationPlanFile); err != nil {
		if finalHeadGitExitCode(err) == 1 {
			return finalHeadPlanSnapshot{Status: "skipped (IMPLEMENTATION_PLAN.local.md is untracked)"}, nil
		}
		return finalHeadPlanSnapshot{}, fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのindex追跡状態を確認できません: %w", err)
	}

	entry, err := finalHeadGitOutput(root, "ls-tree", head.Head, "--", implementationPlanFile)
	if err != nil {
		return finalHeadPlanSnapshot{}, fmt.Errorf("HEADのIMPLEMENTATION_PLAN.local.md存在状態を確認できません: %w", err)
	}
	if strings.TrimSpace(entry) == "" {
		return finalHeadPlanSnapshot{Head: head.Head, Status: "skipped (IMPLEMENTATION_PLAN.local.md is not in HEAD yet)"}, nil
	}

	plan, err := finalHeadGitOutput(root, "show", head.Head+":"+implementationPlanFile)
	if err != nil {
		return finalHeadPlanSnapshot{}, fmt.Errorf("HEADのIMPLEMENTATION_PLAN.local.mdを読めません: %w", err)
	}
	return finalHeadPlanSnapshot{Plan: plan, Head: head.Head, Present: true}, nil
}

func finalHeadRepository(root string) (bool, error) {
	if _, err := finalHeadGitOutput(root, "rev-parse", "--git-dir"); err == nil {
		return true, nil
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return false, err
		}
		metadata, metadataErr := finalHeadGitMetadataPresent(root)
		if metadataErr != nil {
			return false, metadataErr
		}
		if metadata {
			return false, err
		}
	}
	return false, nil
}

func finalHeadGitMetadataPresent(root string) (bool, error) {
	dir, err := filepath.Abs(root)
	if err != nil {
		return false, fmt.Errorf("repository rootを解決できません: %w", err)
	}
	for {
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Lstat(gitPath); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("git metadataを確認できません (%s): %w", gitPath, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, nil
		}
		dir = parent
	}
}

func finalHeadGitExitCode(err error) int {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return -1
	}
	return exitErr.ExitCode()
}

func validateFinalHeadPlan(root, head, plan string) error {
	schedule := taskcontract.ParsePlanSchedule(plan)
	activePath, err := schedule.ValidateComplete()
	if err != nil {
		return err
	}
	if err := validateFinalHeadActiveTask(root, head, activePath); err != nil {
		return err
	}
	for _, entries := range [][]string{schedule.Next, schedule.Blocked} {
		for _, path := range entries {
			if err := validateFinalHeadTask(root, head, path); err != nil {
				return err
			}
		}
	}
	return validateFinalHeadScheduleClosure(root, head, schedule)
}

func validateFinalHeadScheduleClosure(root, head string, schedule taskcontract.PlanSchedule) error {
	entries, err := finalHeadTaskCorpusEntries(root, head)
	if err != nil {
		return err
	}
	failures := schedule.ClosureFailures(entries)
	if len(failures) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(failures))
	for _, failure := range failures {
		reasons = append(reasons, failure.Reason)
	}
	return fmt.Errorf("HEADのPlanとIMPLEMENTATION_TASKS corpusのclosureが成立しません: %s", strings.Join(reasons, "; "))
}

func finalHeadTaskCorpusEntries(root, head string) ([]taskcontract.TaskCorpusEntry, error) {
	output, err := finalHeadGitOutput(root, "ls-tree", "-r", "-t", "-z", head, "--", taskcontract.TasksDir)
	if err != nil {
		return nil, fmt.Errorf("HEADのtask corpusを列挙できません: %w", err)
	}
	var entries []taskcontract.TaskCorpusEntry
	for _, record := range strings.Split(output, "\x00") {
		if record == "" {
			continue
		}
		entry, ok, err := parseFinalHeadTaskCorpusRecord(record)
		if err != nil {
			return nil, err
		}
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func parseFinalHeadTaskCorpusRecord(record string) (taskcontract.TaskCorpusEntry, bool, error) {
	metadata, path, found := strings.Cut(record, "\t")
	if !found {
		return taskcontract.TaskCorpusEntry{}, false, fmt.Errorf("HEADのtask corpus entry %qを読み込めません", record)
	}
	if !strings.HasSuffix(path, ".md") {
		return taskcontract.TaskCorpusEntry{}, false, nil
	}
	fields := strings.Fields(metadata)
	if len(fields) < 2 {
		return taskcontract.TaskCorpusEntry{}, false, fmt.Errorf("HEADのtask corpus entry %qを読み込めません", record)
	}
	regularBlob := (fields[0] == "100644" || fields[0] == "100755") && fields[1] == "blob"
	return taskcontract.TaskCorpusEntry{Path: path, Regular: regularBlob}, true, nil
}

func validateFinalHeadActiveTask(root, head, path string) error {
	if err := validateFinalHeadTask(root, head, path); err != nil {
		return err
	}
	content, err := finalHeadGitOutput(root, "show", head+":"+path)
	if err != nil {
		return fmt.Errorf("HEADのACTIVE task contract %sを読めません: %w", path, err)
	}
	if _, err := taskcontract.ParseExternalFeasibility([]byte(content)); err != nil {
		return fmt.Errorf("HEADのACTIVE task contract %sを受理できません: %w", path, err)
	}
	return nil
}

func validateFinalHeadTask(root, head, path string) error {
	entry, err := finalHeadGitOutput(root, "ls-tree", head, "--", path)
	if err != nil {
		return fmt.Errorf("HEADのtask file %sを確認できません: %w", path, err)
	}
	fields := strings.Fields(entry)
	if len(fields) < 2 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
		return fmt.Errorf("HEADのplanが参照するtask file %s がHEAD treeへregular fileとして存在しません", path)
	}
	return nil
}

func finalHeadGitOutput(root string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimRight(string(output), "\n"), nil
}
