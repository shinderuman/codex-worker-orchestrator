package controller

import (
	"fmt"
)

func (s *Store) applyCandidateCompletion(op ExecutionOperation) error {
	phase, err := s.loadTransitionState(op.Transition.TransitionID)
	if err != nil {
		return err
	}
	if phase.Completion == nil {
		phase.Completion = &CompletionProgress{}
	}
	if phase.Completion.AbortRemoteOID != "" {
		return s.abortCompletionPrepare(op, phase, phase.Completion.AbortRemoteOID)
	}
	if phase.Completion.ObservationRef != nil {
		return s.acknowledgeCandidateCompletion(op, phase)
	}
	c, err := s.LoadAcceptedCandidate(op.Terminal.Record.CandidateRef)
	if err != nil {
		return err
	}
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, c.Policy)
	if err != nil {
		return err
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, c.CommitOID, remote)
	if err != nil {
		return err
	}
	if contains {
		return s.observeCandidateCompletion(op, phase, c, remote)
	}
	if phase.Completion.FailureRef != nil {
		return s.abortCompletionPrepare(op, phase, remote)
	}
	if remote != c.BaseOID {
		return s.abortCompletionPrepare(op, phase, remote)
	}
	return s.publishPreparedCompletion(op, phase, c, remote)
}

func (s *Store) prepareCandidateCompletionWrite(op ExecutionOperation, phase *TransitionState) error {
	if phase.Completion == nil {
		phase.Completion = &CompletionProgress{}
	}
	if _, err := s.applyTerminalMetadataFiles(op); err != nil {
		return err
	}
	if phase.Completion.ReadinessRef == nil {
		ref, err := s.prepareCompletionReadiness(op)
		if err != nil {
			return err
		}
		phase.Completion.ReadinessRef = &ref
	}
	return s.writeTransitionState(*phase)
}

func (s *Store) beginCandidateCompletionWrite(op ExecutionOperation, phase *TransitionState) error {
	if _, err := s.validateCompletionReadiness(op, *phase); err != nil {
		return err
	}
	phase.Completion.WriteIntent = true
	return s.writeTransitionState(*phase)
}

func (s *Store) observeCandidateCompletion(op ExecutionOperation, phase TransitionState, c AcceptedCandidate, remote string) error {
	if phase.Completion.ReadinessRef == nil || !phase.Completion.WriteIntent {
		return fmt.Errorf("remote completion lacks prepared publication intent")
	}
	ref, err := s.storePublicationObservation(op, c, remote)
	if err != nil {
		return err
	}
	phase.Completion.ObservationRef = ref
	if err := s.writeTransitionState(phase); err != nil {
		return err
	}
	return s.acknowledgeCandidateCompletion(op, phase)
}

func (s *Store) acknowledgeCandidateCompletion(op ExecutionOperation, phase TransitionState) error {
	ack, err := s.prepareCompletionAcknowledgement(op, &phase)
	if err != nil {
		return err
	}
	observation, err := s.completionObservation(op, phase)
	if err != nil {
		return err
	}
	if err := s.acknowledgeCompletionRuntime(op, phase); err != nil {
		return err
	}
	actual, err := s.observeTerminalFileEffects(op)
	if err != nil {
		return err
	}
	actual[op.Transition.Effects[0].Key()] = op.Terminal.Record.CandidateOID
	for _, effect := range op.Transition.Effects {
		if effect.Surface == MutationSurfaceHistory {
			actual[effect.Key()] = op.Terminal.Record.CandidateOID
		}
	}
	_, err = s.commitAuthorityTransitionCoreLocked(op.Transition, actual, false, func(head *RepositoryControllerHead) error {
		if err := s.commitTerminalMetadataTarget(head, *op.Terminal); err != nil {
			return err
		}
		head.AcceptedCandidateRef = nil
		head.LiveAttemptID = ""
		head.LiveLeaseID = ""
		head.PublicationPolicy = &op.Terminal.Policy
		head.ObservedPrefix = op.Terminal.Record.CandidateOID
		head.ObservedRemoteTip = observation.RemoteOID
		head.EvidenceHeadRef = &ack.EvidenceHead
		head.EvidenceLedgerHeadRef = &ack.EvidenceLedger
		head.EvidenceLedgerSequence = ack.Sequence
		return nil
	})
	return err
}

func (s *Store) completionObservation(op ExecutionOperation, phase TransitionState) (PublicationObservation, error) {
	var observation PublicationObservation
	if phase.Completion == nil || phase.Completion.ObservationRef == nil || !phase.Completion.WriteIntent {
		return observation, fmt.Errorf("completion acknowledgement requires saved remote observation")
	}
	ref := *phase.Completion.ObservationRef
	if err := validateTypedEvidenceRef(ref, "publication-observation"); err != nil {
		return observation, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return observation, err
	}
	if err := decodeControllerJSON(data, &observation); err != nil {
		return observation, err
	}
	if !completionObservationMatches(op, ref, observation) {
		return observation, fmt.Errorf("completion remote observation binding differs")
	}
	if err := s.verifyCompletionObservationArchive(observation); err != nil {
		return observation, err
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, observation.CandidateOID, observation.RemoteOID)
	if err != nil || !contains {
		return observation, fmt.Errorf("completion observation does not prove publication: %w", err)
	}
	return observation, nil
}

func (s *Store) verifyCommittedCompletion(op ExecutionOperation, head RepositoryControllerHead) error {
	if err := s.verifyCommittedTerminalMetadata(op, head); err != nil {
		return err
	}
	phase, err := s.loadTransitionState(op.Transition.TransitionID)
	if err != nil {
		return err
	}
	ack, err := s.loadCompletionAcknowledgement(op, phase)
	if err != nil {
		return err
	}
	if head.AcceptedCandidateRef != nil || head.EvidenceHeadRef == nil || head.EvidenceLedgerHeadRef == nil || !evidenceRefsEqual(*head.EvidenceHeadRef, ack.EvidenceHead) || !evidenceRefsEqual(*head.EvidenceLedgerHeadRef, ack.EvidenceLedger) || head.EvidenceLedgerSequence != ack.Sequence {
		return fmt.Errorf("completion acknowledgement differs from readiness")
	}
	if _, err = s.completionObservation(op, phase); err != nil {
		return err
	}
	return s.verifyCompletionRuntime(op, phase)
}

func completionObservationMatches(op ExecutionOperation, ref EvidenceObjectRef, observation PublicationObservation) bool {
	return observation.SchemaVersion == controllerSchemaVersion && ref.LogicalIdentity == op.Transition.TransitionID && observation.TransitionID == op.Transition.TransitionID && observation.CandidateOID == op.Terminal.Record.CandidateOID && observation.CandidateID == op.Terminal.Record.CandidateRef.LogicalIdentity && observation.Remote == op.Terminal.Policy.Remote && observation.RemoteRef == op.Terminal.Policy.RemoteRef
}

func (s *Store) verifyCompletionObservationArchive(observation PublicationObservation) error {
	roots, err := s.VerifyGitObjectArchive(observation.GitArchive)
	if err != nil {
		return err
	}
	if len(roots) != 1 || roots[0].OID != observation.RemoteOID || roots[0].Type != gitCommitObjectType {
		return fmt.Errorf("completion observation archive has wrong root")
	}
	return nil
}

func (s *Store) publishPreparedCompletion(op ExecutionOperation, phase TransitionState, c AcceptedCandidate, remote string) error {
	if err := s.prepareCandidateCompletionWrite(op, &phase); err != nil {
		if saveErr := s.saveCompletionFailure(op, &phase, "prepare", "", err.Error(), remote); saveErr != nil {
			return saveErr
		}
		return fmt.Errorf("completion readiness failed: %w; %w", err, s.abortCompletionPrepare(op, phase, remote))
	}
	if err := s.beginCandidateCompletionWrite(op, &phase); err != nil {
		return err
	}
	remote, err := s.pushAndObserveCandidate(op, c, c.BaseOID)
	if err != nil {
		return err
	}
	return s.observeCandidateCompletion(op, phase, c, remote)
}
