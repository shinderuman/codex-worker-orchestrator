package controller

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) applyMaterializationCandidateSource(head RepositoryControllerHead, seal EvidenceObjectRef, rebound *ReboundSuspension) error {
	if head.AcceptedCandidateRef != nil {
		candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
		if err != nil {
			return err
		}
		if !evidenceRefsEqual(candidate.SealRef, seal) {
			return fmt.Errorf("materialization candidate differs from suspension seal")
		}
		rebound.Current.WorktreeTree = candidate.TreeOID
	}
	return s.normalizeMaterializationMetadataBase(head, rebound)
}

func (s *Store) normalizeMaterializationMetadataBase(head RepositoryControllerHead, rebound *ReboundSuspension) error {
	metadataBase, err := s.projectMetadataBase(head.ProjectSnapshotID)
	if err != nil {
		return err
	}
	return normalizeReboundTrees(s.identity.PrimaryRoot, metadataBase, rebound)
}

func (s *Store) RuntimeMaterializationReentryAuthority(transitionID string) (state.ControllerReentryAuthority, error) {
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(transitionID), &op); err != nil {
		return state.ControllerReentryAuthority{}, err
	}
	if err := validateRuntimeReentryMaterialization(op); err != nil {
		return state.ControllerReentryAuthority{}, err
	}
	head, err := s.LoadHead()
	if err != nil {
		return state.ControllerReentryAuthority{}, err
	}
	if _, err := s.validateExecutionOperation(op); err != nil {
		return state.ControllerReentryAuthority{}, err
	}
	if err := s.verifyCommittedMaterialization(op, head); err != nil {
		return state.ControllerReentryAuthority{}, err
	}
	seal, err := s.LoadAttemptSeal(*op.SealRef)
	if err != nil {
		return state.ControllerReentryAuthority{}, err
	}
	if seal.AttemptID != op.Attempt.PredecessorAttemptID || !seal.SemanticTaskRef.Equal(op.Suspension.SemanticTaskRef) || seal.SemanticTaskRef.TaskPath != op.Attempt.SemanticTaskRef.TaskPath {
		return state.ControllerReentryAuthority{}, fmt.Errorf("runtime reentry predecessor authority differs")
	}
	return state.ControllerReentryAuthority{PredecessorAttemptID: seal.AttemptID, AttemptID: op.Attempt.AttemptID, TaskPath: op.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: op.Attempt.SemanticTaskRef.ContractDigest, PredecessorTaskContractDigest: seal.SemanticTaskRef.ContractDigest}, nil
}

func validateRuntimeReentryMaterialization(op ExecutionOperation) error {
	if op.Transition.Kind != executionMaterialize || op.Attempt == nil || op.Suspension == nil || op.SealRef == nil {
		return fmt.Errorf("runtime reentry requires a materialized sealed predecessor")
	}
	return nil
}

func (s *Store) materializationParentSource(head RepositoryControllerHead) (*ParentSourceRevision, error) {
	if head.AcceptedCandidateRef == nil {
		return nil, nil
	}
	current, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for ref := head.AcceptedCandidateRef; ref != nil; {
		source, next, err := s.lineageParentSource(*ref, current, seen)
		if err != nil || source != nil {
			return source, err
		}
		ref = next
	}
	return nil, nil
}

func (s *Store) lineageParentSource(ref EvidenceObjectRef, current AcceptedCandidate, seen map[string]bool) (*ParentSourceRevision, *EvidenceObjectRef, error) {
	if seen[ref.Digest] {
		return nil, nil, fmt.Errorf("materialization candidate lineage cycles")
	}
	seen[ref.Digest] = true
	candidate, err := s.LoadAcceptedCandidate(ref)
	if err != nil {
		return nil, nil, err
	}
	if candidate.BaseOID != current.BaseOID || candidate.TreeOID != current.TreeOID {
		return nil, nil, nil
	}
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(candidate.TransitionID), &op); err != nil {
		return nil, nil, err
	}
	if op.Publication == nil {
		return nil, nil, fmt.Errorf("materialization candidate publication is missing")
	}
	return op.Publication.ParentSource, candidate.Previous, nil
}

func (s *Store) validateMaterializationParentSource(op ExecutionOperation) error {
	if op.ParentSource == nil {
		return nil
	}
	if op.Rebound == nil {
		return fmt.Errorf("parent source materialization lacks execution trees")
	}
	project, err := s.LoadProjectSnapshot(op.Transition.ProjectSnapshotNew)
	if err != nil {
		return err
	}
	if project.HeadOID != op.ParentSource.AuthorityOID {
		return fmt.Errorf("parent source materialization Project differs")
	}
	return s.validateParentSourceArchive(*op.ParentSource, op.Rebound.BaseOID, op.Rebound.Current.WorktreeTree)
}
