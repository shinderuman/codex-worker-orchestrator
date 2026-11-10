package app

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func TestControllerReenterHandoffMaterializeAndRuntimeFixReentry(t *testing.T) {
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)
	_, staleErr := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration - 1, CandidateID: candidate.CandidateID})
	_, foreignErr := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: "foreign-candidate"})
	if staleErr == nil || foreignErr == nil {
		t.Fatalf("stale generation or foreign candidate reentered sealed authority: stale=%v foreign=%v", staleErr, foreignErr)
	}

	handoff := buildParentHandoffWithConfig(cfg, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface ||
		!containsParentAction(handoff.AllowedActions, controllerPublicationSurface) ||
		!containsParentAction(handoff.AllowedActions, controllerExecutionSurface) ||
		!containsParentAction(handoff.AllowedActions, "controller-evidence") {
		t.Fatalf("sealed candidate hid its canonical publication/reentry operations: %#v", handoff)
	}

	reentered, err := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	handoff = buildParentHandoffWithConfig(cfg, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
		t.Fatalf("reentered candidate handoff must direct materialization: %#v", handoff)
	}
	var materializeCommand workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &materializeCommand); err != nil {
		t.Fatal(err)
	}
	if materializeCommand.Action != "materialize" || materializeCommand.ExpectedGeneration != reentered.Head.ControllerGeneration || materializeCommand.SuspensionID != reentered.Suspension.SnapshotID {
		t.Fatalf("reentry handoff lost its exact materialization authority: %#v", materializeCommand)
	}
	if _, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: reentered.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("publication advanced the candidate invalidated for reentry")
	}

	materialized, err := store.MaterializeExecution(controller.MaterializeExecutionInput{
		ExpectedGeneration: materializeCommand.ExpectedGeneration,
		EpisodeID:          materializeCommand.EpisodeID,
		EpisodeRevision:    materializeCommand.EpisodeRevision,
		SuspensionID:       materializeCommand.SuspensionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if materialized.Admission == nil || materialized.Admission.Lease.LeaseID == "" ||
		materialized.Admission.Attempt.PredecessorAttemptID == "" {
		t.Fatalf("reentered materialization has no canonical predecessor or live lease: %#v", materialized)
	}
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("Controller commit unexpectedly completed runtime: %s", runtime.TaskStatus())
	}
	oldBinding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil || oldBinding.AttemptID != materialized.Admission.Attempt.PredecessorAttemptID {
		t.Fatalf("materialization changed retained predecessor binding before runtime completion: %#v %v", oldBinding, err)
	}
	resumed := runControllerRecoveryCLI(t, cfg, materialized.TransitionID)
	if resumed.TransitionID != materialized.TransitionID || resumed.Head.ControllerGeneration != materialized.Head.ControllerGeneration ||
		resumed.Admission == nil || resumed.Admission.Attempt.AttemptID != materialized.Admission.Attempt.AttemptID {
		t.Fatalf("recovery changed canonical Controller attempt: %#v", resumed)
	}
	if runtime.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("controller reentry left the runtime at %s", runtime.TaskStatus())
	}
	open, err := runtime.CurrentParentReview()
	if err != nil || open == nil || open.PacketStatus != string(packet.StatusNeedsSolReview) || open.Risk != string(packet.RiskHigh) {
		t.Fatalf("reentered runtime review = %#v err=%v", open, err)
	}
	plan, err := runtime.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionReview || !plan.Allows(state.ParentActionFix) || plan.Allows(state.ParentActionComplete) || plan.Allows(state.ParentActionAccept) {
		t.Fatalf("reentered runtime plan must admit fix: %#v", plan)
	}

	if _, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: materialized.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("publication advanced the superseded candidate while the reentered attempt is live")
	}
	laneCfg := cfg
	laneCfg.RepoRoot = materialized.Admission.Workspace.Root
	handoff = buildParentHandoffWithConfig(laneCfg, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != string(state.ParentActionReview) {
		inconsistency := ""
		if handoff.Inconsistency != nil {
			inconsistency = *handoff.Inconsistency
		}
		t.Fatalf("reentered live attempt handoff: %#v inconsistency=%s", handoff, inconsistency)
	}
	replayed, err := workflow.ExecuteControllerExecution(cfg, store, workflow.ControllerExecutionCommand{Action: "recover", TransitionID: materialized.TransitionID})
	if err != nil || replayed.Head.ControllerGeneration != materialized.Head.ControllerGeneration || runtime.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("repeated recovery changed the transition or runtime state: %#v %v", replayed, err)
	}
	afterReplay := buildParentHandoffWithConfig(laneCfg, runtime)
	if !afterReplay.Consistent || afterReplay.RequiredAction == nil || *afterReplay.RequiredAction != string(state.ParentActionReview) {
		t.Fatalf("idempotent recovery changed canonical review handoff: %#v", afterReplay)
	}
	foreign := oldBinding
	foreign.AttemptID = "foreign-attempt"
	if err := runtime.SaveControllerRuntimeBinding(foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ExecuteControllerExecution(cfg, store, workflow.ControllerExecutionCommand{Action: "recover", TransitionID: materialized.TransitionID}); err == nil {
		t.Fatal("foreign runtime attempt was accepted by materialization replay")
	}
}

func runControllerRecoveryCLI(t *testing.T, cfg config.AppConfig, transitionID string) controller.ExecutionOperationResult {
	t.Helper()
	command, err := json.Marshal(workflow.ControllerExecutionCommand{Action: "recover", TransitionID: transitionID})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	recognized, err := runControllerOperations(
		[]string{controllerAuthorityFlag, controllerExecutionSurface},
		func() (config.AppConfig, error) { return cfg, nil },
		bytes.NewReader(command), &output,
	)
	if err != nil || !recognized {
		t.Fatalf("typed Controller recovery failed: recognized=%v err=%v", recognized, err)
	}
	var recovered controller.ExecutionOperationResult
	if err := json.Unmarshal(output.Bytes(), &recovered); err != nil {
		t.Fatalf("typed Controller recovery output is invalid: %v", err)
	}
	return recovered
}

func TestPromotedTaskMetadataKeepsPublicationAndReentryAvailable(t *testing.T) {
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"promoted", "reentered"} {
		assertRetiredBootstrap(t, cfg, candidate.TaskRef.TaskPath)
		handoff := buildParentHandoffWithConfig(cfg, runtime)
		if !handoff.Consistent || !containsParentAction(handoff.AllowedActions, controllerExecutionSurface) {
			t.Fatalf("%s Task closure blocked normal lifecycle: %#v", stage, handoff)
		}
		if stage == "promoted" {
			if !containsParentAction(handoff.AllowedActions, controllerPublicationSurface) {
				t.Fatal("promoted candidate lost its publication owner")
			}
			if _, err := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
				t.Fatal(err)
			}
		} else if handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface || handoff.RequiredActionParameters["controller_command_json"] == "" {
			t.Fatal("reentered promoted candidate lost its machine recovery command")
		}
	}
}

func sealedCandidateReentryFixture(t *testing.T, quality ...bool) (config.AppConfig, *controller.Store, controller.ExecutionOperationResult, controller.AcceptedCandidate, *state.StateStore) {
	t.Helper()
	useQuality := len(quality) > 0 && quality[0]
	cfg, admission, store, policy := retiredWorkflowCorpus(t, useQuality)
	evidence := canonicalWorkflowEvidence(t, cfg, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "reentry corpus result", Policy: policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	runtimeCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := state.NewStateStore(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveCurrentTaskAuthority(admission.Attempt.SemanticTaskRef.TaskPath, []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	acceptedReview, err := runtime.AcceptParentReview()
	if err != nil || !acceptedReview {
		t.Fatalf("runtime accept = %v err=%v", acceptedReview, err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          admission.Attempt.AttemptID,
		TaskPath:           admission.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	return cfg, store, accepted, candidate, runtime
}

func retiredWorkflowCorpus(t *testing.T, completionQuality bool, appendedPlanSections ...string) (config.AppConfig, controller.Admission, *controller.Store, controller.PublicationPolicy) {
	t.Helper()
	cfg, policy := prepareRetiredWorkflowCorpus(t, completionQuality, appendedPlanSections...)
	admission, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, admission, store, policy
}

func prepareRetiredWorkflowCorpus(t *testing.T, completionQuality bool, appendedPlanSections ...string) (config.AppConfig, controller.PublicationPolicy) {
	t.Helper()
	cfg := newCanonicalAppConfig(t)
	cfg.StateBase = filepath.Join(t.TempDir(), "sessions")
	repo := cfg.RepoRoot
	writeAppTestFile(t, repo, "IMPLEMENTATION_RULES.md", "# rules\n")
	writeAppTestFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/successor.md`\n"
	for _, addition := range appendedPlanSections {
		plan += addition
	}
	writeAppTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", plan)
	writeAppTestFile(t, repo, "IMPLEMENTATION_TASKS/successor.md", "# successor\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\nsuccessor task\n\n## Dependencies\n\nnone\n")
	if completionQuality {
		prepareCompletionQualityCorpus(t, repo)
	}
	policy := commitCorpusWithPublicationRemote(t, repo, "retired workflow test", "retired@example.invalid", "successor authority")
	return cfg, policy
}

func commitCorpusWithPublicationRemote(t *testing.T, repo, userName, userEmail, commitMessage string) controller.PublicationPolicy {
	t.Helper()
	runControllerActivationGit(t, repo, "config", "user.name", userName)
	runControllerActivationGit(t, repo, "config", "user.email", userEmail)
	runControllerActivationGit(t, repo, "add", ".")
	runControllerActivationGit(t, repo, "commit", "-qm", commitMessage)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runControllerActivationGit(t, repo, "init", "--bare", remote)
	runControllerActivationGit(t, repo, "remote", "add", "origin", remote)
	runControllerActivationGit(t, repo, "push", "-q", "origin", "HEAD:refs/heads/main")
	ref, err := exec.Command("git", "-C", repo, "symbolic-ref", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return controller.PublicationPolicy{Remote: "origin", LocalRef: strings.TrimSpace(string(ref)), RemoteRef: "refs/heads/main"}
}
