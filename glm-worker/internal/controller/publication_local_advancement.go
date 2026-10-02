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
		switch {
		case effect.Surface == MutationSurfaceRef:
			value, exists, err := readExecutionRef(s.identity.PrimaryRoot, effect.Resource)
			if err != nil {
				return nil, err
			}
			if !exists || value != effect.ExpectedNew {
				return nil, fmt.Errorf("base advancement ref postcondition differs")
			}
			actual[effect.Key()] = value
		case effect.Surface == MutationSurfaceState && effect.Resource == "integration-tip" && effect.ExpectedNew == op.Publication.NewTip:
			actual[effect.Key()] = op.Publication.NewTip
		default:
			return nil, fmt.Errorf("invalid base advancement effect")
		}
	}
	return actual, nil
}
