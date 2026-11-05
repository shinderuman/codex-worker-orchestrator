package controller

import "fmt"

func (s *Store) requirePulledSource(head RepositoryControllerHead, policy PublicationPolicy, sourceOID string) error {
	if sourceOID == "" || sourceOID == head.IntegrationTip {
		return fmt.Errorf("post-pull source requires a distinct advanced commit")
	}
	if head.PublicationPolicy == nil || *head.PublicationPolicy != policy {
		return fmt.Errorf("post-pull publication policy differs from Controller authority")
	}
	primary, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, policy.LocalRef)
	if err != nil {
		return err
	}
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, policy)
	if err != nil {
		return err
	}
	if !exists || local != sourceOID || primary != sourceOID || remote != sourceOID {
		return fmt.Errorf("post-pull source requires primary, local ref and observed remote tip to equal %s (primary=%s local=%s remote=%s)", sourceOID, primary, local, remote)
	}
	fastForward, err := isPublicationDescendant(s.identity.PrimaryRoot, head.IntegrationTip, sourceOID)
	if err != nil {
		return err
	}
	if !fastForward {
		return fmt.Errorf("post-pull source is not a fast-forward of Controller integration authority")
	}
	project, err := s.publicationProjectAdvancement(head, sourceOID)
	if err != nil {
		return err
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, sourceOID)
	if err != nil {
		return err
	}
	if head.RootTaskRef == nil || !authority.Task.Equal(*head.RootTaskRef) {
		return fmt.Errorf("post-pull source changes the selected Task authority")
	}
	if head.ExecutionTaskRef != nil && !projectHasTask(project, *head.ExecutionTaskRef) {
		return fmt.Errorf("post-pull source changes execution Task authority")
	}
	return nil
}
