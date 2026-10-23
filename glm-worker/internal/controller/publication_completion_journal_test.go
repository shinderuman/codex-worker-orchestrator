package controller

import (
	"errors"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPublicationCompletesTaskInOneJournal(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "completed implementation\n")
	runtime := acceptedCompletionRuntime(t, fixture.store, source)
	promoted, implementation := promoteTerminalTestSource(t, fixture, policy, source)
	result, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: implementation.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.TaskStatus() != state.TaskStatusComplete {
		t.Fatal("journal did not complete accepted Runtime")
	}
	if result.Head.PendingTransitionID != "" || result.Head.AcceptedCandidateRef != nil || result.Head.MetadataLineageRef == nil {
		t.Fatalf("completion did not close its single journal: %#v", result.Head)
	}
	terminal, err := fixture.store.LoadTerminalTaskRecord(*result.Head.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.TransitionID != result.TransitionID || result.Head.RootTaskRef == nil || !result.Head.RootTaskRef.Equal(fixture.child) {
		t.Fatal("completion metadata and authority belong to different operations")
	}
	parent := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "rev-list", "--parents", "-n", "1", result.Head.IntegrationTip)
	if parent != result.Head.IntegrationTip+" "+implementation.BaseOID {
		t.Fatal("Task publication contains more than one commit")
	}
	remote, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil || remote != result.Head.IntegrationTip {
		t.Fatalf("remote and completion differ: remote=%s error=%v", remote, err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(terminal.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	observations, readiness := 0, 0
	for _, object := range bundle.Objects {
		switch object.Ref.Kind {
		case "publication-observation":
			observations++
		case "completion-readiness":
			readiness++
		}
	}
	if observations != 1 || readiness != 1 {
		t.Fatalf("completion Bundle lacks exact receipts: observation=%d readiness=%d", observations, readiness)
	}
}

func TestCompletionRecoversPublishedTaskWithoutAnotherPush(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "completed implementation\n")
	runtime := acceptedCompletionRuntime(t, fixture.store, source)
	promoted, implementation := promoteTerminalTestSource(t, fixture, policy, source)
	remote := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "remote", "get-url", policy.Remote)
	pushLog := filepath.Join(t.TempDir(), "pushes")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	hook := "#!/bin/sh\nset -eu\nprintf x >> " + quote(pushLog) + "\nchmod 0500 " + quote(runtime.Path(".")) + "\n"
	if err := os.WriteFile(filepath.Join(remote, "hooks", "post-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(runtime.Path("."), 0o700) })
	if _, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: implementation.CandidateID}); err == nil {
		t.Fatal("injected acknowledgement write failure did not fail publication completion")
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.PendingTransitionID == "" || head.MetadataLineageRef != nil || head.ProjectSnapshotID != promoted.Head.ProjectSnapshotID {
		t.Fatal("failed acknowledgement prematurely completed Task authority")
	}
	_, phase, err := fixture.store.LoadTransition(head.PendingTransitionID)
	if err != nil || phase.Completion == nil || phase.Completion.ObservationRef == nil {
		t.Fatalf("successful push lacks saved observation: %v", err)
	}
	before, err := CaptureWorkspaceSnapshot(fixture.store.identity.PrimaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	final := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "rev-parse", "HEAD")
	observed, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil || observed != final {
		t.Fatalf("fault occurred before real remote publication: %v", err)
	}
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("failed Runtime ACK status: %s", runtime.TaskStatus())
	}
	if err := os.Chmod(runtime.Path("."), 0o700); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(fixture.store.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.store.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(fixture.store.dir, 0o700) })
	if _, err := reopened.RecoverExecutionOperation(head.PendingTransitionID); err == nil {
		t.Fatal("Controller ACK write failure was ignored")
	}
	if runtime.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("partial ACK Runtime status: %s", runtime.TaskStatus())
	}
	if err := os.Chmod(fixture.store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	completed, err := reopened.RecoverExecutionOperation(head.PendingTransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Head.PendingTransitionID != "" || completed.Head.IntegrationTip != final || completed.Head.MetadataLineageRef == nil {
		t.Fatal("saved journal did not complete the published Task")
	}
	if _, err := reopened.RecoverExecutionOperation(head.PendingTransitionID); err != nil {
		t.Fatalf("lost completion response could not be replayed: %v", err)
	}
	after, err := CaptureWorkspaceSnapshot(fixture.store.identity.PrimaryRoot)
	if err != nil || after != before {
		t.Fatalf("acknowledgement recovery changed source, Metadata, index or commit: %v", err)
	}
	pushes, err := os.ReadFile(pushLog)
	if err != nil || string(pushes) != "x" {
		t.Fatalf("acknowledgement recovery performed another push: %q %v", pushes, err)
	}
}

func TestCompletionReadinessFailureRestoresUnpublishedPreparation(t *testing.T) {
	t.Parallel()
	for _, successor := range []bool{false, true} {
		name := "current-runtime"
		if successor {
			name = "successor-runtime"
		}
		t.Run(name, func(t *testing.T) {
			fixture, policy := newPublicationTestFixture(t)
			source := publicationTestEdit(t, fixture, "result.txt", "reviewed implementation\n")
			promoted, implementation := promoteTerminalTestSource(t, fixture, policy, source)
			task := source.Attempt.SemanticTaskRef
			if successor {
				task = fixture.child
			}
			cfg := WorkflowTaskConfig(fixture.store.config, fixture.store.identity, task)
			runtime, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: source.Attempt.AttemptID, TaskPath: task.TaskPath, TaskContractDigest: task.ContractDigest}); err != nil {
				t.Fatal(err)
			}
			if successor {
				if err := runtime.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
					t.Fatal(err)
				}
				if err := runtime.Touch("pending-decision"); err != nil {
					t.Fatal(err)
				}
			}
			before, err := CaptureWorkspaceSnapshot(fixture.store.identity.PrimaryRoot)
			if err != nil {
				t.Fatal(err)
			}
			_, err = fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: implementation.CandidateID})
			if successor {
				var blocked *RuntimeDispositionRequired
				head, loadErr := fixture.store.LoadHead()
				if !errors.As(err, &blocked) || blocked.Input.Task != task || loadErr != nil || !reflect.DeepEqual(head, promoted.Head) {
					t.Fatalf("successor refusal changed publication authority: %v %v", err, loadErr)
				}
				if observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy) != implementation.BaseOID {
					t.Fatal("blocked successor changed remote")
				}
				return
			}
			var refusal *PublicationAdvancementRequired
			if !errors.As(err, &refusal) || refusal.Action != "reenter-accepted-candidate" {
				t.Fatalf("unfinished runtime did not return existing repair action: %v", err)
			}
			after, err := CaptureWorkspaceSnapshot(fixture.store.identity.PrimaryRoot)
			if err != nil || after != before {
				t.Fatalf("readiness failure did not restore original source and Git: %v", err)
			}
			head, err := fixture.store.LoadHead()
			if err != nil || head.PendingTransitionID != "" || head.MetadataLineageRef != nil || head.AcceptedCandidateRef == nil {
				t.Fatalf("failed readiness completed Task or retained preparation: %v", err)
			}
			_, phase, err := fixture.store.LoadTransition(refusal.TransitionID)
			if err != nil || phase.Completion == nil || phase.Completion.WriteIntent || phase.Completion.FailureRef == nil {
				t.Fatalf("readiness rejection evidence missing: %v", err)
			}
			if observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy) != implementation.BaseOID {
				t.Fatal("unfinished Task changed remote")
			}
			if _, err := fixture.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: implementation.CandidateID}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompletionRemoteRejectionRestoresUnpublishedPreparation(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "reviewed implementation\n")
	promoted, implementation := promoteTerminalTestSource(t, fixture, policy, source)
	remote := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "remote", "get-url", policy.Remote)
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := CaptureWorkspaceSnapshot(fixture.store.identity.PrimaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: implementation.CandidateID})
	var refusal *PublicationAdvancementRequired
	if !errors.As(err, &refusal) || refusal.Action != "reenter-accepted-candidate" {
		t.Fatalf("definite server refusal did not return repair action: %v", err)
	}
	after, err := CaptureWorkspaceSnapshot(fixture.store.identity.PrimaryRoot)
	if err != nil || after != before {
		t.Fatalf("rejected publication changed original source or Metadata: %v", err)
	}
	if observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy) != implementation.BaseOID {
		t.Fatal("rejected publication changed remote")
	}
	_, err = fixture.store.RecoverExecutionOperation(refusal.TransitionID)
	if !errors.As(err, &refusal) {
		t.Fatalf("aborted journal recovery lost refusal: %v", err)
	}
}

func TestCompletionWriteIntentResumesUnappliedPush(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "reviewed implementation\n")
	prepared, _ := prepareTerminalTestSource(t, fixture, policy, source)
	var op ExecutionOperation
	if err := readJSON(fixture.store.executionOperationPath(prepared.TransitionID), &op); err != nil {
		t.Fatal(err)
	}
	phase, err := fixture.store.loadTransitionState(prepared.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.beginCandidateCompletionWrite(op, &phase); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(prepared.TransitionID); err != nil {
		t.Fatalf("unapplied write intent could not resume its journal: %v", err)
	}
	if observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy) != op.Terminal.Record.CandidateOID {
		t.Fatal("resumed write intent did not publish exact final commit")
	}
}

func TestControllerJSONRejectsRemovedContractFields(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "head.json")
	if err := os.WriteFile(path, []byte(`{"pending_terminal_task_ref":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var head RepositoryControllerHead
	if err := readJSON(path, &head); err == nil {
		t.Fatal("removed contract field was silently accepted")
	}
}

func TestCompletionLostPushResponseObservesWithoutAnotherPush(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "reviewed implementation\n")
	prepared, candidate := prepareTerminalTestSource(t, fixture, policy, source)
	var op ExecutionOperation
	if err := readJSON(fixture.store.executionOperationPath(prepared.TransitionID), &op); err != nil {
		t.Fatal(err)
	}
	phase, err := fixture.store.loadTransitionState(prepared.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.beginCandidateCompletionWrite(op, &phase); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.pushAndObserveCandidate(op, candidate, candidate.BaseOID); err != nil {
		t.Fatal(err)
	}
	remote := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "remote", "get-url", policy.Remote)
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "config", "core.hooksPath", hooks)
	before, err := CaptureWorkspaceSnapshot(fixture.store.identity.PrimaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(fixture.store.config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reopened.RecoverExecutionOperation(prepared.TransitionID)
	if err != nil || result.Head.PendingTransitionID != "" || result.Head.IntegrationTip != candidate.CommitOID {
		t.Fatalf("lost push response did not recover exact journal: %v", err)
	}
	after, err := CaptureWorkspaceSnapshot(fixture.store.identity.PrimaryRoot)
	if err != nil || after != before {
		t.Fatalf("lost response recovery changed final source or Metadata: %v", err)
	}
}

func acceptedCompletionRuntime(t *testing.T, store *Store, source Admission) *state.StateStore {
	t.Helper()
	return acceptedCompletionRuntimeOptions(t, store, source, true)
}

func acceptedCompletionRuntimeOptions(t *testing.T, store *Store, source Admission, telemetry bool) *state.StateStore {
	t.Helper()
	cfg := WorkflowTaskConfig(store.config, store.identity, source.Attempt.SemanticTaskRef)
	runtime, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := runtime.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if telemetry {
		started := time.Now().UTC()
		runtime.RecordModelCallLog(state.ModelCallLog{
			TaskID: taskID, CallType: state.CallTypeTask, Role: state.WorkerRole,
			ModelAlias: "opus", Effort: "high", Outcome: "success",
			StartedAt: started, CompletedAt: started.Add(30 * time.Second), WallDurationMS: 30000,
		})
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: source.Attempt.AttemptID, TaskPath: source.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := runtime.AcceptParentReview(); err != nil || !accepted {
		t.Fatalf("accepted Runtime: %v %v", accepted, err)
	}
	seedFinalValidationNoneObligation(t, store, source)
	return runtime
}
