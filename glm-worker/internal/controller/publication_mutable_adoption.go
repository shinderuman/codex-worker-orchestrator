package controller

import "fmt"

func (s *Store) AdoptMutableExternalAdvancement(admission Admission, policy PublicationPolicy) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, err := s.planMutableAdvancement(admission, policy)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, admission.Head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planMutableAdvancement(admission Admission, policy PublicationPolicy) (ExecutionOperation, error) {
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, policy)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if remote == admission.Head.IntegrationTip {
		return ExecutionOperation{}, fmt.Errorf("configured upstream has no advancement")
	}
	if err := s.ensurePublicationPrefix(admission.Head, remote); err != nil {
		return ExecutionOperation{}, err
	}
	project, err := s.publicationProjectAdvancement(admission.Head, remote)
	if err != nil {
		return ExecutionOperation{}, err
	}
	snapshot, err := s.captureSuspensionLocked(admission)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if _, err := RebindSuspensionTrees(s.identity.PrimaryRoot, snapshot, remote); err != nil {
		return ExecutionOperation{}, s.preservePublicationConflict(admission.Head, &snapshot, err)
	}
	record, err := executionTransition(admission.Head, publicationAdopt)
	if err != nil {
		return ExecutionOperation{}, err
	}
	record.SourceAttemptID = admission.Attempt.AttemptID
	record.SourceLeaseID = admission.Lease.LeaseID
	record.SourceWorkspaceID = admission.Workspace.ID
	record.ProjectSnapshotNew = project.SnapshotID
	op := ExecutionOperation{Transition: record, Source: admission, Suspension: &snapshot, Publication: &PublicationOperation{Policy: policy, OldTip: admission.Head.IntegrationTip, NewTip: remote, RemoteOID: remote, Project: &project}}
	if err := s.rebindPublicationEpisode(&op, admission.Head, project); err != nil {
		return ExecutionOperation{}, err
	}
	op.Episode = op.Publication.Episode
	op.Evidence, op.SealRef, err = s.mutableAdvancementEvidence(op)
	if err != nil {
		return ExecutionOperation{}, err
	}
	archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "external:"+remote, []string{remote})
	if err != nil {
		return ExecutionOperation{}, err
	}
	op.Publication.AdvancementRef = &archive
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: suspensionRef(snapshot.SnapshotID), ExpectedNew: snapshot.RetainedCommitOID}}
	if err := s.planAdvancementLocalRef(&op, admission.Head); err != nil {
		return ExecutionOperation{}, err
	}
	return op, nil
}

func (s *Store) mutableAdvancementEvidence(op ExecutionOperation) (EvidencePublicationInput, *EvidenceObjectRef, error) {
	input, seal, err := s.buildSuspensionEvidence(op)
	return input, &seal, err
}
