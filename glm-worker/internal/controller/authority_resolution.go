package controller

import (
	"fmt"
	"time"
)

func (s *Store) commitAuthorityTransitionLocked(
	record TransitionRecord,
	actual map[string]string,
	finalize bool,
	mutate func(*RepositoryControllerHead) error,
) (RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	if head.PendingTransitionID != record.TransitionID || head.ControllerGeneration != record.PreparedGeneration {
		return RepositoryControllerHead{}, fmt.Errorf("transition %s no longer owns controller CAS", record.TransitionID)
	}
	classifications := s.ClassifyTransition(record, actual)
	for _, classification := range classifications {
		if classification != EffectExpectedNew {
			return RepositoryControllerHead{}, fmt.Errorf("transition %s target effects are not proven", record.TransitionID)
		}
	}
	next := head
	if mutate != nil {
		if err := mutate(&next); err != nil {
			return RepositoryControllerHead{}, err
		}
	}
	phase := TransitionPhaseCommitted
	if finalize {
		next.ControllerGeneration = record.TargetGeneration
		next.PendingTransitionID = ""
		phase = TransitionPhaseFinalized
	} else {
		next.ControllerGeneration = record.CommittedGeneration
	}
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return RepositoryControllerHead{}, err
	}
	transitionState := TransitionState{
		SchemaVersion:   controllerSchemaVersion,
		TransitionID:    record.TransitionID,
		Phase:           phase,
		Observed:        cloneMap(actual),
		Classifications: classifications,
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.writeTransitionState(transitionState); err != nil {
		return RepositoryControllerHead{}, err
	}
	return next, nil
}

func (s *Store) finalizeAuthorityTransitionLocked(record TransitionRecord) (RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	if head.PendingTransitionID != record.TransitionID || head.ControllerGeneration != record.CommittedGeneration {
		return RepositoryControllerHead{}, fmt.Errorf("transition %s is not pending finalization", record.TransitionID)
	}
	stateRecord, err := s.loadTransitionState(record.TransitionID)
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	if stateRecord.Phase != TransitionPhaseCommitted && stateRecord.Phase != TransitionPhaseFinalizing {
		return RepositoryControllerHead{}, fmt.Errorf("transition %s cannot finalize from %s", record.TransitionID, stateRecord.Phase)
	}
	stateRecord.Phase = TransitionPhaseFinalizing
	stateRecord.UpdatedAt = time.Now().UTC()
	if err := s.writeTransitionState(stateRecord); err != nil {
		return RepositoryControllerHead{}, err
	}
	next := head
	next.ControllerGeneration = record.TargetGeneration
	next.PendingTransitionID = ""
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return RepositoryControllerHead{}, err
	}
	stateRecord.Phase = TransitionPhaseFinalized
	stateRecord.UpdatedAt = time.Now().UTC()
	if err := s.writeTransitionState(stateRecord); err != nil {
		return RepositoryControllerHead{}, err
	}
	return next, nil
}
