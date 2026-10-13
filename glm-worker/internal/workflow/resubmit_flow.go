package workflow

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const maxResultReportResubmissions = 1

func (w *Workflow) ExecuteReportResubmission() error {
	return quietWhenTerminalResultEmitted(w.withTemp(w.executeReportResubmission))
}

func (w *Workflow) executeReportResubmission() error {
	if err := w.admitParentAction(state.ParentActionResubmitReport); err != nil {
		return err
	}
	checkpoint, decl, _, err := w.loadResumeCheckpoint()
	if err != nil {
		return err
	}
	previous := checkpoint
	if err := w.authorizeResultReportResubmission(checkpoint); err != nil {
		return err
	}
	if err := w.activateResume(checkpoint); err != nil {
		return w.restoreResumeStop(previous, err)
	}
	w.resetInstructionReadObservation()
	run := checkpoint
	run.ClearStop()
	result, err := w.runModel(run)
	if err != nil {
		return w.handleResumeRunError(run, previous, err)
	}
	return w.routeResumeResult(run, decl, result)
}

func (w *Workflow) authorizeResultReportResubmission(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.StopKind != state.ResumeStopResultFormat || !checkpoint.ResultCorrection {
		return &WorkerError{
			Phase:   checkpoint.Phase,
			Message: "report resubmission requires the terminal result-format stop checkpoint of the failed correction call",
		}
	}
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		return err
	}
	if record.Terminal == nil {
		return w.verifyResultCorrectionRecordIdentity(checkpoint, record)
	}
	if record.Resubmissions >= maxResultReportResubmissions {
		return &WorkerError{
			Phase:   checkpoint.Phase,
			Message: "report resubmission was already authorized once for this task; the terminal result correction record is kept as failure history",
		}
	}
	if err := w.verifyResultCorrectionRecordIdentity(checkpoint, record); err != nil {
		return err
	}
	record.Resubmissions++
	record.Terminal = nil
	return w.saveResultCorrectionRecord(record)
}
