package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const (
	machineReportBundleAnalysis = "bundle-analysis-index"
	machineReportBundleManifest = "bundle-manifest"
	machineReportBundleReceipt  = "bundle-receipt"
)

var bundleMachineReportBudgets = map[string]int{
	machineReportBundleAnalysis: 12 * 1024,
	machineReportBundleManifest: 4 * 1024,
	machineReportBundleReceipt:  2 * 1024,
}

func checkBundleMachineReportBudget(surface string, data []byte) error {
	limit, ok := bundleMachineReportBudgets[surface]
	if !ok {
		return fmt.Errorf("unknown machine report surface %q", surface)
	}
	if len(data) <= limit {
		return nil
	}
	return fmt.Errorf("machine report %s is %d bytes, budget is %d", surface, len(data), limit)
}

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
		{name: "analysis-index", surface: machineReportBundleAnalysis, value: fixture.index},
		{name: "manifest", surface: machineReportBundleManifest, value: fixture.manifest},
		{name: "receipt", surface: machineReportBundleReceipt, value: receipt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkBundleMachineReportBudget(tc.surface, data); err != nil {
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
	limit, ok := bundleMachineReportBudgets[machineReportBundleAnalysis]
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
	if err := checkBundleMachineReportBudget(machineReportBundleAnalysis, inflatedData); err == nil {
		t.Fatalf("prose growth stayed under protected budget: base=%d inflated=%d limit=%d", len(data), len(inflatedData), limit)
	}
}
