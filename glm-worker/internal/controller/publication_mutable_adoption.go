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

func (s *Store) LiveMutableAdvancementAdmission(policy PublicationPolicy) (Admission, error) {
	admission, err := s.liveMutableAdvancementSource()
	if err != nil {
		return Admission{}, err
	}
	if _, err := s.AdmitMutation(admission.MutationAuthority(), admission.Workspace, admission.Snapshot); err != nil {
		attributed, attributionErr := s.attributeExternalPull(admission, policy)
		if attributionErr != nil {
			return Admission{}, err
		}
		return attributed, nil
	}
	return admission, nil
}

func (s *Store) liveMutableAdvancementSource() (Admission, error) {
	head, err := s.liveAdvancementHead()
	if err != nil {
		return Admission{}, err
	}
	attempt, lease, err := s.loadLiveAttemptAndLease(head)
	if err != nil {
		return Admission{}, err
	}
	if attempt.ExecutionAuthority == ExecutionAuthorityParentOnly {
		return Admission{}, fmt.Errorf("parent-only execution admits only completion")
	}
	root := s.projectWorkspaceRoot(lease.WorkspaceID)
	if root == "" {
		return Admission{}, fmt.Errorf("live completion workspace is unavailable")
	}
	workspace, err := ResolveWorkspaceIdentity(root, s.identity)
	if err != nil {
		return Admission{}, err
	}
	observed, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return Admission{}, err
	}
	if _, err := MutationAuthorityFromHead(head); err != nil {
		return Admission{}, err
	}
	return Admission{Head: head, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: observed}, nil
}

func (s *Store) liveAdvancementHead() (RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" || head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return RepositoryControllerHead{}, fmt.Errorf("mutable advancement requires live controller execution authority")
	}
	return head, nil
}

func (s *Store) captureAdvancementSuspensionLocked(admission Admission, policy PublicationPolicy) (SuspensionSnapshot, error) {
	snapshot, err := s.captureSuspensionLocked(admission)
	if err == nil {
		return snapshot, nil
	}
	attributed, attributionErr := s.attributeExternalPull(admission, policy)
	if attributionErr != nil {
		return SuspensionSnapshot{}, err
	}
	return s.captureAdmittedSuspensionLocked(attributed)
}

func (s *Store) attributeExternalPull(admission Admission, policy PublicationPolicy) (Admission, error) {
	if err := s.verifyAttributionAuthority(admission); err != nil {
		return Admission{}, err
	}
	observed, err := s.observeAdvancedPrimary(admission, policy)
	if err != nil {
		return Admission{}, err
	}
	if _, err := s.publicationProjectAdvancement(admission.Head, observed.Head); err != nil {
		return Admission{}, err
	}
	if err := verifyUncommittedAdvancementDisjoint(s.identity.PrimaryRoot, admission.Lease.ExpectedBaseOID, observed.Head); err != nil {
		return Admission{}, err
	}
	if admission.Attempt.BaselineTrees.IndexTree == "" || admission.Attempt.BaselineTrees.WorktreeTree == "" {
		return Admission{}, fmt.Errorf("attempt has no lossless execution baseline")
	}
	return Admission{Head: admission.Head, Attempt: admission.Attempt, Lease: admission.Lease, Workspace: admission.Workspace, Snapshot: observed}, nil
}

func (s *Store) verifyAttributionAuthority(admission Admission) error {
	if admission.Lease.InFlightCallID != "" {
		return fmt.Errorf("external pull attribution requires quiescent execution")
	}
	if admission.Head.IntegrationTip != admission.Lease.ExpectedBaseOID {
		return fmt.Errorf("external pull attribution requires the recorded execution base")
	}
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" || head.LiveAttemptID != admission.Attempt.AttemptID || head.LiveLeaseID != admission.Lease.LeaseID || head.ControllerGeneration != admission.Head.ControllerGeneration {
		return fmt.Errorf("external pull attribution requires current controller authority")
	}
	return nil
}

func (s *Store) observeAdvancedPrimary(admission Admission, policy PublicationPolicy) (WorkspaceSnapshot, error) {
	observed, err := CaptureWorkspaceSnapshot(admission.Workspace.Root)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if observed.Head == admission.Lease.ExpectedBaseOID {
		return WorkspaceSnapshot{}, fmt.Errorf("external pull attribution requires an advanced primary")
	}
	descendant, err := isPublicationDescendant(s.identity.PrimaryRoot, admission.Lease.ExpectedBaseOID, observed.Head)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if !descendant {
		return WorkspaceSnapshot{}, fmt.Errorf("primary HEAD %s is not a fast-forward of the recorded execution base %s", observed.Head, admission.Lease.ExpectedBaseOID)
	}
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, policy.LocalRef)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if !exists || local != observed.Head {
		return WorkspaceSnapshot{}, fmt.Errorf("advanced primary is not bound to the publication local ref")
	}
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, policy)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if remote != observed.Head {
		return WorkspaceSnapshot{}, fmt.Errorf("advanced primary %s does not match the configured remote tip %s", observed.Head, remote)
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if authority.Snapshot.HeadOID != observed.Head {
		return WorkspaceSnapshot{}, fmt.Errorf("committed primary authority %s does not match the advanced primary %s", authority.Snapshot.HeadOID, observed.Head)
	}
	return observed, nil
}

func verifyUncommittedAdvancementDisjoint(repo, base, tip string) error {
	advanced, err := gitTrimmedLines(repo, "diff", "--name-only", "--no-renames", base, tip)
	if err != nil {
		return err
	}
	changed := make(map[string]bool, len(advanced))
	for _, path := range advanced {
		changed[path] = true
	}
	uncommitted, err := gitTrimmedLines(repo, "diff", "--name-only", "--no-renames", "HEAD")
	if err != nil {
		return err
	}
	untracked, err := gitTrimmedLines(repo, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	for _, path := range append(uncommitted, untracked...) {
		if changed[path] {
			return fmt.Errorf("external advancement overlaps uncommitted execution path %s", path)
		}
	}
	return nil
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
	snapshot, err := s.captureAdvancementSuspensionLocked(admission, policy)
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
	archive, err := s.mutableAdvancementArchive(admission, remote)
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

func (s *Store) mutableAdvancementArchive(admission Admission, remote string) (EvidenceObjectRef, error) {
	canonical, err := s.admissionCanonicalArchive(admission.Head)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	archive, _, err := s.captureGitObjectArchiveReusing(s.identity.PrimaryRoot, "external:"+remote, canonical, []string{remote})
	return archive, err
}

func (s *Store) mutableAdvancementEvidence(op ExecutionOperation) (EvidencePublicationInput, *EvidenceObjectRef, error) {
	input, seal, err := s.buildSuspensionEvidence(op)
	return input, &seal, err
}
