package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func dockerRoutingProbe() SmokeProbeDeclaration {
	return SmokeProbeDeclaration{
		ProbeID:  "docker-worker-path",
		Verifies: "the installed worker runs Go tests through Docker and refuses the Mac native path",
		Argv:     []string{"/bin/sh", "-c", "exit 0"},
	}
}

func storeInstallObligationEvidence(t *testing.T, store *Store, c AcceptedCandidate) EvidenceObjectRef {
	t.Helper()
	artifact, err := json.Marshal(CandidateInstallObligation{Required: true})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.StoreCandidateEvidenceJSON(CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: store.identity.LineageID, AttemptID: c.AttemptID, SnapshotID: c.SnapshotID, BaseOID: c.BaseOID, TreeOID: c.TreeOID, Kind: candidateEvidenceKindInstallObligation, Result: candidateEvidenceDeclared}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func sealSmokeProbeInstallProof(t *testing.T, store *Store, c AcceptedCandidate, generation uint64, payload string) (AcceptedCandidate, ExecutionOperationResult) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "deployed-worker")
	data := []byte(payload)
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	artifact, err := json.Marshal(ParentCompletionInstallProof{CommitOID: c.CommitOID, TreeOID: c.TreeOID, Installed: InstalledRuntimeIdentity{SourceOID: c.CommitOID, BinaryPath: binary, BinarySHA256: hex.EncodeToString(sum[:])}, InstallLog: "fixture verification deployment"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.StoreCandidateEvidenceJSON(CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: store.identity.LineageID, AttemptID: c.AttemptID, SnapshotID: c.SnapshotID, CandidateID: c.CandidateID, BaseOID: c.BaseOID, TreeOID: c.TreeOID, Kind: candidateEvidenceKindInstall, Result: candidateEvidencePass}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	revalidated, err := store.RevalidateAcceptedCandidate(PublicationInput{ExpectedGeneration: generation, CandidateID: c.CandidateID}, append(c.Evidence, ref))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := store.LoadAcceptedCandidate(*revalidated.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return sealed, revalidated
}

func acceptProbedCandidate(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, source Admission, probes []SmokeProbeDeclaration) (ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	seedFinalValidationNoneObligation(t, fixture.store, source)
	view := publicationTestCandidateView(t, fixture.store, source, policy)
	obligation := storeInstallObligationEvidence(t, fixture.store, view)
	refs := append(storePublicationTestEvidence(t, fixture.store, view), obligation)
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "probed result", Policy: policy,
		Evidence: refs,
		RequiredValidations: &RequiredValidationDeclaration{
			Forms:  []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}},
			Probes: probes,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	sealed, result := sealSmokeProbeInstallProof(t, fixture.store, candidate, accepted.Head.ControllerGeneration, "deployed runtime for "+candidate.CommitOID)
	return result, sealed
}

func seedSmokeProbeRun(t *testing.T, store *Store, c AcceptedCandidate, probe SmokeProbeDeclaration, status string, mutate func(*qualitygate.RunRecord)) qualitygate.RunRecord {
	t.Helper()
	root, err := store.smokeProbeWorkspace(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ensureFinalValidationCheckout(root, c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.releaseSmokeProbeCheckout(root, c.TaskRef) })
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil || head.ExecutionTaskRef == nil {
		t.Fatalf("probe Task unavailable: %v", err)
	}
	cfg := WorkflowTaskConfig(store.config, store.identity, *head.ExecutionTaskRef)
	module := state.AttachStateStore(cfg)
	runID, err := qualitygate.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	absent, err := qualitygate.NoIgnoredInputs(root)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := store.smokeProbeDeployment(c)
	if err != nil {
		t.Fatal(err)
	}
	record := qualitygate.RunRecord{
		ValidationRunID:               runID,
		Form:                          smokeProbeForm,
		Repository:                    root,
		WorkingDir:                    root,
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
		IgnoredInputsAbsent:           &absent,
		ProbeID:                       probe.ProbeID,
		ArgvDigest:                    SmokeProbeArgvDigest(probe),
		TaskAuthorityDigest:           c.TaskRef.ContractDigest,
		InstallEvidenceDigest:         deployment.EvidenceDigest,
		StartedAt:                     time.Now().UTC().Add(-time.Minute),
		Status:                        status,
	}
	if status == qualitygate.StatusPass {
		completed := time.Now().UTC()
		record.CompletedAt = &completed
		record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
		record.ExitCode = 0
		record.ExitSource = state.ValidationExitSourceTarget
	}
	logPath := filepath.Join(module.Path(qualitygate.RunDirectory), runID, "probe.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("post-install smoke probe output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record.Log = logPath
	if mutate != nil {
		mutate(&record)
	}
	data, err := qualitygate.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Write(qualitygate.RunRelativePath(runID), string(data)); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestPostInstallSmokeProbesBlockEveryCompletionEntry(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "probed.txt", "docker routed implementation\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	accepted, candidate := acceptProbedCandidate(t, fixture, policy, source, []SmokeProbeDeclaration{dockerRoutingProbe()})
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)

	operation, err := fixture.store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if operation.Operation != PublicationOperationPostInstallSmoke || len(operation.SmokeProbeIDs) != 1 || operation.SmokeProbeIDs[0] != "docker-worker-path" {
		t.Fatalf("install smoke PASS must not satisfy the declared probe; next operation = %#v", operation)
	}
	if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil || !strings.Contains(err.Error(), "outstanding post-install smoke probes: docker-worker-path") {
		t.Fatalf("promotion must refuse while the declared probe is open: %v", err)
	}
	runtime := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.identity, source.Attempt.SemanticTaskRef))
	if runtime.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("refused promotion completed the Runtime task")
	}
	remote, remoteErr := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if remoteErr != nil || remote != source.Head.IntegrationTip {
		t.Fatalf("refused completion still moved the remote: remote=%s error=%v", remote, remoteErr)
	}
}

func TestPostInstallSmokeProbeUnmetStatesStayOutstanding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status string
		mutate func(*qualitygate.RunRecord)
	}{
		{name: "running", status: qualitygate.StatusRunning},
		{name: "fail", status: qualitygate.StatusFail},
		{name: "interrupted", status: qualitygate.StatusInterrupted},
		{name: "source-mismatch", status: qualitygate.StatusPass, mutate: func(record *qualitygate.RunRecord) {
			record.Head = strings.Repeat("0", 40)
		}},
		{name: "stale-argv", status: qualitygate.StatusPass, mutate: func(record *qualitygate.RunRecord) {
			record.ArgvDigest = strings.Repeat("a", 64)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, policy := newPublicationTestFixture(t)
			policy.RequireInstall = true
			source := publicationTestEdit(t, fixture, "unmet.txt", "unmet probe implementation\n")
			accepted, candidate := acceptProbedCandidate(t, fixture, policy, source, []SmokeProbeDeclaration{dockerRoutingProbe()})
			seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
			seedSmokeProbeRun(t, fixture.store, candidate, dockerRoutingProbe(), tc.status, tc.mutate)
			probes, err := fixture.store.OutstandingSmokeProbes(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if len(probes) != 1 || probes[0].ProbeID != "docker-worker-path" {
				t.Fatalf("%s must not satisfy the probe: %#v", tc.name, probes)
			}
			if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil || !strings.Contains(err.Error(), "outstanding post-install smoke probes") {
				t.Fatalf("%s must block promotion: %v", tc.name, err)
			}
		})
	}
}

func TestPostInstallSmokeProbePassClosesCompletion(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "closed.txt", "probe satisfied implementation\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	accepted, candidate := acceptProbedCandidate(t, fixture, policy, source, []SmokeProbeDeclaration{dockerRoutingProbe()})
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	seedSmokeProbeRun(t, fixture.store, candidate, dockerRoutingProbe(), qualitygate.StatusPass, nil)
	runtime, err := publishFinalValidationCompletion(t, fixture, accepted, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("closed probe completion left status %s", runtime.TaskStatus())
	}
}

func TestPostInstallSmokeProbesRequireDeploymentPolicy(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "undeployed.txt", "probe without deployment\n")
	seedFinalValidationNoneObligation(t, fixture.store, source)
	if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "no deployment", Policy: policy,
		Evidence: publicationTestEvidence(t, fixture.store, source, policy),
		RequiredValidations: &RequiredValidationDeclaration{
			Forms:  []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}},
			Probes: []SmokeProbeDeclaration{dockerRoutingProbe()},
		},
	}); err == nil || !strings.Contains(err.Error(), "require verification deployment policy") {
		t.Fatalf("probes without an install policy must be rejected: %v", err)
	}
}

func TestPostInstallSmokeProbeObligationIsSealed(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "sealed.txt", "sealed probe declaration\n")
	seedFinalValidationNoneObligation(t, fixture.store, source)
	tree, err := fixture.store.PreviewCandidateTree(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := json.Marshal(CandidateValidationObligation{Required: []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}}, NotRequired: false})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := fixture.store.StoreCandidateEvidenceJSON(CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: fixture.store.identity.LineageID, AttemptID: source.Attempt.AttemptID, SnapshotID: source.Snapshot.ID, BaseOID: source.Head.IntegrationTip, TreeOID: tree, Kind: candidateEvidenceKindValidationObligation, Result: candidateEvidenceDeclared}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	proofs := publicationTestEvidence(t, fixture.store, source, policy)
	if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "unsealed probes", Policy: policy,
		Evidence:            []EvidenceObjectRef{proofs[0], ref},
		RequiredValidations: &RequiredValidationDeclaration{Forms: []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}}, Probes: []SmokeProbeDeclaration{dockerRoutingProbe()}},
	}); err == nil || !strings.Contains(err.Error(), "differs from the declared post-install smoke probes") {
		t.Fatalf("an obligation that drops the declared probes must be rejected: %v", err)
	}
}

func TestPostInstallSmokeWorkspaceRequiresSealedInstall(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "uninstalled.txt", "probe before deployment\n")
	seedFinalValidationNoneObligation(t, fixture.store, source)
	view := publicationTestCandidateView(t, fixture.store, source, policy)
	refs := append(storePublicationTestEvidence(t, fixture.store, view), storeInstallObligationEvidence(t, fixture.store, view))
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "pending deployment", Policy: policy,
		Evidence: refs,
		RequiredValidations: &RequiredValidationDeclaration{
			Forms:  []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}},
			Probes: []SmokeProbeDeclaration{dockerRoutingProbe()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := fixture.store.CandidatePostInstallSmokeWorkspace(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil || !strings.Contains(err.Error(), "requires actual installation") {
		t.Fatalf("probe execution must require the completed verification deployment: %v", err)
	}
}

func TestPostInstallSmokeDeploymentRevisionReopensProbeObligation(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "redeployed.txt", "redeployed implementation\n")
	_, candidate := acceptProbedCandidate(t, fixture, policy, source, []SmokeProbeDeclaration{dockerRoutingProbe()})
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	seedSmokeProbeRun(t, fixture.store, candidate, dockerRoutingProbe(), qualitygate.StatusPass, nil)
	probes, err := fixture.store.OutstandingSmokeProbes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 0 {
		t.Fatalf("deployment-bound probe PASS left obligations open: %#v", probes)
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	revised, resealed := sealSmokeProbeInstallProof(t, fixture.store, candidate, head.ControllerGeneration, "replacement deployed runtime for "+candidate.CommitOID)
	reopened, err := fixture.store.OutstandingSmokeProbes(revised)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened) != 1 || reopened[0].ProbeID != "docker-worker-path" {
		t.Fatalf("a revised verification deployment must reopen the probe obligation: %#v", reopened)
	}
	if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: resealed.Head.ControllerGeneration, CandidateID: revised.CandidateID}); err == nil || !strings.Contains(err.Error(), "outstanding post-install smoke probes: docker-worker-path") {
		t.Fatalf("promotion must refuse a probe PASS bound to a superseded deployment: %v", err)
	}
}
