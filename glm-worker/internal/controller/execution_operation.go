package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ExecutionOperation struct {
	Terminal        *TerminalMetadataOperation `json:"terminal,omitempty"`
	Publication     *PublicationOperation      `json:"publication,omitempty"`
	Transition      TransitionRecord           `json:"transition"`
	Source          Admission                  `json:"source"`
	Suspension      *SuspensionSnapshot        `json:"suspension,omitempty"`
	Episode         *BlockerEpisodeRevision    `json:"episode,omitempty"`
	Rebound         *ReboundSuspension         `json:"rebound,omitempty"`
	Workspace       *WorkspaceIdentity         `json:"workspace,omitempty"`
	Attempt         *AttemptRecord             `json:"attempt,omitempty"`
	Lease           *ExecutionLease            `json:"lease,omitempty"`
	SealRef         *EvidenceObjectRef         `json:"seal_ref,omitempty"`
	CleanupSnapshot *WorkspaceSnapshot         `json:"cleanup_snapshot,omitempty"`
	Evidence        EvidencePublicationInput   `json:"evidence"`
}

type ExecutionOperationResult struct {
	TransitionID string                   `json:"transition_id"`
	Head         RepositoryControllerHead `json:"head"`
	Admission    *Admission               `json:"admission,omitempty"`
	Suspension   *SuspensionSnapshot      `json:"suspension,omitempty"`
	CandidateRef *EvidenceObjectRef       `json:"candidate_ref,omitempty"`
	SealRef      *EvidenceObjectRef       `json:"seal_ref,omitempty"`
}

const (
	executionSuspend     = "SUSPEND_EXECUTION"
	executionMaterialize = "MATERIALIZE_EXECUTION"
	executionCleanup     = "CLEANUP_EXECUTION"
	executionGC          = "GC_SUSPENSION"
)

func (s *Store) SuspendExecution(admission Admission, episodeID string, revision uint64) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, err := s.planExecutionSuspension(admission, episodeID, revision)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, admission.Head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planExecutionSuspension(admission Admission, episodeID string, revision uint64) (ExecutionOperation, error) {
	episode, err := s.LoadEpisodeRevision(episodeID, revision)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if episode.SourceAttemptID != admission.Attempt.AttemptID || episode.SourceLeaseID != admission.Lease.LeaseID || episode.SourceControllerGeneration != admission.Head.ControllerGeneration || episode.ProjectSnapshotID != admission.Head.ProjectSnapshotID {
		return ExecutionOperation{}, fmt.Errorf("blocker suspension episode source is stale")
	}
	if _, err := s.scheduleEpisodeAgainstProject(episode); err != nil {
		return ExecutionOperation{}, err
	}
	snapshot, err := s.captureSuspensionLocked(admission)
	if err != nil {
		return ExecutionOperation{}, err
	}
	id, err := state.NewUUID()
	if err != nil {
		return ExecutionOperation{}, err
	}
	target := authorityFromAdmission(admission, admission.Snapshot)
	target.EpisodeID = episodeID
	target.EpisodeRevision = revision
	target.LeaseID = ""
	refs, err := captureRefs(admission.Workspace.Root)
	if err != nil {
		return ExecutionOperation{}, err
	}
	refs[suspensionRef(snapshot.SnapshotID)] = snapshot.RetainedCommitOID
	target.WorkspaceSnapshot.RefDigest = digestRefs(refs)
	target.WorkspaceSnapshot.ID = workspaceSnapshotID(target.WorkspaceSnapshot)
	record := buildTransitionRecord(TransitionIntent{Kind: executionSuspend, Source: admission, Target: target}, id)
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: suspensionRef(snapshot.SnapshotID), ExpectedNew: snapshot.RetainedCommitOID}}
	op := ExecutionOperation{Transition: record, Source: admission, Suspension: &snapshot, Episode: &episode}
	publication, sealRef, err := s.buildSuspensionEvidence(op)
	if err != nil {
		return ExecutionOperation{}, err
	}
	op.SealRef = &sealRef
	op.Evidence = publication
	return op, nil
}

func (s *Store) prepareExecutionOperation(op *ExecutionOperation, head RepositoryControllerHead) error {
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" || op.Transition.SourceGeneration != head.ControllerGeneration {
		return fmt.Errorf("execution operation source is unavailable")
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "execution-operations"), 0o700); err != nil {
		return err
	}
	digest, err := executionOperationDigest(*op)
	if err != nil {
		return err
	}
	op.Transition.OperationDigest = digest
	if err := writeJSONAtomic(s.executionOperationPath(op.Transition.TransitionID), op); err != nil {
		return err
	}
	return s.persistPreparedTransition(op.Transition, head)
}

func (s *Store) RecoverExecutionOperation(transitionID string) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	var op ExecutionOperation
	if filepath.Base(transitionID) != transitionID || transitionID == "" {
		return ExecutionOperationResult{}, fmt.Errorf("invalid execution transition identity")
	}
	if err := readJSON(s.executionOperationPath(transitionID), &op); err != nil {
		return ExecutionOperationResult{}, err
	}
	record, _, err := s.LoadTransition(transitionID)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if !reflect.DeepEqual(op.Transition, record) {
		return ExecutionOperationResult{}, fmt.Errorf("execution operation journal mismatch")
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) recoverExecutionOperationLocked(op ExecutionOperation) (ExecutionOperationResult, error) {
	if err := s.validateExecutionOperation(op); err != nil {
		return ExecutionOperationResult{}, err
	}
	head, phase, err := s.loadExecutionRecoveryAuthority(op)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if phase.Phase == TransitionPhaseAborted {
		return s.finishPublicationAbort(op, head, phase)
	}
	if executionAlreadyFinalized(op, head) {
		if err := s.verifyCommittedExecutionTarget(op, head); err != nil {
			return ExecutionOperationResult{}, err
		}
		if err := s.reconcileFinalizedExecutionPhase(op.Transition); err != nil {
			return ExecutionOperationResult{}, err
		}
		return s.executionOperationResult(op, head)
	}
	if head.PendingTransitionID != op.Transition.TransitionID {
		return ExecutionOperationResult{}, fmt.Errorf("execution transition no longer owns controller")
	}
	if head.ControllerGeneration == op.Transition.CommittedGeneration {
		return s.finalizeCommittedExecution(op, head)
	}
	if !executionPreparedForRecovery(op, head) {
		return ExecutionOperationResult{}, fmt.Errorf("execution transition generation is unexpected")
	}
	err = s.applyExecutionOperation(op)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	head, err = s.finalizeAuthorityTransitionLocked(op.Transition)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.executionOperationResult(op, head)
}

func (s *Store) finalizeCommittedExecution(op ExecutionOperation, head RepositoryControllerHead) (ExecutionOperationResult, error) {
	if err := s.verifyCommittedExecutionTarget(op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.reconcileCommittedExecutionPhase(op.Transition); err != nil {
		return ExecutionOperationResult{}, err
	}
	next, err := s.finalizeAuthorityTransitionLocked(op.Transition)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.executionOperationResult(op, next)
}

func (s *Store) applyExecutionOperation(op ExecutionOperation) error {
	var err error
	switch op.Transition.Kind {
	case terminalRetire:
		err = s.applyTerminalMetadata(op)
	case executionSuspend:
		err = s.applyExecutionSuspension(op)
	case executionMaterialize:
		err = s.applyExecutionMaterialization(op)
	case executionCleanup:
		err = s.applyExecutionCleanup(op)
	case executionGC:
		err = s.applySuspensionGC(op)
	case publicationAccept, publicationPromote, publicationPublish, publicationRebind, publicationAdopt, publicationRevalidate, publicationReenter:
		err = s.applyPublicationOperation(op)
	default:
		err = fmt.Errorf("unsupported execution operation %q", op.Transition.Kind)
	}
	return err
}

func (s *Store) reconcileCommittedExecutionPhase(record TransitionRecord) error {
	phase, err := s.loadTransitionState(record.TransitionID)
	if err != nil {
		return err
	}
	if phase.Phase == TransitionPhaseCommitted || phase.Phase == TransitionPhaseFinalizing {
		return nil
	}
	if phase.Phase != TransitionPhasePrepared && phase.Phase != TransitionPhaseApplied {
		return fmt.Errorf("execution transition phase is inconsistent with committed controller")
	}
	phase.Phase = TransitionPhaseCommitted
	phase.UpdatedAt = time.Now().UTC()
	return s.writeTransitionState(phase)
}

func (s *Store) executionOperationResult(op ExecutionOperation, head RepositoryControllerHead) (ExecutionOperationResult, error) {
	result := ExecutionOperationResult{TransitionID: op.Transition.TransitionID, Head: head, Suspension: op.Suspension, SealRef: op.SealRef}
	if op.Publication != nil {
		result.CandidateRef = op.Publication.After
	}
	if op.Transition.Kind != executionMaterialize {
		return result, nil
	}
	workspace, err := ResolveWorkspaceIdentity(op.Workspace.Root, s.identity)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	authority, err := MutationAuthorityFromHead(head)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	admission, err := s.AdmitMutation(authority, workspace, snapshot)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	result.Admission = &admission
	return result, nil
}

func (s *Store) applyExecutionSuspension(op ExecutionOperation) error {
	if op.Suspension == nil || op.Episode == nil {
		return fmt.Errorf("suspension operation payload is incomplete")
	}
	if err := s.retainSuspension(op.Source.Workspace.Root, *op.Suspension); err != nil {
		return err
	}
	observed, err := CaptureWorkspaceSnapshot(op.Source.Workspace.Root)
	if err != nil {
		return err
	}
	if observed != op.Transition.WorkspaceSnapshotNew {
		return fmt.Errorf("suspension source workspace changed after prepare")
	}
	if op.SealRef == nil {
		return fmt.Errorf("suspension seal authority is missing")
	}
	actual := map[string]string{op.Transition.Effects[0].Key(): op.Suspension.RetainedCommitOID}
	_, _, err = s.commitAuthorityTransitionWithEvidenceLocked(op.Transition, actual, op.Evidence, func(next *RepositoryControllerHead) error {
		attempt := op.Source.Attempt
		attempt.AttemptState = AttemptStateSuspendedForBlocker
		attempt.AttemptSealID = op.SealRef.LogicalIdentity
		if err := s.writeAttempt(attempt); err != nil {
			return err
		}
		next.LiveLeaseID = ""
		next.LiveAttemptID = ""
		next.ActiveEpisodeID = op.Episode.EpisodeID
		next.ActiveEpisodeRevision = op.Episode.Revision
		if next.IntegrationTip == "" {
			next.IntegrationTip = op.Source.Attempt.ExecutionBaseOID
		}
		return nil
	})
	return err
}

func (s *Store) executionOperationPath(id string) string {
	return filepath.Join(s.dir, "execution-operations", id+".json")
}

func executionTransition(head RepositoryControllerHead, kind string) (TransitionRecord, error) {
	id, err := state.NewUUID()
	if err != nil {
		return TransitionRecord{}, err
	}
	return TransitionRecord{SchemaVersion: controllerSchemaVersion, TransitionID: id, Kind: kind, SourceGeneration: head.ControllerGeneration, PreparedGeneration: head.ControllerGeneration + 1, CommittedGeneration: head.ControllerGeneration + 2, TargetGeneration: head.ControllerGeneration + 3, ProjectSnapshotOld: head.ProjectSnapshotID, ProjectSnapshotNew: head.ProjectSnapshotID, SourceEpisodeID: head.ActiveEpisodeID, TargetEpisodeID: head.ActiveEpisodeID, SourceEpisodeRevision: head.ActiveEpisodeRevision, TargetEpisodeRevision: head.ActiveEpisodeRevision, CreatedAt: time.Now().UTC()}, nil
}

func executionAlreadyFinalized(op ExecutionOperation, head RepositoryControllerHead) bool {
	return head.PendingTransitionID == "" && head.ControllerGeneration == op.Transition.TargetGeneration
}

func executionPreparedForRecovery(op ExecutionOperation, head RepositoryControllerHead) bool {
	return head.Status == ControllerStatusActive && head.ControllerGeneration == op.Transition.PreparedGeneration
}

func (s *Store) loadExecutionRecoveryAuthority(op ExecutionOperation) (RepositoryControllerHead, TransitionState, error) {
	head, err := s.LoadHead()
	if err != nil {
		return head, TransitionState{}, err
	}
	if head.Status != ControllerStatusActive {
		return head, TransitionState{}, fmt.Errorf("execution recovery controller failed closed")
	}
	if op.Terminal != nil && head.ControllerGeneration == op.Transition.PreparedGeneration {
		if err := validateTerminalRecoverySource(op, head); err != nil {
			return head, TransitionState{}, err
		}
	}
	phase, err := s.loadTransitionState(op.Transition.TransitionID)
	return head, phase, err
}

func (s *Store) reconcileFinalizedExecutionPhase(record TransitionRecord) error {
	phase, err := s.loadTransitionState(record.TransitionID)
	if err != nil {
		return err
	}
	if phase.Phase == TransitionPhaseFinalized {
		return nil
	}
	if phase.Phase != TransitionPhaseCommitted && phase.Phase != TransitionPhaseFinalizing {
		return fmt.Errorf("finalized controller disagrees with transition phase")
	}
	phase.Phase = TransitionPhaseFinalized
	phase.UpdatedAt = time.Now().UTC()
	return s.writeTransitionState(phase)
}
