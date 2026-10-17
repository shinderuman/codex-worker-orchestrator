package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func acceptedResultRecoveryFixture(t *testing.T) (*Workflow, AcceptedResultRuleRecoveryInput) {
	t.Helper()
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket(), func(repo string) {
		writeGitTestFile(t, repo, "worker_change.go", "package sample\n")
	})
	sessionID, _, err := st.SessionID(state.WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	w := newGitWorkflowT(t, st, &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("accepted"), result: runner.RunResult{SessionID: sessionID}}}}, repo)
	stop, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.authorizeResultReportResubmission(stop); err != nil {
		t.Fatal(err)
	}
	if err := w.activateResume(stop); err != nil {
		t.Fatal(err)
	}
	run := stop
	run.ClearStop()
	if _, err := w.runModel(run); err != nil {
		t.Fatal(err)
	}
	w.config.CodexConfigDir = t.TempDir()
	writeRuleFile(t, w.config.CodexConfigDir, "go.md", "GO CONTRACT")
	broken := run
	broken.ResultCorrection = false
	broken, err = w.ruleActivationCorrectionCheckpoint(broken, []workerRule{ruleGo}, 1)
	if err != nil {
		t.Fatal(err)
	}
	broken.ResultCorrection = true
	broken.ReadOnly = true
	if _, err := w.runModel(broken); err == nil {
		t.Fatal("old copied correction checkpoint did not fail")
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	call := logs[len(logs)-1]
	w.config.ClaudeConfigDir = t.TempDir()
	path := filepath.Join(w.config.ClaudeConfigDir, "projects", "fixture", call.SessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"sessionId": call.SessionID, "cwd": repo, "timestamp": call.CompletedAt,
		"message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "StructuredOutput", "input": json.RawMessage(call.Response)}}}}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return w, AcceptedResultRuleRecoveryInput{TaskID: taskID, AttemptID: "attempt", CallID: call.CallID, TranscriptPath: path}
}

func TestAcceptedResultRecoveryPreservesEvidenceAndContinuesOnlyRuleActivation(t *testing.T) {
	t.Parallel()
	w, input := acceptedResultRecoveryFixture(t)
	source, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.recoverAcceptedResultRuleActivation(input); err != nil {
		t.Fatal(err)
	}
	record, err := w.loadAcceptedResultRuleRecovery()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(record.SourceCheckpoint, source) || record.AcceptedResult.Status != packet.StatusImplemented || record.ConsumedResubmissions != 1 || len(record.SourceCorrection) == 0 {
		t.Fatalf("recovery record lost source evidence: %#v", record)
	}
	if err := w.recoverAcceptedResultRuleActivation(input); err != nil {
		t.Fatalf("idempotent transition: %v", err)
	}
	if err := w.authorizeResultReportResubmission(source); err == nil || !strings.Contains(err.Error(), "already authorized once") {
		t.Fatalf("consumed one-shot authorization = %v", err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("rules applied")}, {structured: needsSolReviewPacket()}}}
	w.runner = r
	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 2 || r.phases[0] != source.Phase || !strings.HasPrefix(r.phases[1], "reviewer-") || r.readOnlyCalls[0] || !r.readOnlyCalls[1] {
		t.Fatalf("continuation phases=%v permissions=%v", r.phases, r.readOnlyCalls)
	}
	if !strings.Contains(r.prompts[0], "GO CONTRACT") || w.state.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatal("rule context/review connection was lost")
	}
}

func TestAcceptedResultRecoveryRejectsUnprovenSourcesWithoutChangingStop(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"task", "call", "session", "snapshot", "packet", "transcript"} {
		t.Run(mutation, func(t *testing.T) {
			w, input := acceptedResultRecoveryFixture(t)
			switch mutation {
			case "task":
				input.TaskID = "other"
			case "call":
				input.CallID = "other"
			case "session":
				if err := w.state.Write("worker.id", "other"); err != nil {
					t.Fatal(err)
				}
			case "snapshot":
				writeGitTestFile(t, w.config.RepoRoot, "README.md", "changed\n")
			case "packet":
				data, err := os.ReadFile(input.TranscriptPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(input.TranscriptPath, []byte(strings.ReplaceAll(string(data), "accepted", "changed")), 0o600); err != nil {
					t.Fatal(err)
				}
			case "transcript":
				input.TranscriptPath = filepath.Join(t.TempDir(), "missing.jsonl")
			}
			before, err := w.state.LoadResumeCheckpoint()
			if err != nil {
				t.Fatal(err)
			}
			if err := w.recoverAcceptedResultRuleActivation(input); err == nil {
				t.Fatal("unproven recovery was admitted")
			}
			after, err := w.state.LoadResumeCheckpoint()
			if err != nil || !reflect.DeepEqual(before, after) || w.state.Exists(state.AcceptedResultRuleRecoveryStateFile) {
				t.Fatal("rejection changed the stop")
			}
		})
	}
}

func TestAcceptedResultRecoveryRequiresExactIdleCanonicalAttempt(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"different attempt", "in-flight call"} {
		t.Run(scenario, func(t *testing.T) {
			w, input := acceptedResultRecoveryFixture(t)
			w.admitCanonicalMutation = func() error {
				w.canonicalAdmission = &controller.Admission{Attempt: controller.AttemptRecord{AttemptID: input.AttemptID}}
				if scenario == "different attempt" {
					w.canonicalAdmission.Attempt.AttemptID = "other"
				} else {
					w.canonicalAdmission.Lease.InFlightCallID = "live"
				}
				return nil
			}
			if err := w.RecoverAcceptedResultRuleActivation(input); err == nil {
				t.Fatal("unsafe attempt admitted")
			}
			if w.state.Exists(state.AcceptedResultRuleRecoveryStateFile) || w.state.TaskStatus() != state.TaskStatusResultFormatRecoverable {
				t.Fatal("rejection mutated runtime")
			}
		})
	}
}

func TestAcceptedResultContinuationKeepsPhaseReadOnlyContracts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		stage      state.ResumeStage
		reportOnly bool
		poc        bool
		want       bool
	}{
		{state.ResumeStageWorker, false, false, false},
		{state.ResumeStageWorker, false, true, true},
		{state.ResumeStageWorker, true, false, true},
		{state.ResumeStageReview, false, false, true},
		{state.ResumeStageAutoFix, true, false, true},
		{state.ResumeStageAutoFix, false, false, false},
	} {
		decl := externalFeasibility{status: externalFeasibilityStatusNotApplicable}
		if tc.poc {
			decl.status = externalFeasibilityStatusPoC
		}
		got := acceptedResultContinuationCheckpoint(state.ResumeCheckpoint{Stage: tc.stage, ReportOnly: tc.reportOnly, ResultCorrection: true, ReadOnly: true}, decl)
		if got.ResultCorrection || got.ReadOnly != tc.want {
			t.Fatalf("phase contract %#v -> %#v", tc, got)
		}
	}
}
