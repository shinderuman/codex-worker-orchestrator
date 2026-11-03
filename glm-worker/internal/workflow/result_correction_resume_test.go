package workflow

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func coverageCorrectionResume(t *testing.T) (*Workflow, state.ResumeCheckpoint, packet.Result) {
	t.Helper()
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket())
	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		t.Fatal(err)
	}
	record.Terminal = nil
	if err := w.saveResultCorrectionRecord(record); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	report, err := packet.ParseStructured([]byte(implementedPacket("existing result")))
	if err != nil {
		t.Fatal(err)
	}
	report.RequirementCoverage = strings.Repeat("検証済み。", packet.MaxFieldBytes)
	body := packetBody(report)
	st.RecordModelCallLog(state.ModelCallLog{CallID: "coverage-source-call", CallType: state.CallTypeTask,
		TaskID: record.TaskID, SessionID: record.SessionID, Role: record.Role,
		Phase: strings.TrimSuffix(checkpoint.Phase, resultCorrectionPhaseSuffix), Outcome: "invalid_packet",
		Response: body, ResponseSHA256: resultRecoveryDigest([]byte(body))})
	return w, checkpoint, report
}

func TestCorrectionResumeProvidesValidatedPacketWithoutChangingOtherFields(t *testing.T) {
	t.Parallel()
	w, checkpoint, original := coverageCorrectionResume(t)
	input, err := w.resultCorrectionResumeInput(checkpoint)
	if err != nil || input == nil {
		t.Fatalf("input=%+v err=%v", input, err)
	}
	if len(input.Result.RequirementCoverage) > packet.MaxFieldBytes || !utf8.ValidString(input.Result.RequirementCoverage) || input.SourceCallID != "coverage-source-call" {
		t.Fatalf("invalid byte bound or provenance: %+v", input)
	}
	if err := packet.ValidateWorkerResult(input.Result); err != nil {
		t.Fatal(err)
	}
	input.Result.RequirementCoverage = original.RequirementCoverage
	if !reflect.DeepEqual(input.Result, original) {
		t.Fatal("report fields other than requirement_coverage changed")
	}
	logs, err := w.state.ReadModelCallLogs(input.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if logs[len(logs)-1].Response != packetBody(original) {
		t.Fatal("original rejected report changed")
	}
	prompt, err := w.resultCorrectionResumePrompt(checkpoint)
	if err != nil || !strings.Contains(prompt, "MODE: REPORT_CORRECTION_ONLY") || !strings.Contains(prompt, "SOURCE_CALL: coverage-source-call") {
		t.Fatalf("report-only prompt or provenance missing: %v", err)
	}
	_, output, found := strings.Cut(prompt, "OUTPUT_JSON:\n")
	if !found {
		t.Fatal("saved report is missing from correction prompt")
	}
	result, err := packet.ParseStructured([]byte(strings.TrimSpace(output)))
	if err != nil || packet.ValidateWorkerResult(result) != nil {
		t.Fatalf("correction prompt does not contain a valid packet: %v", err)
	}
}

func TestCorrectionResumeRefusesChangedSession(t *testing.T) {
	t.Parallel()
	w, checkpoint, _ := coverageCorrectionResume(t)
	if err := w.state.Write("worker.id", "another-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.resultCorrectionResumeInput(checkpoint); err == nil {
		t.Fatal("foreign session report was reused")
	}
}
