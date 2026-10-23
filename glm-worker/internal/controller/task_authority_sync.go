package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type TaskAuthoritySyncPlan struct {
	SourceSnapshotID string          `json:"source_snapshot_id"`
	PreviousTaskRef  SemanticTaskRef `json:"previous_task_ref"`
	RevisedTaskRef   SemanticTaskRef `json:"revised_task_ref"`
}

type TaskAuthoritySyncRequiredError struct {
	Plan       TaskAuthoritySyncPlan
	Admittable bool
	Reason     string
}

const (
	executionTaskAuthoritySync = "SYNC_TASK_AUTHORITY"
	TaskAuthoritySyncProducer  = "sync-task-authority"
)

func (s *Store) SyncTaskAuthority(admission Admission, sourceSnapshotID string, runtimeRef EvidenceObjectRef, task SemanticTaskRef) (ExecutionOperationResult, error) {
	return s.executeParentSourceRevision(admission, func() (ExecutionOperation, error) {
		return s.planTaskAuthoritySync(admission, sourceSnapshotID, runtimeRef, task)
	})
}

func (s *Store) planTaskAuthoritySync(admission Admission, sourceSnapshotID string, runtimeRef EvidenceObjectRef, task SemanticTaskRef) (ExecutionOperation, error) {
	if err := s.validateTaskAuthoritySyncSource(admission, task); err != nil {
		return ExecutionOperation{}, err
	}
	source, authority, err := s.captureTaskSourceRevision(sourceSnapshotID, admission.Head.IntegrationTip, task.TaskPath)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if !authority.Task.Equal(task) {
		return ExecutionOperation{}, fmt.Errorf("task authority sync changes Task authority")
	}
	base, err := s.projectMetadataBase(admission.Head.ProjectSnapshotID)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if err := verifyTaskOnlyAuthorityDelta(s.identity.PrimaryRoot, base, authority.Snapshot.HeadOID, task.TaskPath); err != nil {
		return ExecutionOperation{}, err
	}
	current, err := CaptureExecutionTrees(admission.Workspace.Root, base)
	if err != nil {
		return ExecutionOperation{}, err
	}
	rebound := ReboundSuspension{Baseline: admission.Attempt.BaselineTrees, Current: current}
	if err := normalizeReboundTrees(s.identity.PrimaryRoot, authority.Snapshot.HeadOID, &rebound); err != nil {
		return ExecutionOperation{}, err
	}
	if err := verifyTaskOnlyTreeDelta(s.identity.PrimaryRoot, current.WorktreeTree, rebound.Current.WorktreeTree, task.TaskPath); err != nil {
		return ExecutionOperation{}, err
	}
	if err := s.writeProjectSnapshot(authority.Snapshot); err != nil {
		return ExecutionOperation{}, err
	}
	return s.buildTaskAuthoritySync(admission, source, authority, rebound, runtimeRef, task)
}

func (s *Store) buildTaskAuthoritySync(admission Admission, source ParentSourceRevision, authority CommittedTaskAuthority, rebound ReboundSuspension, runtimeRef EvidenceObjectRef, task SemanticTaskRef) (ExecutionOperation, error) {
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
	target.ProjectSnapshotID = authority.ProjectSnapshotID
	id, err := state.NewUUID()
	if err != nil {
		return ExecutionOperation{}, err
	}
	record := buildTransitionRecord(TransitionIntent{Kind: executionTaskAuthoritySync, Source: admission, Target: target}, id)
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceSource, Resource: admission.Workspace.Root, ExpectedOld: admission.Snapshot.ID, ExpectedNew: rebound.Current.WorktreeTree}}
	op := ExecutionOperation{
		Transition: record, Source: admission, ParentSource: &source, RuntimeBeforeRef: &runtimeRef,
		Workspace: &admission.Workspace, Attempt: &attempt, Lease: &lease, Rebound: &rebound,
		TaskAmendment: &TaskAmendmentApproval{
			OldRootTaskRef: admission.Attempt.RootTaskRef, OldExecutionTaskRef: admission.Attempt.SemanticTaskRef,
			NewRootTaskRef: task, NewExecutionTaskRef: task,
		},
	}
	op.Evidence, err = s.parentSourceImportEvidence(op)
	return op, err
}

func (s *Store) validateTaskAuthoritySync(op ExecutionOperation) error {
	if !taskAuthoritySyncPayloadComplete(op) {
		return fmt.Errorf("task authority sync payload is incomplete")
	}
	if op.Attempt.AttemptID != op.Source.Attempt.AttemptID || op.Workspace.ID != op.Source.Workspace.ID {
		return fmt.Errorf("task authority sync changes execution authority")
	}
	if !parentSourceRuntimeIdentity(op, TaskAuthoritySyncProducer) {
		return fmt.Errorf("task authority sync runtime producer identity differs")
	}
	if err := s.validateParentSourceImportProject(op); err != nil {
		return err
	}
	tree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", op.ParentSource.AuthorityOID+"^{tree}")
	if err != nil {
		return err
	}
	return s.validateParentSourceArchive(*op.ParentSource, op.Source.Head.IntegrationTip, tree)
}

func (e *TaskAuthoritySyncRequiredError) Error() string {
	if e.Admittable {
		return fmt.Sprintf(
			"model call blocked until the revised Task contract is ingested: %s; run the handoff-guided controller-execution sync-task-authority (source_snapshot_id=%s previous_task_contract_sha256=%s revised_task_contract_sha256=%s)",
			e.Reason, e.Plan.SourceSnapshotID, e.Plan.PreviousTaskRef.ContractDigest, e.Plan.RevisedTaskRef.ContractDigest)
	}
	return fmt.Sprintf("model call blocked: the active Task contract is stale but the regular sync owner cannot admit it: %s", e.Reason)
}

func (s *Store) RequiredTaskAuthoritySync(head RepositoryControllerHead) (*TaskAuthoritySyncRequiredError, error) {
	if head.RootTaskRef == nil || head.ExecutionTaskRef == nil {
		return nil, nil
	}
	sealed, err := s.candidateOwnsTaskAuthority(head)
	if err != nil || sealed {
		return nil, err
	}
	repo := s.identity.PrimaryRoot
	taskPath := head.ExecutionTaskRef.TaskPath
	digest, regular, err := worktreeRegularFileDigest(repo, taskPath)
	if err != nil {
		return nil, err
	}
	if !regular {
		return &TaskAuthoritySyncRequiredError{Reason: fmt.Sprintf("active Task file %q is missing or not a regular file in the primary worktree", taskPath)}, nil
	}
	if digest == head.ExecutionTaskRef.ContractDigest {
		return nil, nil
	}
	reason := s.taskAuthoritySyncBlockReason(head, taskPath)
	if reason != "" {
		return &TaskAuthoritySyncRequiredError{Reason: reason, Plan: TaskAuthoritySyncPlan{
			PreviousTaskRef: *head.ExecutionTaskRef,
			RevisedTaskRef:  SemanticTaskRef{TaskPath: taskPath, ContractDigest: digest},
		}}, nil
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		return nil, err
	}
	return &TaskAuthoritySyncRequiredError{
		Admittable: true,
		Reason:     fmt.Sprintf("primary worktree revises %s", taskPath),
		Plan: TaskAuthoritySyncPlan{
			SourceSnapshotID: snapshot.ID,
			PreviousTaskRef:  *head.ExecutionTaskRef,
			RevisedTaskRef:   SemanticTaskRef{TaskPath: taskPath, ContractDigest: digest},
		},
	}, nil
}

func (s *Store) candidateOwnsTaskAuthority(head RepositoryControllerHead) (bool, error) {
	if head.LiveLeaseID != "" || head.AcceptedCandidateRef == nil {
		return false, nil
	}
	candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return false, err
	}
	if !candidate.TaskRef.Equal(*head.ExecutionTaskRef) || !candidate.RootTaskRef.Equal(*head.RootTaskRef) || candidate.ProjectSnapshotID != head.ProjectSnapshotID {
		return false, fmt.Errorf("sealed candidate Task authority differs from controller")
	}
	for _, file := range candidate.TerminalFiles {
		if file.Path == candidate.TaskRef.TaskPath && file.NewOID == "" && digestBytes(file.OldBytes) == candidate.TaskRef.ContractDigest {
			return true, nil
		}
	}
	return false, fmt.Errorf("sealed candidate lacks source-bound Task retirement metadata")
}

func (s *Store) taskAuthoritySyncBlockReason(head RepositoryControllerHead, taskPath string) string {
	if reason := s.taskAuthoritySyncExecutionReason(head); reason != "" {
		return reason
	}
	repo := s.identity.PrimaryRoot
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		return fmt.Sprintf("committed primary authority is unreadable: %v", err)
	}
	if authority.Snapshot.HeadOID != head.IntegrationTip {
		return "primary HEAD moved beyond the controller integration tip; run the regular adopt or source sync first"
	}
	changed, err := primaryWorktreeChanges(repo)
	if err != nil {
		return fmt.Sprintf("primary worktree status is unreadable: %v", err)
	}
	if len(changed) != 1 || !changed[taskPath] {
		paths := make([]string, 0, len(changed))
		for path := range changed {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		return fmt.Sprintf("parent worktree deviation is not limited to the active Task file %q: %s", taskPath, strings.Join(paths, ", "))
	}
	return ""
}

func (s *Store) TaskAuthoritySyncEvidence(transitionID string) ([]byte, error) {
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(transitionID), &op); err != nil {
		return nil, err
	}
	if op.Transition.Kind != executionTaskAuthoritySync || !parentSourceRuntimeIdentity(op, TaskAuthoritySyncProducer) {
		return nil, nil
	}
	head, err := s.LoadHead()
	if err != nil {
		return nil, err
	}
	if _, err := s.validateExecutionOperation(op); err != nil {
		return nil, err
	}
	if !executionAlreadyFinalized(op, head) {
		return nil, fmt.Errorf("task authority sync is not finalized")
	}
	if err := s.verifyCommittedParentSourceImport(op, head); err != nil {
		return nil, err
	}
	return s.LoadEvidenceObject(*op.RuntimeBeforeRef)
}

func (s *Store) captureTaskSourceRevision(snapshotID, remote, taskPath string) (ParentSourceRevision, CommittedTaskAuthority, error) {
	repo := s.identity.PrimaryRoot
	snapshot, err := captureTaskSourceSnapshot(repo, snapshotID, remote, taskPath)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	tree, err := captureTaskSourceTree(repo, remote, taskPath)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	commit, err := suspensionTreeCommit(repo, tree, remote)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	authority, err := ResolveCommittedTaskAuthorityAt(repo, commit)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	ref, _, err := s.CaptureGitObjectArchive(repo, "task-source:"+snapshot.ID, []string{commit})
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	after, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	if after != snapshot {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, fmt.Errorf("parent source changed during capture")
	}
	return ParentSourceRevision{Snapshot: snapshot, AuthorityOID: commit, Archive: ref}, authority, nil
}

func primaryWorktreeChanges(repo string) (map[string]bool, error) {
	data, err := runGitBinary(repo, nil, "diff", "HEAD", "--name-only", "-z", "--")
	if err != nil {
		return nil, err
	}
	changes := map[string]bool{}
	for _, path := range strings.Split(string(data), "\x00") {
		if path != "" {
			changes[path] = true
		}
	}
	other, err := runGitBinary(repo, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, path := range strings.Split(string(other), "\x00") {
		if path != "" {
			changes[path] = true
		}
	}
	return changes, nil
}

func worktreeRegularFileDigest(repo, taskPath string) (string, bool, error) {
	path := filepath.Join(repo, filepath.FromSlash(taskPath))
	if err := validateSuspensionPath(taskPath); err != nil {
		return "", false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	return digestBytes(content), true, nil
}

func verifyTaskOnlyAuthorityDelta(repo, base, authority string, taskPath string) error {
	before, err := parentManagedTreeEntries(repo, base)
	if err != nil {
		return err
	}
	after, err := parentManagedTreeEntries(repo, authority)
	if err != nil {
		return err
	}
	changed := map[string]bool{}
	for path, entry := range before {
		other, ok := after[path]
		if !ok || other != entry {
			changed[path] = true
		}
	}
	for path, entry := range after {
		other, ok := before[path]
		if !ok || other != entry {
			changed[path] = true
		}
	}
	if len(changed) != 1 || !changed[taskPath] {
		return fmt.Errorf("task authority revision is not limited to the active Task file %q", taskPath)
	}
	return nil
}

func parentManagedTreeEntries(repo, revision string) (map[string]string, error) {
	args := append([]string{"ls-tree", "-r", "-z", revision, "--"}, state.ParentManagedRootPaths()...)
	data, err := runGitBinary(repo, nil, args...)
	if err != nil {
		return nil, err
	}
	entries, err := parseWorkspaceEntries(data, false)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, entry := range entries {
		result[entry.path] = entry.mode + " " + entry.oid
	}
	return result, nil
}

func verifyTaskOnlyTreeDelta(repo, before, after string, taskPath string) error {
	data, err := runGitBinary(repo, nil, "diff-tree", "-r", "--name-only", "-z", "--no-renames", before, after)
	if err != nil {
		return err
	}
	changed := map[string]bool{}
	for _, path := range strings.Split(string(data), "\x00") {
		if path != "" {
			changed[path] = true
		}
	}
	if len(changed) != 1 || !changed[taskPath] {
		return fmt.Errorf("task authority sync must change only the active Task file %q in the lane", taskPath)
	}
	return nil
}

func (s *Store) validateTaskAuthoritySyncSource(admission Admission, task SemanticTaskRef) error {
	if err := s.validateTransitionSource(TransitionIntent{Source: admission, ExpectedGeneration: admission.Head.ControllerGeneration}); err != nil {
		return err
	}
	if admission.Lease.InFlightCallID != "" {
		return fmt.Errorf("task authority sync requires an idle live lease")
	}
	if admission.Head.ActiveEpisodeID != "" || admission.Head.AcceptedCandidateRef != nil {
		return fmt.Errorf("task authority sync requires an unsuspended execution without a sealed candidate")
	}
	if task.TaskPath != admission.Attempt.SemanticTaskRef.TaskPath || task.Equal(admission.Attempt.SemanticTaskRef) {
		return fmt.Errorf("task authority sync requires a revised same-path Task contract")
	}
	return nil
}

func taskAuthoritySyncPayloadComplete(op ExecutionOperation) bool {
	return op.ParentSource != nil && op.RuntimeBeforeRef != nil && op.Workspace != nil && op.Attempt != nil && op.Lease != nil && op.Rebound != nil && op.TaskAmendment != nil
}

func (s *Store) taskAuthoritySyncExecutionReason(head RepositoryControllerHead) string {
	switch {
	case head.PendingTransitionID != "":
		return "controller transition is in flight"
	case head.LiveAttemptID == "" || head.LiveLeaseID == "":
		return "execution has no live attempt to sync"
	case head.ActiveEpisodeID != "":
		return "execution is suspended for a blocker episode"
	case head.AcceptedCandidateRef != nil:
		return "a sealed completion candidate exists; use the regular rebind or reentry review owners"
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return fmt.Sprintf("live lease is unreadable: %v", err)
	}
	if lease.InFlightCallID != "" {
		return "live lease has an in-flight model call"
	}
	if lease.AttemptID != head.LiveAttemptID || !lease.SemanticTaskRef.Equal(*head.ExecutionTaskRef) {
		return "live lease is bound to a different execution authority"
	}
	return ""
}

func captureTaskSourceTree(repo, remote, taskPath string) (string, error) {
	if _, regular, err := worktreeRegularFileDigest(repo, taskPath); err != nil {
		return "", err
	} else if !regular {
		return "", fmt.Errorf("revised Task file %q is not a regular file", taskPath)
	}
	index, err := newSuspensionIndex(repo, remote)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(index) }()
	entries, err := captureSuspensionEntries(repo, []workspaceTreeEntry{{path: taskPath}})
	if err != nil {
		return "", err
	}
	if len(entries) != 1 || entries[0].mode == "120000" || entries[0].mode == "160000" {
		return "", fmt.Errorf("revised Task file %q is not a single regular file", taskPath)
	}
	if err := writeSuspensionEntries(repo, index, entries); err != nil {
		return "", err
	}
	return suspensionGitText(repo, index, nil, "write-tree")
}

func captureTaskSourceSnapshot(repo, snapshotID, remote, taskPath string) (WorkspaceSnapshot, error) {
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if snapshot.ID != snapshotID || snapshot.Head != remote {
		return WorkspaceSnapshot{}, fmt.Errorf("task source revision snapshot or remote HEAD differs")
	}
	changed, err := primaryWorktreeChanges(repo)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if len(changed) != 1 || !changed[taskPath] {
		return WorkspaceSnapshot{}, fmt.Errorf("parent worktree revision is not limited to the active Task file %q", taskPath)
	}
	return snapshot, nil
}
