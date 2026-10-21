package controller

import (
	"fmt"
)

type PublicationAdvancementRequired struct {
	TransitionID         string
	ControllerGeneration uint64
	RemoteOID            string
	Action               string
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
	return fmt.Sprintf("publication transition %s was not applied; %s at controller generation %d for remote %s", e.TransitionID, e.Action, e.ControllerGeneration, e.RemoteOID)
}

func publicationAbortAction(op ExecutionOperation) string {
	if op.Transition.Kind == publicationAdopt || op.Transition.Kind == publicationAdoptSource {
		return "adopt-external-advancement"
	}
	return "rebind-unpublished-candidate"
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
	action := publicationAbortAction(op)
	if op.Terminal != nil && phase.Completion != nil && phase.Completion.AbortRemoteOID == op.Source.Head.IntegrationTip {
		action = "reenter-accepted-candidate"
	}
	remote := phase.Observed[op.Transition.Effects[0].Key()]
	if op.Terminal != nil && phase.Completion != nil {
		remote = phase.Completion.AbortRemoteOID
	}
	return ExecutionOperationResult{}, &PublicationAdvancementRequired{TransitionID: op.Transition.TransitionID, ControllerGeneration: head.ControllerGeneration, RemoteOID: remote, Action: action}
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
	if failure.SchemaVersion != controllerSchemaVersion || ref.LogicalIdentity != op.Transition.TransitionID || failure.OperationDigest != op.Transition.OperationDigest || failure.Message == "" {
		return fmt.Errorf("completion failure evidence binding differs")
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, failure.RemoteOID, remote)
	if err != nil || !contains {
		return fmt.Errorf("completion failure remote proof differs: %w", err)
	}
	if completionFailureProvesNoWrite(phase.Completion.WriteIntent, failure, op.Terminal.Policy) {
		return nil
	}
	return fmt.Errorf("completion publication outcome is unresolved; exact journal retained")
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
	if err != nil || !descendant {
		return fmt.Errorf("completion abort remote is outside the original base: %w", err)
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
	return (!intent && failure.Stage == "prepare") || (intent && failure.Stage == "push-rejected" && publicationPushRejected(failure.GitOutput, policy))
}
