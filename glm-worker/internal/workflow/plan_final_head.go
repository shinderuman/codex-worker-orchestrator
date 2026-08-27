package workflow

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var finalHeadTransitionPattern = regexp.MustCompile(`(^|[^[:alnum:]_])(amend|install)(する)?(の|直)?前`)

func CheckFinalHeadPlan(root string) (string, error) {
	plan, status, ok, err := finalHeadPlan(root)
	if err != nil {
		return "", err
	}
	if !ok {
		return "plan final head: " + status, nil
	}
	if err := validateFinalHeadPlan(root, plan); err != nil {
		return "", err
	}
	return "plan final head: verified", nil
}

func finalHeadPlan(root string) (string, string, bool, error) {
	if _, err := finalHeadGitOutput(root, "rev-parse", "--git-dir"); err != nil {
		return "", "skipped (not a git repository)", false, nil
	}
	if _, err := finalHeadGitOutput(root, "rev-parse", "--verify", "HEAD"); err != nil {
		return "", "skipped (no commits)", false, nil
	}
	if _, err := finalHeadGitOutput(root, "ls-files", "--error-unmatch", "--", implementationPlanFile); err != nil {
		return "", "skipped (IMPLEMENTATION_PLAN.local.md is untracked)", false, nil
	}
	plan, err := finalHeadGitOutput(root, "show", "HEAD:"+implementationPlanFile)
	if err != nil {
		return "", "skipped (IMPLEMENTATION_PLAN.local.md is not in HEAD yet)", false, nil
	}
	return plan, "", true, nil
}

func validateFinalHeadPlan(root string, plan string) error {
	active, err := activeSectionEntries(plan)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		return fmt.Errorf("HEADの%sのACTIVE欄にtask fileがありません", implementationPlanFile)
	}
	if len(active) > 1 {
		return fmt.Errorf("HEADの%sのACTIVE欄が一意ではありません(%d件)", implementationPlanFile, len(active))
	}
	activePath := active[0]
	if err := validateFinalHeadTask(root, activePath); err != nil {
		return err
	}
	scheduled, err := finalHeadScheduledTasks(root, plan)
	if err != nil {
		return err
	}
	if scheduled[activePath] {
		return fmt.Errorf("HEADのplanのACTIVE task file %s がNEXT/BLOCKEDへ重複して記載されています", activePath)
	}
	if err := validateFinalHeadBranch(root, plan); err != nil {
		return err
	}
	return validateFinalHeadTransition(plan)
}

func finalHeadScheduledTasks(root string, plan string) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, section := range []string{"NEXT", "BLOCKED"} {
		entries, err := finalHeadScheduleEntries(plan, section)
		if err != nil {
			return nil, err
		}
		for _, path := range entries {
			if err := validateFinalHeadTask(root, path); err != nil {
				return nil, err
			}
			result[path] = true
		}
	}
	return result, nil
}

func finalHeadScheduleEntries(plan string, section string) ([]string, error) {
	lines := finalHeadSectionLines(plan, section)
	if len(lines) == 0 {
		return nil, nil
	}
	synthetic := "## ACTIVE\n" + strings.Join(lines, "\n") + "\n"
	entries, err := activeSectionEntries(synthetic)
	if err != nil {
		return nil, fmt.Errorf("HEADのplanの%s欄: %w", section, err)
	}
	return entries, nil
}

func validateFinalHeadTask(root string, path string) error {
	if err := validateActiveTaskPath(path); err != nil {
		return err
	}
	entry, err := finalHeadGitOutput(root, "ls-tree", "HEAD", "--", path)
	if err != nil {
		return fmt.Errorf("HEADのtask file %sを確認できません: %w", path, err)
	}
	fields := strings.Fields(entry)
	if len(fields) < 2 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
		return fmt.Errorf("HEADのplanが参照するtask file %s がHEAD treeへregular fileとして存在しません", path)
	}
	return nil
}

func validateFinalHeadBranch(root string, plan string) error {
	want, err := finalHeadBoundaryBranch(plan)
	if err != nil {
		return err
	}
	got, err := finalHeadGitOutput(root, "branch", "--show-current")
	if err != nil {
		return fmt.Errorf("current branch確認に失敗しました: %w", err)
	}
	got = strings.TrimSpace(got)
	if want == got {
		return nil
	}
	if got == "" {
		got = "detached HEAD"
	}
	return fmt.Errorf("HEADのplanのGit境界branch %sが現在のbranch(%s)と矛盾しています", want, got)
}

func finalHeadBoundaryBranch(plan string) (string, error) {
	for _, line := range finalHeadSectionLines(plan, "現在のGit境界") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- branch:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- branch:"))
		if strings.HasPrefix(value, "`") && strings.HasSuffix(value, "`") && len(value) >= 2 {
			value = value[1 : len(value)-1]
		}
		if value == "" || strings.Contains(value, "`") {
			return "", fmt.Errorf("HEADのplanのGit境界branchを解決できません: %q", trimmed)
		}
		return value, nil
	}
	return "", fmt.Errorf("HEADのplanに現在のGit境界branchがありません")
}

func validateFinalHeadTransition(plan string) error {
	for _, section := range []string{"現在のGit境界", "現在の停止理由", "次の親Codex操作"} {
		for _, line := range finalHeadSectionLines(plan, section) {
			if finalHeadTransitionPattern.MatchString(line) {
				return fmt.Errorf("HEADのplanの現在状態記述が完了済みcommitの操作を未実施としています: %s", strings.TrimSpace(line))
			}
		}
	}
	return nil
}

func finalHeadSectionLines(plan string, sectionPrefix string) []string {
	var result []string
	inSection := false
	for _, line := range strings.Split(plan, "\n") {
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			inSection = strings.HasPrefix(heading, sectionPrefix)
			continue
		}
		if inSection {
			result = append(result, line)
		}
	}
	return result
}

func finalHeadGitOutput(root string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimRight(string(output), "\n"), nil
}
