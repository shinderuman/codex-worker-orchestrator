package controller

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) CancelTransition(record TransitionRecord, actual map[string]string) (RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	if head.PendingTransitionID != record.TransitionID || head.ControllerGeneration != record.PreparedGeneration {
		return RepositoryControllerHead{}, fmt.Errorf("transition %s no longer owns controller CAS", record.TransitionID)
	}
	classifications := s.ClassifyTransition(record, actual)
	for _, classification := range classifications {
		if classification != EffectExpectedOld {
			return RepositoryControllerHead{}, fmt.Errorf("transition %s no-effect cancellation is not proven", record.TransitionID)
		}
	}
	next := head
	next.ControllerGeneration = record.TargetGeneration
	next.PendingTransitionID = ""
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return RepositoryControllerHead{}, err
	}
	transitionState := TransitionState{
		SchemaVersion:    controllerSchemaVersion,
		TransitionID:    record.TransitionID,
		Phase:           TransitionPhaseFinalized,
		Observed:        cloneMap(actual),
		Classifications: classifications,
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.writeTransitionState(transitionState); err != nil {
		return RepositoryControllerHead{}, err
	}
	return next, nil
}

func (s *Store) RecordTransitionMutation(
	admission Admission,
	command string,
	outcome string,
	after WorkspaceSnapshot,
) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" {
		return Admission{}, fmt.Errorf("repository controller transition is not finalized")
	}
	if head.ControllerGeneration <= admission.Head.ControllerGeneration {
		return Admission{}, fmt.Errorf("repository controller generation did not advance through a transition")
	}
	if head.LiveLeaseID != admission.Lease.LeaseID || head.LiveAttemptID != admission.Attempt.AttemptID {
		return Admission{}, fmt.Errorf("controller-owned transition changed live attempt or lease identity")
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return Admission{}, err
	}
	if lease.ControllerGeneration != admission.Head.ControllerGeneration || lease.WorkspaceID != admission.Workspace.ID || !lease.SemanticTaskRef.Equal(admission.Lease.SemanticTaskRef) {
		return Admission{}, fmt.Errorf("controller-owned transition source lease no longer matches the admitted mutation")
	}

	mutationID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	leaseID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	nextGeneration := head.ControllerGeneration + 1
	nextLease := lease
	nextLease.LeaseID = leaseID
	nextLease.ControllerGeneration = nextGeneration
	nextLease.ExpectedBaseOID = after.Head
	nextLease.ExpectedWorkspaceSnapshotID = after.ID
	nextLease.InFlightCallID = ""
	nextLease.CreatedAt = time.Now().UTC()
	record := MutationRecord{
		SchemaVersion:    controllerSchemaVersion,
		MutationID:       mutationID,
		AttemptID:        admission.Attempt.AttemptID,
		SourceLeaseID:    admission.Lease.LeaseID,
		TargetLeaseID:    nextLease.LeaseID,
		SourceGeneration: admission.Head.ControllerGeneration,
		TargetGeneration: nextGeneration,
		Command:          command,
		Outcome:          outcome,
		Before:           admission.Snapshot,
		After:            after,
		Surfaces:         changedSurfaces(admission.Snapshot, after),
		CreatedAt:        time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.mutationPath(mutationID), record); err != nil {
		return Admission{}, err
	}
	if err := s.writeLease(nextLease); err != nil {
		return Admission{}, err
	}
	nextHead := head
	nextHead.ControllerGeneration = nextGeneration
	nextHead.LiveLeaseID = nextLease.LeaseID
	if err := s.writeHeadCAS(head.ControllerGeneration, nextHead); err != nil {
		return Admission{}, err
	}
	return Admission{
		Head:      nextHead,
		Attempt:   admission.Attempt,
		Lease:     nextLease,
		Workspace: admission.Workspace,
		Snapshot:  after,
	}, nil
}
