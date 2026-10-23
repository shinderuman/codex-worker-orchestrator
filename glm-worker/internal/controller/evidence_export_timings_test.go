package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeEvidenceGateTimings(t *testing.T, fixture *evidenceExportFixture, record qualitygate.RunRecord, timings qualitygate.TestTimings) {
	t.Helper()
	writeEvidenceGateTimingsContent(t, fixture, record.ValidationRunID, mustEncodeTestTimings(t, record.ValidationRunID, timings))
}

func writeEvidenceGateTimingsContent(t *testing.T, fixture *evidenceExportFixture, runID, content string) {
	t.Helper()
	canonical, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	module := state.AttachStateStore(config.AppConfig{StateBase: fixture.cfg.StateBase, RepoHash: config.RepoHashFor(canonical)})
	runDir := module.Path(filepath.Join(qualitygate.RunDirectory, runID))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, qualitygate.TestTimingsFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustEncodeTestTimings(t *testing.T, runID string, timings qualitygate.TestTimings) string {
	t.Helper()
	timings.ValidationRunID = runID
	data, err := qualitygate.EncodeTestTimings(timings)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func regressionFixtureRun(repoRoot, runID string, started time.Time) qualitygate.RunRecord {
	completed := started.Add(40 * time.Second)
	return qualitygate.RunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      repoRoot,
		WorkingDir:      filepath.Join(repoRoot, "glm-worker"),
		StartedAt:       started,
		CompletedAt:     &completed,
		Status:          qualitygate.StatusPass,
		Toolchain:       regressionFixtureToolchain("go1.24.0"),
	}
}

func regressionFixtureToolchain(goVersion string) *qualitygate.RunToolchain {
	return &qualitygate.RunToolchain{
		GoVersion:           goVersion,
		GOOS:                "darwin",
		GOARCH:              "arm64",
		GoCache:             "/tmp/gocache",
		Parallelism:         8,
		Command:             "go test -json ./...",
		EnvironmentRecorded: true, TestCache: "executed",
	}
}

func TestTimingComparisonUsesRecordedModuleAcrossCheckouts(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"same-module", "other-module", "other-form", "other-toolchain", "outside-repository", "missing-repository"} {
		t.Run(mode, func(t *testing.T) {
			baseline := regressionFixtureRun("/validation/first", "11111111111111111111111111111111", time.Unix(6100, 0))
			latest := regressionFixtureRun("/validation/second", "22222222222222222222222222222222", time.Unix(6200, 0))
			switch mode {
			case "other-module":
				latest.WorkingDir = "/validation/second/other"
			case "other-form":
				latest.Form = "go-test-race"
			case "other-toolchain":
				latest.Toolchain.GoVersion = "go1.25.0"
			case "outside-repository":
				latest.WorkingDir = "/validation/foreign/glm-worker"
			case "missing-repository":
				latest.Repository = ""
			}
			oldTimings := capturedFixtureTimings([]qualitygate.TestTiming{{Package: "example.com/m", Test: "TestSlow", Action: "pass", ElapsedMS: 100}})
			newTimings := capturedFixtureTimings([]qualitygate.TestTiming{{Package: "example.com/m", Test: "TestSlow", Action: "pass", ElapsedMS: 1000}})
			b := evidenceExportBuilder{validationRuns: []collectedValidationRun{{run: baseline, timings: &oldTimings}, {run: latest, timings: &newTimings}}}
			result := b.analysisTestTimingRegression()
			compared := 0
			for _, comparison := range result.Comparisons {
				if comparison.Baseline.RunID != "" {
					compared++
					if comparison.WorkingDir != "glm-worker" || len(comparison.Candidates) != 1 || comparison.Latest.WorkingDir != latest.WorkingDir {
						t.Fatalf("recorded comparison lost module/source: %#v", comparison)
					}
				} else if comparison.Unknown == "" {
					t.Fatal("incomparable runs lack a reason")
				}
			}
			if (compared == 1) != (mode == "same-module") {
				t.Fatalf("comparison count = %d", compared)
			}
		})
	}
}

func capturedFixtureTimings(tests []qualitygate.TestTiming) qualitygate.TestTimings {
	return qualitygate.TestTimings{
		SchemaVersion: qualitygate.TestTimingsSchemaVersion,
		Form:          "go-test",
		Coverage: qualitygate.TestTimingCoverage{
			Source:         qualitygate.TestTimingSourceGoTestJSON,
			Captured:       true,
			TerminalEvents: len(tests),
		},
		Tests: tests,
	}
}

func TestExportEvidenceExposesTestTimingsAndRegressionCandidates(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	baseline := regressionFixtureRun(repoRoot, "11111111111111111111111111111111", time.Unix(6100, 0).UTC())
	latest := regressionFixtureRun(repoRoot, "22222222222222222222222222222222", time.Unix(6150, 0).UTC())
	lonelyDir := regressionFixtureRun(repoRoot, "33333333333333333333333333333333", time.Unix(6170, 0).UTC())
	lonelyDir.WorkingDir = filepath.Join(repoRoot, "other")
	for _, run := range []qualitygate.RunRecord{baseline, latest, lonelyDir} {
		fixture.writeModuleValidationRun(t, run)
	}
	baselineTests := []qualitygate.TestTiming{
		{Package: "example.com/m", Action: qualitygate.TestTimingActionPass, ElapsedMS: 1000},
		{Package: "example.com/m", Test: "TestSteady", Action: qualitygate.TestTimingActionPass, ElapsedMS: 100},
		{Package: "example.com/m", Test: "TestRegressed", Action: qualitygate.TestTimingActionPass, ElapsedMS: 200},
		{Package: "example.com/m", Test: "TestFastish", Action: qualitygate.TestTimingActionPass, ElapsedMS: 700},
		{Package: "example.com/m", Test: "TestNew", Action: qualitygate.TestTimingActionPass, ElapsedMS: 0},
	}
	latestTests := []qualitygate.TestTiming{
		{Package: "example.com/m", Action: qualitygate.TestTimingActionPass, ElapsedMS: 2000},
		{Package: "example.com/m", Test: "TestSteady", Action: qualitygate.TestTimingActionPass, ElapsedMS: 110},
		{Package: "example.com/m", Test: "TestRegressed", Action: qualitygate.TestTimingActionPass, ElapsedMS: 900},
		{Package: "example.com/m", Test: "TestFastish", Action: qualitygate.TestTimingActionPass, ElapsedMS: 1250},
		{Package: "example.com/m", Test: "TestNew", Action: qualitygate.TestTimingActionPass, ElapsedMS: 5000},
	}
	for index := 0; index < testTimingRegressionCandidatesBound+5; index++ {
		baselineTests = append(baselineTests, qualitygate.TestTiming{
			Package: "example.com/m", Test: fmt.Sprintf("TestCapped%02d", index),
			Action: qualitygate.TestTimingActionPass, ElapsedMS: 1000,
		})
		latestTests = append(latestTests, qualitygate.TestTiming{
			Package: "example.com/m", Test: fmt.Sprintf("TestCapped%02d", index),
			Action: qualitygate.TestTimingActionPass, ElapsedMS: int64(1510 + index),
		})
	}
	writeEvidenceGateTimings(t, fixture, baseline, capturedFixtureTimings(baselineTests))
	writeEvidenceGateTimings(t, fixture, latest, capturedFixtureTimings(latestTests))
	writeEvidenceGateTimings(t, fixture, lonelyDir, capturedFixtureTimings([]qualitygate.TestTiming{
		{Package: "example.com/other", Test: "TestOnly", Action: qualitygate.TestTimingActionPass, ElapsedMS: 5},
	}))
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{baseline.ValidationRunID, latest.ValidationRunID} {
		if !exportHasEntry(export.Manifest, "live/validation/quality-gate/"+runID+"/"+qualitygate.TestTimingsFile) {
			t.Fatalf("export lacks the timings entry for run %s", runID)
		}
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	var analysisLatest EvidenceAnalysisValidationRun
	for _, run := range analysis.CanonicalValidationRuns {
		if run.RunID == latest.ValidationRunID {
			analysisLatest = run
		}
	}
	if analysisLatest.Tests != len(latestTests)-1 || analysisLatest.SlowestTestMS != 5000 || analysisLatest.TestTimings == "" {
		t.Fatalf("latest run analysis entry = %#v", analysisLatest)
	}
	regression := analysis.TestTimingRegression
	if regression.Status != evidenceAnalysisStatusCollected || regression.Runs != 3 || regression.Basis != evidenceAnalysisBasisTestTimings {
		t.Fatalf("regression section = %#v", regression)
	}
	if regression.Conditions.RatioThreshold != testTimingRegressionRatioThreshold || regression.Conditions.DeltaFloorMS != testTimingRegressionDeltaFloorMS || regression.Conditions.Candidates != testTimingRegressionCandidatesBound {
		t.Fatalf("regression conditions = %#v", regression.Conditions)
	}
	var mainComparison, lonelyComparison *EvidenceAnalysisTestTimingComparison
	for index := range regression.Comparisons {
		switch regression.Comparisons[index].WorkingDir {
		case "glm-worker":
			mainComparison = &regression.Comparisons[index]
		case "other":
			lonelyComparison = &regression.Comparisons[index]
		}
	}
	if mainComparison == nil || lonelyComparison == nil {
		t.Fatalf("regression comparisons = %#v", regression.Comparisons)
	}
	if mainComparison.Baseline.RunID != baseline.ValidationRunID || mainComparison.Latest.RunID != latest.ValidationRunID ||
		mainComparison.Baseline.Result != qualitygate.StatusPass || mainComparison.Latest.Result != qualitygate.StatusPass {
		t.Fatalf("main comparison sides = %#v", mainComparison)
	}
	if len(mainComparison.Candidates) != testTimingRegressionCandidatesBound {
		t.Fatalf("candidate bound = %d candidates = %#v", len(mainComparison.Candidates), mainComparison.Candidates)
	}
	if mainComparison.Candidates[0].Test != "TestNew" || mainComparison.Candidates[0].BaselineMS != 0 || mainComparison.Candidates[0].Ratio != 0 {
		t.Fatalf("top candidate = %#v", mainComparison.Candidates[0])
	}
	candidates := map[string]EvidenceAnalysisTestTimingCandidate{}
	for _, candidate := range mainComparison.Candidates {
		candidates[candidate.Test] = candidate
	}
	regressed, ok := candidates["TestRegressed"]
	if !ok || regressed.BaselineMS != 200 || regressed.LatestMS != 900 || regressed.DeltaMS != 700 || regressed.Ratio != 4.5 {
		t.Fatalf("TestRegressed candidate = %#v", regressed)
	}
	if fastish, ok := candidates["TestFastish"]; !ok || fastish.Ratio != 1.79 {
		t.Fatalf("TestFastish candidate = %#v", fastish)
	}
	if _, ok := candidates["TestSteady"]; ok {
		t.Fatal("sub-floor delta became a regression candidate")
	}
	if _, ok := candidates["TestCapped07"]; ok {
		t.Fatal("candidate bound admitted an entry beyond the cap")
	}
	if lonelyComparison.Baseline.RunID != "" || lonelyComparison.Unknown == "" {
		t.Fatalf("lonely comparison = %#v", lonelyComparison)
	}
}

func TestExportEvidenceDisclosesCorruptGateRunTimings(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := regressionFixtureRun(repoRoot, "44444444444444444444444444444444", time.Unix(6110, 0).UTC())
	foreign := regressionFixtureRun(repoRoot, "55555555555555555555555555555555", time.Unix(6120, 0).UTC())
	for _, run := range []qualitygate.RunRecord{corrupt, foreign} {
		fixture.writeModuleValidationRun(t, run)
	}
	writeEvidenceGateTimingsContent(t, fixture, corrupt.ValidationRunID, "not-json")
	foreignTimings := qualitygate.TestTimings{SchemaVersion: qualitygate.TestTimingsSchemaVersion, Form: "go-test", Coverage: qualitygate.TestTimingCoverage{Source: qualitygate.TestTimingSourceGoTestJSON, Captured: true, TerminalEvents: 1}, Tests: []qualitygate.TestTiming{
		{Package: "example.com/m", Test: "TestForeign", Action: qualitygate.TestTimingActionPass, ElapsedMS: 5},
	}}
	foreignContent := mustEncodeTestTimings(t, "66666666666666666666666666666666", foreignTimings)
	writeEvidenceGateTimingsContent(t, fixture, foreign.ValidationRunID, foreignContent)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	corruptEntry := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+corrupt.ValidationRunID+"/"+qualitygate.TestTimingsFile)
	if corruptEntry.Unreadable == "" || corruptEntry.Missing {
		t.Fatalf("corrupt timings entry = %#v", corruptEntry)
	}
	foreignEntry := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+foreign.ValidationRunID+"/"+qualitygate.TestTimingsFile)
	if foreignEntry.Unreadable == "" || foreignEntry.Missing {
		t.Fatalf("foreign-identity timings entry = %#v", foreignEntry)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	for _, run := range analysis.CanonicalValidationRuns {
		if (run.RunID == corrupt.ValidationRunID || run.RunID == foreign.ValidationRunID) && (run.Tests != 0 || run.TestTimings != "") {
			t.Fatalf("corrupt timings were counted as collected: %#v", run)
		}
	}
	if analysis.TestTimingRegression.Status != evidenceAnalysisStatusAbsent || analysis.TestTimingRegression.Runs != 0 {
		t.Fatalf("corrupt timings fed the regression derivation: %#v", analysis.TestTimingRegression)
	}
}

func TestExportEvidenceTimingComparisonWithoutRecordedConditionsStaysUnknown(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	baseline := regressionFixtureRun(repoRoot, "77777777777777777777777777777777", time.Unix(6100, 0).UTC())
	latest := regressionFixtureRun(repoRoot, "88888888888888888888888888888888", time.Unix(6150, 0).UTC())
	latest.Toolchain = nil
	for _, run := range []qualitygate.RunRecord{baseline, latest} {
		fixture.writeModuleValidationRun(t, run)
	}
	writeEvidenceGateTimings(t, fixture, baseline, capturedFixtureTimings([]qualitygate.TestTiming{
		{Package: "example.com/m", Test: "TestSlow", Action: qualitygate.TestTimingActionPass, ElapsedMS: 100},
	}))
	writeEvidenceGateTimings(t, fixture, latest, capturedFixtureTimings([]qualitygate.TestTiming{
		{Package: "example.com/m", Test: "TestSlow", Action: qualitygate.TestTimingActionPass, ElapsedMS: 90000},
	}))
	regression := exportTestTimingRegression(t, fixture)
	if len(regression.Comparisons) != 1 {
		t.Fatalf("comparisons = %#v", regression.Comparisons)
	}
	comparison := regression.Comparisons[0]
	if comparison.Baseline.RunID != "" || comparison.Latest.RunID != latest.ValidationRunID || len(comparison.Candidates) != 0 {
		t.Fatalf("unrecorded-conditions comparison = %#v", comparison)
	}
	if comparison.Unknown != "latest run does not record its execution conditions" {
		t.Fatalf("unknown reason = %q", comparison.Unknown)
	}
	if comparison.Latest.Conditions != nil {
		t.Fatalf("unrecorded conditions were disclosed as a baseline side: %#v", comparison.Latest.Conditions)
	}
}

func TestExportEvidenceTimingComparisonSkipsIncomparableRuns(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	oldest := regressionFixtureRun(repoRoot, "99999999999999999999999999999991", time.Unix(6100, 0).UTC())
	middle := regressionFixtureRun(repoRoot, "99999999999999999999999999999992", time.Unix(6120, 0).UTC())
	middle.Toolchain = regressionFixtureToolchain("go1.25.0")
	plain := regressionFixtureRun(repoRoot, "99999999999999999999999999999993", time.Unix(6140, 0).UTC())
	latest := regressionFixtureRun(repoRoot, "99999999999999999999999999999994", time.Unix(6160, 0).UTC())
	for _, run := range []qualitygate.RunRecord{oldest, middle, plain, latest} {
		fixture.writeModuleValidationRun(t, run)
	}
	writeEvidenceGateTimings(t, fixture, oldest, capturedFixtureTimings([]qualitygate.TestTiming{
		{Package: "example.com/m", Test: "TestSlow", Action: qualitygate.TestTimingActionPass, ElapsedMS: 100},
	}))
	writeEvidenceGateTimings(t, fixture, middle, capturedFixtureTimings([]qualitygate.TestTiming{
		{Package: "example.com/m", Test: "TestSlow", Action: qualitygate.TestTimingActionPass, ElapsedMS: 100},
	}))
	writeEvidenceGateTimings(t, fixture, plain, qualitygate.TestTimings{
		SchemaVersion: qualitygate.TestTimingsSchemaVersion,
		Form:          "go-test",
		Coverage:      qualitygate.TestTimingCoverage{Source: qualitygate.TestTimingSourcePlainLog},
		Tests:         nil,
	})
	writeEvidenceGateTimings(t, fixture, latest, capturedFixtureTimings([]qualitygate.TestTiming{
		{Package: "example.com/m", Test: "TestSlow", Action: qualitygate.TestTimingActionPass, ElapsedMS: 1000},
	}))
	regression := exportTestTimingRegression(t, fixture)
	if len(regression.Comparisons) != 1 {
		t.Fatalf("comparisons = %#v", regression.Comparisons)
	}
	comparison := regression.Comparisons[0]
	if comparison.Baseline.RunID != oldest.ValidationRunID {
		t.Fatalf("nearest comparable baseline = %#v", comparison.Baseline)
	}
	if len(comparison.Candidates) != 1 || comparison.Candidates[0].Test != "TestSlow" || comparison.Candidates[0].BaselineMS != 100 || comparison.Candidates[0].LatestMS != 1000 {
		t.Fatalf("candidates = %#v", comparison.Candidates)
	}
	if comparison.Latest.Conditions == nil || comparison.Latest.Conditions.GoVersion != "go1.24.0" {
		t.Fatalf("latest conditions side = %#v", comparison.Latest.Conditions)
	}
}

func TestExportEvidenceTimingComparisonWithPlainLogLatestStaysUnknown(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	baseline := regressionFixtureRun(repoRoot, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Unix(6100, 0).UTC())
	latest := regressionFixtureRun(repoRoot, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", time.Unix(6150, 0).UTC())
	for _, run := range []qualitygate.RunRecord{baseline, latest} {
		fixture.writeModuleValidationRun(t, run)
	}
	writeEvidenceGateTimings(t, fixture, baseline, capturedFixtureTimings([]qualitygate.TestTiming{
		{Package: "example.com/m", Test: "TestSlow", Action: qualitygate.TestTimingActionPass, ElapsedMS: 100},
	}))
	writeEvidenceGateTimings(t, fixture, latest, qualitygate.TestTimings{
		SchemaVersion: qualitygate.TestTimingsSchemaVersion,
		Form:          "go-test",
		Coverage:      qualitygate.TestTimingCoverage{Source: qualitygate.TestTimingSourcePlainLog},
		Tests:         nil,
	})
	regression := exportTestTimingRegression(t, fixture)
	if len(regression.Comparisons) != 1 {
		t.Fatalf("comparisons = %#v", regression.Comparisons)
	}
	comparison := regression.Comparisons[0]
	if comparison.Baseline.RunID != "" || len(comparison.Candidates) != 0 {
		t.Fatalf("plain-log latest comparison = %#v", comparison)
	}
	if comparison.Unknown != "latest run captured no machine-readable test events" {
		t.Fatalf("unknown reason = %q", comparison.Unknown)
	}
}

func exportTestTimingRegression(t *testing.T, fixture *evidenceExportFixture) EvidenceAnalysisTestTimingRegression {
	t.Helper()
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	return analysis.TestTimingRegression
}
