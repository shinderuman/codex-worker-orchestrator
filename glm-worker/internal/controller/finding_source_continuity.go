package controller

import "fmt"

type findingSourceAuthority struct {
	Task       SemanticTaskRef
	Project    string
	Generation uint64
}

func (s *Store) findingSourceContinuedToCurrentAuthority(finding FindingRecord, attempt AttemptRecord, head RepositoryControllerHead) (bool, error) {
	if finding.SourceSemanticTaskRef.TaskPath != head.ExecutionTaskRef.TaskPath {
		return false, nil
	}
	frontier := []findingSourceAuthority{{Task: *head.ExecutionTaskRef, Project: head.ProjectSnapshotID, Generation: head.ControllerGeneration}}
	seen := make(map[string]bool)
	for len(frontier) != 0 {
		current := frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		continued, previous, err := s.findingSourceRevisionPredecessors(head, current, finding, attempt, seen)
		if err != nil || continued {
			return continued, err
		}
		frontier = append(frontier, previous...)
	}
	return false, nil
}

func (s *Store) findingSourceRevisionPredecessors(head RepositoryControllerHead, current findingSourceAuthority, finding FindingRecord, attempt AttemptRecord, seen map[string]bool) (bool, []findingSourceAuthority, error) {
	subject, published, err := s.publishedTaskSubjectRevision(head, current.Task)
	if err != nil || !published {
		return false, nil, err
	}
	revision, err := s.LoadTaskIndexRevision(subject)
	if err != nil {
		return false, nil, err
	}
	var previous []findingSourceAuthority
	for _, ref := range revision.SourceRevisions {
		record, err := s.LoadTaskSourceRevision(ref)
		if err != nil {
			return false, nil, err
		}
		if seen[record.SourceRevisionID] || record.AttemptID != attempt.AttemptID || !record.RevisedTaskRef.Equal(current.Task) || record.RevisedProjectSnapshotID != current.Project {
			continue
		}
		seen[record.SourceRevisionID] = true
		transition, err := s.findingSourceRevisionTransition(record, current)
		if err != nil {
			return false, nil, err
		}
		if findingSourceRevisionAnchors(transition, finding) {
			return true, nil, nil
		}
		previous = append(previous, findingSourceAuthority{Task: record.PreviousTaskRef, Project: record.PreviousProjectSnapshotID, Generation: transition.SourceGeneration})
	}
	return false, previous, nil
}

func (s *Store) findingSourceRevisionTransition(record TaskSourceRevisionRecord, current findingSourceAuthority) (TransitionRecord, error) {
	transition, phase, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return transition, err
	}
	valid := phase.Phase == TransitionPhaseFinalized && transition.Kind == executionParentSourceImport &&
		transition.SourceAttemptID == record.AttemptID && transition.TargetAttemptID == record.AttemptID &&
		transition.SourceExecutionTaskRef.Equal(record.PreviousTaskRef) && transition.TargetExecutionTaskRef.Equal(record.RevisedTaskRef) &&
		transition.ProjectSnapshotOld == record.PreviousProjectSnapshotID && transition.ProjectSnapshotNew == record.RevisedProjectSnapshotID &&
		transition.CommittedGeneration == record.ControllerGeneration && transition.TargetGeneration <= current.Generation
	if !valid {
		return transition, fmt.Errorf("finding source revision lacks its finalized same-attempt transition")
	}
	return transition, nil
}

func findingSourceRevisionAnchors(transition TransitionRecord, finding FindingRecord) bool {
	return transition.SourceExecutionTaskRef.Equal(finding.SourceSemanticTaskRef) &&
		transition.ProjectSnapshotOld == finding.ProjectSnapshotID &&
		transition.SourceGeneration == finding.SourceControllerGeneration &&
		transition.WorkspaceSnapshotOld.ID == finding.SourceWorkspaceSnapshotID
}
