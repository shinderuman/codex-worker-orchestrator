package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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


type observationFenceIdentity struct {
	ExecutionID string `json:"execution_id"`
	Operation string `json:"operation"`
	ParamsDigest string `json:"params_digest"`
}
type observationExecutionDocument struct {
	TaskID string `json:"task_id"`
	Round int `json:"round"`
	Fence []observationFenceIdentity `json:"fence"`
	Records []ObservationExecutionRecord `json:"records"`
}

type ObservationExecutionAdmission struct {
	TaskID string
	Round  int
}

const (
	observationExecutionsStateFile    = "observation-executions"
	observationExecutionsRetention    = 16
	observationReplayFenceLimit       = 1024
	observationExecutionRecoveryGrace = 10 * time.Second

	ObservationExecutionStatusInFlight      = "in-flight"
	ObservationExecutionStatusPass          = "pass"
	ObservationExecutionStatusFail          = "fail"
	ObservationExecutionStatusIndeterminate = "indeterminate"
)


func (s *StateStore) initObservationDocument(taskID string) error {
	return s.writeObservationDocument(observationExecutionDocument{
		TaskID: taskID, Fence: []observationFenceIdentity{}, Records: []ObservationExecutionRecord{},
	})
}

func validateObservationDocument(doc observationExecutionDocument) error {
	if doc.TaskID == "" || doc.Round < 0 || doc.Fence == nil || doc.Records == nil ||
		len(doc.Fence) > observationReplayFenceLimit || doc.Round == 0 && len(doc.Fence) != 0 {
		return fmt.Errorf("observation current-format document is invalid")
	}
	fence, err := validateObservationFence(doc.Fence)
	if err != nil { return err }
	return validateObservationDetails(doc, fence)
}

func validateObservationFence(entries []observationFenceIdentity) (map[string]observationFenceIdentity, error) {
	ids := map[string]bool{}
	identities := map[string]bool{}
	fence := make(map[string]observationFenceIdentity, len(entries))
	for _, entry := range entries {
		key := entry.Operation + "\x00" + entry.ParamsDigest
		if entry.ExecutionID == "" || entry.Operation == "" || entry.ParamsDigest == "" ||
			ids[entry.ExecutionID] || identities[key] {
			return nil, fmt.Errorf("observation replay fence duplicate or invalid identity")
		}
		ids[entry.ExecutionID], identities[key] = true, true
		fence[entry.ExecutionID] = entry
	}
	return fence, nil
}

func validateObservationDetails(doc observationExecutionDocument, fence map[string]observationFenceIdentity) error {
	visible := map[string]bool{}
	terminals := 0
	for _, record := range doc.Records {
		if err := validateObservationDetailIdentity(doc, fence, visible, record); err != nil { return err }
		visible[record.ExecutionID] = true
		if record.Status == ObservationExecutionStatusInFlight {
			if record.DecisionRound != doc.Round || validateObservationInFlightRecord(record) != nil {
				return fmt.Errorf("observation in-flight record is invalid")
			}
		} else {
			terminals++
			if !validObservationTerminal(record) {
				return fmt.Errorf("observation terminal record is invalid")
			}
		}
	}
	if terminals > observationExecutionsRetention {
		return fmt.Errorf("observation terminal retention overflow")
	}
	return nil
}

func validateObservationDetailIdentity(
	doc observationExecutionDocument,
	fence map[string]observationFenceIdentity,
	visible map[string]bool,
	record ObservationExecutionRecord,
) error {
	if record.ExecutionID == "" || record.TaskID != doc.TaskID ||
		record.Operation == "" || record.ParamsDigest == "" ||
		record.DecisionRound < 0 || record.DecisionRound > doc.Round ||
		visible[record.ExecutionID] {
		return fmt.Errorf("observation record identity is invalid")
	}
	if record.DecisionRound == doc.Round {
		entry, ok := fence[record.ExecutionID]
		if !ok || entry.Operation != record.Operation || entry.ParamsDigest != record.ParamsDigest {
			return fmt.Errorf("observation record is missing replay fence")
		}
	}
	return nil
}

func validObservationTerminal(record ObservationExecutionRecord) bool {
	return (record.Status == ObservationExecutionStatusPass ||
		record.Status == ObservationExecutionStatusFail ||
		record.Status == ObservationExecutionStatusIndeterminate) && record.CompletedAtRFC3339 != ""
}

func (s *StateStore) readObservationDocument() (observationExecutionDocument, error) {
	data, err := os.ReadFile(s.Path(observationExecutionsStateFile))
	if err != nil { return observationExecutionDocument{}, fmt.Errorf("observation state unavailable: %w", err) }
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc observationExecutionDocument
	if err := dec.Decode(&doc); err != nil { return doc, fmt.Errorf("observation current-format decode: %w", err) }
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return doc, fmt.Errorf("observation document trailing data")
	}
	if err := validateObservationDocument(doc); err != nil { return doc, err }
	taskID, err := s.TaskID()
	if err != nil { return doc, err }
	if doc.TaskID != taskID { return doc, fmt.Errorf("observation document task mismatch") }
	return doc, nil
}

func (s *StateStore) writeObservationDocument(doc observationExecutionDocument) error {
	doc.Records = trimObservationExecutions(doc.Records)
	if err := validateObservationDocument(doc); err != nil { return err }
	data, err := json.Marshal(doc)
	if err != nil { return err }
	return s.Write(observationExecutionsStateFile, string(data))
}

func (doc *observationExecutionDocument) advanceObservationRound(round int) error {
	if round < doc.Round || round <= 0 { return fmt.Errorf("stale observation decision round") }
	if round == doc.Round { return nil }
	for _, record := range doc.Records {
		if record.Status == ObservationExecutionStatusInFlight {
			return fmt.Errorf("previous-round observation remains in-flight")
		}
	}
	doc.Round = round
	doc.Fence = []observationFenceIdentity{}
	return nil
}

func (doc *observationExecutionDocument) reserveObservationIdentity(record ObservationExecutionRecord) error {
	for _, entry := range doc.Fence {
		if entry.ExecutionID == record.ExecutionID {
			return fmt.Errorf("observation execution id %s already reserved", record.ExecutionID)
		}
		if entry.Operation == record.Operation && entry.ParamsDigest == record.ParamsDigest {
			return fmt.Errorf("同一decision round内の同一operation・parameter再実行はmachineが拒否します(%s)", record.Operation)
		}
	}
	for _, previous := range doc.Records {
		if previous.ExecutionID == record.ExecutionID { return fmt.Errorf("observation execution id still in record history") }
	}
	if len(doc.Fence) >= observationReplayFenceLimit {
		return fmt.Errorf("observation replay fence capacity exceeded (%d identities per task/round)", observationReplayFenceLimit)
	}
	doc.Fence = append(doc.Fence, observationFenceIdentity{record.ExecutionID,record.Operation,record.ParamsDigest})
	doc.Records = append(doc.Records, record)
	return nil
}

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

func (s *StateStore) ObservationExecutions() ([]ObservationExecutionRecord,error) {
	doc,err:=s.readObservationDocument()
	if err!=nil {return nil,err}
	return doc.Records,nil
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

func (s *StateStore) HasObservationExecution(operation,paramsDigest string,round int)(bool,error){
	current,err:=s.ObservationExecutionRound()
	if err!=nil {return false,err}
	if current!=round {return false,fmt.Errorf("observation request has noncanonical round")}
	doc,err:=s.readObservationDocument()
	if err!=nil {return false,err}
	if doc.Round>round {return false,fmt.Errorf("observation fence is ahead of canonical lifecycle")}
	if doc.Round<round {
		for _,record:=range doc.Records {
			if record.Status==ObservationExecutionStatusInFlight {
				return false,fmt.Errorf("old in-flight observation blocks round switch")
			}
		}
		return false,nil
	}
	for _,entry:=range doc.Fence {
		if entry.Operation==operation && entry.ParamsDigest==paramsDigest {return true,nil}
	}
	return false,nil
}

func (s *StateStore) RecoverStaleObservationExecutions(now time.Time) error {
	doc, err := s.readObservationDocument()
	if err != nil { return err }
	records := doc.Records
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
	doc.Records = records
	return s.writeObservationDocument(doc)
}

func (s *StateStore) BeginObservationExecution(record ObservationExecutionRecord)error{
	taskID,err:=s.TaskID();if err!=nil{return err}
	if record.TaskID=="" {record.TaskID=taskID}
	if record.TaskID!=taskID {return fmt.Errorf("observation in-flight task mismatch")}
	if err:=validateObservationInFlightRecord(record);err!=nil{return err}
	round,err:=s.ObservationExecutionRound();if err!=nil{return err}
	if round!=record.DecisionRound{return fmt.Errorf("observation request round is noncanonical")}
	doc,err:=s.readObservationDocument();if err!=nil{return err}
	if err:=doc.advanceObservationRound(round);err!=nil{return err}
	if err:=doc.reserveObservationIdentity(record);err!=nil{return err}
	return s.writeObservationDocument(doc)
}

func (s *StateStore) CompleteObservationExecution(record ObservationExecutionRecord) error {
	taskID, err := s.TaskID()
	if err != nil { return err }
	if record.TaskID != taskID { return fmt.Errorf("observation completion task mismatch") }
	if err := validateObservationExecutionRecord(record, s.ArtifactDir(taskID)); err != nil { return err }
	round, err := s.ObservationExecutionRound()
	if err != nil { return err }
	if round != record.DecisionRound { return fmt.Errorf("observation completion round changed") }
	doc, err := s.readObservationDocument()
	if err != nil { return err }
	if doc.Round != round { return fmt.Errorf("observation completion fence round mismatch") }
	return s.completeObservationInDocument(doc, record)
}

func (s *StateStore) completeObservationInDocument(doc observationExecutionDocument, record ObservationExecutionRecord) error {
	for index, existing := range doc.Records {
		if existing.ExecutionID != record.ExecutionID { continue }
		if existing.Status != ObservationExecutionStatusInFlight || !sameObservationExecutionIdentity(existing, record) {
			return fmt.Errorf("observation completion does not match durable in-flight identity")
		}
		doc.Records[index] = record
		if err := s.writeObservationDocument(doc); err != nil { return err }
		return s.SecureArtifactDir()
	}
	return fmt.Errorf("observation completion lacks durable in-flight identity")
}

func (s *StateStore) AppendObservationExecution(record ObservationExecutionRecord)error{
	taskID,err:=s.TaskID();if err!=nil{return err}
	if record.TaskID==""{record.TaskID=taskID}
	if record.TaskID!=taskID{return fmt.Errorf("observation append task mismatch")}
	if err:=validateObservationExecutionRecord(record,s.ArtifactDir(taskID));err!=nil{return err}
	round,err:=s.ObservationExecutionRound();if err!=nil{return err}
	if round!=record.DecisionRound{return fmt.Errorf("observation append round noncanonical")}
	doc,err:=s.readObservationDocument();if err!=nil{return err}
	if err:=doc.advanceObservationRound(round);err!=nil{return err}
	if err:=doc.reserveObservationIdentity(record);err!=nil{return err}
	if err:=s.writeObservationDocument(doc);err!=nil{return err}
	return s.SecureArtifactDir()
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
