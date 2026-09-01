package state

import (
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
)

func TestLLMFacingEvaluationReportsStayWithinProtectedBudgets(t *testing.T) {
	cases := []struct {
		name    string
		surface string
		value   any
	}{
		{
			name:    "test-impact",
			surface: harnesslint.MachineReportTestImpact,
			value:   BuildTestImpactReport(nil, nil),
		},
		{
			name:    "repo-search",
			surface: harnesslint.MachineReportRepoSearch,
			value:   BuildRepoSearchReport(nil, map[string]TaskStats{}, nil),
		},
		{
			name:    "model-routing",
			surface: harnesslint.MachineReportModelRouting,
			value:   BuildModelRoutingReport(nil),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := harnesslint.CheckMachineReportBudget(tc.surface, data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMachineReportBudgetsExcludeRawEvidenceSurfaces(t *testing.T) {
	for _, surface := range []string{"collection", "task-events", "task-telemetry", "codex-rollout", "validation-log"} {
		if limit, ok := harnesslint.MachineReportBudget(surface); ok {
			t.Fatalf("raw evidence surface %q unexpectedly has %d-byte report budget", surface, limit)
		}
	}
}
