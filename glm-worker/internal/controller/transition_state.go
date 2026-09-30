package controller

import (
	"fmt"
	"time"
)

func (*Store) ClassifyTransition(record TransitionRecord, actual map[string]string) map[string]EffectClassification {
	result := make(map[string]EffectClassification, len(record.Effects))
	for _, effect := range record.Effects {
		observed := actual[effect.Key()]
		switch observed {
		case effect.ExpectedOld:
			result[effect.Key()] = EffectExpectedOld
		case effect.ExpectedNew:
			result[effect.Key()] = EffectExpectedNew
		default:
			result[effect.Key()] = EffectUnexpected
		}
	}
	return result
}

func (s *Store) markTransitionApplied(record TransitionRecord, actual map[string]string) error {
	stateRecord, err := s.loadTransitionState(record.TransitionID)
	if err != nil {
		return err
	}
	if stateRecord.Phase != TransitionPhasePrepared && stateRecord.Phase != TransitionPhaseApplied {
		return fmt.Errorf("transition %s cannot be marked applied from %s", record.TransitionID, stateRecord.Phase)
	}
	stateRecord.Phase = TransitionPhaseApplied
	stateRecord.Observed = cloneMap(actual)
	stateRecord.Classifications = s.ClassifyTransition(record, actual)
	stateRecord.UpdatedAt = time.Now().UTC()
	return s.writeTransitionState(stateRecord)
}

func (s *Store) failClosedLocked(
	reason string,
	transitionID string,
	workspace WorkspaceIdentity,
	expected WorkspaceSnapshot,
	actual WorkspaceSnapshot,
	observed map[string]string,
) (FailureRecord, error) {
	head, err := s.LoadHead()
	if err != nil {
		return FailureRecord{}, err
	}
	failureID, err := newControllerID()
	if err != nil {
		return FailureRecord{}, err
	}
	failure := FailureRecord{
		SchemaVersion: controllerSchemaVersion,
		FailureID:     failureID,
		Reason:        reason,
		TransitionID:  transitionID,
		Generation:    head.ControllerGeneration,
		WorkspaceID:   workspace.ID,
		Expected:      expected,
		Actual:        actual,
		Observed:      cloneMap(observed),
		CreatedAt:     time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.failurePath(failureID), failure); err != nil {
		return FailureRecord{}, err
	}
	next := head
	next.ControllerGeneration++
	next.Status = ControllerStatusFailClosed
	next.FailureID = failureID
	next.LiveLeaseID = ""
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return FailureRecord{}, err
	}
	if transitionID != "" {
		stateRecord, loadErr := s.loadTransitionState(transitionID)
		if loadErr == nil {
			stateRecord.Phase = TransitionPhaseFailed
			stateRecord.Observed = cloneMap(observed)
			stateRecord.UpdatedAt = time.Now().UTC()
			_ = s.writeTransitionState(stateRecord)
		}
	}
	return failure, nil
}

func (s *Store) LoadTransition(id string) (TransitionRecord, TransitionState, error) {
	var record TransitionRecord
	if err := readJSON(s.transitionPath(id), &record); err != nil {
		return TransitionRecord{}, TransitionState{}, err
	}
	transitionState, err := s.loadTransitionState(id)
	if err != nil {
		return TransitionRecord{}, TransitionState{}, err
	}
	return record, transitionState, nil
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
