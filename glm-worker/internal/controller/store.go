package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const controllerSchemaVersion = 1

type Store struct {
	dir      string
	identity RepositoryIdentity
}

func Open(cfg config.AppConfig) (*Store, error) {
	identity, err := ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(filepath.Dir(cfg.StateBase), "controllers", identity.LineageID)
	for _, path := range []string{
		base,
		filepath.Join(base, "attempts"),
		filepath.Join(base, "leases"),
		filepath.Join(base, "transitions"),
		filepath.Join(base, "transition-state"),
		filepath.Join(base, "mutations"),
		filepath.Join(base, "failures"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, fmt.Errorf("create repository controller store: %w", err)
		}
	}
	store := &Store{dir: base, identity: identity}
	if _, err := os.Stat(store.headPath()); errors.Is(err, os.ErrNotExist) {
		head := RepositoryControllerHead{
			SchemaVersion:        controllerSchemaVersion,
			RepositoryIdentity:   identity.LineageID,
			ControllerGeneration: 0,
			Status:               ControllerStatusActive,
		}
		if err := writeJSONAtomic(store.headPath(), head); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect repository controller head: %w", err)
	}
	if _, err := store.LoadHead(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Identity() RepositoryIdentity {
	return s.identity
}

func (s *Store) LockPath() string {
	return filepath.Join(s.dir, "lock")
}

func (s *Store) LoadHead() (RepositoryControllerHead, error) {
	var head RepositoryControllerHead
	if err := readJSON(s.headPath(), &head); err != nil {
		return RepositoryControllerHead{}, fmt.Errorf("read repository controller head: %w", err)
	}
	if head.SchemaVersion != controllerSchemaVersion || head.RepositoryIdentity != s.identity.LineageID {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller head identity is invalid")
	}
	if head.Status != ControllerStatusActive && head.Status != ControllerStatusFailClosed {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller status is invalid: %s", head.Status)
	}
	return head, nil
}

func (s *Store) BootstrapExecution(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" {
		return Admission{}, fmt.Errorf("repository controller is not available for execution bootstrap")
	}
	if head.LiveLeaseID != "" || head.LiveAttemptID != "" {
		return s.AdmitMutation(task, workspace, snapshot)
	}
	if workspace.Root != s.identity.PrimaryRoot {
		return Admission{}, fmt.Errorf("only the verified primary worktree may bootstrap repository mutation authority")
	}
	if workspace.RepositoryID != s.identity.LineageID {
		return Admission{}, fmt.Errorf("workspace repository identity does not match controller")
	}
	attemptID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	leaseID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	nextGeneration := head.ControllerGeneration + 1
	root := task
	attempt := AttemptRecord{
		SchemaVersion:             controllerSchemaVersion,
		AttemptID:                 attemptID,
		SemanticTaskRef:           task,
		RootTaskRef:               root,
		ExecutionBaseOID:          snapshot.Head,
		BaselineSnapshotID:        snapshot.ID,
		WorkspaceSnapshotID:       snapshot.ID,
		StartControllerGeneration: nextGeneration,
		AttemptState:              AttemptStateLive,
		CreatedAt:                 time.Now().UTC(),
	}
	lease := ExecutionLease{
		SchemaVersion:               controllerSchemaVersion,
		LeaseID:                     leaseID,
		AttemptID:                   attemptID,
		SemanticTaskRef:             task,
		Purpose:                     "root-execution",
		ControllerGeneration:        nextGeneration,
		WorkspaceID:                 workspace.ID,
		ExpectedBaseOID:             snapshot.Head,
		ExpectedWorkspaceSnapshotID: snapshot.ID,
		CreatedAt:                   time.Now().UTC(),
	}
	if err := s.writeAttempt(attempt); err != nil {
		return Admission{}, err
	}
	if err := s.writeLease(lease); err != nil {
		return Admission{}, err
	}
	next := head
	next.ControllerGeneration = nextGeneration
	next.RootTaskRef = &root
	next.ExecutionTaskRef = &task
	next.LiveAttemptID = attemptID
	next.LiveLeaseID = leaseID
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return Admission{}, err
	}
	return Admission{Head: next, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: snapshot}, nil
}

func (s *Store) AdmitMutation(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.Status != ControllerStatusActive {
		return Admission{}, fmt.Errorf("repository controller is fail-closed")
	}
	if head.PendingTransitionID != "" {
		return Admission{}, fmt.Errorf("repository controller has pending transition %s", head.PendingTransitionID)
	}
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return Admission{}, fmt.Errorf("repository controller has no live execution lease")
	}
	if !head.ExecutionTaskRef.Equal(task) {
		return Admission{}, fmt.Errorf("semantic execution task does not match repository controller authority")
	}
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return Admission{}, err
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return Admission{}, err
	}
	if attempt.AttemptState != AttemptStateLive || attempt.AttemptID != lease.AttemptID || attempt.AttemptID != head.LiveAttemptID {
		return Admission{}, fmt.Errorf("live attempt/lease identity is inconsistent")
	}
	if !attempt.SemanticTaskRef.Equal(task) || !lease.SemanticTaskRef.Equal(task) {
		return Admission{}, fmt.Errorf("live attempt/lease semantic task is stale")
	}
	if lease.ControllerGeneration != head.ControllerGeneration {
		return Admission{}, fmt.Errorf("execution lease generation is stale: lease=%d controller=%d", lease.ControllerGeneration, head.ControllerGeneration)
	}
	if workspace.RepositoryID != head.RepositoryIdentity || workspace.ID != lease.WorkspaceID {
		return Admission{}, fmt.Errorf("execution workspace does not match live lease")
	}
	if snapshot.Head != lease.ExpectedBaseOID || snapshot.ID != lease.ExpectedWorkspaceSnapshotID {
		return Admission{}, fmt.Errorf("execution workspace snapshot does not match live lease")
	}
	return Admission{Head: head, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: snapshot}, nil
}

func (s *Store) RecordMutation(admission Admission, command, outcome string, after WorkspaceSnapshot) (Admission, error) {
	current, err := s.AdmitMutation(admission.Lease.SemanticTaskRef, admission.Workspace, admission.Snapshot)
	if err != nil {
		return Admission{}, err
	}
	if current.Lease.LeaseID != admission.Lease.LeaseID {
		return Admission{}, fmt.Errorf("execution lease changed before mutation provenance commit")
	}
	mutationID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	leaseID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	nextGeneration := current.Head.ControllerGeneration + 1
	nextLease := current.Lease
	nextLease.LeaseID = leaseID
	nextLease.ControllerGeneration = nextGeneration
	nextLease.ExpectedBaseOID = after.Head
	nextLease.ExpectedWorkspaceSnapshotID = after.ID
	nextLease.InFlightCallID = ""
	nextLease.CreatedAt = time.Now().UTC()
	record := MutationRecord{
		SchemaVersion:    controllerSchemaVersion,
		MutationID:       mutationID,
		AttemptID:        current.Attempt.AttemptID,
		SourceLeaseID:    current.Lease.LeaseID,
		TargetLeaseID:    nextLease.LeaseID,
		SourceGeneration: current.Head.ControllerGeneration,
		TargetGeneration: nextGeneration,
		Command:          command,
		Outcome:          outcome,
		Before:           current.Snapshot,
		After:            after,
		Surfaces:         changedSurfaces(current.Snapshot, after),
		CreatedAt:        time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.mutationPath(mutationID), record); err != nil {
		return Admission{}, err
	}
	if err := s.writeLease(nextLease); err != nil {
		return Admission{}, err
	}
	nextHead := current.Head
	nextHead.ControllerGeneration = nextGeneration
	nextHead.LiveLeaseID = nextLease.LeaseID
	if err := s.writeHeadCAS(current.Head.ControllerGeneration, nextHead); err != nil {
		return Admission{}, err
	}
	return Admission{Head: nextHead, Attempt: current.Attempt, Lease: nextLease, Workspace: current.Workspace, Snapshot: after}, nil
}

func (s *Store) BeginTransition(kind string, expectedGeneration uint64, effects []EffectExpectation) (TransitionRecord, error) {
	head, err := s.LoadHead()
	if err != nil {
		return TransitionRecord{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" {
		return TransitionRecord{}, fmt.Errorf("repository controller is not available for transition")
	}
	if head.ControllerGeneration != expectedGeneration {
		return TransitionRecord{}, fmt.Errorf("controller generation CAS failed: got=%d want=%d", head.ControllerGeneration, expectedGeneration)
	}
	transitionID, err := state.NewUUID()
	if err != nil {
		return TransitionRecord{}, err
	}
	record := TransitionRecord{
		SchemaVersion:          controllerSchemaVersion,
		TransitionID:          transitionID,
		Kind:                  kind,
		SourceGeneration:      expectedGeneration,
		PreparedGeneration:    expectedGeneration + 1,
		TargetGeneration:      expectedGeneration + 2,
		SourceEpisodeID:       head.ActiveEpisodeID,
		SourceEpisodeRevision: head.ActiveEpisodeRevision,
		SourceLeaseID:         head.LiveLeaseID,
		ProjectSnapshotOld:    head.ProjectSnapshotID,
		Effects:               append([]EffectExpectation(nil), effects...),
		CreatedAt:             time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.transitionPath(transitionID), record); err != nil {
		return TransitionRecord{}, err
	}
	transitionState := TransitionState{
		SchemaVersion: controllerSchemaVersion,
		TransitionID: transitionID,
		Phase:        TransitionPhasePrepared,
		UpdatedAt:    time.Now().UTC(),
	}
	if err := s.writeTransitionState(transitionState); err != nil {
		return TransitionRecord{}, err
	}
	next := head
	next.ControllerGeneration = record.PreparedGeneration
	next.PendingTransitionID = transitionID
	if err := s.writeHeadCAS(expectedGeneration, next); err != nil {
		return TransitionRecord{}, err
	}
	return record, nil
}

func (s *Store) ClassifyTransition(record TransitionRecord, actual map[string]string) map[string]EffectClassification {
	result := make(map[string]EffectClassification, len(record.Effects))
	for _, effect := range record.Effects {
		observed := actual[effect.Key()]
		switch observed {
		case effect.ExpectedOld:
			result[effect.Key()] = EffectExpectedOld
		case effect.ExpectedNew:
			result[effect.Key()] = EffectExpectedNew
		default:
			result[effect.Key()] = EffectUnexpected
		}
	}
	return result
}

func (s *Store) MarkTransitionApplied(record TransitionRecord, actual map[string]string) error {
	stateRecord, err := s.loadTransitionState(record.TransitionID)
	if err != nil {
		return err
	}
	if stateRecord.Phase != TransitionPhasePrepared && stateRecord.Phase != TransitionPhaseApplied {
		return fmt.Errorf("transition %s cannot be marked applied from %s", record.TransitionID, stateRecord.Phase)
	}
	stateRecord.Phase = TransitionPhaseApplied
	stateRecord.Observed = cloneMap(actual)
	stateRecord.Classifications = s.ClassifyTransition(record, actual)
	stateRecord.UpdatedAt = time.Now().UTC()
	return s.writeTransitionState(stateRecord)
}

func (s *Store) CommitTransition(record TransitionRecord, actual map[string]string, finalize bool, mutate func(*RepositoryControllerHead) error) (RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	if head.PendingTransitionID != record.TransitionID || head.ControllerGeneration != record.PreparedGeneration {
		return RepositoryControllerHead{}, fmt.Errorf("transition %s no longer owns controller CAS", record.TransitionID)
	}
	classifications := s.ClassifyTransition(record, actual)
	for _, classification := range classifications {
		if classification != EffectExpectedNew {
			return RepositoryControllerHead{}, fmt.Errorf("transition %s target effects are not proven", record.TransitionID)
		}
	}
	next := head
	if mutate != nil {
		if err := mutate(&next); err != nil {
			return RepositoryControllerHead{}, err
		}
	}
	next.ControllerGeneration = record.TargetGeneration
	if finalize {
		next.PendingTransitionID = ""
	}
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return RepositoryControllerHead{}, err
	}
	transitionState := TransitionState{
		SchemaVersion:    controllerSchemaVersion,
		TransitionID:    record.TransitionID,
		Phase:           TransitionPhaseCommitted,
		Observed:        cloneMap(actual),
		Classifications: classifications,
		UpdatedAt:       time.Now().UTC(),
	}
	if finalize {
		transitionState.Phase = TransitionPhaseFinalized
	}
	if err := s.writeTransitionState(transitionState); err != nil {
		return RepositoryControllerHead{}, err
	}
	return next, nil
}

func (s *Store) FinalizeTransition(record TransitionRecord) (RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	if head.PendingTransitionID != record.TransitionID || head.ControllerGeneration != record.TargetGeneration {
		return RepositoryControllerHead{}, fmt.Errorf("transition %s is not pending finalization", record.TransitionID)
	}
	next := head
	next.ControllerGeneration++
	next.PendingTransitionID = ""
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return RepositoryControllerHead{}, err
	}
	stateRecord, err := s.loadTransitionState(record.TransitionID)
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	stateRecord.Phase = TransitionPhaseFinalized
	stateRecord.UpdatedAt = time.Now().UTC()
	if err := s.writeTransitionState(stateRecord); err != nil {
		return RepositoryControllerHead{}, err
	}
	return next, nil
}

func (s *Store) FailClosed(reason string, transitionID string, workspace WorkspaceIdentity, expected, actual WorkspaceSnapshot, observed map[string]string) (FailureRecord, error) {
	head, err := s.LoadHead()
	if err != nil {
		return FailureRecord{}, err
	}
	failureID, err := state.NewUUID()
	if err != nil {
		return FailureRecord{}, err
	}
	failure := FailureRecord{
		SchemaVersion: controllerSchemaVersion,
		FailureID:     failureID,
		Reason:        reason,
		TransitionID:  transitionID,
		Generation:    head.ControllerGeneration,
		WorkspaceID:   workspace.ID,
		Expected:      expected,
		Actual:        actual,
		Observed:      cloneMap(observed),
		CreatedAt:     time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.failurePath(failureID), failure); err != nil {
		return FailureRecord{}, err
	}
	next := head
	next.ControllerGeneration++
	next.Status = ControllerStatusFailClosed
	next.FailureID = failureID
	next.LiveLeaseID = ""
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return FailureRecord{}, err
	}
	if transitionID != "" {
		stateRecord, loadErr := s.loadTransitionState(transitionID)
		if loadErr == nil {
			stateRecord.Phase = TransitionPhaseFailed
			stateRecord.Observed = cloneMap(observed)
			stateRecord.UpdatedAt = time.Now().UTC()
			_ = s.writeTransitionState(stateRecord)
		}
	}
	return failure, nil
}

func (s *Store) LoadTransition(id string) (TransitionRecord, TransitionState, error) {
	var record TransitionRecord
	if err := readJSON(s.transitionPath(id), &record); err != nil {
		return TransitionRecord{}, TransitionState{}, err
	}
	transitionState, err := s.loadTransitionState(id)
	if err != nil {
		return TransitionRecord{}, TransitionState{}, err
	}
	return record, transitionState, nil
}

func (s *Store) writeHeadCAS(expected uint64, next RepositoryControllerHead) error {
	current, err := s.LoadHead()
	if err != nil {
		return err
	}
	if current.ControllerGeneration != expected {
		return fmt.Errorf("controller generation CAS failed: got=%d want=%d", current.ControllerGeneration, expected)
	}
	if next.SchemaVersion != controllerSchemaVersion || next.RepositoryIdentity != s.identity.LineageID || next.ControllerGeneration <= current.ControllerGeneration {
		return fmt.Errorf("repository controller head replacement is invalid")
	}
	return writeJSONAtomic(s.headPath(), next)
}

func (s *Store) writeAttempt(record AttemptRecord) error {
	return writeJSONAtomic(filepath.Join(s.dir, "attempts", record.AttemptID+".json"), record)
}

func (s *Store) loadAttempt(id string) (AttemptRecord, error) {
	var record AttemptRecord
	if err := readJSON(filepath.Join(s.dir, "attempts", id+".json"), &record); err != nil {
		return AttemptRecord{}, fmt.Errorf("read attempt %s: %w", id, err)
	}
	if record.SchemaVersion != controllerSchemaVersion || record.AttemptID != id {
		return AttemptRecord{}, fmt.Errorf("attempt %s identity is invalid", id)
	}
	return record, nil
}

func (s *Store) writeLease(record ExecutionLease) error {
	return writeJSONAtomic(filepath.Join(s.dir, "leases", record.LeaseID+".json"), record)
}

func (s *Store) loadLease(id string) (ExecutionLease, error) {
	var record ExecutionLease
	if err := readJSON(filepath.Join(s.dir, "leases", id+".json"), &record); err != nil {
		return ExecutionLease{}, fmt.Errorf("read lease %s: %w", id, err)
	}
	if record.SchemaVersion != controllerSchemaVersion || record.LeaseID != id {
		return ExecutionLease{}, fmt.Errorf("lease %s identity is invalid", id)
	}
	return record, nil
}

func (s *Store) writeTransitionState(record TransitionState) error {
	return writeJSONAtomic(s.transitionStatePath(record.TransitionID), record)
}

func (s *Store) loadTransitionState(id string) (TransitionState, error) {
	var record TransitionState
	if err := readJSON(s.transitionStatePath(id), &record); err != nil {
		return TransitionState{}, err
	}
	if record.SchemaVersion != controllerSchemaVersion || record.TransitionID != id {
		return TransitionState{}, fmt.Errorf("transition state %s identity is invalid", id)
	}
	return record, nil
}

func changedSurfaces(before, after WorkspaceSnapshot) []MutationSurface {
	set := map[MutationSurface]bool{}
	if before.WorktreeDigest != after.WorktreeDigest {
		set[MutationSurfaceSource] = true
	}
	if before.IndexDigest != after.IndexDigest {
		set[MutationSurfaceIndex] = true
	}
	if before.Head != after.Head {
		set[MutationSurfaceHead] = true
		set[MutationSurfaceHistory] = true
	}
	if before.RefDigest != after.RefDigest {
		set[MutationSurfaceRef] = true
	}
	result := make([]MutationSurface, 0, len(set))
	for surface := range set {
		result = append(result, surface)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (s *Store) headPath() string {
	return filepath.Join(s.dir, "head.json")
}

func (s *Store) transitionPath(id string) string {
	return filepath.Join(s.dir, "transitions", id+".json")
}

func (s *Store) transitionStatePath(id string) string {
	return filepath.Join(s.dir, "transition-state", id+".json")
}

func (s *Store) mutationPath(id string) string {
	return filepath.Join(s.dir, "mutations", id+".json")
}

func (s *Store) failurePath(id string) string {
	return filepath.Join(s.dir, "failures", id+".json")
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".controller-*.tmp")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	return nil
}
