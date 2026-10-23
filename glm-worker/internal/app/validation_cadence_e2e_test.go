//go:build workflowintegration

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactioncmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type cadenceWorkflowRunner struct {
	canonicalWorkflowRunner
	workerCalls   int
	reviewerCalls int
}

func (r *cadenceWorkflowRunner) Run(role state.SessionRole, phase, model string, readOnly bool, effort, prompt, resume string) (runner.RunResult, error) {
	result, err := r.canonicalWorkflowRunner.Run(role, phase, model, readOnly, effort, prompt, resume)
	if err != nil {
		return result, err
	}
	var value packet.Result
	if err := json.Unmarshal(result.StructuredOutput, &value); err != nil {
		return result, err
	}
	if role == state.WorkerRole {
		r.workerCalls++
		writeAppTestFile(r.t, r.cfg.RepoRoot, r.path, fmt.Sprintf("version %d\n", r.workerCalls))
		value.Risk = packet.RiskHigh
		if r.workerCalls == 1 {
			value.SetParentValidationRequest(&packet.ParentValidationRequest{Form: packet.ParentValidationGoTest, WorkingDir: "glm-worker"})
		}
	} else {
		r.reviewerCalls++
		value.Status = packet.StatusPass
		value.Risk = packet.RiskLow
		value.Targets = []string{"none"}
		value.SolQuestion = ""
	}
	body := appPacketBody(value)
	result.StructuredOutput = json.RawMessage(body)
	result.Response = body
	return result, nil
}

func cadenceExecute(t *testing.T, cfg config.AppConfig, mode CommandMode, r *cadenceWorkflowRunner) string {
	t.Helper()
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		r.cfg = actual
		return r
	}
	var output bytes.Buffer
	if err := Execute(Command{Mode: mode, Payload: "review current source", Origin: "glm-reviewer"}, cfg, factory, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func cadenceWorkflowFixture(t *testing.T, binary string) (config.AppConfig, string, controller.PublicationPolicy, string, string) {
	t.Helper()
	cfg, task, _, policy := newCanonicalWorkflowCorpus(t)
	body, err := os.ReadFile(filepath.Join(cfg.RepoRoot, task))
	if err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, cfg.RepoRoot, task, strings.Replace(string(body), "- `IMPLEMENTATION_TASKS/f11.md`", "none", 1))
	writeAppTestFile(t, cfg.RepoRoot, "cadence-result.txt", "base\n")
	runControllerActivationGit(t, cfg.RepoRoot, "add", ".")
	runControllerActivationGit(t, cfg.RepoRoot, "commit", "-qm", "independent cadence fixture")
	runControllerActivationGit(t, cfg.RepoRoot, "push", policy.Remote, policy.LocalRef+":"+policy.RemoteRef)
	tools := strings.SplitN(qualityToolsBinDirEnv(t)[0], "=", 2)[1]
	failFlag := filepath.Join(t.TempDir(), "fail-gate")
	shim, log := writeQualityGateGoShim(t, failFlag)
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(shim, "go")
	shimBody, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatal(err)
	}
	forward := "#!/bin/sh\ncase \"$1\" in test|vet|build) ;; *) exec '" + strings.ReplaceAll(realGo, "'", "'\"'\"'") + "' \"$@\";; esac\n"
	forward += "if [ \"$1\" = test ] && [ -f '" + failFlag + ".plain' ]; then\n  printf 'argv:%s\\n' \"$*\" >>'" + log + "'\n  printf '%s\\n' 'ok example.com/fixture 0.25s'\n  exit 0\nfi\n"
	if err := os.WriteFile(shimPath, []byte(forward+strings.TrimPrefix(string(shimBody), "#!/bin/sh\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	binaryDir := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(binaryDir, "glm-worker")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUALITY_TOOLS_BIN_DIR", tools)
	t.Setenv("PATH", shim+string(os.PathListSeparator)+binaryDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return cfg, task, policy, log, failFlag
}

func TestCanonicalValidationCadenceWithoutRuntimeRepair(t *testing.T) {
	binary, err := buildMultiRepoWorkerBinary(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"success", "final-failure", "observation-failure", "promoted-fix", "publication-interruption"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, task, policy, log, failFlag := cadenceWorkflowFixture(t, binary)
			r := &cadenceWorkflowRunner{canonicalWorkflowRunner: canonicalWorkflowRunner{complete: true, t: t, task: task, path: "cadence-result.txt"}}
			evidence := cadenceExecute(t, cfg, ModeNewTask, r)
			cfg = r.cfg
			st := state.AttachStateStore(cfg)
			taskID := st.ReadOr("task.id", "")
			if taskID == "" || st.TaskStatus() != state.TaskStatusWaitingSolReview {
				t.Fatalf("normal review did not open: %s", st.TaskStatus())
			}
			if st.ReadOr("task.id", "") != taskID || r.workerCalls != 1 || r.reviewerCalls != r.workerCalls {
				t.Fatalf("unexpected model cadence: worker=%d reviewer=%d output=%s", r.workerCalls, r.reviewerCalls, evidence)
			}
			if lines := qualityGateInvocationLines(t, log); cadenceFullCalls(lines) != 0 {
				t.Fatalf("full gate ran before final source: %v", lines)
			}
			review := cadenceParentAcceptance(t, cfg, r)
			candidate, generation := cadenceAcceptCandidate(t, cfg, policy, evidence, review)
			expectedCalls := 1
			switch scenario {
			case "final-failure":
				candidate, generation = cadenceFailedFinalFix(t, binary, cfg, policy, candidate, generation, r, failFlag, taskID)
				expectedCalls = 2
			case "observation-failure", "promoted-fix":
				candidate, generation = cadenceObservationFix(t, binary, cfg, policy, candidate, generation, r, failFlag, taskID, scenario)
				expectedCalls = 2
			}
			cadenceFinalPublication(t, cfg, policy, candidate, generation, log, taskID, expectedCalls, scenario == "publication-interruption")
			cadenceCleanup(t, binary, cfg, candidate)
		})
	}
}

func cadenceParentAcceptance(t *testing.T, cfg config.AppConfig, r *cadenceWorkflowRunner) string {
	t.Helper()
	st := state.AttachStateStore(cfg)
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("accept without delivered diff proof succeeded")
	}
	manifest := writeParentReviewEvidenceManifest(t, parentevidence.Manifest{Version: parentevidence.ManifestVersion, Reason: "verify final implementation diff", Diff: []parentevidence.DiffRequest{{Question: "review the actual changed source", Paths: []string{"cadence-result.txt"}, BudgetBytes: 4096}}})
	var proof bytes.Buffer
	if err := printParentEvidence(Command{EvidenceManifest: manifest}, cfg, st, &proof); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proof.String(), fmt.Sprintf("+version %d", r.workerCalls)) {
		t.Fatal("parent did not receive current diff")
	}
	var accepted bytes.Buffer
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, &accepted, io.Discard); err != nil {
		t.Fatal(err)
	}
	return proof.String() + accepted.String()
}

func cadencePublication(t *testing.T, cfg config.AppConfig, command controllerPublicationCommand) any {
	t.Helper()
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executeControllerPublication(cfg, store, command)
	if err != nil {
		t.Fatalf("normal publication %s: %v", command.Action, err)
	}
	return result
}

func cadenceAcceptCandidate(t *testing.T, cfg config.AppConfig, policy controller.PublicationPolicy, validation, review string) (controller.AcceptedCandidate, uint64) {
	t.Helper()
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	preview := cadencePublication(t, cfg, controllerPublicationCommand{Action: "preview", ExpectedGeneration: head.ControllerGeneration, Policy: policy}).(map[string]string)
	var refs []controller.EvidenceObjectRef
	for _, entry := range []struct{ kind, body string }{{"review", review}, {"validation", validation}} {
		record := controller.CandidateEvidence{SchemaVersion: 1, Kind: entry.kind, RepositoryIdentity: store.Identity().LineageID, AttemptID: preview["attempt_id"], SnapshotID: preview["snapshot_id"], BaseOID: preview["base_oid"], TreeOID: preview["tree_oid"], Result: "pass"}
		artifact, err := json.Marshal(map[string]string{"actual_workflow_output": entry.body})
		if err != nil {
			t.Fatal(err)
		}
		ref := cadencePublication(t, cfg, controllerPublicationCommand{Action: "store-evidence", ExpectedGeneration: head.ControllerGeneration, EvidenceRecord: &record, ArtifactJSON: artifact}).(controller.EvidenceObjectRef)
		refs = append(refs, ref)
	}
	accepted := cadencePublication(t, cfg, controllerPublicationCommand{Action: "accept", ExpectedGeneration: head.ControllerGeneration, Policy: policy, Message: "cadence result", Evidence: refs}).(controller.ExecutionOperationResult)
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.RequiredValidations) != 1 || candidate.RequiredValidations[0].Form != "go-test" || candidate.RequiredValidations[0].WorkingDir != "glm-worker" {
		t.Fatalf("worker obligation was not inherited: %#v", candidate.RequiredValidations)
	}
	return candidate, accepted.Head.ControllerGeneration
}

func cadenceFinalPublication(t *testing.T, cfg config.AppConfig, policy controller.PublicationPolicy, candidate controller.AcceptedCandidate, generation uint64, log, taskID string, expectedCalls int, interrupted bool) {
	t.Helper()
	output := cadenceFinalCheck(t, cfg)
	var final struct {
		Status string `json:"status"`
		Git    struct {
			Head string `json:"head"`
		} `json:"git"`
	}
	if err := json.Unmarshal([]byte(output), &final); err != nil || final.Status != "ready_for_parent_decision" || final.Git.Head != candidate.CommitOID {
		t.Fatalf("final gate: %v %s", err, output)
	}
	calls := cadenceFullCalls(qualityGateInvocationLines(t, log))
	if calls != expectedCalls {
		t.Fatalf("full test invocations = %d", calls)
	}
	promoted := cadencePublication(t, cfg, controllerPublicationCommand{Action: "promote", ExpectedGeneration: generation, CandidateID: candidate.CandidateID}).(controller.ExecutionOperationResult)
	var published controller.ExecutionOperationResult
	if interrupted {
		published = cadenceInterruptedPublication(t, cfg, policy, candidate, promoted.Head.ControllerGeneration)
	} else {
		published = cadencePublication(t, cfg, controllerPublicationCommand{Action: "publish", ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}).(controller.ExecutionOperationResult)
	}
	if published.Head.IntegrationTip != candidate.CommitOID || published.Head.LiveLeaseID != "" || published.Head.ExecutionTaskRef != nil {
		t.Fatal("publication did not retire the exact candidate")
	}
	if remote := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef); !strings.HasPrefix(string(remote), candidate.CommitOID+"\t") {
		t.Fatal("remote differs from validated source")
	}
	if _, err := os.Stat(filepath.Join(cfg.RepoRoot, candidate.TaskRef.TaskPath)); !os.IsNotExist(err) {
		t.Fatal("completed Task was not removed")
	}
	if st := state.AttachStateStore(cfg); st.ReadOr("task.id", "") != taskID || st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("original Task was not completed: %s", st.TaskStatus())
	}
	successorCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if successor := state.AttachStateStore(successorCfg); successor.ReadOr("task.id", "") != "" {
		t.Fatal("successor Task was started")
	}
}

func cadenceInterruptedPublication(t *testing.T, cfg config.AppConfig, policy controller.PublicationPolicy, candidate controller.AcceptedCandidate, generation uint64) controller.ExecutionOperationResult {
	t.Helper()
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	primaryCfg := cfg
	primaryCfg.RepoRoot = store.Identity().PrimaryRoot
	hooks := t.TempDir()
	writeAppTestFile(t, hooks, "pre-push", "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(hooks, "pre-push"), 0o755); err != nil {
		t.Fatal(err)
	}
	runControllerActivationGit(t, primaryCfg.RepoRoot, "config", "core.hooksPath", hooks)
	_, publishErr := executeControllerPublication(cfg, store, controllerPublicationCommand{Action: "publish", ExpectedGeneration: generation, CandidateID: candidate.CandidateID})
	runControllerActivationGit(t, primaryCfg.RepoRoot, "config", "--unset", "core.hooksPath")
	if publishErr == nil {
		t.Fatal("publication interruption was not observed")
	}
	authority, err := controller.ResolveCommittedTaskAuthority(primaryCfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertRetiredBootstrap(t, primaryCfg, authority.Task.TaskPath)
	if code, diagnostics := runCompletionRecoveryThroughParentCLI(t, primaryCfg); code != 0 {
		t.Fatalf("normal publication recovery failed: %d %s", code, diagnostics)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	return controller.ExecutionOperationResult{Head: head}
}

func cadenceFullCalls(lines []string) int {
	calls := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "argv:test ") {
			calls++
		}
	}
	return calls
}

func cadenceFinalCheck(t *testing.T, cfg config.AppConfig) string {
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
	if code := parentactioncmd.Run([]string{"finalize-check", "go-test"}, &output, &diagnostics); code != 0 {
		t.Fatalf("finalize-check: %d %s", code, diagnostics.String())
	}

	return output.String()
}

func cadenceFailedFinalFix(t *testing.T, binary string, cfg config.AppConfig, policy controller.PublicationPolicy, candidate controller.AcceptedCandidate, generation uint64, r *cadenceWorkflowRunner, failFlag, taskID string) (controller.AcceptedCandidate, uint64) {
	t.Helper()
	if err := os.WriteFile(failFlag, []byte("fail"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := cadenceFinalCheck(t, cfg)
	var failed struct {
		Status  string `json:"status"`
		Failure *struct {
			Stage string `json:"stage"`
		} `json:"failure"`
	}
	if err := json.Unmarshal([]byte(output), &failed); err != nil || failed.Status != "blocked" || failed.Failure == nil || failed.Failure.Stage != "validation" {
		t.Fatalf("forced final gate did not fail: %v %s", err, output)
	}
	if err := os.Remove(failFlag); err != nil {
		t.Fatal(err)
	}
	return cadenceFixCandidate(t, binary, cfg, policy, candidate, generation, r, taskID)
}

func cadenceFixCandidate(t *testing.T, binary string, cfg config.AppConfig, policy controller.PublicationPolicy, candidate controller.AcceptedCandidate, generation uint64, r *cadenceWorkflowRunner, taskID string) (controller.AcceptedCandidate, uint64) {
	t.Helper()
	reentered := cadencePublication(t, cfg, controllerPublicationCommand{Action: "reenter", ExpectedGeneration: generation, CandidateID: candidate.CandidateID}).(controller.ExecutionOperationResult)
	materialized := canonicalCLIExecution(t, binary, cfg, controllerExecutionCommand{Action: "materialize", ExpectedGeneration: reentered.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID})
	if materialized.Admission == nil {
		t.Fatal("reentry did not materialize a live execution")
	}
	laneCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	validation := cadenceExecute(t, laneCfg, ModeFix, r)
	laneCfg = r.cfg
	if st := state.AttachStateStore(laneCfg); st.ReadOr("task.id", "") != taskID || r.workerCalls != 2 || r.reviewerCalls != 2 {
		t.Fatal("final failure Fix lost same Task/model cadence")
	}
	review := cadenceParentAcceptance(t, laneCfg, r)
	next, nextGeneration := cadenceAcceptCandidate(t, laneCfg, policy, validation, review)
	if next.CommitOID == candidate.CommitOID || next.TreeOID == candidate.TreeOID {
		t.Fatal("Fix did not create a new final source")
	}
	return next, nextGeneration
}

func cadenceObservationFix(t *testing.T, binary string, cfg config.AppConfig, policy controller.PublicationPolicy, candidate controller.AcceptedCandidate, generation uint64, r *cadenceWorkflowRunner, flag, taskID, scenario string) (controller.AcceptedCandidate, uint64) {
	t.Helper()
	plain := flag + ".plain"
	if scenario == "observation-failure" {
		if err := os.WriteFile(plain, []byte("plain"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if output := cadenceFinalCheck(t, cfg); !strings.Contains(output, "ready_for_parent_decision") {
		t.Fatal(output)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := executeControllerPublication(cfg, store, controllerPublicationCommand{Action: "promote", ExpectedGeneration: generation, CandidateID: candidate.CandidateID})
	if scenario == "observation-failure" {
		if err == nil || !strings.Contains(err.Error(), "not captured from machine-readable") {
			t.Fatalf("uncaptured observation passed promotion: %v", err)
		}
		head, loadErr := store.LoadHead()
		if loadErr != nil || head.ControllerGeneration != generation || head.PendingTransitionID != "" {
			t.Fatal("observation failure crossed the promotion boundary")
		}
		if err := os.Remove(plain); err != nil {
			t.Fatal(err)
		}
	} else {
		if err != nil {
			t.Fatal(err)
		}
		generation = promoted.(controller.ExecutionOperationResult).Head.ControllerGeneration
	}
	primaryCfg := cfg
	primaryCfg.RepoRoot = store.Identity().PrimaryRoot
	handoff := executeParentHandoff(t, primaryCfg)
	if !handoff.Consistent || !containsParentAction(handoff.AllowedActions, controllerPublicationSurface) || !containsParentAction(handoff.AllowedActions, controllerExecutionSurface) {
		t.Fatalf("failure/fix lifecycle closed: %#v", handoff)
	}
	return cadenceFixCandidate(t, binary, cfg, policy, candidate, generation, r, taskID)
}

func cadenceCleanup(t *testing.T, binary string, cfg config.AppConfig, candidate controller.AcceptedCandidate) {
	t.Helper()
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := store.LoadAttemptSeal(candidate.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	primary, err := controller.ResolveWorkspaceIdentity(store.Identity().PrimaryRoot, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	if seal.WorkspaceID == primary.ID {
		if _, err := store.ProveCleanupDurability(candidate.SealRef); err != nil {
			t.Fatal(err)
		}
		return
	}
	suspension, err := store.LoadSuspension(store.Identity().PrimaryRoot, seal.OperationalSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	cfg.RepoRoot = store.Identity().PrimaryRoot
	cleaned := canonicalCLIExecution(t, binary, cfg, controllerExecutionCommand{Action: "cleanup", ExpectedGeneration: head.ControllerGeneration, WorkspaceID: suspension.WorkspaceID, SealRef: &candidate.SealRef})
	if cleaned.Head.LiveLeaseID != "" || cleaned.Head.PendingTransitionID != "" {
		t.Fatal("Cleanup retained live lifecycle authority")
	}
}

func TestCanonicalCadenceCompletedRuntimeAdmitsExternalPullSyncAndSuccessor(t *testing.T) {
	binary, err := buildMultiRepoWorkerBinary(t)
	if err != nil {
		t.Fatal(err)
	}
	cfg, rootTask, policy, log, _ := cadenceWorkflowFixture(t, binary)
	r := &cadenceWorkflowRunner{canonicalWorkflowRunner: canonicalWorkflowRunner{complete: true, t: t, task: rootTask, path: "cadence-result.txt"}}
	evidence := cadenceExecute(t, cfg, ModeNewTask, r)
	cfg = r.cfg
	taskID := state.AttachStateStore(cfg).ReadOr("task.id", "")
	if taskID == "" {
		t.Fatal("cadence runtime did not record a task")
	}
	review := cadenceParentAcceptance(t, cfg, r)
	candidate, generation := cadenceAcceptCandidate(t, cfg, policy, evidence, review)
	cadenceFinalPublication(t, cfg, policy, candidate, generation, log, taskID, 1, false)

	sourceTip := pullExternalSourceChange(t, cfg, policy, map[string]string{"external-code.txt": "external code update\n"}, nil)
	handoff := executeParentHandoff(t, cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("completed cadence runtime external pull handoff: %#v", handoff)
	}
	var sourceCommand controllerPublicationCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &sourceCommand); err != nil {
		t.Fatal(err)
	}
	if sourceCommand.Action != "adopt-source" || sourceCommand.SourceOID != sourceTip {
		t.Fatalf("external source command: %#v", sourceCommand)
	}
	executeStagedParentAction(t, cfg, handoff.RequiredActionParameters["controller_command_json"])

	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil || head.RootTaskRef == nil {
		t.Fatalf("completed controller lacks a selected successor: %v", err)
	}
	pullExternalSourceChange(t, cfg, policy, nil, &[2]string{head.RootTaskRef.TaskPath, "IMPLEMENTATION_TASKS/alternate-successor.md"})
	switchHandoff := executeParentHandoff(t, cfg)
	if !switchHandoff.Consistent || switchHandoff.RequiredAction == nil || *switchHandoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("completed cadence runtime successor switch handoff: %v %#v", switchHandoff.Inconsistency, switchHandoff)
	}
	var switchCommand controllerPublicationCommand
	if err := json.Unmarshal([]byte(switchHandoff.RequiredActionParameters["controller_command_json"]), &switchCommand); err != nil {
		t.Fatal(err)
	}
	if switchCommand.Action != "adopt" {
		t.Fatalf("metadata-only switch command: %#v", switchCommand)
	}
	executeStagedParentAction(t, cfg, switchHandoff.RequiredActionParameters["controller_command_json"])

	final := executeParentHandoff(t, cfg)
	if !final.Consistent || final.RequiredAction == nil || *final.RequiredAction != "start" {
		t.Fatalf("synchronized successor handoff: %#v", final)
	}
	successor := &cadenceWorkflowRunner{canonicalWorkflowRunner: canonicalWorkflowRunner{complete: true, t: t, task: "IMPLEMENTATION_TASKS/alternate-successor.md", path: "successor-result.txt"}}
	cadenceExecute(t, cfg, ModeNewTask, successor)
	successorState := state.AttachStateStore(successor.cfg)
	if successor.workerCalls != 1 || successor.reviewerCalls != 1 || successorState.TaskStatus() != state.TaskStatusWaitingSolReview || successorState.ReadOr("task.id", "") == taskID {
		t.Fatal("normal successor start did not create and review a fresh task")
	}
}
