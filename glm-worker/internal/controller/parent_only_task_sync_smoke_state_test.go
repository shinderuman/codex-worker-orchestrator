package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentOnlyDirectTaskSyncRejectsInvalidLiveSmokeState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		damage func(t *testing.T, smoke *state.StateStore, task SemanticTaskRef, attemptID string)
	}{
		{
			name: "missing-task-id",
			damage: func(t *testing.T, smoke *state.StateStore, _ SemanticTaskRef, _ string) {
				t.Helper()
				if err := os.Remove(smoke.Path("task.id")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "foreign-attempt-id",
			damage: func(t *testing.T, smoke *state.StateStore, _ SemanticTaskRef, _ string) {
				t.Helper()
				if err := smoke.Write("task.id", "another-attempt"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "model-runtime-binding",
			damage: func(t *testing.T, smoke *state.StateStore, task SemanticTaskRef, attemptID string) {
				t.Helper()
				if err := smoke.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
					AttemptID: attemptID, TaskPath: task.TaskPath, TaskContractDigest: task.ContractDigest,
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertParentOnlyDirectTaskSyncRejected(t, tc.damage)
		})
	}
}

func assertParentOnlyDirectTaskSyncRejected(t *testing.T, damage func(*testing.T, *state.StateStore, SemanticTaskRef, string)) {
	t.Helper()
	fixture, task := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
			started := admitParentOnlyExecutionFixture(t, fixture)
			smoke := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.Identity(), task))
			taskPath := filepath.Join(fixture.repo, task.TaskPath)
			original, err := os.ReadFile(taskPath)
			if err != nil {
				t.Fatal(err)
			}
			amended := append(append([]byte(nil), original...), []byte("\n## Amendments\n\nBound parent-only amendment.\n")...)
			if err := os.WriteFile(taskPath, amended, 0o644); err != nil {
				t.Fatal(err)
			}
			admission, err := fixture.store.ParentOnlyTaskAuthoritySyncAdmission(fixture.store.config)
			if err != nil {
				t.Fatal(err)
			}
			revised := SemanticTaskRef{TaskPath: task.TaskPath, ContractDigest: digestBytes(amended)}
			proof := []byte(fmt.Sprintf(`{"runtime":"parent-only","task_id":%q}`, started.Attempt.AttemptID))
			ref, err := fixture.store.PutEvidenceObject("runtime-artifact", "application/json",
				started.Attempt.AttemptID+":"+TaskAuthoritySyncProducer+":"+admission.Snapshot.ID, true, proof)
			if err != nil {
				t.Fatal(err)
			}
			headBefore, err := fixture.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			attemptBefore, err := fixture.store.loadAttempt(started.Attempt.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			leaseBefore, err := fixture.store.loadLease(headBefore.LiveLeaseID)
			if err != nil {
				t.Fatal(err)
			}
			sourceBefore, err := CaptureWorkspaceSnapshot(fixture.repo)
			if err != nil {
				t.Fatal(err)
			}

			damage(t, smoke, task, started.Attempt.AttemptID)
			if _, err := fixture.store.ParentOnlyTaskAuthoritySyncAdmission(fixture.store.config); err == nil || !strings.Contains(err.Error(), "smoke") {
				t.Fatalf("Controller admission accepted invalid actual smoke state: %v", err)
			}
			if _, err := fixture.store.SyncTaskAuthority(admission, admission.Snapshot.ID, ref, revised); err == nil || !strings.Contains(err.Error(), "smoke") {
				t.Fatalf("direct Controller Task sync accepted invalid smoke state with valid evidence: %v", err)
			}
			headAfter, err := fixture.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			attemptAfter, err := fixture.store.loadAttempt(started.Attempt.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			leaseAfter, err := fixture.store.loadLease(headBefore.LiveLeaseID)
			if err != nil {
				t.Fatal(err)
			}
			sourceAfter, err := CaptureWorkspaceSnapshot(fixture.repo)
			if err != nil {
				t.Fatal(err)
			}
			if headAfter.PendingTransitionID != "" || !reflect.DeepEqual(headBefore, headAfter) ||
				!reflect.DeepEqual(attemptBefore, attemptAfter) || !reflect.DeepEqual(leaseBefore, leaseAfter) ||
				!reflect.DeepEqual(sourceBefore, sourceAfter) {
				t.Fatalf("rejected direct sync mutated Controller authority or source: before=%#v after=%#v", headBefore, headAfter)
			}
}
