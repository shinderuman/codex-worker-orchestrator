package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func newUnsealedCompletionState(t *testing.T, risk packet.Risk) (*StateStore, ControllerRuntimeBinding) {
	t.Helper()
	st := newParentActionTestStore(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: risk}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := st.AcceptParentReview(); err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}
	binding := ControllerRuntimeBinding{AttemptID: "attempt-live", TaskPath: "IMPLEMENTATION_TASKS/active.md", TaskContractDigest: "digest-live"}
	if err := st.SaveControllerRuntimeBinding(binding); err != nil {
		t.Fatal(err)
	}
	return st, binding
}

func TestCaptureUnsealedParentCompletionRejectsUnsafeSources(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(t *testing.T, st *StateStore, binding ControllerRuntimeBinding) ControllerRuntimeBinding
		message string
	}{
		{
			name: "terminal task status",
			mutate: func(_ *testing.T, _ *StateStore, binding ControllerRuntimeBinding) ControllerRuntimeBinding {
				return binding
			},
			message: "requires accepted completion",
		},
		{
			name: "pending decision",
			mutate: func(t *testing.T, st *StateStore, binding ControllerRuntimeBinding) ControllerRuntimeBinding {
				if err := st.Write("pending-decision", "decision packet"); err != nil {
					t.Fatal(err)
				}
				return binding
			},
			message: "requires accepted completion",
		},
		{
			name: "resume checkpoint",
			mutate: func(t *testing.T, st *StateStore, binding ControllerRuntimeBinding) ControllerRuntimeBinding {
				if err := st.SaveResumeCheckpoint(ResumeCheckpoint{Model: "worker"}); err != nil {
					t.Fatal(err)
				}
				return binding
			},
			message: "requires no resume checkpoint",
		},
		{
			name: "publication candidate",
			mutate: func(t *testing.T, st *StateStore, binding ControllerRuntimeBinding) ControllerRuntimeBinding {
				saveReopenPublicationState(t, st)
				return binding
			},
			message: "cannot replace a publication candidate",
		},
		{
			name: "runtime binding missing",
			mutate: func(t *testing.T, st *StateStore, binding ControllerRuntimeBinding) ControllerRuntimeBinding {
				if err := st.ClearControllerRuntimeBinding(); err != nil {
					t.Fatal(err)
				}
				return binding
			},
		},
		{
			name: "runtime binding differs",
			mutate: func(t *testing.T, st *StateStore, binding ControllerRuntimeBinding) ControllerRuntimeBinding {
				other := binding
				other.AttemptID = "attempt-other"
				if err := st.SaveControllerRuntimeBinding(other); err != nil {
					t.Fatal(err)
				}
				return binding
			},
			message: "runtime binding differs",
		},
		{
			name: "requested binding incomplete",
			mutate: func(_ *testing.T, _ *StateStore, _ ControllerRuntimeBinding) ControllerRuntimeBinding {
				return ControllerRuntimeBinding{}
			},
			message: "runtime binding differs",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, binding := newUnsealedCompletionState(t, packet.RiskHigh)
			if tc.name == "terminal task status" {
				if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
					t.Fatal(err)
				}
			}
			requested := tc.mutate(t, st, binding)
			statusBefore := st.TaskStatus()
			_, err := st.CaptureUnsealedParentCompletion(requested)
			if err == nil {
				t.Fatal("unsafe source was captured")
			}
			if tc.message != "" && !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("capture error = %v", err)
			}
			if st.TaskStatus() != statusBefore {
				t.Fatalf("rejected capture changed status: %s -> %s", statusBefore, st.TaskStatus())
			}
		})
	}

	t.Run("without accepted outcome", func(t *testing.T) {
		st := newParentActionTestStore(t)
		if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveControllerRuntimeBinding(ControllerRuntimeBinding{AttemptID: "attempt-live", TaskPath: "IMPLEMENTATION_TASKS/active.md", TaskContractDigest: "digest-live"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CaptureUnsealedParentCompletion(ControllerRuntimeBinding{AttemptID: "attempt-live", TaskPath: "IMPLEMENTATION_TASKS/active.md", TaskContractDigest: "digest-live"}); err == nil || !strings.Contains(err.Error(), "accepted parent outcome") {
			t.Fatalf("capture without accepted outcome = %v", err)
		}
	})

	t.Run("no-go terminal", func(t *testing.T) {
		st, binding := newUnsealedCompletionState(t, packet.RiskHigh)
		taskID, err := st.TaskID()
		if err != nil {
			t.Fatal(err)
		}
		noGo := ParentReviewState{Version: parentReviewStateVersion, TaskID: taskID, Completion: &ParentCompletionOutcome{Terminal: SessionRotationTerminalNoGo, Risk: string(packet.RiskLow)}}
		data, err := json.Marshal(noGo)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(st.Path(parentReviewStateFile), append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CaptureUnsealedParentCompletion(binding); err == nil || !strings.Contains(err.Error(), "accepted parent outcome") {
			t.Fatalf("capture of no-go terminal = %v", err)
		}
	})
}

func TestReopenUnsealedParentCompletionReturnsAcceptedTaskToFix(t *testing.T) {
	for _, tc := range []struct {
		accepted packet.Risk
		reopened string
	}{{packet.RiskHigh, string(packet.RiskHigh)}, {packet.RiskLow, string(packet.RiskHigh)}} {
		t.Run(string(tc.accepted), func(t *testing.T) {
			st, binding := newUnsealedCompletionState(t, tc.accepted)
			source, err := st.CaptureUnsealedParentCompletion(binding)
			if err != nil {
				t.Fatal(err)
			}
			taskID, err := st.TaskID()
			if err != nil {
				t.Fatal(err)
			}
			logsBefore, err := st.ReadModelCallLogs(taskID)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.ReopenUnsealedParentCompletion(source); err != nil {
				t.Fatal(err)
			}
			if st.TaskStatus() != TaskStatusWaitingSolReview {
				t.Fatalf("reopened status = %s", st.TaskStatus())
			}
			label, err := st.CurrentParentReviewLabel()
			if err != nil || label != string(packet.StatusNeedsSolReview) {
				t.Fatalf("reopened review label = %q err=%v", label, err)
			}
			open, err := st.CurrentParentReview()
			if err != nil || open == nil || open.Risk != tc.reopened {
				t.Fatalf("reopened open review = %#v err=%v", open, err)
			}
			if completion, err := st.CurrentParentCompletionOutcome(); err != nil || completion != nil {
				t.Fatalf("reopened completion outcome = %#v err=%v", completion, err)
			}
			plan, err := st.ParentActionPlan()
			if err != nil {
				t.Fatal(err)
			}
			if plan.RequiredAction != ParentActionReview || !plan.Allows(ParentActionFix) || plan.Allows(ParentActionAccept) || plan.Allows(ParentActionComplete) {
				t.Fatalf("reopened plan = %#v", plan)
			}
			logs, err := st.ReadModelCallLogs(taskID)
			if err != nil {
				t.Fatal(err)
			}
			if len(logs) != len(logsBefore)+1 {
				t.Fatalf("reopened outcome events = %d -> %d", len(logsBefore), len(logs))
			}
			event := logs[len(logs)-1]
			if event.Outcome != ParentOutcomeFix || event.ParentOrigin != ParentOriginCodexReview || event.ParentCause != ParentCauseProductionWiring || event.WorkerReportedRisk != tc.reopened {
				t.Fatalf("reopened outcome event = %#v", event)
			}
			if err := st.ReopenUnsealedParentCompletion(source); err == nil {
				t.Fatal("reopen was admitted twice")
			}
			if st.TaskStatus() != TaskStatusWaitingSolReview {
				t.Fatalf("second reopen changed status: %s", st.TaskStatus())
			}
		})
	}
}

func TestReopenUnsealedParentCompletionRejectsChangedOrCorruptSource(t *testing.T) {
	st, binding := newUnsealedCompletionState(t, packet.RiskHigh)
	source, err := st.CaptureUnsealedParentCompletion(binding)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(source, []byte(`"risk":"HIGH"`), []byte(`"risk":"LOW"`), 1)
	if bytes.Equal(tampered, source) {
		t.Fatal("fixture did not produce a tamperable completion risk")
	}
	for name, bad := range map[string][]byte{"changed": tampered, "corrupt": []byte("not json")} {
		if err := st.ReopenUnsealedParentCompletion(bad); err == nil {
			t.Fatalf("%s source was reopened", name)
		}
		if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
			t.Fatalf("%s source changed status: %s", name, st.TaskStatus())
		}
		if label, err := st.CurrentParentReviewLabel(); err != nil || label != roundCommentNone {
			t.Fatalf("%s source changed review state: %q err=%v", name, label, err)
		}
	}
}

func TestReopenUnsealedParentCompletionRollsBackIntermediateFailure(t *testing.T) {
	st, binding := newUnsealedCompletionState(t, packet.RiskHigh)
	source, err := st.CaptureUnsealedParentCompletion(binding)
	if err != nil {
		t.Fatal(err)
	}
	original := writeFileAtomic
	failed := false
	writeFileAtomic = func(path string, data []byte, mode os.FileMode) error {
		if !failed && strings.HasSuffix(path, "task.status") {
			failed = true
			return errors.New("injected task status write failure")
		}
		return original(path, data, mode)
	}
	t.Cleanup(func() { writeFileAtomic = original })

	err = st.ReopenUnsealedParentCompletion(source)
	if err == nil || !strings.Contains(err.Error(), "injected task status write failure") {
		t.Fatalf("intermediate reopen failure = %v", err)
	}
	if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
		t.Fatalf("failed reopen mutated status: %s", st.TaskStatus())
	}
	completion, err := st.CurrentParentCompletionOutcome()
	if err != nil || completion == nil || completion.Terminal != SessionRotationTerminalAccept || completion.Risk != string(packet.RiskHigh) {
		t.Fatalf("failed reopen did not restore the accepted outcome: %#v err=%v", completion, err)
	}
	if label, err := st.CurrentParentReviewLabel(); err != nil || label != roundCommentNone {
		t.Fatalf("failed reopen did not restore the review state: %q err=%v", label, err)
	}
	if err := st.ReopenUnsealedParentCompletion(source); err != nil {
		t.Fatalf("retriable after rollback: %v", err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("retried reopen status = %s", st.TaskStatus())
	}
}

func TestReopenUnsealedParentCompletionFailsClosedMidTransitionWithoutFalseSuccess(t *testing.T) {
	st, binding := newUnsealedCompletionState(t, packet.RiskHigh)
	source, err := st.CaptureUnsealedParentCompletion(binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.openParentReviewState(string(packet.StatusNeedsSolReview), string(packet.RiskHigh), ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReopenUnsealedParentCompletion(source); err == nil || !strings.Contains(err.Error(), "accepted parent outcome") {
		t.Fatalf("mid-transition reopen = %v", err)
	}
	if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
		t.Fatalf("mid-transition reopen changed status: %s", st.TaskStatus())
	}
}
