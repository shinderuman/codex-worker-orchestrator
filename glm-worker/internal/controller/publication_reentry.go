package controller

import "fmt"

func (s *Store) ReenterAcceptedCandidate(input PublicationInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, c, ref, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if c.ImplementationRef != nil {
		return ExecutionOperationResult{}, fmt.Errorf("completion candidate cannot reenter execution")
	}
	snapshot, remote, err := s.candidateReentrySource(head, c)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	c.EvidenceValid = false
	if c.State == candidateObserved {
		c.EvidenceValid = true
	}
	op, err := s.planCandidateRevision(head, c, ref, publicationReenter)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op.Publication.RemoteOID = remote
	op.Suspension = &snapshot
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceState, Resource: "accepted-candidate", ExpectedOld: ref.Digest, ExpectedNew: op.Publication.After.Digest}}
	if c.State != candidateObserved {
		if err := s.planUnobservedLocalRetirement(&op, c); err != nil {
			return ExecutionOperationResult{}, err
		}
	}
	metadata := candidateReentryMetadata(c)
	if err := s.verifyCandidateReentryMetadata(metadata); err != nil {
		return ExecutionOperationResult{}, err
	}
	op.Transition.Effects = append(op.Transition.Effects, candidateReentryMetadataEffects(metadata)...)
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func candidateReentryMetadata(c AcceptedCandidate) []TerminalMetadataFile {
	if c.State != candidatePromoted || !c.TaskRef.Equal(c.RootTaskRef) {
		return nil
	}
	files := make([]TerminalMetadataFile, 0, len(c.TerminalFiles))
	for _, file := range c.TerminalFiles {
		file.OldOID, file.NewOID = file.NewOID, file.OldOID
		file.OldBytes, file.NewBytes = file.NewBytes, file.OldBytes
		files = append(files, file)
	}
	return files
}

func candidateReentryMetadataEffects(files []TerminalMetadataFile) []EffectExpectation {
	return terminalMetadataEffects(TerminalMetadataOperation{Files: files}, "")[1:]
}

func (s *Store) verifyCandidateReentryMetadata(files []TerminalMetadataFile) error {
	op := ExecutionOperation{Terminal: &TerminalMetadataOperation{Files: files}}
	actual, err := s.observeTerminalFileEffects(op)
	if err != nil {
		return err
	}
	for _, effect := range candidateReentryMetadataEffects(files) {
		if value := actual[effect.Key()]; value != effect.ExpectedOld && value != effect.ExpectedNew {
			return fmt.Errorf("candidate reentry metadata has an unattributed change: %s", effect.Resource)
		}
	}
	return nil
}

func (s *Store) applyCandidateReentry(op ExecutionOperation, c AcceptedCandidate) error {
	files := candidateReentryMetadata(c)
	if err := s.verifyCandidateReentryMetadata(files); err != nil {
		return err
	}
	effects := candidateReentryMetadataEffects(files)
	for index, file := range files {
		if err := s.applyTerminalMetadataFile(file, effects[index*2:index*2+2]); err != nil {
			return err
		}
	}
	return s.applyPlannedLocalRef(op, c)
}

func (s *Store) planUnobservedLocalRetirement(op *ExecutionOperation, c AcceptedCandidate) error {
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, c.Policy.LocalRef)
	if err != nil {
		return err
	}
	if !exists || (local != c.BaseOID && local != c.CommitOID) {
		return fmt.Errorf("unpublished local ref retirement authority is ambiguous")
	}
	if local == c.CommitOID {
		op.Publication.LocalOld = local
		op.Publication.LocalNew = c.BaseOID
		op.Transition.Effects = append(op.Transition.Effects, EffectExpectation{Surface: MutationSurfaceRef, Resource: c.Policy.LocalRef, ExpectedOld: local, ExpectedNew: c.BaseOID})
	}
	return nil
}

func (s *Store) candidateReentrySource(head RepositoryControllerHead, c AcceptedCandidate) (SuspensionSnapshot, string, error) {
	seal, err := s.LoadAttemptSeal(c.SealRef)
	if err != nil {
		return SuspensionSnapshot{}, "", err
	}
	snapshot, err := s.LoadSuspension(s.identity.PrimaryRoot, seal.OperationalSnapshotID)
	if err != nil {
		return SuspensionSnapshot{}, "", err
	}
	if _, err := RebindSuspensionTrees(s.identity.PrimaryRoot, snapshot, head.IntegrationTip); err != nil {
		return SuspensionSnapshot{}, "", err
	}
	remote, err := s.observeCandidateRemote(head, c)
	if err != nil {
		return SuspensionSnapshot{}, "", err
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, c.CommitOID, remote)
	if err != nil {
		return SuspensionSnapshot{}, "", err
	}
	if contains && c.State != candidateObserved {
		return SuspensionSnapshot{}, "", fmt.Errorf("positive remote observation must be committed before candidate re-entry")
	}
	if remote != head.IntegrationTip {
		return SuspensionSnapshot{}, "", fmt.Errorf("external advancement must be adopted/rebound before candidate re-entry")
	}
	return snapshot, remote, nil
}
