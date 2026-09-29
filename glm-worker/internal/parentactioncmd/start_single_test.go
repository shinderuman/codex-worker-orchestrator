package parentactioncmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExplicitSingleStartEnvRequiresExecutionUnitDisposition(t *testing.T) {
	if _, err := explicitSingleStartEnv([]string{"start"}, nil); err == nil || !strings.Contains(err.Error(), "--execution-unit single") {
		t.Fatalf("bare start error = %v", err)
	}
	if _, err := explicitSingleStartEnv([]string{"start", "--execution-unit", "milestones"}, nil); err == nil {
		t.Fatal("non-single execution unit was admitted by start")
	}

	base := []string{"BASE=1"}
	got, err := explicitSingleStartEnv([]string{"start", "--execution-unit", "single"}, base)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"BASE=1", executionunit.DispositionEnv + "=" + executionunit.ExecutionUnitSingle}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env = %#v want %#v", got, want)
	}
}

func TestExplicitSingleStartEnvPreservesRotationClaim(t *testing.T) {
	claim := "11111111-1111-4111-8111-111111111111"
	got, err := explicitSingleStartEnv([]string{"start", "--execution-unit", "single", "--rotation-claim", claim}, []string{"BASE=1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"BASE=1",
		executionunit.DispositionEnv + "=" + executionunit.ExecutionUnitSingle,
		state.SessionRotationClaimIDEnv + "=" + claim,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env = %#v want %#v", got, want)
	}
}
