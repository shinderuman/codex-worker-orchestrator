package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type runtimeEvidenceFixture struct {
	runtime        *state.StateStore
	telemetry      []byte
	transcript     []byte
	artifact       []byte
	parent         []byte
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
	for _, failure := range []string{"missing-transcript", "stale-attempt", "artifact-symlink", "missing-runtime"} {
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
			case "stale-attempt":
				if err := runtime.runtime.Write(state.ControllerAttemptStateFile, "replacement-attempt"); err != nil {
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
	for name, value := range map[string]string{state.ControllerAttemptStateFile: source.Attempt.AttemptID, state.CanonicalExecutionTaskStateFile: source.Attempt.SemanticTaskRef.TaskPath, "task.id": source.Attempt.AttemptID} {
		if err := runtime.Write(name, value); err != nil {
			t.Fatal(err)
		}
	}
	sessionID := "d9bc7b1e-c37a-42b0-863e-c1f968fe201d"
	store.config.ClaudeConfigDir = filepath.Join(t.TempDir(), "claude")
	transcriptPath := filepath.Join(store.config.ClaudeConfigDir, "projects", "project", sessionID+".jsonl")
	transcript := []byte("{\"transcript\":\"original-b1\"}\n")
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
	parentID := "7e80986f-2269-4d4d-b576-9b048ef131f8"
	if err := runtime.SetParentCodexIdentity(parentID, parentID, func() *state.SessionLimitReading { return nil }); err != nil {
		t.Fatal(err)
	}
	store.config.CodexConfigDir = filepath.Join(t.TempDir(), "codex")
	parent := []byte("{\"type\":\"session_meta\",\"timestamp\":\"" + source.Attempt.CreatedAt.Format(time.RFC3339Nano) + "\",\"payload\":{\"id\":\"" + parentID + "\",\"cwd\":\"" + source.Workspace.Root + "\"}}\n")
	writeRuntimeEvidenceFile(t, filepath.Join(store.config.CodexConfigDir, "sessions", "parent.jsonl"), parent)
	return runtimeEvidenceFixture{runtime: runtime, telemetry: telemetry, transcript: transcript, artifact: artifact, parent: parent, transcriptPath: transcriptPath}
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

func requireRuntimeBundleBytes(t *testing.T, bundle EvidenceBundleProjection, runtime runtimeEvidenceFixture) {
	t.Helper()
	for _, data := range [][]byte{runtime.telemetry, runtime.transcript, runtime.artifact, runtime.parent} {
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
