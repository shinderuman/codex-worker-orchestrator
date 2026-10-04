package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
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
	observationExecutionsStateFile = "observation-executions"
	observationExecutionsRetention = 16

	ObservationExecutionStatusInFlight = "in-flight"
	ObservationExecutionStatusPass     = "pass"
	ObservationExecutionStatusFail     = "fail"
)

func (s *StateStore) ObservationExecuteAdmission() (ObservationExecutionAdmission, error) {
	if s.TaskStatus() != TaskStatusWaitingDecision || !s.Exists("pending-decision") {
		return ObservationExecutionAdmission{}, fmt.Errorf("observation-executeはPoC/observation taskのpending Sol decision境界だけでadmitされます")
	}
	open, err := s.CurrentParentReview()
	if err != nil {
		return ObservationExecutionAdmission{}, fmt.Errorf("pending decisionのparent review状態を読めません: %w", err)
	}
	if open == nil || open.PacketStatus != "NEEDS_SOL_DECISION" {
		return ObservationExecutionAdmission{}, fmt.Errorf("observation-executeはNEEDS_SOL_DECISION待ちの境界だけでadmitされます")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return ObservationExecutionAdmission{}, err
	}
	content, err := os.ReadFile(s.TaskAuthorityContentPath(taskID))
	if err != nil {
		return ObservationExecutionAdmission{}, fmt.Errorf("task authority snapshotを読めません: %w", err)
	}
	if _, err := parseObservationExecutionDeclaration(content); err != nil {
		return ObservationExecutionAdmission{}, err
	}
	round, err := s.ObservationExecutionRound()
	if err != nil {
		return ObservationExecutionAdmission{}, err
	}
	return ObservationExecutionAdmission{TaskID: taskID, Round: round}, nil
}

func (s *StateStore) ObservationExecutionRound() (int, error) {
	stats, err := s.CurrentTaskStats()
	if err != nil {
		return 0, fmt.Errorf("decision roundの統計を読めません: %w", err)
	}
	return stats.DecisionCommands, nil
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
	if len(records) > observationExecutionsRetention {
		records = records[len(records)-observationExecutionsRetention:]
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("observation実行記録をencodeできません: %w", err)
	}
	return s.Write(observationExecutionsStateFile, string(encoded))
}

func validateObservationInFlightRecord(record ObservationExecutionRecord) error {
	if record.ExecutionID == "" || record.TaskID == "" || record.Operation == "" || record.ParamsDigest == "" || record.StartedAtRFC3339 == "" {
		return fmt.Errorf("observation in-flight記録に必須fieldがありません")
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
	if record.Status != ObservationExecutionStatusPass && record.Status != ObservationExecutionStatusFail {
		return fmt.Errorf("observation実行status %qはpass/failのどちらかです", record.Status)
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

func parseObservationExecutionDeclaration(content []byte) (taskcontract.ExternalFeasibility, error) {
	declaration, err := taskcontract.ParseExternalFeasibility(content)
	if err != nil {
		return taskcontract.ExternalFeasibility{}, fmt.Errorf("ACTIVE taskのExternal feasibility宣言を受理できません: %w", err)
	}
	if declaration.Status != taskcontract.StatusPoC && declaration.Status != taskcontract.StatusObservation {
		return taskcontract.ExternalFeasibility{}, fmt.Errorf("observation-executeはstatus %sではadmitされません(poc/observationだけが対象です)", declaration.Status)
	}
	return declaration, nil
}
