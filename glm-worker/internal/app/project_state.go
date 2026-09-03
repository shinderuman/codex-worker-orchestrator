package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type projectStateOutput struct {
	Version      int                      `json:"version"`
	PlanPresent  bool                     `json:"plan_present"`
	Goal         *projectStateGoal        `json:"goal,omitempty"`
	Schedule     *projectStateSchedule    `json:"schedule,omitempty"`
	Dependencies []projectStateDependency `json:"dependencies"`
	NextRunnable *string                  `json:"next_runnable"`
	Blockers     []projectStateBlocker    `json:"blockers"`
	Completion   *projectStateCompletion  `json:"completion,omitempty"`
}

type projectStateGoal struct {
	Present bool   `json:"present"`
	Status  string `json:"status,omitempty"`
}

type projectStateSchedule struct {
	Active  []string `json:"active"`
	Next    []string `json:"next"`
	Blocked []string `json:"blocked"`
}

type projectStateDependency struct {
	Task        string   `json:"task"`
	Outstanding []string `json:"outstanding,omitempty"`
	Fulfilled   []string `json:"fulfilled,omitempty"`
}

type projectStateBlocker struct {
	Task        string   `json:"task"`
	Section     string   `json:"section"`
	Reason      string   `json:"reason"`
	Outstanding []string `json:"outstanding,omitempty"`
}

type projectStateCompletion struct {
	Ready          bool                      `json:"ready"`
	Unmet          []string                  `json:"unmet,omitempty"`
	ActiveTask     string                    `json:"active_task,omitempty"`
	TaskStatus     string                    `json:"task_status,omitempty"`
	RequiredAction string                    `json:"required_action,omitempty"`
	Validations    []parentHandoffValidation `json:"validations"`
	TreeClean      *bool                     `json:"tree_clean,omitempty"`
}

type projectTaskNode struct {
	path        string
	content     []byte
	outstanding []string
	fulfilled   []string
	visiting    bool
}

type projectStateGraph struct {
	repoRoot string
	nodes    []*projectTaskNode
	byPath   map[string]*projectTaskNode
}

const projectStateVersion = 1

func printProjectState(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	output, err := buildProjectState(cfg, st)
	if err != nil {
		return err
	}
	return writeJSON(stdout, output)
}

func executeStatelessProjection(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	if cmd.Mode == ModeProjectState {
		return printProjectState(cfg, state.AttachStateStore(cfg), stdout)
	}
	return printPacketCheck(cmd, stdout)
}

func buildProjectState(cfg config.AppConfig, st *state.StateStore) (projectStateOutput, error) {
	output := projectStateOutput{
		Version:      projectStateVersion,
		Dependencies: []projectStateDependency{},
		Blockers:     []projectStateBlocker{},
	}
	planContent, err := readProjectStatePlan(cfg.RepoRoot)
	if err != nil {
		return output, err
	}
	if planContent == nil {
		return output, nil
	}
	output.PlanPresent = true
	goal, err := taskcontract.ParsePlanGoal(*planContent)
	if err != nil {
		return output, err
	}
	schedule := taskcontract.ParsePlanSchedule(*planContent)
	active, next, blocked, err := projectStateEntries(goal, schedule)
	if err != nil {
		return output, err
	}
	graph, err := buildProjectStateGraph(cfg.RepoRoot, append(append(append([]string{}, active...), next...), blocked...))
	if err != nil {
		return output, err
	}
	output.Goal = &projectStateGoal{Present: goal.Present, Status: goal.Status}
	output.Schedule = &projectStateSchedule{Active: active, Next: next, Blocked: blocked}
	output.Dependencies = graph.dependencies()
	output.NextRunnable = projectStateNextRunnable(graph, next)
	output.Blockers = projectStateBlockers(graph, next, blocked)
	if goal.Present && goal.Status == taskcontract.GoalStatusActive {
		completion, err := buildProjectStateCompletion(cfg, st, active, next, blocked, graph)
		if err != nil {
			return output, err
		}
		output.Completion = completion
	}
	return output, nil
}

func readProjectStatePlan(repoRoot string) (*string, error) {
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

func projectStateEntries(goal taskcontract.PlanGoal, schedule taskcontract.PlanSchedule) ([]string, []string, []string, error) {
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return nil, nil, nil, err
	}
	if goal.Present && goal.Status == taskcontract.GoalStatusCompleted {
		active, err := schedule.ActiveEntries()
		if err != nil {
			return nil, nil, nil, err
		}
		if len(active) > 0 || len(next) > 0 || len(blocked) > 0 {
			return nil, nil, nil, fmt.Errorf("completed GOALではACTIVE/NEXT/BLOCKEDを空にする必要があります(active=%d next=%d blocked=%d)", len(active), len(next), len(blocked))
		}
		return []string{}, []string{}, []string{}, nil
	}
	activeTask, err := schedule.ValidateComplete()
	if err != nil {
		return nil, nil, nil, err
	}
	return []string{activeTask}, next, blocked, nil
}

func buildProjectStateGraph(repoRoot string, entries []string) (*projectStateGraph, error) {
	graph := &projectStateGraph{repoRoot: repoRoot, byPath: map[string]*projectTaskNode{}}
	for _, entry := range entries {
		if _, err := graph.loadTask(entry); err != nil {
			return nil, err
		}
	}
	return graph, nil
}

func (g *projectStateGraph) loadTask(path string) (*projectTaskNode, error) {
	if node, ok := g.byPath[path]; ok {
		if node.visiting {
			return nil, fmt.Errorf("task dependency cycleを検出しました: %s", path)
		}
		return node, nil
	}
	content, err := readProjectStateTask(g.repoRoot, path)
	if err != nil {
		return nil, err
	}
	node := &projectTaskNode{path: path, content: content, visiting: true}
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
		if !projectStateTaskExists(g.repoRoot, dependency) {
			return nil, fmt.Errorf("task %sのdependency %sはcurrent treeに存在せず、%sにも明示されていません", path, dependency, taskcontract.TaskFulfilledDependenciesHeading)
		}
		if _, err := g.loadTask(dependency); err != nil {
			return nil, err
		}
		node.outstanding = append(node.outstanding, dependency)
	}
	node.visiting = false
	return node, nil
}

func readProjectStateTask(repoRoot, path string) ([]byte, error) {
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

func projectStateTaskExists(repoRoot, path string) bool {
	info, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(path)))
	return err == nil && info.Mode().IsRegular()
}

func (g *projectStateGraph) dependencies() []projectStateDependency {
	dependencies := make([]projectStateDependency, 0, len(g.nodes))
	for _, node := range g.nodes {
		dependencies = append(dependencies, projectStateDependency{
			Task:        node.path,
			Outstanding: node.outstanding,
			Fulfilled:   node.fulfilled,
		})
	}
	return dependencies
}

func projectStateNextRunnable(graph *projectStateGraph, next []string) *string {
	for _, entry := range next {
		node := graph.byPath[entry]
		if node != nil && len(node.outstanding) == 0 {
			runnable := entry
			return &runnable
		}
	}
	return nil
}

func projectStateBlockers(graph *projectStateGraph, next, blocked []string) []projectStateBlocker {
	blockers := []projectStateBlocker{}
	for _, entry := range next {
		node := graph.byPath[entry]
		if node == nil || len(node.outstanding) == 0 {
			continue
		}
		blockers = append(blockers, projectStateBlocker{
			Task: entry, Section: "next", Reason: "outstanding-dependencies", Outstanding: node.outstanding,
		})
	}
	for _, entry := range blocked {
		node := graph.byPath[entry]
		outstanding := []string(nil)
		if node != nil {
			outstanding = node.outstanding
		}
		blockers = append(blockers, projectStateBlocker{
			Task: entry, Section: "blocked", Reason: "blocked-section", Outstanding: outstanding,
		})
	}
	return blockers
}

func buildProjectStateCompletion(cfg config.AppConfig, st *state.StateStore, active, next, blocked []string, graph *projectStateGraph) (*projectStateCompletion, error) {
	if len(active) != 1 {
		return nil, fmt.Errorf("Goal進行中のcompletion評価には単一ACTIVE taskが必要です(active=%d)", len(active))
	}
	activeTask := active[0]
	node := graph.byPath[activeTask]
	if node == nil {
		return nil, fmt.Errorf("ACTIVE task %sのdependency状態を解決できません", activeTask)
	}
	findings, err := taskcontract.ParseReviewFindings(node.content)
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", activeTask, err)
	}
	completion := &projectStateCompletion{Ready: false, ActiveTask: activeTask, Validations: []parentHandoffValidation{}}
	unmet := append([]string{}, projectStateScheduleUnmet(next, blocked, findings)...)
	unmet = append(unmet, projectStateLifecycleUnmet(st, activeTask, completion)...)
	unmet = append(unmet, projectStateEvidenceUnmet(cfg, st, completion)...)
	completion.Unmet = unmet
	completion.Ready = len(unmet) == 0
	return completion, nil
}

func projectStateScheduleUnmet(next, blocked []string, findings taskcontract.ReviewFindings) []string {
	unmet := []string{}
	if len(next) > 0 || len(blocked) > 0 {
		unmet = append(unmet, "schedule_not_empty")
	}
	if !findings.None {
		unmet = append(unmet, "open_findings")
	}
	return unmet
}

func projectStateLifecycleUnmet(st *state.StateStore, activeTask string, completion *projectStateCompletion) []string {
	unmet := []string{}
	completion.TaskStatus = string(st.TaskStatus())
	if st.TaskStatus() != state.TaskStatusComplete {
		unmet = append(unmet, "task_not_complete")
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil {
		return append(unmet, "lifecycle_inconsistent")
	}
	completion.RequiredAction = string(plan.RequiredAction)
	if plan.RequiredAction != state.ParentActionNone {
		unmet = append(unmet, "pending_parent_action")
	}
	if pinned := st.ReadOr("active-task", ""); pinned != activeTask {
		unmet = append(unmet, "active_task_mismatch")
	}
	return unmet
}

func projectStateEvidenceUnmet(cfg config.AppConfig, st *state.StateStore, completion *projectStateCompletion) []string {
	unmet := []string{}
	snapshot, snapshotErr := state.CaptureGitSnapshot(cfg.RepoRoot)
	if snapshotErr != nil {
		unmet = append(unmet, "snapshot_unavailable")
	} else {
		completion.Validations = currentParentValidations(st, cfg.RepoRoot, &state.SnapshotDigest{
			Head:           snapshot.Head,
			IndexDigest:    snapshot.IndexDigest,
			WorktreeDigest: snapshot.WorktreeDigest,
		})
		if !projectStateValidationPass(completion.Validations) {
			unmet = append(unmet, "validation_not_current")
		}
	}
	clean, cleanErr := projectStateTreeClean(cfg.RepoRoot)
	if cleanErr != nil {
		return append(unmet, "tree_status_unavailable")
	}
	completion.TreeClean = &clean
	if !clean {
		unmet = append(unmet, "tree_not_clean")
	}
	return unmet
}

func projectStateValidationPass(validations []parentHandoffValidation) bool {
	for _, validation := range validations {
		if validation.Status == qualityGateStatusPass {
			return true
		}
	}
	return false
}

func projectStateTreeClean(repoRoot string) (bool, error) {
	command := exec.Command("git", "-C", repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(output)) == "", nil
}
