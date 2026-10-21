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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func assertRetiredExternalPullSynchronization(t *testing.T, cfg config.AppConfig, store *controller.Store, policy controller.PublicationPolicy) {
	t.Helper()
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	tip := pullExternalSourceChange(t, cfg, policy, map[string]string{"external-result.txt": "external source update\n", ".github/workflows/external.yml": "name: external\non: push\n"}, nil)
	assertRetiredBootstrap(t, cfg, before.RootTaskRef.TaskPath)
	afterRead, err := store.LoadHead()
	if err != nil || !reflect.DeepEqual(before, afterRead) {
		t.Fatal("bootstrap changed controller after external pull")
	}
	bound, runtime := retiredWorkflowRuntime(t, cfg)
	handoff := buildParentHandoffWithConfig(bound, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface || containsParentAction(handoff.AllowedActions, "start") {
		inconsistency := "<consistent>"
		if handoff.Inconsistency != nil {
			inconsistency = *handoff.Inconsistency
		}
		t.Fatalf("external source handoff: %s: %#v", inconsistency, handoff)
	}
	payload := handoff.RequiredActionParameters["controller_command_json"]
	var command controllerPublicationCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != "adopt-source" || command.SourceOID != tip || command.SourceOID == before.IntegrationTip {
		t.Fatalf("external source command: %#v", command)
	}
	executeStagedParentAction(t, cfg, payload)
	synchronized, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if synchronized.IntegrationTip != command.SourceOID || !reflect.DeepEqual(before.MetadataLineageRef, synchronized.MetadataLineageRef) || synchronized.AcceptedCandidateRef != nil {
		t.Fatal("external source sync changed completion proof or lost target")
	}
	handoff = buildParentHandoffWithConfig(bound, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "start" {
		t.Fatalf("source sync did not admit normal successor: %#v", handoff)
	}
}

func pullExternalSourceChange(t *testing.T, cfg config.AppConfig, policy controller.PublicationPolicy, files map[string]string, activeSwitch *[2]string) string {
	t.Helper()
	external := filepath.Join(t.TempDir(), "external")
	runControllerActivationGit(t, cfg.RepoRoot, "clone", "--quiet", cfg.RepoRoot, external)
	runControllerActivationGit(t, external, "config", "user.email", "test@example.com")
	runControllerActivationGit(t, external, "config", "user.name", "external")
	for path, data := range files {
		writeAppTestFile(t, external, path, data)
	}
	if activeSwitch != nil {
		planPath := filepath.Join(external, "IMPLEMENTATION_PLAN.local.md")
		plan, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		switched := strings.Replace(string(plan), "- `"+activeSwitch[0]+"`", "- `"+activeSwitch[1]+"`", 1)
		switched = strings.Replace(switched, "## NEXT", "## NEXT\n\n- `"+activeSwitch[0]+"`", 1)
		if err := os.WriteFile(planPath, []byte(switched), 0o644); err != nil {
			t.Fatal(err)
		}
		writeAppTestFile(t, external, activeSwitch[1], "# alternate successor\n\n## Contract\n\nalternate successor task\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\nnone\n")
	}
	runControllerActivationGit(t, external, "add", "--all")
	runControllerActivationGit(t, external, "commit", "-qm", "external advancement")
	remote, err := exec.Command("git", "-C", cfg.RepoRoot, "remote", "get-url", policy.Remote).Output()
	if err != nil {
		t.Fatal(err)
	}
	runControllerActivationGit(t, external, "push", strings.TrimSpace(string(remote)), "HEAD:"+policy.RemoteRef)
	runControllerActivationGit(t, cfg.RepoRoot, "pull", "--ff-only", policy.Remote, policy.RemoteRef)
	return strings.TrimSpace(string(mustCommandOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")))
}

func mustCommandOutput(t *testing.T, repo string, args ...string) []byte {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func executeStagedParentAction(t *testing.T, cfg config.AppConfig, payload string) {
	t.Helper()
	exit, output, diagnostics := runParentActionCLI(t, cfg, "prepare", controllerPublicationSurface)
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
	if err := os.WriteFile(prepared.Path, []byte(strings.Replace(string(body), "__GLM_PARENT_ACTION_PAYLOAD__", payload, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	exit, _, diagnostics = runParentActionCLI(t, cfg, controllerPublicationSurface, prepared.Token)
	if exit != 0 {
		t.Fatalf("execute staged parent action: %s", diagnostics)
	}
}

func executeParentHandoff(t *testing.T, cfg config.AppConfig) parentHandoffOutput {
	t.Helper()
	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var handoff parentHandoffOutput
	if err := json.Unmarshal(out.Bytes(), &handoff); err != nil {
		t.Fatal(err)
	}
	return handoff
}

func completedRetiredWorkflowFixture(t *testing.T) (config.AppConfig, controller.Admission, *controller.Store, controller.PublicationPolicy, controller.AcceptedCandidate) {
	t.Helper()
	cfg, admission, store, policy := retiredWorkflowCorpus(t, true)
	evidence := canonicalWorkflowEvidence(t, cfg, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{Message: "completed runtime result", Policy: policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	runtime := completeAwaitedRuntimeThroughProductionAPIs(t, cfg, admission)
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("production API fixture left the runtime at %s", runtime.TaskStatus())
	}
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
		t.Fatal(err)
	}
	if plan, planErr := runtime.ParentActionPlan(); planErr != nil || plan.RequiredAction != state.ParentActionNone {
		t.Fatalf("publication did not acknowledge the completed runtime: %v err=%v", plan.RequiredAction, planErr)
	}
	return cfg, admission, store, policy, candidate
}

func completeAwaitedRuntimeThroughProductionAPIs(t *testing.T, cfg config.AppConfig, admission controller.Admission) *state.StateStore {
	t.Helper()
	runtime := attachWorkflowRuntime(t, cfg)
	if _, err := runtime.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveCurrentTaskAuthority(admission.Attempt.SemanticTaskRef.TaskPath, []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	acceptedReview, err := runtime.AcceptParentReview()
	if err != nil || !acceptedReview {
		t.Fatalf("runtime accept = %v err=%v", acceptedReview, err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: admission.Attempt.AttemptID, TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	return runtime
}

func attachWorkflowRuntime(t *testing.T, cfg config.AppConfig) *state.StateStore {
	t.Helper()
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := state.NewStateStore(bound)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestCompletedRuntimeRegularHandoffAdmitsExternalPullSyncAndSuccessorSwitch(t *testing.T) {
	cfg, _, store, policy, _ := completedRetiredWorkflowFixture(t)
	retired, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := store.LoadEvidenceObject(*retired.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	sourceTip := pullExternalSourceChange(t, cfg, policy, map[string]string{"external-code.txt": "external code update\n"}, nil)
	assertRetiredBootstrap(t, cfg, retired.RootTaskRef.TaskPath)
	handoff := executeParentHandoff(t, cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface || containsParentAction(handoff.AllowedActions, "start") {
		t.Fatalf("completed runtime external pull handoff: %#v", handoff)
	}
	payload := handoff.RequiredActionParameters["controller_command_json"]
	var sourceCommand controllerPublicationCommand
	if err := json.Unmarshal([]byte(payload), &sourceCommand); err != nil {
		t.Fatal(err)
	}
	if sourceCommand.Action != "adopt-source" || sourceCommand.SourceOID != sourceTip {
		t.Fatalf("external source command: %#v", sourceCommand)
	}
	executeStagedParentAction(t, cfg, payload)
	synchronized, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if synchronized.IntegrationTip != sourceTip || !reflect.DeepEqual(retired.MetadataLineageRef, synchronized.MetadataLineageRef) {
		t.Fatal("source sync lost completion lineage")
	}
	switchTip := pullExternalSourceChange(t, cfg, policy, nil, &[2]string{retired.RootTaskRef.TaskPath, "IMPLEMENTATION_TASKS/alternate-successor.md"})
	assertRetiredBootstrap(t, cfg, "IMPLEMENTATION_TASKS/alternate-successor.md")
	switchHandoff := executeParentHandoff(t, cfg)
	if !switchHandoff.Consistent || switchHandoff.RequiredAction == nil || *switchHandoff.RequiredAction != controllerPublicationSurface || containsParentAction(switchHandoff.AllowedActions, "start") {
		t.Fatalf("completed runtime successor switch handoff: %#v", switchHandoff)
	}
	switchPayload := switchHandoff.RequiredActionParameters["controller_command_json"]
	var switchCommand controllerPublicationCommand
	if err := json.Unmarshal([]byte(switchPayload), &switchCommand); err != nil {
		t.Fatal(err)
	}
	if switchCommand.Action != "adopt" {
		t.Fatalf("metadata-only switch command: %#v", switchCommand)
	}
	executeStagedParentAction(t, cfg, switchPayload)
	switched, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if switched.IntegrationTip != switchTip || switched.RootTaskRef == nil || switched.RootTaskRef.TaskPath != "IMPLEMENTATION_TASKS/alternate-successor.md" {
		t.Fatal("metadata switch lost the selected successor")
	}
	afterTerminal, err := store.LoadEvidenceObject(*switched.MetadataLineageRef)
	if err != nil || !reflect.DeepEqual(terminal, afterTerminal) || !reflect.DeepEqual(retired.MetadataLineageRef, switched.MetadataLineageRef) {
		t.Fatal("successor switch rewrote terminal evidence")
	}
	start := executeParentHandoff(t, cfg)
	if !start.Consistent || start.RequiredAction == nil || *start.RequiredAction != "start" {
		t.Fatalf("switched successor was not startable: %#v", start)
	}
}

func TestStaleRuntimeBindingRegularHandoffFailsClosed(t *testing.T) {
	cfg, admission, _, _, _ := completedRetiredWorkflowFixture(t)
	runtime := completeAwaitedRuntimeThroughProductionAPIs(t, cfg, admission)
	stale := "IMPLEMENTATION_TASKS/unrelated-stale.md"
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: admission.Attempt.AttemptID, TaskPath: stale, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	handoff := executeParentHandoff(t, cfg)
	if handoff.Consistent || handoff.RequiredAction != nil || containsParentAction(handoff.AllowedActions, controllerPublicationSurface) || containsParentAction(handoff.AllowedActions, "start") {
		t.Fatalf("stale runtime binding handoff admitted an action: %#v", handoff)
	}
}
