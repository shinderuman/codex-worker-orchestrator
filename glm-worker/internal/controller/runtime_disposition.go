package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type RuntimeDispositionRequired struct{ Input RuntimeDispositionInput }

type RuntimeDispositionInput struct {
	ExpectedGeneration uint64          `json:"expected_generation"`
	Task               SemanticTaskRef `json:"task"`
	RuntimeTaskID      string          `json:"runtime_task_id"`
	Reason             string          `json:"reason"`
}

type RuntimeDispositionOperation struct {
	Input        RuntimeDispositionInput  `json:"input"`
	Record       state.RuntimeDisposition `json:"record"`
	EvidenceRefs []EvidenceObjectRef      `json:"evidence_refs"`
}

const executionRuntimeDisposition = "DISPOSE_UNFINISHED_RUNTIME"

func (e *RuntimeDispositionRequired) Error() string {
	return "publication/start requires explicit disposition of unfinished Runtime " + e.Input.RuntimeTaskID + " for " + e.Input.Task.TaskPath
}

func (s *Store) runtimeDispositionTarget(head RepositoryControllerHead) (SemanticTaskRef, error) {
	if head.PendingTransitionID != "" || head.LiveAttemptID != "" || head.LiveLeaseID != "" || runtimeDispositionSuspended(head) || head.RootTaskRef == nil {
		return SemanticTaskRef{}, fmt.Errorf("runtime disposition requires quiescent root authority")
	}
	if head.ExecutionTaskRef == nil {
		return *head.RootTaskRef, nil
	}
	if head.AcceptedCandidateRef == nil {
		return SemanticTaskRef{}, fmt.Errorf("runtime disposition has no successor authority")
	}
	candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	if len(candidate.TerminalFiles) == 0 {
		return SemanticTaskRef{}, fmt.Errorf("runtime disposition requires an accepted terminal candidate")
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, candidate.CommitOID)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	if authority.Task.TaskPath == head.ExecutionTaskRef.TaskPath {
		return SemanticTaskRef{}, fmt.Errorf("runtime disposition cannot terminate current execution")
	}
	return authority.Task, nil
}

func (s *Store) dispositionRuntime(task SemanticTaskRef) *state.StateStore {
	cfg := WorkflowTaskConfig(s.config, s.identity, task)
	cfg.RepoRoot = s.identity.PrimaryRoot
	return state.AttachStateStore(cfg)
}

func runtimeDispositionSuspended(head RepositoryControllerHead) bool {
	return head.ActiveEpisodeID != "" || (head.RootTaskRef != nil && head.ExecutionTaskRef != nil && !head.RootTaskRef.Equal(*head.ExecutionTaskRef))
}

func (s *Store) PreviewRuntimeDisposition(head RepositoryControllerHead) (*RuntimeDispositionInput, error) {
	if runtimeDispositionSuspended(head) {
		return nil, nil
	}
	task, err := s.runtimeDispositionTarget(head)
	if err != nil {
		return nil, err
	}
	runtime := s.dispositionRuntime(task)
	if !runtime.Exists("task.id") {
		return nil, nil
	}
	if admitted, err := quiescentRuntimeReady(runtime); err == nil && admitted {
		return nil, nil
	}
	status := runtime.TaskStatus()
	if !status.Known() || status == state.TaskStatusNone || status == state.TaskStatusComplete || status == state.TaskStatusUserTerminated {
		return nil, fmt.Errorf("runtime disposition requires a known unfinished Runtime")
	}
	if err := verifyDispositionRuntimeBinding(runtime, task, head); err != nil {
		return nil, err
	}
	id, err := runtime.TaskID()
	if err != nil {
		return nil, err
	}
	return &RuntimeDispositionInput{ExpectedGeneration: head.ControllerGeneration, Task: task, RuntimeTaskID: id}, nil
}

func verifyDispositionRuntimeBinding(runtime *state.StateStore, task SemanticTaskRef, head RepositoryControllerHead) error {
	if binding, err := runtime.LoadControllerRuntimeBinding(); err == nil {
		if binding.TaskPath != task.TaskPath || binding.AttemptID == head.LiveAttemptID {
			return fmt.Errorf("runtime disposition binding is not an inactive target")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) DisposeUnfinishedRuntime(input RuntimeDispositionInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, err := s.quiescentExecutionHead(input.ExpectedGeneration)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op, err := s.planRuntimeDisposition(input, head)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planRuntimeDisposition(input RuntimeDispositionInput, head RepositoryControllerHead) (ExecutionOperation, error) {
	expected, err := s.PreviewRuntimeDisposition(head)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if expected == nil || expected.Task != input.Task || expected.RuntimeTaskID != input.RuntimeTaskID || strings.TrimSpace(input.Reason) == "" {
		return ExecutionOperation{}, fmt.Errorf("runtime disposition requires exact target and explicit termination reason")
	}
	runtime := s.dispositionRuntime(input.Task)
	runtimeLock, err := repolock.Acquire(runtime.Path("lock"))
	if err != nil {
		return ExecutionOperation{}, fmt.Errorf("runtime disposition source is running: %w", err)
	}
	defer func() { _ = runtimeLock.Close() }()
	record, err := executionTransition(head, executionRuntimeDisposition)
	if err != nil {
		return ExecutionOperation{}, err
	}
	seal := &AttemptSeal{AttemptID: "runtime-disposition:" + input.RuntimeTaskID}
	capture := attemptRuntimeEvidence{store: s, runtime: runtime, taskID: input.RuntimeTaskID, workspace: s.identity.PrimaryRoot, seal: seal}
	if err := capture.captureStateFiles(); err != nil {
		return ExecutionOperation{}, err
	}
	archive, err := s.putEvidenceJSON("runtime-disposition-snapshot", record.TransitionID, seal.EvidenceRefs)
	if err != nil {
		return ExecutionOperation{}, err
	}
	disposition := &RuntimeDispositionOperation{Input: input, EvidenceRefs: seal.EvidenceRefs, Record: state.RuntimeDisposition{Version: 1, TaskID: input.RuntimeTaskID, TaskPath: input.Task.TaskPath, TransitionID: record.TransitionID, EvidenceDigest: archive.Digest, Reason: input.Reason, PreviousStatus: runtime.TaskStatus()}}
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceState, Resource: "runtime-disposition:" + input.Task.TaskPath, ExpectedOld: input.RuntimeTaskID, ExpectedNew: archive.Digest}}
	op := ExecutionOperation{Transition: record, Source: Admission{Head: head}, RuntimeDisposition: disposition, RuntimeBeforeRef: &archive}
	op.Evidence, err = s.runtimeDispositionEvidence(op)
	if err != nil {
		return ExecutionOperation{}, err
	}
	return op, nil
}

func validateRuntimeDispositionIdentity(op ExecutionOperation) error {
	p := op.RuntimeDisposition
	if p == nil || op.RuntimeBeforeRef == nil {
		return fmt.Errorf("runtime disposition operation is missing")
	}
	expected := state.RuntimeDisposition{Version: 1, TaskID: p.Input.RuntimeTaskID, TaskPath: p.Input.Task.TaskPath, TransitionID: op.Transition.TransitionID, EvidenceDigest: op.RuntimeBeforeRef.Digest, Reason: p.Input.Reason, PreviousStatus: p.Record.PreviousStatus}
	if p.Record != expected || strings.TrimSpace(p.Input.Reason) == "" || p.Input.ExpectedGeneration != op.Transition.SourceGeneration {
		return fmt.Errorf("runtime disposition operation binding differs")
	}
	return nil
}

func (s *Store) validateRuntimeDisposition(op ExecutionOperation) error {
	if err := validateRuntimeDispositionIdentity(op); err != nil {
		return err
	}
	p := op.RuntimeDisposition
	target, err := s.runtimeDispositionTarget(op.Source.Head)
	if err != nil || target != p.Input.Task {
		return fmt.Errorf("runtime disposition target authority differs: %w", err)
	}
	if op.Source.Head.ControllerGeneration != op.Transition.SourceGeneration || op.Source.Head.ProjectSnapshotID != op.Transition.ProjectSnapshotOld || op.Transition.ProjectSnapshotNew != op.Transition.ProjectSnapshotOld {
		return fmt.Errorf("runtime disposition source authority differs")
	}
	if err := s.validateRuntimeDispositionSnapshot(op); err != nil {
		return err
	}
	return s.validateRuntimeDispositionEvidence(op)
}

func (s *Store) validateRuntimeDispositionSnapshot(op ExecutionOperation) error {
	p := op.RuntimeDisposition
	data, err := s.LoadEvidenceObject(*op.RuntimeBeforeRef)
	if err != nil {
		return err
	}
	var refs []EvidenceObjectRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return err
	}
	if !reflect.DeepEqual(refs, p.EvidenceRefs) {
		return fmt.Errorf("runtime disposition archive differs")
	}
	for _, ref := range refs {
		if _, err := s.LoadEvidenceObject(ref); err != nil {
			return err
		}
	}
	expected := []EffectExpectation{{Surface: MutationSurfaceState, Resource: "runtime-disposition:" + p.Input.Task.TaskPath, ExpectedOld: p.Input.RuntimeTaskID, ExpectedNew: op.RuntimeBeforeRef.Digest}}
	if !reflect.DeepEqual(expected, op.Transition.Effects) {
		return fmt.Errorf("runtime disposition effects differ")
	}
	return nil
}

func (s *Store) applyRuntimeDisposition(op ExecutionOperation) error {
	p := op.RuntimeDisposition
	runtime := s.dispositionRuntime(p.Input.Task)
	lock, err := repolock.Acquire(runtime.Path("lock"))
	if err != nil {
		return fmt.Errorf("runtime disposition source is running: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if !runtime.Exists(state.RuntimeDispositionFile) {
		seal := &AttemptSeal{AttemptID: "runtime-disposition:" + p.Input.RuntimeTaskID}
		capture := attemptRuntimeEvidence{store: s, runtime: runtime, taskID: p.Input.RuntimeTaskID, workspace: s.identity.PrimaryRoot, seal: seal}
		if err := capture.captureStateFiles(); err != nil {
			return err
		}
		if !reflect.DeepEqual(seal.EvidenceRefs, p.EvidenceRefs) {
			return fmt.Errorf("runtime disposition source changed after preparation")
		}
	}
	if err := runtime.RecordRuntimeDisposition(p.Record); err != nil {
		return err
	}
	actual := map[string]string{op.Transition.Effects[0].Key(): p.Record.EvidenceDigest}
	_, _, err = s.commitAuthorityTransitionWithEvidenceLocked(op.Transition, actual, op.Evidence, nil)
	return err
}

func (s *Store) verifyCommittedRuntimeDisposition(op ExecutionOperation, _ RepositoryControllerHead) error {
	runtime := s.dispositionRuntime(op.RuntimeDisposition.Input.Task)
	if runtime.ReadOr("task.id", "") != op.RuntimeDisposition.Input.RuntimeTaskID || runtime.TaskStatus() != state.TaskStatusUserTerminated {
		return fmt.Errorf("runtime disposition terminal identity changed")
	}
	data, err := os.ReadFile(runtime.Path(state.RuntimeDispositionFile))
	if err != nil {
		return err
	}
	var record state.RuntimeDisposition
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	if record != op.RuntimeDisposition.Record {
		return fmt.Errorf("runtime disposition terminal record differs")
	}
	_, admitted, err := runtime.AdmitNewTask()
	if err != nil || !admitted {
		return fmt.Errorf("runtime disposition does not release new-task admission: %w", err)
	}
	return nil
}

func (s *Store) runtimeDispositionEvidence(op ExecutionOperation) (EvidencePublicationInput, error) {
	recordRef, err := s.putEvidenceJSON("runtime-disposition", op.Transition.TransitionID, *op.RuntimeDisposition)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	revision, err := s.runtimeDispositionRevision(op, recordRef)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	ref, _, err := s.StoreTaskIndexRevision(revision)
	return EvidencePublicationInput{TaskRevisionRefs: []EvidenceObjectRef{ref}}, err
}

func (s *Store) runtimeDispositionRevision(op ExecutionOperation, recordRef EvidenceObjectRef) (TaskIndexRevision, error) {
	p := op.RuntimeDisposition
	_, _, head, _, err := s.loadPublishedEvidenceAuthority(op.Source.Head)
	if err != nil {
		return TaskIndexRevision{}, err
	}
	revision := TaskIndexRevision{SchemaVersion: evidenceSchemaVersion, TaskRef: p.Input.Task, SemanticStatus: "pending"}
	if previous, ok := evidenceSubjectHead(head.TaskHeads, taskEvidenceSubjectID(p.Input.Task)); ok {
		revision, err = s.LoadTaskIndexRevision(previous.RevisionRef)
		if err != nil {
			return TaskIndexRevision{}, err
		}
		revision.PreviousRevision = &previous.RevisionRef
	}
	revision.ControllerGeneration = op.Transition.CommittedGeneration
	revision.TransitionID = op.Transition.TransitionID
	revision.CreatedAt = op.Transition.CreatedAt
	revision.PublicationLineageRecords = append(revision.PublicationLineageRecords, p.EvidenceRefs...)
	revision.PublicationLineageRecords = append(revision.PublicationLineageRecords, *op.RuntimeBeforeRef, recordRef)
	return canonicalTaskIndexRevision(revision)
}

func (s *Store) validateRuntimeDispositionEvidence(op ExecutionOperation) error {
	if len(op.Evidence.TaskRevisionRefs) != 1 || !reflect.DeepEqual(op.Evidence, EvidencePublicationInput{TaskRevisionRefs: op.Evidence.TaskRevisionRefs}) {
		return fmt.Errorf("runtime disposition evidence publication differs")
	}
	if err := validateTypedEvidenceRef(*op.RuntimeBeforeRef, "runtime-disposition-snapshot"); err != nil {
		return err
	}
	revision, err := s.LoadTaskIndexRevision(op.Evidence.TaskRevisionRefs[0])
	if err != nil {
		return err
	}
	ref, err := runtimeDispositionRecordRef(revision, op.Transition.TransitionID)
	if err != nil {
		return err
	}
	raw, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return err
	}
	var record RuntimeDispositionOperation
	if err := decodeControllerJSON(raw, &record); err != nil {
		return err
	}
	if !reflect.DeepEqual(record, *op.RuntimeDisposition) {
		return fmt.Errorf("runtime disposition evidence record differs")
	}
	expected, err := s.runtimeDispositionRevision(op, ref)
	if err != nil {
		return err
	}
	expected.RevisionID = revision.RevisionID
	if !reflect.DeepEqual(expected, revision) {
		return fmt.Errorf("runtime disposition Task evidence differs")
	}
	return nil
}

func runtimeDispositionRecordRef(revision TaskIndexRevision, transitionID string) (EvidenceObjectRef, error) {
	for _, ref := range revision.PublicationLineageRecords {
		if ref.Kind == "runtime-disposition" && ref.LogicalIdentity == transitionID {
			return ref, nil
		}
	}
	return EvidenceObjectRef{}, fmt.Errorf("runtime disposition evidence record is missing")
}
