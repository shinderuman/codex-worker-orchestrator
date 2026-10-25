package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecutionPreparationDoesNotConsumeParentEvidenceLease(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := reentryReviewRepairAppFixtureWithPreparation(t, false)
	f.revisePrimaryTask(t, "\n## Amendments\n\n親実装の修正版を独立Reviewする。\n")
	for _, action := range []string{"prepare-reentry-review", "repair-reentry-review"} {
		var handoff bytes.Buffer
		if err := printParentHandoffLeasedWithConfig(f.cfg, f.runtime, &handoff); err != nil {
			t.Fatal(err)
		}
		var projected parentHandoffOutput
		if err := json.Unmarshal(handoff.Bytes(), &projected); err != nil || projected.RequiredAction == nil || *projected.RequiredAction != controllerExecutionSurface {
			t.Fatalf("source repair handoff: %v", err)
		}
		var command controllerExecutionCommand
		if err := json.Unmarshal([]byte(projected.RequiredActionParameters["controller_command_json"]), &command); err != nil || command.Action != action {
			t.Fatalf("source repair action: %v", err)
		}
		assertReviewedParentExecutionCLI(t, f.cfg)
	}
	cp, err := f.runtime.LoadResumeCheckpoint()
	if err != nil || f.runtime.TaskStatus() != state.TaskStatusInterrupted || !cp.ReadOnly {
		t.Fatalf("source repair skipped independent read-only review: %v", err)
	}
}

func assertReviewedParentExecutionCLI(t *testing.T, cfg config.AppConfig) {
	t.Helper()
	var prepared struct {
		Slots       []json.RawMessage `json:"slots"`
		NextCommand []string          `json:"next_command"`
	}
	for attempt := 0; attempt < 2; attempt++ {
		code, output, diagnostics := runTypedParentActionCLI(t, cfg, "prepare", controllerExecutionSurface)
		if code != 0 {
			t.Fatalf("prepare after parent handoff: %s", diagnostics)
		}
		if err := json.Unmarshal([]byte(output), &prepared); err != nil {
			t.Fatal(err)
		}
		if len(prepared.Slots) != 0 || len(prepared.NextCommand) < 2 {
			t.Fatalf("source repair unexpectedly required additional operator input: %s", output)
		}
	}
	code, output, diagnostics := runTypedParentActionCLI(t, cfg, prepared.NextCommand[1:]...)
	if code != 0 {
		t.Fatalf("execute after preparation: %s %s", diagnostics, output)
	}
	var terminal struct {
		Handoff struct {
			Consistent bool `json:"consistent"`
		} `json:"handoff"`
	}
	if err := json.Unmarshal([]byte(output), &terminal); err != nil || !terminal.Handoff.Consistent {
		t.Fatalf("source repair lost the canonical terminal handoff: %v", err)
	}
}

func runTypedRuntimeDispositionCLI(t *testing.T, cfg config.AppConfig, reason string) {
	t.Helper()
	code, output, diagnostics := runTypedParentActionCLI(t, cfg, "prepare", controllerExecutionSurface)
	if code != 0 {
		t.Fatalf("prepare Runtime disposition: %s", diagnostics)
	}
	var prepared struct {
		Path        string                               `json:"path"`
		Slots       []struct{ Name, Placeholder string } `json:"slots"`
		NextCommand []string                             `json:"next_command"`
	}
	if err := json.Unmarshal([]byte(output), &prepared); err != nil || len(prepared.Slots) != 1 || prepared.Slots[0].Name != "runtime_disposition.reason" {
		t.Fatalf("Runtime disposition did not declare its semantic reason: %v %s", err, output)
	}
	body, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prepared.Path, []byte(strings.Replace(string(body), prepared.Slots[0].Placeholder, reason, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if code, output, diagnostics := runTypedParentActionCLI(t, cfg, prepared.NextCommand[1:]...); code != 0 {
		t.Fatalf("Runtime disposition failed: %s %s", output, diagnostics)
	}
}
