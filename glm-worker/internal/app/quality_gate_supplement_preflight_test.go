package app

import (
	"context"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDarwinSupplementPreflightFailureFinalizesExecution(t *testing.T) {
	t.Parallel()
	record := qualitygate.RunRecord{
		Form: "go-test", WorkingDir: t.TempDir(),
		Toolchain: &qualitygate.RunToolchain{RequiredGoVersion: "0.0.0"},
		Execution: &qualitygate.Execution{Backend: qualitygate.BackendDockerARM64},
	}
	if err := runDarwinQualitySupplement(context.Background(), nil, &record); err == nil {
		t.Fatal("unavailable required toolchain was accepted")
	}
	supplement := record.Execution.NativeSupplement
	if supplement == nil || supplement.Status != qualitygate.StatusFail ||
		supplement.ExitCode == 0 || supplement.ExitSource != state.ValidationExitSourceWrapper {
		t.Fatalf("early supplement failure retained an in-progress status: %#v", supplement)
	}
	if supplement.Log != "" || supplement.Toolchain == nil {
		t.Fatalf("early failure fabricated execution log or lost toolchain evidence: %#v", supplement)
	}
}
