package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type UserTaskTransitionInput struct {
	ExpectedGeneration  uint64          `json:"expected_generation"`
	SourceProjectID     string          `json:"source_project_id"`
	SourceTask          SemanticTaskRef `json:"source_task"`
	SuccessorTask       SemanticTaskRef `json:"successor_task"`
	SuccessorProjectID  string          `json:"successor_project_id"`
	CleanupTransitionID string          `json:"cleanup_transition_id"`
	Reason              string          `json:"reason"`
}

type UserTaskTermination struct {
	Execution           *UserExecutionTermination        `json:"execution,omitempty"`
	SchemaVersion       int                              `json:"schema_version"`
	RepositoryIdentity  string                           `json:"repository_identity"`
	TransitionID        string                           `json:"transition_id"`
	SourceHead          RepositoryControllerHead         `json:"source_head"`
	SuccessorProject    ProjectSnapshot                  `json:"successor_project"`
	SuccessorTask       SemanticTaskRef                  `json:"successor_task"`
	CleanupTransitionID string                           `json:"cleanup_transition_id"`
	Reason              string                           `json:"reason"`
	GitArchive          EvidenceObjectRef                `json:"git_archive"`
	RuntimeTermination  *state.ControllerTaskTermination `json:"runtime_termination,omitempty"`
}

type UserTaskTransitionOperation struct {
	Record    UserTaskTermination `json:"record"`
	RecordRef EvidenceObjectRef   `json:"record_ref"`
}

const userTaskTransition = "USER_TERMINATE_TASK_AND_SELECT_SUCCESSOR"

func (s *Store) TerminateTaskAndSelectSuccessor(input UserTaskTransitionInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, head, err := s.planUserTaskTransition(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planUserTaskTransition(input UserTaskTransitionInput) (ExecutionOperation, RepositoryControllerHead, error) {
	head, err := s.quiescentExecutionHead(input.ExpectedGeneration)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	if !s.userTaskTerminationSourceAvailable(head) {
		return ExecutionOperation{}, head, fmt.Errorf("user termination source head is unavailable")
	}
	if head.ProjectSnapshotID != input.SourceProjectID || head.ExecutionTaskRef == nil || !head.ExecutionTaskRef.Equal(input.SourceTask) {
		return ExecutionOperation{}, head, fmt.Errorf("user termination source Task/project authority differs")
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	if authority.ProjectSnapshotID != input.SuccessorProjectID || !authority.Task.Equal(input.SuccessorTask) {
		return ExecutionOperation{}, head, fmt.Errorf("user termination successor authority differs from committed ACTIVE")
	}
	transition, err := executionTransition(head, userTaskTransition)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	return s.buildUserTaskTransition(input, head, authority, transition)
}

func (s *Store) buildUserTaskTransition(input UserTaskTransitionInput, head RepositoryControllerHead, authority CommittedTaskAuthority, transition TransitionRecord) (ExecutionOperation, RepositoryControllerHead, error) {
	archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "user-termination:"+transition.TransitionID, []string{authority.Snapshot.HeadOID})
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	record := UserTaskTermination{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: s.identity.LineageID, TransitionID: transition.TransitionID, SourceHead: head, SuccessorProject: authority.Snapshot, SuccessorTask: authority.Task, CleanupTransitionID: input.CleanupTransitionID, Reason: input.Reason, GitArchive: archive}
	if err := s.planUserRuntimeTermination(&record); err != nil {
		return ExecutionOperation{}, head, err
	}
	if err := s.validateUserTaskTermination(record); err != nil {
		return ExecutionOperation{}, head, err
	}
	if err := s.verifyUserTaskTransitionPrimary(record); err != nil {
		return ExecutionOperation{}, head, err
	}
	ref, err := s.putEvidenceJSON("user-task-termination", transition.TransitionID, record)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	transition.ProjectSnapshotNew = authority.ProjectSnapshotID
	transition.SourceRootTaskRef = *head.RootTaskRef
	transition.SourceExecutionTaskRef = *head.ExecutionTaskRef
	transition.TargetRootTaskRef = authority.Task
	transition.Effects = []EffectExpectation{{Surface: MutationSurfaceState, Resource: "user-task-termination", ExpectedNew: ref.Digest}}
	op := ExecutionOperation{Transition: transition, UserTermination: &UserTaskTransitionOperation{Record: record, RecordRef: ref}}
	return op, head, nil
}

func (s *Store) validateUserTaskTermination(record UserTaskTermination) error {
	head := record.SourceHead
	if record.SchemaVersion != controllerSchemaVersion || record.RepositoryIdentity != s.identity.LineageID || record.TransitionID == "" || strings.TrimSpace(record.Reason) == "" {
		return fmt.Errorf("user termination identity/reason is incomplete")
	}
	if record.Execution == nil && !s.userTaskTerminationSourceAvailable(head) {
		return fmt.Errorf("user termination requires exact quiescent unretired root authority")
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return err
	}
	if !projectHasTask(project, *head.ExecutionTaskRef) {
		return fmt.Errorf("user termination source is absent from ProjectSnapshot")
	}
	if err := s.verifyUserTaskSuccessor(record); err != nil {
		return err
	}
	if record.Execution != nil {
		return s.verifyUserExecutionTermination(record)
	}
	return s.verifyUserTaskCleanup(record)
}

func (s *Store) userTaskTerminationSourceAvailable(head RepositoryControllerHead) bool {
	return head.Status == ControllerStatusActive && head.RepositoryIdentity == s.identity.LineageID && head.LiveLeaseID == "" && head.LiveAttemptID == "" && head.PendingTransitionID == "" && head.ActiveEpisodeID == "" && head.RootTaskRef != nil && head.ExecutionTaskRef != nil && head.RootTaskRef.Equal(*head.ExecutionTaskRef) && head.AcceptedCandidateRef != nil
}

func (s *Store) planUserRuntimeTermination(record *UserTaskTermination) error {
	if record.Execution != nil {
		return s.planUserExecutionRuntimeTermination(record)
	}
	cfg := WorkflowTaskConfig(s.config, s.identity, *record.SourceHead.ExecutionTaskRef)
	st := state.AttachStateStore(cfg)
	if st.TaskStatus() == state.TaskStatusNone {
		return nil
	}
	candidate, err := s.LoadAcceptedCandidate(*record.SourceHead.AcceptedCandidateRef)
	if err != nil {
		return err
	}
	binding, err := st.LoadControllerRuntimeBinding()
	if err != nil {
		return err
	}
	if binding.TaskPath != candidate.TaskRef.TaskPath || binding.TaskContractDigest != candidate.TaskRef.ContractDigest || binding.AttemptID != candidate.AttemptID || st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		return fmt.Errorf("user termination runtime does not match cleaned accepted attempt")
	}
	var cleanup ExecutionOperation
	if err := readJSON(s.executionOperationPath(record.CleanupTransitionID), &cleanup); err != nil {
		return err
	}
	if cleanup.Workspace == nil || st.ReadOr("repo-root", "") != cleanup.Workspace.Root {
		return fmt.Errorf("user termination runtime root differs from cleaned workspace")
	}
	record.RuntimeTermination = &state.ControllerTaskTermination{Version: 1, TransitionID: record.TransitionID, SourceGeneration: record.SourceHead.ControllerGeneration, TaskPath: binding.TaskPath, TaskContractDigest: binding.TaskContractDigest, AttemptID: binding.AttemptID, Reason: record.Reason, PreviousRepoRoot: cleanup.Workspace.Root, PrimaryRoot: s.identity.PrimaryRoot, PreviousStatus: st.TaskStatus()}
	return nil
}

func (s *Store) verifyUserTaskSuccessor(record UserTaskTermination) error {
	project := record.SuccessorProject
	head := record.SourceHead
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, project.HeadOID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(authority.Snapshot, project) || !authority.Task.Equal(record.SuccessorTask) || record.SuccessorTask.TaskPath == head.ExecutionTaskRef.TaskPath {
		return fmt.Errorf("user termination successor is not exact committed ACTIVE")
	}
	if err := s.verifyTerminatedTaskDisposition(record); err != nil {
		return err
	}
	return s.verifyUserTaskSuccessorArchive(record)
}

func (s *Store) verifyTerminatedTaskAbsent(record UserTaskTermination) error {
	head := record.SourceHead
	for _, task := range record.SuccessorProject.Tasks {
		if task.TaskPath == head.ExecutionTaskRef.TaskPath {
			return fmt.Errorf("user terminated Task remains scheduled or in current corpus")
		}
	}
	if _, err := os.Lstat(filepath.Join(s.identity.PrimaryRoot, head.ExecutionTaskRef.TaskPath)); !os.IsNotExist(err) {
		return fmt.Errorf("user terminated Task still exists in primary workspace")
	}
	return nil
}

func (s *Store) verifyUserTaskSuccessorArchive(record UserTaskTermination) error {
	project := record.SuccessorProject
	descendant, err := isPublicationDescendant(s.identity.PrimaryRoot, record.SourceHead.IntegrationTip, project.HeadOID)
	if err != nil || !descendant {
		return fmt.Errorf("user termination successor does not descend from source integration: %w", err)
	}
	roots, err := s.VerifyGitObjectArchive(record.GitArchive)
	if err != nil {
		return err
	}
	if len(roots) != 1 || roots[0].OID != project.HeadOID || roots[0].Type != gitCommitObjectType {
		return fmt.Errorf("user termination successor archive differs")
	}
	return nil
}

func (s *Store) verifyUserTaskCleanup(record UserTaskTermination) error {
	cleanup, err := s.loadUserTaskCleanup(record)
	if err != nil {
		return err
	}
	if err := s.verifyUserTaskCandidate(record, cleanup); err != nil {
		return err
	}
	return s.verifyUserTaskEvidence(record.SourceHead)
}

func (s *Store) loadUserTaskCleanup(record UserTaskTermination) (ExecutionOperation, error) {
	if filepath.Base(record.CleanupTransitionID) != record.CleanupTransitionID || record.CleanupTransitionID == "" {
		return ExecutionOperation{}, fmt.Errorf("user termination requires exact cleanup identity")
	}
	var cleanup ExecutionOperation
	if err := readJSON(s.executionOperationPath(record.CleanupTransitionID), &cleanup); err != nil {
		return ExecutionOperation{}, err
	}
	transition, phase, err := s.LoadTransition(record.CleanupTransitionID)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if !userTaskCleanupMatches(record, cleanup, transition, phase) {
		return ExecutionOperation{}, fmt.Errorf("user termination cleanup authority differs")
	}
	digest, err := executionOperationDigest(cleanup)
	if err != nil || digest != cleanup.Transition.OperationDigest {
		return ExecutionOperation{}, fmt.Errorf("user termination cleanup journal integrity differs: %w", err)
	}
	if err := validateExecutionOperationEffects(cleanup, executionOperationEffectsSingle); err != nil {
		return ExecutionOperation{}, err
	}
	if err := s.validateCleanupOperation(cleanup); err != nil {
		return ExecutionOperation{}, err
	}
	if err := s.verifyCommittedCleanup(cleanup, record.SourceHead); err != nil {
		return ExecutionOperation{}, err
	}
	return cleanup, nil
}

func userTaskCleanupMatches(record UserTaskTermination, cleanup ExecutionOperation, transition TransitionRecord, phase TransitionState) bool {
	return reflect.DeepEqual(transition, cleanup.Transition) && transition.Kind == executionCleanup && phase.Phase == TransitionPhaseFinalized && transition.TargetGeneration == record.SourceHead.ControllerGeneration && transition.ProjectSnapshotNew == record.SourceHead.ProjectSnapshotID
}

func (s *Store) verifyUserTaskCandidate(record UserTaskTermination, cleanup ExecutionOperation) error {
	candidate, err := s.LoadAcceptedCandidate(*record.SourceHead.AcceptedCandidateRef)
	if err != nil {
		return err
	}
	if candidate.EvidenceValid || candidate.State == candidateObserved || !candidate.TaskRef.Equal(*record.SourceHead.ExecutionTaskRef) || candidate.ProjectSnapshotID != record.SourceHead.ProjectSnapshotID || cleanup.SealRef == nil || !evidenceRefsEqual(candidate.SealRef, *cleanup.SealRef) {
		return fmt.Errorf("user termination candidate is not the cleaned unvalidated source")
	}
	if _, err := s.VerifyGitObjectArchive(candidate.GitArchive); err != nil {
		return err
	}
	return nil
}

func (s *Store) verifyUserTaskEvidence(head RepositoryControllerHead) error {
	if _, _, _, _, err := s.loadPublishedEvidenceAuthority(head); err != nil {
		return err
	}
	if head.EvidenceHeadRef == nil || head.EvidenceLedgerHeadRef == nil {
		return fmt.Errorf("user termination requires preserved evidence authority")
	}
	_, err := s.validateEvidenceGraphRoots(*head.EvidenceLedgerHeadRef, *head.EvidenceHeadRef, head.EvidenceLedgerSequence)
	return err
}

func (s *Store) validateUserTaskTransitionOperation(op ExecutionOperation) error {
	p := op.UserTermination
	if p == nil || op.Terminal != nil || op.Publication != nil {
		return fmt.Errorf("user termination operation is incomplete")
	}
	record, err := s.LoadUserTaskTermination(p.RecordRef)
	if err != nil {
		return err
	}
	if op.Transition.Kind != userTerminationKind(record) {
		return fmt.Errorf("user termination kind differs from disposition")
	}
	if err := validateUserTaskTransitionJournal(op, record); err != nil {
		return err
	}
	expected := []EffectExpectation{{Surface: MutationSurfaceState, Resource: "user-task-termination", ExpectedNew: p.RecordRef.Digest}}
	if !reflect.DeepEqual(expected, op.Transition.Effects) {
		return fmt.Errorf("user termination effects differ")
	}
	return s.validateUserTaskTermination(record)
}

func validateUserTaskTransitionJournal(op ExecutionOperation, record UserTaskTermination) error {
	if !reflect.DeepEqual(record, op.UserTermination.Record) || record.TransitionID != op.Transition.TransitionID || record.SourceHead.ControllerGeneration != op.Transition.SourceGeneration || record.SourceHead.ProjectSnapshotID != op.Transition.ProjectSnapshotOld || record.SuccessorProject.SnapshotID != op.Transition.ProjectSnapshotNew || !record.SuccessorTask.Equal(op.Transition.TargetRootTaskRef) {
		return fmt.Errorf("user termination journal authority differs")
	}
	return nil
}

func (s *Store) LoadUserTaskTermination(ref EvidenceObjectRef) (UserTaskTermination, error) {
	if err := validateTypedEvidenceRef(ref, "user-task-termination"); err != nil {
		return UserTaskTermination{}, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return UserTaskTermination{}, err
	}
	var record UserTaskTermination
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	if record.TransitionID != ref.LogicalIdentity {
		return record, fmt.Errorf("user termination evidence identity differs")
	}
	return record, nil
}

func (s *Store) applyUserTaskTransition(op ExecutionOperation) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	source := head
	source.ControllerGeneration = op.Transition.SourceGeneration
	source.PendingTransitionID = ""
	if !reflect.DeepEqual(source, op.UserTermination.Record.SourceHead) {
		return fmt.Errorf("user termination prepared source authority changed")
	}
	if err := s.verifyUserTaskTransitionPrimary(op.UserTermination.Record); err != nil {
		return err
	}
	if err := s.writeProjectSnapshot(op.UserTermination.Record.SuccessorProject); err != nil {
		return err
	}
	if runtime := op.UserTermination.Record.RuntimeTermination; runtime != nil {
		cfg := WorkflowTaskConfig(s.config, s.identity, *op.UserTermination.Record.SourceHead.ExecutionTaskRef)
		if err := state.AttachStateStore(cfg).RecordControllerTaskTermination(*runtime); err != nil {
			return err
		}
	}
	if err := s.applyUserExecutionTermination(op.UserTermination.Record); err != nil {
		return err
	}
	actual := map[string]string{op.Transition.Effects[0].Key(): op.UserTermination.RecordRef.Digest}
	_, err = s.commitAuthorityTransitionCoreLocked(op.Transition, actual, false, func(next *RepositoryControllerHead) error {
		next.UserTerminationRef = &op.UserTermination.RecordRef
		next.RootTaskRef = &op.UserTermination.Record.SuccessorTask
		next.ExecutionTaskRef = nil
		next.AcceptedCandidateRef = nil
		next.LiveAttemptID = ""
		next.LiveLeaseID = ""
		next.MetadataLineageRef = nil
		next.ProjectSnapshotID = op.UserTermination.Record.SuccessorProject.SnapshotID
		next.IntegrationTip = op.UserTermination.Record.SuccessorProject.HeadOID
		next.ObservedRemoteTip = next.IntegrationTip
		return nil
	})
	return err
}

func (s *Store) verifyUserTaskTransitionPrimary(record UserTaskTermination) error {
	current, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return err
	}
	if current.ProjectSnapshotID != record.SuccessorProject.SnapshotID {
		return fmt.Errorf("user termination primary HEAD changed")
	}
	plan, err := os.ReadFile(filepath.Join(s.identity.PrimaryRoot, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		return err
	}
	active, err := taskcontract.ParsePlanSchedule(string(plan)).ActiveTask()
	if err != nil || active != record.SuccessorTask.TaskPath {
		return fmt.Errorf("user termination working Plan ACTIVE differs: %w", err)
	}
	if record.SourceHead.PublicationPolicy == nil {
		return fmt.Errorf("user termination publication policy is unavailable")
	}
	policy := *record.SourceHead.PublicationPolicy
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, policy.LocalRef)
	if err != nil || !exists || local != current.Snapshot.HeadOID {
		return fmt.Errorf("user termination local authority differs: %w", err)
	}
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, policy)
	if err != nil {
		return err
	}
	if remote != local {
		return fmt.Errorf("user termination requires matching observed local/remote successor tip")
	}
	return nil
}

func (*Store) verifyCommittedUserTaskTransition(op ExecutionOperation, head RepositoryControllerHead) error {
	p := op.UserTermination
	if p == nil || head.UserTerminationRef == nil || !evidenceRefsEqual(*head.UserTerminationRef, p.RecordRef) || head.RootTaskRef == nil || !head.RootTaskRef.Equal(p.Record.SuccessorTask) || head.ProjectSnapshotID != p.Record.SuccessorProject.SnapshotID || head.IntegrationTip != p.Record.SuccessorProject.HeadOID {
		return fmt.Errorf("committed user termination authority differs")
	}
	return verifyUserTerminationClearedExecution(head)
}

func verifyUserTerminationClearedExecution(head RepositoryControllerHead) error {
	if head.ExecutionTaskRef != nil || head.LiveAttemptID != "" || head.LiveLeaseID != "" || head.AcceptedCandidateRef != nil {
		return fmt.Errorf("committed user termination retains live execution authority")
	}
	return nil
}

func (s *Store) UserTerminatedSuccessorAuthority(head RepositoryControllerHead) (CommittedTaskAuthority, error) {
	if head.UserTerminationRef == nil || head.ExecutionTaskRef != nil || head.LiveLeaseID != "" || head.LiveAttemptID != "" || head.PendingTransitionID != "" || head.RootTaskRef == nil {
		return CommittedTaskAuthority{}, fmt.Errorf("user termination successor is not quiescent")
	}
	record, err := s.LoadUserTaskTermination(*head.UserTerminationRef)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	op := ExecutionOperation{UserTermination: &UserTaskTransitionOperation{Record: record, RecordRef: *head.UserTerminationRef}}
	if err := s.verifyCommittedUserTaskTransition(op, head); err != nil {
		return CommittedTaskAuthority{}, err
	}
	if err := s.verifyFinalizedUserTaskTransition(record, head); err != nil {
		return CommittedTaskAuthority{}, err
	}
	return ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, head.IntegrationTip)
}

func (s *Store) verifyFinalizedUserTaskTransition(record UserTaskTermination, head RepositoryControllerHead) error {
	transition, phase, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return err
	}
	if transition.Kind != userTerminationKind(record) || phase.Phase != TransitionPhaseFinalized || transition.TargetGeneration != head.ControllerGeneration {
		return fmt.Errorf("user termination successor lacks finalized transition")
	}
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(record.TransitionID), &op); err != nil {
		return err
	}
	digest, err := executionOperationDigest(op)
	if err != nil || digest != transition.OperationDigest || !reflect.DeepEqual(op.Transition, transition) || op.UserTermination == nil || !reflect.DeepEqual(op.UserTermination.Record, record) {
		return fmt.Errorf("user termination finalized journal differs")
	}
	return nil
}
