package controller

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

func writeEvidenceGateResourcesContent(t *testing.T, fixture *evidenceExportFixture, runID, content string) {
	t.Helper()
	writeEvidenceGateRunFileContent(t, fixture, runID, qualitygate.ResourceSamplesFile, content)
}

func gateRunResourceFixture(runID string) qualitygate.ResourceSamples {
	return qualitygate.ResourceSamples{
		SchemaVersion:   qualitygate.ResourceSamplesSchemaVersion,
		ValidationRunID: runID,
		Form:            "go-test",
		Host:            qualitygate.ResourceSampleHost{GOOS: "darwin", GOARCH: "arm64", LogicalCPUs: 8, TotalMemoryBytes: 16 << 30},
		Coverage: qualitygate.ResourceSampleCoverage{
			Status: qualitygate.ResourceCoveragePartial, Source: "darwin-sysctl-vm-stat-top",
			IntervalMS: 5000, SampleBound: 720, StartedAt: time.Unix(6100, 0).UTC(), EndedAt: time.Unix(6140, 0).UTC(),
			CPUState: qualitygate.ResourceStateMeasured, MemoryState: qualitygate.ResourceStateMeasured,
			SwapState:    qualitygate.ResourceStatePartial,
			ProblemTicks: 1, Problems: []string{"swap: sysctl vm.swapusage: operation not permitted"},
		},
		Samples: []qualitygate.HostResourceSample{
			{At: time.Unix(6100, 0).UTC(), ElapsedMS: 0, Memory: &qualitygate.MemorySample{UsedBytes: 1 << 30, AvailableBytes: 2 << 30}, Swap: &qualitygate.SwapSample{TotalBytes: 4 << 30, FreeBytes: 4 << 30}},
			{At: time.Unix(6105, 0).UTC(), ElapsedMS: 5000, CPU: &qualitygate.CPUSample{BusyPercent: 41.5, UserPercent: 20.1, SystemPercent: 21.4, IdlePercent: 58.5}, Memory: &qualitygate.MemorySample{UsedBytes: 2 << 30, AvailableBytes: 1 << 30}, Swap: &qualitygate.SwapSample{TotalBytes: 4 << 30, FreeBytes: 4 << 30}},
			{At: time.Unix(6110, 0).UTC(), ElapsedMS: 10000, CPU: &qualitygate.CPUSample{BusyPercent: 55.25, UserPercent: 30.1, SystemPercent: 25.15, IdlePercent: 44.75}, Memory: &qualitygate.MemorySample{UsedBytes: 3 << 30, AvailableBytes: 1 << 30}},
		},
	}
}

func TestExportEvidenceExposesResourceSamples(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	run := regressionFixtureRun(repoRoot, "abababababababababababababababab", time.Unix(6100, 0).UTC())
	fixture.writeModuleValidationRun(t, run)
	resources := gateRunResourceFixture(run.ValidationRunID)
	data, err := qualitygate.EncodeResourceSamples(resources)
	if err != nil {
		t.Fatal(err)
	}
	writeEvidenceGateResourcesContent(t, fixture, run.ValidationRunID, string(data))
	writeEvidenceGateTimings(t, fixture, run, capturedFixtureTimings([]qualitygate.TestTiming{
		{Package: "example.com/m", Test: "TestSlow", Action: "pass", ElapsedMS: 100},
	}))
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entryPath := "live/validation/quality-gate/" + run.ValidationRunID + "/" + qualitygate.ResourceSamplesFile
	if !exportHasEntry(export.Manifest, entryPath) {
		t.Fatalf("export lacks the resource samples entry %s", entryPath)
	}
	bundled, err := qualitygate.DecodeResourceSamples(exportFileByPath(t, export, entryPath))
	if err != nil {
		t.Fatalf("bundled resource samples do not decode: %v", err)
	}
	if bundled.Coverage.Status != qualitygate.ResourceCoveragePartial || len(bundled.Samples) != 3 {
		t.Fatalf("bundled resource samples = %#v", bundled)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, analysisRun := range analysis.CanonicalValidationRuns {
		if analysisRun.RunID == run.ValidationRunID {
			if analysisRun.ResourceSamples != entryPath {
				t.Fatalf("analysis run entry resource link = %q", analysisRun.ResourceSamples)
			}
			linked = true
		}
	}
	if !linked {
		t.Fatalf("analysis index lacks the run entry: %#v", analysis.CanonicalValidationRuns)
	}
}

func TestExportEvidenceDisclosesCorruptGateRunResources(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := regressionFixtureRun(repoRoot, "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd", time.Unix(6110, 0).UTC())
	foreign := regressionFixtureRun(repoRoot, "efefefefefefefefefefefefefefefef", time.Unix(6120, 0).UTC())
	absent := regressionFixtureRun(repoRoot, "90909090909090909090909090909090", time.Unix(6130, 0).UTC())
	for _, run := range []qualitygate.RunRecord{corrupt, foreign, absent} {
		fixture.writeModuleValidationRun(t, run)
	}
	writeEvidenceGateResourcesContent(t, fixture, corrupt.ValidationRunID, "not-json")
	foreignResources := gateRunResourceFixture("12121212121212121212121212121212")
	foreignData, err := qualitygate.EncodeResourceSamples(foreignResources)
	if err != nil {
		t.Fatal(err)
	}
	writeEvidenceGateResourcesContent(t, fixture, foreign.ValidationRunID, string(foreignData))
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	corruptEntry := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+corrupt.ValidationRunID+"/"+qualitygate.ResourceSamplesFile)
	if corruptEntry.Unreadable == "" || corruptEntry.Missing {
		t.Fatalf("corrupt resource samples entry = %#v", corruptEntry)
	}
	foreignEntry := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+foreign.ValidationRunID+"/"+qualitygate.ResourceSamplesFile)
	if foreignEntry.Unreadable == "" || foreignEntry.Missing {
		t.Fatalf("foreign-identity resource samples entry = %#v", foreignEntry)
	}
	absentEntry := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+absent.ValidationRunID+"/"+qualitygate.ResourceSamplesFile)
	if !absentEntry.Missing || absentEntry.Unreadable != "" {
		t.Fatalf("absent resource samples entry stayed implicit: %#v", absentEntry)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	for _, analysisRun := range analysis.CanonicalValidationRuns {
		if (analysisRun.RunID == corrupt.ValidationRunID || analysisRun.RunID == foreign.ValidationRunID ||
			analysisRun.RunID == absent.ValidationRunID) && analysisRun.ResourceSamples != "" {
			t.Fatalf("corrupt resource samples were linked: %#v", analysisRun)
		}
	}
}
