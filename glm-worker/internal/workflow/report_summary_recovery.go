package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type summaryRecoveryEvidence struct {
	TaskID           string                  `json:"task_id"`
	CallID           string                  `json:"call_id"`
	SessionID        string                  `json:"session_id"`
	ResponseSHA256   string                  `json:"response_sha256"`
	Snapshot         state.SnapshotDigest    `json:"snapshot"`
	Correction       *resultCorrectionRecord `json:"correction"`
	OriginalResponse string                  `json:"original_response"`
	CorrectedResult  packet.Result           `json:"corrected_result"`
}

const (
	summaryRecoveryArtifactPrefix = "summary-recovery-"
	rejectedReportCallOutcome     = "invalid_packet"
)

func (w *Workflow) admitReportResubmission() error {
	if w.SummaryRecoveryAvailable() != nil {
		return w.admitParentAction(state.ParentActionResubmitReport)
	}
	_, admitted, err := w.state.AdmitParentAction(state.ParentActionResubmitReport)
	if err != nil {
		return err
	}
	if !admitted {
		return w.parentActionDenied(state.ParentActionResubmitReport)
	}
	exists, err := controller.Exists(w.config)
	if err != nil {
		return err
	}
	if !exists {
		return w.admitCanonicalMutation()
	}
	admission, err := controller.Activate(w.config)
	if err != nil {
		return err
	}
	if admission.Lease.InFlightCallID != "" {
		return fmt.Errorf("summary recovery requires the idle canonical execution")
	}
	if err := w.bindCanonicalWorkflowAttempt(admission); err != nil {
		return err
	}
	w.canonicalAdmission = &admission
	return nil
}

func (w *Workflow) ReportResubmissionAvailable() error {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		return err
	}
	if record.Resubmissions < maxResultReportResubmissions || record.Terminal == nil {
		return nil
	}
	_, _, err = w.summaryRecoveryResult(checkpoint, record)
	return err
}

func (w *Workflow) SummaryRecoveryAvailable() error {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		return err
	}
	_, _, err = w.summaryRecoveryResult(checkpoint, record)
	return err
}

func (w *Workflow) recoverOversizedSummary(checkpoint state.ResumeCheckpoint, decl externalFeasibility, record *resultCorrectionRecord) error {
	result, evidence, err := w.summaryRecoveryResult(checkpoint, record)
	if err != nil {
		return &WorkerError{Phase: checkpoint.Phase, Message: "report resubmission was already authorized once; summary recovery refused: " + err.Error()}
	}
	if err := w.saveSummaryRecoveryEvidence(evidence); err != nil {
		return err
	}
	target := acceptedResultContinuationCheckpoint(checkpoint, decl)
	target.SetStopKind(state.ResumeStopInterrupted)
	target.StopGitSnapshot = checkpoint.StopGitSnapshot
	target.StopDirtyFiles = checkpoint.StopDirtyFiles
	target.SummaryRecovered = true
	target.CompletedResult = &result
	if err := w.state.EnterStop(target); err != nil {
		return err
	}
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID: record.TaskID, CallType: state.CallTypeEvent, Role: checkpoint.Role,
		Phase: checkpoint.Phase + "-summary-recovery", StartedAt: now, CompletedAt: now,
		Outcome: "report_summary_recovered", Response: w.summaryRecoveryPath(evidence.CallID),
	})
	return machinecli.WriteJSON(w.output, map[string]any{"status": "report_summary_recovered", "task_id": record.TaskID, "source_call_id": evidence.CallID, "summary_bytes": len(result.Summary), "artifact": w.summaryRecoveryPath(evidence.CallID)})
}

func (w *Workflow) verifyRecoveredSummary(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.CompletedResult == nil || checkpoint.StopKind != state.ResumeStopInterrupted || checkpoint.Stage != state.ResumeStageWorker {
		return fmt.Errorf("summary recovery has no retained worker report")
	}
	if err := w.verifyInterruptedRetention(checkpoint); err != nil {
		return err
	}
	for _, path := range checkpoint.CompletedResult.Artifacts {
		if strings.HasPrefix(filepath.Base(path), summaryRecoveryArtifactPrefix) {
			return w.verifyRetainedRecoveryEvidence(checkpoint, path)
		}
	}
	return fmt.Errorf("retained summary recovery evidence is unavailable")
}

func (w *Workflow) verifyRetainedRecoveryEvidence(checkpoint state.ResumeCheckpoint, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var evidence summaryRecoveryEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return err
	}
	if err := w.verifyRetainedRecoveryBinding(checkpoint, evidence, path); err != nil {
		return err
	}
	logs, err := w.state.ReadModelCallLogs(evidence.TaskID)
	if err != nil {
		return err
	}
	call := latestAcceptedResultTaskCall(logs)
	if call.CallID != evidence.CallID || call.SessionID != evidence.SessionID || call.ResponseSHA256 != evidence.ResponseSHA256 || call.Response != evidence.OriginalResponse || call.Outcome != rejectedReportCallOutcome {
		return fmt.Errorf("retained summary recovery no longer matches the rejected model call")
	}
	if err := packet.ValidateArtifacts(checkpoint.CompletedResult.Artifacts, w.state.ArtifactDir(evidence.TaskID)); err != nil {
		return err
	}
	return packet.ValidateWorkerResult(*checkpoint.CompletedResult)
}

func (w *Workflow) verifyRetainedRecoveryBinding(checkpoint state.ResumeCheckpoint, evidence summaryRecoveryEvidence, path string) error {
	a, _ := json.Marshal(evidence.CorrectedResult)
	b, _ := json.Marshal(checkpoint.CompletedResult)
	current, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return err
	}
	if evidence.TaskID != w.state.ReadOr("task.id", "") || evidence.SessionID != w.state.ReadOr("worker.id", "") || path != w.summaryRecoveryPath(evidence.CallID) || !bytes.Equal(a, b) || evidence.Snapshot.Head != current.Head || evidence.Snapshot.WorktreeDigestExcludingParent != current.WorktreeDigestExcludingParent || resultRecoveryDigest([]byte(evidence.OriginalResponse)) != evidence.ResponseSHA256 {
		return fmt.Errorf("retained summary recovery task/session/source/report binding changed")
	}
	return nil
}

func (w *Workflow) summaryRecoveryResult(checkpoint state.ResumeCheckpoint, record *resultCorrectionRecord) (packet.Result, summaryRecoveryEvidence, error) {
	var evidence summaryRecoveryEvidence
	if err := requireExhaustedCorrectionBoundary(checkpoint, record); err != nil {
		return packet.Result{}, evidence, err
	}
	if err := w.verifySummaryRecoveryBoundary(checkpoint, record); err != nil {
		return packet.Result{}, evidence, err
	}
	call, err := w.rejectedReportCall(checkpoint, record)
	if err != nil {
		return packet.Result{}, evidence, err
	}
	result, err := w.correctedReportResult(call, record)
	if err != nil {
		return packet.Result{}, evidence, err
	}
	evidence = summaryRecoveryEvidence{TaskID: record.TaskID, CallID: call.CallID, SessionID: record.SessionID,
		ResponseSHA256: call.ResponseSHA256, Snapshot: record.Snapshot, Correction: record,
		OriginalResponse: call.Response, CorrectedResult: result}
	return result, evidence, nil
}

func requireExhaustedCorrectionBoundary(checkpoint state.ResumeCheckpoint, record *resultCorrectionRecord) error {
	if checkpoint.StopKind != state.ResumeStopResultFormat || !checkpoint.ResultCorrection || !checkpoint.ReadOnly || checkpoint.Role != state.WorkerRole || checkpoint.Stage != state.ResumeStageWorker || record.Terminal == nil || record.Terminal.Reason != "repeated_violation" || record.Resubmissions != maxResultReportResubmissions {
		return fmt.Errorf("requires the exhausted worker result-format correction boundary")
	}
	return nil
}

func (w *Workflow) rejectedReportCall(checkpoint state.ResumeCheckpoint, record *resultCorrectionRecord) (state.ModelCallLog, error) {
	logs, err := w.state.ReadModelCallLogs(record.TaskID)
	if err != nil {
		return state.ModelCallLog{}, err
	}
	call := latestAcceptedResultTaskCall(logs)
	if call.TaskID != record.TaskID || call.SessionID != record.SessionID || call.Role != record.Role || call.Phase != checkpoint.Phase || call.Outcome != rejectedReportCallOutcome || call.ResumeSource != "result-format-recovery" || !call.ReadOnly || call.CallID == "" || filepath.Base(call.CallID) != call.CallID || resultRecoveryDigest([]byte(call.Response)) != call.ResponseSHA256 {
		return state.ModelCallLog{}, fmt.Errorf("latest rejected report is not bound to the same task/session/correction")
	}
	return call, nil
}

func (w *Workflow) correctedReportResult(call state.ModelCallLog, record *resultCorrectionRecord) (packet.Result, error) {
	result, err := packet.ParseStructured([]byte(call.Response))
	if err != nil {
		return packet.Result{}, err
	}
	if result.Status != packet.StatusImplemented || len(result.Summary) <= packet.MaxFieldBytes || !utf8.ValidString(result.Summary) || strings.ContainsAny(result.Summary, "\r\n") {
		return packet.Result{}, fmt.Errorf("only an oversized implementation summary is recoverable")
	}
	result.Summary = boundedSummary(result.Summary)
	if err := packet.ValidateArtifacts(result.Artifacts, w.state.ArtifactDir(record.TaskID)); err != nil {
		return packet.Result{}, err
	}
	result.Artifacts = append(result.Artifacts, w.summaryRecoveryPath(call.CallID))
	if err := packet.ValidateWorkerResult(result); err != nil {
		return packet.Result{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return packet.Result{}, err
	}
	if len(encoded) > packet.MaxPacketBytes {
		return packet.Result{}, fmt.Errorf("corrected report exceeds the packet byte budget")
	}
	return result, nil
}

func (w *Workflow) verifySummaryRecoveryBoundary(checkpoint state.ResumeCheckpoint, record *resultCorrectionRecord) error {
	if err := w.verifyRecoveryIdentity(checkpoint, record); err != nil {
		return err
	}
	current, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return err
	}
	if snapshotDigest(current) != record.Snapshot || checkpoint.StopGitSnapshot == nil || checkpoint.StopGitSnapshot.Head != current.Head || checkpoint.StopDirtyFiles == nil {
		return fmt.Errorf("summary recovery source snapshot changed")
	}
	files, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return err
	}
	if diff := state.DescribeStopDirtyDiff(checkpoint.StopDirtyFiles, files); diff != "" {
		return fmt.Errorf("summary recovery retained source changed: %s", diff)
	}
	return nil
}

func (w *Workflow) verifyRecoveryIdentity(checkpoint state.ResumeCheckpoint, record *resultCorrectionRecord) error {
	if record.TaskID == "" || record.TaskID != w.state.ReadOr("task.id", "") || record.SessionID == "" || record.SessionID != w.state.ReadOr(string(record.Role)+".id", "") || checkpoint.Role != record.Role || strings.Count(checkpoint.Phase, resultCorrectionPhaseSuffix) != record.Attempts {
		return fmt.Errorf("summary recovery task/session identity changed")
	}
	return nil
}

func boundedSummary(summary string) string {
	if end := strings.Index(summary, ". "); end >= 0 && end+1 <= packet.MaxFieldBytes {
		return summary[:end+1]
	}
	if end := strings.Index(summary, "。"); end >= 0 && end+len("。") <= packet.MaxFieldBytes {
		return summary[:end+len("。")]
	}
	end := packet.MaxFieldBytes - len("…")
	for !utf8.ValidString(summary[:end]) {
		end--
	}
	return summary[:end] + "…"
}

func (w *Workflow) summaryRecoveryPath(callID string) string {
	return filepath.Join(w.state.ArtifactDir(w.state.ReadOr("task.id", "")), summaryRecoveryArtifactPrefix+callID+".json")
}

func (w *Workflow) saveSummaryRecoveryEvidence(evidence summaryRecoveryEvidence) error {
	if err := w.state.SecureArtifactDir(); err != nil {
		return err
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	path := w.summaryRecoveryPath(evidence.CallID)
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("summary recovery evidence differs from the retained report")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
