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
		SchemaVersion:   controllerSchemaVersion,
		TransitionID:    record.TransitionID,
		Phase:           TransitionPhaseAborted,
		Observed:        cloneMap(actual),
		Classifications: classifications,
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.writeTransitionState(transitionState); err != nil {
		return RepositoryControllerHead{}, err
	}
	return next, nil
}

func (s *Store) RecordGuardedMutation(
	admission Admission,
	command string,
	outcome string,
	after WorkspaceSnapshot,
) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.ControllerGeneration == admission.Head.ControllerGeneration {
		return s.RecordMutation(admission, command, outcome, after)
	}
	if err := validateFinalizedLiveHead(head); err != nil {
		return Admission{}, err
	}
	if head.LiveLeaseID == admission.Lease.LeaseID {
		return s.RecordTransitionMutation(admission, command, outcome, after)
	}
	return s.reconciledGuardedAdmission(head, admission, after)
}

func validateFinalizedLiveHead(head RepositoryControllerHead) error {
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" || head.LiveLeaseID == "" || head.LiveAttemptID == "" {
		return fmt.Errorf("controller changed without a finalized live lease")
	}
	return nil
}

func (s *Store) reconciledGuardedAdmission(
	head RepositoryControllerHead,
	admission Admission,
	after WorkspaceSnapshot,
) (Admission, error) {
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return Admission{}, err
	}
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return Admission{}, err
	}
	if !reconciledMutationMatches(head, admission, attempt, lease, after) {
		return Admission{}, fmt.Errorf("controller changed without matching transitioned mutation provenance")
	}
	return Admission{Head: head, Attempt: attempt, Lease: lease, Workspace: admission.Workspace, Snapshot: after}, nil
}

func reconciledMutationMatches(
	head RepositoryControllerHead,
	admission Admission,
	attempt AttemptRecord,
	lease ExecutionLease,
	after WorkspaceSnapshot,
) bool {
	return attempt.AttemptID == admission.Attempt.AttemptID &&
		lease.AttemptID == admission.Attempt.AttemptID &&
		lease.SemanticTaskRef.Equal(admission.Lease.SemanticTaskRef) &&
		lease.WorkspaceID == admission.Workspace.ID &&
		lease.ControllerGeneration == head.ControllerGeneration &&
		lease.ExpectedWorkspaceSnapshotID == after.ID &&
		lease.ExpectedBaseOID == after.Head
}

func (s *Store) RecordTransitionMutation(
	admission Admission,
	command string,
	outcome string,
	after WorkspaceSnapshot,
) (Admission, error) {
	head, lease, err := s.transitionMutationSource(admission)
	if err != nil {
		return Admission{}, err
	}
	return s.persistTransitionMutation(admission, head, lease, command, outcome, after)
}

func (s *Store) transitionMutationSource(admission Admission) (RepositoryControllerHead, ExecutionLease, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, ExecutionLease{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" {
		return RepositoryControllerHead{}, ExecutionLease{}, fmt.Errorf("repository controller transition is not finalized")
	}
	if head.ControllerGeneration <= admission.Head.ControllerGeneration {
		return RepositoryControllerHead{}, ExecutionLease{}, fmt.Errorf("repository controller generation did not advance through a transition")
	}
	if head.LiveLeaseID != admission.Lease.LeaseID || head.LiveAttemptID != admission.Attempt.AttemptID {
		return RepositoryControllerHead{}, ExecutionLease{}, fmt.Errorf("controller-owned transition changed live attempt or lease identity")
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return RepositoryControllerHead{}, ExecutionLease{}, err
	}
	if !transitionSourceLeaseMatches(admission, lease) {
		return RepositoryControllerHead{}, ExecutionLease{}, fmt.Errorf("controller-owned transition source lease no longer matches the admitted mutation")
	}
	return head, lease, nil
}

func transitionSourceLeaseMatches(admission Admission, lease ExecutionLease) bool {
	return lease.ControllerGeneration == admission.Head.ControllerGeneration &&
		lease.WorkspaceID == admission.Workspace.ID &&
		lease.SemanticTaskRef.Equal(admission.Lease.SemanticTaskRef)
}

func (s *Store) persistTransitionMutation(
	admission Admission,
	head RepositoryControllerHead,
	lease ExecutionLease,
	command string,
	outcome string,
	after WorkspaceSnapshot,
) (Admission, error) {
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
