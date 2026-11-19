package controller

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExportEvidenceDecodedRoundsCoverageAgreesWithWorkflow(t *testing.T) {
	cases := []struct {
		name   string
		round  func(string) string
		status string
		count  int
	}{
		{
			name:   "valid",
			round:  func(id string) string { return exportValidRound(id, 1) },
			status: evidenceAnalysisStatusCollected, count: 1,
		},
		{
			name:   "malformed_suffix",
			round:  func(id string) string { return exportValidRound(id, 1) + "{broken\n" },
			status: evidenceAnalysisStatusPartial,
		},
		{
			name:   "foreign_task",
			round:  func(string) string { return exportValidRound("foreign-task", 1) },
			status: evidenceAnalysisStatusPartial,
		},
		{
			name:   "invalid_sequence",
			round:  func(id string) string { return exportValidRound(id, 0) },
			status: evidenceAnalysisStatusPartial,
		},
		{
			name:   "duplicate_sequence",
			round:  func(id string) string { return exportValidRound(id, 1) + exportValidRound(id, 1) },
			status: evidenceAnalysisStatusPartial,
		},
		{
			name: "zero_timestamp",
			round: func(id string) string {
				return `{"task_id":"` + id + `","seq":1,"captured_at":"0001-01-01T00:00:00Z"}` + "\n"
			},
			status: evidenceAnalysisStatusPartial,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newEvidenceExportFixture(t)
			overwriteLiveTelemetry(t, fixture, analysisTelemetryCall(fixture, "test-call", state.CallTypeTask, state.WorkerRole, "session-worker", time.Unix(6100, 0).UTC()))
			path := fixture.runtime.RoundLogPath(fixture.runtimeTask)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.round(fixture.runtimeTask)), 0600); err != nil {
				t.Fatal(err)
			}
			export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
			if err != nil {
				t.Fatal(err)
			}
			index := decodeAnalysisIndex(t, export)
			if index.ReviewRounds.Status != tc.status || index.ReviewRounds.Count != tc.count ||
				index.Workflow.Interactions.ReviewRounds != tc.count || index.Workflow.Status != tc.status {
				t.Fatalf("round source coverage mismatch: analysis=%#v workflow=%#v", index.ReviewRounds, index.Workflow)
			}
			unknown := strings.Join(index.Workflow.Unknown, " ")
			if (tc.count == 0 && !strings.Contains(unknown, "review rounds unavailable or malformed")) ||
				(tc.count > 0 && strings.Contains(unknown, "review rounds unavailable or malformed")) {
				t.Fatalf("rounds uncertainty did not match coverage: %s", unknown)
			}
			if index.ModelCalls.Status != evidenceAnalysisStatusCollected || index.ModelCalls.Total != 1 {
				t.Fatalf("valid independent telemetry was discarded: %#v", index.ModelCalls)
			}
		})
	}
}

func exportValidRound(taskID string, seq int) string {
	return `{"version":1,"task_id":"` + taskID + `","seq":` + strconv.Itoa(seq) + `,"review_number":1,"auto_fixes":0,"captured_at":"2026-10-06T00:03:00Z"}` + "\n"
}

func TestExportEvidenceDecodedTelemetryRejectsForeignTask(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	foreign := analysisTelemetryCall(fixture, "foreign", state.CallTypeTask, state.WorkerRole, "session-foreign", time.Unix(6100, 0).UTC())
	foreign.TaskID = "foreign-task"
	overwriteLiveTelemetry(t, fixture, foreign)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	index := decodeAnalysisIndex(t, export)
	if index.ModelCalls.Status != evidenceAnalysisStatusPartial || index.ModelCalls.Total != 0 ||
		len(index.Workflow.ModelStages) != 0 || index.Retries.ResumedModelCalls != 0 ||
		index.Workflow.Status != evidenceAnalysisStatusPartial {
		t.Fatalf("cross-task telemetry reached a projection: model=%#v workflow=%#v", index.ModelCalls, index.Workflow)
	}
	if !strings.Contains(strings.Join(index.Workflow.Unknown, " "), "worker model telemetry unavailable or malformed") {
		t.Fatalf("workflow omitted telemetry uncertainty: %#v", index.Workflow.Unknown)
	}
}

func TestExportEvidenceDecodedEventsRejectPartialAndForeign(t *testing.T) {
	for _, suffix := range []string{"{broken\n", `{"version":1,"task_id":"foreign-task","seq":99,"timestamp":"2026-10-06T00:05:00Z","kind":"user"}` + "\n"} {
		t.Run(strconv.Itoa(len(suffix)), func(t *testing.T) {
			fixture := newEvidenceExportFixture(t)
			path := fixture.runtime.TaskEventLogPath(fixture.runtimeTask)
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = file.WriteString(suffix)
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
			if err != nil {
				t.Fatal(err)
			}
			index := decodeAnalysisIndex(t, export)
			if len(index.ObservedValidationActions) != 0 {
				t.Fatalf("partial or foreign task-events counted as valid: %#v", index.ObservedValidationActions)
			}
		})
	}
}

func TestExportEvidenceDecodedSnapshotSingleSharedResult(t *testing.T) {
	b := &evidenceExportBuilder{
		observedAt:     time.Now().UTC(),
		attemptSection: EvidenceExportAttemptSection{RuntimeTaskID: "this-task"},
		files:          []EvidenceExportFile{{Path: "attempt/rounds.jsonl", Data: []byte(exportValidRound("this-task", 1))}},
	}
	first := b.exportDecodedEvidence()
	if b.exportDecodedEvidence() != first || first.rounds.status != evidenceAnalysisStatusCollected ||
		len(first.rounds.records) != 1 {
		t.Fatalf("builder did not reuse a valid decoded export source: %#v", first)
	}
	b2 := &evidenceExportBuilder{
		observedAt:     b.observedAt,
		attemptSection: b.attemptSection,
		files:          b.files,
		entries:        []EvidenceExportEntry{{Path: "attempt/rounds.jsonl", TrailingFragment: true}},
	}
	partial := b2.exportDecodedEvidence().rounds
	if partial.status != evidenceAnalysisStatusPartial || len(partial.records) != 0 {
		t.Fatalf("truncated export promoted to complete: %#v", partial)
	}
}

func TestExportEvidenceDecodedEventsPreserveUnsequencedValidation(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	fixture.runtime.RecordValidationEvent(state.TaskValidationEvent{
		Form:            "go-test",
		ValidationRunID: "unsequenced-canonical-validation",
		Result:          "pass",
	})
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	index := decodeAnalysisIndex(t, export)
	for _, validation := range index.ObservedValidationActions {
		if validation.RunID == "unsequenced-canonical-validation" {
			return
		}
	}
	t.Fatalf("canonical zero-sequence validation event was lost: %#v", index.ObservedValidationActions)
}

func TestExportEvidenceDecodedTelemetryRejectsInvalidTime(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	started := time.Unix(6100, 0).UTC()
	call := analysisTelemetryCall(fixture, "reversed", state.CallTypeTask, state.WorkerRole, "session-worker", started)
	call.CompletedAt = started.Add(-time.Second)
	overwriteLiveTelemetry(t, fixture, call)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	index := decodeAnalysisIndex(t, export)
	if index.ModelCalls.Status != evidenceAnalysisStatusPartial || index.ModelCalls.Total != 0 ||
		index.Workflow.Status != evidenceAnalysisStatusPartial || len(index.Workflow.ModelStages) != 0 {
		t.Fatalf("invalid timestamp reached a projection: model=%#v workflow=%#v", index.ModelCalls, index.Workflow)
	}
}

func TestExportEvidenceUnreadableSourceKeepsPartialCoverage(t *testing.T) {
	b := &evidenceExportBuilder{
		observedAt:     time.Now().UTC(),
		attemptSection: EvidenceExportAttemptSection{RuntimeTaskID: "this-task"},
		entries: []EvidenceExportEntry{
			{Path: "live/task/rounds.jsonl", Unreadable: "test read failure"},
		},
		files: []EvidenceExportFile{
			{Path: "attempt/rounds.jsonl", Data: []byte(exportValidRound("this-task", 1))},
		},
	}
	rounds := b.analysisReviewRounds()
	workflow := b.analysisWorkflow(EvidenceExportRuntimeSection{})
	if rounds.Status != evidenceAnalysisStatusPartial || rounds.Count != 0 || workflow.Status != evidenceAnalysisStatusPartial {
		t.Fatalf("unreadable evidence was reclassified: rounds=%#v workflow=%#v", rounds, workflow)
	}
}
