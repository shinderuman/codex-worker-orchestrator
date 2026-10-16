package controller

import (
	"fmt"
	"os"
)

func (s *Store) RebindUnpublishedCandidate(input PublicationInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, c, ref, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if c.State == candidateObserved || c.ImplementationRef != nil {
		return ExecutionOperationResult{}, fmt.Errorf("remote-observed candidate is immutable")
	}
	remote, err := s.unpublishedRebindRemote(head, c)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	c, err = s.reboundCandidate(c, remote)
	if err != nil {
		return ExecutionOperationResult{}, s.preservePublicationConflict(head, nil, err)
	}
	project, err := s.approvedAmendmentProject(head, c, remote, input.Amendment)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	c.ProjectSnapshotID = project.SnapshotID
	local, localNew, err := s.reboundLocalCandidate(&c, ref, remote)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op, err := s.planCandidateRevision(head, c, ref, publicationRebind)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	applyRebindOperationPlan(&op, head, c, project, remote, local, localNew, input.Amendment)
	if err := s.rebindPublicationEpisode(&op, head, project); err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func applyRebindOperationPlan(op *ExecutionOperation, head RepositoryControllerHead, c AcceptedCandidate, project ProjectSnapshot, remote, local, localNew string, approval *TaskAmendmentApproval) {
	op.Publication.NewTip = remote
	op.Publication.RemoteOID = remote
	op.Publication.Project = &project
	op.Transition.ProjectSnapshotNew = project.SnapshotID
	op.Publication.LocalOld = local
	op.Publication.LocalNew = localNew
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: candidateRetentionRef(c.CommitOID), ExpectedNew: c.CommitOID}}
	if local != localNew {
		op.Transition.Effects = append(op.Transition.Effects, EffectExpectation{Surface: MutationSurfaceRef, Resource: c.Policy.LocalRef, ExpectedOld: local, ExpectedNew: localNew})
	}
	recordAmendmentAuthority(op, head, approval)
}

func recordAmendmentAuthority(op *ExecutionOperation, head RepositoryControllerHead, approval *TaskAmendmentApproval) {
	if approval == nil {
		return
	}
	op.Transition.SourceRootTaskRef = *head.RootTaskRef
	op.Transition.TargetRootTaskRef = approval.NewRootTaskRef
	op.Transition.SourceExecutionTaskRef = *head.ExecutionTaskRef
	op.Transition.TargetExecutionTaskRef = approval.NewExecutionTaskRef
}

func (s *Store) approvedAmendmentProject(head RepositoryControllerHead, c AcceptedCandidate, remote string, approval *TaskAmendmentApproval) (ProjectSnapshot, error) {
	if approval == nil {
		return s.publicationProjectAdvancement(head, remote)
	}
	if err := s.validateApprovedAmendmentClaims(head, c, remote, approval); err != nil {
		return ProjectSnapshot{}, err
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, remote)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	if err := validateApprovedAmendmentProject(authority, approval); err != nil {
		return ProjectSnapshot{}, err
	}
	return authority.Snapshot, nil
}

func (s *Store) validateApprovedAmendmentClaims(head RepositoryControllerHead, c AcceptedCandidate, remote string, approval *TaskAmendmentApproval) error {
	if remote != approval.RemoteOID {
		return fmt.Errorf("approved Task amendment remote differs from the observed advancement")
	}
	if head.RootTaskRef == nil || head.ExecutionTaskRef == nil {
		return fmt.Errorf("approved Task amendment requires held root and execution Task authority")
	}
	if !approval.OldRootTaskRef.Equal(*head.RootTaskRef) || !approval.OldExecutionTaskRef.Equal(*head.ExecutionTaskRef) {
		return fmt.Errorf("approved Task amendment old authority differs from the controller head")
	}
	if !c.TaskRef.Equal(approval.OldExecutionTaskRef) {
		return fmt.Errorf("approved Task amendment candidate provenance differs")
	}
	if approval.NewRootTaskRef.TaskPath != approval.OldRootTaskRef.TaskPath || approval.NewExecutionTaskRef.TaskPath != approval.OldExecutionTaskRef.TaskPath {
		return fmt.Errorf("approved Task amendment changes Task authority path")
	}
	return validateParentMetadataHistory(s.identity.PrimaryRoot, head.IntegrationTip, remote)
}

func validateApprovedAmendmentProject(authority CommittedTaskAuthority, approval *TaskAmendmentApproval) error {
	if !authority.Task.Equal(approval.NewRootTaskRef) {
		return fmt.Errorf("approved Task amendment new root authority differs from the committed Project")
	}
	if !projectHasTask(authority.Snapshot, approval.NewExecutionTaskRef) {
		return fmt.Errorf("approved Task amendment execution Task is absent from the committed Project")
	}
	return nil
}

func (s *Store) reboundCandidate(old AcceptedCandidate, base string) (AcceptedCandidate, error) {
	oldTree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", old.BaseOID+"^{tree}")
	if err != nil {
		return AcceptedCandidate{}, err
	}
	newTree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", base+"^{tree}")
	if err != nil {
		return AcceptedCandidate{}, err
	}
	tree, err := mergeSuspensionTrees(s.identity.PrimaryRoot, oldTree, old.TreeOID, newTree)
	if err != nil {
		return AcceptedCandidate{}, err
	}
	index, err := newSuspensionIndex(s.identity.PrimaryRoot, tree)
	if err != nil {
		return AcceptedCandidate{}, err
	}
	tree, err = normalizeCandidateTree(s.identity.PrimaryRoot, index, base)
	if err != nil {
		return AcceptedCandidate{}, err
	}
	old.BaseOID = base
	old.TreeOID = tree
	old.SnapshotID = digestStrings("rebound-candidate", old.SnapshotID, base, tree)
	old.CommitOID, err = createAcceptedCandidateCommit(s.identity.PrimaryRoot, tree, base, old.Message)
	if err != nil {
		return AcceptedCandidate{}, err
	}
	old.CandidateID = acceptedCandidateID(old)
	old.EvidenceValid = false
	old.Evidence = nil
	old.State = candidatePrepared
	old.ObservedRemoteOID = ""
	old.GitArchive, _, err = s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "candidate:"+old.CommitOID, []string{old.CommitOID})
	return old, err
}

func (s *Store) publicationProjectAdvancement(head RepositoryControllerHead, tip string) (ProjectSnapshot, error) {
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, tip)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	if head.RootTaskRef == nil || !projectHasTask(authority.Snapshot, *head.RootTaskRef) {
		return ProjectSnapshot{}, fmt.Errorf("external advancement changes root Task authority")
	}
	if head.ExecutionTaskRef != nil && !projectHasTask(authority.Snapshot, *head.ExecutionTaskRef) {
		return ProjectSnapshot{}, fmt.Errorf("external advancement changes execution Task authority")
	}
	return authority.Snapshot, nil
}

func normalizeCandidateTree(repo, index, base string) (string, error) {
	defer func() { _ = os.Remove(index) }()
	if _, err := normalizeParentPaths(repo, index, base); err != nil {
		return "", err
	}
	return suspensionGitText(repo, index, nil, "write-tree")
}

func (s *Store) reboundLocalCandidate(c *AcceptedCandidate, ref EvidenceObjectRef, remote string) (string, string, error) {
	old, err := s.LoadAcceptedCandidate(ref)
	if err != nil {
		return "", "", err
	}
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, c.Policy.LocalRef)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", "", fmt.Errorf("candidate local ref is missing")
	}
	if local == old.CommitOID {
		c.State = candidatePromoted
		return local, c.CommitOID, nil
	}
	if local != old.BaseOID && local != remote {
		return "", "", fmt.Errorf("unpublished local ref ownership is ambiguous")
	}
	return local, remote, nil
}

func (s *Store) unpublishedRebindRemote(head RepositoryControllerHead, c AcceptedCandidate) (string, error) {
	remote, err := s.observeCandidateRemote(head, c)
	if err != nil {
		return "", err
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, c.CommitOID, remote)
	if err != nil {
		return "", err
	}
	if contains {
		return "", fmt.Errorf("candidate is positively observed; publish/adopt its immutable lineage")
	}
	return remote, nil
}
