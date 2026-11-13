package controller

import (
	"errors"
	"fmt"
)

type PublicationAdvancementRequired struct {
	TransitionID         string
	ControllerGeneration uint64
	RemoteOID            string
	Action               string
	Reason               string
}

type CompletionFailure struct {
	SchemaVersion   int    `json:"schema_version"`
	OperationDigest string `json:"operation_digest"`
	Stage           string `json:"stage"`
	GitOutput       string `json:"git_output,omitempty"`
	Message         string `json:"message"`
	RemoteOID       string `json:"remote_oid"`
}

func (e *PublicationAdvancementRequired) Error() string {
	if e.Reason != "" {
		if e.Action == PublicationOperationRebind {
			return fmt.Sprintf("publication transition %s was not applied; run glm-parent-action prepare controller-publication --operation rebind at controller generation %d for remote %s: %s", e.TransitionID, e.ControllerGeneration, e.RemoteOID, e.Reason)
		}
		return fmt.Sprintf("publication transition %s was not applied; candidate reentry unavailable at controller generation %d for remote %s: %s", e.TransitionID, e.ControllerGeneration, e.RemoteOID, e.Reason)
	}
	if e.Action == PublicationOperationReenter {
		return fmt.Sprintf("publication transition %s was not applied; run glm-parent-action prepare controller-publication --operation reenter at controller generation %d for remote %s", e.TransitionID, e.ControllerGeneration, e.RemoteOID)
	}
	return fmt.Sprintf("publication transition %s was not applied; %s at controller generation %d for remote %s", e.TransitionID, e.Action, e.ControllerGeneration, e.RemoteOID)
}

func publicationAbortAction(op ExecutionOperation) string {
	if op.Transition.Kind == publicationAdopt || op.Transition.Kind == publicationAdoptSource {
		return "adopt-external-advancement"
	}
	return ""
}

func (s *Store) finishPublicationAbort(op ExecutionOperation, head RepositoryControllerHead, phase TransitionState) (ExecutionOperationResult, error) {
	if head.PendingTransitionID == op.Transition.TransitionID && head.ControllerGeneration == op.Transition.PreparedGeneration {
		head.ControllerGeneration = op.Transition.TargetGeneration
		head.PendingTransitionID = ""
		if err := s.writeHeadCAS(op.Transition.PreparedGeneration, head); err != nil {
			return ExecutionOperationResult{}, err
		}
	}
	if head.ControllerGeneration != op.Transition.TargetGeneration || head.PendingTransitionID != "" {
		return ExecutionOperationResult{}, fmt.Errorf("aborted publication no longer owns controller")
	}
	action, reason := s.publicationAbortRecoveryAction(op, head, phase)
	remote := phase.Observed[op.Transition.Effects[0].Key()]
	if op.Terminal != nil && phase.Completion != nil {
		remote = phase.Completion.AbortRemoteOID
	}
	return ExecutionOperationResult{}, &PublicationAdvancementRequired{TransitionID: op.Transition.TransitionID, ControllerGeneration: head.ControllerGeneration, RemoteOID: remote, Action: action, Reason: reason}
}

func (s *Store) publicationAbortRecoveryAction(op ExecutionOperation, head RepositoryControllerHead, phase TransitionState) (string, string) {
	if op.Transition.Kind == publicationAdopt || op.Transition.Kind == publicationAdoptSource {
		return publicationAbortAction(op), ""
	}
	if op.Terminal == nil || phase.Completion == nil || phase.Completion.AbortRemoteOID == "" ||
		phase.Completion.ObservationRef != nil {
		return "", "aborted publication lacks a bound unpublished terminal remote observation for candidate reentry"
	}
	if head.AcceptedCandidateRef == nil {
		return "", "aborted completion has no accepted candidate"
	}
	candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return "", err.Error()
	}
	if err := s.admitPromotedCandidateReentry(head, candidate); err != nil {
		if errors.Is(err, errCandidateReentryExternalAdvancement) {
			return PublicationOperationRebind, err.Error()
		}
		return "", err.Error()
	}
	return PublicationOperationReenter, ""
}

func (s *Store) abortCompletionPrepare(op ExecutionOperation, phase TransitionState, remote string) error {
	if phase.Completion == nil || phase.Completion.ObservationRef != nil {
		return fmt.Errorf("completion publication outcome is unresolved; exact journal retained")
	}
	if phase.Completion.WriteIntent || remote == op.Source.Head.IntegrationTip {
		if err := s.validateCompletionFailure(op, phase, remote); err != nil {
			return err
		}
	}
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	if err := s.validateCompletionAbortSource(op, head, remote); err != nil {
		return err
	}
	phase.Completion.AbortRemoteOID = remote
	if err := s.writeTransitionState(phase); err != nil {
		return err
	}
	if err := s.restoreCompletionPrepare(op); err != nil {
		return err
	}
	phase.Phase = TransitionPhaseAborted
	if err := s.writeTransitionState(phase); err != nil {
		return err
	}
	_, err = s.finishPublicationAbort(op, head, phase)
	return err
}

func (s *Store) saveCompletionFailure(op ExecutionOperation, phase *TransitionState, stage, output, message, remote string) error {
	failure := CompletionFailure{SchemaVersion: controllerSchemaVersion, OperationDigest: op.Transition.OperationDigest, Stage: stage, GitOutput: output, Message: message, RemoteOID: remote}
	ref, err := s.putEvidenceJSON("completion-failure", op.Transition.TransitionID, failure)
	if err != nil {
		return err
	}
	phase.Completion.FailureRef = &ref
	return s.writeTransitionState(*phase)
}

func (s *Store) validateCompletionFailure(op ExecutionOperation, phase TransitionState, remote string) error {
	if phase.Completion == nil || phase.Completion.FailureRef == nil {
		return fmt.Errorf("completion abort lacks saved non-publication proof")
	}
	ref, failure, err := s.loadCompletionFailure(*phase.Completion.FailureRef)
	if err != nil {
		return err
	}
	if !completionFailureBindingMatches(op, ref, failure) {
		return fmt.Errorf("completion failure evidence binding differs")
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, failure.RemoteOID, remote)
	if err != nil {
		return fmt.Errorf("inspect completion failure remote proof: %w", err)
	}
	if !contains {
		return fmt.Errorf("completion failure remote proof differs")
	}
	if (op.Terminal.ObserveOnly && failure.Stage == "observe-only") || completionFailureProvesNoWrite(phase.Completion.WriteIntent, failure, op.Terminal.Policy) {
		return nil
	}
	return fmt.Errorf("completion publication outcome is unresolved; exact journal retained")
}

func completionFailureBindingMatches(op ExecutionOperation, ref EvidenceObjectRef, failure CompletionFailure) bool {
	return failure.SchemaVersion == controllerSchemaVersion && ref.LogicalIdentity == op.Transition.TransitionID &&
		failure.OperationDigest == op.Transition.OperationDigest && failure.Message != ""
}

func (s *Store) restoreCompletionPrepare(op ExecutionOperation) error {
	for index, file := range op.Terminal.Files {
		effects := append([]EffectExpectation(nil), op.Transition.Effects[1+index*2:3+index*2]...)
		if err := s.applyTerminalMetadataFile(file, effects); err != nil {
			return err
		}
	}
	effect := op.Transition.Effects[0]
	if err := updatePublicationRef(s.identity.PrimaryRoot, effect.Resource, effect.ExpectedOld, effect.ExpectedNew); err != nil {
		return err
	}
	return nil
}

func (s *Store) validateCompletionAbortSource(op ExecutionOperation, head RepositoryControllerHead, remote string) error {
	c, err := s.LoadAcceptedCandidate(op.Terminal.Record.CandidateRef)
	if err != nil {
		return err
	}
	if err := s.ensurePublicationPrefix(head, remote); err != nil {
		return err
	}
	descendant, err := isPublicationDescendant(s.identity.PrimaryRoot, c.BaseOID, remote)
	if err != nil {
		return fmt.Errorf("inspect completion abort base history: %w", err)
	}
	if !descendant {
		return fmt.Errorf("completion abort remote is outside the original base")
	}
	if err := s.validateTerminalPrimarySource(op); err != nil {
		return err
	}
	return nil
}

func (s *Store) loadCompletionFailure(ref EvidenceObjectRef) (EvidenceObjectRef, CompletionFailure, error) {
	var failure CompletionFailure
	if err := validateTypedEvidenceRef(ref, "completion-failure"); err != nil {
		return ref, failure, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return ref, failure, err
	}
	err = decodeControllerJSON(data, &failure)
	return ref, failure, err
}

func completionFailureProvesNoWrite(intent bool, failure CompletionFailure, policy PublicationPolicy) bool {
	return (!intent && failure.Stage == "prepare") ||
		(intent && failure.Stage == "push-guard") ||
		(intent && failure.Stage == "push-rejected" && publicationPushRejected(failure.GitOutput, policy))
}
