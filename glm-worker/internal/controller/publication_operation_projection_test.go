package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func seedAwaitingCompletionRuntime(t *testing.T, store *Store, source Admission, sourceEvidence bool) {
	t.Helper()
	cfg := WorkflowTaskConfig(store.config, store.identity, source.Attempt.SemanticTaskRef)
	runtime, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveCurrentTaskAuthority(source.Attempt.SemanticTaskRef.TaskPath, []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if sourceEvidence {
		snapshot, err := state.CaptureGitSnapshot(source.Workspace.Root)
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.SaveWorkerEndSnapshot(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: source.Attempt.AttemptID, TaskPath: source.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	if !runtime.Exists(state.FinalValidationObligationKey) {
		if err := runtime.Write(state.FinalValidationObligationKey, `{"None":true}`); err != nil {
			t.Fatal(err)
		}
	}
}

func bindPublicationUpstream(t *testing.T, store *Store, policy PublicationPolicy) {
	t.Helper()
	branch := strings.TrimPrefix(policy.LocalRef, "refs/heads/")
	runControllerGit(t, store.identity.PrimaryRoot, "config", "branch."+branch+".remote", policy.Remote)
	runControllerGit(t, store.identity.PrimaryRoot, "config", "branch."+branch+".merge", policy.RemoteRef)
}

func TestParentCompletionProjectionRetainsPublicationAcrossTasks(t *testing.T) {
	t.Parallel()
	fixture, policy := configurePublicationTestFixture(t, newFindingAcceptanceFixture(t, "IMPLEMENTATION_TASKS/third.md"))
	for sequence := 0; sequence < 2; sequence++ {
		if sequence == 1 {
			primary := fixture.store.identity.PrimaryRoot
			plan, err := os.ReadFile(filepath.Join(primary, "IMPLEMENTATION_PLAN.local.md"))
			if err != nil {
				t.Fatal(err)
			}
			writeSuspensionTestFile(t, primary, "IMPLEMENTATION_PLAN.local.md", string(plan)+"\n")
		}
		source := publicationTestEdit(t, fixture, "result.txt", strings.Repeat("reviewed result\n", sequence+1))
		accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "task result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
		if err != nil {
			t.Fatal(err)
		}
		promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
		if err != nil {
			t.Fatal(err)
		}
		operation, err := fixture.store.ProjectParentCompletion()
		if err != nil || operation.Operation != PublicationOperationPublish || operation.Command == nil || operation.Command.CandidateID != candidate.CandidateID || operation.Command.ExpectedGeneration != promoted.Head.ControllerGeneration {
			t.Fatalf("task %d lost its owned publication: operation=%#v err=%v", sequence+1, operation, err)
		}
		if sequence == 1 {
			fixture.source = reenterProjectedCompletion(t, fixture.store, promoted.Head, candidate)
			source = publicationTestEdit(t, fixture, "result.txt", "reviewed result after reentry\n")
			promoted, candidate = promoteTerminalTestSource(t, fixture, policy, source)
			operation, err = fixture.store.ProjectParentCompletion()
			if err != nil || operation.Operation != PublicationOperationPublish || operation.Command == nil || operation.Command.CandidateID != candidate.CandidateID || operation.Command.ExpectedGeneration != promoted.Head.ControllerGeneration {
				t.Fatalf("reentered continuation lost its owned publication: %#v err=%v", operation, err)
			}
		}
		published, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
		if err != nil || published.Head.MetadataLineageRef == nil {
			t.Fatalf("task %d did not retain its completion history: %v", sequence+1, err)
		}
		if sequence == 0 {
			materialized, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: published.Head.ControllerGeneration})
			if err != nil {
				t.Fatal(err)
			}
			if materialized.Admission == nil {
				t.Fatal("successor Task has no materialized execution")
			}
			fixture.source = *materialized.Admission
			seedFinalValidationNoneObligation(t, fixture.store, fixture.source)
		}
	}
}

func reenterProjectedCompletion(t *testing.T, store *Store, head RepositoryControllerHead, candidate AcceptedCandidate) Admission {
	t.Helper()
	if _, err := store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
		t.Fatal(err)
	}
	operation, err := store.ProjectParentCompletion()
	if err != nil || operation.Execution == nil || operation.Execution.Action != "cleanup" || operation.Execution.SealRef == nil {
		t.Fatalf("reentered continuation lost its exact lane cleanup: %#v err=%v", operation, err)
	}
	command := operation.Execution
	if _, err := store.CleanupExecution(CleanupExecutionInput{ExpectedGeneration: command.ExpectedGeneration, WorkspaceID: command.WorkspaceID, SealRef: *command.SealRef}); err != nil {
		t.Fatal(err)
	}
	operation, err = store.ProjectParentCompletion()
	if err != nil || operation.Execution == nil || operation.Execution.Action != "materialize" {
		t.Fatalf("cleaned continuation lost its exact materialization: %#v err=%v", operation, err)
	}
	command = operation.Execution
	materialized, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: command.ExpectedGeneration, SuspensionID: command.SuspensionID})
	if err != nil || materialized.Admission == nil {
		t.Fatalf("projected continuation could not materialize: %v", err)
	}
	return *materialized.Admission
}

func TestParentCompletionProjectionSuppliesFixedBindingAndSemanticSlots(t *testing.T) {
	t.Parallel()
	for _, remote := range []string{"origin", "second"} {
		t.Run(remote, func(t *testing.T) {
			fixture, policy := newPublicationTestFixture(t)
			url := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "remote", "get-url", "origin")
			runControllerGit(t, fixture.store.identity.PrimaryRoot, "remote", "add", "second", url)
			policy.Remote = remote
			bindPublicationUpstream(t, fixture.store, policy)
			source := publicationTestEdit(t, fixture, "projection.txt", "typed completion source\n")
			seedAwaitingCompletionRuntime(t, fixture.store, source, true)
			operation, err := fixture.store.ProjectParentCompletion()
			if err != nil {
				t.Fatal(err)
			}
			if operation.Operation != PublicationOperationAccept || operation.Command == nil || operation.Command.Policy == nil {
				t.Fatalf("completion projection: %#v", operation)
			}
			if operation.Command.ExpectedGeneration != source.Head.ControllerGeneration || operation.Command.Policy.Remote != policy.Remote || operation.Command.Policy.LocalRef != policy.LocalRef || operation.Command.Policy.RemoteRef != policy.RemoteRef {
				t.Fatalf("completion machine binding: %#v", operation.Command)
			}
			if len(operation.SemanticSlots) != 3 || operation.SemanticSlots[0] != CompletionSlotMessage || operation.SemanticSlots[1] != CompletionSlotRequireInstall || operation.SemanticSlots[2] != CompletionSlotRequiredValidations {
				t.Fatalf("semantic inputs: %#v", operation.SemanticSlots)
			}
			if operation.Command.RequiredValidations != nil {
				t.Fatalf("completion machine prefilled the parent-determined validation set: %#v", operation.Command.RequiredValidations)
			}
			if len(operation.Command.Evidence) != 0 || operation.Command.EvidenceRecord != nil {
				t.Fatalf("completion asks the parent to rebuild evidence: %#v", operation.Command)
			}
		})
	}
}

func TestParentCompletionProjectionMaterializesInvalidatedCandidates(t *testing.T) {
	t.Parallel()
	for _, promote := range []bool{false, true} {
		name := "prepared"
		if promote {
			name = "promoted"
		}
		t.Run(name, func(t *testing.T) {
			fixture, _, accepted, candidate := primaryPromotionFixture(t)
			generation := accepted.Head.ControllerGeneration
			if promote {
				promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID})
				if err != nil {
					t.Fatal(err)
				}
				generation = promoted.Head.ControllerGeneration
			}
			reentered, err := fixture.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID})
			if err != nil {
				t.Fatal(err)
			}
			operation, err := fixture.store.ProjectParentCompletion()
			if err != nil || operation.Surface != ExecutionOperationSurface || operation.Execution == nil || operation.Execution.ExpectedGeneration != reentered.Head.ControllerGeneration {
				t.Fatalf("invalidated %s candidate did not project its bound execution recovery: %#v err=%v", name, operation, err)
			}
			if operation.Operation != "cleanup" && operation.Operation != "materialize" {
				t.Fatalf("invalidated candidate projected %q instead of source recovery", operation.Operation)
			}
		})
	}
}

func TestParentCompletionProjectionRejectsUnsettablePolicyAndRunningRuntime(t *testing.T) {
	t.Parallel()
	fixture, _ := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "policy.txt", "policy derivation source\n")
	seedAwaitingCompletionRuntime(t, fixture.store, source, true)
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "remote", "add", "second", t.TempDir())

	if _, err := fixture.store.ProjectParentCompletion(); err == nil || !strings.Contains(err.Error(), "publication policy") {
		t.Fatalf("publication without a configured upstream was accepted: %v", err)
	}
}

func TestParentCompletionProjectionWaitsForRuntimeCompletion(t *testing.T) {
	t.Parallel()
	fixture, _ := newPublicationTestFixture(t)
	publicationTestEdit(t, fixture, "running.txt", "still running\n")

	operation, err := fixture.store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if operation.Surface != "" || operation.Operation != PublicationOperationNone {
		t.Fatalf("running runtime admitted completion: %#v", operation)
	}
}

func TestParentCompletionProjectionWithoutAcceptedSourceOffersReopen(t *testing.T) {
	t.Parallel()
	fixture, _ := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "legacy-source.txt", "accepted source without retained proof\n")
	seedAwaitingCompletionRuntime(t, fixture.store, source, false)
	operation, err := fixture.store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if operation.Operation != PublicationOperationReopen || operation.Command == nil || operation.Command.Policy != nil {
		t.Fatal("missing source proof was treated as completed or required publication policy")
	}
	if _, err := fixture.store.ProjectParentCompletionOperation(PublicationOperationAccept); err == nil {
		t.Fatal("completion bypassed mandatory source re-review")
	}
}

func TestParentCompletionProjectionRejectsRuntimeOnlyCandidateAuthority(t *testing.T) {
	t.Parallel()
	fixture, _ := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "runtime-only.txt", "candidate authority must remain with Controller\n")
	seedAwaitingCompletionRuntime(t, fixture.store, source, true)
	cfg := WorkflowTaskConfig(fixture.store.config, fixture.store.identity, source.Attempt.SemanticTaskRef)
	runtime := state.AttachStateStore(cfg)
	taskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := state.SnapshotDigest{Head: source.Snapshot.Head, IndexDigest: source.Snapshot.IndexDigest, WorktreeDigest: source.Snapshot.WorktreeDigest}
	tree := controllerGitOutput(t, source.Workspace.Root, "rev-parse", "HEAD^{tree}")
	candidate := state.PublicationCandidate{Version: 1, TaskID: taskID, BaseHead: snapshot.Head, CommitOID: snapshot.Head, TreeOID: tree, MessageDigest: strings.Repeat("a", 64), Snapshot: snapshot, SnapshotID: state.ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest), PreparedAt: time.Now().UTC()}
	if err := runtime.SavePublicationCandidate(candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RecordPublicationInvalidatingFinding(state.ParentOriginUserAmendment, state.ParentCauseSolGate); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ProjectParentCompletionOperation(PublicationOperationAccept); err == nil {
		t.Fatal("Runtime reopen-only finding was overridden by Controller completion")
	}
	if _, err := fixture.store.AcceptParentCompletion(source, CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "must reject"}); err == nil || !strings.Contains(err.Error(), "runtime-only") {
		t.Fatalf("candidate owner did not enforce Runtime authority: %v", err)
	}

}
