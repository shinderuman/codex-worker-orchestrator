package controller

import (
	"fmt"
	"os"
)

func (s *Store) cleanupRetiredLaneBeforeStart(input MaterializeExecutionInput) (MaterializeExecutionInput, error) {
	head, err := s.LoadHead()
	if err != nil {
		return input, err
	}
	if input.EpisodeID != "" || input.SuspensionID != "" || !retiredMetadataHead(head) {
		return input, nil
	}
	if head.ControllerGeneration != input.ExpectedGeneration {
		return input, fmt.Errorf("ordinary start controller generation is stale")
	}
	if _, _, _, err := s.nextRootMaterializationAuthority(head); err != nil {
		return input, err
	}
	generation, err := s.cleanupOccupiedRetiredLane(head, input.ExpectedGeneration)
	if err != nil {
		return input, err
	}
	input.ExpectedGeneration = generation
	return input, nil
}

func (s *Store) cleanupOccupiedRetiredLane(head RepositoryControllerHead, expectedGeneration uint64) (uint64, error) {
	root, err := s.executionLaneRoot()
	if err != nil {
		return 0, err
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return head.ControllerGeneration, nil
	} else if err != nil {
		return 0, err
	}
	workspace, err := ResolveWorkspaceIdentity(root, s.identity)
	if err != nil {
		return 0, err
	}
	sealRef, err := s.retiredLaneSeal(head, workspace)
	if err != nil {
		return 0, err
	}
	op, source, err := s.planExecutionCleanup(CleanupExecutionInput{ExpectedGeneration: expectedGeneration, WorkspaceID: workspace.ID, SealRef: sealRef})
	if err != nil {
		return 0, err
	}
	if err := s.prepareExecutionOperation(&op, source); err != nil {
		return 0, err
	}
	cleaned, err := s.recoverExecutionOperationLocked(op)
	if err != nil {
		return 0, err
	}
	return cleaned.Head.ControllerGeneration, nil
}

func (s *Store) retiredLaneSeal(head RepositoryControllerHead, workspace WorkspaceIdentity) (EvidenceObjectRef, error) {
	if err := s.validateExecutionLaneLocation(workspace); err != nil {
		return EvidenceObjectRef{}, err
	}
	record, err := s.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	candidate, err := s.LoadAcceptedCandidate(record.CandidateRef)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	ref := candidate.SealRef
	visited := map[string]bool{}
	for {
		matched, next, err := s.inspectRetiredLaneSeal(head, workspace, candidate, ref, visited)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		if matched != nil {
			return *matched, nil
		}
		ref = next
	}
}

func (s *Store) inspectRetiredLaneSeal(head RepositoryControllerHead, workspace WorkspaceIdentity, candidate AcceptedCandidate, ref EvidenceObjectRef, visited map[string]bool) (*EvidenceObjectRef, EvidenceObjectRef, error) {
	seal, attempt, err := s.loadRetiredLaneAttempt(candidate, ref, visited)
	if err != nil {
		return nil, EvidenceObjectRef{}, err
	}
	if seal.WorkspaceID == workspace.ID {
		return &ref, EvidenceObjectRef{}, nil
	}
	if seal.PredecessorAttemptID == "" {
		return nil, EvidenceObjectRef{}, fmt.Errorf("occupied lane is not owned by retired attempt lineage")
	}
	next, err := s.retiredLanePredecessorSeal(head, attempt)
	if err != nil {
		return nil, EvidenceObjectRef{}, err
	}
	return nil, next, nil
}

func (s *Store) loadRetiredLaneAttempt(candidate AcceptedCandidate, ref EvidenceObjectRef, visited map[string]bool) (AttemptSeal, AttemptRecord, error) {
	seal, err := s.LoadAttemptSeal(ref)
	if err != nil {
		return AttemptSeal{}, AttemptRecord{}, err
	}
	if visited[seal.AttemptID] || seal.SemanticTaskRef.TaskPath != candidate.TaskRef.TaskPath {
		return AttemptSeal{}, AttemptRecord{}, fmt.Errorf("retired lane attempt lineage is inconsistent")
	}
	visited[seal.AttemptID] = true
	attempt, err := s.loadAttempt(seal.AttemptID)
	if err != nil {
		return AttemptSeal{}, AttemptRecord{}, err
	}
	if attempt.AttemptSealID != seal.AttemptSealID || attempt.PredecessorAttemptID != seal.PredecessorAttemptID || !sealedExecutionAttemptState(attempt.AttemptState) {
		return AttemptSeal{}, AttemptRecord{}, fmt.Errorf("retired lane attempt seal binding is inconsistent")
	}
	return seal, attempt, nil
}

func (s *Store) retiredLanePredecessorSeal(head RepositoryControllerHead, attempt AttemptRecord) (EvidenceObjectRef, error) {
	predecessor, err := s.loadAttempt(attempt.PredecessorAttemptID)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	next, _, err := s.findAttemptSeal(head, predecessor)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	if attempt.ResumedFromSealID != next.LogicalIdentity || predecessor.StartControllerGeneration >= attempt.StartControllerGeneration {
		return EvidenceObjectRef{}, fmt.Errorf("retired lane successor binding is inconsistent")
	}
	return next, nil
}
