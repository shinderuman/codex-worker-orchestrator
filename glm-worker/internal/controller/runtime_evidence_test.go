package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type runtimeEvidenceFixture struct {
	runtime        *state.StateStore
	telemetry      []byte
	transcript     []byte
	artifact       []byte
	parent         []byte
	gateRun        []byte
	smokeLog       []byte
	smokeRun       []byte
	plan           []byte
	rules          []byte
	history        []byte
	transcriptPath string
}

func TestSuspensionBundlePreservesBoundRuntimeTelemetry(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	runtime := bindRuntimeEvidenceFixture(t, fixture.store, fixture.source)
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	before, err := fixture.store.BuildAttemptEvidenceBundle(*suspended.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{runtime.runtime.Path("."), fixture.store.config.ClaudeConfigDir, fixture.store.config.CodexConfigDir} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	after, err := fixture.store.BuildAttemptEvidenceBundle(*suspended.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	requireRuntimeBundleBytes(t, after, runtime)
	if before.EvidenceGraphDigest != after.EvidenceGraphDigest {
		t.Fatal("runtime deletion changed historical bundle digest")
	}
	writeRuntimeEvidenceFile(t, runtime.runtime.ModelCallLogPath(fixture.source.Attempt.AttemptID), []byte("replacement runtime\n"))
	reused, err := fixture.store.BuildAttemptEvidenceBundle(*suspended.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	if reused.EvidenceGraphDigest != after.EvidenceGraphDigest {
		t.Fatal("same-path replacement contaminated historical bundle")
	}
	for _, object := range after.Objects {
		if object.Ref.Kind != "telemetry" {
			continue
		}
		if err := os.Remove(fixture.store.evidenceObjectPath(object.Ref.Digest)); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.BuildAttemptEvidenceBundle(*suspended.SealRef); err == nil {
			t.Fatal("missing sealed telemetry was accepted")
		} else {
			var integrity *EvidenceIntegrityError
			if !errors.As(err, &integrity) {
				t.Fatalf("missing telemetry returned untyped error: %v", err)
			}
		}
		return
	}
	t.Fatal("suspension bundle has no sealed telemetry")
}

func TestSuspensionSealsRepositoryScopedValidationRuns(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	runtime := bindRuntimeEvidenceFixture(t, fixture.store, fixture.source)
	workspaceRoot := fixture.source.Workspace.Root
	completed := fixture.source.Attempt.CreatedAt.Add(time.Minute)
	moduleRun := qualitygate.RunRecord{
		ValidationRunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Form: "go-test",
		Repository: workspaceRoot, WorkingDir: filepath.Join(workspaceRoot, "glm-worker"),
		StartedAt: fixture.source.Attempt.CreatedAt, CompletedAt: &completed, Status: qualitygate.StatusPass,
	}
	staleCompleted := fixture.source.Attempt.CreatedAt.Add(-59 * time.Minute)
	staleRun := moduleRun
	staleRun.ValidationRunID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	staleRun.StartedAt = fixture.source.Attempt.CreatedAt.Add(-time.Hour)
	staleRun.CompletedAt = &staleCompleted
	module := state.AttachStateStore(config.AppConfig{StateBase: fixture.store.config.StateBase, RepoHash: config.RepoHashFor(workspaceRoot)})
	if err := module.Write("repo-root", workspaceRoot); err != nil {
		t.Fatal(err)
	}
	writeEvidenceGateRun(t, module, moduleRun)
	writeEvidenceGateRun(t, module, staleRun)
	secondRuntimeRun := moduleRun
	secondRuntimeRun.ValidationRunID = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	writeEvidenceGateRun(t, runtime.runtime, secondRuntimeRun)
	foreignTaskRun := moduleRun
	foreignTaskRun.ValidationRunID = "dddddddddddddddddddddddddddddddd"
	foreignTaskRun.TaskID = "older-runtime-task-of-same-path"
	writeEvidenceGateRun(t, runtime.runtime, foreignTaskRun)
	moduleRunData, err := json.Marshal(moduleRun)
	if err != nil {
		t.Fatal(err)
	}
	secondRuntimeRunData, err := json.Marshal(secondRuntimeRun)
	if err != nil {
		t.Fatal(err)
	}

	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := fixture.store.LoadAttemptSeal(*suspended.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	sealedLogical := map[string]bool{}
	for _, ref := range seal.EvidenceRefs {
		sealedLogical[ref.LogicalIdentity] = true
	}
	attemptID := fixture.source.Attempt.AttemptID
	for _, file := range []string{qualitygate.RunFile, qualitygate.RunLog} {
		if !sealedLogical[attemptID+":state/quality-gate-runs/"+moduleRun.ValidationRunID+"/"+file] {
			t.Fatalf("seal lost the repository-scoped run file %s", file)
		}
		if !sealedLogical[attemptID+":state/quality-gate-runs/"+evidenceExportGateRunID+"/"+file] {
			t.Fatalf("seal lost the first runtime gate run file %s", file)
		}
		if !sealedLogical[attemptID+":state/quality-gate-runs/"+secondRuntimeRun.ValidationRunID+"/"+file] {
			t.Fatalf("seal lost the second runtime gate run file %s", file)
		}
		if !sealedLogical[attemptID+":state/install-smoke-runs/"+evidenceExportSmokeRunID+"/"+file] &&
			file == qualitygate.RunFile {
			t.Fatalf("seal lost the runtime install smoke run record %s", file)
		}
		if sealedLogical[attemptID+":state/quality-gate-runs/"+staleRun.ValidationRunID+"/"+file] {
			t.Fatalf("seal absorbed a run completed before the attempt: %s", file)
		}
		if sealedLogical[attemptID+":state/quality-gate-runs/"+foreignTaskRun.ValidationRunID+"/"+file] {
			t.Fatalf("seal absorbed another runtime task's run left in the shared runtime store: %s", file)
		}
	}
	if !sealedLogical[attemptID+":state/install-smoke-runs/"+evidenceExportSmokeRunID+"/"+evidenceExportValidationSmokeLog] {
		t.Fatal("seal lost the runtime install smoke log")
	}
	for _, path := range []string{runtime.runtime.Path("."), module.Path(".")} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := fixture.store.BuildAttemptEvidenceBundle(*suspended.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	runSealed, logSealed := false, false
	secondRunSealed, secondLogSealed := false, false
	for _, object := range bundle.Objects {
		if bytes.Equal(object.Data, moduleRunData) {
			runSealed = true
		}
		if bytes.Equal(object.Data, []byte("gate log for "+moduleRun.ValidationRunID+"\n")) {
			logSealed = true
		}
		if bytes.Equal(object.Data, secondRuntimeRunData) {
			secondRunSealed = true
		}
		if bytes.Equal(object.Data, []byte("gate log for "+secondRuntimeRun.ValidationRunID+"\n")) {
			secondLogSealed = true
		}
	}
	if !runSealed || !logSealed || !secondRunSealed || !secondLogSealed {
		t.Fatalf("validation raw not retrievable after owner deletion: moduleRun=%v moduleLog=%v secondRun=%v secondLog=%v",
			runSealed, logSealed, secondRunSealed, secondLogSealed)
	}
	requireRuntimeBundleBytes(t, bundle, runtime)
}

func TestSuspensionWithoutRuntimeRecordsIncompleteCoverage(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := fixture.store.LoadAttemptSeal(*suspended.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	if seal.Coverage != "incomplete" {
		t.Fatalf("runtimeless suspension coverage = %q", seal.Coverage)
	}
	found := false
	for _, missing := range seal.Missing {
		if missing == attemptSealRuntimeEvidenceMissing {
			found = true
		}
	}
	if !found {
		t.Fatalf("runtimeless suspension missing = %#v", seal.Missing)
	}
	if len(seal.SessionAssociationRefs) != 0 {
		t.Fatalf("runtimeless suspension claimed session associations: %#v", seal.SessionAssociationRefs)
	}
}

func TestAcceptedCandidateBundlePreservesBoundRuntimeEvidence(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "accepted.txt", "accepted runtime result\n")
	runtime := bindRuntimeEvidenceFixture(t, fixture.store, source)
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "accept runtime result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(runtime.runtime.Path(".")); err != nil {
		t.Fatal(err)
	}
	bundle, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	requireRuntimeBundleBytes(t, bundle, runtime)
}

func TestRuntimeEvidenceFailurePreservesSourceAuthority(t *testing.T) {
	for _, failure := range []string{"missing-transcript", "untimestamped-transcript", "stale-attempt", "stale-task", "artifact-symlink", "missing-runtime"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newFindingAcceptanceFixture(t)
			runtime := bindRuntimeEvidenceFixture(t, fixture.store, fixture.source)
			switch failure {
			case "missing-runtime":
				bound, err := fixture.store.BindModelCall(fixture.source)
				if err != nil {
					t.Fatal(err)
				}
				fixture.source, err = fixture.store.RecordAdmittedMutation(bound, "model:worker:test", "success", bound.Snapshot)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(runtime.runtime.Path(".")); err != nil {
					t.Fatal(err)
				}
			case "missing-transcript":
				if err := os.Remove(runtime.transcriptPath); err != nil {
					t.Fatal(err)
				}
			case "untimestamped-transcript":
				writeRuntimeEvidenceFile(t, runtime.transcriptPath, []byte("{\"transcript\":\"reused-session-mix\"}\n"))
			case "stale-attempt":
				binding, err := runtime.runtime.LoadControllerRuntimeBinding()
				if err != nil {
					t.Fatal(err)
				}
				binding.AttemptID = "replacement-attempt"
				if err := runtime.runtime.SaveControllerRuntimeBinding(binding); err != nil {
					t.Fatal(err)
				}
			case "stale-task":
				binding, err := runtime.runtime.LoadControllerRuntimeBinding()
				if err != nil {
					t.Fatal(err)
				}
				binding.TaskPath = "IMPLEMENTATION_TASKS/stale.md"
				if err := runtime.runtime.SaveControllerRuntimeBinding(binding); err != nil {
					t.Fatal(err)
				}
			case "artifact-symlink":
				if err := os.Symlink(runtime.transcriptPath, filepath.Join(runtime.runtime.ArtifactDir(fixture.source.Attempt.AttemptID), "foreign")); err != nil {
					t.Fatal(err)
				}
			}
			episode := planSuspensionTestEpisode(t, fixture)
			if _, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision); err == nil {
				t.Fatal("suspension accepted invalid runtime evidence")
			}
			head, err := fixture.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if head.LiveLeaseID != fixture.source.Lease.LeaseID || head.ControllerGeneration != fixture.source.Head.ControllerGeneration || head.PendingTransitionID != "" {
				t.Fatal("failed evidence preflight changed source authority")
			}
		})
	}
}

func bindRuntimeEvidenceFixture(t *testing.T, store *Store, source Admission) runtimeEvidenceFixture {
	t.Helper()
	cfg := store.config
	cfg.RepoRoot = source.Workspace.Root
	cfg, err := WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: source.Attempt.AttemptID, TaskPath: source.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("task.id", source.Attempt.AttemptID); err != nil {
		t.Fatal(err)
	}
	sessionID := "d9bc7b1e-c37a-42b0-863e-c1f968fe201d"
	store.config.ClaudeConfigDir = filepath.Join(t.TempDir(), "claude")
	transcriptPath := filepath.Join(store.config.ClaudeConfigDir, "projects", "project", sessionID+".jsonl")
	transcript := []byte("{\"timestamp\":\"" + source.Attempt.CreatedAt.Format(time.RFC3339Nano) + "\",\"transcript\":\"original-b1\"}\n")
	telemetry, err := json.Marshal(state.ModelCallLog{Version: state.ModelCallLogVersion, CallType: state.CallTypeTask, TaskID: source.Attempt.AttemptID, SessionID: sessionID, Response: "runtime-b1"})
	if err != nil {
		t.Fatal(err)
	}
	telemetry = append(telemetry, '\n')
	probe, err := json.Marshal(state.ModelCallLog{Version: state.ModelCallLogVersion, CallType: state.CallTypeProbe, TaskID: source.Attempt.AttemptID, SessionID: "none"})
	if err != nil {
		t.Fatal(err)
	}
	telemetry = append(telemetry, append(probe, '\n')...)
	artifact := []byte("original validation output\n")
	writeRuntimeEvidenceFile(t, transcriptPath, transcript)
	writeRuntimeEvidenceFile(t, runtime.ModelCallLogPath(source.Attempt.AttemptID), telemetry)
	writeRuntimeEvidenceFile(t, filepath.Join(runtime.ArtifactDir(source.Attempt.AttemptID), "validation.txt"), artifact)
	writeRuntimeEvidenceFile(t, runtime.ModelCallLogPath("unrelated-task"), []byte("foreign runtime\n"))
	gateCompleted := source.Attempt.CreatedAt.Add(time.Minute)
	gateRun, err := json.Marshal(qualitygate.RunRecord{
		ValidationRunID: "0123456789abcdef0123456789abcdef", Form: "go-test",
		Repository: source.Workspace.Root, WorkingDir: source.Workspace.Root,
		Head: "head-oid", IndexDigest: "index-digest", WorktreeDigest: "worktree-digest",
		StartedAt: source.Attempt.CreatedAt, CompletedAt: &gateCompleted, Status: qualitygate.StatusPass,
		ExitCode: 0, ExitSource: state.ValidationExitSourceTarget,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeRuntimeEvidenceFile(t, runtime.Path(filepath.Join(qualitygate.RunDirectory, "0123456789abcdef0123456789abcdef", qualitygate.RunFile)), gateRun)
	writeRuntimeEvidenceFile(t, runtime.Path(filepath.Join(qualitygate.RunDirectory, "0123456789abcdef0123456789abcdef", qualitygate.RunLog)), []byte("sealed gate log\n"))
	smokeLog := []byte("sealed install smoke tail\n")
	smokeRun, err := json.Marshal(qualitygate.RunRecord{
		ValidationRunID: evidenceExportSmokeRunID, Form: "install-smoke",
		Repository: source.Workspace.Root, WorkingDir: source.Workspace.Root,
		StartedAt: source.Attempt.CreatedAt, Status: qualitygate.StatusPass,
		ExitCode: 0, ExitSource: state.ValidationExitSourceTarget, DurationMS: 2000,
		Log: "install-smoke-runs/" + evidenceExportSmokeRunID + "/smoke.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeRuntimeEvidenceFile(t, runtime.Path(filepath.Join("install-smoke-runs", evidenceExportSmokeRunID, "run.json")), smokeRun)
	writeRuntimeEvidenceFile(t, runtime.Path(filepath.Join("install-smoke-runs", evidenceExportSmokeRunID, "smoke.log")), smokeLog)
	plan := readWorkspaceInstructionFile(t, source.Workspace.Root, evidenceExportInstructionPlanPath)
	rules := readWorkspaceInstructionFile(t, source.Workspace.Root, evidenceExportInstructionRulesPath)
	history := readWorkspaceInstructionFile(t, source.Workspace.Root, evidenceExportInstructionHistoryPath)
	parentID := "7e80986f-2269-4d4d-b576-9b048ef131f8"
	if err := runtime.SetParentCodexIdentity(parentID, parentID, func() *state.SessionLimitReading { return nil }); err != nil {
		t.Fatal(err)
	}
	store.config.CodexConfigDir = filepath.Join(t.TempDir(), "codex")
	parent := []byte("{\"type\":\"session_meta\",\"timestamp\":\"" + source.Attempt.CreatedAt.Format(time.RFC3339Nano) + "\",\"payload\":{\"id\":\"" + parentID + "\",\"cwd\":\"" + source.Workspace.Root + "\"}}\n")
	writeRuntimeEvidenceFile(t, filepath.Join(store.config.CodexConfigDir, "sessions", "parent.jsonl"), parent)
	return runtimeEvidenceFixture{runtime: runtime, telemetry: telemetry, transcript: transcript, artifact: artifact, parent: parent, gateRun: gateRun, smokeLog: smokeLog, smokeRun: smokeRun, plan: plan, rules: rules, history: history, transcriptPath: transcriptPath}
}

func writeRuntimeEvidenceFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readWorkspaceInstructionFile(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func requireRuntimeBundleBytes(t *testing.T, bundle EvidenceBundleProjection, runtime runtimeEvidenceFixture) {
	t.Helper()
	for _, data := range [][]byte{runtime.telemetry, runtime.transcript, runtime.artifact, runtime.parent, runtime.gateRun, runtime.smokeLog, runtime.smokeRun, runtime.plan, runtime.rules, runtime.history} {
		if data == nil {
			continue
		}
		found := false
		for _, object := range bundle.Objects {
			if bytes.Equal(object.Data, []byte("foreign runtime\n")) {
				t.Fatal("bundle absorbed an unrelated runtime task")
			}
			if bytes.Equal(object.Data, data) {
				found = true
			}
		}
		if !found {
			t.Fatalf("bundle lost required runtime bytes: %s", data)
		}
	}
}

func TestSuspensionBundlePreservesFindingAndEpisodeRecords(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	if _, err := fixture.store.ObserveFinding(fixture.source, FindingObservationInput{Producer: "external-review", ProofClass: FindingProofUnverified, ProblemKey: "unverified-locator", Evidence: []FindingEvidenceRef{{Kind: "caller-locator", ID: "untrusted/path"}}}); err != nil {
		t.Fatal(err)
	}
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := fixture.store.BuildAttemptEvidenceBundle(*suspended.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, object := range bundle.Objects {
		kinds[object.Ref.Kind] = true
	}
	for _, kind := range []string{"finding-record", "finding-disposition", "episode-revision"} {
		if !kinds[kind] {
			t.Errorf("suspension bundle omitted %s", kind)
		}
	}
}
