package controller

import (
	"encoding/json"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
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
	required, err := s.inheritRequiredValidations(admission, input.RequiredValidationForms)
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
		{candidateEvidenceKindValidationObligation, candidateEvidenceDeclared, CandidateValidationObligation{Required: required, NotRequired: len(required) == 0}},
	}
	if input.Policy.RequireInstall {
		proofs = append(proofs, struct {
			kind, result string
			artifact     any
		}{candidateEvidenceKindInstallObligation, candidateEvidenceDeclared, CandidateInstallObligation{Required: true}})
	}
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

func completionSourceMatches(source state.SnapshotDigest, current state.GitSnapshot) bool {
	return source.Head == current.Head && source.IndexDigest == current.IndexDigest && source.WorktreeDigestExcludingParent != "" && source.WorktreeDigestExcludingParent == current.WorktreeDigestExcludingParent
}
