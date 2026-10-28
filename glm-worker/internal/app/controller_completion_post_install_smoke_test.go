package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func declaredNoValidations() *controller.RequiredValidationDeclaration {
	return &controller.RequiredValidationDeclaration{}
}

func sealAppInstallProof(t *testing.T, store *controller.Store, c controller.AcceptedCandidate, generation uint64, payload string) (controller.AcceptedCandidate, controller.ExecutionOperationResult) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "deployed-worker")
	data := []byte("#!/bin/sh\nprintf '%s\\n' '" + payload + "'\n")
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	artifact, err := json.Marshal(controller.ParentCompletionInstallProof{CommitOID: c.CommitOID, TreeOID: c.TreeOID, Installed: controller.InstalledRuntimeIdentity{SourceOID: c.CommitOID, BinaryPath: binary, BinarySHA256: hex.EncodeToString(sum[:])}, InstallLog: "fixture verification deployment"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.StoreCandidateEvidenceJSON(controller.CandidateEvidence{SchemaVersion: 1, RepositoryIdentity: store.Identity().LineageID, AttemptID: c.AttemptID, SnapshotID: c.SnapshotID, CandidateID: c.CandidateID, BaseOID: c.BaseOID, TreeOID: c.TreeOID, Kind: "install", Result: "pass"}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	revalidated, err := store.RevalidateAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: generation, CandidateID: c.CandidateID}, append(c.Evidence, ref))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := store.LoadAcceptedCandidate(*revalidated.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return sealed, revalidated
}

func appProbeFixture(t *testing.T, argv []string) (config.AppConfig, *controller.Store, controller.AcceptedCandidate, uint64) {
	t.Helper()
	cfg, admission, store, policy := retiredWorkflowCorpus(t, false)
	policy.RequireInstall = true
	tree, err := store.PreviewCandidateTree(admission, policy)
	if err != nil {
		t.Fatal(err)
	}
	obligation, err := store.StoreCandidateEvidenceJSON(controller.CandidateEvidence{SchemaVersion: 1, RepositoryIdentity: store.Identity().LineageID, AttemptID: admission.Attempt.AttemptID, SnapshotID: admission.Snapshot.ID, BaseOID: admission.Head.IntegrationTip, TreeOID: tree, Kind: "install-obligation", Result: "declared"}, []byte(`{"required":true}`))
	if err != nil {
		t.Fatal(err)
	}
	probe := controller.SmokeProbeDeclaration{ProbeID: "docker-worker-path", Verifies: "the deployed worker path routes Go execution through Docker", Argv: argv}
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{
		Message: "probed corpus result", Policy: policy,
		Evidence:            append(canonicalWorkflowEvidence(t, cfg, store, admission, policy), obligation),
		RequiredValidations: &controller.RequiredValidationDeclaration{Probes: []controller.SmokeProbeDeclaration{probe}},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	sealed, revalidated := sealAppInstallProof(t, store, candidate, accepted.Head.ControllerGeneration, "deployed runtime for "+candidate.CommitOID)
	return cfg, store, sealed, revalidated.Head.ControllerGeneration
}

func latestAppProbeRun(t *testing.T, cfg config.AppConfig, store *controller.Store, c controller.AcceptedCandidate, probeID string) qualitygate.RunRecord {
	t.Helper()
	module := state.AttachStateStore(controller.WorkflowTaskConfig(cfg, store.Identity(), c.TaskRef))
	entries, err := os.ReadDir(module.Path(qualitygate.RunDirectory))
	if err != nil {
		t.Fatal(err)
	}
	var latest qualitygate.RunRecord
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := qualitygate.Read(module, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if record.Form != "post-install-smoke" || record.ProbeID != probeID {
			continue
		}
		if latest.ValidationRunID == "" || latest.StartedAt.Before(record.StartedAt) {
			latest = record
		}
	}
	if latest.ValidationRunID == "" {
		t.Fatalf("no recorded run for probe %s", probeID)
	}
	return latest
}

func TestPostInstallSmokeActionRunsDeclaredProbes(t *testing.T) {
	t.Parallel()
	cfg, store, candidate, generation := appProbeFixture(t, []string{"/bin/sh", "-c", "exit 0"})
	result, err := executeParentCompletionPostInstallSmoke(cfg, store, controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}, controllerPublicationCommand{SourceOID: candidate.CommitOID})
	if err != nil {
		t.Fatal(err)
	}
	output, ok := result.(postInstallSmokeOutput)
	if !ok || output.Result != "pass" || len(output.ProbeRuns) != 1 || output.ProbeRuns[0].ProbeID != "docker-worker-path" || output.ProbeRuns[0].Status != qualitygate.StatusPass {
		t.Fatalf("probe action output = %#v err=%v", result, err)
	}
	if output.ProbeRuns[0].InstallEvidenceBinding == "" {
		t.Fatalf("probe run lost its verification deployment binding: %#v", output.ProbeRuns[0])
	}
	probes, err := store.OutstandingSmokeProbes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 0 {
		t.Fatalf("passing probe run left obligations open: %#v", probes)
	}
}

func TestPostInstallSmokeActionRecordsFailedProbeAndKeepsReentry(t *testing.T) {
	t.Parallel()
	cfg, store, candidate, generation := appProbeFixture(t, []string{"/bin/sh", "-c", "exit 3"})
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}, controllerPublicationCommand{SourceOID: candidate.CommitOID}); err == nil || !strings.Contains(err.Error(), "docker-worker-path failed") {
		t.Fatalf("failing probe argv must surface its evidence: %v", err)
	}
	record := latestAppProbeRun(t, cfg, store, candidate, "docker-worker-path")
	if record.Status != qualitygate.StatusFail {
		t.Fatalf("failed probe argv recorded status %s", record.Status)
	}
	probes, err := store.OutstandingSmokeProbes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].ProbeID != "docker-worker-path" {
		t.Fatalf("failed probe must remain outstanding: %#v", probes)
	}
	operation, err := store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if operation.Operation != controller.PublicationOperationPostInstallSmoke || len(operation.SmokeProbeIDs) != 1 {
		t.Fatalf("the same Task must keep its outstanding probe as the legal next operation: %#v", operation)
	}
}

func TestPostInstallSmokeActionRefusesSourceMutatingProbe(t *testing.T) {
	t.Parallel()
	cfg, store, candidate, generation := appProbeFixture(t, []string{"/bin/sh", "-c", "echo drift >> IMPLEMENTATION_RULES.md"})
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}, controllerPublicationCommand{SourceOID: candidate.CommitOID}); err == nil || !strings.Contains(err.Error(), "docker-worker-path failed") {
		t.Fatalf("a probe that mutates the candidate source must not pass: %v", err)
	}
	record := latestAppProbeRun(t, cfg, store, candidate, "docker-worker-path")
	if record.Status != qualitygate.StatusFail {
		t.Fatalf("source-mutating probe recorded status %s", record.Status)
	}
	log, err := os.ReadFile(record.Log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "source verification failed") {
		t.Fatalf("probe evidence must name the post-run source verification failure: %s", log)
	}
	probes, err := store.OutstandingSmokeProbes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 {
		t.Fatalf("a source-mutating probe must keep the obligation open: %#v", probes)
	}
}

func TestPostInstallSmokeDeploymentRevisionReopensObligation(t *testing.T) {
	t.Parallel()
	cfg, store, candidate, generation := appProbeFixture(t, []string{"/bin/sh", "-c", "exit 0"})
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}, controllerPublicationCommand{SourceOID: candidate.CommitOID}); err != nil {
		t.Fatal(err)
	}
	probes, err := store.OutstandingSmokeProbes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 0 {
		t.Fatalf("passing probe run left obligations open: %#v", probes)
	}
	revised, resealed := sealAppInstallProof(t, store, candidate, generation, "replacement deployed runtime for "+candidate.CommitOID)
	reopened, err := store.OutstandingSmokeProbes(revised)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened) != 1 || reopened[0].ProbeID != "docker-worker-path" {
		t.Fatalf("a revised verification deployment must reopen the probe obligation: %#v", reopened)
	}
	if _, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: resealed.Head.ControllerGeneration, CandidateID: revised.CandidateID}); err == nil || !strings.Contains(err.Error(), "outstanding post-install smoke probes: docker-worker-path") {
		t.Fatalf("promotion must refuse a probe PASS bound to a superseded deployment: %v", err)
	}
}

func TestPostInstallSmokeUsesSealedRuntimeEnvironment(t *testing.T) {
	t.Parallel()
	argv := []string{"/bin/sh", "-c", `test -n "$GLM_POST_INSTALL_SOURCE_OID" && test -n "$GLM_POST_INSTALL_EVIDENCE_DIGEST" && "$GLM_POST_INSTALL_BINARY"`}
	cfg, store, candidate, generation := appProbeFixture(t, argv)
	result, err := executeParentCompletionPostInstallSmoke(cfg, store, controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}, controllerPublicationCommand{SourceOID: candidate.CommitOID})
	if err != nil {
		t.Fatal(err)
	}
	output := result.(postInstallSmokeOutput)
	log, err := os.ReadFile(output.ProbeRuns[0].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "deployed runtime for "+candidate.CommitOID) {
		t.Fatalf("probe did not execute the sealed runtime: %s", log)
	}
}

func TestPostInstallSmokeRejectsRuntimeMutationAndProjectsReinstall(t *testing.T) {
	t.Parallel()
	argv := []string{"/bin/sh", "-c", `printf tampered > "$GLM_POST_INSTALL_BINARY"`}
	cfg, store, candidate, generation := appProbeFixture(t, argv)
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}, controllerPublicationCommand{SourceOID: candidate.CommitOID}); err == nil {
		t.Fatal("runtime-mutating probe passed")
	}
	record := latestAppProbeRun(t, cfg, store, candidate, "docker-worker-path")
	if record.Status != qualitygate.StatusFail {
		t.Fatalf("runtime-mutating probe recorded %s", record.Status)
	}
	operation, err := store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if operation.Operation != controller.PublicationOperationInstall {
		t.Fatalf("runtime drift must retain the same candidate and project reinstall: %#v", operation)
	}
}

func TestPostInstallSmokeRefusesReplacedRuntimeBeforeProbe(t *testing.T) {
	t.Parallel()
	cfg, store, candidate, generation := appProbeFixture(t, []string{"/bin/sh", "-c", "exit 0"})
	deployment, err := store.PostInstallSmokeDeployment(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deployment.BinaryPath, []byte("replacement"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}, controllerPublicationCommand{SourceOID: candidate.CommitOID}); err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("replaced runtime was admitted: %v", err)
	}
}

func TestPostInstallSmokeInterruptedProbeRetriesInSameTask(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "interrupted")
	argv := []string{"/bin/sh", "-c", `if [ ! -f "$1" ]; then touch "$1"; kill -TERM $$; fi`, "probe", marker}
	cfg, store, candidate, generation := appProbeFixture(t, argv)
	input := controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}
	command := controllerPublicationCommand{SourceOID: candidate.CommitOID}
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, input, command); err == nil {
		t.Fatal("interrupted probe passed")
	}
	first := latestAppProbeRun(t, cfg, store, candidate, "docker-worker-path")
	if first.Status != qualitygate.StatusInterrupted {
		t.Fatalf("interrupted probe recorded %s", first.Status)
	}
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, input, command); err != nil {
		t.Fatal(err)
	}
	last := latestAppProbeRun(t, cfg, store, candidate, "docker-worker-path")
	if last.Status != qualitygate.StatusPass || last.TaskID != first.TaskID || last.ValidationRunID == first.ValidationRunID {
		t.Fatalf("retry did not preserve Task identity with a new successful run: first=%#v last=%#v", first, last)
	}
	if err := store.PreparePostInstallSmokeCheckout(candidate, false); err != nil {
		t.Fatal(err)
	}
	_, root, release, err := store.CandidatePostInstallSmokeWorkspace(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	path := filepath.Join(root, "after-pass.txt")
	if err := os.WriteFile(path, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, input, command); err == nil {
		t.Fatal("old Interrupted evidence authorized cleanup after a newer PASS")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "retained" {
		t.Fatalf("newer PASS did not supersede old recovery evidence: %q %v", data, err)
	}
}

func TestPostInstallSmokeRecoversAbandonedRunInSameTask(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name        string
		script      string
		interrupted bool
	}{
		{name: "clean", script: "exit 0"},
		{name: "dirty", script: "echo abandoned >> IMPLEMENTATION_RULES.md"},
		{name: "interrupted-before-cleanup", script: "echo abandoned >> IMPLEMENTATION_RULES.md", interrupted: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			cfg, store, candidate, generation := appProbeFixture(t, []string{"/bin/sh", "-c", "exit 0"})
			input := controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}
			_, root, release, err := store.CandidatePostInstallSmokeWorkspace(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.PreparePostInstallSmokeCheckout(candidate, false); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = release() })
			runtime := state.AttachStateStore(controller.WorkflowTaskConfig(cfg, store.Identity(), candidate.TaskRef))
			record, err := store.NewSmokeProbeRunRecord(candidate, candidate.RequiredSmokeProbes[0], root, runtime.ReadOr("task.id", ""))
			if err != nil {
				t.Fatal(err)
			}
			dead := exec.Command("/bin/sh", "-c", scenario.script)
			dead.Dir = root
			if err := dead.Start(); err != nil {
				t.Fatal(err)
			}
			record.RunnerPID = dead.Process.Pid
			if err := writePostInstallSmokeRecord(runtime, record); err != nil {
				t.Fatal(err)
			}
			if err := dead.Wait(); err != nil {
				t.Fatal(err)
			}
			if scenario.interrupted {
				if _, err := reconcileQualityGateRun(runtime, record.ValidationRunID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := executeParentCompletionPostInstallSmoke(cfg, store, input, controllerPublicationCommand{SourceOID: candidate.CommitOID}); err != nil {
				t.Fatal(err)
			}
			interrupted, err := qualitygate.Read(runtime, record.ValidationRunID)
			if err != nil {
				t.Fatal(err)
			}
			last := latestAppProbeRun(t, cfg, store, candidate, "docker-worker-path")
			if interrupted.Status != qualitygate.StatusInterrupted || last.Status != qualitygate.StatusPass || last.TaskID != record.TaskID {
				t.Fatalf("abandoned run was not preserved before same-Task retry: old=%#v new=%#v", interrupted, last)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("completed retry left its checkout retained: %v", err)
			}
		})
	}
}

func TestPostInstallSmokePreservesUnprovenDirtyCheckout(t *testing.T) {
	t.Parallel()
	cfg, store, candidate, generation := appProbeFixture(t, []string{"/bin/sh", "-c", "exit 0"})
	input := controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID}
	_, root, release, err := store.CandidatePostInstallSmokeWorkspace(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PreparePostInstallSmokeCheckout(candidate, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	path := filepath.Join(root, "unproven.txt")
	if err := os.WriteFile(path, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := executeParentCompletionPostInstallSmoke(cfg, store, input, controllerPublicationCommand{SourceOID: candidate.CommitOID}); err == nil {
		t.Fatal("dirty checkout without an abandoned run was admitted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "retained" {
		t.Fatalf("unproven dirty checkout was discarded: %q %v", data, err)
	}
}
