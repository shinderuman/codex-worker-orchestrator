package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type ResumeTransitionEvidence struct {
	Version          int            `json:"version"`
	TaskID           string         `json:"task_id"`
	AttemptID        string         `json:"attempt_id"`
	CheckpointDigest string         `json:"checkpoint_digest"`
	StopKind         ResumeStopKind `json:"stop_kind"`
	Phase            string         `json:"phase"`
	ObservedAt       time.Time      `json:"observed_at"`
}

const (
	GuardRepairResumeAttemptEnv = "GLM_WORKER_GUARD_REPAIR_RESUME_ATTEMPT"
	resumeTransitionFile        = "resume-transition.json"
	resumeTransitionVersion     = 1
)

var ErrNoResumeTransitionEvidence = errors.New("resume transition evidence is not available")

func (s *StateStore) BeginResumeWithEvidence(checkpoint ResumeCheckpoint, attemptID string) error {
	evidence, err := s.expectedResumeTransitionEvidence(checkpoint, attemptID)
	if err != nil {
		return err
	}
	if err := s.BeginResume(checkpoint); err != nil {
		return err
	}
	if err := s.saveResumeTransitionEvidence(evidence); err != nil {
		return errors.Join(
			fmt.Errorf("resume transition evidenceを書き込めません: %w", err),
			s.RestoreResumeStop(checkpoint),
		)
	}
	return nil
}

func (s *StateStore) VerifyResumeTransitionEvidence(taskID, attemptID string, checkpoint ResumeCheckpoint) error {
	expected, err := s.expectedResumeTransitionEvidence(checkpoint, attemptID)
	if err != nil {
		return err
	}
	if expected.TaskID != taskID {
		return fmt.Errorf("resume transition expected task mismatch: current=%s expected=%s", expected.TaskID, taskID)
	}
	observed, err := s.loadResumeTransitionEvidence()
	if err != nil {
		return err
	}
	if observed.TaskID != expected.TaskID || observed.AttemptID != expected.AttemptID ||
		observed.CheckpointDigest != expected.CheckpointDigest || observed.StopKind != expected.StopKind || observed.Phase != expected.Phase {
		return fmt.Errorf("resume transition evidence does not match the expected attempt")
	}
	return nil
}

func (s *StateStore) expectedResumeTransitionEvidence(checkpoint ResumeCheckpoint, attemptID string) (ResumeTransitionEvidence, error) {
	if !ValidGeneratedUUID(attemptID) {
		return ResumeTransitionEvidence{}, fmt.Errorf("resume transition attempt IDが不正です")
	}
	if !checkpoint.IsStopped() {
		return ResumeTransitionEvidence{}, fmt.Errorf("resume transition evidence requires stopped checkpoint")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return ResumeTransitionEvidence{}, err
	}
	digest, err := resumeTransitionCheckpointDigest(checkpoint)
	if err != nil {
		return ResumeTransitionEvidence{}, err
	}
	return ResumeTransitionEvidence{
		Version:          resumeTransitionVersion,
		TaskID:           taskID,
		AttemptID:        attemptID,
		CheckpointDigest: digest,
		StopKind:         checkpoint.StopKind,
		Phase:            checkpoint.Phase,
	}, nil
}

func resumeTransitionCheckpointDigest(checkpoint ResumeCheckpoint) (string, error) {
	checkpoint.Version = resumeStateVersion
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return "", fmt.Errorf("resume transition checkpointをJSON化できません: %w", err)
	}
	var canonical any
	if err := json.Unmarshal(data, &canonical); err != nil {
		return "", fmt.Errorf("resume transition checkpointをcanonical化できません: %w", err)
	}
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("resume transition checkpointをcanonical化できません: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (s *StateStore) saveResumeTransitionEvidence(evidence ResumeTransitionEvidence) error {
	if err := evidence.validate(); err != nil {
		return err
	}
	evidence.ObservedAt = time.Now().UTC()
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("resume transition evidenceをJSON化できません: %w", err)
	}
	return writeFileAtomic(s.Path(resumeTransitionFile), append(data, '\n'), 0o600)
}

func (s *StateStore) loadResumeTransitionEvidence() (ResumeTransitionEvidence, error) {
	data, err := os.ReadFile(s.Path(resumeTransitionFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ResumeTransitionEvidence{}, ErrNoResumeTransitionEvidence
		}
		return ResumeTransitionEvidence{}, err
	}
	var evidence ResumeTransitionEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return ResumeTransitionEvidence{}, fmt.Errorf("resume transition evidenceを読めません: %w", err)
	}
	if err := evidence.validate(); err != nil {
		return ResumeTransitionEvidence{}, err
	}
	return evidence, nil
}

func (evidence ResumeTransitionEvidence) validate() error {
	if evidence.Version != resumeTransitionVersion {
		return fmt.Errorf("unsupported resume transition evidence version: %d", evidence.Version)
	}
	if !ValidGeneratedUUID(evidence.TaskID) || !ValidGeneratedUUID(evidence.AttemptID) || evidence.CheckpointDigest == "" ||
		!evidence.StopKind.IsStopped() || evidence.Phase == "" {
		return fmt.Errorf("resume transition evidenceが不完全です")
	}
	return nil
}
