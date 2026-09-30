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
	if err := s.validateExecutionTransitionTarget(source, targetProjectSnapshotID, targetTask, targetWorkspace, purpose); err != nil {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, err
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
	if err := s.persistExecutionTarget(attempt, lease); err != nil {
		return TransitionRecord{}, AttemptRecord{}, ExecutionLease{}, err
	}
	target := executionTransitionAuthority(source, targetProjectSnapshotID, targetTask, targetWorkspace, targetSnapshot, attempt, lease)
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

func (s *Store) validateExecutionTransitionTarget(
	source Admission,
	projectSnapshotID string,
	task SemanticTaskRef,
	workspace WorkspaceIdentity,
	purpose string,
) error {
	if purpose == "" {
		return fmt.Errorf("execution transition purpose is required")
	}
	if source.Head.RootTaskRef == nil || !source.Attempt.RootTaskRef.Equal(*source.Head.RootTaskRef) {
		return fmt.Errorf("source root task authority is incomplete")
	}
	project, err := s.LoadProjectSnapshot(projectSnapshotID)
	if err != nil {
		return err
	}
	if !projectContainsTask(project, task) {
		return fmt.Errorf("execution target task is not present in target project snapshot")
	}
	if workspace.RepositoryID != s.identity.LineageID {
		return fmt.Errorf("execution target workspace belongs to a different repository lineage")
	}
	return nil
}

func (s *Store) persistExecutionTarget(attempt AttemptRecord, lease ExecutionLease) error {
	if err := s.writeAttempt(attempt); err != nil {
		return err
	}
	return s.writeLease(lease)
}

func executionTransitionAuthority(
	source Admission,
	projectSnapshotID string,
	task SemanticTaskRef,
	workspace WorkspaceIdentity,
	snapshot WorkspaceSnapshot,
	attempt AttemptRecord,
	lease ExecutionLease,
) TransitionAuthority {
	return TransitionAuthority{
		ProjectSnapshotID: projectSnapshotID,
		RootTaskRef:       source.Attempt.RootTaskRef,
		ExecutionTaskRef:  task,
		EpisodeID:         source.Head.ActiveEpisodeID,
		EpisodeRevision:   source.Head.ActiveEpisodeRevision,
		AttemptID:         attempt.AttemptID,
		LeaseID:           lease.LeaseID,
		WorkspaceID:       workspace.ID,
		WorkspaceSnapshot: snapshot,
	}
}

func (s *Store) CommitExecutionAuthorityTransition(
	record TransitionRecord,
	actual map[string]string,
	targetWorkspace WorkspaceIdentity,
) (Admission, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	attempt, lease, err := s.loadExecutionTransitionTarget(record, targetWorkspace)
	if err != nil {
		return Admission{}, err
	}
	if err := s.markTransitionApplied(record, actual); err != nil {
		return Admission{}, err
	}
	head, err := s.commitAuthorityTransitionLocked(record, actual, true, func(next *RepositoryControllerHead) error {
		applyExecutionTransitionHead(next, record)
		return nil
	})
	if err != nil {
		return Admission{}, err
	}
	attempt.AttemptState = AttemptStateLive
	if err := s.writeAttempt(attempt); err != nil {
		return Admission{}, err
	}
	return Admission{Head: head, Attempt: attempt, Lease: lease, Workspace: targetWorkspace, Snapshot: record.WorkspaceSnapshotNew}, nil
}

func (s *Store) loadExecutionTransitionTarget(
	record TransitionRecord,
	workspace WorkspaceIdentity,
) (AttemptRecord, ExecutionLease, error) {
	if workspace.RepositoryID != s.identity.LineageID || workspace.ID != record.TargetWorkspaceID {
		return AttemptRecord{}, ExecutionLease{}, fmt.Errorf("verified target workspace does not match transition authority")
	}
	attempt, err := s.loadAttempt(record.TargetAttemptID)
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	lease, err := s.loadLease(record.TargetLeaseID)
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	if err := validateExecutionTransitionRecords(record, attempt, lease); err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	return attempt, lease, nil
}

func validateExecutionTransitionRecords(record TransitionRecord, attempt AttemptRecord, lease ExecutionLease) error {
	if !attempt.RootTaskRef.Equal(record.TargetRootTaskRef) || !attempt.SemanticTaskRef.Equal(record.TargetExecutionTaskRef) {
		return fmt.Errorf("target attempt does not match transition authority")
	}
	if lease.AttemptID != attempt.AttemptID || !lease.SemanticTaskRef.Equal(record.TargetExecutionTaskRef) || lease.WorkspaceID != record.TargetWorkspaceID {
		return fmt.Errorf("target execution lease does not match transition authority")
	}
	if lease.ControllerGeneration != record.TargetGeneration || lease.ExpectedWorkspaceSnapshotID != record.WorkspaceSnapshotNew.ID {
		return fmt.Errorf("target execution lease generation or workspace snapshot is stale")
	}
	return nil
}

func applyExecutionTransitionHead(next *RepositoryControllerHead, record TransitionRecord) {
	root := record.TargetRootTaskRef
	execution := record.TargetExecutionTaskRef
	next.ProjectSnapshotID = record.ProjectSnapshotNew
	next.RootTaskRef = &root
	next.ExecutionTaskRef = &execution
	next.ActiveEpisodeID = record.TargetEpisodeID
	next.ActiveEpisodeRevision = record.TargetEpisodeRevision
	next.LiveAttemptID = record.TargetAttemptID
	next.LiveLeaseID = record.TargetLeaseID
}

func projectContainsTask(snapshot ProjectSnapshot, task SemanticTaskRef) bool {
	for _, candidate := range snapshot.Tasks {
		if candidate.Equal(task) {
			return true
		}
	}
	return false
}
