package controller

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) BeginAuthorityTransition(intent TransitionIntent) (TransitionRecord, error) {
	if intent.Kind == "" {
		return TransitionRecord{}, fmt.Errorf("transition kind is required")
	}
	lock, err := s.acquireMutationLock()
	if err != nil {
		return TransitionRecord{}, err
	}
	defer func() { _ = lock.Close() }()
	if err := s.validateTransitionSource(intent); err != nil {
		return TransitionRecord{}, err
	}
	if err := validateTransitionTarget(intent.Target); err != nil {
		return TransitionRecord{}, err
	}
	transitionID, err := state.NewUUID()
	if err != nil {
		return TransitionRecord{}, err
	}
	source := intent.Source
	record := TransitionRecord{
		SchemaVersion:          controllerSchemaVersion,
		TransitionID:           transitionID,
		Kind:                   intent.Kind,
		SourceGeneration:       source.Head.ControllerGeneration,
		PreparedGeneration:     source.Head.ControllerGeneration + 1,
		TargetGeneration:       source.Head.ControllerGeneration + 2,
		SourceEpisodeID:        source.Head.ActiveEpisodeID,
		SourceEpisodeRevision:  source.Head.ActiveEpisodeRevision,
		TargetEpisodeID:        intent.Target.EpisodeID,
		TargetEpisodeRevision:  intent.Target.EpisodeRevision,
		SourceAttemptID:        source.Attempt.AttemptID,
		TargetAttemptID:        intent.Target.AttemptID,
		SourceLeaseID:          source.Lease.LeaseID,
		TargetLeaseID:          intent.Target.LeaseID,
		SourceWorkspaceID:      source.Workspace.ID,
		TargetWorkspaceID:      intent.Target.WorkspaceID,
		SourceRootTaskRef:      source.Attempt.RootTaskRef,
		TargetRootTaskRef:      intent.Target.RootTaskRef,
		SourceExecutionTaskRef: source.Attempt.SemanticTaskRef,
		TargetExecutionTaskRef: intent.Target.ExecutionTaskRef,
		WorkspaceSnapshotOld:   source.Snapshot,
		WorkspaceSnapshotNew:   intent.Target.WorkspaceSnapshot,
		ProjectSnapshotOld:     source.Head.ProjectSnapshotID,
		ProjectSnapshotNew:     intent.Target.ProjectSnapshotID,
		Effects:                append([]EffectExpectation(nil), intent.Effects...),
		CreatedAt:              time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.transitionPath(transitionID), record); err != nil {
		return TransitionRecord{}, err
	}
	transitionState := TransitionState{
		SchemaVersion: controllerSchemaVersion,
		TransitionID:  transitionID,
		Phase:         TransitionPhasePrepared,
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.writeTransitionState(transitionState); err != nil {
		return TransitionRecord{}, err
	}
	next := source.Head
	next.ControllerGeneration = record.PreparedGeneration
	next.PendingTransitionID = transitionID
	if err := s.writeHeadCAS(source.Head.ControllerGeneration, next); err != nil {
		return TransitionRecord{}, err
	}
	return record, nil
}

func (s *Store) validateTransitionSource(intent TransitionIntent) error {
	source := intent.Source
	if source.Head.ControllerGeneration != intent.ExpectedGeneration {
		return fmt.Errorf("transition source generation does not match expected generation")
	}
	current, err := s.AdmitMutation(source.Lease.SemanticTaskRef, source.Workspace, source.Snapshot)
	if err != nil {
		return err
	}
	if current.Head.ControllerGeneration != intent.ExpectedGeneration ||
		current.Attempt.AttemptID != source.Attempt.AttemptID ||
		current.Lease.LeaseID != source.Lease.LeaseID ||
		current.Workspace.ID != source.Workspace.ID ||
		current.Snapshot.ID != source.Snapshot.ID ||
		current.Head.ProjectSnapshotID != source.Head.ProjectSnapshotID {
		return fmt.Errorf("transition source authority is stale")
	}
	return nil
}

func validateTransitionTarget(target TransitionAuthority) error {
	if target.ProjectSnapshotID == "" {
		return fmt.Errorf("transition target project snapshot is required")
	}
	if target.RootTaskRef.Empty() || target.ExecutionTaskRef.Empty() {
		return fmt.Errorf("transition target root and execution task authority are required")
	}
	if target.AttemptID == "" || target.LeaseID == "" || target.WorkspaceID == "" {
		return fmt.Errorf("transition target attempt, lease, and workspace authority are required")
	}
	if target.WorkspaceSnapshot.ID == "" {
		return fmt.Errorf("transition target workspace snapshot is required")
	}
	return nil
}

func authorityFromAdmission(admission Admission, snapshot WorkspaceSnapshot) TransitionAuthority {
	return TransitionAuthority{
		ProjectSnapshotID: admission.Head.ProjectSnapshotID,
		RootTaskRef:       admission.Attempt.RootTaskRef,
		ExecutionTaskRef:  admission.Attempt.SemanticTaskRef,
		EpisodeID:         admission.Head.ActiveEpisodeID,
		EpisodeRevision:   admission.Head.ActiveEpisodeRevision,
		AttemptID:         admission.Attempt.AttemptID,
		LeaseID:           admission.Lease.LeaseID,
		WorkspaceID:       admission.Workspace.ID,
		WorkspaceSnapshot: snapshot,
	}
}
