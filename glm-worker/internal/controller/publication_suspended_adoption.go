package controller

import "fmt"

func (s *Store) planSuspendedExternalAdvancement(head RepositoryControllerHead, policy PublicationPolicy) (ExecutionOperationResult, error) {
	remote, err := s.suspendedAdvancementRemote(head, policy)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	project, err := s.publicationProjectAdvancement(head, remote)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	record, err := executionTransition(head, publicationAdopt)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op := ExecutionOperation{Transition: record, Publication: &PublicationOperation{Policy: policy, OldTip: head.IntegrationTip, NewTip: remote, RemoteOID: remote, Project: &project}}
	op.Transition.ProjectSnapshotNew = project.SnapshotID
	if err := s.preflightSuspendedRebind(remote); err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.rebindPublicationEpisode(&op, head, project); err != nil {
		return ExecutionOperationResult{}, err
	}
	archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "external:"+remote, []string{remote})
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op.Publication.AdvancementRef = &archive
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceState, Resource: "integration-tip", ExpectedOld: head.IntegrationTip, ExpectedNew: remote}}
	if err := s.planAdvancementLocalRef(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) suspendedAdvancementRemote(head RepositoryControllerHead, policy PublicationPolicy) (string, error) {
	if head.ExecutionTaskRef == nil || head.RootTaskRef == nil {
		return "", fmt.Errorf("external advancement requires suspended or accepted execution authority")
	}
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, policy)
	if err != nil {
		return "", err
	}
	if remote == head.IntegrationTip {
		return "", fmt.Errorf("configured upstream has no advancement")
	}
	if err := s.ensurePublicationPrefix(head, remote); err != nil {
		return "", err
	}
	return remote, nil
}
