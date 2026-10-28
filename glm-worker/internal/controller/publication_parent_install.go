package controller

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type InstalledRuntimeIdentity struct {
	SourceOID    string `json:"source_oid"`
	BinaryPath   string `json:"binary_path"`
	BinarySHA256 string `json:"binary_sha256"`
}

type ParentCompletionInstallProof struct {
	CommitOID  string                   `json:"commit_oid"`
	TreeOID    string                   `json:"tree_oid"`
	Installed  InstalledRuntimeIdentity `json:"installed"`
	InstallLog string                   `json:"install_log"`
	Smoke      qualitygate.RunRecord    `json:"smoke"`
	SmokeLog   string                   `json:"smoke_log"`
}

var errCandidateInstallPending = errors.New("candidate requires actual installation and smoke evidence before promotion")

func (s *Store) requireCompletedCandidateInstall(candidate AcceptedCandidate) error {
	if !candidate.Policy.RequireInstall {
		return nil
	}
	for _, ref := range candidate.Evidence {
		kind, err := s.validateCandidateEvidenceRecord(candidate, ref)
		if err != nil {
			return err
		}
		if kind == candidateEvidenceKindInstall {
			if len(candidate.RequiredSmokeProbes) != 0 {
				if _, err := s.PostInstallSmokeDeployment(candidate); err != nil {
					return errors.Join(errCandidateInstallPending, err)
				}
			}
			return nil
		}
	}
	return errCandidateInstallPending
}

func (s *Store) CandidateInstallWorkspace(input PublicationInput) (AcceptedCandidate, string, func() error, error) {
	_, candidate, _, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return candidate, "", nil, err
	}
	if candidate.State != candidatePrepared || !candidate.EvidenceValid || !candidate.Policy.RequireInstall {
		return candidate, "", nil, fmt.Errorf("candidate installation requires a prepared obligated source")
	}
	root, err := s.finalValidationWorkspace(candidate, FinalValidationRequest{Form: "install"})
	if err == nil {
		err = s.ensureFinalValidationCheckout(root, candidate)
	}
	if err != nil {
		return candidate, "", nil, err
	}
	return candidate, root, func() error { return s.releaseFinalValidationCheckout(root, candidate.TaskRef) }, nil
}

func (s *Store) RecordParentCompletionInstall(input PublicationInput, root, smokeID string, installed InstalledRuntimeIdentity, installLog []byte) (ExecutionOperationResult, error) {
	_, candidate, _, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.validateInstalledCandidateSource(candidate, root, installed); err != nil {
		return ExecutionOperationResult{}, err
	}
	smoke, smokeLog, err := s.readCandidateInstallSmoke(candidate, root, smokeID)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	proof, err := json.Marshal(ParentCompletionInstallProof{CommitOID: candidate.CommitOID, TreeOID: candidate.TreeOID, Installed: installed, InstallLog: string(installLog), Smoke: smoke, SmokeLog: string(smokeLog)})
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	ref, err := s.StoreCandidateEvidenceJSON(CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: s.identity.LineageID, AttemptID: candidate.AttemptID, SnapshotID: candidate.SnapshotID, CandidateID: candidate.CandidateID, BaseOID: candidate.BaseOID, TreeOID: candidate.TreeOID, Kind: candidateEvidenceKindInstall, Result: candidateEvidencePass}, proof)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.RevalidateAcceptedCandidate(input, append(append([]EvidenceObjectRef(nil), candidate.Evidence...), ref))
}

func (s *Store) validateInstalledCandidateSource(candidate AcceptedCandidate, root string, installed InstalledRuntimeIdentity) error {
	expectedRoot, err := s.finalValidationWorkspace(candidate, FinalValidationRequest{Form: "install"})
	if err != nil {
		return err
	}
	if filepath.Clean(root) != filepath.Clean(expectedRoot) {
		return fmt.Errorf("install proof names a different candidate checkout")
	}
	if err := s.verifyFinalValidationCheckout(root, candidate); err != nil {
		return err
	}
	if installed.SourceOID != candidate.CommitOID || installed.BinaryPath == "" || len(installed.BinarySHA256) != 64 {
		return fmt.Errorf("installed runtime identity differs from the final candidate")
	}
	if _, err := hex.DecodeString(installed.BinarySHA256); err != nil {
		return err
	}
	return nil
}

func (s *Store) readCandidateInstallSmoke(candidate AcceptedCandidate, root, smokeID string) (qualitygate.RunRecord, []byte, error) {
	attempt, err := s.loadAttempt(candidate.AttemptID)
	if err != nil {
		return qualitygate.RunRecord{}, nil, err
	}
	runtime, taskID, err := s.boundAttemptRuntime(Admission{Attempt: attempt})
	if err != nil {
		return qualitygate.RunRecord{}, nil, err
	}
	if runtime == nil || !qualitygate.ValidRunID(smokeID) {
		return qualitygate.RunRecord{}, nil, fmt.Errorf("candidate install smoke identity is unavailable")
	}
	data, err := os.ReadFile(runtime.Path(filepath.Join("install-smoke-runs", smokeID, qualitygate.RunFile)))
	if err != nil {
		return qualitygate.RunRecord{}, nil, err
	}
	var smoke qualitygate.RunRecord
	if err := json.Unmarshal(data, &smoke); err != nil {
		return qualitygate.RunRecord{}, nil, err
	}
	if !candidateInstallSmokeMatches(smoke, smokeID, taskID, root) {
		return qualitygate.RunRecord{}, nil, fmt.Errorf("candidate install has no successful source-bound smoke")
	}
	if smoke.Log != filepath.ToSlash(filepath.Join("install-smoke-runs", smokeID, "smoke.log")) {
		return qualitygate.RunRecord{}, nil, fmt.Errorf("candidate smoke log locator is invalid")
	}
	smokeLog, err := os.ReadFile(runtime.Path(filepath.FromSlash(smoke.Log)))
	if err != nil {
		return qualitygate.RunRecord{}, nil, err
	}
	return smoke, smokeLog, nil
}

func candidateInstallSmokeMatches(smoke qualitygate.RunRecord, runID, taskID, root string) bool {
	return smoke.ValidationRunID == runID && smoke.TaskID == taskID && smoke.Form == "install-smoke" && smoke.Status == "pass" && smoke.ExitCode == 0 && smoke.ExitSource == state.ValidationExitSourceTarget && smoke.CompletedAt != nil && filepath.Clean(smoke.Repository) == filepath.Clean(root)
}
