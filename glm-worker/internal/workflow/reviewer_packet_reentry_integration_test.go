package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func reviewerReentryInvalidTargets() string {
	result := resultFromBody(passPacket())
	result.Targets = nil
	return packetBody(result)
}

func reviewerReentryOversizedPacket() string {
	result := resultFromBody(passPacket())
	result.Summary = strings.Repeat("x", packet.MaxFieldBytes+1)
	return packetBody(result)
}

func TestReviewerPacketCorrectionProductionEntryThroughFinalGate(t *testing.T) {
	cases := []struct {
		name   string
		broken string
		reason string
	}{
		{name: "invalid-targets", broken: reviewerReentryInvalidTargets(), reason: "targets-none"},
		{name: "oversized-field", broken: reviewerReentryOversizedPacket(), reason: "size"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			st := newStateStoreT(t)
			r := &scriptedRunner{steps: []runnerStep{
				{structured: implementedPacket("implementation")},
				{structured: tt.broken},
				{structured: passPacket()},
			}}
			w := newWorkflowT(t, st, r)
			gateCalls := 0
			w.qualityGate = func(string) (harnesslint.Report, error) {
				gateCalls++
				return harnesslint.Report{Status: "pass"}, nil
			}
			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if st.TaskStatus() != state.TaskStatusComplete || gateCalls == 0 {
				t.Fatalf("final gate was not reached: status=%s gates=%d", st.TaskStatus(), gateCalls)
			}
			if len(r.phases) != 3 || r.phases[0] != "worker-new" || r.phases[1] != "reviewer-1" ||
				r.phases[2] != "reviewer-1"+resultCorrectionPhaseSuffix {
				t.Fatalf("worker -> independent reviewer -> correction route = %v", r.phases)
			}
			if r.readOnlyCalls[0] || !r.readOnlyCalls[1] || !r.readOnlyCalls[2] {
				t.Fatalf("reviewer correction changed tool authority: %v", r.readOnlyCalls)
			}
			stats := currentStats(t, st)
			if stats.ResultCorrections != 1 || stats.ModelCalls != 3 {
				t.Fatalf("finite correction counters = %#v", stats)
			}
			taskID, err := st.TaskID()
			if err != nil {
				t.Fatal(err)
			}
			logs, err := st.ReadModelCallLogs(taskID)
			if err != nil {
				t.Fatal(err)
			}
			if len(logs) != 3 || logs[1].Outcome != "invalid_packet" || logs[1].PacketRejectReason != tt.reason ||
				logs[2].Outcome != "success" || logs[2].RetryOf != logs[1].CallID ||
				logs[1].Role != state.ReviewerRole || logs[2].Role != state.ReviewerRole ||
				logs[0].SessionID == logs[1].SessionID || logs[1].SessionID != logs[2].SessionID {
				t.Fatalf("model call source/review/retry telemetry = %#v", logs)
			}
			if st.Exists(state.ResultCorrectionStateFile) {
				t.Fatal("accepted corrected review retained correction state")
			}
		})
	}
}

func reviewerPacketCorrectionStoppedFixture(t *testing.T) (*Workflow, *state.StateStore, *scriptedRunner) {
	t.Helper()
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("implementation")},
		{structured: reviewerReentryInvalidTargets()},
		{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	err := w.ExecuteNewTask("request")
	var limit runner.ZaiRateLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("review correction interruption = %v", err)
	}
	cp, err := st.LoadResumeCheckpoint()
	if err != nil || cp.Role != state.ReviewerRole || !cp.ResultCorrection || cp.StopKind != state.ResumeStopRateLimited {
		t.Fatalf("correction stop checkpoint = %#v, %v", cp, err)
	}
	if stats := currentStats(t, st); stats.ResultCorrections != 1 {
		t.Fatalf("correction attempts at stop = %d", stats.ResultCorrections)
	}
	if len(r.phases) != 3 || r.phases[2] != "reviewer-1"+resultCorrectionPhaseSuffix {
		t.Fatalf("unexpected stopped model calls = %v", r.phases)
	}
	return w, st, r
}

func TestReviewerPacketCorrectionResumePreservesReviewAndGate(t *testing.T) {
	w, st, r := reviewerPacketCorrectionStoppedFixture(t)
	if err := w.ExecuteResume(); err != nil {
		t.Fatalf("reviewer correction resume failed: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete || len(r.phases) != 4 ||
		r.phases[3] != "reviewer-1"+resultCorrectionPhaseSuffix {
		t.Fatalf("resume did not complete the original reviewer round: status=%s phases=%v", st.TaskStatus(), r.phases)
	}
	stats := currentStats(t, st)
	if stats.ResultCorrections != 1 || stats.ModelCalls != 4 {
		t.Fatalf("resume caused a duplicate correction: %#v", stats)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 4 || logs[1].Role != state.ReviewerRole || logs[2].Role != state.ReviewerRole ||
		logs[3].Role != state.ReviewerRole || logs[3].Outcome != "success" ||
		logs[1].SessionID != logs[3].SessionID {
		t.Fatalf("resumed reviewer attribution = %#v", logs)
	}
}

func TestReviewerPacketCorrectionResumeRejectsStaleSourceWithoutModelCall(t *testing.T) {
	w, st, r := reviewerPacketCorrectionStoppedFixture(t)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) {
		changed := fixedSnapshot
		changed.WorktreeDigest = "stale-after-review-stop"
		return changed, nil
	}
	err := w.ExecuteResume()
	if err == nil && st.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("stale review was accepted as final PASS")
	}
	if len(r.phases) != 3 {
		t.Fatalf("stale source invoked another model call: %v", r.phases)
	}
	if stats := currentStats(t, st); stats.ResultCorrections != 1 {
		t.Fatalf("stale source altered correction count: %d", stats.ResultCorrections)
	}
	if st.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("stale source completed task")
	}
}
