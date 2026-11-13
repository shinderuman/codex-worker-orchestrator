package controller

import "fmt"

func (s *Store) registeredParentOnlyAuthority(head RepositoryControllerHead) (TaskAuthority, error) {
	if err := s.validateRegisteredTaskStart(head); err != nil {
		return TaskAuthority{}, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(s.identity.PrimaryRoot)
	if err != nil {
		return TaskAuthority{}, err
	}
	if !IsPristine(head) && snapshot.Head != head.IntegrationTip {
		return TaskAuthority{}, fmt.Errorf("registered parent-only source differs from the idle integration tip")
	}
	return s.captureRegisteredTaskAuthority(snapshot)
}

func parentOnlySourceOID(attempt AttemptRecord) string {
	if attempt.SourceRevision != nil {
		return attempt.SourceRevision.AuthorityOID
	}
	return attempt.ExecutionBaseOID
}

func (s *Store) parentOnlySourceAuthority(attempt AttemptRecord) (TaskAuthority, error) {
	if attempt.SourceRevision == nil {
		return ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	}
	head, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", "HEAD")
	if err != nil {
		return TaskAuthority{}, err
	}
	if head != attempt.ExecutionBaseOID || attempt.SourceRevision.Snapshot.Head != head {
		return TaskAuthority{}, fmt.Errorf("registered parent-only source base changed")
	}
	if _, err := gitTrimmed(s.identity.PrimaryRoot, "cat-file", "-t", parentOnlySourceOID(attempt)); err != nil {
		if err := s.restoreExecutionBaseline(s.identity.PrimaryRoot, attempt); err != nil {
			return TaskAuthority{}, err
		}
	}
	return ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, parentOnlySourceOID(attempt))
}
