package controller

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ParentOnlyAcceptanceProof struct {
	SchemaVersion   int               `json:"schema_version"`
	Mode            string            `json:"mode"`
	TaskPath        string            `json:"task_path"`
	ContractDigest  string            `json:"contract_digest"`
	AttemptID       string            `json:"attempt_id"`
	BaseOID         string            `json:"base_oid"`
	SourceSnapshot  WorkspaceSnapshot `json:"source_snapshot"`
	ModelExecution  string            `json:"model_execution"`
	ReviewAuthority string            `json:"review_authority"`
	AcceptedAt      time.Time         `json:"accepted_at"`
}

const (
	parentOnlyModelExecutionNone   = "none"
	parentOnlyReviewAuthorityValue = "parent-decision"
)

func (s *Store) parentCompletionReview(admission Admission) (*state.StateStore, *state.ParentCompletionOutcome, state.ModelCallLog, error) {
	runtime, taskID, err := s.boundAttemptRuntime(admission)
	if err != nil {
		return nil, nil, state.ModelCallLog{}, err
	}
	if runtime == nil || runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		return nil, nil, state.ModelCallLog{}, fmt.Errorf("parent completion requires an accepted bound Runtime")
	}
	if err := validateCompletionRuntimeAuthority(runtime); err != nil {
		return nil, nil, state.ModelCallLog{}, err
	}
	outcome, err := runtime.CurrentParentCompletionOutcome()
	if err != nil {
		return nil, nil, state.ModelCallLog{}, err
	}
	if outcome == nil || outcome.Terminal != state.SessionRotationTerminalAccept || outcome.Source == nil {
		return nil, nil, state.ModelCallLog{}, fmt.Errorf("parent completion lacks accepted source evidence")
	}
	current, err := state.CaptureGitSnapshot(admission.Workspace.Root)
	if err != nil {
		return nil, nil, state.ModelCallLog{}, err
	}
	source := outcome.Source
	if !completionSourceMatches(*source, current) {
		return nil, nil, state.ModelCallLog{}, fmt.Errorf("parent completion source differs from the accepted source")
	}
	reviewer, err := independentCompletionReviewer(runtime, taskID)
	return runtime, outcome, reviewer, err
}

func independentCompletionReviewer(runtime *state.StateStore, taskID string) (state.ModelCallLog, error) {
	calls, err := runtime.ReadModelCallLogs(taskID)
	if err != nil {
		return state.ModelCallLog{}, err
	}
	var reviewer *state.ModelCallLog
	for i := range calls {
		call := &calls[i]
		if call.Role == state.WorkerRole {
			reviewer = nil
		}
		if call.Role == state.ReviewerRole && call.ReadOnly && call.Outcome == "success" && (call.PacketStatus == "PASS" || call.PacketStatus == "NEEDS_SOL_REVIEW") {
			reviewer = call
		}
	}
	if reviewer == nil {
		return state.ModelCallLog{}, fmt.Errorf("parent completion lacks an independent successful reviewer after the last worker")
	}
	return *reviewer, nil
}

func (s *Store) AcceptParentCompletion(admission Admission, input CandidateAcceptanceInput) (ExecutionOperationResult, error) {
	if admission.Attempt.ExecutionAuthority == ExecutionAuthorityParentOnly {
		return s.acceptParentOnlyCompletion(admission, input)
	}
	_, outcome, reviewer, err := s.parentCompletionReview(admission)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if len(input.Evidence) != 0 {
		return ExecutionOperationResult{}, fmt.Errorf("parent completion receives proof from the Runtime, not caller evidence JSON")
	}
	tree, err := s.PreviewCandidateTree(admission, input.Policy)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	required, requiredProbes, err := s.resolveAdoptedValidations(admission, input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	reviewProof := struct {
		Acceptance             *state.ParentCompletionOutcome `json:"parent_acceptance"`
		ReviewerCallID         string                         `json:"reviewer_call_id"`
		ReviewerSessionID      string                         `json:"reviewer_session_id"`
		ReviewerModel          string                         `json:"reviewer_model"`
		ReviewerPacketStatus   string                         `json:"reviewer_packet_status"`
		ReviewerResponseSHA256 string                         `json:"reviewer_response_sha256"`
	}{outcome, reviewer.CallID, reviewer.SessionID, reviewer.ModelAlias, reviewer.PacketStatus, reviewer.ResponseSHA256}
	proofs := []struct {
		kind, result string
		artifact     any
	}{
		{candidateEvidenceKindReview, candidateEvidencePass, reviewProof},
		{candidateEvidenceKindValidationObligation, candidateEvidenceDeclared, CandidateValidationObligation{Required: required, Probes: requiredProbes, NotRequired: len(required) == 0 && len(requiredProbes) == 0}},
	}
	if input.Policy.RequireInstall {
		proofs = append(proofs, struct {
			kind, result string
			artifact     any
		}{candidateEvidenceKindInstallObligation, candidateEvidenceDeclared, CandidateInstallObligation{Required: true}})
	}
	return s.storeCompletionProofs(admission, input, tree, proofs)
}

func (s *Store) acceptParentOnlyCompletion(admission Admission, input CandidateAcceptanceInput) (ExecutionOperationResult, error) {
	source, err := s.parentOnlyCompletionReview(admission)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if len(input.Evidence) != 0 {
		return ExecutionOperationResult{}, fmt.Errorf("parent completion receives proof from the Controller, not caller evidence JSON")
	}
	tree, err := s.PreviewCandidateTree(admission, input.Policy)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	required, requiredProbes, err := s.resolveAdoptedValidations(admission, input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	acceptance := ParentOnlyAcceptanceProof{SchemaVersion: controllerSchemaVersion, Mode: ExecutionAuthorityParentOnly, TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, ContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest, AttemptID: admission.Attempt.AttemptID, BaseOID: admission.Head.IntegrationTip, SourceSnapshot: source, ModelExecution: parentOnlyModelExecutionNone, ReviewAuthority: parentOnlyReviewAuthorityValue, AcceptedAt: time.Now().UTC()}
	proofs := []struct {
		kind, result string
		artifact     any
	}{
		{candidateEvidenceKindParentAccept, candidateEvidenceAccepted, acceptance},
		{candidateEvidenceKindValidationObligation, candidateEvidenceDeclared, CandidateValidationObligation{Required: required, Probes: requiredProbes, NotRequired: len(required) == 0 && len(requiredProbes) == 0}},
	}
	if input.Policy.RequireInstall {
		proofs = append(proofs, struct {
			kind, result string
			artifact     any
		}{candidateEvidenceKindInstallObligation, candidateEvidenceDeclared, CandidateInstallObligation{Required: true}})
	}
	return s.storeCompletionProofs(admission, input, tree, proofs)
}

func (s *Store) storeCompletionProofs(admission Admission, input CandidateAcceptanceInput, tree string, proofs []struct {
	kind, result string
	artifact     any
}) (ExecutionOperationResult, error) {
	for _, proof := range proofs {
		artifact, err := json.Marshal(proof.artifact)
		if err != nil {
			return ExecutionOperationResult{}, err
		}
		ref, err := s.StoreCandidateEvidenceJSON(CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: s.identity.LineageID, AttemptID: admission.Attempt.AttemptID, SnapshotID: admission.Snapshot.ID, BaseOID: admission.Head.IntegrationTip, TreeOID: tree, Kind: proof.kind, Result: proof.result}, artifact)
		if err != nil {
			return ExecutionOperationResult{}, err
		}
		input.Evidence = append(input.Evidence, ref)
	}
	return s.AcceptExecutionCandidate(admission, input)
}

func (s *Store) parentOnlyCompletionReview(admission Admission) (WorkspaceSnapshot, error) {
	if admission.Attempt.ExecutionAuthority != ExecutionAuthorityParentOnly {
		return WorkspaceSnapshot{}, fmt.Errorf("parent-only completion requires an attempt with parent-only execution authority")
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if authority.ExecutionMode != ExecutionAuthorityParentOnly {
		return WorkspaceSnapshot{}, fmt.Errorf("committed task is no longer designated for parent-only execution")
	}
	if !authority.Task.Equal(admission.Attempt.SemanticTaskRef) {
		return WorkspaceSnapshot{}, fmt.Errorf("committed task contract changed during parent-only execution")
	}
	if authority.Snapshot.HeadOID != admission.Attempt.ExecutionBaseOID {
		return WorkspaceSnapshot{}, fmt.Errorf("committed repository HEAD moved during parent-only execution")
	}
	runtime, _, err := s.boundAttemptRuntime(admission)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if runtime != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("parent-only completion requires an execution without a Runtime")
	}
	workspace, err := CaptureWorkspaceSnapshot(admission.Workspace.Root)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if workspace != admission.Snapshot {
		return WorkspaceSnapshot{}, fmt.Errorf("parent-only execution source changed after admission")
	}
	return workspace, nil
}

func (s *Store) validateParentAcceptanceArtifact(candidate AcceptedCandidate, ref EvidenceObjectRef) error {
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return err
	}
	var proof ParentOnlyAcceptanceProof
	if err := json.Unmarshal(data, &proof); err != nil {
		return err
	}
	if !parentAcceptanceProofShapeValid(proof) {
		return fmt.Errorf("parent acceptance proof is not an explicit parent-only acceptance without model execution")
	}
	if !parentAcceptanceProofBindsCandidate(proof, candidate) {
		return fmt.Errorf("parent acceptance proof binds a different candidate source")
	}
	attempt, err := s.loadAttempt(candidate.AttemptID)
	if err != nil {
		return err
	}
	if attempt.ExecutionAuthority != ExecutionAuthorityParentOnly {
		return fmt.Errorf("parent acceptance proof binds an attempt without parent-only execution authority")
	}
	return nil
}

func parentAcceptanceProofShapeValid(proof ParentOnlyAcceptanceProof) bool {
	return proof.SchemaVersion == controllerSchemaVersion && proof.Mode == ExecutionAuthorityParentOnly && proof.ModelExecution == parentOnlyModelExecutionNone && proof.ReviewAuthority == parentOnlyReviewAuthorityValue && !proof.AcceptedAt.IsZero() && proof.SourceSnapshot.ID == workspaceSnapshotID(proof.SourceSnapshot)
}

func parentAcceptanceProofBindsCandidate(proof ParentOnlyAcceptanceProof, candidate AcceptedCandidate) bool {
	return proof.AttemptID == candidate.AttemptID && proof.BaseOID == candidate.BaseOID && proof.TaskPath == candidate.TaskRef.TaskPath && proof.ContractDigest == candidate.TaskRef.ContractDigest && proof.SourceSnapshot.ID == candidate.SnapshotID && proof.SourceSnapshot.Head == candidate.BaseOID
}

func completionSourceMatches(source state.SnapshotDigest, current state.GitSnapshot) bool {
	return source.Head == current.Head && source.IndexDigest == current.IndexDigest && source.WorktreeDigestExcludingParent != "" && source.WorktreeDigestExcludingParent == current.WorktreeDigestExcludingParent
}
