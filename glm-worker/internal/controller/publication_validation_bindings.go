package controller

import "fmt"

const (
	amendmentRefsAbsent = iota
	amendmentRefsComplete
	amendmentRefsPartial
)

func (s *Store) validatePublicationCandidateBinding(op ExecutionOperation, c AcceptedCandidate) error {
	seal, err := s.LoadAttemptSeal(c.SealRef)
	if err != nil {
		return err
	}
	if seal.AttemptID != c.AttemptID || !seal.SemanticTaskRef.Equal(c.TaskRef) || !seal.RootTaskRef.Equal(c.RootTaskRef) {
		return fmt.Errorf("candidate seal subject differs")
	}
	roots, err := s.VerifyGitObjectArchive(c.GitArchive)
	if err != nil {
		return err
	}
	if len(roots) != 1 || roots[0].OID != c.CommitOID || roots[0].Type != gitCommitObjectType {
		return fmt.Errorf("candidate archive root differs")
	}
	if op.Publication.Before == nil {
		return validateCandidateAcceptanceBinding(op, c)
	}
	return s.validateCandidateRevisionLineage(op, c)
}

func (s *Store) validateCandidateRevisionLineage(op ExecutionOperation, c AcceptedCandidate) error {
	previous, err := s.LoadAcceptedCandidate(*op.Publication.Before)
	if err != nil {
		return err
	}
	switch op.Transition.Kind {
	case publicationRebind:
		if err := s.validateParentSourceRevision(op, c); err != nil {
			return err
		}
		return s.validateReboundCandidateLineage(op, previous, c)
	default:
		if previous.CandidateID != c.CandidateID {
			return fmt.Errorf("candidate identity changed outside typed rebind")
		}
	}
	return nil
}

func (s *Store) validateReboundCandidateLineage(op ExecutionOperation, previous, c AcceptedCandidate) error {
	if op.Publication.RemoteOID == "" || op.Publication.RemoteOID != op.Publication.NewTip || op.Publication.RemoteOID != c.BaseOID {
		return fmt.Errorf("candidate rebind remote observation differs from adopted base")
	}
	if previous.State == candidateObserved || c.EvidenceValid || len(c.Evidence) != 0 {
		return fmt.Errorf("candidate rebind preserves immutable/stale authority")
	}
	return s.validateApprovedAmendmentTransition(op)
}

func (s *Store) validateApprovedAmendmentTransition(op ExecutionOperation) error {
	refs := amendmentTransitionRefs(op.Transition)
	if refs == amendmentRefsAbsent {
		return nil
	}
	if refs == amendmentRefsPartial {
		return fmt.Errorf("approved Task amendment authority is incomplete")
	}
	if err := s.validateAmendmentHeadAuthority(op.Transition); err != nil {
		return err
	}
	return s.validateAmendmentProjectAuthority(op)
}

func amendmentTransitionRefs(record TransitionRecord) int {
	refs := 0
	for _, ref := range []SemanticTaskRef{record.SourceRootTaskRef, record.TargetRootTaskRef, record.SourceExecutionTaskRef, record.TargetExecutionTaskRef} {
		if !ref.Empty() {
			refs++
		}
	}
	if refs == 0 {
		return amendmentRefsAbsent
	}
	if refs == 4 {
		return amendmentRefsComplete
	}
	return amendmentRefsPartial
}

func (s *Store) validateAmendmentHeadAuthority(record TransitionRecord) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	if head.RootTaskRef == nil || head.ExecutionTaskRef == nil {
		return fmt.Errorf("approved Task amendment authority is missing from the controller head")
	}
	if !amendmentAuthorityMatches(head, record) {
		return fmt.Errorf("approved Task amendment authority is stale")
	}
	if record.TargetRootTaskRef.TaskPath != record.SourceRootTaskRef.TaskPath ||
		record.TargetExecutionTaskRef.TaskPath != record.SourceExecutionTaskRef.TaskPath {
		return fmt.Errorf("approved Task amendment changes Task authority path")
	}
	return nil
}

func amendmentAuthorityMatches(head RepositoryControllerHead, record TransitionRecord) bool {
	sourceMatch := head.RootTaskRef.Equal(record.SourceRootTaskRef) && head.ExecutionTaskRef.Equal(record.SourceExecutionTaskRef)
	targetMatch := head.RootTaskRef.Equal(record.TargetRootTaskRef) && head.ExecutionTaskRef.Equal(record.TargetExecutionTaskRef)
	return sourceMatch || targetMatch
}

func (s *Store) validateAmendmentProjectAuthority(op ExecutionOperation) error {
	if op.Publication.Project == nil || !projectHasTask(*op.Publication.Project, op.Transition.TargetExecutionTaskRef) {
		return fmt.Errorf("approved Task amendment Project lacks the amended execution Task")
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, op.Publication.Project.HeadOID)
	if err != nil {
		return err
	}
	if !authority.Task.Equal(op.Transition.TargetRootTaskRef) || authority.ProjectSnapshotID != op.Publication.Project.SnapshotID {
		return fmt.Errorf("approved Task amendment differs from the committed advancement")
	}
	return nil
}

func (s *Store) verifyAcceptanceSource(op ExecutionOperation) error {
	workspace, err := ResolveWorkspaceIdentity(op.Source.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	if workspace != op.Source.Workspace {
		return fmt.Errorf("candidate source workspace was replaced")
	}
	head, err := gitTrimmed(workspace.Root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != op.Source.Attempt.ExecutionBaseOID {
		return fmt.Errorf("candidate execution base changed after prepare")
	}
	metadataBase, err := s.projectMetadataBase(op.Source.Head.ProjectSnapshotID)
	if err != nil {
		return err
	}
	trees, err := CaptureExecutionTrees(workspace.Root, metadataBase)
	if err != nil {
		return err
	}
	if trees != op.Suspension.Current {
		return fmt.Errorf("candidate source changed after prepare")
	}
	return nil
}

func validateCandidateAcceptanceBinding(op ExecutionOperation, c AcceptedCandidate) error {
	if op.Transition.Kind != publicationAccept || op.Suspension == nil {
		return fmt.Errorf("candidate acceptance source is missing")
	}
	if !c.TaskRef.Equal(op.Source.Attempt.SemanticTaskRef) || c.AttemptID != op.Source.Attempt.AttemptID || c.SnapshotID != op.Source.Snapshot.ID {
		return fmt.Errorf("candidate acceptance source differs")
	}
	return nil
}
