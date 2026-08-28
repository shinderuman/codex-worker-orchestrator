package workflow

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type GuardRecoverableError struct {
	Phase       string
	Failure     string
	TaskID      string
	RepoRoot    string
	ResultSaved bool
}

func (e *GuardRecoverableError) Error() string {
	return fmt.Sprintf("guard failure stopped task at %s; parent repair is required before --resume: %s", e.Phase, e.Failure)
}

func (w *Workflow) saveGuardRecoverableState(
	checkpoint state.ResumeCheckpoint,
	execution modelCallExecution,
	outputPath string,
) error {
	if err := w.state.SecureArtifactDir(); err != nil {
		w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return err
	}

	checkpoint = w.guardRecoveryCheckpoint(checkpoint, execution)
	if err := w.captureGuardRecoveryRetention(&checkpoint); err != nil {
		return err
	}
	if err := w.state.SaveResumeCheckpoint(checkpoint); err != nil {
		return err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusGuardRecoverable); err != nil {
		return err
	}
	w.recordGuardRecoverableCall(checkpoint, execution, outputPath)
	return w.guardRecoverableError(checkpoint)
}

func (w *Workflow) guardRecoveryCheckpoint(
	checkpoint state.ResumeCheckpoint,
	execution modelCallExecution,
) state.ResumeCheckpoint {
	checkpoint.GuardRecoverable = true
	checkpoint.GuardFailure = boundedText(execution.runErr.Error(), packet.MaxDiagnosticBytes)
	checkpoint.RateLimited = false
	checkpoint.ResetAtCST = ""
	checkpoint.ResetAtRFC3339 = ""
	checkpoint.ProviderUnavailable = false
	checkpoint.ProviderUnavailableClassification = ""
	checkpoint.ProviderUnavailableProbes = 0
	checkpoint.ProviderUnavailableStartedAt = time.Time{}
	checkpoint.UserInterrupted = false
	checkpoint.CompletedResult = w.completedGuardWorkerResult(checkpoint, execution.runResult)
	checkpoint.StopParentFiles = captureStopParentFiles(w.config.RepoRoot)
	return checkpoint
}

func (w *Workflow) captureGuardRecoveryRetention(checkpoint *state.ResumeCheckpoint) error {
	snapshot, err := state.CaptureGitSnapshot(w.config.RepoRoot)
	if err != nil {
		return err
	}
	files, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return err
	}
	checkpoint.StopGitSnapshot = &snapshot
	checkpoint.StopDirtyFiles = files
	return nil
}

func (w *Workflow) recordGuardRecoverableCall(
	checkpoint state.ResumeCheckpoint,
	execution modelCallExecution,
	outputPath string,
) {
	packetStatus := ""
	if checkpoint.CompletedResult != nil {
		packetStatus = string(checkpoint.CompletedResult.Status)
	}
	w.recordModelCall(
		checkpoint,
		execution.runResult,
		execution.startedAt,
		execution.completedAt,
		"guard_recoverable",
		packetStatus,
		execution.runErr,
		outputPath,
		callDiagnostics{},
	)
}

func (w *Workflow) guardRecoverableError(checkpoint state.ResumeCheckpoint) error {
	taskID, _ := w.state.TaskID()
	return &GuardRecoverableError{
		Phase:       checkpoint.Phase,
		Failure:     checkpoint.GuardFailure,
		TaskID:      taskID,
		RepoRoot:    w.config.RepoRoot,
		ResultSaved: checkpoint.CompletedResult != nil,
	}
}

func (w *Workflow) completedGuardWorkerResult(checkpoint state.ResumeCheckpoint, runResult runner.RunResult) *packet.Result {
	if checkpoint.Stage != state.ResumeStageWorker || checkpoint.Role != state.WorkerRole {
		return nil
	}
	result, err := w.parseModelCallResult(checkpoint, runResult)
	if err != nil {
		return nil
	}
	if err := w.validateCompletedGuardResult(result); err != nil {
		return nil
	}
	return &result
}

func (w *Workflow) prepareGuardRecovery(checkpoint state.ResumeCheckpoint) (bool, error) {
	if !checkpoint.GuardRecoverable {
		return false, nil
	}
	if err := validateGuardRecoveryRetention(checkpoint); err != nil {
		return false, err
	}
	if err := w.verifyGuardRecoveryDirty(checkpoint); err != nil {
		return false, err
	}
	if err := w.verifyGuardRecoveryHead(checkpoint); err != nil {
		return false, err
	}
	return w.guardRecoveryResultReusable(checkpoint), nil
}

func validateGuardRecoveryRetention(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.StopGitSnapshot != nil && checkpoint.StopGitSnapshot.Head != "" && checkpoint.StopDirtyFiles != nil {
		return nil
	}
	return &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery checkpoint has no repository retention baseline"}
}

func (w *Workflow) verifyGuardRecoveryDirty(checkpoint state.ResumeCheckpoint) error {
	currentFiles, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("guard recovery cannot enumerate current dirty files: %v", err)}
	}
	if diff := state.DescribeStopDirtyDiff(checkpoint.StopDirtyFiles, currentFiles); diff != "" {
		return &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery dirty work changed after stop: " + diff}
	}
	return nil
}

func (w *Workflow) verifyGuardRecoveryHead(checkpoint state.ResumeCheckpoint) error {
	current, err := state.CaptureGitSnapshot(w.config.RepoRoot)
	if err != nil {
		return &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("guard recovery cannot capture current repository snapshot: %v", err)}
	}
	if current.Head == checkpoint.StopGitSnapshot.Head {
		return nil
	}
	if err := verifyHeadAncestry(w.config.RepoRoot, checkpoint.StopGitSnapshot.Head, current.Head); err != nil {
		return &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery HEAD no longer descends from the stopped task HEAD"}
	}
	return nil
}

func (w *Workflow) guardRecoveryResultReusable(checkpoint state.ResumeCheckpoint) bool {
	if checkpoint.Stage != state.ResumeStageWorker || checkpoint.CompletedResult == nil {
		return false
	}
	return w.validateCompletedGuardResult(*checkpoint.CompletedResult) == nil
}

func (w *Workflow) validateCompletedGuardResult(result packet.Result) error {
	if err := packet.ValidateWorkerResult(result); err != nil {
		return err
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return err
	}
	return packet.ValidateArtifacts(result.Artifacts, w.state.ArtifactDir(taskID))
}

func clearGuardRecoveryState(checkpoint *state.ResumeCheckpoint) {
	checkpoint.GuardRecoverable = false
	checkpoint.GuardFailure = ""
	checkpoint.CompletedResult = nil
	checkpoint.StopGitSnapshot = nil
	checkpoint.StopDirtyFiles = nil
}
