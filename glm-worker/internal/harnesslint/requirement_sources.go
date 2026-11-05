package harnesslint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const requirementSourceRule = "task-requirement-source-closure"

// requirementSourceViolations checks explicit, typed requirement references.
// A filename mentioned in a historical narrative is not a live dependency.
func requirementSourceViolations(root string, paths []string) ([]Violation, error) {
	present := make(map[string]bool, len(paths))
	for _, path := range paths {
		present[path] = true
	}
	var violations []Violation
	for _, taskPath := range paths {
		if !isTaskCorpusPath(taskPath) {
			continue
		}
		content, err := readRegularFile(root, taskPath)
		if err != nil {
			return nil, err
		}
		sources, err := taskcontract.ParseRequirementSources(content)
		if err != nil {
			violations = append(violations, requirementSourceViolation(taskPath, 1, err.Error()))
			continue
		}
		for _, source := range sources {
			switch source.Kind {
			case taskcontract.RequirementSourceTask, taskcontract.RequirementSourceFile:
				if !present[source.Path] {
					violations = append(violations, requirementSourceViolation(taskPath, source.Line,
						fmt.Sprintf("current requirement source %s is missing; parent must transfer or fix the source", source.Path)))
					continue
				}
				info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(source.Path)))
				if err != nil || !info.Mode().IsRegular() {
					violations = append(violations, requirementSourceViolation(taskPath, source.Line,
						fmt.Sprintf("current requirement source %s is not a regular file", source.Path)))
				}
			case taskcontract.RequirementSourceGit:
				if err := verifyPinnedRequirementSource(root, source); err != nil {
					violations = append(violations, requirementSourceViolation(taskPath, source.Line,
						fmt.Sprintf("pinned requirement source %s:%s is unavailable: %v", source.Commit, source.Path, err)))
				}
			}
		}
	}
	return violations, nil
}

func verifyPinnedRequirementSource(root string, source taskcontract.RequirementSource) error {
	typeName, err := exec.Command("git", "-C", root, "cat-file", "-t", source.Commit).Output()
	if err != nil || strings.TrimSpace(string(typeName)) != "commit" {
		return fmt.Errorf("referenced Git commit is not available")
	}
	typeName, err = exec.Command("git", "-C", root, "cat-file", "-t", source.Commit+":"+source.Path).Output()
	if err != nil || strings.TrimSpace(string(typeName)) != "blob" {
		return fmt.Errorf("referenced Git path is not a blob at the pinned commit")
	}
	return nil
}

func requirementSourceViolation(path string, line int, message string) Violation {
	return Violation{
		Rule:    requirementSourceRule,
		Path:    path,
		Line:    line,
		Column:  1,
		Message: message,
	}
}
