package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
)

func TestBundleMachineReportsStayWithinProtectedBudgets(t *testing.T) {
	fixture := newAnalysisBundleFixture(t)
	receipt := bundleOutput{
		bundleEvidenceProjection: bundleEvidenceProjection{
			TaskID:           fixture.taskID,
			TaskStatus:       "active",
			EvidenceStatus:   "complete",
			Coverage:         bundleCoverageOpen,
			CoverageScope:    bundleCoverageScope,
			ClaudeSessionIDs: []string{"session-worker"},
			Missing:          []string{},
		},
		ArchivePath: "/tmp/task.zip",
	}

	cases := []struct {
		name    string
		surface string
		value   any
	}{
		{name: "analysis-index", surface: harnesslint.MachineReportBundleAnalysis, value: fixture.index},
		{name: "manifest", surface: harnesslint.MachineReportBundleManifest, value: fixture.manifest},
		{name: "receipt", surface: harnesslint.MachineReportBundleReceipt, value: receipt},
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

func TestBundleAnalysisBudgetRejectsExplanatoryProseGrowth(t *testing.T) {
	data, err := json.Marshal(newAnalysisBundleFixture(t).index)
	if err != nil {
		t.Fatal(err)
	}
	limit, ok := harnesslint.MachineReportBudget(harnesslint.MachineReportBundleAnalysis)
	if !ok {
		t.Fatal("analysis-index budget is missing")
	}

	var inflated map[string]any
	if err := json.Unmarshal(data, &inflated); err != nil {
		t.Fatal(err)
	}
	growth := limit - len(data) + 1
	if growth < 1024 {
		growth = 1024
	}
	inflated["basis"] = strings.Repeat("x", growth)
	inflatedData, err := json.Marshal(inflated)
	if err != nil {
		t.Fatal(err)
	}
	if err := harnesslint.CheckMachineReportBudget(harnesslint.MachineReportBundleAnalysis, inflatedData); err == nil {
		t.Fatalf("prose growth stayed under protected budget: base=%d inflated=%d limit=%d", len(data), len(inflatedData), limit)
	}
}
