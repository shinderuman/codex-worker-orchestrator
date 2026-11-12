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

func TestParentOnlyTaskAmendmentUsesAdmittedSourceAfterMutation(t *testing.T) {
	fixture, task := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	writeParentOnlyProduct(t, fixture, "initial-untracked.txt", "existing evaluation product\n")
	started := admitParentOnlyExecutionFixture(t, fixture)

	tracked := filepath.Join(fixture.repo, "source.txt")
	if _, err := os.Stat(tracked); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(tracked, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\nAdmitted evaluation product.\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeParentOnlyProduct(t, fixture, "later-untracked.txt", "admitted mutation product\n")
	after, err := CaptureWorkspaceSnapshot(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	mutated, err := fixture.store.RecordAdmittedMutation(started, "parent-only evaluation", "success", after)
	if err != nil {
		t.Fatal(err)
	}
	if mutated.Attempt.BaselineTrees != started.Attempt.BaselineTrees {
		t.Fatal("admitted mutation changed the candidate implementation baseline")
	}
	if mutated.Lease.AdmittedSourceTrees == nil || started.Lease.AdmittedSourceTrees == nil ||
		*mutated.Lease.AdmittedSourceTrees == *started.Lease.AdmittedSourceTrees {
		t.Fatal("admitted mutation did not advance its source tree binding")
	}

	taskFile := filepath.Join(fixture.repo, task.TaskPath)
	original, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskFile, append(original, []byte("\n## Amendments\n\nRevision one.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	required, err := fixture.store.RequiredTaskAuthoritySync(head)
	if err != nil || required == nil || !required.Admittable {
		t.Fatalf("admitted products hid a Task-only revision: %#v %v", required, err)
	}
	first, err := fixture.store.ParentOnlyTaskAuthoritySyncAdmission(fixture.store.config)
	if err != nil {
		t.Fatalf("admitted products prevented Task-only admission: %v", err)
	}
	if first.Attempt.BaselineTrees != started.Attempt.BaselineTrees {
		t.Fatal("Task amendment changed candidate implementation baseline")
	}

	writeParentOnlyProduct(t, fixture, "foreign-product.txt", "not admitted\n")
	if _, err := fixture.store.ParentOnlyTaskAuthoritySyncAdmission(fixture.store.config); err == nil {
		t.Fatal("unadmitted worktree edit was accepted")
	}
	if err := os.Remove(filepath.Join(fixture.repo, "foreign-product.txt")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, fixture.repo, "add", task.TaskPath)
	if _, err := fixture.store.ParentOnlyTaskAuthoritySyncAdmission(fixture.store.config); err == nil {
		t.Fatal("unadmitted index mutation was accepted")
	}
	runControllerGit(t, fixture.repo, "reset", "-q", "--", task.TaskPath)
	if _, err := fixture.store.ParentOnlyTaskAuthoritySyncAdmission(fixture.store.config); err != nil {
		t.Fatalf("restored index did not restore admitted Task-only source: %v", err)
	}
}

func TestParentOnlyConsecutiveTaskAmendmentsKeepAdmittedSource(t *testing.T) {
	fixture, task := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	writeParentOnlyProduct(t, fixture, "existing-product.txt", "already admitted\n")
	started := admitParentOnlyExecutionFixture(t, fixture)
	if started.Lease.AdmittedSourceTrees == nil {
		t.Fatal("parent-only source has no admitted tree binding")
	}
	previousTrees := *started.Lease.AdmittedSourceTrees
	taskFile := filepath.Join(fixture.repo, task.TaskPath)
	for _, revision := range []string{"\n## Amendments\n\nRevision one.\n", "\nRevision two.\n"} {
		content, err := os.ReadFile(taskFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(taskFile, append(content, []byte(revision)...), 0o644); err != nil {
			t.Fatal(err)
		}
		head, err := fixture.store.LoadHead()
		if err != nil {
			t.Fatal(err)
		}
		required, err := fixture.store.RequiredTaskAuthoritySync(head)
		if err != nil || required == nil || !required.Admittable {
			t.Fatalf("consecutive Task amendment was not admissible: %#v %v", required, err)
		}
		admission, err := fixture.store.ParentOnlyTaskAuthoritySyncAdmission(fixture.store.config)
		if err != nil {
			t.Fatalf("consecutive Task amendment was not admitted: %v", err)
		}
		amended := SemanticTaskRef{TaskPath: task.TaskPath, ContractDigest: digestBytes(append(content, []byte(revision)...))}
		proof := []byte(fmt.Sprintf(`{"runtime":"parent-only","task_id":%q}`, started.Attempt.AttemptID))
		runtimeRef, err := fixture.store.PutEvidenceObject("runtime-artifact", "application/json",
			started.Attempt.AttemptID+":"+TaskAuthoritySyncProducer+":"+admission.Snapshot.ID, true, proof)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.SyncTaskAuthority(admission, admission.Snapshot.ID, runtimeRef, amended); err != nil {
			t.Fatalf("consecutive Task sync failed: %v", err)
		}
		updatedHead, err := fixture.store.LoadHead()
		if err != nil {
			t.Fatal(err)
		}
		updatedAttempt, err := fixture.store.loadAttempt(updatedHead.LiveAttemptID)
		if err != nil {
			t.Fatal(err)
		}
		updatedLease, err := fixture.store.loadLease(updatedHead.LiveLeaseID)
		if err != nil {
			t.Fatal(err)
		}
		if updatedAttempt.BaselineTrees != started.Attempt.BaselineTrees ||
			updatedLease.AdmittedSourceTrees == nil ||
			*updatedLease.AdmittedSourceTrees == previousTrees {
			t.Fatal("Task sync did not advance admitted source independently of candidate baseline")
		}
		previousTrees = *updatedLease.AdmittedSourceTrees
	}
}

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
