package controller

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

func (s *Store) failedFinalValidation(candidate AcceptedCandidate) (*FinalValidationRequest, error) {
	if len(candidate.RequiredValidations) == 0 {
		return nil, nil
	}
	source, err := s.finalValidationSource(candidate)
	if err != nil {
		return nil, err
	}
	module, err := s.finalValidationRunStore(candidate.TaskRef)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	for _, request := range candidate.RequiredValidations {
		root, err := s.finalValidationWorkspace(candidate, request)
		if err != nil {
			return nil, err
		}
		record, found, err := latestFinalValidationRun(module, root, request.Form)
		if err != nil {
			return nil, err
		}
		if found && record.Status == qualitygate.StatusFail && finalValidationMatchesSource(record, root, source, request) {
			return &request, nil
		}
	}
	return nil, nil
}

func (s *Store) projectSuspendedExecutionMaterialization(head RepositoryControllerHead) (ParentCompletionOperation, bool, error) {
	if head.LiveLeaseID != "" || head.PendingTransitionID != "" || head.AcceptedCandidateRef != nil || head.ExecutionTaskRef == nil || head.ActiveEpisodeID != "" {
		return ParentCompletionOperation{}, false, nil
	}
	snapshot, suspended, err := s.suspendedExecutionSnapshot(head, *head.ExecutionTaskRef)
	if err != nil || !suspended {
		return ParentCompletionOperation{}, false, err
	}
	command, err := s.suspensionResumeCommand(head, *snapshot)
	if err != nil {
		return ParentCompletionOperation{}, false, err
	}
	return ParentCompletionOperation{Surface: ExecutionOperationSurface, Operation: command.Action, Execution: command}, true, nil
}

func (s *Store) suspensionResumeCommand(head RepositoryControllerHead, snapshot SuspensionSnapshot) (*CompletionExecutionCommand, error) {
	sealRef, err := s.suspensionSealRef(snapshot)
	if err != nil {
		return nil, err
	}
	seal, err := s.LoadAttemptSeal(sealRef)
	if err != nil {
		return nil, err
	}
	lane, err := s.executionLaneRoot()
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(lane); err == nil {
		return s.suspensionCleanupCommand(head, sealRef, seal.WorkspaceID)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return &CompletionExecutionCommand{Action: "materialize", ExpectedGeneration: head.ControllerGeneration, SuspensionID: snapshot.SnapshotID}, nil
}

func (s *Store) suspensionCleanupCommand(head RepositoryControllerHead, sealRef EvidenceObjectRef, sealedWorkspace string) (*CompletionExecutionCommand, error) {
	lane, err := s.executionLaneRoot()
	if err != nil {
		return nil, err
	}
	workspace, err := ResolveWorkspaceIdentity(lane, s.identity)
	if err != nil {
		return nil, err
	}
	if workspace.ID != sealedWorkspace {
		return nil, fmt.Errorf("reentry lane is occupied outside the suspended execution seal")
	}
	return &CompletionExecutionCommand{Action: "cleanup", ExpectedGeneration: head.ControllerGeneration, WorkspaceID: workspace.ID, SealRef: &sealRef}, nil
}

func (s *Store) projectCompletionMaterialization(head RepositoryControllerHead, candidate AcceptedCandidate) (ParentCompletionOperation, error) {
	seal, err := s.LoadAttemptSeal(candidate.SealRef)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	lane, err := s.executionLaneRoot()
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	command := &CompletionExecutionCommand{Action: "materialize", ExpectedGeneration: head.ControllerGeneration, SuspensionID: seal.OperationalSnapshotID}
	if _, err := os.Lstat(lane); err == nil {
		workspace, err := ResolveWorkspaceIdentity(lane, s.identity)
		if err != nil {
			return ParentCompletionOperation{}, err
		}
		if workspace.ID != seal.WorkspaceID {
			return ParentCompletionOperation{}, fmt.Errorf("reentry lane is occupied outside the candidate seal")
		}
		command = &CompletionExecutionCommand{Action: "cleanup", ExpectedGeneration: head.ControllerGeneration, WorkspaceID: workspace.ID, SealRef: &candidate.SealRef}
	} else if !os.IsNotExist(err) {
		return ParentCompletionOperation{}, err
	}
	return ParentCompletionOperation{Surface: ExecutionOperationSurface, Operation: command.Action, Execution: command}, nil
}
