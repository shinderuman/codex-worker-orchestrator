package controller

import "fmt"

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
	if op.Transition.Kind == publicationRebind {
		if previous.State == candidateObserved || c.EvidenceValid || len(c.Evidence) != 0 {
			return fmt.Errorf("candidate rebind preserves immutable/stale authority")
		}
	} else if previous.CandidateID != c.CandidateID {
		return fmt.Errorf("candidate identity changed outside typed rebind")
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
	trees, err := CaptureExecutionTrees(workspace.Root, op.Source.Attempt.ExecutionBaseOID)
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
