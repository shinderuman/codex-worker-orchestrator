package workflow

import (
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCanonicalReentryPreservesPendingDecisionAndRefreshesStoppedReview(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped review", true: "pending decision"}[pending], func(t *testing.T) {
			cfg, st := newWorkflowGuardFixture(t, t.TempDir())
			cfg.WorkerModel = "test-worker"
			cfg.EscalatedEffort = "high"
			admission, err := controller.Activate(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: "predecessor", TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
				t.Fatal(err)
			}
			if err := st.Write("worker.id", "old-worker-session"); err != nil {
				t.Fatal(err)
			}
			if err := st.Write("reviewer.id", "old-review-session"); err != nil {
				t.Fatal(err)
			}
			cp := state.ResumeCheckpoint{Stage: state.ResumeStageReview, Phase: "review", Role: state.ReviewerRole, Model: "old-reviewer", Prompt: "old snapshot", Request: "preserved request", Decision: "approved decision", StopKind: state.ResumeStopInterrupted}
			if err := st.EnterStop(cp); err != nil {
				t.Fatal(err)
			}
			if pending {
				cp.ClearStop()
				if err := st.SaveResumeCheckpoint(cp); err != nil {
					t.Fatal(err)
				}
				if err := st.Write("pending-decision", "preserved decision packet"); err != nil {
					t.Fatal(err)
				}
				if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
					t.Fatal(err)
				}
			}
			admission.Attempt.PredecessorAttemptID = "predecessor"
			w := NewWorkflow(cfg, st, nil, io.Discard)
			if err := w.bindCanonicalWorkflowAttempt(admission); err != nil {
				t.Fatal(err)
			}
			if st.Exists("worker.id") || st.Exists("reviewer.id") {
				t.Fatal("reentry reused old model sessions")
			}
			current, err := st.LoadResumeCheckpoint()
			if err != nil {
				t.Fatal(err)
			}
			if pending {
				if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") || current.IsStopped() {
					t.Fatal("reentry consumed the pending semantic decision")
				}
			} else {
				if current.Stage != state.ResumeStageWorker || current.Role != state.WorkerRole || current.CompletedResult != nil || current.Request != cp.Request || current.Decision != cp.Decision || current.Model != cfg.WorkerModel {
					t.Fatalf("stale review was not replaced by worker reentry: %#v", current)
				}
			}
			binding, err := st.LoadControllerRuntimeBinding()
			if err != nil || binding.AttemptID != admission.Attempt.AttemptID || binding.TaskPath != admission.Attempt.SemanticTaskRef.TaskPath || binding.TaskContractDigest != admission.Attempt.SemanticTaskRef.ContractDigest {
				t.Fatalf("workflow runtime binding was not refreshed: %#v %v", binding, err)
			}
		})
	}
}

func TestCanonicalReentryRejectsUnrelatedWorkflowSession(t *testing.T) {
	cfg, st := newWorkflowGuardFixture(t, t.TempDir())
	admission, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: "unrelated-attempt", TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("worker.id", "preserved-session"); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(cfg, st, nil, io.Discard)
	if err := w.bindCanonicalWorkflowAttempt(admission); err == nil {
		t.Fatal("unrelated attempt rebound the session")
	}
	if st.ReadOr("worker.id", "") != "preserved-session" {
		t.Fatal("rejected reentry consumed the session")
	}
}
