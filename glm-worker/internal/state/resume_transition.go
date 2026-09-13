package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const GuardRepairResumeAttemptEnv = "GLM_WORKER_GUARD_REPAIR_RESUME_ATTEMPT"

func (s *StateStore) PrepareGuardRepairResume(record GuardRepairRecord, checkpoint ResumeCheckpoint, attemptID string) (GuardRepairRecord, error) {
	if record.Status != GuardRepairReady {
		return record, fmt.Errorf("guard repair resume requires ready transaction, got %q", record.Status)
	}
	if !ValidGeneratedUUID(attemptID) {
		return record, fmt.Errorf("guard repair resume attempt IDが不正です")
	}
	if checkpoint.StopKind != ResumeStopGuardRecoverable || checkpoint.Phase != record.Phase {
		return record, fmt.Errorf("guard repair resume checkpoint does not match transaction")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return record, err
	}
	if taskID != record.TaskID {
		return record, fmt.Errorf("guard repair resume transaction belongs to a different task")
	}
	digest, err := resumeTransitionCheckpointDigest(checkpoint)
	if err != nil {
		return record, err
	}
	record.Status = GuardRepairResuming
	record.ResumeAttemptID = attemptID
	record.ResumeCheckpointDigest = digest
	record.OriginalResumeObserved = false
	if err := s.SaveGuardRepairRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *StateStore) ObserveGuardRepairResume(checkpoint ResumeCheckpoint, attemptID string) error {
	record, err := s.LoadGuardRepairRecord()
	if err != nil {
		return err
	}
	if err := validateGuardRepairResumeProof(record, checkpoint, attemptID); err != nil {
		return err
	}
	if record.OriginalResumeObserved {
		return fmt.Errorf("guard repair resume attempt was already observed")
	}
	if err := s.BeginResume(checkpoint); err != nil {
		return err
	}
	record.OriginalResumeObserved = true
	if err := s.SaveGuardRepairRecord(record); err != nil {
		return errors.Join(
			fmt.Errorf("guard repair resume evidenceを書き込めません: %w", err),
			s.RestoreResumeStop(checkpoint),
		)
	}
	return nil
}

func (s *StateStore) VerifyGuardRepairResume(taskID, attemptID string, checkpoint ResumeCheckpoint) (GuardRepairRecord, error) {
	record, err := s.LoadGuardRepairRecord()
	if err != nil {
		return GuardRepairRecord{}, err
	}
	if record.TaskID != taskID {
		return GuardRepairRecord{}, fmt.Errorf("guard repair resume expected task mismatch: current=%s expected=%s", record.TaskID, taskID)
	}
	if err := validateGuardRepairResumeProof(record, checkpoint, attemptID); err != nil {
		return GuardRepairRecord{}, err
	}
	if !record.OriginalResumeObserved {
		return GuardRepairRecord{}, fmt.Errorf("guard repair resume attempt did not enter original resume lifecycle")
	}
	return record, nil
}

func validateGuardRepairResumeProof(record GuardRepairRecord, checkpoint ResumeCheckpoint, attemptID string) error {
	if record.Status != GuardRepairResuming {
		return fmt.Errorf("guard repair transaction is not resuming")
	}
	if !ValidGeneratedUUID(attemptID) || record.ResumeAttemptID != attemptID {
		return fmt.Errorf("guard repair resume attempt does not match transaction")
	}
	if checkpoint.StopKind != ResumeStopGuardRecoverable || checkpoint.Phase != record.Phase {
		return fmt.Errorf("guard repair resume checkpoint does not match transaction")
	}
	digest, err := resumeTransitionCheckpointDigest(checkpoint)
	if err != nil {
		return err
	}
	if digest != record.ResumeCheckpointDigest {
		return fmt.Errorf("guard repair resume checkpoint digest does not match transaction")
	}
	return nil
}

func resumeTransitionCheckpointDigest(checkpoint ResumeCheckpoint) (string, error) {
	checkpoint.Version = resumeStateVersion
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return "", fmt.Errorf("guard repair resume checkpointをJSON化できません: %w", err)
	}
	var canonical any
	if err := json.Unmarshal(data, &canonical); err != nil {
		return "", fmt.Errorf("guard repair resume checkpointをcanonical化できません: %w", err)
	}
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("guard repair resume checkpointをcanonical化できません: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
