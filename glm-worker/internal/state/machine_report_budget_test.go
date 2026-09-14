package state

import (
	"encoding/json"
	"fmt"
	"testing"
)

const (
	machineReportTestImpact   = "test-impact"
	machineReportRepoSearch   = "repo-search"
	machineReportModelRouting = "model-routing"
)

var stateMachineReportBudgets = map[string]int{
	machineReportTestImpact:   6 * 1024,
	machineReportRepoSearch:   8 * 1024,
	machineReportModelRouting: 8 * 1024,
}

func checkStateMachineReportBudget(surface string, data []byte) error {
	limit, ok := stateMachineReportBudgets[surface]
	if !ok {
		return fmt.Errorf("unknown machine report surface %q", surface)
	}
	if len(data) <= limit {
		return nil
	}
	return fmt.Errorf("machine report %s is %d bytes, budget is %d", surface, len(data), limit)
}

func TestLLMFacingEvaluationReportsStayWithinProtectedBudgets(t *testing.T) {
	cases := []struct {
		name    string
		surface string
		value   any
	}{
		{
			name:    "test-impact",
			surface: machineReportTestImpact,
			value:   BuildTestImpactReport(nil, nil),
		},
		{
			name:    "repo-search",
			surface: machineReportRepoSearch,
			value:   BuildRepoSearchReport(nil, map[string]TaskStats{}, nil),
		},
		{
			name:    "model-routing",
			surface: machineReportModelRouting,
			value:   BuildModelRoutingReport(nil),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkStateMachineReportBudget(tc.surface, data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMachineReportBudgetsExcludeRawEvidenceSurfaces(t *testing.T) {
	for _, surface := range []string{"collection", "task-events", "task-telemetry", "codex-rollout", "validation-log"} {
		if limit, ok := stateMachineReportBudgets[surface]; ok {
			t.Fatalf("raw evidence surface %q unexpectedly has %d-byte report budget", surface, limit)
		}
	}
}
