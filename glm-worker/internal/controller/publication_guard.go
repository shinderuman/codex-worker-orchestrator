package controller

import (
	"fmt"
	"reflect"
	"strings"
)

type PublicationRefGuardInput struct {
	OldOID string
	NewOID string
	Ref    string
}

type PublicationPushGuardInput struct {
	RemoteName string
	LocalRef   string
	LocalOID   string
	RemoteRef  string
	RemoteOID  string
}

func (s *Store) GuardPublicationRefUpdate(input PublicationRefGuardInput) error {
	if !validPublicationGuardOID(input.OldOID) || !validPublicationGuardOID(input.NewOID) || strings.TrimSpace(input.Ref) == "" {
		return fmt.Errorf("invalid publication ref update")
	}
	if !strings.HasPrefix(input.Ref, "refs/heads/") {
		return nil
	}
	op, present, err := s.pendingPublicationGuardOperation()
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	effect, found := publicationGuardEffect(op.Transition.Effects, MutationSurfaceRef, input.Ref)
	if !found {
		if publicationGuardOwnsGitMutation(op.Transition.Effects) {
			return fmt.Errorf("publication ref update rejected: pending controller publication does not authorize ref %s", input.Ref)
		}
		return nil
	}
	if effect.ExpectedOld != input.OldOID || effect.ExpectedNew != input.NewOID {
		return fmt.Errorf("publication ref update rejected: update does not match pending controller transition")
	}
	return nil
}

func (s *Store) GuardPublicationPush(input PublicationPushGuardInput) error {
	if err := validatePublicationPushGuardInput(input); err != nil {
		return err
	}
	op, present, err := s.pendingPublicationGuardOperation()
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("publication push rejected: no pending controller publication authority")
	}
	policy, ok := publicationGuardPolicy(op)
	if !ok {
		return fmt.Errorf("publication push rejected: pending controller transition is not a publication operation")
	}
	if err := validatePublicationPushTarget(policy, input); err != nil {
		return err
	}
	return validatePublicationPushEffect(op.Transition.Effects, input)
}

func validatePublicationPushGuardInput(input PublicationPushGuardInput) error {
	if strings.TrimSpace(input.RemoteName) == "" ||
		!strings.HasPrefix(input.LocalRef, "refs/heads/") ||
		!strings.HasPrefix(input.RemoteRef, "refs/heads/") ||
		!validPublicationGuardOID(input.LocalOID) ||
		!validPublicationGuardOID(input.RemoteOID) {
		return fmt.Errorf("invalid publication push update")
	}
	return nil
}

func validatePublicationPushTarget(policy PublicationPolicy, input PublicationPushGuardInput) error {
	if policy.Remote != input.RemoteName || policy.LocalRef != input.LocalRef || policy.RemoteRef != input.RemoteRef {
		return fmt.Errorf("publication push rejected: target does not match pending controller publication policy")
	}
	return nil
}

func validatePublicationPushEffect(effects []EffectExpectation, input PublicationPushGuardInput) error {
	resource := input.RemoteName + ":" + input.RemoteRef
	effect, found := publicationGuardEffect(effects, MutationSurfaceHistory, resource)
	if !found {
		return fmt.Errorf("publication push rejected: pending controller transition does not authorize remote history mutation")
	}
	if effect.ExpectedOld != input.RemoteOID || effect.ExpectedNew != input.LocalOID {
		return fmt.Errorf("publication push rejected: update does not match pending controller transition")
	}
	return nil
}

func (s *Store) pendingPublicationGuardOperation() (ExecutionOperation, bool, error) {
	head, err := s.LoadHead()
	if err != nil {
		return ExecutionOperation{}, false, err
	}
	if head.PendingTransitionID == "" {
		return ExecutionOperation{}, false, nil
	}
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(head.PendingTransitionID), &op); err != nil {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard cannot load pending controller operation: %w", err)
	}
	record, phase, err := s.LoadTransition(head.PendingTransitionID)
	if err != nil {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard cannot load pending controller transition: %w", err)
	}
	if !reflect.DeepEqual(op.Transition, record) {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard rejected mismatched controller operation journal")
	}
	if head.ControllerGeneration != record.PreparedGeneration || phase.Phase != TransitionPhasePrepared {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard rejected stale controller transition")
	}
	if err := s.validateExecutionOperation(op); err != nil {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard rejected invalid controller operation: %w", err)
	}
	return op, true, nil
}

func publicationGuardPolicy(op ExecutionOperation) (PublicationPolicy, bool) {
	if op.Publication != nil {
		return op.Publication.Policy, true
	}
	if op.Terminal != nil {
		return op.Terminal.Policy, true
	}
	return PublicationPolicy{}, false
}

func publicationGuardEffect(effects []EffectExpectation, surface MutationSurface, resource string) (EffectExpectation, bool) {
	for _, effect := range effects {
		if effect.Surface == surface && effect.Resource == resource {
			return effect, true
		}
	}
	return EffectExpectation{}, false
}

func publicationGuardOwnsGitMutation(effects []EffectExpectation) bool {
	for _, effect := range effects {
		if effect.Surface == MutationSurfaceRef || effect.Surface == MutationSurfaceHistory {
			return true
		}
	}
	return false
}

func validPublicationGuardOID(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
