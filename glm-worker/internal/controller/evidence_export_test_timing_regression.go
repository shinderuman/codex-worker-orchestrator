package controller

import (
	"bytes"
	"math"
	"path/filepath"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

type EvidenceAnalysisTestTimingRegression struct {
	Status            string                                 `json:"status"`
	Basis             string                                 `json:"basis"`
	Runs              int                                    `json:"runs"`
	Conditions        EvidenceAnalysisTestTimingConditions   `json:"conditions"`
	Comparisons       []EvidenceAnalysisTestTimingComparison `json:"comparisons,omitempty"`
	IdentityConflicts []string                               `json:"identity_conflicts,omitempty"`
}

type EvidenceAnalysisTestTimingConditions struct {
	RatioThreshold float64 `json:"ratio_threshold"`
	DeltaFloorMS   int64   `json:"delta_floor_ms"`
	Candidates     int     `json:"candidates_bound"`
	ComparedPairs  string  `json:"compared_pairs"`
	Uncaptured     string  `json:"uncaptured"`
}

type EvidenceAnalysisTestTimingComparison struct {
	Form       string                                `json:"form"`
	WorkingDir string                                `json:"working_dir"`
	Baseline   EvidenceAnalysisTestTimingRunSide     `json:"baseline"`
	Latest     EvidenceAnalysisTestTimingRunSide     `json:"latest"`
	Candidates []EvidenceAnalysisTestTimingCandidate `json:"candidates,omitempty"`
	Unknown    string                                `json:"unknown,omitempty"`
}

type EvidenceAnalysisTestTimingRunSide struct {
	RunID      string                                    `json:"run_id"`
	Repository string                                    `json:"repository"`
	WorkingDir string                                    `json:"working_dir"`
	Head       string                                    `json:"head,omitempty"`
	TaskID     string                                    `json:"task_id,omitempty"`
	Result     string                                    `json:"result"`
	StartedAt  time.Time                                 `json:"started_at"`
	Conditions *EvidenceAnalysisTestTimingConditionsSide `json:"conditions,omitempty"`
}

type EvidenceAnalysisTestTimingConditionsSide = qualitygate.RunToolchain

type EvidenceAnalysisTestTimingCandidate struct {
	Package    string  `json:"package"`
	Test       string  `json:"test"`
	BaselineMS int64   `json:"baseline_ms"`
	LatestMS   int64   `json:"latest_ms"`
	DeltaMS    int64   `json:"delta_ms"`
	Ratio      float64 `json:"ratio,omitempty"`
}

type testTimingGroupKey struct {
	form       string
	workingDir string
}

type testTimingKey struct {
	pkg  string
	test string
}

const evidenceAnalysisBasisTestTimings = "validation-test-timings"

const (
	testTimingRegressionRatioThreshold  = 1.5
	testTimingRegressionDeltaFloorMS    = int64(500)
	testTimingRegressionCandidatesBound = 20
)

func (b *evidenceExportBuilder) analysisTestTimingRegression() EvidenceAnalysisTestTimingRegression {
	regression := EvidenceAnalysisTestTimingRegression{
		Status: evidenceAnalysisStatusAbsent,
		Basis:  evidenceAnalysisBasisTestTimings,
		Conditions: EvidenceAnalysisTestTimingConditions{
			RatioThreshold: testTimingRegressionRatioThreshold,
			DeltaFloorMS:   testTimingRegressionDeltaFloorMS,
			Candidates:     testTimingRegressionCandidatesBound,
			ComparedPairs:  "latest run vs nearest older run with equal recorded toolchain conditions per form and repository-relative module; pass-to-pass tests only",
			Uncaptured:     "machine load, CPU model, and cache warmth are not captured; runs without recorded toolchain conditions never form pairs",
		},
	}
	groups, conflicts := collectTestTimingGroups(b.validationRuns)
	regression.IdentityConflicts = conflicts
	for _, runs := range groups {
		regression.Runs += len(runs)
	}
	if regression.Runs == 0 && len(conflicts) == 0 {
		return regression
	}
	regression.Status = evidenceAnalysisStatusCollected
	if len(conflicts) > 0 {
		regression.Status = evidenceAnalysisStatusPartial
	}
	for _, key := range sortedTestTimingGroupKeys(groups) {
		regression.Comparisons = append(regression.Comparisons, testTimingComparison(key.form, key.workingDir, groups[key]))
	}
	return regression
}

func collectTestTimingGroups(validationRuns []collectedValidationRun) (map[testTimingGroupKey][]collectedValidationRun, []string) {
	converged, conflicts := convergeValidationRunIdentities(validationRuns)
	groups := map[testTimingGroupKey][]collectedValidationRun{}
	for _, collected := range converged {
		if collected.run.Form != packet.ParentValidationGoTest && collected.run.Form != packet.ParentValidationGoTestRace {
			continue
		}
		module := testTimingModule(collected.run)
		key := testTimingGroupKey{form: collected.run.Form, workingDir: module}
		groups[key] = append(groups[key], collected)
	}
	return groups, sortedConflictedRunIDs(conflicts)
}

func convergeValidationRunIdentities(runs []collectedValidationRun) ([]collectedValidationRun, map[string]struct{}) {
	conflicts := map[string]struct{}{}
	byID := map[string][]collectedValidationRun{}
	var order []string
	unidentified := []collectedValidationRun{}
	for _, run := range runs {
		id := run.run.ValidationRunID
		if id == "" {
			unidentified = append(unidentified, run)
			continue
		}
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = append(byID[id], run)
	}
	converged := make([]collectedValidationRun, 0, len(runs))
	for _, id := range order {
		copies := byID[id]
		merged, ok := mergeValidationRunCopies(copies)
		if !ok {
			conflicts[id] = struct{}{}
			continue
		}
		converged = append(converged, merged)
	}
	return append(converged, unidentified...), conflicts
}

func mergeValidationRunCopies(copies []collectedValidationRun) (collectedValidationRun, bool) {
	merged := copies[0]
	for _, copy := range copies[1:] {
		if !mergeValidationRunCopy(&merged, copy) {
			return collectedValidationRun{}, false
		}
	}
	return merged, true
}

func mergeValidationRunCopy(merged *collectedValidationRun, copy collectedValidationRun) bool {
	if !sameRunRecordEncoding(merged.run, copy.run) {
		return false
	}
	if copy.timings != nil {
		if merged.timings == nil {
			merged.timings = copy.timings
			merged.timingsEntry = copy.timingsEntry
		} else if !sameTestTimingsEncoding(*merged.timings, *copy.timings) {
			return false
		}
	}
	if copy.resources == nil {
		return true
	}
	if merged.resources == nil {
		merged.resources = copy.resources
		merged.resourcesEntry = copy.resourcesEntry
		return true
	}
	return sameResourceSamplesEncoding(merged.resources, copy.resources)
}

func sameResourceSamplesEncoding(left, right *qualitygate.ResourceSamples) bool {
	encodedLeft, err := qualitygate.EncodeResourceSamples(*left)
	if err != nil {
		return false
	}
	encodedRight, err := qualitygate.EncodeResourceSamples(*right)
	if err != nil {
		return false
	}
	return bytes.Equal(encodedLeft, encodedRight)
}

func sameRunRecordEncoding(left, right qualitygate.RunRecord) bool {
	encodedLeft, err := qualitygate.Encode(left)
	if err != nil {
		return false
	}
	encodedRight, err := qualitygate.Encode(right)
	if err != nil {
		return false
	}
	return bytes.Equal(encodedLeft, encodedRight)
}

func sameTestTimingsEncoding(left, right qualitygate.TestTimings) bool {
	encodedLeft, err := qualitygate.EncodeTestTimings(left)
	if err != nil {
		return false
	}
	encodedRight, err := qualitygate.EncodeTestTimings(right)
	if err != nil {
		return false
	}
	return bytes.Equal(encodedLeft, encodedRight)
}

func sortedConflictedRunIDs(conflicts map[string]struct{}) []string {
	ids := make([]string, 0, len(conflicts))
	for id := range conflicts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func testTimingModule(run qualitygate.RunRecord) string {
	if !filepath.IsAbs(run.Repository) || !filepath.IsAbs(run.WorkingDir) {
		return ""
	}
	module, err := filepath.Rel(run.Repository, run.WorkingDir)
	if err != nil || !validFinalValidationWorkingDir(module) {
		return ""
	}
	return filepath.ToSlash(module)
}

func sortedTestTimingGroupKeys(groups map[testTimingGroupKey][]collectedValidationRun) []testTimingGroupKey {
	keys := make([]testTimingGroupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].form != keys[j].form {
			return keys[i].form < keys[j].form
		}
		return keys[i].workingDir < keys[j].workingDir
	})
	return keys
}

func testTimingComparison(form, workingDir string, runs []collectedValidationRun) EvidenceAnalysisTestTimingComparison {
	runs = append([]collectedValidationRun(nil), runs...)
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].run.StartedAt.Equal(runs[j].run.StartedAt) {
			return runs[i].run.StartedAt.Before(runs[j].run.StartedAt)
		}
		return runs[i].run.ValidationRunID < runs[j].run.ValidationRunID
	})
	latest := runs[len(runs)-1]
	comparison := EvidenceAnalysisTestTimingComparison{
		Form:       form,
		WorkingDir: workingDir,
		Latest:     testTimingRunSide(latest),
	}
	if latest.timings == nil {
		comparison.Unknown = "latest run captured no machine-readable test events"
		return comparison
	}
	if workingDir == "" {
		comparison.Unknown = "run does not record a module within its repository checkout"
		return comparison
	}
	latestConditions := testTimingConditions(latest)
	if latestConditions == nil {
		comparison.Unknown = "latest run does not record its execution conditions"
		return comparison
	}
	if !latest.timings.Coverage.Captured || latest.timings.Coverage.UntimedTerminalEvents > 0 || latest.run.Status != qualitygate.StatusPass {
		comparison.Unknown = "latest run captured no machine-readable test events"
		return comparison
	}
	baseline, ok := nearestComparableTimingRun(runs, len(runs)-1, latestConditions)
	if !ok {
		comparison.Unknown = "no older comparable run with recorded conditions and captured test events in this export"
		return comparison
	}
	comparison.Baseline = testTimingRunSide(baseline)
	comparison.Candidates = testTimingCandidates(baseline.timings, latest.timings)
	return comparison
}

func nearestComparableTimingRun(runs []collectedValidationRun, latest int, latestConditions *qualitygate.RunToolchain) (collectedValidationRun, bool) {
	for index := latest - 1; index >= 0; index-- {
		if runs[index].run.ValidationRunID == runs[latest].run.ValidationRunID {
			continue
		}
		if conditions := testTimingConditions(runs[index]); conditions == nil || *conditions != *latestConditions {
			continue
		}
		if runs[index].timings == nil || !runs[index].timings.Coverage.Captured || runs[index].timings.Coverage.UntimedTerminalEvents > 0 || runs[index].run.Status != qualitygate.StatusPass {
			continue
		}
		return runs[index], true
	}
	return collectedValidationRun{}, false
}

func testTimingRunSide(collected collectedValidationRun) EvidenceAnalysisTestTimingRunSide {
	return EvidenceAnalysisTestTimingRunSide{
		RunID:      collected.run.ValidationRunID,
		Repository: collected.run.Repository,
		WorkingDir: collected.run.WorkingDir,
		Head:       collected.run.Head,
		TaskID:     collected.run.TaskID,
		Result:     collected.run.Status,
		StartedAt:  collected.run.StartedAt,
		Conditions: testTimingConditionsSide(collected),
	}
}

func testTimingConditionsSide(collected collectedValidationRun) *EvidenceAnalysisTestTimingConditionsSide {
	toolchain := testTimingConditions(collected)
	if toolchain == nil {
		return nil
	}
	copy := *toolchain
	return &copy
}

func testTimingConditions(collected collectedValidationRun) *qualitygate.RunToolchain {
	if collected.run.Toolchain == nil || !collected.run.Toolchain.Recorded() {
		return nil
	}
	return collected.run.Toolchain
}

func testTimingCandidates(baseline, latest *qualitygate.TestTimings) []EvidenceAnalysisTestTimingCandidate {
	base := baselineTestTimings(baseline)
	var candidates []EvidenceAnalysisTestTimingCandidate
	for _, timing := range latest.Tests {
		candidate, ok := testTimingCandidate(timing, base)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	return boundTestTimingCandidates(candidates)
}

func baselineTestTimings(baseline *qualitygate.TestTimings) map[testTimingKey]qualitygate.TestTiming {
	base := map[testTimingKey]qualitygate.TestTiming{}
	for _, timing := range baseline.Tests {
		if timing.IsPackageTiming() {
			continue
		}
		base[testTimingKey{pkg: timing.Package, test: timing.Test}] = timing
	}
	return base
}

func testTimingCandidate(timing qualitygate.TestTiming, base map[testTimingKey]qualitygate.TestTiming) (EvidenceAnalysisTestTimingCandidate, bool) {
	if timing.IsPackageTiming() || timing.Action != qualitygate.TestTimingActionPass {
		return EvidenceAnalysisTestTimingCandidate{}, false
	}
	compared, ok := base[testTimingKey{pkg: timing.Package, test: timing.Test}]
	if !ok || compared.Action != qualitygate.TestTimingActionPass {
		return EvidenceAnalysisTestTimingCandidate{}, false
	}
	delta := timing.ElapsedMS - compared.ElapsedMS
	if delta < testTimingRegressionDeltaFloorMS {
		return EvidenceAnalysisTestTimingCandidate{}, false
	}
	if compared.ElapsedMS > 0 && float64(timing.ElapsedMS) < float64(compared.ElapsedMS)*testTimingRegressionRatioThreshold {
		return EvidenceAnalysisTestTimingCandidate{}, false
	}
	return newTestTimingCandidate(timing, compared, delta), true
}

func newTestTimingCandidate(timing, compared qualitygate.TestTiming, delta int64) EvidenceAnalysisTestTimingCandidate {
	candidate := EvidenceAnalysisTestTimingCandidate{
		Package: timing.Package, Test: timing.Test,
		BaselineMS: compared.ElapsedMS, LatestMS: timing.ElapsedMS, DeltaMS: delta,
	}
	if compared.ElapsedMS > 0 {
		candidate.Ratio = math.Round(float64(timing.ElapsedMS)/float64(compared.ElapsedMS)*100) / 100
	}
	return candidate
}

func boundTestTimingCandidates(candidates []EvidenceAnalysisTestTimingCandidate) []EvidenceAnalysisTestTimingCandidate {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].DeltaMS != candidates[j].DeltaMS {
			return candidates[i].DeltaMS > candidates[j].DeltaMS
		}
		if candidates[i].Package != candidates[j].Package {
			return candidates[i].Package < candidates[j].Package
		}
		return candidates[i].Test < candidates[j].Test
	})
	if len(candidates) > testTimingRegressionCandidatesBound {
		candidates = candidates[:testTimingRegressionCandidatesBound]
	}
	return candidates
}
