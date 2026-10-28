package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestFinalCandidateConsumesActualQualityGateDispatchEvidence(t *testing.T) {
	useInlineQualityGateRunner(t)
	shimDir, _ := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "absent-flag"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, admission, store, policy := retiredWorkflowCorpus(t, false)
	if err := os.Remove(filepath.Join(cfg.RepoRoot, repositoryharness.MarkerPath)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	admission, err = store.RecordAdmittedMutation(admission, "minimal publication corpus", "success", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	evidence := canonicalWorkflowEvidence(t, cfg, store, admission, policy)
	runtimeCfg := controller.WorkflowTaskConfig(cfg, store.Identity(), admission.Attempt.SemanticTaskRef)
	runtime, err := state.NewStateStore(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write(state.FinalValidationObligationKey, `{"Form":"go-test","WorkingDir":"."}`); err != nil {
		t.Fatal(err)
	}
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{
		Message: "final source", Policy: policy, Evidence: evidence,
		RequiredValidations: &controller.RequiredValidationDeclaration{Forms: []controller.FinalValidationRequest{{Form: "go-test", WorkingDir: "."}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	root, release, err := controller.PrepareFinalValidationWorkspace(cfg, "go-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	raw := cfg
	raw.RepoRoot = root
	raw.RepoHash = config.RepoHashFor(root)
	var stdout, stderr bytes.Buffer
	if err := dispatchQualityGateMachineOutput(Command{Mode: ModeQualityGate, Payload: "go-test"}, raw, &stdout, &stderr); err != nil {
		t.Fatalf("actual gate dispatch failed: %v %s", err, stderr.String())
	}
	if got := runtime.ReadOr("repo-root", ""); got != cfg.RepoRoot {
		t.Fatalf("candidate validation changed Runtime repo-root: %s", got)
	}
	if err := os.Chdir(previous); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
		t.Fatalf("publication cannot consume the actual CLI run: %v", err)
	}
}

func TestQualityGateDispatchBindsValidationToCanonicalRuntime(t *testing.T) {
	useInlineQualityGateRunner(t)
	shimDir, _ := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "absent-flag"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	canonical := newCanonicalAppConfig(t)
	raw := canonical
	raw.RepoHash = config.RepoHashFor(raw.RepoRoot)
	raw.RepoShort = raw.RepoHash[:12]
	raw.RepositoryLockPath = ""
	if raw.RepoHash == canonical.RepoHash {
		t.Fatal("fixture did not produce distinct runtime identities")
	}
	stateBase := t.TempDir()
	canonical.StateBase = stateBase
	raw.StateBase = stateBase
	canonicalSt, err := state.NewStateStore(canonical)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := canonicalSt.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(canonical.RepoRoot); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := dispatchQualityGateMachineOutput(Command{Mode: ModeQualityGate, Payload: "go-test"}, raw, &stdout, &stderr); err != nil {
		t.Fatalf("dispatch: %v: %s", err, stderr.String())
	}
	var gateOutput qualityGateOutput
	if err := json.Unmarshal(stdout.Bytes(), &gateOutput); err != nil {
		t.Fatalf("stdout: %s: %v", stdout.String(), err)
	}
	if gateOutput.Status != qualitygate.StatusPass || !qualitygate.ValidRunID(gateOutput.ValidationRunID) {
		t.Fatalf("gate output = %#v", gateOutput)
	}
	runs, err := os.ReadDir(canonicalSt.Path(qualitygate.RunDirectory))
	if err != nil {
		t.Fatal(err)
	}
	runCount := 0
	for _, entry := range runs {
		if entry.IsDir() {
			runCount++
		}
	}
	if runCount != 1 {
		t.Fatalf("canonical run directories = %d", runCount)
	}
	rawSt := state.AttachStateStore(raw)
	if _, err := os.Stat(rawSt.Path(qualitygate.RunDirectory)); !os.IsNotExist(err) {
		t.Fatalf("raw runtime received quality gate data: %v", err)
	}
	record := readSingleValidationEvent(t, canonicalSt, taskID)
	if record.Validation.ValidationRunID != gateOutput.ValidationRunID {
		t.Fatalf("validation run = %#v, gate = %s", record.Validation, gateOutput.ValidationRunID)
	}
}

func TestQualityGateCanonicalRunKeepsFinalizationHandoffFreshUnderDecisionLease(t *testing.T) {
	useInlineQualityGateRunner(t)
	shimDir, _ := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "absent-flag"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	canonical := newCanonicalAppConfig(t)
	resolvedRoot, err := filepath.EvalSymlinks(canonical.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	canonical.RepoRoot = resolvedRoot
	raw := canonical
	raw.RepoHash = config.RepoHashFor(raw.RepoRoot)
	raw.RepoShort = raw.RepoHash[:12]
	raw.RepositoryLockPath = ""
	if raw.RepoHash == canonical.RepoHash {
		t.Fatal("fixture did not produce distinct runtime identities")
	}
	stateBase := t.TempDir()
	canonical.StateBase = stateBase
	raw.StateBase = stateBase
	st, err := state.NewStateStore(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(resolvedRoot); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	var first bytes.Buffer
	if err := printParentHandoffLeasedWithConfig(canonical, st, &first); err != nil {
		t.Fatalf("first handoff read: %v", err)
	}
	repeatErr := printParentHandoffLeasedWithConfig(canonical, st, io.Discard)
	var duplicate *parentevidence.DuplicateProjectionError
	if !errors.As(repeatErr, &duplicate) {
		t.Fatalf("identical handoff repeat error = %v", repeatErr)
	}

	var gateStdout bytes.Buffer
	var gateStderr bytes.Buffer
	if err := dispatchQualityGateMachineOutput(Command{Mode: ModeQualityGate, Payload: "go-test"}, raw, &gateStdout, &gateStderr); err != nil {
		t.Fatalf("gate dispatch: %v: %s", err, gateStderr.String())
	}
	var gateOutput qualityGateOutput
	if err := json.Unmarshal(gateStdout.Bytes(), &gateOutput); err != nil {
		t.Fatalf("gate stdout: %s: %v", gateStdout.String(), err)
	}

	var third bytes.Buffer
	if err := printParentHandoffLeasedWithConfig(canonical, st, &third); err != nil {
		t.Fatalf("post-gate handoff read: %v", err)
	}
	var served struct {
		Validations []parentHandoffValidation `json:"validations"`
	}
	if err := json.Unmarshal(third.Bytes(), &served); err != nil {
		t.Fatalf("post-gate handoff: %s: %v", third.String(), err)
	}
	found := false
	for _, validation := range served.Validations {
		if validation.ValidationRunID == gateOutput.ValidationRunID && validation.Status == qualitygate.StatusPass {
			found = true
		}
	}
	if !found {
		t.Fatalf("post-gate handoff lacks canonical validation %s: %s", gateOutput.ValidationRunID, third.String())
	}
}
