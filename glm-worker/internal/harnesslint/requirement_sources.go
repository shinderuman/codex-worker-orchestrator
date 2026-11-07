package harnesslint

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojectcommit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const requirementSourceRule = "task-requirement-source-closure"

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
		taskViolations, err := checkTaskRequirementSources(root, taskPath, present)
		if err != nil {
			return nil, err
		}
		violations = append(violations, taskViolations...)
	}
	return violations, nil
}

func checkTaskRequirementSources(root, taskPath string, present map[string]bool) ([]Violation, error) {
	content, err := readRegularFile(root, taskPath)
	if err != nil {
		return nil, err
	}
	sources, err := taskcontract.ParseRequirementSources(content)
	if err != nil {
		return []Violation{requirementSourceViolation(taskPath, 1, err.Error())}, nil
	}
	var violations []Violation
	for _, source := range sources {
		message := requirementSourceProblem(root, source, present)
		if message != "" {
			violations = append(violations, requirementSourceViolation(taskPath, source.Line, message))
		}
	}
	return violations, nil
}

func requirementSourceProblem(root string, source taskcontract.RequirementSource, present map[string]bool) string {
	switch source.Kind {
	case taskcontract.RequirementSourceTask, taskcontract.RequirementSourceFile:
		return currentRequirementSourceProblem(root, source.Path, present)
	case taskcontract.RequirementSourceGit:
		if err := repositoryprojectcommit.VerifyRequirementSource(root, "", source); err != nil {
			return fmt.Sprintf("pinned requirement source %s:%s is unavailable: %v", source.Commit, source.Path, err)
		}
	}
	return ""
}

func currentRequirementSourceProblem(root, sourcePath string, present map[string]bool) string {
	if !present[sourcePath] {
		return fmt.Sprintf("current requirement source %s is missing; parent must transfer or fix the source", sourcePath)
	}
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(sourcePath)))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Sprintf("current requirement source %s is not a regular file", sourcePath)
	}
	return ""
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
