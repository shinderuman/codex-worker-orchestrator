package controller

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func analysisTelemetryCall(fixture *evidenceExportFixture, callID string, callType string, role state.SessionRole, sessionID string, start time.Time) state.ModelCallLog {
	return state.ModelCallLog{
		Version: state.ModelCallLogVersion, CallID: callID, CallType: callType,
		TaskID: fixture.runtimeTask, SessionID: sessionID,
		StartedAt: start, CompletedAt: start.Add(time.Second),
		Phase: "completed", Role: role, Outcome: "success",
	}
}

func dogfoodTelemetryCalls(fixture *evidenceExportFixture) []state.ModelCallLog {
	start := time.Unix(6100, 0).UTC()
	var calls []state.ModelCallLog
	for index := 0; index < 10; index++ {
		calls = append(calls, analysisTelemetryCall(fixture, fmt.Sprintf("worker-call-%02d", index), state.CallTypeTask, state.WorkerRole, "session-worker", start.Add(time.Duration(index)*time.Minute)))
	}
	for index := 0; index < 4; index++ {
		calls = append(calls, analysisTelemetryCall(fixture, fmt.Sprintf("reviewer-call-%02d", index), state.CallTypeTask, state.ReviewerRole, "session-reviewer", start.Add(time.Duration(10+index)*time.Minute)))
	}
	calls = append(calls,
		state.ModelCallLog{
			Version: state.ModelCallLogVersion, CallID: "parent-fix-1", CallType: state.CallTypeEvent,
			TaskID: fixture.runtimeTask, StartedAt: start.Add(20 * time.Minute), CompletedAt: start.Add(20 * time.Minute),
			Phase: state.ParentPhaseFix, Role: state.ReviewerRole, Outcome: state.ParentOutcomeFix,
		},
		state.ModelCallLog{
			Version: state.ModelCallLogVersion, CallID: "parent-accept-1", CallType: state.CallTypeEvent,
			TaskID: fixture.runtimeTask, StartedAt: start.Add(21 * time.Minute), CompletedAt: start.Add(21 * time.Minute),
			Phase: state.ParentPhaseAccept, Role: state.ReviewerRole, Outcome: state.ParentOutcomeAccepted,
		},
	)
	return calls
}

func exportAnalysisWithTelemetry(t *testing.T, build func(*evidenceExportFixture) []state.ModelCallLog) EvidenceAnalysisIndex {
	t.Helper()
	fixture := newEvidenceExportFixture(t)
	overwriteLiveTelemetry(t, fixture, build(fixture)...)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return decodeAnalysisIndex(t, export)
}

func TestExportEvidenceAnalysisModelCallsExcludeNonModelEvents(t *testing.T) {
	t.Parallel()
	analysis := exportAnalysisWithTelemetry(t, dogfoodTelemetryCalls)
	modelCalls := analysis.ModelCalls
	if modelCalls.Status != evidenceAnalysisStatusCollected || modelCalls.Total != 14 || modelCalls.Basis != evidenceAnalysisBasisTelemetry {
		t.Fatalf("model calls = %#v", modelCalls)
	}
	if !reflect.DeepEqual(modelCalls.ByRole, map[string]int{"worker": 10, "reviewer": 4}) {
		t.Fatalf("model calls by role = %#v", modelCalls.ByRole)
	}
	if !reflect.DeepEqual(modelCalls.BySession, map[string]int{"session-worker": 10, "session-reviewer": 4}) {
		t.Fatalf("model calls by session = %#v", modelCalls.BySession)
	}
	if analysis.Retries.ResumedModelCalls != 0 {
		t.Fatalf("resumed model calls = %d", analysis.Retries.ResumedModelCalls)
	}
	roleSum, sessionSum := 0, 0
	for _, count := range modelCalls.ByRole {
		roleSum += count
	}
	for _, count := range modelCalls.BySession {
		sessionSum += count
	}
	if roleSum != modelCalls.Total || sessionSum != modelCalls.Total {
		t.Fatalf("model call totals diverge: total=%d by_role=%d by_session=%d", modelCalls.Total, roleSum, sessionSum)
	}
	observedRoles := map[string]int{}
	for _, stage := range analysis.Workflow.ModelStages {
		observedRoles[stage.Role] = stage.Calls
	}
	if !reflect.DeepEqual(observedRoles, map[string]int{"worker": 10, "reviewer": 4}) {
		t.Fatalf("workflow model stage calls = %#v", observedRoles)
	}
}

func TestExportEvidenceAnalysisModelCallsClassifyTelemetryCallTypes(t *testing.T) {
	t.Parallel()
	start := time.Unix(6100, 0).UTC()
	cases := []struct {
		name         string
		calls        func(*evidenceExportFixture) []state.ModelCallLog
		expected     EvidenceAnalysisModelCalls
		workflowRole map[string]int
	}{
		{
			name: "probe calls join model calls under their recorded session",
			calls: func(fixture *evidenceExportFixture) []state.ModelCallLog {
				return []state.ModelCallLog{
					analysisTelemetryCall(fixture, "w1", state.CallTypeTask, state.WorkerRole, "session-worker", start),
					analysisTelemetryCall(fixture, "p1", state.CallTypeProbe, state.WorkerRole, "none", start.Add(time.Minute)),
					analysisTelemetryCall(fixture, "e1", state.CallTypeEvent, state.ReviewerRole, "", start.Add(2*time.Minute)),
				}
			},
			expected:     EvidenceAnalysisModelCalls{Status: evidenceAnalysisStatusCollected, Total: 2, ByRole: map[string]int{"worker": 2}, BySession: map[string]int{"session-worker": 1, "none": 1}},
			workflowRole: map[string]int{"worker": 2},
		},
		{
			name: "duplicate call id counts once",
			calls: func(fixture *evidenceExportFixture) []state.ModelCallLog {
				return []state.ModelCallLog{
					analysisTelemetryCall(fixture, "dup", state.CallTypeTask, state.WorkerRole, "session-worker", start),
					analysisTelemetryCall(fixture, "dup", state.CallTypeTask, state.ReviewerRole, "session-reviewer", start.Add(time.Minute)),
				}
			},
			expected:     EvidenceAnalysisModelCalls{Status: evidenceAnalysisStatusCollected, Total: 1, ByRole: map[string]int{"worker": 1}, BySession: map[string]int{"session-worker": 1}},
			workflowRole: map[string]int{"worker": 1},
		},
		{
			name: "missing or unknown call type stays a non-model record",
			calls: func(fixture *evidenceExportFixture) []state.ModelCallLog {
				return []state.ModelCallLog{
					analysisTelemetryCall(fixture, "w1", state.CallTypeTask, state.WorkerRole, "session-worker", start),
					analysisTelemetryCall(fixture, "u1", "", state.WorkerRole, "session-worker", start.Add(time.Minute)),
					analysisTelemetryCall(fixture, "u2", "model", state.ReviewerRole, "session-reviewer", start.Add(2*time.Minute)),
				}
			},
			expected:     EvidenceAnalysisModelCalls{Status: evidenceAnalysisStatusCollected, Total: 1, ByRole: map[string]int{"worker": 1}, BySession: map[string]int{"session-worker": 1}},
			workflowRole: map[string]int{"worker": 1},
		},
		{
			name: "event-only telemetry reports no model calls",
			calls: func(fixture *evidenceExportFixture) []state.ModelCallLog {
				return []state.ModelCallLog{
					analysisTelemetryCall(fixture, "e1", state.CallTypeEvent, state.WorkerRole, "", start),
					analysisTelemetryCall(fixture, "e2", state.CallTypeEvent, state.ReviewerRole, "", start.Add(time.Minute)),
				}
			},
			expected: EvidenceAnalysisModelCalls{Status: evidenceAnalysisStatusAbsent},
		},
		{
			name: "unsupported telemetry version stays unreadable",
			calls: func(fixture *evidenceExportFixture) []state.ModelCallLog {
				call := analysisTelemetryCall(fixture, "w1", state.CallTypeTask, state.WorkerRole, "session-worker", start)
				call.Version = state.ModelCallLogVersion - 1
				return []state.ModelCallLog{call}
			},
			expected: EvidenceAnalysisModelCalls{Status: evidenceAnalysisStatusAbsent},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := exportAnalysisWithTelemetry(t, testCase.calls)
			modelCalls := analysis.ModelCalls
			if modelCalls.Status != testCase.expected.Status || modelCalls.Total != testCase.expected.Total ||
				!reflect.DeepEqual(modelCalls.ByRole, testCase.expected.ByRole) ||
				!reflect.DeepEqual(modelCalls.BySession, testCase.expected.BySession) {
				t.Fatalf("model calls = %#v want %#v", modelCalls, testCase.expected)
			}
			roleSum, sessionSum := 0, 0
			for _, count := range modelCalls.ByRole {
				roleSum += count
			}
			for _, count := range modelCalls.BySession {
				sessionSum += count
			}
			if roleSum != modelCalls.Total || sessionSum != modelCalls.Total {
				t.Fatalf("model call totals diverge: total=%d by_role=%d by_session=%d", modelCalls.Total, roleSum, sessionSum)
			}
			observedRoles := map[string]int{}
			for _, stage := range analysis.Workflow.ModelStages {
				observedRoles[stage.Role] = stage.Calls
			}
			if len(observedRoles) != len(testCase.workflowRole) {
				t.Fatalf("workflow model stage calls = %#v want %#v", observedRoles, testCase.workflowRole)
			}
			for role, calls := range testCase.workflowRole {
				if observedRoles[role] != calls {
					t.Fatalf("workflow model stage calls = %#v want %#v", observedRoles, testCase.workflowRole)
				}
			}
		})
	}
}

func TestExportEvidenceAnalysisModelCallsKeepUnattributedCountsConsistent(t *testing.T) {
	t.Parallel()
	start := time.Unix(6100, 0).UTC()
	cases := []struct {
		name     string
		calls    func(*evidenceExportFixture) []state.ModelCallLog
		expected EvidenceAnalysisModelCalls
		resumed  int
	}{
		{
			name: "task call without session id keeps session totals consistent",
			calls: func(fixture *evidenceExportFixture) []state.ModelCallLog {
				return []state.ModelCallLog{
					analysisTelemetryCall(fixture, "w1", state.CallTypeTask, state.WorkerRole, "", start),
					analysisTelemetryCall(fixture, "w2", state.CallTypeTask, state.WorkerRole, "session-worker", start.Add(time.Minute)),
				}
			},
			expected: EvidenceAnalysisModelCalls{Status: evidenceAnalysisStatusCollected, Total: 2, ByRole: map[string]int{"worker": 2}, BySession: map[string]int{"": 1, "session-worker": 1}},
		},
		{
			name: "resumed event does not join resumed model calls",
			calls: func(fixture *evidenceExportFixture) []state.ModelCallLog {
				taskCall := analysisTelemetryCall(fixture, "w1", state.CallTypeTask, state.WorkerRole, "session-worker", start)
				taskCall.Resumed = true
				eventCall := analysisTelemetryCall(fixture, "e1", state.CallTypeEvent, state.ReviewerRole, "", start.Add(time.Minute))
				eventCall.Resumed = true
				return []state.ModelCallLog{taskCall, eventCall}
			},
			expected: EvidenceAnalysisModelCalls{Status: evidenceAnalysisStatusCollected, Total: 1, ByRole: map[string]int{"worker": 1}, BySession: map[string]int{"session-worker": 1}},
			resumed:  1,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := exportAnalysisWithTelemetry(t, testCase.calls)
			modelCalls := analysis.ModelCalls
			if modelCalls.Status != testCase.expected.Status || modelCalls.Total != testCase.expected.Total ||
				!reflect.DeepEqual(modelCalls.ByRole, testCase.expected.ByRole) ||
				!reflect.DeepEqual(modelCalls.BySession, testCase.expected.BySession) {
				t.Fatalf("model calls = %#v want %#v", modelCalls, testCase.expected)
			}
			if analysis.Retries.ResumedModelCalls != testCase.resumed {
				t.Fatalf("resumed model calls = %d want %d", analysis.Retries.ResumedModelCalls, testCase.resumed)
			}
			roleSum, sessionSum := 0, 0
			for _, count := range modelCalls.ByRole {
				roleSum += count
			}
			for _, count := range modelCalls.BySession {
				sessionSum += count
			}
			if roleSum != modelCalls.Total || sessionSum != modelCalls.Total {
				t.Fatalf("model call totals diverge: total=%d by_role=%d by_session=%d", modelCalls.Total, roleSum, sessionSum)
			}
		})
	}
}

func TestExportEvidenceAnalysisModelCallsKeepRawEventRecords(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	overwriteLiveTelemetry(t, fixture, dogfoodTelemetryCalls(fixture)...)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entry := exportEntryByPath(t, export.Manifest, "live/task/telemetry.jsonl")
	if entry.Missing || entry.Unreadable != "" {
		t.Fatalf("telemetry entry = %#v", entry)
	}
	events := 0
	for _, line := range strings.Split(strings.TrimSuffix(string(exportFileByPath(t, export, "live/task/telemetry.jsonl")), "\n"), "\n") {
		if line == "" {
			continue
		}
		var record state.ModelCallLog
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record.CallType == state.CallTypeEvent {
			events++
		}
	}
	if events != 2 {
		t.Fatalf("exported telemetry kept %d event records want 2", events)
	}
}
