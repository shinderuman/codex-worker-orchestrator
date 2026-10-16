package controller

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func retiredMetadataHead(head RepositoryControllerHead) bool {
	return head.Status == ControllerStatusActive && head.MetadataLineageRef != nil && head.RootTaskRef != nil &&
		head.ExecutionTaskRef == nil && head.LiveAttemptID == "" && head.LiveLeaseID == "" &&
		head.AcceptedCandidateRef == nil && head.PendingTransitionID == "" && head.ActiveEpisodeID == ""
}

func (s *Store) retiredProjectAuthority(head RepositoryControllerHead) (CommittedTaskAuthority, error) {
	if !retiredMetadataHead(head) {
		return CommittedTaskAuthority{}, fmt.Errorf("ordinary successor requires finalized retired controller authority")
	}
	if err := s.ensureTerminalMetadataFinalized(head); err != nil {
		return CommittedTaskAuthority{}, err
	}
	if err := s.verifyTerminalTaskRecord(*head.MetadataLineageRef); err != nil {
		return CommittedTaskAuthority{}, err
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, head.IntegrationTip)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	if err := validateCompletionSuccessorAuthority(head, authority); err != nil {
		return authority, err
	}
	if !reflect.DeepEqual(authority.Snapshot, project) || !authority.Task.Equal(*head.RootTaskRef) {
		return CommittedTaskAuthority{}, fmt.Errorf("ordinary successor project authority is stale")
	}
	record, err := s.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	if record.ResultProject.SnapshotID != project.SnapshotID {
		if err := s.verifyRetiredMetadataLineage(head); err != nil {
			return CommittedTaskAuthority{}, err
		}
	}
	return authority, nil
}

func (s *Store) verifyRetiredMetadataLineage(head RepositoryControllerHead) error {
	if head.EvidenceLedgerHeadRef == nil {
		return fmt.Errorf("retired metadata advancement lacks published evidence")
	}
	op, evidence, ledger, err := s.retiredMetadataLineageOperation(head)
	if err != nil {
		return err
	}
	transition, phase, err := s.LoadTransition(ledger.TransitionID)
	if err != nil {
		return err
	}
	if !retiredLineageMatchesFinalizedTransition(op, evidence, ledger, transition, phase, head) {
		return fmt.Errorf("retired metadata lineage differs from finalized controller transition")
	}
	digest, err := executionOperationDigest(op)
	if err != nil {
		return err
	}
	if op.Transition.OperationDigest != digest {
		return fmt.Errorf("retired metadata operation content integrity failed")
	}
	return s.validateBaseAdvancement(op)
}

func retiredLineageMatchesFinalizedTransition(op ExecutionOperation, evidence EvidenceHead, ledger EvidenceLedgerRecord, transition TransitionRecord, phase TransitionState, head RepositoryControllerHead) bool {
	return evidence.ProjectSnapshotID == head.ProjectSnapshotID && ledger.ProjectSnapshotID == head.ProjectSnapshotID &&
		reflect.DeepEqual(op.Transition, transition) && phase.Phase == TransitionPhaseFinalized && transition.TargetGeneration <= head.ControllerGeneration &&
		retiredMetadataAdvancement(op) && op.Source.Head.ControllerGeneration < head.ControllerGeneration &&
		op.Publication.Project.SnapshotID == head.ProjectSnapshotID && op.Publication.NewTip == head.IntegrationTip &&
		op.Transition.TargetRootTaskRef.Equal(*head.RootTaskRef) && evidenceRefsEqual(*op.Source.Head.MetadataLineageRef, *head.MetadataLineageRef)
}

func (s *Store) retiredMetadataLineageOperation(head RepositoryControllerHead) (ExecutionOperation, EvidenceHead, EvidenceLedgerRecord, error) {
	_, _, evidence, ledger, err := s.loadPublishedEvidenceAuthority(head)
	if err != nil {
		return ExecutionOperation{}, evidence, ledger, err
	}
	for {
		var op ExecutionOperation
		if err := readJSON(s.executionOperationPath(ledger.TransitionID), &op); err != nil {
			return op, evidence, ledger, err
		}
		if op.Transition.Kind != executionCleanup {
			return op, evidence, ledger, nil
		}
		ledger, evidence, err = s.retiredCleanupLineageStep(head, op, ledger)
		if err != nil {
			return op, evidence, ledger, err
		}
	}
}

func (s *Store) retiredCleanupLineageStep(head RepositoryControllerHead, op ExecutionOperation, ledger EvidenceLedgerRecord) (EvidenceLedgerRecord, EvidenceHead, error) {
	transition, err := s.verifyRetiredCleanupLineageRecord(head, op, ledger)
	if err != nil {
		return ledger, EvidenceHead{}, err
	}
	previous, err := s.LoadEvidenceLedgerRecord(*ledger.PreviousRecord)
	if err != nil {
		return ledger, EvidenceHead{}, err
	}
	prior, _, err := s.LoadTransition(previous.TransitionID)
	if err != nil {
		return ledger, EvidenceHead{}, err
	}
	if previous.Sequence+1 != ledger.Sequence || prior.TargetGeneration != transition.SourceGeneration {
		return ledger, EvidenceHead{}, fmt.Errorf("retired cleanup evidence generation chain differs")
	}
	evidence, err := s.LoadEvidenceHead(previous.EvidenceHeadRef)
	if err != nil {
		return ledger, EvidenceHead{}, err
	}
	return previous, evidence, nil
}

func (s *Store) verifyRetiredCleanupLineageRecord(head RepositoryControllerHead, op ExecutionOperation, ledger EvidenceLedgerRecord) (TransitionRecord, error) {
	transition, phase, err := s.LoadTransition(ledger.TransitionID)
	if err != nil {
		return TransitionRecord{}, err
	}
	if !retiredCleanupLineageMatches(op, transition, phase, ledger, head) {
		return TransitionRecord{}, fmt.Errorf("retired cleanup lineage authority differs")
	}
	digest, err := executionOperationDigest(op)
	if err != nil {
		return TransitionRecord{}, err
	}
	if digest != op.Transition.OperationDigest || len(op.Transition.Effects) != 1 {
		return TransitionRecord{}, fmt.Errorf("retired cleanup operation integrity differs")
	}
	if err := s.validateCleanupOperation(op); err != nil {
		return TransitionRecord{}, err
	}
	ref, err := s.retiredLaneSeal(head, *op.Workspace)
	if err != nil || !evidenceRefsEqual(ref, *op.SealRef) {
		return TransitionRecord{}, fmt.Errorf("retired cleanup seal does not belong to terminal lineage: %w", err)
	}
	return transition, nil
}

func retiredCleanupLineageMatches(op ExecutionOperation, transition TransitionRecord, phase TransitionState, ledger EvidenceLedgerRecord, head RepositoryControllerHead) bool {
	return reflect.DeepEqual(op.Transition, transition) && phase.Phase == TransitionPhaseFinalized &&
		transition.ProjectSnapshotOld == head.ProjectSnapshotID && transition.ProjectSnapshotNew == head.ProjectSnapshotID &&
		transition.TargetGeneration <= head.ControllerGeneration && ledger.ControllerGeneration == transition.CommittedGeneration &&
		ledger.ProjectSnapshotID == head.ProjectSnapshotID && op.CleanupArchive != nil && ledger.PreviousRecord != nil
}

func validateParentMetadataHistory(repo, old, next string) error {
	descendant, err := isPublicationDescendant(repo, old, next)
	if err != nil {
		return err
	}
	if !descendant {
		return fmt.Errorf("parent metadata advancement is not a descendant of integration authority")
	}
	commits, err := gitTrimmed(repo, "rev-list", old+".."+next)
	if err != nil {
		return err
	}
	for _, commit := range strings.Fields(commits) {
		if err := validateParentMetadataCommit(repo, commit); err != nil {
			return err
		}
	}
	return nil
}

func validateParentMetadataCommit(repo, commit string) error {
	data, err := runGitBinary(repo, nil, "diff-tree", "--root", "-m", "--no-commit-id", "--name-only", "-r", "-z", commit)
	if err != nil {
		return err
	}
	for _, path := range strings.Split(string(data), "\x00") {
		if path == "" {
			continue
		}
		if !state.IsParentManagedPath(path) || !strings.HasSuffix(path, ".md") {
			return fmt.Errorf("external advancement changes non-parent metadata: %s", path)
		}
		if err := validateParentMetadataEntry(repo, commit, path); err != nil {
			return err
		}
	}
	return nil
}

func validateParentMetadataEntry(repo, commit, path string) error {
	entry, err := gitTrimmed(repo, "ls-tree", commit, "--", path)
	if err != nil {
		return err
	}
	if entry != "" && !strings.HasPrefix(entry, "100644 blob ") && !strings.HasPrefix(entry, "100755 blob ") {
		return fmt.Errorf("parent metadata advancement contains a non-regular file: %s", path)
	}
	return nil
}

func retiredMetadataAdvancement(op ExecutionOperation) bool {
	return op.Transition.Kind == publicationAdopt && op.Publication != nil && op.Publication.After == nil && op.Publication.Project != nil && retiredMetadataHead(op.Source.Head)
}

func (s *Store) validateRetiredMetadataAdvancement(op ExecutionOperation) error {
	head := op.Source.Head
	if op.Transition.SourceGeneration != head.ControllerGeneration || op.Transition.ProjectSnapshotOld != head.ProjectSnapshotID ||
		op.Publication.OldTip != head.IntegrationTip || head.PublicationPolicy == nil || *head.PublicationPolicy != op.Publication.Policy {
		return fmt.Errorf("retired metadata advancement source authority differs")
	}
	if err := validateParentMetadataHistory(s.identity.PrimaryRoot, op.Publication.OldTip, op.Publication.NewTip); err != nil {
		return err
	}
	if err := validateParentMetadataHistory(s.identity.PrimaryRoot, op.Publication.OldTip, op.Publication.RemoteOID); err != nil {
		return err
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, op.Publication.NewTip)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(authority.Snapshot, *op.Publication.Project) || !authority.Task.Equal(op.Transition.TargetRootTaskRef) || authority.Task.TaskPath != head.RootTaskRef.TaskPath {
		return fmt.Errorf("retired metadata advancement changes selected Task authority")
	}
	return nil
}

func (s *Store) planRetiredMetadataAdvancement(head RepositoryControllerHead, policy PublicationPolicy) (ExecutionOperationResult, error) {
	if _, err := s.retiredProjectAuthority(head); err != nil {
		return ExecutionOperationResult{}, err
	}
	if head.PublicationPolicy == nil || policy != *head.PublicationPolicy {
		return ExecutionOperationResult{}, fmt.Errorf("retired metadata advancement publication policy differs")
	}
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, policy)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.ensurePublicationPrefix(head, remote); err != nil {
		return ExecutionOperationResult{}, err
	}
	tip, err := s.retiredAdvancementTip(head, policy, remote)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := validateParentMetadataHistory(s.identity.PrimaryRoot, head.IntegrationTip, tip); err != nil {
		return ExecutionOperationResult{}, err
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, tip)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op, err := s.retiredAdvancementOperation(head, policy, authority, tip, remote)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.finalizeRetiredAdvancement(head, op, tip)
}

func (s *Store) retiredAdvancementOperation(head RepositoryControllerHead, policy PublicationPolicy, authority CommittedTaskAuthority, tip, remote string) (ExecutionOperation, error) {
	record, err := executionTransition(head, publicationAdopt)
	if err != nil {
		return ExecutionOperation{}, err
	}
	record.ProjectSnapshotNew = authority.ProjectSnapshotID
	record.TargetRootTaskRef = authority.Task
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceState, Resource: "integration-tip", ExpectedOld: head.IntegrationTip, ExpectedNew: tip}}
	if remote != tip {
		record.Effects = append(record.Effects, EffectExpectation{Surface: MutationSurfaceHistory, Resource: policy.Remote + ":" + policy.RemoteRef, ExpectedOld: remote, ExpectedNew: tip})
	}
	op := ExecutionOperation{Transition: record, Source: Admission{Head: head}, Publication: &PublicationOperation{Policy: policy, OldTip: head.IntegrationTip, NewTip: tip, RemoteOID: remote, Project: &authority.Snapshot}}
	if err := s.validateRetiredMetadataAdvancement(op); err != nil {
		return ExecutionOperation{}, err
	}
	return op, nil
}

func (s *Store) finalizeRetiredAdvancement(head RepositoryControllerHead, op ExecutionOperation, tip string) (ExecutionOperationResult, error) {
	archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "external:"+tip, []string{tip})
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op.Publication.AdvancementRef = &archive
	if err := s.planAdvancementLocalRef(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) retiredAdvancementTip(head RepositoryControllerHead, policy PublicationPolicy, remote string) (string, error) {
	tip := remote
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, policy.LocalRef)
	if err != nil || !exists {
		return "", fmt.Errorf("parent metadata local authority is unavailable: %w", err)
	}
	if local != remote && local != head.IntegrationTip {
		if err := validateParentMetadataHistory(s.identity.PrimaryRoot, remote, local); err != nil {
			return "", err
		}
		tip = local
	}
	if tip == head.IntegrationTip {
		return "", fmt.Errorf("configured upstream has no advancement")
	}
	return tip, nil
}
