package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type QualityGateRecoverableError struct {
	Phase       string
	Failure     string
	TaskID      string
	RepoRoot    string
	ResultSaved bool
}

func (e *QualityGateRecoverableError) Error() string {
	return fmt.Sprintf(
		"quality gate stopped the completed worker result at %s; repair the reported gate failure before resuming the same task: %s",
		e.Phase, e.Failure,
	)
}

func (w *Workflow) saveQualityGateStop(
	request string,
	result packet.Result,
	reviewNumber int,
	autoFixes int,
	phase string,
	gateErr error,
) error {
	if err := w.state.SecureArtifactDir(); err != nil {
		return err
	}
	checkpoint := state.ResumeCheckpoint{
		Stage:        state.ResumeStageWorker,
		Phase:        phase,
		Role:         state.WorkerRole,
		Model:        w.config.WorkerModel,
		Effort:       w.config.RoutineEffort,
		Request:      request,
		Decision:     w.state.ReadOr("last-decision", ""),
		ReviewNumber: reviewNumber,
		AutoFixes:    autoFixes,
	}
	checkpoint.SetStopKind(state.ResumeStopQualityGate)
	savedResult := result
	checkpoint.CompletedResult = &savedResult
	checkpoint.QualityGateFailure = boundedText(gateErr.Error(), packet.MaxDiagnosticBytes)
	if err := w.captureStopRetention(&checkpoint); err != nil {
		return err
	}
	if err := w.state.EnterStop(checkpoint); err != nil {
		return err
	}
	taskID, _ := w.state.TaskID()
	return &QualityGateRecoverableError{
		Phase:       checkpoint.Phase,
		Failure:     checkpoint.QualityGateFailure,
		TaskID:      taskID,
		RepoRoot:    w.config.RepoRoot,
		ResultSaved: checkpoint.CompletedResult != nil,
	}
}
