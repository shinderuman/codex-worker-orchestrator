package workflow

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const retainedReviewerTestCallID = "source-reviewer-call"

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
				logs[1].SessionID != logs[2].SessionID {
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

func reviewerRiskFloorArtifactStop(t *testing.T) (*Workflow, *state.StateStore, *scriptedRunner, string, string) {
	t.Helper()
	st := newStateStoreT(t)
	callID := retainedReviewerTestCallID
	pass := resultFromBody(passPacket())
	pass.RequirementCoverage = strings.Repeat("C", packet.MaxFieldBytes)
	pass.TestEvidence = strings.Repeat("T", packet.MaxFieldBytes)
	pass.Issues = strings.Repeat("I", packet.MaxFieldBytes)
	raw := " " + packetBody(pass)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("implementation", "HIGH")},
		{structured: reviewerReentryInvalidTargets()},
		{structured: raw, result: runner.RunResult{CallID: callID, Response: "separate CLI result field"}},
	}}
	w := newWorkflowTInRepo(t, st, r, newRetentionGitRepo(t))
	w.captureSnapshot = state.CaptureGitSnapshot
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"target.go"}, nil
	}
	var artifactDir string
	w.captureSnapshot = func(root string) (state.GitSnapshot, error) {
		if len(r.phases) == 3 && artifactDir == "" {
			taskID, err := st.TaskID()
			if err != nil {
				t.Fatal(err)
			}
			artifactDir = st.ArtifactDir(taskID)
			if err := os.Rename(artifactDir, artifactDir+".saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(artifactDir, []byte("conflict"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return state.CaptureGitSnapshot(root)
	}
	r.onRun = func() {
		if len(r.phases) == 2 {
			if err := st.Write("reviewer.id", "test-session"); err != nil {
				t.Fatal(err)
			}
		}
	}

	var stop *GuardRecoverableError
	if err := w.ExecuteNewTask("request"); !errors.As(err, &stop) || !stop.ResultSaved {
		t.Fatalf("artifact persistence failure did not retain review: %v, status=%s phases=%v artifact=%q", err, st.TaskStatus(), r.phases, artifactDir)
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil || checkpoint.RetainedReviewerCall == nil || checkpoint.RetainedReviewerCall.CallID != callID ||
		string(checkpoint.RetainedReviewerCall.RawPacket) != raw || !checkpoint.ResultCorrection {
		t.Fatalf("corrected reviewer result was not retained: %v", err)
	}
	return w, st, r, artifactDir, raw
}

func newRetainedReviewerResume(t *testing.T, previous *Workflow, st *state.StateStore, r *scriptedRunner) *Workflow {
	t.Helper()
	resumed := newWorkflowTInRepo(t, st, r, previous.config.RepoRoot)
	resumed.captureSnapshot = state.CaptureGitSnapshot
	resumed.collectChangedPaths = previous.collectChangedPaths
	return resumed
}

func TestReviewerRiskFloorArtifactFailureResumesSameCall(t *testing.T) {
	previous, st, r, artifactDir, raw := reviewerRiskFloorArtifactStop(t)
	resumed := newRetainedReviewerResume(t, previous, st, r)
	var stop *GuardRecoverableError
	if err := resumed.ExecuteResume(); !errors.As(err, &stop) || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("second artifact failure did not preserve the stop: %v, %s", err, st.TaskStatus())
	}
	if err := os.Remove(artifactDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(artifactDir+".saved", artifactDir); err != nil {
		t.Fatal(err)
	}
	lastReview := st.Path("last-review")
	if err := os.Mkdir(lastReview, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := resumed.ExecuteResume(); !errors.As(err, &stop) || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("last-review persistence failure did not preserve the stop: %v, %s", err, st.TaskStatus())
	}
	if err := os.Remove(lastReview); err != nil {
		t.Fatal(err)
	}
	if err := resumed.ExecuteResume(); err != nil {
		t.Fatalf("resume did not reach the Sol review gate: %v", err)
	}
	report, err := st.Read("last-review")
	if err != nil {
		t.Fatal(err)
	}
	review, err := packet.ParseStructured([]byte(report))
	if err != nil || review.Status != packet.StatusNeedsSolReview || len(review.Artifacts) != 1 ||
		!strings.Contains(review.Issues, "source_call_id="+retainedReviewerTestCallID) {
		t.Fatalf("resume did not preserve the HIGH risk floor and call locator: %#v, %v", review, err)
	}
	original, err := os.ReadFile(review.Artifacts[0])
	if err != nil || string(original) != raw {
		t.Fatalf("reviewer raw packet changed on resume: %v", err)
	}
	if len(r.phases) != 3 || st.Exists(state.ResultCorrectionStateFile) {
		t.Fatalf("resume duplicated the reviewer call or left correction state: %v", r.phases)
	}
}

func TestReviewerRiskFloorResumeRejectsUnboundEvidence(t *testing.T) {
	for _, invalid := range []string{"call-id", "raw-packet", "missing-correction", "correction-source"} {
		t.Run(invalid, func(t *testing.T) {
			previous, st, r, _, _ := reviewerRiskFloorArtifactStop(t)
			checkpoint, err := st.LoadResumeCheckpoint()
			if err != nil {
				t.Fatal(err)
			}
			switch invalid {
			case "call-id":
				checkpoint.RetainedReviewerCall.CallID = "different-call"
			case "raw-packet":
				checkpoint.RetainedReviewerCall.RawPacket = []byte(passPacket())
			case "missing-correction":
				if err := st.Remove(state.ResultCorrectionStateFile); err != nil {
					t.Fatal(err)
				}
			case "correction-source":
				record, err := previous.loadResultCorrectionRecord()
				if err != nil {
					t.Fatal(err)
				}
				record.Snapshot.Head = "different-source"
				if err := previous.saveResultCorrectionRecord(record); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			resumed := newRetainedReviewerResume(t, previous, st, r)
			resumed.output = &output
			if err := resumed.ExecuteResume(); err == nil {
				t.Fatal("unbound reviewer result was admitted")
			}
			if len(r.phases) != 3 || output.Len() != 0 || st.Exists("last-review") {
				t.Fatalf("unbound result invoked or emitted a review: calls=%v output=%q", r.phases, output.String())
			}
		})
	}
}
