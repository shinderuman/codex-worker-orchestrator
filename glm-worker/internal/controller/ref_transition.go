package controller

import (
	"fmt"
	"os/exec"
	"strings"
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
	return s.RecordTransitionMutation(admission, command, "success", after)
}

func (s *Store) abortRefTransition(
	record TransitionRecord,
	admission Admission,
	observed map[string]string,
	command string,
	applyErr error,
) error {
	if _, err := s.CancelTransition(record, observed); err != nil {
		if applyErr != nil {
			return fmt.Errorf("ref transition apply failed: %w; cancel failed: %v", applyErr, err)
		}
		return err
	}
	if _, err := s.RecordTransitionMutation(admission, command, "aborted", admission.Snapshot); err != nil {
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
