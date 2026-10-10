package controller

func (s *Store) buildAcceptedCandidateSeal(op ExecutionOperation, c AcceptedCandidate) (EvidenceObjectRef, error) {
	x := op.Suspension
	archive, roots, err := s.CaptureGitObjectArchive(op.Source.Workspace.Root, "accepted:"+x.AttemptID, []string{x.ExecutionBaseOID, x.Baseline.IndexTree, x.Baseline.WorktreeTree, x.Current.IndexTree, x.Current.WorktreeTree, c.CommitOID})
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	seal := AttemptSeal{SchemaVersion: evidenceSchemaVersion, RepositoryIdentity: s.identity.LineageID, SemanticTaskRef: x.SemanticTaskRef, RootTaskRef: x.RootTaskRef, AttemptID: x.AttemptID, PredecessorAttemptID: op.Source.Attempt.PredecessorAttemptID, EpisodeID: op.Source.Head.ActiveEpisodeID, EpisodeRevision: op.Source.Head.ActiveEpisodeRevision, ControllerGeneration: op.Transition.CommittedGeneration, SealingTransitionID: op.Transition.TransitionID, RevokedLeaseID: op.Source.Lease.LeaseID, WorkspaceID: x.WorkspaceID, ExecutionPurpose: op.Source.Lease.Purpose, SourceProjectSnapshotID: x.SourceProjectSnapshotID, StartedAt: op.Source.Attempt.CreatedAt, SealedAt: op.Transition.CreatedAt, Disposition: string(AttemptStateAccepted), ExecutionBaseOID: x.ExecutionBaseOID, BaselineIndexTree: x.Baseline.IndexTree, BaselineWorktreeTree: x.Baseline.WorktreeTree, CurrentIndexTree: x.Current.IndexTree, CurrentWorktreeTree: x.Current.WorktreeTree, ParentAuthorityDigest: x.Current.ParentAuthorityDigest, OperationalSnapshotID: x.SnapshotID, CandidateCommitOID: c.CommitOID, CandidateTreeOID: c.TreeOID, CandidateBaseOID: c.BaseOID, CandidateSnapshotID: c.SnapshotID, CandidateID: acceptedCandidateID(c), GitObjectArchive: archive, GitObjectArchiveRoots: roots, ReviewValidationRefs: c.Evidence}
	if err := s.captureAttemptSemanticEvidence(op, &seal); err != nil {
		return EvidenceObjectRef{}, err
	}
	runtimeCaptured, err := s.captureAttemptRuntimeEvidence(op.Source, &seal)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	applyAttemptSealRuntimeCoverage(&seal, runtimeCaptured)
	ref, _, err := s.StoreAttemptSeal(seal)
	return ref, err
}
