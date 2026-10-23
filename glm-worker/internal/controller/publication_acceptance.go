package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojectcommit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecthead"
)

func (s *Store) PreviewCandidateTree(admission Admission, policy PublicationPolicy) (string, error) {
	implementationTree, err := s.previewImplementationTree(admission, policy)
	if err != nil {
		return "", err
	}
	_, finalTree, err := s.finalizeCandidateTree(admission, implementationTree)
	return finalTree, err
}

func (s *Store) previewImplementationTree(admission Admission, policy PublicationPolicy) (string, error) {
	if err := validatePublicationPolicy(s.identity.PrimaryRoot, policy); err != nil {
		return "", err
	}
	if _, err := s.AdmitMutation(admission.MutationAuthority(), admission.Workspace, admission.Snapshot); err != nil {
		return "", err
	}
	metadataBase, err := s.projectMetadataBase(admission.Head.ProjectSnapshotID)
	if err != nil {
		return "", err
	}
	trees, err := CaptureExecutionTrees(admission.Workspace.Root, metadataBase)
	if err != nil {
		return "", err
	}
	base, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", admission.Head.IntegrationTip+"^{tree}")
	if err != nil {
		return "", err
	}
	index, err := newSuspensionIndex(s.identity.PrimaryRoot, base)
	if err != nil {
		return "", err
	}
	base, err = normalizeCandidateTree(s.identity.PrimaryRoot, index, metadataBase)
	if err != nil {
		return "", err
	}
	return mergeSuspensionTrees(s.identity.PrimaryRoot, admission.Attempt.BaselineTrees.WorktreeTree, trees.WorktreeTree, base)
}

func (s *Store) finalizeCandidateTree(admission Admission, implementationTree string) ([]TerminalMetadataFile, string, error) {
	return s.finalizeCandidateTrees(admission.Attempt.SemanticTaskRef, admission.Attempt.RootTaskRef, admission.Head.IntegrationTip, implementationTree)
}

func (s *Store) finalizeCandidateTrees(task, root SemanticTaskRef, base string, implementationTree string) ([]TerminalMetadataFile, string, error) {
	implementation, err := createAcceptedCandidateCommit(s.identity.PrimaryRoot, implementationTree, base, "terminal finalization source")
	if err != nil {
		return nil, "", err
	}
	project, err := repositoryprojectcommit.Load(s.identity.PrimaryRoot, implementation)
	if err != nil {
		return nil, "", err
	}
	metadata, err := project.RetireTerminalMetadata(task.TaskPath, !task.Equal(root))
	if err != nil {
		return nil, "", err
	}
	files, tree, err := s.terminalMetadataTree(implementation, metadata)
	if err != nil {
		return nil, "", err
	}
	final, err := createAcceptedCandidateCommit(s.identity.PrimaryRoot, tree, base, "terminal metadata preflight")
	if err != nil {
		return nil, "", err
	}
	if _, err := repositoryprojecthead.CheckCommitPlan(s.identity.PrimaryRoot, final); err != nil {
		return nil, "", fmt.Errorf("terminal candidate Plan preflight: %w", err)
	}
	return files, tree, nil
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
	requiredValidations, err := s.inheritRequiredValidations(admission, input.RequiredValidationForms)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if err := s.validateUnresolvedAttemptFindings(admission); err != nil {
		return ExecutionOperation{}, err
	}
	tree, err := s.previewImplementationTree(admission, input.Policy)
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
	candidate, err := s.buildAcceptedCandidate(admission, input, record, tree, requiredValidations)
	if err != nil {
		return ExecutionOperation{}, err
	}
	return s.assembleCandidateAcceptance(admission, input, record, captured, candidate)
}

func (s *Store) buildAcceptedCandidate(admission Admission, input CandidateAcceptanceInput, record TransitionRecord, implementationTree string, requiredValidations []FinalValidationRequest) (AcceptedCandidate, error) {
	files, finalTree, err := s.finalizeCandidateTree(admission, implementationTree)
	if err != nil {
		return AcceptedCandidate{}, err
	}
	if len(files) == 0 {
		return AcceptedCandidate{}, fmt.Errorf("candidate terminal finalization produced no metadata changes")
	}
	commit, err := createAcceptedCandidateCommit(s.identity.PrimaryRoot, finalTree, admission.Head.IntegrationTip, input.Message)
	if err != nil {
		return AcceptedCandidate{}, err
	}
	candidate := AcceptedCandidate{SchemaVersion: controllerSchemaVersion, AttemptID: admission.Attempt.AttemptID, TaskRef: admission.Attempt.SemanticTaskRef, RootTaskRef: admission.Attempt.RootTaskRef, EpisodeID: admission.Head.ActiveEpisodeID, EpisodeRevision: admission.Head.ActiveEpisodeRevision, ProjectSnapshotID: admission.Head.ProjectSnapshotID, ControllerGeneration: record.CommittedGeneration, TransitionID: record.TransitionID, BaseOID: admission.Head.IntegrationTip, CommitOID: commit, TreeOID: finalTree, ImplementationTree: implementationTree, SnapshotID: admission.Snapshot.ID, Message: input.Message, State: candidatePrepared, TerminalFiles: files, RequiredValidations: requiredValidations, EvidenceValid: true, Evidence: input.Evidence, Policy: input.Policy, CreatedAt: record.CreatedAt}
	if err := s.validateCandidateEvidence(candidate, input.Evidence); err != nil {
		return AcceptedCandidate{}, err
	}
	return candidate, nil
}

func (s *Store) assembleCandidateAcceptance(admission Admission, input CandidateAcceptanceInput, record TransitionRecord, captured SuspensionSnapshot, candidate AcceptedCandidate) (ExecutionOperation, error) {
	op := ExecutionOperation{Transition: record, Source: admission, Suspension: &captured, Publication: &PublicationOperation{Policy: input.Policy, OldTip: admission.Head.IntegrationTip, NewTip: admission.Head.IntegrationTip}}
	seal, err := s.buildAcceptedCandidateSeal(op, candidate)
	if err != nil {
		return ExecutionOperation{}, err
	}
	candidate.SealRef = seal
	candidate.GitArchive, _, err = s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "candidate:"+candidate.CommitOID, []string{candidate.CommitOID})
	if err != nil {
		return ExecutionOperation{}, err
	}
	ref, err := s.storeAcceptedCandidate(candidate)
	if err != nil {
		return ExecutionOperation{}, err
	}
	op.SealRef = &seal
	op.Publication.After = &ref
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: candidateRetentionRef(candidate.CommitOID), ExpectedNew: candidate.CommitOID}, {Surface: MutationSurfaceRef, Resource: suspensionRef(captured.SnapshotID), ExpectedNew: captured.RetainedCommitOID}}
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
