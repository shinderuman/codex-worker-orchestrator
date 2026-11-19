package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type SmokeProbeEvidence struct {
	ProbeID         string `json:"probe_id"`
	ValidationRunID string `json:"validation_run_id"`
}

type SmokeProbeDeployment struct {
	EvidenceDigest string
	SourceOID      string
	BinaryPath     string
	BinarySHA256   string
}

const smokeProbeForm = "post-install-smoke"

var smokeProbeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func normalizeSmokeProbes(probes []SmokeProbeDeclaration) ([]SmokeProbeDeclaration, error) {
	if len(probes) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(probes))
	normalized := make([]SmokeProbeDeclaration, 0, len(probes))
	for _, probe := range probes {
		if err := validateSmokeProbeDeclaration(seen, probe); err != nil {
			return nil, err
		}
		seen[probe.ProbeID] = true
		normalized = append(normalized, probe)
	}
	return normalized, nil
}

func validateSmokeProbeDeclaration(seen map[string]bool, probe SmokeProbeDeclaration) error {
	if !smokeProbeIDPattern.MatchString(probe.ProbeID) {
		return fmt.Errorf("post-install smoke probe id %q is not a bounded stable identifier", probe.ProbeID)
	}
	if seen[probe.ProbeID] {
		return fmt.Errorf("post-install smoke probe %s is declared twice", probe.ProbeID)
	}
	if strings.TrimSpace(probe.Verifies) == "" || len(probe.Verifies) > 512 {
		return fmt.Errorf("post-install smoke probe %s must state what it verifies", probe.ProbeID)
	}
	if len(probe.Argv) == 0 || len(probe.Argv) > 32 {
		return fmt.Errorf("post-install smoke probe %s must declare a bounded argv", probe.ProbeID)
	}
	return validateSmokeProbeArgv(probe)
}

func validateSmokeProbeArgv(probe SmokeProbeDeclaration) error {
	for _, arg := range probe.Argv {
		if arg == "" || len(arg) > 1024 {
			return fmt.Errorf("post-install smoke probe %s declares an invalid argv entry", probe.ProbeID)
		}
	}
	return nil
}

func SmokeProbeArgvDigest(probe SmokeProbeDeclaration) string {
	return digestStrings(append([]string{"post-install-smoke-argv"}, probe.Argv...)...)
}

func (s *Store) NewSmokeProbeRunRecord(c AcceptedCandidate, probe SmokeProbeDeclaration, root, taskID string) (qualitygate.RunRecord, error) {
	deployment, err := s.PostInstallSmokeDeployment(c)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	runID, err := qualitygate.NewRunID()
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	absent, err := qualitygate.NoIgnoredInputs(root)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	return qualitygate.RunRecord{
		ValidationRunID:               runID,
		Form:                          smokeProbeForm,
		Repository:                    root,
		WorkingDir:                    root,
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
		IgnoredInputsAbsent:           &absent,
		TaskID:                        taskID,
		ProbeID:                       probe.ProbeID,
		ArgvDigest:                    SmokeProbeArgvDigest(probe),
		TaskAuthorityDigest:           c.TaskRef.ContractDigest,
		InstallEvidenceDigest:         deployment.EvidenceDigest,
		StartedAt:                     time.Now().UTC(),
		Status:                        qualitygate.StatusRunning,
	}, nil
}

func (s *Store) VerifyPostInstallSmokeSource(c AcceptedCandidate, root string) error {
	if err := s.verifyFinalValidationCheckout(root, c); err != nil {
		return err
	}
	_, err := s.PostInstallSmokeDeployment(c)
	return err
}

func (s *Store) smokeProbeDeployment(c AcceptedCandidate) (SmokeProbeDeployment, error) {
	var ref *EvidenceObjectRef
	for i := range c.Evidence {
		kind, err := s.validateCandidateEvidenceRecord(c, c.Evidence[i])
		if err != nil {
			return SmokeProbeDeployment{}, err
		}
		if kind == candidateEvidenceKindInstall {
			ref = &c.Evidence[i]
		}
	}
	if ref == nil {
		return SmokeProbeDeployment{}, fmt.Errorf("candidate has no sealed verification deployment")
	}
	return s.smokeProbeDeploymentProof(c, *ref)
}

func (s *Store) smokeProbeDeploymentProof(c AcceptedCandidate, ref EvidenceObjectRef) (SmokeProbeDeployment, error) {
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return SmokeProbeDeployment{}, err
	}
	var evidence CandidateEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return SmokeProbeDeployment{}, err
	}
	artifact, err := s.LoadEvidenceObject(evidence.Artifact)
	if err != nil {
		return SmokeProbeDeployment{}, err
	}
	var proof ParentCompletionInstallProof
	if err := json.Unmarshal(artifact, &proof); err != nil {
		return SmokeProbeDeployment{}, fmt.Errorf("sealed verification deployment is unreadable: %w", err)
	}
	if proof.Installed.SourceOID != c.CommitOID || proof.Installed.BinaryPath == "" || len(proof.Installed.BinarySHA256) != 64 {
		return SmokeProbeDeployment{}, fmt.Errorf("sealed verification deployment identity differs from the candidate")
	}
	return SmokeProbeDeployment{EvidenceDigest: ref.Digest, SourceOID: proof.Installed.SourceOID, BinaryPath: proof.Installed.BinaryPath, BinarySHA256: proof.Installed.BinarySHA256}, nil
}

func (s *Store) PostInstallSmokeDeployment(c AcceptedCandidate) (SmokeProbeDeployment, error) {
	deployment, err := s.smokeProbeDeployment(c)
	if err != nil {
		return deployment, err
	}
	data, err := os.ReadFile(deployment.BinaryPath)
	if err != nil {
		return deployment, fmt.Errorf("verification deployment runtime is unavailable: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != deployment.BinarySHA256 {
		return deployment, fmt.Errorf("verification deployment runtime no longer matches the sealed install identity")
	}
	return deployment, nil
}

func (s *Store) CandidatePostInstallSmokeWorkspace(input PublicationInput) (AcceptedCandidate, string, func() error, error) {
	_, candidate, _, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return candidate, "", nil, err
	}
	if err := s.requireCurrentCandidateSource(candidate); err != nil {
		return candidate, "", nil, err
	}
	if candidate.State != candidatePrepared || !candidate.EvidenceValid {
		return candidate, "", nil, fmt.Errorf("post-install smoke requires a prepared obligated source")
	}
	if err := s.requireCompletedCandidateInstall(candidate); err != nil {
		return candidate, "", nil, err
	}
	if _, err := s.PostInstallSmokeDeployment(candidate); err != nil {
		return candidate, "", nil, err
	}
	root, err := s.smokeProbeWorkspace(candidate)
	if err != nil {
		return candidate, "", nil, err
	}
	return candidate, root, func() error { return s.releaseSmokeProbeCheckout(root, candidate.TaskRef) }, nil
}

func (s *Store) InterruptedPostInstallSmokeWorkspace(candidate AcceptedCandidate) (bool, error) {
	root, err := s.smokeProbeWorkspace(candidate)
	if err != nil {
		return false, err
	}
	source, err := s.finalValidationSource(candidate)
	if err != nil {
		return false, err
	}
	deployment, err := s.PostInstallSmokeDeployment(candidate)
	if err != nil {
		return false, err
	}
	module, err := s.finalValidationRunStore(candidate.TaskRef)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	inventory, err := readPublicationRunInventory(module)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, probe := range candidate.RequiredSmokeProbes {
		record, found, err := inventory.latestSmokeProbe(root, probe.ProbeID)
		if err != nil {
			return false, err
		}
		if found && record.Status == qualitygate.StatusInterrupted && smokeProbeMatchesSource(record, root, source, candidate, probe, deployment) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) PreparePostInstallSmokeCheckout(candidate AcceptedCandidate, interrupted bool) error {
	root, err := s.smokeProbeWorkspace(candidate)
	if err != nil {
		return err
	}
	if interrupted {
		if err := s.recoverPostInstallSmokeCheckout(root, candidate); err != nil {
			return err
		}
	}
	return s.ensureFinalValidationCheckout(root, candidate)
}

func (s *Store) recoverPostInstallSmokeCheckout(root string, candidate AcceptedCandidate) error {
	if _, err := os.Lstat(root); err == nil {
		identity, err := ResolveRepositoryIdentity(root)
		if err != nil {
			return err
		}
		snapshot, err := state.CaptureGitSnapshot(root)
		if err != nil {
			return err
		}
		if identity.LineageID != s.identity.LineageID || snapshot.Head != candidate.CommitOID {
			return fmt.Errorf("interrupted smoke checkout belongs to a different repository or candidate")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return s.releaseSmokeProbeCheckout(root, candidate.TaskRef)
}

func (s *Store) releaseSmokeProbeCheckout(root string, task SemanticTaskRef) error {
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	}
	cfg := WorkflowTaskConfig(s.config, s.identity, task)
	if err := finalValidationCheckoutQuiescent(state.AttachStateStore(cfg), root); err != nil {
		return err
	}
	_, err := runGitBinary(s.identity.PrimaryRoot, nil, "worktree", "remove", "--force", root)
	return err
}

func (s *Store) smokeProbeWorkspace(c AcceptedCandidate) (string, error) {
	return s.finalValidationWorkspace(c, FinalValidationRequest{Form: smokeProbeForm})
}

func (s *Store) OutstandingSmokeProbes(c AcceptedCandidate) ([]SmokeProbeDeclaration, error) {
	return s.outstandingSmokeProbes(c)
}

func (s *Store) outstandingSmokeProbes(c AcceptedCandidate) ([]SmokeProbeDeclaration, error) {
	return s.outstandingSmokeProbesFromInventory(c, nil)
}

func (s *Store) outstandingSmokeProbesFromInventory(c AcceptedCandidate, inventory *publicationRunInventory) ([]SmokeProbeDeclaration, error) {
	probes, err := normalizeSmokeProbes(c.RequiredSmokeProbes)
	if err != nil || len(probes) == 0 {
		return nil, err
	}
	source, err := s.finalValidationSource(c)
	if err != nil {
		return nil, err
	}
	if inventory == nil {
		inventory, err = s.publicationRunInventoryFor(c)
		if errors.Is(err, os.ErrNotExist) {
			return probes, nil
		}
		if err != nil {
			return nil, err
		}
	}
	if inventory.module == nil {
		return probes, nil
	}
	repoRoot, err := s.smokeProbeWorkspace(c)
	if err != nil {
		return nil, err
	}
	deployment, err := s.PostInstallSmokeDeployment(c)
	if err != nil {
		return nil, err
	}
	return outstandingObligations(probes, func(probe SmokeProbeDeclaration) (bool, error) {
		return smokeProbeSatisfied(inventory, repoRoot, source, c, probe, deployment)
	})
}

func outstandingObligations[T any](requests []T, satisfied func(T) (bool, error)) ([]T, error) {
	outstanding := make([]T, 0, len(requests))
	for _, request := range requests {
		ready, err := satisfied(request)
		if err != nil {
			return nil, err
		}
		if !ready {
			outstanding = append(outstanding, request)
		}
	}
	return outstanding, nil
}

func smokeProbeSatisfied(inventory *publicationRunInventory, repoRoot string, source finalValidationSource, c AcceptedCandidate, probe SmokeProbeDeclaration, deployment SmokeProbeDeployment) (bool, error) {
	record, found, err := inventory.latestSmokeProbe(repoRoot, probe.ProbeID)
	if err != nil || !found {
		return false, err
	}
	if !smokeProbeMatchesSource(record, repoRoot, source, c, probe, deployment) {
		return false, nil
	}
	return verifyTerminalSmokeProbePass(inventory.module, record) == nil, nil
}

func (s *Store) collectSmokeProbeEvidence(c AcceptedCandidate) ([]SmokeProbeEvidence, error) {
	return s.collectSmokeProbeEvidenceFromInventory(c, nil)
}

func (s *Store) collectSmokeProbeEvidenceFromInventory(c AcceptedCandidate, inventory *publicationRunInventory) ([]SmokeProbeEvidence, error) {
	probes, err := normalizeSmokeProbes(c.RequiredSmokeProbes)
	if err != nil || len(probes) == 0 {
		return nil, err
	}
	source, err := s.finalValidationSource(c)
	if err != nil {
		return nil, err
	}
	repoRoot, err := s.smokeProbeWorkspace(c)
	if err != nil {
		return nil, err
	}
	deployment, err := s.PostInstallSmokeDeployment(c)
	if err != nil {
		return nil, err
	}
	if inventory == nil {
		inventory, err = s.publicationRunInventoryFor(c)
		if err != nil {
			return nil, err
		}
	}
	evidence := make([]SmokeProbeEvidence, 0, len(probes))
	for _, probe := range probes {
		record, found, err := inventory.latestSmokeProbe(repoRoot, probe.ProbeID)
		if err != nil {
			return nil, err
		}
		if !found || !smokeProbeMatchesSource(record, repoRoot, source, c, probe, deployment) {
			return nil, fmt.Errorf("post-install smoke probe %s has no terminal PASS run for the current candidate source and verification deployment", probe.ProbeID)
		}
		if err := verifyTerminalSmokeProbePass(inventory.module, record); err != nil {
			return nil, fmt.Errorf("post-install smoke probe %s has no complete PASS evidence: %w", probe.ProbeID, err)
		}
		evidence = append(evidence, SmokeProbeEvidence{ProbeID: probe.ProbeID, ValidationRunID: record.ValidationRunID})
	}
	return evidence, nil
}

func (s *Store) verifySmokeProbeEvidence(c AcceptedCandidate, recorded []SmokeProbeEvidence) error {
	return s.verifySmokeProbeEvidenceFromInventory(c, recorded, nil)
}

func (s *Store) verifySmokeProbeEvidenceFromInventory(c AcceptedCandidate, recorded []SmokeProbeEvidence, inventory *publicationRunInventory) error {
	probes, err := normalizeSmokeProbes(c.RequiredSmokeProbes)
	if err != nil {
		return err
	}
	byProbe, err := smokeProbeRecordShape(probes, recorded)
	if err != nil {
		return err
	}
	return s.verifySmokeProbeRuns(c, probes, byProbe, inventory)
}

func smokeProbeRecordShape(probes []SmokeProbeDeclaration, recorded []SmokeProbeEvidence) (map[string]string, error) {
	if len(probes) == 0 {
		if len(recorded) != 0 {
			return nil, fmt.Errorf("completion readiness carries post-install smoke evidence for an unobligated candidate")
		}
		return nil, nil
	}
	if len(recorded) != len(probes) {
		return nil, fmt.Errorf("completion readiness post-install smoke evidence covers %d of %d required probes", len(recorded), len(probes))
	}
	byProbe := make(map[string]string, len(recorded))
	for _, item := range recorded {
		if _, duplicate := byProbe[item.ProbeID]; duplicate || item.ValidationRunID == "" {
			return nil, fmt.Errorf("completion readiness post-install smoke evidence is not one run per required probe")
		}
		byProbe[item.ProbeID] = item.ValidationRunID
	}
	for _, probe := range probes {
		if _, ok := byProbe[probe.ProbeID]; !ok {
			return nil, fmt.Errorf("completion readiness lacks post-install smoke evidence for probe %s", probe.ProbeID)
		}
	}
	return byProbe, nil
}

func (s *Store) verifySmokeProbeRuns(c AcceptedCandidate, probes []SmokeProbeDeclaration, byProbe map[string]string, inventory *publicationRunInventory) error {
	if len(probes) == 0 {
		return nil
	}
	source, err := s.finalValidationSource(c)
	if err != nil {
		return err
	}
	repoRoot, err := s.smokeProbeWorkspace(c)
	if err != nil {
		return err
	}
	deployment, err := s.PostInstallSmokeDeployment(c)
	if err != nil {
		return err
	}
	if inventory == nil {
		inventory, err = s.publicationRunInventoryFor(c)
		if err != nil {
			return err
		}
	}
	for _, probe := range probes {
		runID := byProbe[probe.ProbeID]
		record, err := inventory.record(runID)
		if err != nil {
			return fmt.Errorf("post-install smoke probe %s run %s is unreadable: %w", probe.ProbeID, runID, err)
		}
		if err := verifySmokeProbeRun(inventory, record, repoRoot, source, c, probe, deployment); err != nil {
			return err
		}
	}
	return nil
}

func verifySmokeProbeRun(inventory *publicationRunInventory, record qualitygate.RunRecord, repoRoot string, source finalValidationSource, c AcceptedCandidate, probe SmokeProbeDeclaration, deployment SmokeProbeDeployment) error {
	runID := record.ValidationRunID
	if record.Form != smokeProbeForm || !smokeProbeMatchesSource(record, repoRoot, source, c, probe, deployment) {
		return fmt.Errorf("post-install smoke probe %s run %s does not match the current candidate source, declaration, and verification deployment", probe.ProbeID, runID)
	}
	if err := verifyTerminalSmokeProbePass(inventory.module, record); err != nil {
		return fmt.Errorf("post-install smoke probe %s run %s has no complete PASS evidence: %w", probe.ProbeID, runID, err)
	}
	latest, found, err := inventory.latestSmokeProbe(repoRoot, probe.ProbeID)
	if err != nil {
		return err
	}
	if !found || latest.ValidationRunID != runID {
		return fmt.Errorf("post-install smoke probe %s has a newer run; saved readiness is stale", probe.ProbeID)
	}
	return nil
}

func latestSmokeProbeRun(module *state.StateStore, repoRoot, probeID string) (qualitygate.RunRecord, bool, error) {
	inventory, err := readPublicationRunInventory(module)
	if errors.Is(err, os.ErrNotExist) {
		return qualitygate.RunRecord{}, false, nil
	}
	if err != nil {
		return qualitygate.RunRecord{}, false, err
	}
	return inventory.latestSmokeProbe(repoRoot, probeID)
}

func verifyTerminalSmokeProbePass(module *state.StateStore, record qualitygate.RunRecord) error {
	taskID, err := module.TaskID()
	if err != nil {
		return fmt.Errorf("read post-install smoke probe runtime Task ID: %w", err)
	}
	if record.TaskID != taskID {
		return fmt.Errorf("post-install smoke probe run %s runtime Task ID differs from the task store", record.ValidationRunID)
	}
	if err := qualitygate.VerifyTerminalPass(record); err != nil {
		return err
	}
	return verifySmokeProbeLog(module, record)
}

func verifySmokeProbeLog(module *state.StateStore, record qualitygate.RunRecord) error {
	if record.LogSHA256 == "" {
		return fmt.Errorf("post-install smoke probe run %s has no log SHA256", record.ValidationRunID)
	}
	expected := module.Path(filepath.Join(qualitygate.RunDirectory, record.ValidationRunID, "probe.log"))
	if record.Log != expected {
		return fmt.Errorf("post-install smoke probe run %s has an unexpected log locator", record.ValidationRunID)
	}
	data, _, err := readValidationRunFile(module, record.ValidationRunID, "probe.log")
	if err != nil {
		return fmt.Errorf("post-install smoke probe run %s has unreadable log: %w", record.ValidationRunID, err)
	}
	if digestBytes(data) != record.LogSHA256 {
		return fmt.Errorf("post-install smoke probe run %s log SHA256 differs from the persisted record", record.ValidationRunID)
	}
	return nil
}

func smokeProbeMatchesSource(record qualitygate.RunRecord, repoRoot string, source finalValidationSource, c AcceptedCandidate, probe SmokeProbeDeclaration, deployment SmokeProbeDeployment) bool {
	return filepath.Clean(record.Repository) == filepath.Clean(repoRoot) &&
		record.IgnoredInputsAbsent != nil && *record.IgnoredInputsAbsent &&
		record.Head == source.CommitOID &&
		record.IndexDigest == source.IndexDigest &&
		record.WorktreeDigest == source.CleanWorktreeDig &&
		record.WorktreeDigestExcludingParent == source.CleanWorktreeDig &&
		record.ProbeID == probe.ProbeID &&
		record.ArgvDigest == SmokeProbeArgvDigest(probe) &&
		record.TaskAuthorityDigest == c.TaskRef.ContractDigest &&
		record.InstallEvidenceDigest != "" &&
		record.InstallEvidenceDigest == deployment.EvidenceDigest
}
