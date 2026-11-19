package controller

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExportEvidenceLiveAttemptCollectsRawSections(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	objectsBefore := evidenceObjectTreeDigest(t, fixture.store)
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded EvidenceExportManifest
	if err := json.Unmarshal(exportFileByPath(t, export, "manifest.json"), &decoded); err != nil {
		t.Fatal(err)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	telemetry := exportEntryByPath(t, export.Manifest, "live/task/telemetry.jsonl")
	transcript := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/session-worker/0")
	missing := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/session-reviewer")
	unsafe := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/../escape")
	artifact := exportEntryByPath(t, export.Manifest, "live/task/artifacts/report.txt")
	staged := exportFileByPath(t, export, "live/git/diff-staged.patch")
	untracked := exportEntryByPath(t, export.Manifest, "live/git/untracked/untracked-note.txt")
	plan := exportEntryByPath(t, export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath)
	gate := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+evidenceExportGateRunID+"/run.json")
	smoke := exportEntryByPath(t, export.Manifest, "live/validation/install-smoke/"+evidenceExportSmokeRunID+"/smoke.log")
	smokeMeta := exportEntryByPath(t, export.Manifest, "live/validation/install-smoke/"+evidenceExportSmokeRunID+"/run.json")
	for _, check := range []struct {
		name   string
		valid  bool
		actual any
	}{
		{"manifest-round-trip/identity", decoded.Format == evidenceExportFormat && decoded.SchemaVersion == evidenceSchemaVersion, decoded},
		{"manifest-round-trip/analysis-index", decoded.AnalysisIndex.Path == "analysis-index.json" && decoded.AnalysisIndex.SHA256 == digestBytes(exportFileByPath(t, export, "analysis-index.json")), decoded.AnalysisIndex},
		{"live-result/coverage", result.RuntimeStatus == evidenceExportRuntimeCollected && result.Coverage == evidenceExportCoveragePartial, result},
		{"live-result/attempt-absent", result.AttemptSectionStatus == evidenceExportAttemptAbsent && export.Manifest.Attempt.AbsenceReason != "", []any{result.AttemptSectionStatus, export.Manifest.Attempt}},
		{"live-result/target", result.Target.Live && result.Target.AttemptID == evidenceExportLiveAttemptID && result.Target.Kind == evidenceExportTargetRunning, result.Target},
		{"live-result/selection", result.Target.RuntimeTaskID == fixture.runtimeTask && result.Target.SelectionBasis == evidenceExportBasisLiveAttempt, result.Target},
		{"manifest-digest", result.ManifestDigest == digestBytes(exportFileByPath(t, export, "manifest.json")), result.ManifestDigest},
		{"telemetry/window", telemetry.TrailingFragment && telemetry.Records == 1 && telemetry.InProgress, telemetry},
		{"telemetry/digest", telemetry.SHA256 == digestBytes(exportFileByPath(t, export, "live/task/telemetry.jsonl")), telemetry},
		{"transcript/digest", transcript.SHA256 == digestBytes([]byte(`{"timestamp":"1970-01-01T02:00:01Z","line":"in-window"}`+"\n")), transcript},
		{"transcript/window", transcript.Window != nil && transcript.Window.RecordsBefore == 1 && transcript.Window.Basis == evidenceWindowBasisTimestamp, transcript.Window},
		{"transcript/missing", missing.Missing, missing},
		{"transcript/unsafe", unsafe.Unreadable != "" && !unsafe.Missing, unsafe},
		{"artifact/digest", artifact.SHA256 == digestBytes([]byte("artifact bytes\n")), artifact},
		{"git/staged", bytes.Contains(staged, []byte("source.txt")), string(staged)},
		{"git/untracked", untracked.SHA256 == digestBytes([]byte("untracked bytes\n")), untracked},
		{"instructions/plan", plan.Basis == evidenceExportBasisWorkspaceObservation && plan.SHA256 != "", plan},
		{"instructions/rules", exportHasEntry(export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionRulesPath), export.Manifest.Entries},
		{"instructions/history", exportHasEntry(export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionHistoryPath), export.Manifest.Entries},
		{"git/audit", export.Manifest.Git != nil && export.Manifest.Git.ExecutionBaseOID != "" && export.Manifest.Git.Snapshot != nil, export.Manifest.Git},
		{"analysis/sessions", analysis.Sessions.Status == evidenceAnalysisStatusCollected && len(analysis.Sessions.Sessions) > 0, analysis.Sessions},
		{"analysis/window", !analysis.Window.Start.IsZero() && !analysis.Window.End.IsZero(), analysis.Window},
		{"analysis/instructions", analysis.Instructions.Status == evidenceAnalysisStatusCollected && analysis.Instructions.Task != nil && analysis.Instructions.Task.Entry == "live/task/authority.md" && analysis.Instructions.Plan != nil && analysis.Instructions.Plan.Entry == "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath && analysis.Instructions.Rules != nil && analysis.Instructions.Rules.SHA256 != "" && analysis.Instructions.History != nil && analysis.Instructions.History.SHA256 != "", analysis.Instructions},
		{"validation/gate-metadata", gate.Basis == evidenceExportBasisRuntimeValidation && !gate.Missing && gate.Unreadable == "", gate},
		{"validation/gate-log", string(exportFileByPath(t, export, "live/validation/quality-gate/"+evidenceExportGateRunID+"/gate.log")) == "gate log for "+evidenceExportGateRunID+"\n", gate},
		{"validation/smoke-log", !smoke.Missing && smoke.Unreadable == "", smoke},
		{"validation/smoke-metadata", smokeMeta.Basis == evidenceExportBasisRuntimeValidation && !smokeMeta.Missing, smokeMeta},
		{"validation/canonical-gate", analysisValidationRunPresent(analysis.CanonicalValidationRuns, evidenceExportGateRunID, evidenceExportBasisRuntimeValidation), analysis.CanonicalValidationRuns},
		{"validation/canonical-smoke", analysisValidationRunPresent(analysis.CanonicalValidationRuns, evidenceExportSmokeRunID, evidenceExportBasisRuntimeValidation), analysis.CanonicalValidationRuns},
	} {
		t.Run(check.name, func(t *testing.T) {
			if !check.valid {
				t.Fatalf("%s = %#v", check.name, check.actual)
			}
		})
	}
	for _, file := range export.Files {
		if strings.Contains(file.Path, "..") || strings.Contains(file.Path, `\`) || strings.HasPrefix(file.Path, "/") {
			t.Fatalf("export archive entry escapes its prefix: %s", file.Path)
		}
	}
	for _, entry := range export.Manifest.Entries {
		if entry.Path == "live/git/object-archive.json" {
			t.Fatal("live export embedded a git object archive payload")
		}
	}
	gateRows := 0
	for _, run := range analysis.CanonicalValidationRuns {
		if run.Basis == evidenceAnalysisBasisBlockObservation || run.Basis == evidenceAnalysisBasisEvents ||
			(run.RunID == evidenceExportGateRunID && run.Basis == evidenceExportBasisRuntimeValidation && run.Binding != evidenceValidationBindingTaskStore) ||
			(run.Log != "" && !exportHasEntry(export.Manifest, run.Log)) {
			t.Fatalf("canonical validation run has invalid classification, binding or archive log: %#v", run)
		}
		if run.RunID == evidenceExportGateRunID {
			gateRows++
		}
	}
	eventObserved, mergedDuplicates := false, 0
	for _, run := range analysis.ObservedValidationActions {
		if run.Basis == evidenceAnalysisBasisEvents {
			eventObserved = run.RunID == evidenceExportGateRunID && run.Log == "" && run.Result == "pass"
			continue
		}
		if run.Basis != evidenceAnalysisBasisBlockObservation || run.Attempt != state.ValidationAttemptRetry || run.Result != "unknown" {
			t.Fatalf("observed validation action = %#v", run)
		}
		if run.SnapshotID == "snapshot-block-2" {
			mergedDuplicates = run.Occurrences
		}
	}
	for _, check := range []struct {
		name   string
		valid  bool
		actual any
	}{
		{"validation/canonical-uniqueness", gateRows == 1, analysis.CanonicalValidationRuns},
		{"validation/event-relation", eventObserved, analysis.ObservedValidationActions},
		{"validation/duplicate-provenance", mergedDuplicates == 2 && len(analysis.ObservedValidationActions) == 3, analysis.ObservedValidationActions},
		{"validation/retries", len(analysis.Retries.ValidationReruns) > 0, analysis.Retries},
		{"validation/in-progress", len(analysis.EvidenceStates.InProgress) > 0, analysis.EvidenceStates},
		{"export/read-only", evidenceObjectTreeDigest(t, fixture.store) == objectsBefore, fixture.store.dir},
	} {
		t.Run(check.name, func(t *testing.T) {
			if !check.valid {
				t.Fatalf("%s = %#v", check.name, check.actual)
			}
		})
	}
	t.Run("task-id-live-binding", func(t *testing.T) {
		_, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: fixture.runtimeTask})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Target.Live || result.Target.SelectionBasis != evidenceExportBasisLiveBinding {
			t.Fatalf("live task id target = %#v", result.Target)
		}
	})
}

func analysisValidationRunPresent(runs []EvidenceAnalysisValidationRun, runID, basis string) bool {
	for _, run := range runs {
		if run.RunID == runID && run.Basis == basis && run.Log != "" {
			return true
		}
	}
	return false
}

func TestExportEvidenceBindsRepositoryScopedValidationRuns(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	inWindow := time.Unix(6100, 0).UTC()
	inWindowEnd := time.Unix(6200, 0).UTC()
	beforeAttempt := time.Unix(100, 0).UTC()
	beforeAttemptEnd := time.Unix(200, 0).UTC()
	foreignRepo := t.TempDir()
	boundRun := qualitygate.RunRecord{
		ValidationRunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Form: "go-test",
		Repository: repoRoot, WorkingDir: filepath.Join(repoRoot, "glm-worker"),
		StartedAt: inWindow, CompletedAt: &inWindowEnd, Status: qualitygate.StatusPass,
	}
	foreignRun := boundRun
	foreignRun.ValidationRunID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	foreignRun.Repository = foreignRepo
	foreignRun.WorkingDir = filepath.Join(foreignRepo, "glm-worker")
	staleRun := boundRun
	staleRun.ValidationRunID = "cccccccccccccccccccccccccccccccc"
	staleRun.StartedAt = beforeAttempt
	staleRun.CompletedAt = &beforeAttemptEnd
	otherTaskRun := boundRun
	otherTaskRun.ValidationRunID = "dddddddddddddddddddddddddddddddd"
	otherTaskRun.TaskID = "another-runtime-task"
	for _, run := range []qualitygate.RunRecord{boundRun, foreignRun, staleRun, otherTaskRun} {
		fixture.writeModuleValidationRun(t, run)
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]EvidenceExportEntry{}
	for _, entry := range export.Manifest.Entries {
		entries[entry.Path] = entry
	}
	boundEntry, ok := entries["live/validation/quality-gate/"+boundRun.ValidationRunID+"/run.json"]
	if !ok || boundEntry.Basis != evidenceExportBasisRepositoryWorkspace || boundEntry.Missing {
		t.Fatalf("repository-bound gate run entry = %#v ok=%v", boundEntry, ok)
	}
	if !exportHasEntry(export.Manifest, "live/validation/quality-gate/"+boundRun.ValidationRunID+"/gate.log") {
		t.Fatal("repository-bound gate log entry is missing")
	}
	for _, excluded := range []string{foreignRun.ValidationRunID, staleRun.ValidationRunID, otherTaskRun.ValidationRunID} {
		if exportHasEntry(export.Manifest, "live/validation/quality-gate/"+excluded+"/run.json") {
			t.Fatalf("export absorbed unbound gate run %s", excluded)
		}
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	if !analysisValidationRunPresent(analysis.CanonicalValidationRuns, boundRun.ValidationRunID, evidenceExportBasisRepositoryWorkspace) {
		t.Fatalf("analysis miss the repository-bound run: %#v", analysis.CanonicalValidationRuns)
	}
	for _, run := range analysis.CanonicalValidationRuns {
		if run.RunID == boundRun.ValidationRunID && run.Binding != evidenceValidationBindingWorkspaceWindow {
			t.Fatalf("repository-bound run binding = %#v", run)
		}
	}
}

func TestExportEvidenceAggregatesParentEvidenceRepetitions(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	ledger := evidenceTestParentEvidenceLedger(t, time.Unix(6500, 0).UTC())
	if err := os.WriteFile(fixture.runtime.Path(evidenceExportParentEvidenceFile), ledger, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"parent-evidence-ledger.lock", "worker.ready"} {
		if err := os.WriteFile(fixture.runtime.Path(name), []byte("ephemeral\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entry := exportEntryByPath(t, export.Manifest, "live/state/parent-evidence.aggregate.json")
	if entry.Missing || entry.Unreadable != "" || entry.OmittedPayload == "" || entry.Basis != evidenceExportBasisLiveRuntime {
		t.Fatalf("parent evidence aggregate entry = %#v", entry)
	}
	if entry.SHA256 != digestBytes(exportFileByPath(t, export, "live/state/parent-evidence.aggregate.json")) {
		t.Fatal("aggregate entry digest does not match archived bytes")
	}
	var aggregate evidenceParentEvidenceAggregate
	if err := json.Unmarshal(exportFileByPath(t, export, "live/state/parent-evidence.aggregate.json"), &aggregate); err != nil {
		t.Fatal(err)
	}
	if aggregate.Records != 6 || aggregate.GroupCount != 4 || aggregate.RepetitionsOmitted != 2 ||
		aggregate.Outcomes[state.ParentEvidenceOutcomeProjected] != 3 ||
		aggregate.Outcomes[state.ParentEvidenceOutcomeDuplicate] != 2 ||
		aggregate.Outcomes[state.ParentEvidenceOutcomeError] != 1 ||
		aggregate.SourceSHA256 != digestBytes(ledger) || aggregate.SourceBytes != int64(len(ledger)) {
		t.Fatalf("parent evidence aggregate = %#v", aggregate)
	}
	if len(aggregate.Groups) != 4 || aggregate.Groups[0].Count != 2 ||
		aggregate.Groups[0].LastTime.IsZero() || aggregate.Groups[2].Outcome != state.ParentEvidenceOutcomeDuplicate {
		t.Fatalf("aggregate groups lost transitions or counts: %#v", aggregate.Groups)
	}
	if !exportHasEntry(export.Manifest, "live/state/worker.id") {
		t.Fatal("aggregate projection dropped the canonical worker identity file")
	}
	for _, path := range []string{
		"live/state/parent-evidence.jsonl",
		"live/state/parent-evidence-ledger.lock",
		"live/state/worker.ready",
	} {
		if exportHasEntry(export.Manifest, path) {
			t.Fatalf("export embedded %s", path)
		}
	}
}
