package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ObservationExecutionRecord struct {
	ExecutionID        string   `json:"execution_id"`
	TaskID             string   `json:"task_id"`
	Operation          string   `json:"operation"`
	ParamsDigest       string   `json:"params_digest"`
	Status             string   `json:"status"`
	Detail             string   `json:"detail,omitempty"`
	Artifacts          []string `json:"artifacts,omitempty"`
	ExitCode           int      `json:"exit_code,omitempty"`
	ExitSource         string   `json:"exit_source,omitempty"`
	DurationMS         int64    `json:"duration_ms,omitempty"`
	DeadlineMS         int64    `json:"deadline_ms,omitempty"`
	Head               string   `json:"head,omitempty"`
	IndexDigest        string   `json:"index_digest,omitempty"`
	WorktreeDigest     string   `json:"worktree_digest,omitempty"`
	DecisionRound      int      `json:"decision_round"`
	StartedAtRFC3339   string   `json:"started_at,omitempty"`
	CompletedAtRFC3339 string   `json:"completed_at,omitempty"`
}

type ObservationExecutionAdmission struct {
	TaskID string
	Round  int
}

const (
	observationExecutionsStateFile    = "observation-executions"
	observationExecutionsRetention    = 16
	observationExecutionRecoveryGrace = 10 * time.Second

	ObservationExecutionStatusInFlight      = "in-flight"
	ObservationExecutionStatusPass          = "pass"
	ObservationExecutionStatusFail          = "fail"
	ObservationExecutionStatusIndeterminate = "indeterminate"
)

func (s *StateStore) ObservationExecuteAdmission() (ObservationExecutionAdmission, error) {
	if s.TaskStatus() != TaskStatusWaitingDecision || !s.Exists("pending-decision") {
		return ObservationExecutionAdmission{}, fmt.Errorf("observation capability is only available at the pending Sol decision boundary")
	}
	open, err := s.CurrentParentReview()
	if err != nil {
		return ObservationExecutionAdmission{}, fmt.Errorf("pending decisionのparent review状態を読めません: %w", err)
	}
	if open == nil || open.PacketStatus != "NEEDS_SOL_DECISION" {
		return ObservationExecutionAdmission{}, fmt.Errorf("observation capability is only available while NEEDS_SOL_DECISION is pending")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return ObservationExecutionAdmission{}, err
	}
	round, err := s.ObservationExecutionRound()
	if err != nil {
		return ObservationExecutionAdmission{}, err
	}
	return ObservationExecutionAdmission{TaskID: taskID, Round: round}, nil
}

func (s *StateStore) ValidateObservationExecuteAdmission(expected ObservationExecutionAdmission) error {
	current, err := s.ObservationExecuteAdmission()
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("observation lifecycle admission changed: task=%s round=%d", current.TaskID, current.Round)
	}
	return nil
}

func (s *StateStore) ObservationExecutionRound() (int, error) {
	if _, err := os.Stat(s.Path(parentEvidenceLeasePath)); err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("decision roundのcanonical lifecycle identityがありません")
		}
		return 0, fmt.Errorf("decision roundのcanonical lifecycle identityを確認できません: %w", err)
	}
	epoch, err := s.ParentEvidenceLeaseEpoch()
	if err != nil {
		return 0, fmt.Errorf("decision roundのcanonical lifecycle identityを読めません: %w", err)
	}
	round := int(epoch)
	if epoch <= 0 || int64(round) != epoch {
		return 0, fmt.Errorf("decision roundのcanonical lifecycle identityが不正です: %d", epoch)
	}
	return round, nil
}

func (s *StateStore) ObservationExecutions() ([]ObservationExecutionRecord, error) {
	data, err := os.ReadFile(s.Path(observationExecutionsStateFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("observation実行記録を読めません: %w", err)
	}
	var records []ObservationExecutionRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("observation実行記録をdecodeできません: %w", err)
	}
	for index, record := range records {
		if strings.TrimSpace(record.TaskID) == "" {
			return nil, fmt.Errorf("observation実行記録[%d]はcurrent schema必須のtask_idを欠いています", index)
		}
	}
	return records, nil
}

func (s *StateStore) ObservationExecutionsForRound(round int) ([]ObservationExecutionRecord, error) {
	taskID, err := s.TaskID()
	if err != nil {
		return nil, err
	}
	records, err := s.ObservationExecutions()
	if err != nil {
		return nil, err
	}
	filtered := make([]ObservationExecutionRecord, 0, len(records))
	for _, record := range records {
		if record.TaskID == taskID && record.DecisionRound == round && record.Status != ObservationExecutionStatusInFlight {
			filtered = append(filtered, record)
		}
	}
	return filtered, nil
}

func (s *StateStore) HasObservationExecution(operation, paramsDigest string, round int) (bool, error) {
	taskID, err := s.TaskID()
	if err != nil {
		return false, err
	}
	records, err := s.ObservationExecutions()
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.TaskID == taskID && record.Operation == operation && record.ParamsDigest == paramsDigest && record.DecisionRound == round {
			return true, nil
		}
	}
	return false, nil
}

func (s *StateStore) RecoverStaleObservationExecutions(now time.Time) error {
	records, err := s.ObservationExecutions()
	if err != nil {
		return err
	}
	changed := false
	for index := range records {
		record := &records[index]
		if record.Status != ObservationExecutionStatusInFlight {
			continue
		}
		if err := validateObservationInFlightRecord(*record); err != nil {
			return fmt.Errorf("observation in-flight記録[%d]を回収できません: %w", index, err)
		}
		startedAt, err := time.Parse(time.RFC3339Nano, record.StartedAtRFC3339)
		if err != nil {
			return fmt.Errorf("observation in-flight記録[%d]のstarted_atが不正です: %w", index, err)
		}
		recoveryAt := startedAt.Add(time.Duration(record.DeadlineMS)*time.Millisecond + observationExecutionRecoveryGrace)
		if now.Before(recoveryAt) {
			continue
		}
		record.Status = ObservationExecutionStatusIndeterminate
		record.Detail = "observation execution crossed its durable recovery horizon without completion; the operation may have run and automatic replay is refused"
		record.ExitSource = "recovery"
		record.CompletedAtRFC3339 = now.UTC().Format(time.RFC3339Nano)
		changed = true
	}
	if !changed {
		return nil
	}
	return s.writeObservationExecutions(records)
}

func (s *StateStore) BeginObservationExecution(record ObservationExecutionRecord) error {
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if record.TaskID == "" {
		record.TaskID = taskID
	}
	if record.TaskID != taskID {
		return fmt.Errorf("observation in-flight task identity does not match current task")
	}
	if err := validateObservationInFlightRecord(record); err != nil {
		return err
	}
	records, err := s.ObservationExecutions()
	if err != nil {
		return err
	}
	for _, existing := range records {
		if existing.ExecutionID == record.ExecutionID {
			return fmt.Errorf("observation execution id %s already exists", record.ExecutionID)
		}
		if sameObservationExecutionIdentity(existing, record) {
			return fmt.Errorf("同一decision round内の同一operation・parameter再実行はmachineが拒否します(%s)", record.Operation)
		}
	}
	return s.writeObservationExecutions(append(records, record))
}

func (s *StateStore) CompleteObservationExecution(record ObservationExecutionRecord) error {
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if record.TaskID != taskID {
		return fmt.Errorf("observation completion task identity does not match current task")
	}
	if err := validateObservationExecutionRecord(record, s.ArtifactDir(taskID)); err != nil {
		return err
	}
	records, err := s.ObservationExecutions()
	if err != nil {
		return err
	}
	matched := false
	for index := range records {
		existing := records[index]
		if existing.ExecutionID != record.ExecutionID {
			continue
		}
		if existing.Status != ObservationExecutionStatusInFlight || !sameObservationExecutionIdentity(existing, record) {
			return fmt.Errorf("observation completion does not match its in-flight identity")
		}
		records[index] = record
		matched = true
		break
	}
	if !matched {
		return fmt.Errorf("observation completion has no durable in-flight identity")
	}
	if err := s.writeObservationExecutions(records); err != nil {
		return err
	}
	return s.SecureArtifactDir()
}

func (s *StateStore) AppendObservationExecution(record ObservationExecutionRecord) error {
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if record.TaskID == "" {
		record.TaskID = taskID
	}
	if record.TaskID != taskID {
		return fmt.Errorf("observation execution task identity does not match current task")
	}
	if err := validateObservationExecutionRecord(record, s.ArtifactDir(taskID)); err != nil {
		return err
	}
	records, err := s.ObservationExecutions()
	if err != nil {
		return err
	}
	if err := s.writeObservationExecutions(append(records, record)); err != nil {
		return err
	}
	return s.SecureArtifactDir()
}

func (s *StateStore) writeObservationExecutions(records []ObservationExecutionRecord) error {
	records = trimObservationExecutions(records)
	encoded, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("observation実行記録をencodeできません: %w", err)
	}
	return s.Write(observationExecutionsStateFile, string(encoded))
}

func trimObservationExecutions(records []ObservationExecutionRecord) []ObservationExecutionRecord {
	completed := 0
	for _, record := range records {
		if record.Status != ObservationExecutionStatusInFlight {
			completed++
		}
	}
	dropCompleted := completed - observationExecutionsRetention
	if dropCompleted <= 0 {
		return records
	}
	trimmed := make([]ObservationExecutionRecord, 0, len(records)-dropCompleted)
	for _, record := range records {
		if record.Status != ObservationExecutionStatusInFlight && dropCompleted > 0 {
			dropCompleted--
			continue
		}
		trimmed = append(trimmed, record)
	}
	return trimmed
}

func validateObservationInFlightRecord(record ObservationExecutionRecord) error {
	if record.ExecutionID == "" || record.TaskID == "" || record.Operation == "" || record.ParamsDigest == "" || record.StartedAtRFC3339 == "" || record.DeadlineMS <= 0 {
		return fmt.Errorf("observation in-flight記録にcurrent schema必須fieldがありません")
	}
	if record.Status != ObservationExecutionStatusInFlight || record.CompletedAtRFC3339 != "" || len(record.Artifacts) != 0 {
		return fmt.Errorf("observation in-flight記録のlifecycle fieldが不正です")
	}
	return nil
}

func validateObservationExecutionRecord(record ObservationExecutionRecord, artifactRoot string) error {
	if record.ExecutionID == "" || record.TaskID == "" || record.Operation == "" || record.ParamsDigest == "" {
		return fmt.Errorf("observation実行記録に必須fieldがありません")
	}
	if record.Status != ObservationExecutionStatusPass && record.Status != ObservationExecutionStatusFail && record.Status != ObservationExecutionStatusIndeterminate {
		return fmt.Errorf("observation実行status %qはpass/fail/indeterminateのいずれかです", record.Status)
	}
	if record.CompletedAtRFC3339 == "" {
		return fmt.Errorf("observation実行記録に完了時刻がありません")
	}
	return validateObservationExecutionArtifacts(artifactRoot, record.Artifacts)
}

func sameObservationExecutionIdentity(left, right ObservationExecutionRecord) bool {
	return left.TaskID == right.TaskID && left.Operation == right.Operation && left.ParamsDigest == right.ParamsDigest && left.DecisionRound == right.DecisionRound
}

func validateObservationExecutionArtifacts(artifactRoot string, artifacts []string) error {
	root, err := filepath.EvalSymlinks(artifactRoot)
	if err != nil {
		root = artifactRoot
	}
	for _, artifact := range artifacts {
		if artifact == "" {
			return fmt.Errorf("observation実行artifactsに空のpathがあります")
		}
		if !filepath.IsAbs(artifact) {
			return fmt.Errorf("observation実行artifact %qは絶対pathである必要があります", artifact)
		}
		resolved, err := filepath.EvalSymlinks(artifact)
		if err != nil {
			return fmt.Errorf("observation実行artifact %qを解決できません: %w", artifact, err)
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("observation実行artifact %qがtask artifact dirの外です", artifact)
		}
	}
	return nil
}
