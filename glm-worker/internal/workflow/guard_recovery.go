package workflow

import (
	"errors"
	"fmt"
	"strings"

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

var captureCurrentGuardRecoveryRefDigest = runner.CaptureGitAuthorityRefDigest

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
	if err := w.captureStopRetention(&checkpoint); err != nil {
		return err
	}
	if err := w.state.EnterStop(checkpoint); err != nil {
		return err
	}
	w.recordGuardRecoverableCall(checkpoint, execution, outputPath)
	return w.guardRecoverableError(checkpoint)
}

func (w *Workflow) guardRecoveryCheckpoint(
	checkpoint state.ResumeCheckpoint,
	execution modelCallExecution,
) state.ResumeCheckpoint {
	checkpoint.SetStopKind(state.ResumeStopGuardRecoverable)
	checkpoint.GuardFailure = boundedText(execution.runErr.Error(), packet.MaxDiagnosticBytes)
	checkpoint.CompletedResult = w.completedGuardWorkerResult(checkpoint, execution.runResult)
	captureGuardRefEvidence(&checkpoint, execution.runErr)
	return checkpoint
}

func clearGuardRecoveryState(checkpoint *state.ResumeCheckpoint) {
	checkpoint.ClearStop()
}

func captureGuardRefEvidence(checkpoint *state.ResumeCheckpoint, runErr error) {
	var gitErr *runner.GitAuthorityGuardError
	if !errors.As(runErr, &gitErr) || gitErr.RefBeforeDigest == "" {
		return
	}
	checkpoint.GuardRefBeforeDigest = gitErr.RefBeforeDigest
	checkpoint.GuardRefAfterDigest = gitErr.RefAfterDigest
	checkpoint.GuardRefChangesTruncated = gitErr.RefChangesTruncated
	checkpoint.GuardRefChanges = make([]state.GuardRefChange, 0, len(gitErr.RefChanges))
	for _, change := range gitErr.RefChanges {
		converted := state.GuardRefChange{Name: change.Name}
		if change.Before != nil {
			converted.Before = &state.GuardRefState{Name: change.Before.Name, ObjectID: change.Before.ObjectID, Symref: change.Before.Symref}
		}
		if change.After != nil {
			converted.After = &state.GuardRefState{Name: change.After.Name, ObjectID: change.After.ObjectID, Symref: change.After.Symref}
		}
		checkpoint.GuardRefChanges = append(checkpoint.GuardRefChanges, converted)
	}
}

func (w *Workflow) captureStopRetention(checkpoint *state.ResumeCheckpoint) error {
	if err := w.attachStopRepositoryBoundary(checkpoint); err != nil {
		return err
	}
	files, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return err
	}
	checkpoint.StopDirtyFiles = files
	if checkpoint.StopKind == state.ResumeStopGuardRecoverable && checkpoint.GuardRefBeforeDigest != "" {
		digest, err := runner.CaptureGitAuthorityRefDigest(w.config.RepoRoot)
		if err != nil {
			return fmt.Errorf("capture guard stop refs: %w", err)
		}
		checkpoint.GuardRefStopDigest = digest
	}
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

func (w *Workflow) prepareStoppedResultReuse(checkpoint state.ResumeCheckpoint) (bool, error) {
	switch checkpoint.StopKind {
	case state.ResumeStopGuardRecoverable, state.ResumeStopQualityGate:
	default:
		return false, nil
	}
	if err := validateGuardRecoveryRetention(checkpoint); err != nil {
		return false, err
	}
	if err := w.verifyGuardRecoveryRefs(checkpoint); err != nil {
		return false, err
	}
	if err := w.verifyGuardRecoveryDirty(checkpoint); err != nil {
		return false, err
	}
	if err := w.verifyGuardRecoveryHead(checkpoint); err != nil {
		return false, err
	}
	reusable := w.guardRecoveryResultReusable(checkpoint)
	if checkpoint.StopKind == state.ResumeStopQualityGate && !reusable {
		return false, &WorkerError{Phase: checkpoint.Phase, Message: "quality-gate recovery checkpoint has no reusable worker result"}
	}
	return reusable, nil
}

func validateGuardRecoveryRetention(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.StopGitSnapshot != nil && checkpoint.StopGitSnapshot.Head != "" && checkpoint.StopDirtyFiles != nil {
		return nil
	}
	return &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery checkpoint has no repository retention baseline"}
}

func (w *Workflow) verifyGuardRecoveryRefs(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.GuardRefBeforeDigest == "" {
		return nil
	}
	if checkpoint.GuardRefAfterDigest == "" || len(checkpoint.GuardRefChanges) == 0 {
		return &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery ref evidence is incomplete"}
	}
	current, err := captureCurrentGuardRecoveryRefDigest(w.config.RepoRoot)
	if err != nil {
		failure := &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("guard recovery cannot capture current refs: %v", err)}
		if repairErr := w.requestGuardRepair(checkpoint, failure); repairErr != nil {
			return errors.Join(failure, fmt.Errorf("persist guard repair request: %w", repairErr))
		}
		return failure
	}
	if !checkpoint.GuardRefChangesTruncated && guardRefChangesOnlyVolatile(checkpoint.GuardRefChanges) {
		if checkpoint.GuardRefStopDigest == "" {
			return &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery legacy volatile ref evidence has no stop-time authority baseline"}
		}
		if current == checkpoint.GuardRefStopDigest {
			return nil
		}
		return &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery refs changed after stop"}
	}
	if current == checkpoint.GuardRefBeforeDigest {
		return nil
	}
	return &WorkerError{Phase: checkpoint.Phase, Message: "guard recovery refs are not restored to the pre-call state: " + describeGuardRefChanges(checkpoint.GuardRefChanges, checkpoint.GuardRefChangesTruncated)}
}

func guardRefChangesOnlyVolatile(changes []state.GuardRefChange) bool {
	for _, change := range changes {
		if !runner.IsVolatileCodexDesktopRef(change.Name) {
			return false
		}
	}
	return len(changes) > 0
}

func describeGuardRefChanges(changes []state.GuardRefChange, truncated bool) string {
	parts := make([]string, 0, len(changes)+1)
	for _, change := range changes {
		before := "<missing>"
		after := "<missing>"
		if change.Before != nil {
			before = change.Before.ObjectID
		}
		if change.After != nil {
			after = change.After.ObjectID
		}
		parts = append(parts, fmt.Sprintf("%s:%s->%s", change.Name, before, after))
	}
	if truncated {
		parts = append(parts, "...")
	}
	return strings.Join(parts, ",")
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
