package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestControllerReenterHandoffMaterializeAndRuntimeFixReentry(t *testing.T) {
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)

	handoff := buildParentHandoffWithConfig(cfg, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("sealed unpublished candidate handoff: %#v", handoff)
	}
	if !containsParentAction(handoff.AllowedActions, controllerPublicationSurface) || !containsParentAction(handoff.AllowedActions, controllerExecutionSurface) || !containsParentAction(handoff.AllowedActions, "controller-evidence") {
		t.Fatalf("sealed candidate hid publication or reenter operations: %v", handoff.AllowedActions)
	}

	reentered, err := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	handoff = buildParentHandoffWithConfig(cfg, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
		t.Fatalf("reentered candidate handoff must direct materialization: %#v", handoff)
	}
	if !containsParentAction(handoff.AllowedActions, controllerPublicationSurface) {
		t.Fatal("quiescent candidate hid the existing publication owner")
	}

	materialized, err := executeControllerExecution(cfg, store, controllerExecutionCommand{
		Action:             "materialize",
		ExpectedGeneration: reentered.Head.ControllerGeneration,
		SuspensionID:       reentered.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if materialized.Admission == nil || materialized.Admission.Lease.LeaseID == "" {
		t.Fatalf("reentered materialization has no live lease: %#v", materialized)
	}
	if materialized.Admission.Attempt.PredecessorAttemptID == "" {
		t.Fatalf("materialized attempt lost its predecessor identity: %#v", materialized.Admission.Attempt)
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
}

func TestControllerReenterRejectsStaleAndForeignAuthority(t *testing.T) {
	_, store, accepted, candidate, _ := sealedCandidateReentryFixture(t)
	if _, err := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration - 1, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("stale generation reentered the candidate")
	}
	if _, err := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: "foreign-candidate"}); err == nil {
		t.Fatal("foreign candidate reentered the sealed authority")
	}
}

func sealedCandidateReentryFixture(t *testing.T) (config.AppConfig, *controller.Store, controller.ExecutionOperationResult, controller.AcceptedCandidate, *state.StateStore) {
	t.Helper()
	cfg, admission, store, policy := retiredWorkflowCorpus(t, false)
	evidence := canonicalWorkflowEvidence(t, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{Message: "reentry corpus result", Policy: policy, Evidence: evidence})
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

func retiredWorkflowCorpus(t *testing.T, completionQuality bool) (config.AppConfig, controller.Admission, *controller.Store, controller.PublicationPolicy) {
	t.Helper()
	cfg := newCanonicalAppConfig(t)
	cfg.StateBase = filepath.Join(t.TempDir(), "sessions")
	repo := cfg.RepoRoot
	writeAppTestFile(t, repo, "IMPLEMENTATION_RULES.md", "# rules\n")
	writeAppTestFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/successor.md`\n")
	writeAppTestFile(t, repo, "IMPLEMENTATION_TASKS/successor.md", "# successor\n\n## Contract\n\nsuccessor task\n\n## Dependencies\n\nnone\n")
	if completionQuality {
		prepareCompletionQualityCorpus(t, repo)
	}
	policy := commitCorpusWithPublicationRemote(t, repo, "retired workflow test", "retired@example.invalid", "successor authority")
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
