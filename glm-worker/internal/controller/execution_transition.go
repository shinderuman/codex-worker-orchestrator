package controller

import "fmt"

func (s *Store) PrepareExecutionAuthorityTransition(
	source Admission,
	targetProjectSnapshotID string,
	targetTask SemanticTaskRef,
	targetWorkspace WorkspaceIdentity,
	targetSnapshot WorkspaceSnapshot,
	purpose string,
	effects []EffectExpectation,
) (TransitionRecord, AttemptRecord, ExecutionLease, error) {
	if purpose == "" {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, fmt.Errorf("execution transition purpose is required")
	}
	if source.Head.RootTaskRef == nil || !source.Attempt.RootTaskRef.Equal(*source.Head.RootTaskRef) {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, fmt.Errorf("source root task authority is incomplete")
	}
	project, err := s.LoadProjectSnapshot(targetProjectSnapshotID)
	if err != nil {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, err
	}
	if !projectContainsTask(project, targetTask) {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, fmt.Errorf("execution target task is not present in target project snapshot")
	}
	if targetWorkspace.RepositoryID != s.identity.LineageID {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, fmt.Errorf("execution target workspace belongs to a different repository lineage")
	}
	targetGeneration := source.Head.ControllerGeneration + 3
	attempt, lease, err := newExecutionRecords(
		targetTask,
		source.Attempt.RootTaskRef,
		targetWorkspace,
		targetSnapshot,
		targetGeneration,
		purpose,
	)
	if err != nil {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, err
	}
	attempt.AttemptState = AttemptStatePrepared
	attempt.PredecessorAttemptID = source.Attempt.AttemptID
	if err := s.writeAttempt(attempt); err != nil {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, err
	}
	if err := s.writeLease(lease); err != nil {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, err
	}
	target := TransitionAuthority{
		ProjectSnapshotID: targetProjectSnapshotID,
		RootTaskRef:       source.Attempt.RootTaskRef,
		ExecutionTaskRef:  targetTask,
		EpisodeID:         source.Head.ActiveEpisodeID,
		EpisodeRevision:   source.Head.ActiveEpisodeRevision,
		AttemptID:         attempt.AttemptID,
		LeaseID:           lease.LeaseID,
		WorkspaceID:       targetWorkspace.ID,
		WorkspaceSnapshot: targetSnapshot,
	}
	record, err := s.BeginAuthorityTransition(TransitionIntent{
		Kind:               "execution-authority:" + purpose,
		ExpectedGeneration: source.Head.ControllerGeneration,
		Source:             source,
		Target:             target,
		Effects:            effects,
	})
	if err != nil {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, err
	}
	return record, attempt, lease, nil
}

func (s *Store) CommitExecutionAuthorityTransition(
	record TransitionRecord,
	actual map[string]string,
	targetWorkspace WorkspaceIdentity,
) (Admission, error) {
	if targetWorkspace.RepositoryID != s.identity.LineageID || targetWorkspace.ID != record.TargetWorkspaceID {
		return Admission{}, fmt.Errorf("verified target workspace does not match transition authority")
	}
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	attempt, err := s.loadAttempt(record.TargetAttemptID)
	if err != nil {
		return Admission{}, err
	}
	lease, err := s.loadLease(record.TargetLeaseID)
	if err != nil {
		return Admission{}, err
	}
	if !attempt.RootTaskRef.Equal(record.TargetRootTaskRef) || !attempt.SemanticTaskRef.Equal(record.TargetExecutionTaskRef) {
		return Admission{}, fmt.Errorf("target attempt does not match transition authority")
	}
	if lease.AttemptID != attempt.AttemptID || !lease.SemanticTaskRef.Equal(record.TargetExecutionTaskRef) || lease.WorkspaceID != record.TargetWorkspaceID {
		return Admission{}, fmt.Errorf("target execution lease does not match transition authority")
	}
	if lease.ControllerGeneration != record.TargetGeneration || lease.ExpectedWorkspaceSnapshotID != record.WorkspaceSnapshotNew.ID {
		return Admission{}, fmt.Errorf("target execution lease generation or workspace snapshot is stale")
	}
	if err := s.markTransitionApplied(record, actual); err != nil {
		return Admission{}, err
	}
	head, err := s.commitAuthorityTransitionLocked(record, actual, true, func(next *RepositoryControllerHead) error {
		root := record.TargetRootTaskRef
		execution := record.TargetExecutionTaskRef
		next.ProjectSnapshotID = record.ProjectSnapshotNew
		next.RootTaskRef = &root
		next.ExecutionTaskRef = &execution
		next.ActiveEpisodeID = record.TargetEpisodeID
		next.ActiveEpisodeRevision = record.TargetEpisodeRevision
		next.LiveAttemptID = record.TargetAttemptID
		next.LiveLeaseID = record.TargetLeaseID
		return nil
	})
	if err != nil {
		return Admission{}, err
	}
	attempt.AttemptState = AttemptStateLive
	if err := s.writeAttempt(attempt); err != nil {
		return Admission{}, err
	}
	return Admission{
		Head:      head,
		Attempt:   attempt,
		Lease:     lease,
		Workspace: targetWorkspace,
		Snapshot:  record.WorkspaceSnapshotNew,
	}, nil
}

func projectContainsTask(snapshot ProjectSnapshot, task SemanticTaskRef) bool {
	for _, candidate := range snapshot.Tasks {
		if candidate.Equal(task) {
			return true
		}
	}
	return false
}
