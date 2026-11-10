package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactioncmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type pulledCandidateFixture struct {
	cfg        config.AppConfig
	runtime    *state.StateStore
	scripted   *typedCompletionRunner
	store      *controller.Store
	candidate  controller.AcceptedCandidate
	policy     controller.PublicationPolicy
	generation uint64
	tip        string
}

type pulledAdmissionFixture struct {
	cfg      config.AppConfig
	runtime  *state.StateStore
	scripted *typedCompletionRunner
	store    *controller.Store
	policy   controller.PublicationPolicy
	tip      string
}

func candidateBehindMetadataPull(t *testing.T) pulledCandidateFixture {
	t.Helper()
	binary := typedCompletionWorkerBinary(t)
	cfg, runtime, _, policy, scripted := typedCompletionReviewFixture(t, binary, true, false)
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("parent accept left Runtime at %s", runtime.TaskStatus())
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := canonicalWorkflowAdmission(t, cfg, store)
	accepted, err := store.AcceptExecutionCandidate(source, controller.CandidateAcceptanceInput{
		Message:             "pre-pull candidate",
		Policy:              policy,
		Evidence:            canonicalWorkflowEvidence(t, cfg, store, source, policy),
		RequiredValidations: &controller.RequiredValidationDeclaration{Forms: []controller.FinalValidationRequest{{Form: "go-test", WorkingDir: "glm-worker"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	primaryCfg := cfg
	primaryCfg.RepoRoot = store.Identity().PrimaryRoot
	tip := pullExternalSourceChange(t, primaryCfg, policy, map[string]string{"IMPLEMENTATION_HISTORY.md": "cross-lane parent metadata record\n"}, nil)
	return pulledCandidateFixture{cfg: cfg, runtime: runtime, scripted: scripted, store: store, candidate: candidate, policy: policy, generation: accepted.Head.ControllerGeneration, tip: tip}
}

func rebindStaleCandidateThroughStagedAction(t *testing.T, cfg config.AppConfig) {
	t.Helper()
	handoff := executeParentHandoff(t, cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("stale candidate handoff: %v %#v", stringPointerValue(handoff.Inconsistency), handoff)
	}
	var command controllerPublicationCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != controller.PublicationOperationRebind {
		t.Fatalf("stale candidate advertised %q instead of the typed rebind", command.Action)
	}
	executeStagedParentAction(t, cfg, handoff.RequiredActionParameters["controller_command_json"])
}

func refuseStaleFinalizeCheck(t *testing.T, cfg config.AppConfig, form string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cfg.RepoRoot); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	home := filepath.Dir(cfg.StateBase)
	t.Setenv("HOME", home)
	t.Setenv("GLM_WORKER_HOME", home)
	t.Setenv("CODEX_CONFIG_DIR", cfg.CodexConfigDir)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg.ClaudeConfigDir)
	var output, diagnostics bytes.Buffer
	code := parentactioncmd.Run([]string{"finalize-check", form}, &output, &diagnostics)
	if code == 0 {
		t.Fatalf("finalize-check %s admitted a stale candidate source: %s", form, output.String())
	}
	if !strings.Contains(diagnostics.String(), controller.PublicationOperationRebind) {
		t.Fatalf("finalize-check %s refusal lost the typed rebind guidance: %s", form, diagnostics.String())
	}
}

func driveReboundExecutionSteps(t *testing.T, cfg config.AppConfig, store *controller.Store) {
	t.Helper()
	for step := 0; step < 4; step++ {
		handoff := executeParentHandoff(t, cfg)
		if handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
			return
		}
		payload := handoff.RequiredActionParameters["controller_command_json"]
		if payload == "" {
			t.Fatalf("rebound execution handoff lost its typed command: %#v", handoff)
		}
		var command workflow.ControllerExecutionCommand
		if err := json.Unmarshal([]byte(payload), &command); err != nil {
			t.Fatal(err)
		}
		if _, err := workflow.ExecuteControllerExecution(cfg, store, command); err != nil {
			t.Fatalf("rebound execution step %s: %v", command.Action, err)
		}
	}
	t.Fatal("rebound execution steps did not reach the reentered review boundary")
}

func TestUnpublishedCandidateMetadataPullProjectsTypedRebindBeforeValidation(t *testing.T) {
	f := candidateBehindMetadataPull(t)
	handoff := executeParentHandoff(t, f.cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("stale candidate handoff: %v %#v", stringPointerValue(handoff.Inconsistency), handoff)
	}
	var command controllerPublicationCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != controller.PublicationOperationRebind || command.CandidateID != f.candidate.CandidateID || command.SourceOID != f.tip || command.ExpectedGeneration != f.generation {
		t.Fatalf("typed rebind command: %#v", command)
	}
	if handoff.RequiredActionParameters["form"] != "" {
		t.Fatalf("stale candidate advertised final validation before the rebind: %#v", handoff.RequiredActionParameters)
	}

	refuseStaleFinalizeCheck(t, f.cfg, "go-test")
	if _, err := executeControllerPublication(f.cfg, f.store, controllerPublicationCommand{Action: "promote", ExpectedGeneration: f.generation, CandidateID: f.candidate.CandidateID}); err == nil || !strings.Contains(err.Error(), controller.PublicationOperationRebind) {
		t.Fatalf("stale candidate promoted before the rebind: %v", err)
	}
	remoteFields := strings.Fields(string(appTestGitOutput(t, f.store.Identity().PrimaryRoot, "ls-remote", f.policy.Remote, f.policy.RemoteRef)))
	if len(remoteFields) != 2 || remoteFields[0] != f.tip {
		t.Fatalf("stale-source refusals moved the remote: %v", remoteFields)
	}
}

func TestMetadataPullRebindRebindsSourceAndInvalidatesEvidence(t *testing.T) {
	f := candidateBehindMetadataPull(t)
	rebindStaleCandidateThroughStagedAction(t, f.cfg)
	reboundHead, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	rebound, err := f.store.LoadAcceptedCandidate(*reboundHead.AcceptedCandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.BaseOID != f.tip || rebound.EvidenceValid || len(rebound.Evidence) != 0 || rebound.CandidateID == f.candidate.CandidateID {
		t.Fatalf("rebind did not rebind the advanced source and invalidate evidence: %#v", rebound)
	}
	if remoteFields := strings.Fields(string(appTestGitOutput(t, f.store.Identity().PrimaryRoot, "ls-remote", f.policy.Remote, f.policy.RemoteRef))); len(remoteFields) != 2 || remoteFields[0] != f.tip {
		t.Fatalf("rebind moved the remote: %v", remoteFields)
	}
}

func TestReboundCandidatePublishesOnceOnPulledSource(t *testing.T) {
	t.Parallel()
	runIsolatedScenario(t, isolatedScenarioKeyReboundCandidatePublish, "default")
}

func reboundCandidatePublishBody(t *testing.T) {
	f := candidateBehindMetadataPull(t)
	rebindStaleCandidateThroughStagedAction(t, f.cfg)
	driveReboundExecutionSteps(t, f.cfg, f.store)
	if f.runtime.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("rebound reentry did not reopen the runtime review: %s", f.runtime.TaskStatus())
	}
	publishFreshRoundOnceOnPulledSource(t, f.cfg, f.scripted, f.store, f.tip, Command{Mode: ModeFix, Payload: "recovered source review"})
}

func publishFreshRoundOnceOnPulledSource(t *testing.T, cfg config.AppConfig, scripted *typedCompletionRunner, store *controller.Store, tip string, restart Command) {
	t.Helper()
	laneCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		scripted.cfg = actual
		return scripted
	}
	var output bytes.Buffer
	if err := Execute(restart, laneCfg, factory, &output, io.Discard); err != nil {
		t.Fatalf("fresh worker round: %v %s", err, output.String())
	}
	freshCfg := scripted.cfg
	freshState := state.AttachStateStore(freshCfg)
	if freshState.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("fresh fix round left the runtime at %s", freshState.TaskStatus())
	}
	if scripted.workerRounds != 2 {
		t.Fatalf("source recovery did not require a fresh worker round: %d", scripted.workerRounds)
	}

	manifest := writeParentReviewEvidenceManifest(t, parentevidence.Manifest{
		Version: parentevidence.ManifestVersion,
		Reason:  "verify the recovered implementation source",
		Diff:    []parentevidence.DiffRequest{{Question: "review the recovered source change", Paths: []string{"typed-result.txt"}, BudgetBytes: 4096}},
	})
	var proof bytes.Buffer
	if err := printParentEvidence(Command{EvidenceManifest: manifest}, freshCfg, freshState, &proof); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proof.String(), "+worker round 2") {
		t.Fatalf("parent did not receive the fresh round diff: %s", proof.String())
	}
	if err := Execute(Command{Mode: ModeAccept}, freshCfg, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	completionHandoff := executeParentHandoff(t, freshCfg)
	if !completionHandoff.Consistent || completionHandoff.RequiredAction == nil || *completionHandoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("fresh completion handoff: %v %#v", stringPointerValue(completionHandoff.Inconsistency), completionHandoff)
	}
	var completionCommand controllerPublicationCommand
	if err := json.Unmarshal([]byte(completionHandoff.RequiredActionParameters["controller_command_json"]), &completionCommand); err != nil {
		t.Fatal(err)
	}
	if completionCommand.Action != controller.PublicationOperationAccept {
		t.Fatalf("fresh completion action: %#v", completionCommand)
	}
	runTypedPublicationStep(t, freshCfg, completionHandoff.RequiredActionParameters, true, false, true, false)
	assertTypedCompletionTerminalState(t, freshCfg, freshState)
	published, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := store.LoadTerminalTaskRecord(*published.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	final, err := store.LoadAcceptedCandidate(terminal.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if final.BaseOID != tip {
		t.Fatalf("single publication did not build on the pulled source %s: %#v", tip, final)
	}
	terminalHandoff := executeParentHandoff(t, freshCfg)
	if terminalHandoff.Consistent && terminalHandoff.RequiredAction != nil && *terminalHandoff.RequiredAction == controllerPublicationSurface && terminalHandoff.RequiredActionParameters["controller_command_json"] == "" {
		t.Fatalf("published task dead-ended at controller-publication without a typed command: %#v", terminalHandoff)
	}
}

func awaitingAdmissionBehindPull(t *testing.T) pulledAdmissionFixture {
	t.Helper()
	binary := typedCompletionWorkerBinary(t)
	cfg, runtime, _, policy, scripted := typedCompletionReviewFixture(t, binary, true, false)
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("parent accept left Runtime at %s", runtime.TaskStatus())
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	primaryCfg := cfg
	primaryCfg.RepoRoot = store.Identity().PrimaryRoot
	tip := pullExternalSourceChange(t, primaryCfg, policy, map[string]string{"IMPLEMENTATION_HISTORY.md": "cross-lane parent metadata record\n"}, nil)
	return pulledAdmissionFixture{cfg: cfg, runtime: runtime, scripted: scripted, store: store, policy: policy, tip: tip}
}

func adoptPulledAdvancementThroughStagedAction(t *testing.T, f pulledAdmissionFixture, executeInProcess bool) {
	t.Helper()
	handoff := executeParentHandoff(t, f.cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("pulled admission handoff: %v %#v", stringPointerValue(handoff.Inconsistency), handoff)
	}
	var command controllerPublicationCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != controller.PublicationOperationAdoptMutable {
		t.Fatalf("pulled admission advertised %q instead of the typed adopt-mutable", command.Action)
	}
	if executeInProcess {
		executeStagedParentAction(t, f.cfg, handoff.RequiredActionParameters["controller_command_json"])
	} else {
		executeStagedParentActionThroughChild(t, f.cfg, handoff.RequiredActionParameters["controller_command_json"])
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.Status != controller.ControllerStatusActive || head.LiveLeaseID != "" || head.IntegrationTip != f.tip || head.AcceptedCandidateRef != nil {
		t.Fatalf("staged adopt-mutable did not adopt the pulled source without fail-closing: %#v", head)
	}
	if remoteFields := strings.Fields(string(appTestGitOutput(t, f.store.Identity().PrimaryRoot, "ls-remote", f.policy.Remote, f.policy.RemoteRef))); len(remoteFields) != 2 || remoteFields[0] != f.tip {
		t.Fatalf("adoption moved the remote: %v", remoteFields)
	}
}

func TestAwaitingAdmissionPullExecutesStagedAdoptMutable(t *testing.T) {
	f := awaitingAdmissionBehindPull(t)
	adoptPulledAdvancementThroughStagedAction(t, f, false)
	resume := executeParentHandoff(t, f.cfg)
	if !resume.Consistent || resume.RequiredAction == nil || *resume.RequiredAction != controllerExecutionSurface {
		t.Fatalf("post-adoption handoff lost the execution resume surface: %v %#v", stringPointerValue(resume.Inconsistency), resume)
	}
	var resumeCommand workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(resume.RequiredActionParameters["controller_command_json"]), &resumeCommand); err != nil || resumeCommand.Action != "materialize" || resumeCommand.SuspensionID == "" {
		t.Fatalf("post-adoption handoff lost the typed resume command: %#v %v", resumeCommand, err)
	}
}

func TestAdoptedAdmissionPublishesOnceOnPulledSource(t *testing.T) {
	t.Parallel()
	runIsolatedScenario(t, isolatedScenarioKeyAdoptedAdmissionPublish, "default")
}

func adoptedAdmissionPublishBody(t *testing.T) {
	f := awaitingAdmissionBehindPull(t)
	adoptPulledAdvancementThroughStagedAction(t, f, true)
	driveReboundExecutionSteps(t, f.cfg, f.store)
	if f.runtime.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("adopted advancement did not reopen the runtime review: %s", f.runtime.TaskStatus())
	}
	publishFreshRoundOnceOnPulledSource(t, f.cfg, f.scripted, f.store, f.tip, Command{Mode: ModeFix, Payload: "recovered source review"})
}

func advanceRemoteBeyondPulledPrimary(t *testing.T, primary string, policy controller.PublicationPolicy, path, data string) string {
	t.Helper()
	external := filepath.Join(t.TempDir(), "external")
	runControllerActivationGit(t, primary, "clone", "--quiet", primary, external)
	runControllerActivationGit(t, external, "config", "user.email", "test@example.com")
	runControllerActivationGit(t, external, "config", "user.name", "external")
	writeAppTestFile(t, external, path, data)
	runControllerActivationGit(t, external, "add", "--all")
	runControllerActivationGit(t, external, "commit", "-qm", "remote advancement beyond the pulled primary")
	remote := strings.TrimSpace(string(appTestGitOutput(t, primary, "remote", "get-url", policy.Remote)))
	runControllerActivationGit(t, external, "push", remote, "HEAD:"+policy.RemoteRef)
	return strings.TrimSpace(string(appTestGitOutput(t, external, "rev-parse", "HEAD")))
}

func prepareStagedPublication(t *testing.T, cfg config.AppConfig) (string, controllerPublicationCommand) {
	t.Helper()
	exit, output, diagnostics := runParentActionCLIInProcess(t, cfg, "prepare", controllerPublicationSurface)
	if exit != 0 {
		t.Fatalf("prepare staged parent action: %s", diagnostics)
	}
	var prepared struct {
		Path  string `json:"path"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(output), &prepared); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	template := ""
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "COMMAND_TEMPLATE: ") {
			template = strings.TrimPrefix(line, "COMMAND_TEMPLATE: ")
		}
	}
	if template == "" {
		t.Fatalf("staged publication template is missing: %s", string(body))
	}
	var command controllerPublicationCommand
	if err := json.Unmarshal([]byte(template), &command); err != nil {
		t.Fatal(err)
	}
	return prepared.Token, command
}

func TestRemoteAheadAdvancementHandoffFailsClosedUntilSynced(t *testing.T) {
	f := awaitingAdmissionBehindPull(t)
	handoff := executeParentHandoff(t, f.cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("pulled admission handoff: %v %#v", stringPointerValue(handoff.Inconsistency), handoff)
	}
	var advertised controllerPublicationCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &advertised); err != nil {
		t.Fatal(err)
	}
	if advertised.Action != controller.PublicationOperationAdoptMutable {
		t.Fatalf("pulled admission advertised %q instead of the typed adopt-mutable", advertised.Action)
	}
	token, staged := prepareStagedPublication(t, f.cfg)
	if staged.Action != controller.PublicationOperationAdoptMutable {
		t.Fatalf("staged template advertised %q instead of the typed adopt-mutable", staged.Action)
	}

	primary := f.store.Identity().PrimaryRoot
	before, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	raced := advanceRemoteBeyondPulledPrimary(t, primary, f.policy, "unpulled-external.txt", "advancement beyond the pulled primary\n")

	exit, _, diagnostics := runParentActionCLI(t, f.cfg, controllerPublicationSurface, token)
	if exit == 0 || !strings.Contains(diagnostics, "does not match the configured remote tip") {
		t.Fatalf("staged adopt-mutable did not refuse the remote race with its cause: %s", diagnostics)
	}
	head, err := f.store.LoadHead()
	if err != nil || head.IntegrationTip != before.IntegrationTip || head.LiveLeaseID != before.LiveLeaseID || head.PendingTransitionID != "" || head.AcceptedCandidateRef != nil {
		t.Fatalf("refused staged adoption changed controller authority: %v %#v", err, head)
	}
	if remoteFields := strings.Fields(string(appTestGitOutput(t, primary, "ls-remote", f.policy.Remote, f.policy.RemoteRef))); len(remoteFields) != 2 || remoteFields[0] != raced {
		t.Fatalf("refused staged adoption moved the remote: %v", remoteFields)
	}
	if _, err := executeControllerPublication(f.cfg, f.store, controllerPublicationCommand{Action: controller.PublicationOperationAdoptMutable, ExpectedGeneration: head.ControllerGeneration - 1, Policy: f.policy}); err == nil || !strings.Contains(err.Error(), "generation is stale") {
		t.Fatalf("stale generation bypassed the publication freshness guard: %v", err)
	}

	blocked := executeParentHandoff(t, f.cfg)
	if blocked.Consistent && blocked.RequiredAction != nil && *blocked.RequiredAction == controllerPublicationSurface && blocked.RequiredActionParameters["controller_command_json"] != "" {
		t.Fatalf("remote-ahead handoff advertised an executable adopt-mutable: %#v", blocked)
	}
	if blocked.Inconsistency == nil || !strings.Contains(*blocked.Inconsistency, "does not match the configured remote tip") {
		t.Fatalf("remote-ahead handoff lost the refusal cause: %v", stringPointerValue(blocked.Inconsistency))
	}
	exit, _, diagnostics = runParentActionCLIInProcess(t, f.cfg, "prepare", controllerPublicationSurface)
	if exit == 0 || !strings.Contains(diagnostics, "does not match the configured remote tip") {
		t.Fatalf("remote-ahead preparation did not fail closed with its cause: %s", diagnostics)
	}

	runControllerActivationGit(t, primary, "pull", "--ff-only", f.policy.Remote, f.policy.RemoteRef)
	synced := strings.TrimSpace(string(appTestGitOutput(t, primary, "rev-parse", "HEAD")))
	if synced != raced {
		t.Fatalf("primary did not fast-forward to the raced tip: %s", synced)
	}
	resynced := executeParentHandoff(t, f.cfg)
	if !resynced.Consistent || resynced.RequiredAction == nil || *resynced.RequiredAction != controllerPublicationSurface {
		t.Fatalf("resynchronized handoff: %v %#v", stringPointerValue(resynced.Inconsistency), resynced)
	}
	executeStagedParentActionThroughChild(t, f.cfg, resynced.RequiredActionParameters["controller_command_json"])
	adopted, err := f.store.LoadHead()
	if err != nil || adopted.IntegrationTip != raced || adopted.LiveLeaseID != "" || adopted.AcceptedCandidateRef != nil || adopted.PendingTransitionID != "" {
		t.Fatalf("resynchronized adopt-mutable did not converge on the remote tip: %v %#v", err, adopted)
	}
	resume := executeParentHandoff(t, f.cfg)
	if !resume.Consistent || resume.RequiredAction == nil || *resume.RequiredAction != controllerExecutionSurface {
		t.Fatalf("resynchronized handoff lost the execution resume surface: %v %#v", stringPointerValue(resume.Inconsistency), resume)
	}
}
