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

type parentOnlyInstallSmokeFixture struct {
	fixture   parentOnlyTaskFixture
	candidate AcceptedCandidate
	input     PublicationInput
	root      string
	runtime   *state.StateStore
	smoke     qualitygate.RunRecord
	runID     string
	installed InstalledRuntimeIdentity
}

func newParentOnlyInstallSmokeFixture(t *testing.T) parentOnlyInstallSmokeFixture {
	t.Helper()
	fixture, task := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	writeParentOnlyProduct(t, fixture, "evaluation-result.md", "parent-only install source\n")
	admission := admitParentOnlyExecutionFixture(t, fixture)
	completion := parentOnlyCompletionAdmission(t, fixture)
	policy := fixture.policy
	policy.RequireInstall = true
	accepted, err := fixture.store.AcceptParentCompletion(completion, CandidateAcceptanceInput{
		Message: "parent-only install evidence", Policy: policy, RequiredValidations: declaredNoValidations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	input := PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}
	_, root, release, err := fixture.store.CandidateInstallWorkspace(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	runtime := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.Identity(), task))
	taskID, err := runtime.Read("task.id")
	if err != nil || taskID != admission.Attempt.AttemptID {
		t.Fatalf("parent-only task identity is not bound to its attempt: %q %v", taskID, err)
	}
	if runtime.Exists("controller-runtime-binding.json") {
		t.Fatal("parent-only task created a model Runtime binding")
	}
	runID, err := qualitygate.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	completed := time.Now().UTC()
	smoke := qualitygate.RunRecord{
		ValidationRunID: runID, Form: "install-smoke", TaskID: taskID,
		Repository: root, WorkingDir: root,
		Status: "pass", ExitCode: 0, ExitSource: state.ValidationExitSourceTarget,
		StartedAt: completed.Add(-time.Second), CompletedAt: &completed,
		Log: filepath.ToSlash(filepath.Join("install-smoke-runs", runID, "smoke.log")),
	}
	bin := filepath.Join(t.TempDir(), "installed-worker")
	if err := os.WriteFile(bin, []byte("installed runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("installed runtime"))
	installed := InstalledRuntimeIdentity{SourceOID: candidate.CommitOID, BinaryPath: bin, BinarySHA256: hex.EncodeToString(sum[:])}
	result := parentOnlyInstallSmokeFixture{fixture: fixture, candidate: candidate, input: input, root: root, runtime: runtime, smoke: smoke, runID: runID, installed: installed}
	result.writeSmoke(t)
	return result
}

func (f parentOnlyInstallSmokeFixture) writeSmoke(t *testing.T) {
	t.Helper()
	path := f.runtime.Path(filepath.Join("install-smoke-runs", f.runID))
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(f.smoke)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, qualitygate.RunFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "smoke.log"), []byte("parent-only smoke passed"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParentOnlyInstallSmokeBindsCandidateWithoutModelRuntime(t *testing.T) {
	t.Parallel()
	f := newParentOnlyInstallSmokeFixture(t)
	record, log, err := f.fixture.store.readCandidateInstallSmoke(f.candidate, f.root, f.smoke.ValidationRunID)
	if err != nil || record.TaskID != f.candidate.AttemptID || !strings.Contains(string(log), "passed") {
		t.Fatalf("parent-only smoke evidence: record=%#v log=%q err=%v", record, log, err)
	}
	if _, err := f.fixture.store.RecordParentCompletionInstall(f.input, f.root, f.smoke.ValidationRunID, f.installed, []byte("installer passed")); err != nil {
		t.Fatalf("parent-only installed candidate was rejected: %v", err)
	}
	if f.runtime.Exists("controller-runtime-binding.json") {
		t.Fatal("parent-only install synthesized a model Runtime binding")
	}
}

func TestParentOnlyInstallSmokeRejectsWrongEvidenceIdentity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		change func(*qualitygate.RunRecord)
	}{
		{"other attempt", func(r *qualitygate.RunRecord) { r.TaskID = "other-attempt" }},
		{"stale contract task", func(r *qualitygate.RunRecord) { r.TaskID = "old-contract-attempt" }},
		{"different run ID", func(r *qualitygate.RunRecord) { r.ValidationRunID = "different-run-id" }},
		{"failed smoke", func(r *qualitygate.RunRecord) { r.Status = "fail" }},
		{"nonzero exit", func(r *qualitygate.RunRecord) { r.ExitCode = 13 }},
		{"incomplete smoke", func(r *qualitygate.RunRecord) { r.CompletedAt = nil }},
		{"wrong exit source", func(r *qualitygate.RunRecord) { r.ExitSource = state.ValidationExitSourceWrapper }},
		{"wrong repository", func(r *qualitygate.RunRecord) { r.Repository = "/unrelated/source" }},
		{"wrong log locator", func(r *qualitygate.RunRecord) { r.Log = "other-source/smoke.log" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newParentOnlyInstallSmokeFixture(t)
			tc.change(&f.smoke)
			f.writeSmoke(t)
			if _, _, err := f.fixture.store.readCandidateInstallSmoke(f.candidate, f.root, f.runID); err == nil {
				t.Fatal("unbound install smoke evidence was accepted")
			}
		})
	}
}

func TestParentOnlyInstallSmokeRejectsMissingOrStaleStoreTaskID(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"empty", "old-attempt", "missing"} {
		t.Run(mode, func(t *testing.T) {
			f := newParentOnlyInstallSmokeFixture(t)
			switch mode {
			case "missing":
				if err := os.Remove(f.runtime.Path("task.id")); err != nil {
					t.Fatal(err)
				}
			case "empty":
				if err := f.runtime.Write("task.id", ""); err != nil {
					t.Fatal(err)
				}
			default:
				if err := f.runtime.Write("task.id", "previous-attempt"); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := f.fixture.store.readCandidateInstallSmoke(f.candidate, f.root, f.smoke.ValidationRunID); err == nil {
				t.Fatal("parent-only store identity mismatch was accepted")
			}
		})
	}
}

func TestParentOnlyInstallSmokeRejectsOldTaskContract(t *testing.T) {
	t.Parallel()
	f := newParentOnlyInstallSmokeFixture(t)
	candidate := f.candidate
	candidate.TaskRef.ContractDigest = "old-contract"
	if _, _, err := f.fixture.store.readCandidateInstallSmoke(candidate, f.root, f.runID); err == nil {
		t.Fatal("old Task contract reused a current attempt's smoke evidence")
	}
}

func TestParentOnlyInstallSmokeReadIsStableAcrossRetry(t *testing.T) {
	t.Parallel()
	f := newParentOnlyInstallSmokeFixture(t)
	first, firstLog, err := f.fixture.store.readCandidateInstallSmoke(f.candidate, f.root, f.smoke.ValidationRunID)
	if err != nil {
		t.Fatal(err)
	}
	second, secondLog, err := f.fixture.store.readCandidateInstallSmoke(f.candidate, f.root, f.smoke.ValidationRunID)
	if err != nil || first.ValidationRunID != second.ValidationRunID || string(firstLog) != string(secondLog) {
		t.Fatalf("repeat evidence read changed the install identity: first=%#v second=%#v err=%v", first, second, err)
	}
}
