package parentactioncmd

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type finalizationEvidenceValidationProbe struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Status          string `json:"status"`
	Head            string `json:"head"`
	IndexDigest     string `json:"index_digest"`
	WorktreeDigest  string `json:"worktree_digest"`
}

type finalizationEvidenceHandoffProbe struct {
	Validations []finalizationEvidenceValidationProbe `json:"validations"`
}

func clearManagedFinalizationEvidence(repoRoot string) *finalizationFailure {
	st, managed, failure := managedFinalizationState(repoRoot)
	if failure != nil || !managed {
		return failure
	}
	if err := st.ClearFinalizationEvidence(); err != nil {
		return &finalizationFailure{Stage: "finalization_evidence", Reason: "clear_failed", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	return nil
}

func persistManagedFinalizationEvidence(repoRoot string, output finalizationCheckOutput) *finalizationFailure {
	st, managed, failure := managedFinalizationState(repoRoot)
	if failure != nil || !managed {
		return failure
	}
	validationRunID, snapshotID, failure := finalizationEvidenceIdentity(output)
	if failure != nil {
		return failure
	}
	taskID, err := st.TaskID()
	if err != nil {
		return &finalizationFailure{Stage: "finalization_evidence", Reason: "task_identity_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	if err := st.SaveFinalizationEvidence(state.NewFinalizationEvidence(taskID, output.Form, validationRunID, snapshotID)); err != nil {
		return &finalizationFailure{Stage: "finalization_evidence", Reason: "persist_failed", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	return nil
}

func finalizationEvidenceIdentity(output finalizationCheckOutput) (string, string, *finalizationFailure) {
	var validation finalizationValidationProbe
	if err := json.Unmarshal(output.Validation, &validation); err != nil || validation.ValidationRunID == "" {
		return "", "", &finalizationFailure{Stage: "finalization_evidence", Reason: "validation_identity_unavailable", Detail: compactFinalizationDiagnostic(string(output.Validation))}
	}
	var handoff finalizationEvidenceHandoffProbe
	if err := json.Unmarshal(output.Handoff, &handoff); err != nil {
		return "", "", &finalizationFailure{Stage: "finalization_evidence", Reason: "handoff_identity_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	for _, candidate := range handoff.Validations {
		if candidate.ValidationRunID != validation.ValidationRunID || candidate.Form != output.Form || candidate.Status != finalizationValidationStatusPass {
			continue
		}
		snapshotID := state.ValidationSnapshotID(candidate.Head, candidate.IndexDigest, candidate.WorktreeDigest)
		if snapshotID != "" {
			return validation.ValidationRunID, snapshotID, nil
		}
	}
	return "", "", &finalizationFailure{Stage: "finalization_evidence", Reason: "snapshot_identity_unavailable"}
}

func managedFinalizationState(repoRoot string) (*state.StateStore, bool, *finalizationFailure) {
	cfg, err := config.Load()
	if err != nil {
		return nil, false, &finalizationFailure{Stage: "finalization_evidence", Reason: "state_config_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	configuredRoot, err := filepath.EvalSymlinks(cfg.RepoRoot)
	if err != nil {
		return nil, false, &finalizationFailure{Stage: "finalization_evidence", Reason: "repository_identity_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	requestedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return nil, false, &finalizationFailure{Stage: "finalization_evidence", Reason: "repository_identity_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	if filepath.Clean(configuredRoot) != filepath.Clean(requestedRoot) {
		return nil, false, nil
	}
	st := state.AttachStateStore(cfg)
	storedRoot, err := st.Read("repo-root")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, &finalizationFailure{Stage: "finalization_evidence", Reason: "state_repository_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	storedResolved, err := filepath.EvalSymlinks(storedRoot)
	if err != nil || filepath.Clean(storedResolved) != filepath.Clean(requestedRoot) {
		return nil, false, &finalizationFailure{Stage: "finalization_evidence", Reason: "state_repository_mismatch", Detail: compactFinalizationDiagnostic(storedRoot)}
	}
	return st, true, nil
}
