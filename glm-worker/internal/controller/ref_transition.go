package controller

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) ApplyRefTransition(
	admission Admission,
	kind string,
	refName string,
	expectedOld string,
	expectedNew string,
	command string,
	apply func() error,
) (Admission, error) {
	current, err := s.AdmitMutation(admission.Lease.SemanticTaskRef, admission.Workspace, admission.Snapshot)
	if err != nil {
		return Admission{}, err
	}
	if current.Lease.LeaseID != admission.Lease.LeaseID {
		return Admission{}, fmt.Errorf("execution lease changed before ref transition")
	}
	predicted, err := PredictRefTransitionSnapshot(admission.Workspace.Root, admission.Snapshot, refName, expectedOld, expectedNew)
	if err != nil {
		return Admission{}, err
	}
	effect := EffectExpectation{
		Surface:     MutationSurfaceRef,
		Resource:    refName,
		ExpectedOld: expectedOld,
		ExpectedNew: expectedNew,
	}
	record, err := s.BeginTransition(kind, current.Head.ControllerGeneration, []EffectExpectation{effect})
	if err != nil {
		return Admission{}, err
	}
	persisted, transitionState, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return Admission{}, err
	}
	if persisted.TransitionID != record.TransitionID || transitionState.Phase != TransitionPhasePrepared {
		return Admission{}, fmt.Errorf("ref transition prepare record is not durable")
	}

	observed, err := observeRefEffect(admission.Workspace.Root, effect)
	if err != nil {
		return Admission{}, err
	}
	classification := s.ClassifyTransition(record, observed)[effect.Key()]
	if classification == EffectUnexpected {
		return Admission{}, s.failClosedRefTransition(record, admission, observed, "ref state changed before apply")
	}
	if classification == EffectExpectedOld {
		applyErr := apply()
		observed, err = observeRefEffect(admission.Workspace.Root, effect)
		if err != nil {
			return Admission{}, err
		}
		classification = s.ClassifyTransition(record, observed)[effect.Key()]
		if classification == EffectExpectedOld {
			return Admission{}, s.abortRefTransition(record, admission, observed, command, applyErr)
		}
		if classification == EffectUnexpected {
			return Admission{}, s.failClosedRefTransition(record, admission, observed, "ref state became unexpected after apply")
		}
	}

	after, err := CaptureWorkspaceSnapshot(admission.Workspace.Root)
	if err != nil {
		return Admission{}, err
	}
	if after.ID != predicted.ID {
		return Admission{}, s.failClosedRefTransition(record, admission, observed, "ref transition reached unexpected workspace snapshot")
	}
	if err := s.MarkTransitionApplied(record, observed); err != nil {
		return Admission{}, err
	}
	if _, err := s.CommitTransition(record, observed, false, nil); err != nil {
		return Admission{}, err
	}
	if _, err := s.FinalizeTransition(record); err != nil {
		return Admission{}, err
	}
	return s.rotateLeaseAfterTransition(admission, command, "success", after)
}

func (s *Store) RecordGuardedMutation(admission Admission, command, outcome string, after WorkspaceSnapshot) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.ControllerGeneration == admission.Head.ControllerGeneration {
		return s.RecordMutation(admission, command, outcome, after)
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" || head.LiveLeaseID == "" {
		return Admission{}, fmt.Errorf("controller changed without a finalized live lease")
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return Admission{}, err
	}
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return Admission{}, err
	}
	if attempt.AttemptID != admission.Attempt.AttemptID || lease.AttemptID != admission.Attempt.AttemptID ||
		!lease.SemanticTaskRef.Equal(admission.Lease.SemanticTaskRef) || lease.WorkspaceID != admission.Workspace.ID ||
		lease.ControllerGeneration != head.ControllerGeneration || lease.ExpectedWorkspaceSnapshotID != after.ID || lease.ExpectedBaseOID != after.Head {
		return Admission{}, fmt.Errorf("controller changed without matching transitioned mutation provenance")
	}
	return Admission{Head: head, Attempt: attempt, Lease: lease, Workspace: admission.Workspace, Snapshot: after}, nil
}

func (s *Store) rotateLeaseAfterTransition(admission Admission, command, outcome string, after WorkspaceSnapshot) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" || head.LiveLeaseID != admission.Lease.LeaseID || head.LiveAttemptID != admission.Attempt.AttemptID {
		return Admission{}, fmt.Errorf("transition finalization no longer owns the live execution lease")
	}
	leaseID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	mutationID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	nextGeneration := head.ControllerGeneration + 1
	nextLease := admission.Lease
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
	return Admission{Head: nextHead, Attempt: admission.Attempt, Lease: nextLease, Workspace: admission.Workspace, Snapshot: after}, nil
}

func (s *Store) abortRefTransition(
	record TransitionRecord,
	admission Admission,
	observed map[string]string,
	command string,
	applyErr error,
) error {
	if err := s.abortPreparedTransition(record, observed); err != nil {
		if applyErr != nil {
			return fmt.Errorf("ref transition apply failed: %w; abort failed: %v", applyErr, err)
		}
		return err
	}
	if _, err := s.rotateLeaseAfterTransition(admission, command, "aborted", admission.Snapshot); err != nil {
		if applyErr != nil {
			return fmt.Errorf("ref transition apply failed: %w; lease refresh failed: %v", applyErr, err)
		}
		return err
	}
	if applyErr != nil {
		return applyErr
	}
	return fmt.Errorf("ref transition did not reach expected-new state")
}

func (s *Store) abortPreparedTransition(record TransitionRecord, observed map[string]string) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	if head.PendingTransitionID != record.TransitionID || head.ControllerGeneration != record.PreparedGeneration {
		return fmt.Errorf("transition %s cannot abort from current controller state", record.TransitionID)
	}
	next := head
	next.ControllerGeneration = record.TargetGeneration
	next.PendingTransitionID = ""
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return err
	}
	transitionState, err := s.loadTransitionState(record.TransitionID)
	if err != nil {
		return err
	}
	transitionState.Phase = TransitionPhaseAborted
	transitionState.Observed = cloneMap(observed)
	transitionState.Classifications = s.ClassifyTransition(record, observed)
	transitionState.UpdatedAt = time.Now().UTC()
	return s.writeTransitionState(transitionState)
}

func (s *Store) failClosedRefTransition(record TransitionRecord, admission Admission, observed map[string]string, reason string) error {
	actual, captureErr := CaptureWorkspaceSnapshot(admission.Workspace.Root)
	if captureErr != nil {
		actual = admission.Snapshot
	}
	_, failErr := s.FailClosed(reason, record.TransitionID, admission.Workspace, admission.Snapshot, actual, observed)
	if failErr != nil {
		return fmt.Errorf("%s; fail-close failed: %w", reason, failErr)
	}
	return fmt.Errorf("%s", reason)
}

func observeRefEffect(repoRoot string, effect EffectExpectation) (map[string]string, error) {
	command := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", effect.Resource)
	output, err := command.Output()
	value := strings.TrimSpace(string(output))
	if err != nil {
		value = ""
	}
	return map[string]string{effect.Key(): value}, nil
}
