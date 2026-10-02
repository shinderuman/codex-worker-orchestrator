package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) PreviewCandidateTree(admission Admission, policy PublicationPolicy) (string, error) {
	if err := validatePublicationPolicy(s.identity.PrimaryRoot, policy); err != nil {
		return "", err
	}
	if _, err := s.AdmitMutation(admission.MutationAuthority(), admission.Workspace, admission.Snapshot); err != nil {
		return "", err
	}
	trees, err := CaptureExecutionTrees(admission.Workspace.Root, admission.Attempt.ExecutionBaseOID)
	if err != nil {
		return "", err
	}
	base, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", admission.Head.IntegrationTip+"^{tree}")
	if err != nil {
		return "", err
	}
	return mergeSuspensionTrees(s.identity.PrimaryRoot, admission.Attempt.BaselineTrees.WorktreeTree, trees.WorktreeTree, base)
}

func (s *Store) AcceptExecutionCandidate(admission Admission, input CandidateAcceptanceInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, err := s.planCandidateAcceptance(admission, input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, admission.Head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planCandidateAcceptance(admission Admission, input CandidateAcceptanceInput) (ExecutionOperation, error) {
	if strings.TrimSpace(input.Message) == "" || admission.Lease.InFlightCallID != "" {
		return ExecutionOperation{}, fmt.Errorf("candidate acceptance requires message and quiescent execution")
	}
	if err := s.validateUnresolvedAttemptFindings(admission); err != nil {
		return ExecutionOperation{}, err
	}
	tree, err := s.PreviewCandidateTree(admission, input.Policy)
	if err != nil {
		return ExecutionOperation{}, err
	}
	captured, err := s.captureSuspensionLocked(admission)
	if err != nil {
		return ExecutionOperation{}, err
	}
	record, err := executionTransition(admission.Head, publicationAccept)
	if err != nil {
		return ExecutionOperation{}, err
	}
	record.SourceAttemptID = admission.Attempt.AttemptID
	record.SourceLeaseID = admission.Lease.LeaseID
	record.SourceWorkspaceID = admission.Workspace.ID
	record.TargetExecutionTaskRef = admission.Attempt.SemanticTaskRef
	record.TargetRootTaskRef = admission.Attempt.RootTaskRef
	commit, err := createAcceptedCandidateCommit(s.identity.PrimaryRoot, tree, admission.Head.IntegrationTip, input.Message)
	if err != nil {
		return ExecutionOperation{}, err
	}
	candidate := AcceptedCandidate{SchemaVersion: controllerSchemaVersion, AttemptID: admission.Attempt.AttemptID, TaskRef: admission.Attempt.SemanticTaskRef, RootTaskRef: admission.Attempt.RootTaskRef, EpisodeID: admission.Head.ActiveEpisodeID, EpisodeRevision: admission.Head.ActiveEpisodeRevision, ProjectSnapshotID: admission.Head.ProjectSnapshotID, ControllerGeneration: record.CommittedGeneration, TransitionID: record.TransitionID, BaseOID: admission.Head.IntegrationTip, CommitOID: commit, TreeOID: tree, SnapshotID: admission.Snapshot.ID, Message: input.Message, State: candidatePrepared, EvidenceValid: true, Evidence: input.Evidence, Policy: input.Policy, CreatedAt: record.CreatedAt}
	if err := s.validateCandidateEvidence(candidate, input.Evidence); err != nil {
		return ExecutionOperation{}, err
	}
	op := ExecutionOperation{Transition: record, Source: admission, Suspension: &captured, Publication: &PublicationOperation{Policy: input.Policy, OldTip: admission.Head.IntegrationTip, NewTip: admission.Head.IntegrationTip}}
	seal, err := s.buildAcceptedCandidateSeal(op, candidate)
	if err != nil {
		return ExecutionOperation{}, err
	}
	candidate.SealRef = seal
	candidate.GitArchive, _, err = s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "candidate:"+commit, []string{commit})
	if err != nil {
		return ExecutionOperation{}, err
	}
	ref, err := s.storeAcceptedCandidate(candidate)
	if err != nil {
		return ExecutionOperation{}, err
	}
	op.SealRef = &seal
	op.Publication.After = &ref
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: candidateRetentionRef(commit), ExpectedNew: commit}, {Surface: MutationSurfaceRef, Resource: suspensionRef(captured.SnapshotID), ExpectedNew: captured.RetainedCommitOID}}
	return op, nil
}

func createAcceptedCandidateCommit(repo, tree, base, message string) (string, error) {
	data, err := runGitBinary(repo, []byte(message), "-c", "user.name=Repository Controller", "-c", "user.email=controller@invalid", "commit-tree", tree, "-p", base)
	return strings.TrimSpace(string(data)), err
}

func candidateRetentionRef(commit string) string { return "refs/glm-worker/candidates/" + commit }

func (s *Store) validateUnresolvedAttemptFindings(admission Admission) error {
	entries, err := os.ReadDir(filepath.Join(s.dir, "findings"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		finding, err := s.LoadFinding(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return err
		}
		if finding.SourceAttemptID != admission.Attempt.AttemptID {
			continue
		}
		disposition, err := s.LoadFindingDisposition(finding.CanonicalFindingID)
		if err != nil {
			return fmt.Errorf("candidate has unresolved finding: %w", err)
		}
		if disposition.Kind == FindingDispositionIndependentBlocking {
			return fmt.Errorf("candidate has unresolved blocker finding")
		}
	}
	return nil
}
