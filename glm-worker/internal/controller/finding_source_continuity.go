package controller

import (
	"fmt"
	"os"
	"path/filepath"
)

type findingSourceAuthority struct {
	Task       SemanticTaskRef
	Project    string
	Generation uint64
}

type findingAuthorityProgressionStep struct {
	sourceGeneration        uint64
	targetWorkspaceSnapshot string
	transitionID            string
	fromTransition          bool
	anchor                  *findingAuthorityAnchor
}

type findingAuthorityAnchor struct {
	task      SemanticTaskRef
	project   string
	workspace string
}

type findingSourceRecordOutcome int

const (
	findingSourceRecordSkip findingSourceRecordOutcome = iota
	findingSourceRecordPrevious
	findingSourceRecordContinued
)

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
		before, outcome, err := s.findingSourceRevisionRecordPredecessor(ref, current, finding, attempt, seen)
		if err != nil || outcome == findingSourceRecordContinued {
			return outcome == findingSourceRecordContinued, nil, err
		}
		if outcome == findingSourceRecordPrevious {
			previous = append(previous, *before)
		}
	}
	return false, previous, nil
}

func (s *Store) findingSourceRevisionRecordPredecessor(ref EvidenceObjectRef, current findingSourceAuthority, finding FindingRecord, attempt AttemptRecord, seen map[string]bool) (*findingSourceAuthority, findingSourceRecordOutcome, error) {
	record, err := s.LoadTaskSourceRevision(ref)
	if err != nil {
		return nil, findingSourceRecordSkip, err
	}
	if seen[record.SourceRevisionID] || record.AttemptID != attempt.AttemptID || !record.RevisedTaskRef.Equal(current.Task) || record.RevisedProjectSnapshotID != current.Project {
		return nil, findingSourceRecordSkip, nil
	}
	seen[record.SourceRevisionID] = true
	transition, err := s.findingSourceRevisionTransition(record, current)
	if err != nil {
		return nil, findingSourceRecordSkip, err
	}
	if findingSourceRevisionAnchors(transition, finding) {
		return nil, findingSourceRecordContinued, nil
	}
	before := findingSourceAuthority{Task: record.PreviousTaskRef, Project: record.PreviousProjectSnapshotID, Generation: transition.SourceGeneration}
	bridged, err := s.findingSourceProgressionBridgesToObservation(finding, attempt, before, transition.WorkspaceSnapshotOld.ID)
	if err != nil {
		return nil, findingSourceRecordSkip, err
	}
	if bridged {
		return nil, findingSourceRecordContinued, nil
	}
	return &before, findingSourceRecordPrevious, nil
}

func (s *Store) findingSourceRevisionTransition(record TaskSourceRevisionRecord, current findingSourceAuthority) (TransitionRecord, error) {
	transition, phase, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return transition, err
	}
	valid := phase.Phase == TransitionPhaseFinalized && findingSourceRevisionKindOwnsTransition(transition) &&
		transition.SourceAttemptID == record.AttemptID && transition.TargetAttemptID == record.AttemptID &&
		transition.SourceExecutionTaskRef.Equal(record.PreviousTaskRef) && transition.TargetExecutionTaskRef.Equal(record.RevisedTaskRef) &&
		transition.ProjectSnapshotOld == record.PreviousProjectSnapshotID && transition.ProjectSnapshotNew == record.RevisedProjectSnapshotID &&
		transition.CommittedGeneration == record.ControllerGeneration && transition.TargetGeneration <= current.Generation
	if !valid {
		return transition, fmt.Errorf("finding source revision lacks its finalized same-attempt transition")
	}
	return transition, nil
}

func findingSourceRevisionKindOwnsTransition(transition TransitionRecord) bool {
	switch transition.Kind {
	case executionParentSourceImport:
		return true
	case executionTaskAuthoritySync:
		return transition.SourceWorkspaceID == transition.TargetWorkspaceID &&
			transition.SourceRootTaskRef.Equal(transition.SourceExecutionTaskRef) &&
			transition.TargetRootTaskRef.Equal(transition.TargetExecutionTaskRef)
	default:
		return false
	}
}

func findingSourceRevisionAnchors(transition TransitionRecord, finding FindingRecord) bool {
	return transition.SourceExecutionTaskRef.Equal(finding.SourceSemanticTaskRef) &&
		transition.ProjectSnapshotOld == finding.ProjectSnapshotID &&
		transition.SourceGeneration == finding.SourceControllerGeneration &&
		transition.WorkspaceSnapshotOld.ID == finding.SourceWorkspaceSnapshotID
}

func (s *Store) findingSourceProgressionBridgesToObservation(finding FindingRecord, attempt AttemptRecord, before findingSourceAuthority, workspace string) (bool, error) {
	if !findingSourceObservationReachable(finding, before) {
		return false, nil
	}
	progressions, err := s.findingAuthorityProgressions(attempt.AttemptID)
	if err != nil {
		return false, err
	}
	generation := before.Generation
	for generation > finding.SourceControllerGeneration {
		step, ok, err := s.nextFindingAuthorityProgression(progressions, generation)
		if err != nil || !ok {
			return false, err
		}
		if step.sourceGeneration >= generation {
			return false, fmt.Errorf("finding authority progression does not advance generation")
		}
		if !findingProgressionStepMatchesAuthority(step, before, workspace) {
			return false, nil
		}
		if findingAuthorityProgressionAnchors(step, finding) {
			return true, nil
		}
		if step.sourceGeneration < finding.SourceControllerGeneration {
			return false, nil
		}
		generation = step.sourceGeneration
		workspace = step.anchor.workspace
	}
	return false, nil
}

func findingSourceObservationReachable(finding FindingRecord, before findingSourceAuthority) bool {
	return before.Task.Equal(finding.SourceSemanticTaskRef) &&
		before.Project == finding.ProjectSnapshotID &&
		before.Generation > finding.SourceControllerGeneration
}

func findingAuthorityProgressionAnchors(step findingAuthorityProgressionStep, finding FindingRecord) bool {
	return step.fromTransition &&
		step.sourceGeneration == finding.SourceControllerGeneration &&
		step.anchor.task.Equal(finding.SourceSemanticTaskRef) &&
		step.anchor.project == finding.ProjectSnapshotID &&
		step.anchor.workspace == finding.SourceWorkspaceSnapshotID
}

func (s *Store) nextFindingAuthorityProgression(progressions map[uint64][]findingAuthorityProgressionStep, generation uint64) (findingAuthorityProgressionStep, bool, error) {
	var step findingAuthorityProgressionStep
	found := false
	for _, candidate := range progressions[generation] {
		if candidate.fromTransition {
			phase, err := s.loadTransitionState(candidate.transitionID)
			if err != nil {
				return step, false, err
			}
			if phase.Phase != TransitionPhaseFinalized {
				continue
			}
		}
		if found {
			return step, false, fmt.Errorf("finding authority progression at generation %d is ambiguous", generation)
		}
		step = candidate
		found = true
	}
	return step, found, nil
}

func (s *Store) findingAuthorityProgressions(attemptID string) (map[uint64][]findingAuthorityProgressionStep, error) {
	progressions := map[uint64][]findingAuthorityProgressionStep{}
	entries, err := os.ReadDir(filepath.Join(s.dir, "transitions"))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		var record TransitionRecord
		if err := readJSON(filepath.Join(s.dir, "transitions", entry.Name()), &record); err != nil {
			return nil, err
		}
		if !findingAuthorityProgressionPreserved(record, attemptID) {
			continue
		}
		progressions[record.TargetGeneration] = append(progressions[record.TargetGeneration], findingAuthorityProgressionStep{
			sourceGeneration:        record.SourceGeneration,
			targetWorkspaceSnapshot: record.WorkspaceSnapshotNew.ID,
			transitionID:            record.TransitionID,
			fromTransition:          true,
			anchor:                  &findingAuthorityAnchor{task: record.SourceExecutionTaskRef, project: record.ProjectSnapshotOld, workspace: record.WorkspaceSnapshotOld.ID},
		})
	}
	if err := s.appendFindingMutationProgressions(attemptID, progressions); err != nil {
		return nil, err
	}
	return progressions, nil
}

func (s *Store) appendFindingMutationProgressions(attemptID string, progressions map[uint64][]findingAuthorityProgressionStep) error {
	mutations, err := os.ReadDir(filepath.Join(s.dir, "mutations"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range mutations {
		var record MutationRecord
		if err := readJSON(filepath.Join(s.dir, "mutations", entry.Name()), &record); err != nil {
			return err
		}
		if record.AttemptID != attemptID {
			continue
		}
		step, err := s.findingMutationAuthorityProgression(record, entry.Name())
		if err != nil {
			return err
		}
		progressions[record.TargetGeneration] = append(progressions[record.TargetGeneration], step)
	}
	return nil
}

func findingAuthorityProgressionPreserved(record TransitionRecord, attemptID string) bool {
	return record.SourceAttemptID == attemptID && record.TargetAttemptID == attemptID &&
		record.SourceWorkspaceID != "" && record.SourceWorkspaceID == record.TargetWorkspaceID &&
		record.SourceRootTaskRef.Equal(record.TargetRootTaskRef) &&
		record.SourceExecutionTaskRef.Equal(record.TargetExecutionTaskRef) &&
		record.ProjectSnapshotOld == record.ProjectSnapshotNew &&
		record.WorkspaceSnapshotOld.ID == record.WorkspaceSnapshotNew.ID
}

func (s *Store) findingMutationAuthorityProgression(record MutationRecord, filename string) (findingAuthorityProgressionStep, error) {
	var step findingAuthorityProgressionStep
	if !findingMutationIdentityValid(record, filename) {
		return step, fmt.Errorf("finding authority mutation has invalid identity or generation")
	}
	source, err := s.loadLease(record.SourceLeaseID)
	if err != nil {
		return step, err
	}
	target, err := s.loadLease(record.TargetLeaseID)
	if err != nil {
		return step, err
	}
	if !findingMutationLeaseIdentitiesMatch(record, source, target) || !findingMutationLeaseSnapshotsMatch(record, source, target) {
		return step, fmt.Errorf("finding authority mutation does not match its lease or snapshot")
	}
	return findingAuthorityProgressionStep{sourceGeneration: record.SourceGeneration, targetWorkspaceSnapshot: record.After.ID,
		anchor: &findingAuthorityAnchor{task: source.SemanticTaskRef, workspace: record.Before.ID}}, nil
}

func findingProgressionStepMatchesAuthority(step findingAuthorityProgressionStep, before findingSourceAuthority, workspace string) bool {
	return step.anchor != nil && step.anchor.task.Equal(before.Task) && step.targetWorkspaceSnapshot == workspace &&
		(!step.fromTransition || step.anchor.project == before.Project)
}

func findingMutationIdentityValid(record MutationRecord, filename string) bool {
	return record.SchemaVersion == controllerSchemaVersion && record.MutationID+".json" == filename &&
		record.SourceGeneration != 0 && record.TargetGeneration > record.SourceGeneration && record.TargetGeneration-record.SourceGeneration == 1 &&
		record.Before.ID != "" && record.After.ID != "" && record.SourceLeaseID != record.TargetLeaseID
}

func findingMutationLeaseIdentitiesMatch(record MutationRecord, source, target ExecutionLease) bool {
	return source.AttemptID == record.AttemptID && target.AttemptID == record.AttemptID &&
		source.ControllerGeneration == record.SourceGeneration && target.ControllerGeneration == record.TargetGeneration &&
		source.SemanticTaskRef.Equal(target.SemanticTaskRef) && source.WorkspaceID != "" && source.WorkspaceID == target.WorkspaceID
}

func findingMutationLeaseSnapshotsMatch(record MutationRecord, source, target ExecutionLease) bool {
	return source.ExpectedWorkspaceSnapshotID == record.Before.ID && target.ExpectedWorkspaceSnapshotID == record.After.ID &&
		source.ExpectedBaseOID == record.Before.Head && target.ExpectedBaseOID == record.After.Head
}
