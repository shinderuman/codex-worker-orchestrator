package state

import (
	"fmt"
	"time"
)

type SessionRotationCreationResult struct {
	Version        int    `json:"version"`
	Outcome        string `json:"outcome"`
	ParentThreadID string `json:"parent_thread_id"`
	DirectiveID    string `json:"directive_id"`
	ClaimID        string `json:"claim_id"`
	TargetTaskID   string `json:"target_task_id"`
}

type SessionRotationCreationOutcome struct {
	Version        int    `json:"version"`
	Outcome        string `json:"outcome"`
	ParentThreadID string `json:"parent_thread_id"`
	DirectiveID    string `json:"directive_id"`
	ClaimID        string `json:"claim_id"`
	TargetTaskID   string `json:"target_task_id"`
	RecordedAt     string `json:"recorded_at"`
}

const (
	SessionRotationCreationResultVersion = 1
	SessionRotationCreationOutcomeFailed = "failed"
)

func (marker *SessionRotationMarker) validateCreationOutcome() error {
	if marker.LastCreationOutcome == nil {
		return nil
	}
	if marker.Directive == nil {
		return fmt.Errorf("session rotation creation outcomeにdirectiveがありません")
	}
	if marker.LastCreationOutcome.DirectiveID != marker.Directive.DirectiveID {
		return fmt.Errorf("session rotation creation outcomeがcurrent directiveと一致しません")
	}
	return marker.LastCreationOutcome.validate()
}

func (result SessionRotationCreationResult) validateForRelease(parentThreadID, directiveID, claimID, targetTaskID string) error {
	if err := validateSessionRotationCreationIdentity(result.Version, result.Outcome, result.ParentThreadID, result.DirectiveID, result.ClaimID, result.TargetTaskID); err != nil {
		return err
	}
	if result.ParentThreadID != parentThreadID || result.DirectiveID != directiveID || result.ClaimID != claimID || result.TargetTaskID != targetTaskID {
		return fmt.Errorf("session rotation creation resultがcurrent claim identityと一致しません")
	}
	return nil
}

func validateSessionRotationCreationIdentity(version int, outcome, parentThreadID, directiveID, claimID, targetTaskID string) error {
	if version != SessionRotationCreationResultVersion {
		return fmt.Errorf("session rotation creation result versionが未対応です: %d", version)
	}
	if outcome != SessionRotationCreationOutcomeFailed {
		return fmt.Errorf("session rotation creation outcomeはconfirmed failureではありません: %q", outcome)
	}
	if !ValidUUIDFormat(parentThreadID) || !ValidGeneratedUUID(directiveID) || !ValidGeneratedUUID(claimID) || !ValidGeneratedUUID(targetTaskID) {
		return fmt.Errorf("session rotation creation resultのidentityが不正です")
	}
	return nil
}

func newSessionRotationCreationOutcome(result SessionRotationCreationResult) *SessionRotationCreationOutcome {
	return &SessionRotationCreationOutcome{
		Version:        result.Version,
		Outcome:        result.Outcome,
		ParentThreadID: result.ParentThreadID,
		DirectiveID:    result.DirectiveID,
		ClaimID:        result.ClaimID,
		TargetTaskID:   result.TargetTaskID,
		RecordedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (outcome *SessionRotationCreationOutcome) validate() error {
	if outcome == nil {
		return nil
	}
	if err := validateSessionRotationCreationIdentity(outcome.Version, outcome.Outcome, outcome.ParentThreadID, outcome.DirectiveID, outcome.ClaimID, outcome.TargetTaskID); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, outcome.RecordedAt); err != nil {
		return fmt.Errorf("session rotation creation outcomeのrecorded_atが不正です: %w", err)
	}
	return nil
}
