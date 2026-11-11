package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type parentOnlyWorkflowTasks struct {
	parent    string
	successor string
}

type parentOnlyCompletionScenario struct {
	name         string
	sourceChange bool
	interrupt    bool
	fullProcess  bool
}

func parentOnlyCompletionScenarios() []parentOnlyCompletionScenario {
	return []parentOnlyCompletionScenario{
		{name: "metadata-only", fullProcess: true},
		{name: "declared-validation", sourceChange: true},
		{name: "publication-recovery", sourceChange: true, interrupt: true},
	}
}

func TestParentOnlyTaskCompletionThroughProductionBoundary(t *testing.T) {
	t.Parallel()
	for _, scenario := range parentOnlyCompletionScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			runIsolatedScenario(t, isolatedScenarioKeyParentOnlyCompletion, scenario.name)
		})
	}
}

func parentOnlyCompletionScenarioBody(t *testing.T, scenario parentOnlyCompletionScenario) {
	t.Helper()
	started := time.Now()
	cfg, store, policy, tasks, goShim := parentOnlyCompletionFixture(t, scenario.sourceChange)
	logAppFixturePhase(t, "parent-only/fixture", started)
	started = time.Now()
	handoff := executeParentHandoff(t, cfg)
	if handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
		t.Fatalf("parent-only materialization handoff: %#v", handoff)
	}
	runParentOnlyExecutionStep(t, cfg, handoff.RequiredActionParameters, scenario.fullProcess)
	head, err := store.LoadHead()
	if err != nil || head.ExecutionTaskRef == nil || head.ExecutionTaskRef.TaskPath != tasks.parent {
		t.Fatalf("parent-only export target: %v", err)
	}
	assertParentOnlyEvidenceExport(t, cfg, store, head.LiveAttemptID, true)
	driveParentOnlyCompletion(t, cfg, goShim, scenario.sourceChange, scenario.interrupt, scenario.fullProcess)
	logAppFixturePhase(t, "parent-only/production-cli", started)
	started = time.Now()
	assertParentOnlyTerminalState(t, cfg, store, policy, tasks, scenario.sourceChange)
	assertParentOnlyEvidenceExport(t, cfg, store, head.LiveAttemptID, false)
	logAppFixturePhase(t, "parent-only/terminal-assertions", started)
}

func TestParentOnlyReentryThroughProductionBoundary(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"reenter", "parent-source-rebind"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			runIsolatedScenario(t, isolatedScenarioKeyParentOnlyReentry, route)
		})
	}
}

func parentOnlyReentryRouteBody(t *testing.T, route string) {
	t.Helper()
	decomposed := route != "reenter"
	failFlag := filepath.Join(t.TempDir(), "fail-final-gate")
	if err := os.WriteFile(failFlag, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	cfg, store, policy, tasks, goShim := parentOnlyCompletionFixture(t, true, failFlag)
	logAppFixturePhase(t, "parent-only-reentry/fixture", started)
	started = time.Now()
	acceptParentOnlyThroughProductionBoundary(t, cfg, decomposed)
	logAppFixturePhase(t, "parent-only-reentry/accept-and-failed-gate", started)

	candidate := parentOnlyReentryCandidate(t, store)
	handoff := executeParentHandoff(t, cfg)
	assertParentOnlyFailedGateHandoff(t, handoff)
	reenterParentOnlyFailedGate(t, cfg, handoff, decomposed, failFlag)
	if route == "parent-source-rebind" {
		rebindParentSourceThroughNativeOperations(t, cfg, store, candidate)
	}
	assertParentOnlyReentryMaterialization(t, cfg, candidate, decomposed)
	started = time.Now()
	driveParentOnlyCompletion(t, cfg, goShim, true, false, !decomposed)
	logAppFixturePhase(t, "parent-only-reentry/production-cli", started)
	started = time.Now()
	assertParentOnlyTerminalState(t, cfg, store, policy, tasks, true)
	logAppFixturePhase(t, "parent-only-reentry/terminal-assertions", started)
}

func parentOnlyReentryCandidate(t *testing.T, store *controller.Store) controller.AcceptedCandidate {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil || head.AcceptedCandidateRef == nil {
		t.Fatalf("parent-only candidate missing: %v", err)
	}
	candidate, err := store.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func assertParentOnlyFailedGateHandoff(t *testing.T, handoff parentHandoffOutput) {
	t.Helper()
	commandJSON := handoff.RequiredActionParameters["controller_command_json"]
	if commandJSON == "" {
		t.Fatalf("parent-only failed-gate handoff omitted a controller command: %#v", handoff)
	}
	var failure controller.PublicationParentCommand
	if err := json.Unmarshal([]byte(commandJSON), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Action != controller.PublicationOperationValidationFailure {
		t.Fatalf("failed parent-only gate did not advertise failure disposition: %#v", failure)
	}
}

func reenterParentOnlyFailedGate(t *testing.T, cfg config.AppConfig, handoff parentHandoffOutput, decomposed bool, failFlag string) {
	t.Helper()
	var exit int
	var diagnostics string
	if decomposed {
		exit, _, diagnostics = runTypedPublicationInProcessCLI(t, cfg, handoff.RequiredActionParameters, map[string]string{"failure_disposition": "reenter"})
	} else {
		exit, _, diagnostics = runTypedPublicationStagedCLI(t, cfg, handoff.RequiredActionParameters, map[string]string{"failure_disposition": "reenter"})
	}
	if exit != 0 {
		t.Fatalf("parent-only gate failure reentry: %s", diagnostics)
	}
	if err := os.Remove(failFlag); err != nil {
		t.Fatal(err)
	}
}

func rebindParentSourceThroughNativeOperations(t *testing.T, cfg config.AppConfig, store *controller.Store, candidate controller.AcceptedCandidate) {
	t.Helper()
	writeAppTestFile(t, cfg.RepoRoot, "evaluation-result.md", "corrected parent evaluation product\n")
	snapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"action": "rebind-parent-source", "expected_generation": head.ControllerGeneration, "candidate_id": candidate.CandidateID, "source_snapshot_id": snapshot.ID}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	handled, err := runControllerOperations([]string{"--authority", controllerExecutionSurface}, func() (config.AppConfig, error) { return cfg, nil }, strings.NewReader(string(data)), &output)
	if !handled || err != nil {
		t.Fatalf("native parent source rebind: %v", err)
	}
}

func assertParentOnlyReentryMaterialization(t *testing.T, cfg config.AppConfig, candidate controller.AcceptedCandidate, decomposed bool) {
	t.Helper()
	materialize := executeParentHandoff(t, cfg)
	runParentOnlyExecutionStep(t, cfg, materialize.RequiredActionParameters, decomposed)
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	smokeStore := state.AttachStateStore(bound)
	currentID, err := smokeStore.Read("task.id")
	if err != nil || currentID == "" || currentID == candidate.AttemptID {
		t.Fatalf("reentry smoke identity: %q err=%v", currentID, err)
	}
	if err := smokeStore.Write("task.id", candidate.AttemptID); err != nil {
		t.Fatal(err)
	}
	stale := executeParentHandoff(t, cfg)
	if stale.Consistent || stale.Inconsistency == nil {
		t.Fatalf("predecessor smoke identity accepted: %#v", stale)
	}
	if err := smokeStore.Write("task.id", currentID); err != nil {
		t.Fatal(err)
	}
}

func TestParentOnlyReviewStateIntegrity(t *testing.T) {
	failFlag := filepath.Join(t.TempDir(), "fail-final-gate")
	if err := os.WriteFile(failFlag, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, _, _, _ := parentOnlyCompletionFixture(t, true, failFlag)
	acceptParentOnlyThroughProductionBoundary(t, cfg, true)
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reviewState := state.AttachStateStore(bound).Path("parent-review-state.json")
	for _, kind := range []string{"corrupt", "dangling-link"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "corrupt" {
				if err := os.WriteFile(reviewState, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(filepath.Join(t.TempDir(), "missing-review"), reviewState); err != nil {
				t.Fatal(err)
			}
			projection := executeParentHandoff(t, cfg)
			if projection.Consistent || projection.Inconsistency == nil {
				t.Fatalf("untrusted review state accepted: %#v", projection)
			}
			if err := os.Remove(reviewState); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func acceptParentOnlyThroughProductionBoundary(t *testing.T, cfg config.AppConfig, executeInProcess bool) {
	t.Helper()
	for step := 0; step < 6; step++ {
		handoff := executeParentHandoff(t, cfg)
		if !handoff.Consistent || handoff.RequiredAction == nil {
			t.Fatalf("parent-only acceptance handoff: %#v", handoff)
		}
		switch *handoff.RequiredAction {
		case controllerExecutionSurface:
			runParentOnlyExecutionStep(t, cfg, handoff.RequiredActionParameters, executeInProcess)
		case controllerPublicationSurface:
			var command controller.PublicationParentCommand
			if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil {
				t.Fatal(err)
			}
			if command.Action == controller.PublicationOperationAccept {
				var exit int
				var output, diagnostics string
				if executeInProcess {
					exit, output, diagnostics = runTypedPublicationInProcessCLI(t, cfg, handoff.RequiredActionParameters, parentOnlySemanticSlots(t, handoff.RequiredActionParameters, true))
				} else {
					exit, output, diagnostics = runTypedPublicationStagedCLI(t, cfg, handoff.RequiredActionParameters, parentOnlySemanticSlots(t, handoff.RequiredActionParameters, true))
				}
				if exit != 0 {
					t.Fatalf("parent-only completion boundary: %s", diagnostics)
				}
				var result struct {
					Terminal struct {
						Status string `json:"status"`
					} `json:"terminal"`
				}
				if err := json.Unmarshal([]byte(output), &result); err != nil || result.Terminal.Status != "blocked" {
					t.Fatalf("parent-only final-gate failure was not retained: %s %v", output, err)
				}
				return
			}
			runParentOnlyPublicationStep(t, cfg, handoff.RequiredActionParameters, true, executeInProcess)
		default:
			t.Fatalf("unexpected parent-only acceptance action %q", *handoff.RequiredAction)
		}
	}
	t.Fatal("parent-only acceptance did not converge")
}

func parentOnlyAppTaskContent(mode string) string {
	return "# parent evaluation\n\n## External feasibility\n\nstatus: not-applicable\n\n## Execution authority\n\nmode: " + mode + "\n\n## Contract\n\nparent evaluation products\n\n## Dependencies\n\nnone\n"
}

func parentOnlyWorkerShimDir(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' -test.run=^TestParentOnlyWorkerProcess$ -- \"$@\"\n"
	if err := execfile.Write(filepath.Join(dir, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_PARENT_ONLY_TEST_WORKER", "1")
	return dir
}

func TestParentOnlyWorkerProcess(_ *testing.T) {
	if os.Getenv("GLM_PARENT_ONLY_TEST_WORKER") != "1" {
		return
	}
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		os.Exit(2)
	}
	launchQualityGateRunner = func(st *state.StateStore, record qualitygate.RunRecord) (qualityGateRunnerWait, error) {
		return func() error { return executeQualityGateRun(st, record.ValidationRunID) }, nil
	}
	startQualityGateResourceSampler = newDeterministicQualityGateResourceSampler
	if err := Run(os.Args[separator+1:]); err != nil {
		_ = WriteProcessError(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func parentOnlyCompletionFixture(t *testing.T, sourceChange bool, failFlags ...string) (config.AppConfig, *controller.Store, controller.PublicationPolicy, parentOnlyWorkflowTasks, string) {
	t.Helper()
	started := time.Now()
	cfg := newCanonicalAppConfig(t)
	logAppFixturePhase(t, "parent-only/corpus-git", started)
	stateHome := t.TempDir()
	cfg.StateBase = filepath.Join(stateHome, "sessions")
	cfg.ClaudeConfigDir = filepath.Join(stateHome, "claude")
	cfg.CodexConfigDir = filepath.Join(stateHome, "codex")
	t.Setenv("GLM_WORKER_HOME", stateHome)
	workerShim := parentOnlyWorkerShimDir(t)
	path := workerShim + string(os.PathListSeparator) + os.Getenv("PATH")
	goShim := ""
	if sourceChange {
		goShim = prepareParentOnlyGoShim(t, failFlags...)
		path = goShim + string(os.PathListSeparator) + path
	}
	t.Setenv("PATH", path)

	tasks := parentOnlyWorkflowTasks{parent: "IMPLEMENTATION_TASKS/parent-evaluation.md", successor: "IMPLEMENTATION_TASKS/successor.md"}
	repo := cfg.RepoRoot
	writeAppTestFile(t, repo, "IMPLEMENTATION_RULES.md", "integration rules\n")
	writeAppTestFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## NEXT\n\n- `"+tasks.parent+"`\n\n- `"+tasks.successor+"`\n")
	writeAppTestFile(t, repo, tasks.parent, parentOnlyAppTaskContent(controller.ExecutionAuthorityParentOnly))
	writeAppTestFile(t, repo, tasks.successor, "# successor\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\nsuccessor task\n\n## Dependencies\n\nnone\n")
	started = time.Now()
	prepareCompletionQualityCorpus(t, repo)
	logAppFixturePhase(t, "parent-only/quality-corpus", started)
	started = time.Now()
	policy := commitCorpusWithPublicationRemote(t, repo, "parent-only completion test", "parent-only@example.invalid", "parent-only completion corpus")
	logAppFixturePhase(t, "parent-only/fixture-git-remote", started)

	started = time.Now()
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	evidence := canonicalWorkflowEvidence(t, cfg, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "root runtime result", Policy: policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if runtime := completeAwaitedRuntimeThroughProductionAPIs(t, cfg, admission); runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("root fixture left the runtime at %s", runtime.TaskStatus())
	}
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
		t.Fatal(err)
	}
	logAppFixturePhase(t, "parent-only/root-controller-accept-publish", started)

	if sourceChange {
		writeAppTestFile(t, repo, "glm-worker/internal/fixture/fixture.go", "package fixture\n\nfunc Run() { _ = parentOnlyProduct() }\n\nfunc parentOnlyProduct() error { return nil }\n")
	} else {
		writeAppTestFile(t, repo, "evaluation-result.md", "parent evaluation product\n")
	}
	return cfg, store, policy, tasks, goShim
}

func prepareParentOnlyGoShim(t *testing.T, failFlags ...string) string {
	t.Helper()
	failFlag := filepath.Join(t.TempDir(), "unused-fail-flag")
	if len(failFlags) != 0 {
		failFlag = failFlags[0]
	}
	shim, _ := writeQualityGateGoShim(t, failFlag)
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
	if err := execfile.Write(shimPath, []byte(forward+strings.TrimPrefix(string(shimBody), "#!/bin/sh\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	return shim
}

func driveParentOnlyCompletion(t *testing.T, cfg config.AppConfig, goShim string, sourceChange, interrupt, fullProcess bool) {
	t.Helper()
	for step := 0; step < 12; step++ {
		handoff := executeParentHandoff(t, cfg)
		if !handoff.Consistent {
			t.Fatalf("inconsistent parent-only handoff: %s", stringPointerValue(handoff.Inconsistency))
		}
		if handoff.RequiredAction == nil {
			t.Fatalf("parent-only handoff lost its required action: %#v", handoff)
		}
		switch *handoff.RequiredAction {
		case controllerExecutionSurface:
			runParentOnlyExecutionStep(t, cfg, handoff.RequiredActionParameters, !fullProcess)
		case controllerPublicationSurface:
			if interrupt {
				interruptParentOnlyPublication(t, cfg, handoff.RequiredActionParameters, sourceChange)
				interrupt = false
				continue
			}
			runParentOnlyPublicationStep(t, cfg, handoff.RequiredActionParameters, sourceChange, !fullProcess)
		case controller.FinalValidationSurface:
			runTypedFinalValidationStep(t, cfg, goShim, handoff.RequiredActionParameters)
		case canonicalStartAction:
			return
		default:
			dump, _ := json.Marshal(handoff)
			t.Fatalf("parent-only completion advertised unexpected action %q: %s", *handoff.RequiredAction, dump)
		}
	}
	t.Fatal("parent-only completion did not converge to the successor start boundary")
}

func runParentOnlyExecutionStep(t *testing.T, cfg config.AppConfig, parameters map[string]string, executeInProcess bool) {
	t.Helper()
	payload := parameters["controller_command_json"]
	if payload == "" {
		t.Fatalf("parent-only execution handoff lost its typed command: %#v", parameters)
	}
	var command workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != controller.PublicationOperationParentOnlyMaterialize && command.Action != "materialize" && command.Action != "cleanup" && command.Action != "recover" {
		t.Fatalf("parent-only completion advertised unexpected execution action %q", command.Action)
	}
	executeParentOnlyControllerAction(t, cfg, payload, executeInProcess)
}

func executeParentOnlyControllerAction(t *testing.T, cfg config.AppConfig, payload string, executeInProcess bool) {
	t.Helper()
	exit, output, diagnostics := runParentActionCLIInProcess(t, cfg, "prepare", controllerExecutionSurface)
	if exit != 0 {
		t.Fatalf("prepare parent-only controller action: %s", diagnostics)
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
	if strings.Contains(string(body), "__GLM_PARENT_ACTION_PAYLOAD__") {
		if err := os.WriteFile(prepared.Path, []byte(strings.Replace(string(body), "__GLM_PARENT_ACTION_PAYLOAD__", payload, 1)), 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		assertStagedPublicationTemplate(t, body, payload)
	}
	if executeInProcess {
		exit, output, diagnostics = runParentActionCLIInProcess(t, cfg, controllerExecutionSurface, prepared.Token)
	} else {
		exit, output, diagnostics = runTypedParentActionCLI(t, cfg, controllerExecutionSurface, prepared.Token)
	}
	if exit == 0 {
		return
	}
	var envelope struct {
		Status       string `json:"status"`
		Continuation *struct {
			Locator string `json:"locator"`
			SHA256  string `json:"sha256"`
		} `json:"continuation"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil || envelope.Status != "parent_action_terminal_projection_overflow" || envelope.Continuation == nil || !strings.Contains(diagnostics, "terminal projection exceeds budget") {
		t.Fatalf("execute parent-only controller action: %s", diagnostics)
	}
	artifact, err := os.ReadFile(envelope.Continuation.Locator)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(artifact)
	if !json.Valid(artifact) || hex.EncodeToString(digest[:]) != envelope.Continuation.SHA256 {
		t.Fatal("parent-only overflow continuation lost its exact terminal result")
	}
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.AttachStateStore(bound).LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent-only overflow continuation created a model Runtime binding: %v", err)
	}
}

func runParentOnlyPublicationStep(t *testing.T, cfg config.AppConfig, parameters map[string]string, sourceChange, executeInProcess bool) {
	t.Helper()
	var command controller.PublicationParentCommand
	if err := json.Unmarshal([]byte(parameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	var exit int
	var output, diagnostics string
	if executeInProcess {
		exit, output, diagnostics = runTypedPublicationInProcessCLI(t, cfg, parameters, parentOnlySemanticSlots(t, parameters, sourceChange))
	} else {
		exit, output, diagnostics = runTypedPublicationStagedCLI(t, cfg, parameters, parentOnlySemanticSlots(t, parameters, sourceChange))
	}
	if exit != 0 {
		t.Fatalf("execute parent-only publication %s: %s", command.Action, diagnostics)
	}
	var envelope struct {
		Terminal struct {
			Status  string `json:"status"`
			Failure any    `json:"failure"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Terminal.Status == "blocked" {
		t.Fatalf("parent-only completion blocked: %#v", envelope.Terminal.Failure)
	}
}

func interruptParentOnlyPublication(t *testing.T, cfg config.AppConfig, parameters map[string]string, sourceChange bool) {
	t.Helper()
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	hooks := t.TempDir()
	if err := execfile.Write(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	primary := store.Identity().PrimaryRoot
	runMeasuredCompletionFixtureGit(t, primary, "config", "core.hooksPath", hooks)
	exit, output, diagnostics := runTypedPublicationCLI(t, cfg, parameters, parentOnlySemanticSlots(t, parameters, sourceChange))
	runMeasuredCompletionFixtureGit(t, primary, "config", "--unset", "core.hooksPath")
	if exit == 0 {
		t.Fatalf("injected parent-only publication fault succeeded: out=<%s> err=<%s>", output, diagnostics)
	}
	head, err := store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("parent-only publication fault lost its recovery journal: %v", err)
	}
}

func parentOnlySemanticSlots(t *testing.T, parameters map[string]string, sourceChange bool) map[string]string {
	t.Helper()
	var command controller.PublicationParentCommand
	if err := json.Unmarshal([]byte(parameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	switch command.Action {
	case controller.PublicationOperationAccept:
		validations := `{"forms":[]}`
		if sourceChange {
			validations = `{"forms":[{"form":"go-test","working_dir":"glm-worker"}]}`
		}
		return map[string]string{
			"message":              "parent accepts the parent-only products",
			"require_install":      "false",
			"required_validations": validations,
		}
	case controller.PublicationOperationInstall, controller.PublicationOperationValidationFailure:
		return map[string]string{"failure_disposition": "retry"}
	default:
		return map[string]string{}
	}
}

func assertParentOnlyTerminalState(t *testing.T, cfg config.AppConfig, store *controller.Store, policy controller.PublicationPolicy, tasks parentOnlyWorkflowTasks, sourceChange bool) {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.ExecutionTaskRef != nil || head.LiveLeaseID != "" || head.MetadataLineageRef == nil {
		t.Fatalf("parent-only completion did not retire the task authority: %#v", head)
	}
	remote := appTestGitOutput(t, store.Identity().PrimaryRoot, "ls-remote", policy.Remote, policy.RemoteRef)
	if !strings.Contains(string(remote), head.IntegrationTip) {
		t.Fatalf("published remote tip %s differs from final authority %s", strings.TrimSpace(string(remote)), head.IntegrationTip)
	}
	terminal, err := store.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(terminal.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.CommitOID != head.IntegrationTip || candidate.BaseOID == candidate.CommitOID {
		t.Fatal("parent-only terminal source differs from the final authority")
	}
	if commits := strings.TrimSpace(string(appTestGitOutput(t, store.Identity().PrimaryRoot, "rev-list", "--count", candidate.BaseOID+".."+candidate.CommitOID))); commits != "1" {
		t.Fatalf("parent-only task published %s commits", commits)
	}
	assertParentOnlyCandidateEvidence(t, store, candidate, sourceChange)
	authority, err := controller.ResolveCommittedTaskAuthorityAt(store.Identity().PrimaryRoot, candidate.CommitOID)
	if err != nil {
		t.Fatal(err)
	}
	if authority.Task.TaskPath != tasks.successor {
		t.Fatalf("parent-only completion did not promote the successor: %#v", authority.Task)
	}
	if _, err := exec.Command("git", "-C", store.Identity().PrimaryRoot, "cat-file", "-e", candidate.CommitOID+":"+tasks.parent).Output(); err == nil {
		t.Fatal("parent-only completion kept its completed task file in the final commit")
	}
	if _, err := exec.Command("git", "-C", store.Identity().PrimaryRoot, "cat-file", "-e", candidate.BaseOID+":"+tasks.parent).Output(); err != nil {
		t.Fatal("parent-only completion lost the original requirement from Git history")
	}
	successorCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state.AttachStateStore(successorCfg).Exists("task.id") {
		t.Fatal("parent-only completion started the NEXT task")
	}
	if _, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("parent-only completion repeated a succeeded remote publication")
	}
}

func assertParentOnlyCandidateEvidence(t *testing.T, store *controller.Store, candidate controller.AcceptedCandidate, sourceChange bool) {
	t.Helper()
	kinds := map[string]string{}
	for _, ref := range candidate.Evidence {
		data, err := store.LoadEvidenceObject(ref)
		if err != nil {
			t.Fatal(err)
		}
		var record controller.CandidateEvidence
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		kinds[record.Kind] = record.Result
		if record.Kind != "parent-acceptance" {
			continue
		}
		artifact, err := store.LoadEvidenceObject(record.Artifact)
		if err != nil {
			t.Fatal(err)
		}
		var proof controller.ParentOnlyAcceptanceProof
		if err := json.Unmarshal(artifact, &proof); err != nil {
			t.Fatal(err)
		}
		if proof.Mode != controller.ExecutionAuthorityParentOnly || proof.ModelExecution != "none" || proof.ReviewAuthority != "parent-decision" {
			t.Fatalf("published parent acceptance lost its explicit parent-only authority: %#v", proof)
		}
	}
	if kinds["parent-acceptance"] != "accepted" || kinds["review"] != "" || kinds["validation-obligation"] != "declared" {
		t.Fatalf("parent-only candidate evidence kinds: %#v", kinds)
	}
	if candidate.BundleCheckRef == nil {
		t.Fatal("parent-only candidate froze without its completion Bundle observation")
	}
	bundleData, err := store.LoadEvidenceObject(*candidate.BundleCheckRef)
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		RuntimeTaskID  string `json:"runtime_task_id"`
		TaskClockBasis string `json:"task_clock_basis"`
		FinalRuns      []any  `json:"final_validation_runs"`
	}
	if err := json.Unmarshal(bundleData, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.RuntimeTaskID != "" || bundle.TaskClockBasis != "no-model-runtime" {
		t.Fatalf("parent-only Bundle hid the missing Runtime: %#v", bundle)
	}
	expectedRuns := 0
	if sourceChange {
		expectedRuns = 1
	}
	if len(bundle.FinalRuns) != expectedRuns {
		t.Fatalf("parent-only Bundle final validation runs: %#v", bundle.FinalRuns)
	}
	if len(candidate.RequiredValidations) != expectedRuns {
		t.Fatalf("parent-only candidate required validations: %#v", candidate.RequiredValidations)
	}
}

func TestParentOnlyAmendmentSyncThroughProductionBoundary(t *testing.T) {
	cfg, store, policy, tasks, goShim := parentOnlyCompletionFixture(t, false)
	start := executeParentHandoff(t, cfg)
	if !start.Consistent || start.RequiredAction == nil || *start.RequiredAction != controllerExecutionSurface {
		t.Fatalf("parent-only materialization not projected: %#v", start)
	}
	runParentOnlyExecutionStep(t, cfg, start.RequiredActionParameters, true)
	// Only the Task file may change during authority sync. The product is
	// restored after the revised contract is admitted.
	if err := os.Remove(filepath.Join(cfg.RepoRoot, "evaluation-result.md")); err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if before.LiveAttemptID == "" || before.ExecutionTaskRef == nil {
		t.Fatalf("materialization omitted parent-only attempt: %#v", before)
	}
	path := filepath.Join(cfg.RepoRoot, tasks.parent)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	amended := append(append([]byte(nil), original...), []byte("\n## Amendments\n\n- Same-attempt parent-only acceptance requirement.\n")...)
	if err := os.WriteFile(path, amended, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(amended)
	revised := controller.SemanticTaskRef{TaskPath: tasks.parent, ContractDigest: hex.EncodeToString(sum[:])}
	pending := executeParentHandoff(t, cfg)
	if !pending.Consistent || pending.RequiredAction == nil || *pending.RequiredAction != controllerExecutionSurface {
		t.Fatalf("parent-only amendment did not demand a Controller sync: %#v", pending)
	}
	var command workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(pending.RequiredActionParameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != "sync-task-authority" || command.ExpectedGeneration != before.ControllerGeneration ||
		command.RevisedTask == nil || !command.RevisedTask.Equal(revised) || command.SourceSnapshotID == "" {
		t.Fatalf("parent-only amendment sync projected wrong authority: %#v", command)
	}
	stale := command
	stale.ExpectedGeneration--
	if _, err := workflow.ExecuteControllerExecution(cfg, store, stale); err == nil {
		t.Fatal("stale Controller generation was admitted for parent-only sync")
	}
	foreign := command
	unrelated := *command.RevisedTask
	unrelated.TaskPath = "IMPLEMENTATION_TASKS/successor.md"
	foreign.RevisedTask = &unrelated
	if _, err := workflow.ExecuteControllerExecution(cfg, store, foreign); err == nil {
		t.Fatal("foreign Task was admitted as a parent-only amendment")
	}
	result, err := workflow.ExecuteControllerExecution(cfg, store, command)
	if err != nil {
		t.Fatalf("existing sync owner rejected parent-only smoke identity: %v", err)
	}
	if result.Head.LiveAttemptID != before.LiveAttemptID ||
		result.Head.ExecutionTaskRef == nil || !result.Head.ExecutionTaskRef.Equal(revised) ||
		result.Head.RootTaskRef == nil || !result.Head.RootTaskRef.Equal(revised) {
		t.Fatalf("amendment changed Root attempt or lost revised authority: %#v", result.Head)
	}
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime := state.AttachStateStore(bound)
	id, err := runtime.Read("task.id")
	if err != nil || id != before.LiveAttemptID {
		t.Fatalf("amendment changed smoke attempt identity: %q %v", id, err)
	}
	if _, err := runtime.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent-only sync created a model Runtime binding: %v", err)
	}
	writeAppTestFile(t, cfg.RepoRoot, "evaluation-result.md", "parent evaluation product\n")
	after := executeParentHandoff(t, cfg)
	if !after.Consistent || after.RequiredAction == nil || *after.RequiredAction != controllerPublicationSurface {
		t.Fatalf("amended parent-only attempt did not return to completion: %#v", after)
	}
	driveParentOnlyCompletion(t, cfg, goShim, false, false, true)
	assertParentOnlyTerminalState(t, cfg, store, policy, tasks, false)
}

func TestParentOnlyAmendmentSyncRejectsForgedRuntimeBinding(t *testing.T) {
	cfg, store, _, _, _ := parentOnlyCompletionFixture(t, false)
	start := executeParentHandoff(t, cfg)
	runParentOnlyExecutionStep(t, cfg, start.RequiredActionParameters, true)
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime := state.AttachStateStore(bound)
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID: head.LiveAttemptID, TaskPath: head.ExecutionTaskRef.TaskPath,
		TaskContractDigest: head.ExecutionTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	runtimeCfg, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wf := workflow.NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	admission, err := controller.CurrentSemanticAdmission(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wf.CaptureParentOnlyTaskAuthoritySyncEvidence(admission); err == nil {
		t.Fatal("forged model Runtime binding was accepted as a smoke-only identity")
	}
}
