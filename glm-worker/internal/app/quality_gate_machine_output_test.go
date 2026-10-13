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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

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
