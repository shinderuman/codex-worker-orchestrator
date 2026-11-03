package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func exhaustedSummaryReport(t *testing.T, prepare ...func(string)) (*Workflow, *scriptedRunner, packet.Result) {
	t.Helper()
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket(), prepare...)
	report, err := packet.ParseStructured([]byte(oversizeImplementedPacket()))
	if err != nil {
		t.Fatal(err)
	}
	report.Summary = strings.Repeat("報告", packet.MaxFieldBytes)
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		t.Fatal(err)
	}
	record.Resubmissions = maxResultReportResubmissions
	if err := w.saveResultCorrectionRecord(record); err != nil {
		t.Fatal(err)
	}
	response := packetBody(report)
	st.RecordModelCallLog(state.ModelCallLog{
		CallID: "summary-source-call", CallType: state.CallTypeTask, TaskID: record.TaskID,
		SessionID: record.SessionID, Role: record.Role, Phase: checkpoint.Phase,
		Outcome: rejectedReportCallOutcome, ResumeSource: "result-format-recovery", ReadOnly: true,
		Response: response, ResponseSHA256: resultRecoveryDigest([]byte(response)),
	})
	r := &scriptedRunner{steps: []runnerStep{{structured: needsSolReviewPacket()}}}
	w.runner = r
	return w, r, report
}

func TestExhaustedSummaryRecoveryPreservesReportAndReviewsWithoutWorkerRerun(t *testing.T) {
	t.Parallel()
	w, r, original := exhaustedSummaryReport(t)
	if err := w.ReportResubmissionAvailable(); err != nil {
		t.Fatal(err)
	}
	before := w.state.ReadOr("task.id", "")
	if err := w.ExecuteReportResubmission(); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 0 || w.state.TaskStatus() != state.TaskStatusInterrupted || !w.state.Exists(state.ResultCorrectionStateFile) {
		t.Fatalf("normalization did not retain the correction history at a model-free boundary: phases=%v status=%s", r.phases, w.state.TaskStatus())
	}
	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 1 || !strings.HasPrefix(r.phases[0], "reviewer-") || w.state.TaskStatus() != state.TaskStatusWaitingSolReview || w.state.ReadOr("task.id", "") != before {
		t.Fatalf("recovery changed task or reran implementation: phases=%v status=%s", r.phases, w.state.TaskStatus())
	}
	data, err := os.ReadFile(w.summaryRecoveryPath("summary-source-call"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence summaryRecoveryEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.OriginalResponse != packetBody(original) || evidence.Correction.Resubmissions != 1 || evidence.Correction.Terminal == nil || evidence.CorrectedResult.Status != original.Status || evidence.CorrectedResult.Tests != original.Tests || len(evidence.CorrectedResult.Summary) > packet.MaxFieldBytes || !utf8.ValidString(evidence.CorrectedResult.Summary) {
		t.Fatalf("original report/history or byte bound was lost: %+v", evidence)
	}
}

func TestExhaustedSummaryRecoveryRejectsChangedSourceAndSession(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"source", "session", "task", "other-field"} {
		t.Run(change, func(t *testing.T) {
			w, r, _ := exhaustedSummaryReport(t)
			switch change {
			case "source":
				if err := os.WriteFile(filepath.Join(w.config.RepoRoot, "unrelated.txt"), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "session", "task":
				key := "worker.id"
				if change == "task" {
					key = "task.id"
				}
				if err := w.state.Write(key, "another-identity"); err != nil {
					t.Fatal(err)
				}
			default:
				record, err := w.loadResultCorrectionRecord()
				if err != nil {
					t.Fatal(err)
				}
				logs, err := w.state.ReadModelCallLogs(record.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				call := latestAcceptedResultTaskCall(logs)
				result, err := packet.ParseStructured([]byte(call.Response))
				if err != nil {
					t.Fatal(err)
				}
				result.Tests = ""
				call.Response = packetBody(result)
				call.ResponseSHA256 = resultRecoveryDigest([]byte(call.Response))
				w.state.RecordModelCallLog(call)
			}
			if err := w.ReportResubmissionAvailable(); err == nil {
				t.Fatal("changed source/session was advertised as recoverable")
			}
			if err := w.ExecuteReportResubmission(); err == nil || len(r.phases) != 0 {
				t.Fatalf("changed source/session recovered: err=%v phases=%v", err, r.phases)
			}
		})
	}
}

func seedCanonicalTaskAuthority(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md"), []byte("# root\n\n## Contract\n\nroot\n\n## Dependencies\n\nnone\n\n## External feasibility\n\nstatus: not-applicable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, repo, "add", ".")
	runRetentionGit(t, repo, "commit", "-q", "-m", "canonical task authority")
}

func controllerBackedSummaryRecovery(t *testing.T) (*Workflow, *scriptedRunner) {
	t.Helper()
	w, r, _ := exhaustedSummaryReport(t, func(repo string) { seedCanonicalTaskAuthority(t, repo) })
	w.admitCanonicalMutation = w.admitCanonicalWorkflowMutation
	w.admitNewTaskMutation = w.admitCanonicalNewTaskMutation
	w.admitModelMutation = w.admitControllerModelCall
	w.config.StateBase = filepath.Join(t.TempDir(), "sessions")
	if _, err := controller.Open(w.config); err != nil {
		t.Fatal(err)
	}
	return w, r
}

func TestControllerBackedSummaryRecoveryBindsIdleCanonicalAttemptAndReturnsToReviewer(t *testing.T) {
	t.Parallel()
	w, r := controllerBackedSummaryRecovery(t)
	before := w.state.ReadOr("task.id", "")
	if err := w.ExecuteReportResubmission(); err != nil {
		t.Fatal(err)
	}
	binding, err := w.state.LoadControllerRuntimeBinding()
	if err != nil {
		t.Fatal(err)
	}
	store, err := controller.Open(w.config)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if w.canonicalAdmission == nil ||
		w.canonicalAdmission.Attempt.AttemptID != binding.AttemptID ||
		head.LiveAttemptID != binding.AttemptID ||
		binding.TaskPath != w.canonicalAdmission.Attempt.SemanticTaskRef.TaskPath ||
		binding.TaskContractDigest != w.canonicalAdmission.Attempt.SemanticTaskRef.ContractDigest {
		t.Fatalf("recovery did not bind the idle canonical attempt: binding=%#v admission=%#v head=%#v", binding, w.canonicalAdmission, head)
	}
	if len(r.phases) != 0 || w.state.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("report-only recovery reran the worker: phases=%v status=%s", r.phases, w.state.TaskStatus())
	}
	if required, syncErr := store.RequiredTaskAuthoritySync(head); syncErr != nil || required != nil {
		t.Fatalf("canonical task authority is not in sync: required=%v err=%v", required, syncErr)
	}
	if _, err := w.admitControllerModelCall(state.ResumeCheckpoint{Role: state.ReviewerRole, ReadOnly: true}); err != nil {
		t.Fatalf("real model-call guard refused the reviewer continuation: %v", err)
	}
	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 1 || !strings.HasPrefix(r.phases[0], "reviewer-") || w.state.TaskStatus() != state.TaskStatusWaitingSolReview || w.state.ReadOr("task.id", "") != before {
		t.Fatalf("recovery did not return to the independent reviewer: phases=%v status=%s", r.phases, w.state.TaskStatus())
	}
}

func TestControllerBackedSummaryRecoveryRefusesInFlightCanonicalLease(t *testing.T) {
	t.Parallel()
	w, r := controllerBackedSummaryRecovery(t)
	admission, err := controller.Activate(w.config)
	if err != nil {
		t.Fatal(err)
	}
	store, err := controller.Open(w.config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindModelCall(admission); err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	stop, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	err = w.ExecuteReportResubmission()
	if err == nil || !strings.Contains(err.Error(), "idle canonical execution") {
		t.Fatalf("in-flight canonical lease was admitted: %v", err)
	}
	after, loadErr := w.state.LoadResumeCheckpoint()
	if loadErr != nil || after.StopKind != stop.StopKind || after.ResultCorrection != stop.ResultCorrection || after.Phase != stop.Phase {
		t.Fatalf("refusal changed the retained result-format stop: %#v err=%v", after, loadErr)
	}
	if _, bindErr := w.state.LoadControllerRuntimeBinding(); !errors.Is(bindErr, os.ErrNotExist) {
		t.Fatalf("refusal bound a canonical attempt: %v", bindErr)
	}
	if current, headErr := store.LoadHead(); headErr != nil || current.ControllerGeneration != head.ControllerGeneration || current.Status != controller.ControllerStatusActive {
		t.Fatalf("refusal changed controller authority: %#v err=%v", current, headErr)
	}
	if len(r.phases) != 0 || w.state.TaskStatus() != state.TaskStatusResultFormatRecoverable {
		t.Fatalf("refusal ran a model call or changed status: phases=%v status=%s", r.phases, w.state.TaskStatus())
	}
}
