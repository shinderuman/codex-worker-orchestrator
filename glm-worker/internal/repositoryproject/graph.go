package repositoryproject

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type Dependency struct {
	Task        string   `json:"task"`
	Outstanding []string `json:"outstanding,omitempty"`
	Fulfilled   []string `json:"fulfilled,omitempty"`
}

type Blocker struct {
	Task        string   `json:"task"`
	Section     string   `json:"section"`
	Reason      string   `json:"reason"`
	Outstanding []string `json:"outstanding,omitempty"`
}

type taskNode struct {
	path        string
	content     []byte
	outstanding []string
	fulfilled   []string
	visiting    bool
}

type TaskGraph struct {
	nodes  []*taskNode
	byPath map[string]*taskNode
}

func BuildTaskGraph(entries []string, contents map[string][]byte) (*TaskGraph, error) {
	graph := &TaskGraph{byPath: map[string]*taskNode{}}
	for _, entry := range entries {
		if _, err := graph.loadTask(entry, contents); err != nil {
			return nil, err
		}
	}
	return graph, nil
}

func (g *TaskGraph) loadTask(path string, contents map[string][]byte) (*taskNode, error) {
	if node, ok := g.byPath[path]; ok {
		if node.visiting {
			return nil, fmt.Errorf("task dependency cycleを検出しました: %s", path)
		}
		return node, nil
	}
	content, ok := contents[path]
	if !ok {
		return nil, fmt.Errorf("task file %sの内容を解決できません", path)
	}
	node := &taskNode{path: path, content: append([]byte(nil), content...), visiting: true}
	g.byPath[path] = node
	g.nodes = append(g.nodes, node)
	dependencies, err := taskcontract.ParseTaskDependencyState(content)
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", path, err)
	}
	for _, dependency := range dependencies.Fulfilled {
		if dependency == path {
			return nil, fmt.Errorf("task %sは自身へのfulfilled dependencyを持っています", path)
		}
		node.fulfilled = append(node.fulfilled, dependency)
	}
	for _, dependency := range dependencies.Outstanding {
		if dependency == path {
			return nil, fmt.Errorf("task %sは自身へのdependencyを持っています", path)
		}
		if _, ok := contents[dependency]; !ok {
			return nil, fmt.Errorf(
				"task %sのdependency %sはcurrent treeに存在せず、%sにも明示されていません",
				path, dependency, taskcontract.TaskFulfilledDependenciesHeading,
			)
		}
		if _, err := g.loadTask(dependency, contents); err != nil {
			return nil, err
		}
		node.outstanding = append(node.outstanding, dependency)
	}
	node.visiting = false
	return node, nil
}

func (g *TaskGraph) Dependencies() []Dependency {
	dependencies := make([]Dependency, 0, len(g.nodes))
	for _, node := range g.nodes {
		dependencies = append(dependencies, Dependency{
			Task:        node.path,
			Outstanding: append([]string(nil), node.outstanding...),
			Fulfilled:   append([]string(nil), node.fulfilled...),
		})
	}
	return dependencies
}

func (g *TaskGraph) NextRunnable(next []string) *string {
	for _, entry := range next {
		node := g.byPath[entry]
		if node != nil && len(node.outstanding) == 0 {
			runnable := entry
			return &runnable
		}
	}
	return nil
}

func (g *TaskGraph) Blockers(next, blocked []string) []Blocker {
	blockers := []Blocker{}
	for _, entry := range next {
		node := g.byPath[entry]
		if node == nil || len(node.outstanding) == 0 {
			continue
		}
		blockers = append(blockers, Blocker{
			Task:        entry,
			Section:     "next",
			Reason:      "outstanding-dependencies",
			Outstanding: append([]string(nil), node.outstanding...),
		})
	}
	for _, entry := range blocked {
		node := g.byPath[entry]
		outstanding := []string(nil)
		if node != nil {
			outstanding = append([]string(nil), node.outstanding...)
		}
		blockers = append(blockers, Blocker{
			Task: entry, Section: "blocked", Reason: "blocked-section", Outstanding: outstanding,
		})
	}
	return blockers
}

func (g *TaskGraph) TaskContent(path string) ([]byte, bool) {
	node := g.byPath[path]
	if node == nil {
		return nil, false
	}
	return append([]byte(nil), node.content...), true
}

func CompletionScheduleUnmet(next, blocked []string, activeTask string, content []byte) ([]string, error) {
	findings, err := taskcontract.ParseReviewFindings(content)
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", activeTask, err)
	}
	unmet := []string{}
	if len(next) > 0 || len(blocked) > 0 {
		unmet = append(unmet, "schedule_not_empty")
	}
	if !findings.None {
		unmet = append(unmet, "open_findings")
	}
	return unmet, nil
}
