package state

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestRecoverInterruptedParentReopenRollsBackEveryDurablePrefix(t *testing.T) {
	for prefix := 0; prefix <= 6; prefix++ {
		t.Run(fmt.Sprintf("prefix-%d", prefix), func(t *testing.T) {
			st := newAcceptedParentCompletionStore(t)
			candidate := saveReopenPublicationState(t, st)
			finding := recordReopenFinding(t, st)
			evidence, err := st.LoadRuntimeInstallEvidence()
			if err != nil {
				t.Fatal(err)
			}
			completion, err := st.CurrentParentCompletionOutcome()
			if err != nil || completion == nil {
				t.Fatalf("completion = %#v err=%v", completion, err)
			}
			snapshots, err := st.snapshotReopenStateFiles()
			if err != nil {
				t.Fatal(err)
			}
			if err := st.saveParentReopenTransaction(snapshots); err != nil {
				t.Fatal(err)
			}

			operations := []func() error{
				func() error { return st.CapturePublicationReopenLineage(candidate) },
				func() error { return st.ClearPublicationCandidate() },
				func() error { return st.ClearRuntimeInstallEvidence() },
				func() error { return st.ClearPublicationInvalidatingFinding() },
				func() error {
					return st.openParentReviewState(string(packet.StatusNeedsSolReview), completion.Risk, ParentReviewProducer{}, false)
				},
				func() error { return st.SetTaskStatus(TaskStatusWaitingSolReview) },
			}
			for index := 0; index < prefix; index++ {
				if err := operations[index](); err != nil {
					t.Fatalf("apply prefix operation %d: %v", index, err)
				}
			}

			if _, err := st.ParentActionPlan(); err == nil || !strings.Contains(err.Error(), "parent reopen transition recovery is pending") {
				t.Fatalf("pending reopen transaction did not fail closed: %v", err)
			}
			recovered, err := st.RecoverInterruptedParentReopen()
			if err != nil || !recovered {
				t.Fatalf("recover = %t err=%v", recovered, err)
			}
			if st.Exists(parentReopenTransactionStateFile) {
				t.Fatal("recovery record remains after successful rollback")
			}
			if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
				t.Fatalf("recovered status = %s", st.TaskStatus())
			}
			candidateAfter, err := st.LoadPublicationCandidate()
			if err != nil || candidateAfter != candidate {
				t.Fatalf("recovered candidate = %#v err=%v", candidateAfter, err)
			}
			evidenceAfter, err := st.LoadRuntimeInstallEvidence()
			if err != nil || evidenceAfter != evidence {
				t.Fatalf("recovered install evidence = %#v err=%v", evidenceAfter, err)
			}
			findingAfter, err := st.LoadPublicationInvalidatingFinding()
			if err != nil || findingAfter != finding {
				t.Fatalf("recovered finding = %#v err=%v", findingAfter, err)
			}
			if _, err := st.LoadPublicationReopenLineage(); !os.IsNotExist(err) {
				t.Fatalf("recovered source retained reopen lineage: %v", err)
			}
			completionAfter, err := st.CurrentParentCompletionOutcome()
			if err != nil || completionAfter == nil || *completionAfter != *completion {
				t.Fatalf("recovered completion = %#v err=%v", completionAfter, err)
			}
			plan, err := st.ParentActionPlan()
			if err != nil || plan.RequiredAction != ParentActionReopen || !plan.Allows(ParentActionReopen) {
				t.Fatalf("recovered plan = %#v err=%v", plan, err)
			}

			if err := st.ReopenAcceptedParentCompletion(); err != nil {
				t.Fatal(err)
			}
			if st.TaskStatus() != TaskStatusWaitingSolReview || st.Exists(parentReopenTransactionStateFile) {
				t.Fatalf("post-recovery reopen status=%s marker=%t", st.TaskStatus(), st.Exists(parentReopenTransactionStateFile))
			}
		})
	}
}

func TestRecoverInterruptedParentReopenFailsClosedForWrongTask(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	saveReopenPublicationState(t, st)
	recordReopenFinding(t, st)
	snapshots, err := st.snapshotReopenStateFiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.saveParentReopenTransaction(snapshots); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", "00000000-0000-4000-8000-000000000001"); err != nil {
		t.Fatal(err)
	}

	recovered, err := st.RecoverInterruptedParentReopen()
	if !recovered || err == nil || !strings.Contains(err.Error(), "task identity changed") {
		t.Fatalf("wrong-task recovery = %t err=%v", recovered, err)
	}
	if !st.Exists(parentReopenTransactionStateFile) {
		t.Fatal("wrong-task recovery discarded durable record")
	}
}

func TestRecoverInterruptedParentReopenFailsClosedForCorruptRecord(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	if err := os.WriteFile(st.Path(parentReopenTransactionStateFile), []byte("{not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	recovered, err := st.RecoverInterruptedParentReopen()
	if !recovered || err == nil || !strings.Contains(err.Error(), "transaction record") {
		t.Fatalf("corrupt recovery = %t err=%v", recovered, err)
	}
	if !st.Exists(parentReopenTransactionStateFile) {
		t.Fatal("corrupt recovery discarded durable record")
	}
	if _, planErr := st.ParentActionPlan(); planErr == nil || !strings.Contains(planErr.Error(), "recovery is pending") {
		t.Fatalf("corrupt recovery record did not block parent actions: %v", planErr)
	}
}

func TestFreshTaskClearsParentReopenTransactionRecord(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(parentReopenTransactionStateFile, "stale"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if st.Exists(parentReopenTransactionStateFile) {
		t.Fatal("fresh task retained parent reopen transaction record")
	}
}
