package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type resultCorrectionRecord struct {
	Version        int                  `json:"version"`
	TaskID         string               `json:"task_id"`
	Role           state.SessionRole    `json:"role"`
	SessionID      string               `json:"session_id"`
	Snapshot       state.SnapshotDigest `json:"snapshot"`
	Attempts       int                  `json:"attempts"`
	SeenViolations []string             `json:"seen_violations"`
}

type ResultCorrectionFailure struct {
	Reason           string               `json:"reason"`
	Attempts         int                  `json:"attempts"`
	TaskID           string               `json:"task_id"`
	SessionID        string               `json:"session_id"`
	Snapshot         state.SnapshotDigest `json:"snapshot"`
	Violations       []string             `json:"violations"`
	BoundaryMismatch string               `json:"boundary_mismatch,omitempty"`
}

const (
	resultCorrectionVersion = 1
	maxResultCorrections    = 2
)

func (e *ResultCorrectionFailure) Error() string {
	var message string
	switch e.Reason {
	case "budget_exhausted":
		message = fmt.Sprintf("result correction budget exhausted after %d correction attempts", e.Attempts)
	case "repeated_violation":
		message = fmt.Sprintf("result correction did not converge after %d correction attempt", e.Attempts)
	case "boundary_changed":
		message = "result correction boundary changed: " + e.BoundaryMismatch
	case "boundary_unavailable":
		message = "result correction boundary is unavailable: " + e.BoundaryMismatch
	default:
		message = "result correction failed"
	}
	if len(e.Violations) != 0 {
		message += ": " + strings.Join(e.Violations, "; ")
	}
	return message
}

func NewResultCorrectionWorkerError(phase string, failure *ResultCorrectionFailure) *WorkerError {
	return &WorkerError{
		Phase:   phase + "-format",
		Message: failure.Error(),
		cause:   failure,
	}
}

func ResultCorrectionFailureFromError(err error) (*ResultCorrectionFailure, bool) {
	var failure *ResultCorrectionFailure
	if !errors.As(err, &failure) {
		return nil, false
	}
	return failure, true
}

func (w *Workflow) handleResultCorrectionViolation(
	checkpoint state.ResumeCheckpoint,
	resultErr error,
) (packet.Result, error) {
	violations := packet.ConstraintReasons(resultErr)
	if !checkpoint.ResultCorrection {
		record, err := w.newResultCorrectionRecord(checkpoint, violations)
		if err != nil {
			return packet.Result{}, err
		}
		if err := w.saveResultCorrectionRecord(record); err != nil {
			return packet.Result{}, err
		}
		w.state.RecordResultCorrection()
		return w.runModel(w.nextResultCorrectionCheckpoint(checkpoint, resultErr.Error()))
	}

	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		return packet.Result{}, w.resultCorrectionBoundaryFailure(checkpoint, nil, "boundary_unavailable", err.Error())
	}
	if record.Attempts >= maxResultCorrections {
		return packet.Result{}, w.resultCorrectionTerminalFailure(checkpoint, record, "budget_exhausted", "", violations)
	}
	if !hasNewConstraintViolation(record.SeenViolations, violations) {
		return packet.Result{}, w.resultCorrectionTerminalFailure(checkpoint, record, "repeated_violation", "", violations)
	}

	record.Attempts++
	record.SeenViolations = appendUniqueViolations(record.SeenViolations, violations)
	if err := w.saveResultCorrectionRecord(record); err != nil {
		return packet.Result{}, err
	}
	w.state.RecordResultCorrection()
	return w.runModel(w.nextResultCorrectionCheckpoint(checkpoint, resultErr.Error()))
}

func (w *Workflow) newResultCorrectionRecord(checkpoint state.ResumeCheckpoint, violations []string) (*resultCorrectionRecord, error) {
	taskID, err := w.state.TaskID()
	if err != nil {
		return nil, err
	}
	sessionID, _, err := w.state.SessionID(checkpoint.Role)
	if err != nil {
		return nil, err
	}
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return nil, err
	}
	return &resultCorrectionRecord{
		Version:        resultCorrectionVersion,
		TaskID:         taskID,
		Role:           checkpoint.Role,
		SessionID:      sessionID,
		Snapshot:       snapshotDigest(snapshot),
		Attempts:       1,
		SeenViolations: appendUniqueViolations(nil, violations),
	}, nil
}

func (w *Workflow) nextResultCorrectionCheckpoint(checkpoint state.ResumeCheckpoint, reason string) state.ResumeCheckpoint {
	checkpoint.Phase += resultCorrectionPhaseSuffix
	prompt := resultCorrectionPrompt(reason)
	checkpoint.Prompt = prompt
	checkpoint.OriginalPrompt = prompt
	checkpoint.ResultCorrection = true
	w.pendingRetry = &callRetryContext{callID: w.lastCallID, reason: "invalid-packet-result-correction"}
	return checkpoint
}

func (w *Workflow) validateResultCorrectionBoundary(checkpoint state.ResumeCheckpoint) error {
	if !checkpoint.ResultCorrection {
		return nil
	}
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		return w.resultCorrectionBoundaryFailure(checkpoint, nil, "boundary_unavailable", err.Error())
	}
	if checkpoint.Role != record.Role {
		return w.resultCorrectionBoundaryFailure(checkpoint, record, "boundary_changed", fmt.Sprintf("role=%s want=%s", checkpoint.Role, record.Role))
	}
	if attempts := strings.Count(checkpoint.Phase, resultCorrectionPhaseSuffix); attempts != record.Attempts {
		return w.resultCorrectionBoundaryFailure(checkpoint, record, "boundary_changed", fmt.Sprintf("attempt=%d want=%d", attempts, record.Attempts))
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return w.resultCorrectionBoundaryFailure(checkpoint, record, "boundary_unavailable", err.Error())
	}
	if taskID != record.TaskID {
		return w.resultCorrectionBoundaryFailure(checkpoint, record, "boundary_changed", fmt.Sprintf("task_id=%s want=%s", taskID, record.TaskID))
	}
	sessionID, err := w.state.Read(string(checkpoint.Role) + ".id")
	if err != nil {
		return w.resultCorrectionBoundaryFailure(checkpoint, record, "boundary_unavailable", "session identity is unavailable")
	}
	if sessionID != record.SessionID {
		return w.resultCorrectionBoundaryFailure(checkpoint, record, "boundary_changed", fmt.Sprintf("session_id=%s want=%s", sessionID, record.SessionID))
	}
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return w.resultCorrectionBoundaryFailure(checkpoint, record, "boundary_unavailable", err.Error())
	}
	current := snapshotDigest(snapshot)
	if current != record.Snapshot {
		return w.resultCorrectionBoundaryFailure(checkpoint, record, "boundary_changed", "repository snapshot changed")
	}
	return nil
}

func (w *Workflow) resultCorrectionBoundaryFailure(
	checkpoint state.ResumeCheckpoint,
	record *resultCorrectionRecord,
	reason string,
	mismatch string,
) error {
	failure := &ResultCorrectionFailure{Reason: reason, BoundaryMismatch: mismatch}
	if record != nil {
		failure.Attempts = record.Attempts
		failure.TaskID = record.TaskID
		failure.SessionID = record.SessionID
		failure.Snapshot = record.Snapshot
		failure.Violations = append([]string(nil), record.SeenViolations...)
	}
	_ = w.state.ClearResumeCheckpoint()
	_ = w.clearResultCorrectionRecord()
	return NewResultCorrectionWorkerError(checkpoint.Phase, failure)
}

func (w *Workflow) resultCorrectionTerminalFailure(
	checkpoint state.ResumeCheckpoint,
	record *resultCorrectionRecord,
	reason string,
	mismatch string,
	currentViolations []string,
) error {
	failure := &ResultCorrectionFailure{
		Reason:           reason,
		Attempts:         record.Attempts,
		TaskID:           record.TaskID,
		SessionID:        record.SessionID,
		Snapshot:         record.Snapshot,
		Violations:       appendUniqueViolations(record.SeenViolations, currentViolations),
		BoundaryMismatch: mismatch,
	}
	_ = w.state.ClearResumeCheckpoint()
	_ = w.clearResultCorrectionRecord()
	return NewResultCorrectionWorkerError(checkpoint.Phase, failure)
}

func (w *Workflow) saveResultCorrectionRecord(record *resultCorrectionRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return w.state.Write(state.ResultCorrectionStateFile, string(data))
}

func (w *Workflow) loadResultCorrectionRecord() (*resultCorrectionRecord, error) {
	data, err := w.state.Read(state.ResultCorrectionStateFile)
	if err != nil {
		return nil, err
	}
	var record resultCorrectionRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return nil, err
	}
	if record.Version != resultCorrectionVersion || record.TaskID == "" || record.SessionID == "" || record.Attempts < 1 || record.Attempts > maxResultCorrections {
		return nil, fmt.Errorf("invalid result correction state")
	}
	return &record, nil
}

func (w *Workflow) clearResultCorrectionRecord() error {
	return w.state.Remove(state.ResultCorrectionStateFile)
}

func snapshotDigest(snapshot state.GitSnapshot) state.SnapshotDigest {
	return state.SnapshotDigest{
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
	}
}

func hasNewConstraintViolation(seen, current []string) bool {
	known := make(map[string]struct{}, len(seen))
	for _, violation := range seen {
		known[violation] = struct{}{}
	}
	for _, violation := range current {
		if _, exists := known[violation]; !exists {
			return true
		}
	}
	return false
}

func appendUniqueViolations(dst, src []string) []string {
	result := append([]string(nil), dst...)
	seen := make(map[string]struct{}, len(result)+len(src))
	for _, violation := range result {
		seen[violation] = struct{}{}
	}
	for _, violation := range src {
		if violation == "" {
			continue
		}
		if _, exists := seen[violation]; exists {
			continue
		}
		seen[violation] = struct{}{}
		result = append(result, violation)
	}
	return result
}
