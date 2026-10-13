package controller

import "fmt"

func (s *Store) planAdvancementLocalRef(op *ExecutionOperation, head RepositoryControllerHead) error {
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, op.Publication.Policy.LocalRef)
	if err != nil {
		return err
	}
	if !exists || (local != head.IntegrationTip && local != op.Publication.NewTip) {
		return fmt.Errorf("external advancement local ref ownership is ambiguous")
	}
	if local == op.Publication.NewTip {
		return nil
	}
	op.Publication.LocalOld = local
	op.Publication.LocalNew = op.Publication.NewTip
	op.Transition.Effects = append(op.Transition.Effects, EffectExpectation{Surface: MutationSurfaceRef, Resource: op.Publication.Policy.LocalRef, ExpectedOld: local, ExpectedNew: op.Publication.NewTip})
	return nil
}

func (s *Store) applyAdvancementLocalRef(op ExecutionOperation) error {
	if op.Publication.LocalOld == "" {
		return nil
	}
	return updatePublicationRef(s.identity.PrimaryRoot, op.Publication.Policy.LocalRef, op.Publication.LocalOld, op.Publication.LocalNew)
}

func (s *Store) baseAdvancementEffects(op ExecutionOperation) (map[string]string, error) {
	actual := make(map[string]string, len(op.Transition.Effects))
	for _, effect := range op.Transition.Effects {
		value, err := s.baseAdvancementEffect(op, effect)
		if err != nil {
			return nil, err
		}
		actual[effect.Key()] = value
	}
	return actual, nil
}

func (s *Store) baseAdvancementEffect(op ExecutionOperation, effect EffectExpectation) (string, error) {
	switch {
	case effect.Surface == MutationSurfaceRef:
		return s.proveAdvancementRefEffect(effect)
	case effect.Surface == MutationSurfaceState && effect.Resource == "integration-tip" && effect.ExpectedNew == op.Publication.NewTip:
		return op.Publication.NewTip, nil
	case effect.Surface == MutationSurfaceHistory && retiredMetadataAdvancement(op):
		return s.proveRetiredMetadataHistoryEffect(op, effect)
	default:
		return "", fmt.Errorf("invalid base advancement effect")
	}
}

func (s *Store) proveAdvancementRefEffect(effect EffectExpectation) (string, error) {
	value, exists, err := readExecutionRef(s.identity.PrimaryRoot, effect.Resource)
	if err != nil {
		return "", err
	}
	if !exists || value != effect.ExpectedNew {
		return "", fmt.Errorf("base advancement ref postcondition differs")
	}
	return value, nil
}

func (s *Store) proveRetiredMetadataHistoryEffect(op ExecutionOperation, effect EffectExpectation) (string, error) {
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, op.Publication.Policy)
	if err != nil {
		return "", err
	}
	if effect.Resource != op.Publication.Policy.Remote+":"+op.Publication.Policy.RemoteRef || effect.ExpectedNew != remote || remote != op.Publication.NewTip {
		return "", fmt.Errorf("parent metadata publication postcondition differs")
	}
	return remote, nil
}
