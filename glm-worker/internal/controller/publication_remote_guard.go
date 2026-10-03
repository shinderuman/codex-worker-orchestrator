package controller

import "fmt"

func (s *Store) ensurePublicationPrefix(head RepositoryControllerHead, remote string) error {
	if err := enforceObservedPrefix(s.identity.PrimaryRoot, head, remote); err != nil {
		_, failureErr := s.failClosedLocked("publication immutable-prefix violation", head.PendingTransitionID, WorkspaceIdentity{}, WorkspaceSnapshot{}, WorkspaceSnapshot{}, map[string]string{"remote": remote, "observed_prefix": head.ObservedPrefix, "integration_tip": head.IntegrationTip})
		if failureErr != nil {
			return fmt.Errorf("publication prefix failure could not be sealed: %w", failureErr)
		}
		return err
	}
	return nil
}

func (s *Store) guardPublicationRemote(op ExecutionOperation, c AcceptedCandidate) error {
	if op.Transition.Kind != publicationRebind && op.Transition.Kind != publicationReenter && op.Transition.Kind != publicationAdopt {
		return nil
	}
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, c.Policy)
	if err != nil {
		return err
	}
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	if err := s.ensurePublicationPrefix(head, remote); err != nil {
		return err
	}
	if remote != op.Publication.RemoteOID {
		if err := s.guardRacedLocalEffects(op, head, remote); err != nil {
			return err
		}
		return s.abortRacedPublication(op, remote)
	}
	return nil
}

func (s *Store) guardRacedLocalEffects(op ExecutionOperation, head RepositoryControllerHead, remote string) error {
	p := op.Publication
	if p.LocalOld == "" || p.LocalOld == p.LocalNew {
		return nil
	}
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, p.Policy.LocalRef)
	if err != nil {
		return err
	}
	if exists && local == p.LocalOld {
		return nil
	}
	_, err = s.failClosedLocked("remote raced with applied local publication effect", op.Transition.TransitionID, WorkspaceIdentity{}, WorkspaceSnapshot{}, WorkspaceSnapshot{}, map[string]string{"remote": remote, "local": local, "expected_old": p.LocalOld, "expected_new": p.LocalNew, "integration_tip": head.IntegrationTip})
	if err != nil {
		return err
	}
	return fmt.Errorf("raced local publication effect preserved for explicit authority resolution")
}
