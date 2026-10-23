package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type TaskAuthoritySyncEvidence struct {
	Runtime string                          `json:"runtime"`
	TaskID  string                          `json:"task_id,omitempty"`
	Binding *state.ControllerRuntimeBinding `json:"binding,omitempty"`
}

const (
	TaskAuthorityRuntimeAttached  = "attached"
	TaskAuthorityRuntimeUnstarted = "unstarted"
)

func (w *Workflow) CaptureTaskAuthoritySyncEvidence() ([]byte, error) {
	if !w.state.Exists("task.id") {
		for _, name := range []string{"task.status", "worker.id", "reviewer.id", "parent-review-state.json", "resume-checkpoint.json", state.ExecutionMilestonesStateFile, "controller-runtime-binding.json"} {
			if _, err := os.Lstat(w.state.Path(name)); err == nil {
				return nil, fmt.Errorf("task identity is missing while retained runtime state exists: %s", name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		return json.Marshal(TaskAuthoritySyncEvidence{Runtime: TaskAuthorityRuntimeUnstarted})
	}
	binding, err := w.state.LoadControllerRuntimeBinding()
	if err != nil {
		return nil, fmt.Errorf("task authority sync requires an intact runtime binding: %w", err)
	}
	status := w.state.TaskStatus()
	if !executionmilestone.RevisionStatusAllowed(status) {
		return nil, fmt.Errorf("task authority sync requires an admitted stopped runtime boundary, got %q", status)
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return nil, err
	}
	if err := w.verifyTaskAuthoritySyncMilestoneSource(taskID, binding); err != nil {
		return nil, err
	}

	return json.Marshal(TaskAuthoritySyncEvidence{Runtime: TaskAuthorityRuntimeAttached, TaskID: taskID, Binding: &binding})
}

func (w *Workflow) CompleteTaskAuthoritySync(data []byte) error {
	var source TaskAuthoritySyncEvidence
	if err := json.Unmarshal(data, &source); err != nil {
		return err
	}
	if source.Runtime == TaskAuthorityRuntimeUnstarted {
		return nil
	}
	if source.Runtime != TaskAuthorityRuntimeAttached || source.Binding == nil {
		return fmt.Errorf("task authority sync evidence is incomplete")
	}
	binding, target, err := w.taskAuthorityCompletionBinding(source)
	if err != nil {
		return err
	}
	if err := executionmilestone.SyncTaskContract(w.state, source.TaskID, target.TaskPath, source.Binding.TaskContractDigest, target.TaskContractDigest, w.now().UTC()); err != nil {
		return err
	}
	if binding != target {
		if err := w.state.SaveControllerRuntimeBinding(target); err != nil {
			return err
		}
	}
	content, err := os.ReadFile(filepath.Join(w.config.RepoRoot, filepath.FromSlash(target.TaskPath)))
	if err != nil {
		return err
	}
	if err := w.state.SaveCurrentTaskAuthority(target.TaskPath, content); err != nil {
		return err
	}
	return w.refreshRetainedTaskAuthorityBoundary()
}

func (w *Workflow) refreshRetainedTaskAuthorityBoundary() error {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) {
		return nil
	}
	if err != nil {
		return err
	}
	target := checkpoint
	if err := w.attachStopRepositoryBoundary(&target); err != nil {
		return err
	}
	dirty, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return err
	}
	target.StopDirtyFiles = dirty
	if reflect.DeepEqual(target, checkpoint) {
		return nil
	}
	return w.state.SaveResumeCheckpoint(target)
}

func (w *Workflow) taskAuthorityCompletionBinding(source TaskAuthoritySyncEvidence) (state.ControllerRuntimeBinding, state.ControllerRuntimeBinding, error) {
	binding, err := w.state.LoadControllerRuntimeBinding()
	if err != nil {
		return state.ControllerRuntimeBinding{}, state.ControllerRuntimeBinding{}, err
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return state.ControllerRuntimeBinding{}, state.ControllerRuntimeBinding{}, err
	}
	if source.TaskID != taskID {
		return state.ControllerRuntimeBinding{}, state.ControllerRuntimeBinding{}, fmt.Errorf("task authority sync changed Task or Attempt binding")
	}
	target, err := w.revisedTaskAuthorityBinding(source)
	if err != nil {
		return state.ControllerRuntimeBinding{}, state.ControllerRuntimeBinding{}, err
	}

	if binding.AttemptID != source.Binding.AttemptID || binding.TaskPath != source.Binding.TaskPath ||
		(binding.TaskContractDigest != source.Binding.TaskContractDigest && binding != target) {
		return state.ControllerRuntimeBinding{}, state.ControllerRuntimeBinding{}, fmt.Errorf("task authority sync runtime binding is outside its recovery lineage: %#v", binding)
	}
	return binding, target, nil
}

func (w *Workflow) revisedTaskAuthorityBinding(source TaskAuthoritySyncEvidence) (state.ControllerRuntimeBinding, error) {
	exists, err := controller.Exists(w.config)
	if err != nil {
		return state.ControllerRuntimeBinding{}, err
	}
	if !exists {
		return state.ControllerRuntimeBinding{}, fmt.Errorf("task authority sync lacks its controller authority")
	}
	store, err := controller.Open(w.config)
	if err != nil {
		return state.ControllerRuntimeBinding{}, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return state.ControllerRuntimeBinding{}, err
	}
	task, err := store.ExecutionTaskForWorkspace(w.config.RepoRoot)
	if err != nil {
		return state.ControllerRuntimeBinding{}, err
	}
	digest, err := executionmilestone.TaskContractDigest(w.config.RepoRoot, task.TaskPath)
	if err != nil {
		return state.ControllerRuntimeBinding{}, err
	}
	if digest != task.ContractDigest {
		return state.ControllerRuntimeBinding{}, fmt.Errorf("revised Task contract digest differs from its file")
	}
	if head.LiveAttemptID != source.Binding.AttemptID || task.TaskPath != source.Binding.TaskPath {
		return state.ControllerRuntimeBinding{}, fmt.Errorf("task authority sync changed runtime Task or attempt")
	}
	return state.ControllerRuntimeBinding{AttemptID: source.Binding.AttemptID, TaskPath: task.TaskPath, TaskContractDigest: digest}, nil
}

func (w *Workflow) verifyTaskAuthoritySyncMilestoneSource(taskID string, binding state.ControllerRuntimeBinding) error {
	plan, err := executionmilestone.Load(w.state)
	if err != nil {
		return err
	}
	if plan != nil && (plan.TaskID != taskID || plan.ActiveTaskPath != binding.TaskPath || plan.TaskContractSHA256 != binding.TaskContractDigest) {
		return fmt.Errorf("execution milestone authority differs from the task sync source binding")
	}
	return nil
}
