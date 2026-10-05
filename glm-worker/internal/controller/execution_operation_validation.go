package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func (s *Store) validateExecutionOperation(op ExecutionOperation) (executionOperationContract, error) {
	digest, err := executionOperationDigest(op)
	if err != nil {
		return executionOperationContract{}, err
	}
	if op.Transition.OperationDigest == "" || op.Transition.OperationDigest != digest {
		return executionOperationContract{}, fmt.Errorf("execution operation content integrity failed")
	}
	contract, err := s.executionOperationContract(op.Transition.Kind)
	if err != nil {
		return executionOperationContract{}, err
	}
	if err := validateExecutionOperationEffects(op, contract.effects); err != nil {
		return executionOperationContract{}, err
	}
	if err := contract.validate(op); err != nil {
		return executionOperationContract{}, err
	}
	return contract, nil
}

func executionOperationDigest(op ExecutionOperation) (string, error) {
	op.Transition.OperationDigest = ""
	data, err := json.Marshal(op)
	return digestStrings("controller-execution-operation-v1", string(data)), err
}

func (s *Store) validateExecutionLaneLocation(workspace WorkspaceIdentity) error {
	base, err := canonicalPath(s.dir)
	if err != nil {
		return err
	}
	name := s.identity.LineageID[:16] + "-lane"
	if workspace.RepositoryID != s.identity.LineageID || workspace.Root != filepath.Join(base, name) || workspace.GitDir != filepath.Join(s.identity.CommonDir, "worktrees", name) {
		return fmt.Errorf("execution lane is outside controller ownership")
	}
	return nil
}

func readExecutionRef(repo, ref string) (string, bool, error) {
	data, err := runGitBinary(repo, nil, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(data)), true, nil
}

func (s *Store) validateSuspensionOperation(op ExecutionOperation) error {
	if op.Suspension == nil || op.Episode == nil || op.SealRef == nil {
		return fmt.Errorf("suspension operation is incomplete")
	}
	workspace, err := ResolveWorkspaceIdentity(op.Source.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	if workspace != op.Source.Workspace || workspace.ID != op.Transition.SourceWorkspaceID {
		return fmt.Errorf("suspension source workspace is inconsistent")
	}
	if op.Transition.Effects[0].Resource != suspensionRef(op.Suspension.SnapshotID) || op.Transition.Effects[0].ExpectedNew != op.Suspension.RetainedCommitOID {
		return fmt.Errorf("suspension retention intent is inconsistent")
	}
	return nil
}

func (s *Store) validateMaterializationOperation(op ExecutionOperation) error {
	if op.Workspace == nil || op.Rebound == nil || op.Attempt == nil || op.Lease == nil {
		return fmt.Errorf("materialization operation is incomplete")
	}
	if err := s.validateExecutionLaneLocation(*op.Workspace); err != nil {
		return err
	}
	if op.Transition.Effects[0].ExpectedNew != laneMaterializationIdentity(*op.Workspace, *op.Rebound) || op.Transition.TargetWorkspaceID != op.Workspace.ID || op.Transition.TargetAttemptID != op.Attempt.AttemptID || op.Transition.TargetLeaseID != op.Lease.LeaseID {
		return fmt.Errorf("materialization plan differs from journal authority")
	}
	return nil
}

func (s *Store) validateCleanupOperation(op ExecutionOperation) error {
	if op.Workspace == nil || op.CleanupSnapshot == nil || op.SealRef == nil {
		return fmt.Errorf("cleanup operation is incomplete")
	}
	if err := s.validateExecutionLaneLocation(*op.Workspace); err != nil {
		return err
	}
	if op.Transition.Effects[0].Resource != op.Workspace.Root || op.Transition.Effects[0].ExpectedOld != op.CleanupSnapshot.ID {
		return fmt.Errorf("cleanup intent is inconsistent")
	}
	return nil
}

func validateSuspensionGCOperation(op ExecutionOperation) error {
	if op.Suspension == nil || op.SealRef == nil || op.Transition.Effects[0].Resource != suspensionRef(op.Suspension.SnapshotID) || op.Transition.Effects[0].ExpectedOld != op.Suspension.RetainedCommitOID {
		return fmt.Errorf("suspension GC authority is inconsistent")
	}
	return nil
}

func (s *Store) verifyCommittedMaterialization(op ExecutionOperation, head RepositoryControllerHead) error {
	if head.LiveLeaseID != op.Lease.LeaseID || head.LiveAttemptID != op.Attempt.AttemptID {
		return fmt.Errorf("committed execution bindings differ from operation")
	}
	workspace, err := ResolveWorkspaceIdentity(op.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	if workspace != *op.Workspace {
		return fmt.Errorf("committed execution workspace was replaced")
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return err
	}
	if snapshot.ID != lease.ExpectedWorkspaceSnapshotID || snapshot.Head != lease.ExpectedBaseOID {
		return fmt.Errorf("committed execution workspace changed unexpectedly")
	}
	return nil
}

func (s *Store) verifyCommittedSuspension(op ExecutionOperation, head RepositoryControllerHead) error {
	actual, err := CaptureWorkspaceSnapshot(op.Source.Workspace.Root)
	if err != nil {
		return err
	}
	if actual != op.Transition.WorkspaceSnapshotNew || head.LiveLeaseID != "" {
		return fmt.Errorf("committed suspension state is unexpected")
	}
	_, err = s.ProveCleanupDurability(*op.SealRef)
	return err
}
