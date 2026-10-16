package controller

import (
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const executionParentSourceImport = "IMPORT_PARENT_SOURCE"

func (s *Store) ImportParentSource(admission Admission, sourceSnapshotID string, runtimeRef EvidenceObjectRef, task SemanticTaskRef) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, err := s.planParentSourceImport(admission, sourceSnapshotID, runtimeRef, task)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, admission.Head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planParentSourceImport(admission Admission, sourceSnapshotID string, runtimeRef EvidenceObjectRef, task SemanticTaskRef) (ExecutionOperation, error) {
	if err := s.validateTransitionSource(TransitionIntent{Source: admission, ExpectedGeneration: admission.Head.ControllerGeneration}); err != nil {
		return ExecutionOperation{}, err
	}
	if admission.Lease.InFlightCallID != "" {
		return ExecutionOperation{}, fmt.Errorf("parent source import requires an idle live lease")
	}
	source, authority, err := s.captureParentSourceRevision(sourceSnapshotID, admission.Head.IntegrationTip)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if !authority.Task.Equal(task) || task.TaskPath != admission.Attempt.SemanticTaskRef.TaskPath {
		return ExecutionOperation{}, fmt.Errorf("live parent source import changes Task authority")
	}
	base, err := s.projectMetadataBase(admission.Head.ProjectSnapshotID)
	if err != nil {
		return ExecutionOperation{}, err
	}
	current, err := CaptureExecutionTrees(admission.Workspace.Root, base)
	if err != nil {
		return ExecutionOperation{}, err
	}
	tree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", source.AuthorityOID+"^{tree}")
	if err != nil {
		return ExecutionOperation{}, err
	}
	rebound := ReboundSuspension{BaseOID: admission.Snapshot.Head, Baseline: admission.Attempt.BaselineTrees, Current: current}
	rebound.Current.WorktreeTree = tree
	if err := normalizeReboundTrees(s.identity.PrimaryRoot, authority.Snapshot.HeadOID, &rebound); err != nil {
		return ExecutionOperation{}, err
	}
	if rebound.Current.WorktreeTree != tree {
		return ExecutionOperation{}, fmt.Errorf("live parent source import differs from its Project metadata")
	}
	if err := s.writeProjectSnapshot(authority.Snapshot); err != nil {
		return ExecutionOperation{}, err
	}
	return s.buildParentSourceImport(admission, source, rebound, runtimeRef, task)
}

func (s *Store) buildParentSourceImport(admission Admission, source ParentSourceRevision, rebound ReboundSuspension, runtimeRef EvidenceObjectRef, task SemanticTaskRef) (ExecutionOperation, error) {
	if _, err := s.LoadEvidenceObject(runtimeRef); err != nil {
		return ExecutionOperation{}, err
	}
	lease := admission.Lease
	lease.SemanticTaskRef = task
	attempt := admission.Attempt
	attempt.SemanticTaskRef = task
	attempt.RootTaskRef = task
	var err error
	lease.LeaseID, err = state.NewUUID()
	if err != nil {
		return ExecutionOperation{}, err
	}
	lease.ControllerGeneration = admission.Head.ControllerGeneration + 3
	lease.ExpectedWorkspaceSnapshotID = ""
	lease.InFlightCallID = ""
	target := authorityFromAdmission(admission, WorkspaceSnapshot{})
	target.RootTaskRef = task
	target.ExecutionTaskRef = task
	target.LeaseID = lease.LeaseID
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, source.AuthorityOID)
	if err != nil {
		return ExecutionOperation{}, err
	}
	target.ProjectSnapshotID = authority.ProjectSnapshotID
	id, err := state.NewUUID()
	if err != nil {
		return ExecutionOperation{}, err
	}
	record := buildTransitionRecord(TransitionIntent{Kind: executionParentSourceImport, Source: admission, Target: target}, id)
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceSource, Resource: admission.Workspace.Root, ExpectedOld: admission.Snapshot.ID, ExpectedNew: rebound.Current.WorktreeTree}}
	op := ExecutionOperation{Transition: record, Source: admission, ParentSource: &source, RuntimeBeforeRef: &runtimeRef, Workspace: &admission.Workspace, Attempt: &attempt, Lease: &lease, Rebound: &rebound}
	if !task.Equal(admission.Attempt.SemanticTaskRef) {
		op.TaskAmendment = &TaskAmendmentApproval{OldRootTaskRef: admission.Attempt.RootTaskRef, OldExecutionTaskRef: admission.Attempt.SemanticTaskRef, NewRootTaskRef: task, NewExecutionTaskRef: task}
	}
	op.Evidence, err = s.parentSourceImportEvidence(op)
	return op, err
}

func (s *Store) validateParentSourceImport(op ExecutionOperation) error {
	if op.ParentSource == nil || op.RuntimeBeforeRef == nil || op.Workspace == nil || op.Attempt == nil || op.Lease == nil || op.Rebound == nil {
		return fmt.Errorf("parent source import payload is incomplete")
	}
	if op.Attempt.AttemptID != op.Source.Attempt.AttemptID || op.Workspace.ID != op.Source.Workspace.ID {
		return fmt.Errorf("parent source import changes execution authority")
	}
	if err := s.validateParentSourceImportProject(op); err != nil {
		return err
	}
	if err := s.validateParentSourceImportRetention(op); err != nil {
		return err
	}
	return s.validateParentSourceArchive(*op.ParentSource, op.Source.Head.IntegrationTip, op.Rebound.Current.WorktreeTree)
}

func (s *Store) validateParentSourceImportRetention(op ExecutionOperation) error {
	if _, err := s.LoadEvidenceObject(*op.RuntimeBeforeRef); err != nil {
		return err
	}
	if op.TaskAmendment != nil {
		if !parentSourceRuntimeIdentity(op, "reentry-review-repair") {
			return fmt.Errorf("parent source import runtime producer identity differs")
		}
	}
	return nil
}

func (s *Store) applyParentSourceImport(op ExecutionOperation) error {
	if err := s.verifyParentSourceImportProtected(op); err != nil {
		return err
	}
	if err := s.applyParentSourceFiles(op); err != nil {
		return err
	}
	if err := s.verifyParentSourceImportProtected(op); err != nil {
		return err
	}
	base, err := s.projectMetadataBase(op.Transition.ProjectSnapshotNew)
	if err != nil {
		return err
	}
	trees, err := CaptureExecutionTrees(op.Workspace.Root, base)
	if err != nil {
		return err
	}
	if trees != op.Rebound.Current {
		return fmt.Errorf("parent source import output differs from its archived tree")
	}
	snapshot, err := CaptureWorkspaceSnapshot(op.Workspace.Root)
	if err != nil {
		return err
	}
	lease := *op.Lease
	lease.ExpectedWorkspaceSnapshotID = snapshot.ID
	if err := s.writeLease(lease); err != nil {
		return err
	}
	if err := s.writeAttempt(*op.Attempt); err != nil {
		return err
	}
	actual := map[string]string{op.Transition.Effects[0].Key(): trees.WorktreeTree}
	_, _, err = s.commitAuthorityTransitionWithEvidenceLocked(op.Transition, actual, op.Evidence, func(head *RepositoryControllerHead) error {
		head.LiveLeaseID = lease.LeaseID
		head.ProjectSnapshotID = op.Transition.ProjectSnapshotNew
		if op.TaskAmendment != nil {
			head.RootTaskRef = &op.TaskAmendment.NewRootTaskRef
			head.ExecutionTaskRef = &op.TaskAmendment.NewExecutionTaskRef
		}
		return nil
	})
	return err
}

func (s *Store) validateParentSourceImportProject(op ExecutionOperation) error {
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, op.ParentSource.AuthorityOID)
	if err != nil {
		return err
	}
	if authority.ProjectSnapshotID != op.Transition.ProjectSnapshotNew {
		return fmt.Errorf("parent source import changes active Task authority")
	}
	if err := validateParentSourceTaskBinding(op, authority.Task); err != nil {
		return err
	}
	rebound := ReboundSuspension{Current: op.Source.Attempt.BaselineTrees}
	if err := normalizeReboundTrees(s.identity.PrimaryRoot, authority.Snapshot.HeadOID, &rebound); err != nil {
		return err
	}
	if rebound.Current.IndexTree != op.Rebound.Current.IndexTree {
		return fmt.Errorf("parent source import changes protected index")
	}
	return nil
}

func (s *Store) verifyParentSourceImportProtected(op ExecutionOperation) error {
	workspace, err := ResolveWorkspaceIdentity(op.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	if workspace != *op.Workspace {
		return fmt.Errorf("parent source import workspace was replaced")
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return err
	}
	before := op.Source.Snapshot
	if snapshot.Head != before.Head || snapshot.IndexDigest != before.IndexDigest || snapshot.RefDigest != before.RefDigest {
		return fmt.Errorf("parent source import changed protected Git authority")
	}
	return nil
}

func (s *Store) verifyCommittedParentSourceImport(op ExecutionOperation, head RepositoryControllerHead) error {
	if head.LiveAttemptID != op.Source.Attempt.AttemptID || head.LiveLeaseID != op.Lease.LeaseID || head.ProjectSnapshotID != op.Transition.ProjectSnapshotNew || head.RootTaskRef == nil || head.ExecutionTaskRef == nil || !head.RootTaskRef.Equal(op.Attempt.RootTaskRef) || !head.ExecutionTaskRef.Equal(op.Attempt.SemanticTaskRef) {
		return fmt.Errorf("parent source import committed a different execution")
	}
	return s.verifyCommittedMaterialization(op, head)
}

func validateParentSourceTaskBinding(op ExecutionOperation, task SemanticTaskRef) error {
	if op.TaskAmendment == nil {
		if !task.Equal(op.Source.Attempt.SemanticTaskRef) {
			return fmt.Errorf("parent source import changes active Task authority")
		}
		return nil
	}
	a := op.TaskAmendment
	if !a.OldRootTaskRef.Equal(op.Source.Attempt.RootTaskRef) || !a.OldExecutionTaskRef.Equal(op.Source.Attempt.SemanticTaskRef) || !a.NewRootTaskRef.Equal(task) || !a.NewExecutionTaskRef.Equal(task) || task.TaskPath != op.Source.Attempt.SemanticTaskRef.TaskPath || !op.Attempt.SemanticTaskRef.Equal(task) || !op.Attempt.RootTaskRef.Equal(task) || !op.Lease.SemanticTaskRef.Equal(task) {
		return fmt.Errorf("parent source Task amendment differs")
	}
	return nil
}
