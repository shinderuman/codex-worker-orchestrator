package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func userTaskTransitionFixture(t *testing.T) (config.AppConfig, *controller.Store, controller.UserTaskTransitionInput, *state.StateStore) {
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
	writeAppTestFile(t, source.Workspace.Root, "preserved.txt", "saved implementation\n")
	snapshot, err := controller.CaptureWorkspaceSnapshot(source.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	source, err = store.RecordAdmittedMutation(source, "fixture implementation", "success", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	policy := candidate.Policy
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: source.Attempt.AttemptID, TaskPath: source.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("repo-root", source.Workspace.Root); err != nil {
		t.Fatal(err)
	}
	accepted, err = store.AcceptExecutionCandidate(source, controller.CandidateAcceptanceInput{Message: "saved implementation", Policy: policy, Evidence: canonicalWorkflowEvidence(t, cfg, store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if ok, err := runtime.AcceptParentReview(); err != nil || !ok {
		t.Fatalf("accept fixture: %v %v", ok, err)
	}
	reentered, err = store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "cleanup", ExpectedGeneration: reentered.Head.ControllerGeneration, WorkspaceID: source.Workspace.ID, SealRef: &candidate.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	handoff := buildParentHandoffWithConfig(cfg, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
		t.Fatalf("cleanup handoff lost normal materialization: %#v", handoff)
	}
	if err := os.Remove(filepath.Join(cfg.RepoRoot, source.Attempt.SemanticTaskRef.TaskPath)); err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/successor.md`\n\n## NEXT\n\n## BLOCKED\n")
	writeAppTestFile(t, cfg.RepoRoot, "preserved.txt", "saved implementation\n")
	runControllerActivationGit(t, cfg.RepoRoot, "add", "--all")
	runControllerActivationGit(t, cfg.RepoRoot, "commit", "-qm", "user stops old task and preserves implementation")
	runControllerActivationGit(t, cfg.RepoRoot, "push", policy.Remote, "HEAD:"+policy.RemoteRef)
	authority, err := controller.ResolveCommittedTaskAuthority(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	input := controller.UserTaskTransitionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration, SourceProjectID: cleaned.Head.ProjectSnapshotID, SourceTask: *cleaned.Head.ExecutionTaskRef, SuccessorTask: authority.Task, SuccessorProjectID: authority.ProjectSnapshotID, CleanupTransitionID: cleaned.TransitionID, Reason: "user-directed termination; transfer unfinished lifecycle to successor"}
	return cfg, store, input, runtime
}

func TestControllerExecutionRecoveryDispatchesFailClosedThroughCLI(t *testing.T) {
	t.Parallel()
	cfg, store, accepted, _, _ := sealedCandidateReentryFixture(t)
	_, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "recover-accepted-result-rule-activation", ExpectedGeneration: accepted.Head.ControllerGeneration})
	if err == nil || !strings.Contains(err.Error(), "accepted-result recovery requires exact source identity") {
		t.Fatalf("accepted-result recovery dispatch = %v", err)
	}
	_, err = executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "recover-runtime-start", ExpectedGeneration: accepted.Head.ControllerGeneration})
	if err == nil || strings.Contains(err.Error(), "unsupported controller execution action") {
		t.Fatalf("runtime-start recovery dispatch = %v", err)
	}
}

func TestUserTaskTransitionValidatesAuthorityAndStartsSuccessor(t *testing.T) {
	cfg, store, input, runtime := userTaskTransitionFixture(t)
	oldID := runtime.ReadOr("task.id", "")
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name   string
		mutate func(*controller.UserTaskTransitionInput)
	}{
		{"stale-generation", func(in *controller.UserTaskTransitionInput) { in.ExpectedGeneration-- }},
		{"foreign-task", func(in *controller.UserTaskTransitionInput) { in.SourceTask.TaskPath = in.SuccessorTask.TaskPath }},
		{"foreign-source", func(in *controller.UserTaskTransitionInput) { in.SourceProjectID = "foreign" }},
		{"foreign-successor", func(in *controller.UserTaskTransitionInput) { in.SuccessorTask.ContractDigest = "foreign" }},
		{"stale-project", func(in *controller.UserTaskTransitionInput) { in.SuccessorProjectID = "foreign" }},
		{"unproven-cleanup", func(in *controller.UserTaskTransitionInput) { in.CleanupTransitionID = "missing" }},
		{"missing-reason", func(in *controller.UserTaskTransitionInput) { in.Reason = "" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			denied := input
			change.mutate(&denied)
			if _, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "user-terminate-task", ExpectedGeneration: denied.ExpectedGeneration, UserTermination: &denied}); err == nil {
				t.Fatal("invalid termination admitted")
			}
			after, err := store.LoadHead()
			if err != nil || !reflect.DeepEqual(before, after) || runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
				t.Fatal("rejected transition mutated authority")
			}
		})
	}
	oldCandidate, err := store.LoadAcceptedCandidate(*before.AcceptedCandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "user-terminate-task", ExpectedGeneration: input.ExpectedGeneration, UserTermination: &input})
	if err != nil {
		t.Fatal(err)
	}
	if result.Head.UserTerminationRef == nil || result.Head.MetadataLineageRef != nil || result.Head.AcceptedCandidateRef != nil || result.Head.ExecutionTaskRef != nil {
		t.Fatalf("termination pretended to retire or accept: %#v", result.Head)
	}
	record, err := store.LoadUserTaskTermination(*result.Head.UserTerminationRef)
	if err != nil || !reflect.DeepEqual(record.SourceHead, before) || record.Reason != input.Reason {
		t.Fatalf("source authority lost: %#v %v", record, err)
	}
	preserved, err := store.LoadAcceptedCandidate(*before.AcceptedCandidateRef)
	if err != nil || !reflect.DeepEqual(preserved, oldCandidate) || preserved.EvidenceValid {
		t.Fatalf("old candidate changed: %#v %v", preserved, err)
	}
	if runtime.TaskStatus() != state.TaskStatusUserTerminated || runtime.ReadOr("task.id", "") != oldID || runtime.ReadOr("repo-root", "") != store.Identity().PrimaryRoot {
		t.Fatalf("runtime termination: status=%s task=%s root=%s", runtime.TaskStatus(), runtime.ReadOr("task.id", ""), runtime.ReadOr("repo-root", ""))
	}
	for _, action := range []state.ParentAction{state.ParentActionAccept, state.ParentActionComplete, state.ParentActionResume} {
		_, admitted, err := runtime.AdmitParentAction(action)
		if err != nil || admitted {
			t.Fatalf("old runtime action admitted: %s %v", action, err)
		}
	}
	if _, err := store.RecoverExecutionOperation(result.TransitionID); err != nil {
		t.Fatal(err)
	}
	assertRetiredBootstrap(t, cfg, input.SuccessorTask.TaskPath)
	cfg, err = controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	handoff := buildParentHandoffWithConfig(cfg, state.AttachStateStore(cfg))
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "start" || handoff.ParentRequest == nil || handoff.ParentRequest.TaskAttribution.ActiveTask != input.SuccessorTask.TaskPath {
		t.Fatalf("successor bootstrap/handoff: %#v", handoff)
	}
	lane, err := store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: result.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(lane.Admission.Workspace.Root, "preserved.txt"))
	if err != nil || string(data) != "saved implementation\n" || !lane.Admission.Attempt.SemanticTaskRef.Equal(input.SuccessorTask) {
		t.Fatal("successor failed to inherit saved source")
	}
	if _, err := store.RecoverExecutionOperation(result.TransitionID); err == nil {
		t.Fatal("old closure recovered over new live authority")
	}
}
