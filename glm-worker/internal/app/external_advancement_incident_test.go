package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type publishedIncident struct {
	cfg                    config.AppConfig
	store                  *controller.Store
	policy                 controller.PublicationPolicy
	retired                controller.RepositoryControllerHead
	terminal               []byte
	terminatedTaskContents []byte
	terminatedRuntimeID    string
	successorState         *state.StateStore
}

const (
	incidentTerminatedTask = "IMPLEMENTATION_TASKS/incident-terminated.md"
	incidentSuccessorTask  = "IMPLEMENTATION_TASKS/incident-successor.md"
	incidentRetiredTask    = "IMPLEMENTATION_TASKS/incident-final.md"
	incidentPostExternal   = incidentTerminatedTask
)

func incidentCorpusFixture(t *testing.T) (config.AppConfig, controller.Admission, *controller.Store, controller.PublicationPolicy, *state.StateStore) {
	t.Helper()
	cfg := newCanonicalAppConfig(t)
	cfg.StateBase = filepath.Join(t.TempDir(), "sessions")
	cfg.ClaudeConfigDir = filepath.Join(filepath.Dir(cfg.StateBase), "claude")
	repo := cfg.RepoRoot
	writeAppTestFile(t, repo, "IMPLEMENTATION_RULES.md", "# rules\n")
	writeAppTestFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `"+incidentTerminatedTask+"`\n\n## NEXT\n\n- `"+incidentSuccessorTask+"`\n\n- `"+incidentRetiredTask+"`\n")
	writeAppTestFile(t, repo, incidentTerminatedTask, "# terminated\n\n## Contract\n\nterminated work\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\nnone\n")
	writeAppTestFile(t, repo, incidentSuccessorTask, "# successor\n\n## Contract\n\nsuccessor work\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\nnone\n")
	writeAppTestFile(t, repo, incidentRetiredTask, "# final\n\n## Contract\n\nfinal work\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\nnone\n")
	if err := os.Remove(filepath.Join(repo, "IMPLEMENTATION_TASKS/root.md")); err != nil {
		t.Fatal(err)
	}
	prepareCompletionQualityCorpus(t, repo)
	stubQualityPreflight(t, func(string) error { return nil })
	policy := commitCorpusWithPublicationRemote(t, repo, "incident recurrence test", "incident@example.invalid", "incident corpus")
	admission, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store, err := controller.Open(cfg)
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
	return cfg, admission, store, policy, runtime
}

func terminateIncidentTask(t *testing.T, cfg config.AppConfig, admission controller.Admission, store *controller.Store, policy controller.PublicationPolicy, runtime *state.StateStore) {
	t.Helper()
	evidence := canonicalWorkflowEvidence(t, cfg, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "terminated corpus result", Policy: policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	taskContent, err := os.ReadFile(filepath.Join(admission.Workspace.Root, admission.Attempt.SemanticTaskRef.TaskPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveCurrentTaskAuthority(admission.Attempt.SemanticTaskRef.TaskPath, taskContent); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if ok, err := runtime.AcceptParentReview(); err != nil || !ok {
		t.Fatalf("termination fixture accept: %v %v", ok, err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: admission.Attempt.AttemptID, TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
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
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: source.Attempt.AttemptID, TaskPath: source.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("repo-root", source.Workspace.Root); err != nil {
		t.Fatal(err)
	}
	accepted, err = store.AcceptExecutionCandidate(source, controller.CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "saved implementation", Policy: policy, Evidence: canonicalWorkflowEvidence(t, cfg, store, source, policy)})
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
		t.Fatalf("termination fixture reaccept: %v %v", ok, err)
	}
	reentered, err = store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "cleanup", ExpectedGeneration: reentered.Head.ControllerGeneration, WorkspaceID: source.Workspace.ID, SealRef: &candidate.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cfg.RepoRoot, incidentTerminatedTask)); err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `"+incidentSuccessorTask+"`\n\n## NEXT\n\n- `"+incidentRetiredTask+"`\n")
	writeAppTestFile(t, cfg.RepoRoot, "preserved.txt", "saved implementation\n")
	runControllerActivationGit(t, cfg.RepoRoot, "add", "--all")
	runControllerActivationGit(t, cfg.RepoRoot, "commit", "-qm", "user stops old task and preserves implementation")
	runControllerActivationGit(t, cfg.RepoRoot, "push", policy.Remote, "HEAD:"+policy.RemoteRef)
	authority, err := controller.ResolveCommittedTaskAuthority(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	input := controller.UserTaskTransitionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration, SourceProjectID: cleaned.Head.ProjectSnapshotID, SourceTask: *cleaned.Head.ExecutionTaskRef, SuccessorTask: authority.Task, SuccessorProjectID: authority.ProjectSnapshotID, CleanupTransitionID: cleaned.TransitionID, Reason: "user-directed termination ahead of external successor switch"}
	result, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "user-terminate-task", ExpectedGeneration: input.ExpectedGeneration, UserTermination: &input})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecoverExecutionOperation(result.TransitionID); err != nil {
		t.Fatal(err)
	}
	if head, err = store.LoadHead(); err != nil || head.UserTerminationRef == nil || head.RootTaskRef == nil || head.RootTaskRef.TaskPath != incidentSuccessorTask {
		t.Fatalf("termination left the wrong authority: %#v err=%v", head, err)
	}
	if runtime.TaskStatus() != state.TaskStatusUserTerminated {
		t.Fatalf("terminated runtime status: %s", runtime.TaskStatus())
	}
}

func dispatchCompletedIncidentWorkflow(t *testing.T, cfg config.AppConfig, request, task, resultPath string) *canonicalWorkflowRunner {
	t.Helper()
	scripted := &canonicalWorkflowRunner{complete: true, t: t, task: task, path: resultPath}
	scripted.cfg = cfg
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		scripted.cfg = actual
		return scripted
	}
	if err := Execute(Command{Mode: ModeNewTask, Payload: request}, cfg, factory, io.Discard, io.Discard); err != nil {
		t.Fatalf("completed dispatch failed: %v", err)
	}
	return scripted
}

func TestUserTerminatedHistoryExternalAdvancementRecurrenceThroughCLI(t *testing.T) {
	binary, err := buildMultiRepoWorkerBinary(t)
	if err != nil {
		t.Fatal(err)
	}
	fixture := publishedIncidentFixture(t, binary)
	cfg, store, policy := fixture.cfg, fixture.store, fixture.policy
	retired, terminal := fixture.retired, fixture.terminal
	terminatedTaskContents, terminatedRuntimeID := fixture.terminatedTaskContents, fixture.terminatedRuntimeID
	successorState := fixture.successorState
	if retired.UserTerminationRef == nil || retired.MetadataLineageRef == nil || retired.RootTaskRef == nil || retired.RootTaskRef.TaskPath != incidentRetiredTask {
		t.Fatalf("publication lost the terminated-history retired authority: %#v", retired)
	}
	tip := pullExternalSourceChange(t, cfg, policy, map[string]string{"external-code.txt": "external code update\n", incidentPostExternal: string(terminatedTaskContents)}, &[2]string{incidentRetiredTask, incidentPostExternal})
	assertRetiredBootstrap(t, cfg, incidentPostExternal)
	handoff := executeParentHandoff(t, cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface || containsParentAction(handoff.AllowedActions, "start") {
		inconsistency := "<consistent>"
		if handoff.Inconsistency != nil {
			inconsistency = *handoff.Inconsistency
		}
		t.Fatalf("external advancement handoff after terminated history: %s: %#v", inconsistency, handoff)
	}
	if handoff.RequiredActionParameters["previous_task_path"] != incidentRetiredTask || handoff.RequiredActionParameters["active_task_path"] != incidentPostExternal || handoff.RequiredActionParameters["sync_reason"] == "" {
		t.Fatalf("sync guidance lost old/new authority and reason: %#v", handoff.RequiredActionParameters)
	}

	payload := handoff.RequiredActionParameters["controller_command_json"]
	var command controllerPublicationCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != "adopt-source" || command.SourceOID != tip || command.SourceOID == retired.IntegrationTip {
		t.Fatalf("external advancement command: %#v", command)
	}
	executeStagedParentAction(t, cfg, payload)
	synchronized, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if synchronized.IntegrationTip != tip || !reflect.DeepEqual(retired.UserTerminationRef, synchronized.UserTerminationRef) || !reflect.DeepEqual(retired.MetadataLineageRef, synchronized.MetadataLineageRef) {
		t.Fatal("external advancement sync lost the terminated history or completion lineage")
	}
	afterTerminal, err := store.LoadEvidenceObject(*synchronized.MetadataLineageRef)
	if err != nil || !reflect.DeepEqual(terminal, afterTerminal) {
		t.Fatal("external advancement sync rewrote terminal evidence")
	}
	start := executeParentHandoff(t, cfg)
	if !start.Consistent || start.RequiredAction == nil || *start.RequiredAction != "start" {
		t.Fatalf("synchronized external successor was not startable: %#v", start)
	}

	selectedCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	selectedState := state.AttachStateStore(selectedCfg)
	if selectedState.TaskStatus() != state.TaskStatusUserTerminated || selectedState.ReadOr("task.id", "") != terminatedRuntimeID {
		t.Fatalf("resurrected successor did not retain its actual terminated runtime: %s", selectedState.TaskStatus())
	}
	startIncidentSuccessorThroughCLI(t, cfg, binary, start)
	resumedCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	postExternalState := state.AttachStateStore(resumedCfg)
	if postExternalState.TaskStatus() != state.TaskStatusWaitingDecision || postExternalState.ReadOr("task.id", "") == successorState.ReadOr("task.id", "") {
		t.Fatalf("production CLI did not dispatch a fresh Task: %s", postExternalState.TaskStatus())
	}
	binding, err := postExternalState.LoadControllerRuntimeBinding()
	if err != nil || binding.TaskPath != incidentPostExternal {
		t.Fatalf("new Task binding: %#v %v", binding, err)
	}
	if data, err := os.ReadFile(filepath.Join(resumedCfg.RepoRoot, "preserved.txt")); err != nil || string(data) != "saved implementation\n" {
		t.Fatalf("new CLI task lost preserved source: %v %q", err, data)
	}

}

func startIncidentSuccessorThroughCLI(t *testing.T, cfg config.AppConfig, binary string, handoff parentHandoffOutput) {
	t.Helper()
	raw, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		ActionSpecs map[string]struct {
			Command []string `json:"command"`
		} `json:"action_specs"`
	}
	if err := json.Unmarshal(raw, &projected); err != nil {
		t.Fatal(err)
	}
	args := projected.ActionSpecs["start"].Command
	if len(args) < 2 || args[0] != "glm-parent-action" {
		t.Fatalf("missing canonical start command: %s", raw)
	}
	home := filepath.Dir(cfg.StateBase)
	prompts := filepath.Join(home, "prompts")
	writeAppTestFile(t, prompts, "WORKER.md", "worker prompt\n")
	writeAppTestFile(t, filepath.Join(home, "claude"), "settings.json", `{"env":{"ANTHROPIC_BASE_URL":"https://api.z.ai/api/anthropic"}}`)
	provider := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nif [ \"$1\" = --help ]; then echo --json-schema; exit 0; fi\nprintf '%s\\n' '" + terminalPayloadFakeResultJSON() + "'\n"
	if err := os.WriteFile(provider, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(shim, "glm-worker")); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, append([]string{"-test.run", "^TestParentActionCLIProcess$", "--"}, args[1:]...)...)
	command.Dir = cfg.RepoRoot
	command.Env = append(parentActionChildEnv(t, cfg, shim), "GLM_WORKER_PROMPT_DIR="+prompts, "GLM_WORKER_CLAUDE_BIN="+provider, "GLM_WORKER_TELEMETRY_CONTENT=false")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("canonical start failed: %v %s %s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "NEEDS_SOL_DECISION") {
		t.Fatalf("provider was not dispatched: %s", stdout.String())
	}
}

func publishedIncidentFixture(t *testing.T, binary string) publishedIncident {
	t.Helper()
	cfg, admission, store, policy, runtime := incidentCorpusFixture(t)
	terminatedTaskContents, err := os.ReadFile(filepath.Join(cfg.RepoRoot, incidentTerminatedTask))
	if err != nil {
		t.Fatal(err)
	}
	terminateIncidentTask(t, cfg, admission, store, policy, runtime)
	terminatedRuntimeID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}

	assertRetiredBootstrap(t, cfg, incidentSuccessorTask)
	terminatedHead, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	lane, err := store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: terminatedHead.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	successorCfg := cfg
	successorCfg.RepoRoot = lane.Admission.Workspace.Root
	successor := dispatchCompletedIncidentWorkflow(t, successorCfg, "successor request", incidentSuccessorTask, "successor-result.txt")
	successorState := state.AttachStateStore(successor.cfg)
	if successorState.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("successor dispatch left the runtime at %s", successorState.TaskStatus())
	}
	acceptCanonicalWorkflowReview(t, successor, successor.cfg, successorState, "completed fixture dispatch")
	source := canonicalWorkflowAdmission(t, successor.cfg, store)
	accepted := canonicalCLIPublication(t, binary, successor.cfg, controllerPublicationCommand{Action: "accept", ExpectedGeneration: source.Head.ControllerGeneration, Message: "successor result", Policy: policy, RequiredValidations: declaredNoValidations(), Evidence: canonicalWorkflowEvidence(t, successor.cfg, store, source, policy)})
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted := canonicalCLIPublication(t, binary, successor.cfg, controllerPublicationCommand{Action: "promote", ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	published := canonicalCLIPublication(t, binary, successor.cfg, controllerPublicationCommand{Action: "publish", ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	retired := published.Head
	terminal, err := store.LoadEvidenceObject(*retired.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}

	return publishedIncident{cfg: cfg, store: store, policy: policy, retired: retired, terminal: terminal, terminatedTaskContents: terminatedTaskContents, terminatedRuntimeID: terminatedRuntimeID, successorState: successorState}
}
