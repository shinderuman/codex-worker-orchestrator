package repositoryprojecttree

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type ProjectState struct {
	PlanPresent bool
	Plan        repositoryproject.ProjectStatePlan
	Graph       *repositoryproject.TaskGraph
}

func LoadProjectState(repoRoot string) (ProjectState, error) {
	planContent, err := ReadPlan(repoRoot)
	if err != nil {
		return ProjectState{}, err
	}
	if planContent == nil {
		return ProjectState{}, nil
	}
	prepared, err := repositoryproject.PrepareProjectState(*planContent)
	if err != nil {
		return ProjectState{}, err
	}
	if err := validateClosure(repoRoot, prepared.Schedule, "scheduleとIMPLEMENTATION_TASKS corpusのclosureが成立しません"); err != nil {
		return ProjectState{}, err
	}
	graph, err := loadGraph(repoRoot, projectStateEntries(prepared))
	if err != nil {
		return ProjectState{}, err
	}
	return ProjectState{PlanPresent: true, Plan: prepared, Graph: graph}, nil
}

func BuildParentRequestCompletionProjection(repoRoot string) (repositoryproject.ParentRequestCompletionProjection, error) {
	planContent, err := ReadPlan(repoRoot)
	if err != nil {
		return repositoryproject.ParentRequestCompletionProjection{}, err
	}
	if planContent == nil {
		return repositoryproject.ParentRequestCompletionProjection{
			Continuation: repositoryproject.UnknownContinuation(repositoryproject.ReasonPlanAbsent),
		}, nil
	}
	prepared, err := repositoryproject.PreparePostCompletion(*planContent)
	if err != nil {
		return repositoryproject.ParentRequestCompletionProjection{}, err
	}
	if prepared.Kind == repositoryproject.PostCompletionUnbound {
		return repositoryproject.PostCompletionProjection(prepared, nil), nil
	}
	if err := validateClosure(repoRoot, prepared.Schedule, "scheduleとIMPLEMENTATION_TASKS corpusのclosureが成立しません"); err != nil {
		return repositoryproject.ParentRequestCompletionProjection{}, err
	}
	if prepared.Kind == repositoryproject.PostCompletionTerminal {
		return repositoryproject.PostCompletionProjection(prepared, nil), nil
	}
	graph, err := loadGraph(repoRoot, prepared.Entries())
	if err != nil {
		return repositoryproject.ParentRequestCompletionProjection{}, err
	}
	return repositoryproject.PostCompletionProjection(prepared, graph), nil
}

func ReadPlan(repoRoot string) (*string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, state.ParentPlanFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", state.ParentPlanFile, err)
	}
	plan := string(data)
	return &plan, nil
}

func validateClosure(repoRoot string, schedule taskcontract.PlanSchedule, prefix string) error {
	entries, err := taskcontract.EnumerateTaskCorpus(repoRoot)
	if err != nil {
		return err
	}
	return repositoryproject.ValidateClosure(schedule, entries, prefix)
}

func loadGraph(repoRoot string, entries []string) (*repositoryproject.TaskGraph, error) {
	contents := make(map[string][]byte, len(entries))
	for _, path := range entries {
		if _, ok := contents[path]; ok {
			continue
		}
		content, err := readTask(repoRoot, path)
		if err != nil {
			return nil, err
		}
		contents[path] = content
	}
	return repositoryproject.BuildTaskGraph(entries, contents)
}

func readTask(repoRoot, path string) ([]byte, error) {
	if err := taskcontract.ValidateActiveTaskPath(path); err != nil {
		return nil, err
	}
	target := filepath.Join(repoRoot, filepath.FromSlash(path))
	info, err := os.Lstat(target)
	if err != nil {
		return nil, fmt.Errorf("task file %sを確認できません: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("task file %sはregular fileではありません(%s)", path, info.Mode().Type())
	}
	content, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("read task file %s: %w", path, err)
	}
	return content, nil
}

func projectStateEntries(prepared repositoryproject.ProjectStatePlan) []string {
	entries := append([]string{}, prepared.Active...)
	entries = append(entries, prepared.Next...)
	return append(entries, prepared.Blocked...)
}
