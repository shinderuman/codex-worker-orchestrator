package controller

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type refTransitionObservation struct {
	values         map[string]string
	classification EffectClassification
	applyErr       error
}

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
	predicted, effect, record, err := s.prepareRefTransition(admission, current, kind, refName, expectedOld, expectedNew)
	if err != nil {
		return Admission{}, err
	}
	observation, err := s.applyRefTransitionEffect(record, effect, admission, apply)
	if err != nil {
		return Admission{}, err
	}
	if observation.classification == EffectExpectedOld {
		return Admission{}, s.abortRefTransition(record, admission, observation.values, command, observation.applyErr)
	}
	return s.commitRefTransition(record, admission, observation, predicted, command)
}

func (s *Store) prepareRefTransition(
	admission Admission,
	current Admission,
	kind string,
	refName string,
	expectedOld string,
	expectedNew string,
) (WorkspaceSnapshot, EffectExpectation, TransitionRecord, error) {
	predicted, err := PredictRefTransitionSnapshot(admission.Workspace.Root, admission.Snapshot, refName, expectedOld, expectedNew)
	if err != nil {
		return WorkspaceSnapshot{}, EffectExpectation{}, TransitionRecord{}, err
	}
	effect := EffectExpectation{
		Surface:     MutationSurfaceRef,
		Resource:    refName,
		ExpectedOld: expectedOld,
		ExpectedNew: expectedNew,
	}
	targetLease, err := s.prepareContinuationLease(current.Lease, predicted, current.Head.ControllerGeneration+3)
	if err != nil {
		return WorkspaceSnapshot{}, EffectExpectation{}, TransitionRecord{}, err
	}
	target := authorityFromAdmission(current, predicted)
	target.LeaseID = targetLease.LeaseID
	record, err := s.BeginAuthorityTransition(TransitionIntent{
		Kind:               kind,
		ExpectedGeneration: current.Head.ControllerGeneration,
		Source:             current,
		Target:             target,
		Effects:            []EffectExpectation{effect},
	})
	if err != nil {
		return WorkspaceSnapshot{}, EffectExpectation{}, TransitionRecord{}, err
	}
	persisted, transitionState, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return WorkspaceSnapshot{}, EffectExpectation{}, TransitionRecord{}, err
	}
	if persisted.TransitionID != record.TransitionID || transitionState.Phase != TransitionPhasePrepared {
		return WorkspaceSnapshot{}, EffectExpectation{}, TransitionRecord{}, fmt.Errorf("ref transition prepare record is not durable")
	}
	return predicted, effect, record, nil
}

func (s *Store) applyRefTransitionEffect(
	record TransitionRecord,
	effect EffectExpectation,
	admission Admission,
	apply func() error,
) (refTransitionObservation, error) {
	observed, err := observeRefEffect(admission.Workspace.Root, effect)
	if err != nil {
		return refTransitionObservation{}, err
	}
	classification := s.ClassifyTransition(record, observed)[effect.Key()]
	if classification == EffectUnexpected {
		return refTransitionObservation{}, s.failClosedRefTransition(record, admission, observed, "ref state changed before apply")
	}
	observation := refTransitionObservation{values: observed, classification: classification}
	if classification == EffectExpectedNew {
		return observation, nil
	}
	observation.applyErr = apply()
	observed, err = observeRefEffect(admission.Workspace.Root, effect)
	if err != nil {
		return refTransitionObservation{}, err
	}
	observation.values = observed
	observation.classification = s.ClassifyTransition(record, observed)[effect.Key()]
	if observation.classification == EffectUnexpected {
		return refTransitionObservation{}, s.failClosedRefTransition(record, admission, observed, "ref state became unexpected after apply")
	}
	return observation, nil
}

func (s *Store) commitRefTransition(
	record TransitionRecord,
	admission Admission,
	observation refTransitionObservation,
	predicted WorkspaceSnapshot,
	command string,
) (Admission, error) {
	after, err := CaptureWorkspaceSnapshot(admission.Workspace.Root)
	if err != nil {
		return Admission{}, err
	}
	if after.ID != predicted.ID {
		return Admission{}, s.failClosedRefTransition(record, admission, observation.values, "ref transition reached unexpected workspace snapshot")
	}
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	targetLease, err := s.loadLease(record.TargetLeaseID)
	if err != nil {
		return Admission{}, err
	}
	if targetLease.AttemptID != record.SourceAttemptID || targetLease.WorkspaceID != record.TargetWorkspaceID ||
		targetLease.ControllerGeneration != record.TargetGeneration || targetLease.ExpectedWorkspaceSnapshotID != after.ID {
		return Admission{}, fmt.Errorf("ref transition target lease does not match journal authority")
	}
	if err := s.markTransitionApplied(record, observation.values); err != nil {
		return Admission{}, err
	}
	if _, err := s.commitAuthorityTransitionLocked(record, observation.values, false, func(next *RepositoryControllerHead) error {
		next.LiveLeaseID = record.TargetLeaseID
		return nil
	}); err != nil {
		return Admission{}, err
	}
	finalHead, err := s.finalizeAuthorityTransitionLocked(record)
	if err != nil {
		return Admission{}, err
	}
	if err := s.writeTransitionMutationProvenance(record, record.TargetLeaseID, command, "success", admission.Snapshot, after); err != nil {
		return Admission{}, err
	}
	return Admission{
		Head:      finalHead,
		Attempt:   admission.Attempt,
		Lease:     targetLease,
		Workspace: admission.Workspace,
		Snapshot:  after,
	}, observation.applyErr
}

func (s *Store) abortRefTransition(
	record TransitionRecord,
	admission Admission,
	observed map[string]string,
	command string,
	applyErr error,
) error {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	abortLease, err := s.prepareContinuationLease(admission.Lease, admission.Snapshot, record.TargetGeneration)
	if err != nil {
		return err
	}
	if _, err := s.abortAuthorityTransitionLocked(record, observed, abortLease.LeaseID); err != nil {
		if applyErr != nil {
			return errors.Join(applyErr, fmt.Errorf("abort ref transition: %w", err))
		}
		return err
	}
	if err := s.writeTransitionMutationProvenance(record, abortLease.LeaseID, command, "aborted", admission.Snapshot, admission.Snapshot); err != nil {
		if applyErr != nil {
			return errors.Join(applyErr, fmt.Errorf("record aborted ref transition: %w", err))
		}
		return err
	}
	if applyErr != nil {
		return applyErr
	}
	return fmt.Errorf("ref transition did not reach expected-new state")
}

func (s *Store) failClosedRefTransition(record TransitionRecord, admission Admission, observed map[string]string, reason string) error {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	actual, captureErr := CaptureWorkspaceSnapshot(admission.Workspace.Root)
	if captureErr != nil {
		actual = admission.Snapshot
	}
	_, failErr := s.failClosedLocked(reason, record.TransitionID, admission.Workspace, admission.Snapshot, actual, observed)
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
