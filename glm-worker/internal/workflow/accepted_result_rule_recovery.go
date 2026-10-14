package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type AcceptedResultRuleRecoveryInput struct {
	TaskID         string `json:"task_id"`
	AttemptID      string `json:"attempt_id"`
	CallID         string `json:"call_id"`
	TranscriptPath string `json:"transcript_path"`
}

type acceptedResultRuleRecovery struct {
	Version               int                             `json:"version"`
	Input                 AcceptedResultRuleRecoveryInput `json:"input"`
	ConsumedResubmissions int                             `json:"consumed_resubmissions"`
	SourceCheckpoint      state.ResumeCheckpoint          `json:"source_checkpoint"`
	SourceCorrection      json.RawMessage                 `json:"source_correction"`
	SourceSessionID       string                          `json:"source_session_id"`
	SourceResponseSHA256  string                          `json:"source_response_sha256"`
	AcceptedResult        packet.Result                   `json:"accepted_result"`
	TargetCheckpoint      state.ResumeCheckpoint          `json:"target_checkpoint"`
}

const acceptedResultCallOutcome = "success"

func (w *Workflow) RecoverAcceptedResultRuleActivation(input AcceptedResultRuleRecoveryInput) error {
	if err := w.admitCanonicalMutation(); err != nil {
		return err
	}
	if w.canonicalAdmission == nil || w.canonicalAdmission.Attempt.AttemptID != input.AttemptID || w.canonicalAdmission.Lease.InFlightCallID != "" {
		return fmt.Errorf("accepted-result recovery requires the exact idle canonical attempt")
	}
	return w.recoverAcceptedResultRuleActivation(input)
}

func (w *Workflow) recoverAcceptedResultRuleActivation(input AcceptedResultRuleRecoveryInput) error {
	if w.state.Exists(state.AcceptedResultRuleRecoveryStateFile) {
		record, err := w.loadAcceptedResultRuleRecovery()
		if err != nil {
			return err
		}
		if record.Input != input {
			return fmt.Errorf("accepted-result recovery authority differs from the saved transition")
		}
		return w.finishAcceptedResultRuleRecovery(record)
	}
	record, err := w.prepareAcceptedResultRuleRecovery(input)
	if err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := w.state.Write(state.AcceptedResultRuleRecoveryStateFile, string(data)); err != nil {
		return err
	}
	return w.finishAcceptedResultRuleRecovery(record)
}

func (w *Workflow) prepareAcceptedResultRuleRecovery(input AcceptedResultRuleRecoveryInput) (*acceptedResultRuleRecovery, error) {
	checkpoint, decl, _, err := w.loadResumeCheckpoint()
	if err != nil {
		return nil, err
	}
	call, correction, err := w.acceptedResultRuleRecoverySource(input, checkpoint)
	if err != nil {
		return nil, err
	}
	result, err := w.acceptedResultFromTranscript(input.TranscriptPath, call)
	if err != nil {
		return nil, err
	}
	target := acceptedResultContinuationCheckpoint(checkpoint, decl)
	target.SetStopKind(state.ResumeStopInterrupted)
	target.StopGitSnapshot = checkpoint.StopGitSnapshot
	target.StopDirtyFiles = checkpoint.StopDirtyFiles
	return &acceptedResultRuleRecovery{Version: 1, Input: input, ConsumedResubmissions: 1,
		SourceCheckpoint: checkpoint, SourceCorrection: correction, SourceSessionID: call.SessionID,
		SourceResponseSHA256: call.ResponseSHA256, AcceptedResult: result, TargetCheckpoint: target}, nil
}

func (w *Workflow) acceptedResultRuleRecoverySource(input AcceptedResultRuleRecoveryInput, checkpoint state.ResumeCheckpoint) (state.ModelCallLog, json.RawMessage, error) {
	if err := w.verifyAcceptedResultRecoveryStop(checkpoint); err != nil {
		return state.ModelCallLog{}, nil, err
	}
	call, err := w.acceptedResultRecoveryCall(input)
	if err != nil {
		return call, nil, err
	}
	if checkpoint.Phase != call.Phase+"-rule-activation-1" || !strings.HasSuffix(call.Phase, resultCorrectionPhaseSuffix) {
		return call, nil, fmt.Errorf("accepted-result recovery phase does not follow the accepted correction call")
	}
	correction, err := w.state.Read(state.ResultCorrectionStateFile)
	if err != nil {
		return call, nil, err
	}
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		return call, nil, err
	}
	if record.Terminal == nil || record.Terminal.Reason != "boundary_unavailable" || record.Terminal.BoundaryMismatch != "result correction state is missing" {
		return call, nil, fmt.Errorf("accepted-result recovery cannot replace an unrelated terminal correction")
	}
	if err := w.verifyAcceptedResultRecoverySnapshot(checkpoint); err != nil {
		return call, nil, err
	}
	return call, json.RawMessage(correction), nil
}

func (w *Workflow) verifyAcceptedResultRecoveryStop(checkpoint state.ResumeCheckpoint) error {
	if w.state.TaskStatus() != state.TaskStatusResultFormatRecoverable || checkpoint.StopKind != state.ResumeStopResultFormat || !checkpoint.ResultCorrection || checkpoint.Stage != state.ResumeStageWorker || checkpoint.Role != state.WorkerRole || checkpoint.ReportOnly {
		return fmt.Errorf("accepted-result recovery requires the failed worker rule-activation correction stop")
	}
	return nil
}

func (w *Workflow) acceptedResultRecoveryCall(input AcceptedResultRuleRecoveryInput) (state.ModelCallLog, error) {
	taskID, err := w.state.TaskID()
	if err != nil || taskID != input.TaskID || input.CallID == "" {
		return state.ModelCallLog{}, fmt.Errorf("accepted-result recovery task/call identity mismatch")
	}
	logs, err := w.state.ReadModelCallLogs(taskID)
	if err != nil {
		return state.ModelCallLog{}, err
	}
	call := latestAcceptedResultTaskCall(logs)
	if err := validateAcceptedResultTaskCall(call, input); err != nil {
		return call, err
	}
	if w.state.ReadOr("worker.id", "") != call.SessionID || call.SessionID == "" || resultRecoveryDigest([]byte(call.Response)) != call.ResponseSHA256 {
		return call, fmt.Errorf("accepted-result recovery session/response identity mismatch")
	}
	return call, nil
}

func latestAcceptedResultTaskCall(logs []state.ModelCallLog) state.ModelCallLog {
	var call state.ModelCallLog
	for _, entry := range logs {
		if entry.CallType == state.CallTypeTask {
			call = entry
		}
	}
	return call
}

func validateAcceptedResultTaskCall(call state.ModelCallLog, input AcceptedResultRuleRecoveryInput) error {
	if call.CallID != input.CallID || call.TaskID != input.TaskID || call.Role != state.WorkerRole || call.Outcome != acceptedResultCallOutcome || call.PacketStatus != string(packet.StatusImplemented) || call.ResumeSource != "result-format-recovery" || !call.ReadOnly {
		return fmt.Errorf("accepted-result recovery requires the latest successful one-shot worker report")
	}
	return nil
}

func (w *Workflow) verifyAcceptedResultRecoverySnapshot(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.StopGitSnapshot == nil || checkpoint.StopDirtyFiles == nil {
		return fmt.Errorf("accepted-result recovery has no retained snapshot")
	}
	snapshot, err := w.captureBoundarySnapshot(w.config.RepoRoot)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(snapshot, *checkpoint.StopGitSnapshot) {
		return fmt.Errorf("accepted-result recovery snapshot mismatch")
	}
	return w.verifyInterruptedRetention(checkpoint)
}

func (w *Workflow) finishAcceptedResultRuleRecovery(record *acceptedResultRuleRecovery) error {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(checkpoint, record.SourceCheckpoint) && !reflect.DeepEqual(checkpoint, record.TargetCheckpoint) {
		return fmt.Errorf("accepted-result recovery checkpoint changed")
	}
	if err := w.verifyAcceptedResultRecoverySnapshot(checkpoint); err != nil {
		return err
	}
	if w.state.Exists(state.ResultCorrectionStateFile) {
		data, err := w.state.Read(state.ResultCorrectionStateFile)
		if err != nil || string(record.SourceCorrection) != data {
			return fmt.Errorf("accepted-result recovery terminal history changed")
		}
		if err := w.clearResultCorrectionRecord(); err != nil {
			return err
		}
	}
	return w.state.EnterStop(record.TargetCheckpoint)
}

func (w *Workflow) loadAcceptedResultRuleRecovery() (*acceptedResultRuleRecovery, error) {
	data, err := w.state.Read(state.AcceptedResultRuleRecoveryStateFile)
	if err != nil {
		return nil, err
	}
	var record acceptedResultRuleRecovery
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return nil, err
	}
	taskID, err := w.state.TaskID()
	if err != nil || record.Version != 1 || record.Input.TaskID != taskID || record.ConsumedResubmissions != 1 {
		return nil, fmt.Errorf("invalid accepted-result recovery record")
	}
	return &record, nil
}

func resultRecoveryDigest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
