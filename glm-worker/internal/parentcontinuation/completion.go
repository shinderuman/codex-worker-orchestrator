package parentcontinuation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type CompletionEvidence struct {
	View           repositoryproject.CompletionView
	ActiveTask     string
	TaskStatus     state.TaskStatus
	RequiredAction state.ParentAction
	Validations    []qualitygate.RunRecord
	TreeClean      *bool
}

func BuildCompletionEvidence(repoRoot string, st *state.StateStore, loaded repositoryprojecttree.ProjectState) (*CompletionEvidence, error) {
	if !loaded.Plan.Goal.Present || loaded.Plan.Goal.Status != taskcontract.GoalStatusActive {
		return nil, nil
	}
	if len(loaded.Plan.Active) != 1 {
		return nil, fmt.Errorf("Goal進行中のcompletion評価には単一ACTIVE taskが必要です(active=%d)", len(loaded.Plan.Active))
	}
	activeTask := loaded.Plan.Active[0]
	content, ok := loaded.Graph.TaskContent(activeTask)
	if !ok {
		return nil, fmt.Errorf("ACTIVE task %sのdependency状態を解決できません", activeTask)
	}
	unmet, err := repositoryproject.CompletionScheduleUnmet(loaded.Plan.Next, loaded.Plan.Blocked, activeTask, content)
	if err != nil {
		return nil, err
	}
	evidence := &CompletionEvidence{
		ActiveTask:  activeTask,
		TaskStatus:  st.TaskStatus(),
		Validations: []qualitygate.RunRecord{},
	}
	unmet = append(unmet, completionLifecycleUnmet(st, activeTask, evidence)...)
	unmet = append(unmet, completionEvidenceUnmet(repoRoot, st, evidence)...)
	evidence.View = repositoryproject.CompletionView{Ready: len(unmet) == 0, Unmet: unmet}
	return evidence, nil
}

func continuationCompletionView(repoRoot string, st *state.StateStore, loaded repositoryprojecttree.ProjectState) (*repositoryproject.CompletionView, error) {
	evidence, err := BuildCompletionEvidence(repoRoot, st, loaded)
	if err != nil || evidence == nil {
		return nil, err
	}
	view := evidence.View
	return &view, nil
}

func completionLifecycleUnmet(st *state.StateStore, activeTask string, evidence *CompletionEvidence) []string {
	unmet := []string{}
	if evidence.TaskStatus != state.TaskStatusComplete {
		unmet = append(unmet, "task_not_complete")
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil {
		return append(unmet, "lifecycle_inconsistent")
	}
	evidence.RequiredAction = plan.RequiredAction
	if plan.RequiredAction != state.ParentActionNone {
		unmet = append(unmet, "pending_parent_action")
	}
	if pinned := st.ReadOr("active-task", ""); pinned != activeTask {
		unmet = append(unmet, "active_task_mismatch")
	}
	return unmet
}

func completionEvidenceUnmet(repoRoot string, st *state.StateStore, evidence *CompletionEvidence) []string {
	unmet := []string{}
	snapshot, snapshotErr := state.CaptureGitSnapshot(repoRoot)
	if snapshotErr != nil {
		unmet = append(unmet, "snapshot_unavailable")
	} else {
		evidence.Validations = continuationValidationRuns(st, repoRoot, snapshot)
		if !continuationValidationPass(evidence.Validations) {
			unmet = append(unmet, "validation_not_current")
		}
	}
	clean, cleanErr := continuationTreeClean(repoRoot)
	if cleanErr != nil {
		return append(unmet, "tree_status_unavailable")
	}
	evidence.TreeClean = &clean
	if !clean {
		unmet = append(unmet, "tree_not_clean")
	}
	return unmet
}

func continuationValidationPass(validations []qualitygate.RunRecord) bool {
	for _, record := range validations {
		if record.Status == qualitygate.StatusPass {
			return true
		}
	}
	return false
}

func continuationValidationRuns(st *state.StateStore, repoRoot string, snapshot state.GitSnapshot) []qualitygate.RunRecord {
	latestByForm := latestContinuationValidationRuns(st, repoRoot, snapshot)
	forms := make([]string, 0, len(latestByForm))
	for form := range latestByForm {
		forms = append(forms, form)
	}
	sort.Strings(forms)
	validations := make([]qualitygate.RunRecord, 0, len(forms))
	for _, form := range forms {
		validations = append(validations, latestByForm[form])
	}
	return validations
}

func latestContinuationValidationRuns(st *state.StateStore, repoRoot string, snapshot state.GitSnapshot) map[string]qualitygate.RunRecord {
	latestByForm := map[string]qualitygate.RunRecord{}
	entries, err := os.ReadDir(st.Path(qualitygate.RunDirectory))
	if err != nil {
		return latestByForm
	}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := qualitygate.Read(st, entry.Name())
		if err != nil || !continuationValidationMatches(record, repoRoot, snapshot) {
			continue
		}
		previous, found := latestByForm[record.Form]
		if !found || previous.StartedAt.Before(record.StartedAt) {
			latestByForm[record.Form] = record
		}
	}
	return latestByForm
}

func continuationValidationMatches(record qualitygate.RunRecord, repoRoot string, snapshot state.GitSnapshot) bool {
	return filepath.Clean(record.Repository) == filepath.Clean(repoRoot) &&
		record.Head == snapshot.Head &&
		record.IndexDigest == snapshot.IndexDigest &&
		record.WorktreeDigest == snapshot.WorktreeDigest
}

func continuationTreeClean(repoRoot string) (bool, error) {
	command := exec.Command("git", "-C", repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(output)) == "", nil
}
