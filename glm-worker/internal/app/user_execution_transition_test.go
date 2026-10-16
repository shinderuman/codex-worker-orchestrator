package app

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func userExecutionTerminationFixture(t *testing.T) (config.AppConfig, *controller.Store, controller.UserExecutionTransitionInput, *state.StateStore, string) {
	t.Helper()
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)
	reentered, err := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	lane, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "materialize", ExpectedGeneration: reentered.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	source := *lane.Admission
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          source.Attempt.AttemptID,
		TaskPath:           source.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("repo-root", source.Workspace.Root); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := runtime.AcceptParentReview(); err != nil || !accepted {
		t.Fatalf("fixture review accept: %v %v", accepted, err)
	}
	writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/successor.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/root.md`\n")
	runControllerActivationGit(t, cfg.RepoRoot, "add", "--all")
	runControllerActivationGit(t, cfg.RepoRoot, "commit", "-qm", "user stops the unfinished execution and keeps it queued")
	runControllerActivationGit(t, cfg.RepoRoot, "push", "origin", "HEAD:refs/heads/main")
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := controller.ResolveCommittedTaskAuthority(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	input := controller.UserExecutionTransitionInput{
		ExpectedGeneration: head.ControllerGeneration,
		SourceProjectID:    head.ProjectSnapshotID,
		SourceTask:         *head.ExecutionTaskRef,
		SourceAttemptID:    head.LiveAttemptID,
		SourceLeaseID:      head.LiveLeaseID,
		SuccessorTask:      authority.Task,
		SuccessorProjectID: authority.ProjectSnapshotID,
		Reason:             "user terminates the unfinished execution; requirements stay queued",
	}
	return cfg, store, input, runtime, source.Workspace.Root
}

func TestUserExecutionTerminationEndsUnfinishedAttemptAndKeepsRequirementsQueued(t *testing.T) {
	cfg, store, input, runtime, workspaceRoot := userExecutionTerminationFixture(t)
	result, err := executeControllerExecution(cfg, store, controllerExecutionCommand{
		Action: "user-terminate-execution", ExpectedGeneration: input.ExpectedGeneration, ExecutionTermination: &input,
	})
	if err != nil {
		t.Fatal(err)
	}
	head := result.Head
	if head.UserTerminationRef == nil || head.ExecutionTaskRef != nil || head.LiveAttemptID != "" || head.LiveLeaseID != "" || head.AcceptedCandidateRef != nil {
		t.Fatalf("termination left resumable live authority: %#v", head)
	}
	if head.RootTaskRef == nil || !head.RootTaskRef.Equal(input.SuccessorTask) {
		t.Fatalf("successor authority missing: %#v", head.RootTaskRef)
	}
	record, err := store.LoadUserTaskTermination(*head.UserTerminationRef)
	if err != nil {
		t.Fatal(err)
	}
	if record.Execution == nil || record.Execution.Attempt.AttemptID != input.SourceAttemptID || len(record.Execution.RuntimeRefs) == 0 || record.RuntimeTermination == nil {
		t.Fatalf("termination retention evidence missing: %#v", record)
	}
	project, err := store.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Next) == 0 || project.Next[0] != input.SourceTask.TaskPath {
		t.Fatalf("terminated execution lost its queued requirements: %#v", project.Next)
	}
	if _, queued := taskRefPresent(project.Tasks, input.SourceTask.TaskPath); !queued {
		t.Fatalf("terminated execution task corpus missing: %#v", project.Tasks)
	}
	if runtime.TaskStatus() != state.TaskStatusUserTerminated || runtime.ReadOr("repo-root", "") != store.Identity().PrimaryRoot {
		t.Fatalf("runtime termination: status=%s root=%s", runtime.TaskStatus(), runtime.ReadOr("repo-root", ""))
	}
	for _, action := range []state.ParentAction{state.ParentActionResume, state.ParentActionAccept, state.ParentActionComplete} {
		if _, admitted, err := runtime.AdmitParentAction(action); err != nil || admitted {
			t.Fatalf("terminated runtime admitted %s: %v %v", action, admitted, err)
		}
	}
	if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
		t.Fatalf("terminated lane workspace remains: %v", err)
	}
	if _, err := executeControllerExecution(cfg, store, controllerExecutionCommand{
		Action: "user-terminate-execution", ExpectedGeneration: head.ControllerGeneration, ExecutionTermination: &input,
	}); err == nil {
		t.Fatal("terminated execution was terminated again")
	}
}

func TestUserExecutionTerminationRejectsInvalidAuthority(t *testing.T) {
	cfg, store, input, runtime, _ := userExecutionTerminationFixture(t)
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executeControllerExecution(cfg, store, controllerExecutionCommand{
		Action: "user-terminate-execution", ExpectedGeneration: input.ExpectedGeneration,
	}); err == nil || !strings.Contains(err.Error(), "execution termination requires exact transition authority") {
		t.Fatalf("action without transition input = %v", err)
	}
	for _, change := range []struct {
		name   string
		mutate func(*controller.UserExecutionTransitionInput)
	}{
		{"stale-generation", func(in *controller.UserExecutionTransitionInput) { in.ExpectedGeneration-- }},
		{"foreign-source-project", func(in *controller.UserExecutionTransitionInput) { in.SourceProjectID = "foreign" }},
		{"foreign-attempt", func(in *controller.UserExecutionTransitionInput) { in.SourceAttemptID = "foreign" }},
		{"foreign-lease", func(in *controller.UserExecutionTransitionInput) { in.SourceLeaseID = "foreign" }},
		{"foreign-successor-task", func(in *controller.UserExecutionTransitionInput) { in.SuccessorTask.ContractDigest = "foreign" }},
		{"foreign-successor-project", func(in *controller.UserExecutionTransitionInput) { in.SuccessorProjectID = "foreign" }},
		{"missing-reason", func(in *controller.UserExecutionTransitionInput) { in.Reason = "" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			denied := input
			change.mutate(&denied)
			if _, err := executeControllerExecution(cfg, store, controllerExecutionCommand{
				Action: "user-terminate-execution", ExpectedGeneration: denied.ExpectedGeneration, ExecutionTermination: &denied,
			}); err == nil {
				t.Fatal("invalid execution termination admitted")
			}
			after, err := store.LoadHead()
			if err != nil || !executionTerminationAuthorityEqual(before, after) || runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
				t.Fatal("rejected execution termination mutated authority")
			}
		})
	}
}

func taskRefPresent(refs []controller.SemanticTaskRef, path string) (controller.SemanticTaskRef, bool) {
	for _, ref := range refs {
		if ref.TaskPath == path {
			return ref, true
		}
	}
	return controller.SemanticTaskRef{}, false
}

func executionTerminationAuthorityEqual(before, after controller.RepositoryControllerHead) bool {
	return before.ControllerGeneration == after.ControllerGeneration &&
		before.Status == after.Status &&
		before.LiveAttemptID == after.LiveAttemptID &&
		before.LiveLeaseID == after.LiveLeaseID &&
		before.PendingTransitionID == after.PendingTransitionID &&
		before.ProjectSnapshotID == after.ProjectSnapshotID &&
		before.IntegrationTip == after.IntegrationTip &&
		reflect.DeepEqual(before.RootTaskRef, after.RootTaskRef) &&
		reflect.DeepEqual(before.ExecutionTaskRef, after.ExecutionTaskRef) &&
		reflect.DeepEqual(before.AcceptedCandidateRef, after.AcceptedCandidateRef)
}
