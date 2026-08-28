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

	if err := w.state.SaveResumeCheckpoint(checkpoint); err != nil {
		return err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusGuardRecoverable); err != nil {
		return err
	}

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
	taskID, err := w.state.TaskID()
	if err != nil {
		return nil
	}
	if err := packet.ValidateArtifacts(result.Artifacts, w.state.ArtifactDir(taskID)); err != nil {
		return nil
	}
	return &result
}

func (w *Workflow) prepareGuardRecovery(checkpoint state.ResumeCheckpoint) (bool, error) {
	if !checkpoint.GuardRecoverable {
		return false, nil
	}
	stop := checkpoint.StopGitSnapshot
	if stop == nil || stop.Head == "" || checkpoint.StopDirtyFiles == nil {
		return false, &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery checkpoint has no repository retention baseline"}
	}

	currentFiles, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return false, &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("guard recovery cannot enumerate current dirty files: %v", err)}
	}
	if diff := state.DescribeStopDirtyDiff(checkpoint.StopDirtyFiles, currentFiles); diff != "" {
		return false, &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery dirty work changed after stop: " + diff}
	}

	current, err := state.CaptureGitSnapshot(w.config.RepoRoot)
	if err != nil {
		return false, &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("guard recovery cannot capture current repository snapshot: %v", err)}
	}
	reuseResult := current.Head == stop.Head
	if current.Head != stop.Head {
		if err := verifyHeadAncestry(w.config.RepoRoot, stop.Head, current.Head); err != nil {
			return false, &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery HEAD no longer descends from the stopped task HEAD"}
		}
		nonParent, err := headDeltaNonParentPaths(w.config.RepoRoot, stop.Head, current.Head)
		if err != nil {
			return false, &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("guard recovery cannot classify parent repair delta: %v", err)}
		}
		reuseResult = len(nonParent) == 0
	}

	if !reuseResult || checkpoint.Stage != state.ResumeStageWorker || checkpoint.CompletedResult == nil {
		return false, nil
	}
	if err := w.validateCompletedGuardResult(checkpoint, *checkpoint.CompletedResult); err != nil {
		return false, nil
	}
	return true, nil
}

func (w *Workflow) validateCompletedGuardResult(checkpoint state.ResumeCheckpoint, result packet.Result) error {
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
