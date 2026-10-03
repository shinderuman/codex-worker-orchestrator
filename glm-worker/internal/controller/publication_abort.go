package controller

import "fmt"

type PublicationAdvancementRequired struct {
	TransitionID         string
	ControllerGeneration uint64
	RemoteOID            string
	Action               string
}

func (e *PublicationAdvancementRequired) Error() string {
	return fmt.Sprintf("publication transition %s was not applied; %s at controller generation %d for remote %s", e.TransitionID, e.Action, e.ControllerGeneration, e.RemoteOID)
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
	action := "rebind-unpublished-candidate"
	if op.Transition.Kind == publicationAdopt {
		action = "adopt-external-advancement"
	}
	remote := phase.Observed[op.Transition.Effects[0].Key()]
	return ExecutionOperationResult{}, &PublicationAdvancementRequired{TransitionID: op.Transition.TransitionID, ControllerGeneration: head.ControllerGeneration, RemoteOID: remote, Action: action}
}
