package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactioncmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type typedCompletionRunner struct {
	canonicalWorkflowRunner
	sourceChange bool
	workerRounds int
}

type typedCompletionScenario struct {
	name         string
	sourceChange bool
	interrupt    bool
	fullProcess  bool
	handoffFault string
}

type typedPublicationEnvelope struct {
	Status   string              `json:"status"`
	Handoff  parentHandoffOutput `json:"handoff"`
	Recovery struct {
		Command    []string `json:"command"`
		WorkingDir string   `json:"working_dir"`
	} `json:"handoff_recovery"`
	Terminal struct {
		Status       string `json:"status"`
		Failure      any    `json:"failure"`
		SourceOID    string `json:"source_oid"`
		FinalOID     string `json:"final_oid"`
		RemoteOID    string `json:"remote_oid"`
		StopBoundary string `json:"stop_boundary"`
		TransitionID string `json:"transition_id"`
	} `json:"terminal"`
}

func typedCompletionScenarios() []typedCompletionScenario {
	return []typedCompletionScenario{
		{name: "no-code", fullProcess: true},
		{name: "no-code-publication-reentry", interrupt: true},
		{name: "runtime-change", sourceChange: true},
		{name: "runtime-change-publication-reentry", sourceChange: true, interrupt: true},
		{name: "no-code-handoff-successor-telemetry", fullProcess: true, handoffFault: "telemetry"},
		{name: "no-code-handoff-io-failure", fullProcess: true, handoffFault: "io"},
		{name: "no-code-handoff-task-change", fullProcess: true, handoffFault: "task"},
		{name: "no-code-handoff-lease-change", fullProcess: true, handoffFault: "lease"},
		{name: "no-code-handoff-status-change", fullProcess: true, handoffFault: "status"},
	}
}

func appFixtureCostTarget(t *testing.T) bool {
	if os.Getenv(isolatedScenarioEnv) != "" {
		return true
	}
	for _, name := range []string{
		"TestTypedParentCompletionWorkflowThroughProductionBoundary",
		"TestParentOnlyTaskCompletionThroughProductionBoundary",
		"TestParentOnlyReentryThroughProductionBoundary",
	} {
		if t.Name() == name || strings.HasPrefix(t.Name(), name+"/") {
			return true
		}
	}
	return false
}

func logAppFixturePhase(t *testing.T, phase string, started time.Time) {
	t.Helper()
	if appFixtureCostTarget(t) {
		t.Logf("app-fixture-cost phase=%s elapsed=%s", phase, time.Since(started))
	}
}

func runMeasuredCompletionFixtureGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	started := time.Now()
	runControllerActivationGit(t, repo, args...)
	if appFixtureCostTarget(t) {
		t.Logf("app-fixture-cost kind=fixture-git args=%q elapsed=%s", args, time.Since(started))
	}
}

func TestTypedParentCompletionWorkflowThroughProductionBoundary(t *testing.T) {
	t.Parallel()
	for _, scenario := range typedCompletionScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			runIsolatedScenario(t, isolatedScenarioKeyTypedCompletion, scenario.name)
		})
	}
}

func typedCompletionScenarioBody(t *testing.T, scenario typedCompletionScenario) {
	t.Helper()
	binary := typedCompletionWorkerBinary(t)
	started := time.Now()
	cfg, runtime, goShim := typedCompletionWorkflowFixture(t, binary, scenario.sourceChange)
	logAppFixturePhase(t, "typed/fixture", started)
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	remote := strings.TrimSpace(string(appTestGitOutput(t, store.Identity().PrimaryRoot, "remote", "get-url", "origin")))
	if err := execfile.Write(filepath.Join(remote, "hooks", "post-receive"), []byte("#!/bin/sh\ncat >> task-publication-receipts\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if scenario.handoffFault != "" {
		t.Setenv("GLM_TYPED_COMPLETION_HANDOFF_FAULT", scenario.handoffFault)
	}
	started = time.Now()
	driveTypedCompletionToStart(t, cfg, goShim, scenario.sourceChange, scenario.interrupt, scenario.fullProcess)
	logAppFixturePhase(t, "typed/production-cli", started)
	started = time.Now()
	assertTypedCompletionTerminalState(t, cfg, runtime)
	receipts, err := os.ReadFile(filepath.Join(remote, "task-publication-receipts"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(receipts)), "\n"); len(lines) != 1 {
		t.Fatalf("expected one accepted remote update, got %q", receipts)
	}
	logAppFixturePhase(t, "typed/terminal-assertions", started)
}

func typedCompletionWorkerBinary(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "glm-worker")
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' -test.run=^TestTypedCompletionWorkerProcess$ -- \"$@\"\n"
	if err := execfile.Write(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_TYPED_COMPLETION_TEST_WORKER", "1")
	return path
}

func TestTypedCompletionWorkerProcess(_ *testing.T) {
	if os.Getenv("GLM_TYPED_COMPLETION_TEST_WORKER") != "1" {
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
	args := os.Args[separator+1:]
	if primary := os.Getenv("GLM_TYPED_COMPLETION_HANDOFF_PRIMARY"); primary != "" && len(args) == 1 && args[0] == "--handoff" {
		root, err := os.Getwd()
		if err != nil || root != primary {
			_, _ = fmt.Fprintln(os.Stderr, "completion handoff cannot read the retired execution lane")
			os.Exit(1)
		}
		if fault := os.Getenv("GLM_TYPED_COMPLETION_HANDOFF_FAULT"); fault != "" {
			if err := typedCompletionHandoffFault(fault); err != nil {
				_ = WriteProcessError(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	launchQualityGateRunner = func(st *state.StateStore, record qualitygate.RunRecord) (qualityGateRunnerWait, error) {
		return func() error { return executeQualityGateRun(st, record.ValidationRunID) }, nil
	}
	startQualityGateResourceSampler = newDeterministicQualityGateResourceSampler
	runCompletionInstaller = func(root, sourceOID string, output io.Writer) (controller.InstalledRuntimeIdentity, error) {
		if flag := os.Getenv("GLM_TYPED_TEST_INSTALL_FAILURE"); flag != "" {
			if _, err := os.Stat(flag); err == nil {
				return controller.InstalledRuntimeIdentity{}, fmt.Errorf("test native installation failure")
			}
		}

		command := exec.Command("git", "rev-parse", "HEAD")
		command.Dir, command.Stdout, command.Stderr = root, output, output
		if err := command.Run(); err != nil {
			return controller.InstalledRuntimeIdentity{}, err
		}
		binary := filepath.Join(os.Getenv("GLM_WORKER_HOME"), "typed-installed-worker")
		data := []byte("#!/bin/sh\nexit 0\n")
		if err := execfile.Write(binary, data, 0o755); err != nil {
			return controller.InstalledRuntimeIdentity{}, err
		}
		sum := sha256.Sum256(data)
		return controller.InstalledRuntimeIdentity{SourceOID: sourceOID, BinaryPath: binary, BinarySHA256: hex.EncodeToString(sum[:])}, nil
	}
	if err := Run(args); err != nil {
		_ = WriteProcessError(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func typedCompletionHandoffFault(fault string) error {
	if fault == "io" {
		return os.ErrPermission
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg, err = controller.WorkflowConfig(cfg)
	if err != nil {
		return err
	}
	if state.AttachStateStore(cfg).Present() {
		return fmt.Errorf("successor already has Runtime state")
	}
	if fault == "telemetry" {
		return typedCompletionHandoffWithSuccessorTelemetry(cfg)
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(st.Path("repo-root"))) }()
	return printParentHandoffLeased(st, os.Stdout, func() (parentHandoffOutput, error) {
		value := buildParentHandoffWithConfig(cfg, st)
		return value, changeTypedCompletionHandoffScope(st, fault)
	})
}

func typedCompletionHandoffWithSuccessorTelemetry(cfg config.AppConfig) error {
	st := state.AttachStateStore(cfg)
	return printParentHandoffLeased(st, os.Stdout, func() (parentHandoffOutput, error) {
		value := buildParentHandoffWithConfig(cfg, st)
		st.RecordParentActionAttempt(state.ParentActionAttemptRecord{
			AttemptID: "parallel-export", Action: "export-bundle", EnteredAt: time.Now().UTC(), Outcome: "started",
		})
		if !st.Present() || st.ReadOr("task.id", "") != "" || st.TaskStatus() != state.TaskStatusNone {
			return parentHandoffOutput{}, fmt.Errorf("successor telemetry did not create a directory without starting the Task")
		}
		return value, nil
	})
}

func changeTypedCompletionHandoffScope(st *state.StateStore, fault string) error {
	switch fault {
	case "task":
		return st.Write("task.id", "another-task")
	case "lease":
		return st.AdvanceParentEvidenceLease()
	case "status":
		return st.SetTaskStatus(state.TaskStatusComplete)
	default:
		return fmt.Errorf("unknown handoff fault %q", fault)
	}
}

func runTypedParentActionCLI(t *testing.T, cfg config.AppConfig, args ...string) (int, string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	worker, err := exec.LookPath("glm-worker")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, append([]string{"-test.run", "^TestParentActionCLIProcess$", "--"}, args...)...)
	command.Dir = cfg.RepoRoot
	command.Env = parentActionChildEnv(t, cfg, filepath.Dir(worker))
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	started := time.Now()
	code := childExitCode(command.Run())
	if appFixtureCostTarget(t) {
		t.Logf("app-fixture-cost kind=cli args=%q elapsed=%s exit=%d", args, time.Since(started), code)
	}
	return code, stdout.String(), stderr.String()
}

func runParentActionCLIInProcess(t *testing.T, cfg config.AppConfig, args ...string) (int, string, string) {
	t.Helper()
	workerDir := writeRetirementCLIShimDir(t)
	if worker, err := exec.LookPath("glm-worker"); err == nil {
		workerDir = filepath.Dir(worker)
	}
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cfg.RepoRoot); err != nil {
		t.Fatal(err)
	}
	savedEnv := os.Environ()
	defer func() {
		restoreErr := applyProcessEnvironment(savedEnv)
		dirErr := os.Chdir(previousDir)
		if restoreErr != nil || dirErr != nil {
			t.Fatalf("in-process parent action環境の復元に失敗: %v %v", restoreErr, dirErr)
		}
	}()
	if err := applyProcessEnvironment(parentActionChildEnv(t, cfg, workerDir)); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	started := time.Now()
	code := parentactioncmd.Run(args, &stdout, &stderr)
	if appFixtureCostTarget(t) {
		t.Logf("app-fixture-cost kind=cli-inprocess args=%q elapsed=%s exit=%d", args, time.Since(started), code)
	}
	return code, stdout.String(), stderr.String()
}

func applyProcessEnvironment(env []string) error {
	os.Clearenv()
	for _, item := range env {
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if err := os.Setenv(name, value); err != nil {
			return err
		}
	}
	return nil
}

func driveTypedCompletionToStart(t *testing.T, cfg config.AppConfig, goShim string, requireInstall, interrupt, fullProcess bool) {
	t.Helper()
	for step := 0; step < 12; step++ {
		handoff := executeParentHandoff(t, cfg)
		if !handoff.Consistent {
			t.Fatalf("inconsistent handoff during typed completion: %v", stringPointerValue(handoff.Inconsistency))
		}
		if handoff.RequiredAction == nil {
			t.Fatalf("typed completion lost its required action: %#v", handoff)
		}
		switch *handoff.RequiredAction {
		case controllerPublicationSurface:
			if runTypedPublicationStep(t, cfg, handoff.RequiredActionParameters, requireInstall, interrupt, !fullProcess) {
				return
			}
			interrupt = false
		case "finalize-check":
			runTypedFinalValidationStep(t, cfg, goShim, handoff.RequiredActionParameters)
		case controllerExecutionSurface:
			recovered := handoff.RequiredActionParameters["controller_command_json"]
			if recovered == "" {
				t.Fatalf("recovery handoff lost its typed command: %#v", handoff)
			}
			exit, output, diagnostics := runParentActionCLIInProcess(t, cfg, "prepare", controllerExecutionSurface)
			if exit != 0 {
				t.Fatal(diagnostics)
			}
			var prepared struct {
				NextCommand []string `json:"next_command"`
				Slots       []any    `json:"slots"`
			}
			if err := json.Unmarshal([]byte(output), &prepared); err != nil {
				t.Fatal(err)
			}
			if len(prepared.Slots) != 0 {
				t.Fatalf("mechanical recovery requested semantic inputs: %#v", prepared.Slots)
			}
			if fullProcess {
				if code, _, diagnostics := runTypedParentActionCLI(t, cfg, prepared.NextCommand[1:]...); code != 0 {
					t.Fatal(diagnostics)
				}
			} else if code, _, diagnostics := runParentActionCLIInProcess(t, cfg, prepared.NextCommand[1:]...); code != 0 {
				t.Fatal(diagnostics)
			}
		case canonicalStartAction:
			return
		default:
			t.Fatalf("typed completion advertised unexpected action %q: %#v", *handoff.RequiredAction, handoff)
		}
	}
	t.Fatal("typed completion did not converge to the successor start boundary")
}

func runTypedPublicationStep(t *testing.T, cfg config.AppConfig, parameters map[string]string, requireInstall, interrupt, executeInProcess bool) bool {
	t.Helper()
	var command controller.PublicationParentCommand
	if err := json.Unmarshal([]byte(parameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	if interrupt && command.Action == "complete" {
		interruptTypedPublication(t, cfg, parameters, requireInstall)
		return false
	}
	if command.Action == controller.PublicationOperationAccept {
		store, err := controller.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("GLM_TYPED_COMPLETION_HANDOFF_PRIMARY", store.Identity().PrimaryRoot)
	}
	var exit int
	var output, diagnostics string
	if executeInProcess {
		exit, output, diagnostics = runTypedPublicationInProcessCLI(t, cfg, parameters, typedSemanticSlotValues(t, parameters, requireInstall))
	} else {
		exit, output, diagnostics = runTypedPublicationStagedCLI(t, cfg, parameters, typedSemanticSlotValues(t, parameters, requireInstall))
	}
	handoffFault := os.Getenv("GLM_TYPED_COMPLETION_HANDOFF_FAULT")
	expectHandoffFailure := handoffFault != "" && handoffFault != "telemetry"
	if exit != 0 && !expectHandoffFailure {
		t.Fatalf("execute typed publication %s: %s", command.Action, diagnostics)
	}
	var envelope typedPublicationEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Terminal.Status == "blocked" {
		t.Fatalf("completion blocked at its first failed boundary: %#v", envelope.Terminal.Failure)
	}
	if envelope.Terminal.Status != "completed" && !expectHandoffFailure {
		return false
	}
	if expectHandoffFailure {
		assertTypedCompletionFailure(t, cfg, exit, envelope, diagnostics)
	} else if envelope.Status != "parent_action_terminal" || !envelope.Handoff.Consistent ||
		stringPointerValue(envelope.Handoff.RequiredAction) != canonicalStartAction {
		t.Fatalf("completion lacks a canonical successor handoff: %s", output)
	}
	assertTypedPublicationProof(t, envelope)
	return true
}

func assertTypedPublicationProof(t *testing.T, envelope typedPublicationEnvelope) {
	t.Helper()
	terminal := envelope.Terminal
	if terminal.Status != "completed" || terminal.FinalOID == "" || terminal.SourceOID != terminal.FinalOID || terminal.RemoteOID != terminal.FinalOID ||
		terminal.TransitionID == "" || terminal.StopBoundary != "next-task-not-started" {
		t.Fatalf("completion lacks its final publication proof: %#v", terminal)
	}
}

func assertTypedCompletionFailure(t *testing.T, cfg config.AppConfig, exit int, envelope typedPublicationEnvelope, diagnostics string) {
	t.Helper()
	if exit == 0 || envelope.Status != "parent_action_terminal_handoff_failed" || envelope.Handoff.RequiredAction != nil ||
		strings.Join(envelope.Recovery.Command, " ") != "glm-worker --handoff recovery" ||
		envelope.Recovery.WorkingDir != os.Getenv("GLM_TYPED_COMPLETION_HANDOFF_PRIMARY") {
		t.Fatalf("handoff I/O failure lost its read-only recovery: exit=%d recovery=%#v %s", exit, envelope.Recovery, diagnostics)
	}
	if os.Getenv("GLM_TYPED_COMPLETION_HANDOFF_FAULT") != "io" &&
		!strings.Contains(diagnostics, "parent evidence scope changed during projection") {
		t.Fatalf("handoff did not fail at the read-scope guard: %s", diagnostics)
	}
	assertTypedCompletionRecovery(t, cfg, envelope.Recovery.WorkingDir)
}

func assertTypedCompletionRecovery(t *testing.T, cfg config.AppConfig, root string) {
	t.Helper()
	cfg.RepoRoot = root
	var output bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	var handoff parentHandoffRecoveryOutput
	if err := json.Unmarshal(output.Bytes(), &handoff); err != nil {
		t.Fatal(err)
	}
	if !handoff.Consistent || stringPointerValue(handoff.RequiredAction) != canonicalStartAction {
		t.Fatalf("read-only recovery lost the successor boundary: %s", output.String())
	}
}

func interruptTypedPublication(t *testing.T, cfg config.AppConfig, parameters map[string]string, requireInstall bool) {
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
	exit, _, diagnostics := runTypedPublicationCLI(t, cfg, parameters, typedSemanticSlotValues(t, parameters, requireInstall))
	runMeasuredCompletionFixtureGit(t, primary, "config", "--unset", "core.hooksPath")
	if exit == 0 {
		t.Fatalf("injected publication fault succeeded: %s", diagnostics)
	}
	head, err := store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("publication fault lost its recovery journal: %v", err)
	}
}

func runTypedPublicationCLI(t *testing.T, cfg config.AppConfig, parameters map[string]string, semantic map[string]string, prepareOptions ...string) (int, string, string) {
	t.Helper()
	return runTypedPublicationStaging(t, cfg, parameters, semantic, false, false, prepareOptions...)
}

func runTypedPublicationStagedCLI(t *testing.T, cfg config.AppConfig, parameters map[string]string, semantic map[string]string, prepareOptions ...string) (int, string, string) {
	t.Helper()
	return runTypedPublicationStaging(t, cfg, parameters, semantic, true, false, prepareOptions...)
}

func runTypedPublicationInProcessCLI(t *testing.T, cfg config.AppConfig, parameters map[string]string, semantic map[string]string, prepareOptions ...string) (int, string, string) {
	t.Helper()
	return runTypedPublicationStaging(t, cfg, parameters, semantic, true, true, prepareOptions...)
}

func runTypedPublicationStaging(t *testing.T, cfg config.AppConfig, parameters map[string]string, semantic map[string]string, prepareInProcess, executeInProcess bool, prepareOptions ...string) (int, string, string) {
	t.Helper()
	var exit int
	var output, diagnostics string
	if prepareInProcess {
		exit, output, diagnostics = runParentActionCLIInProcess(t, cfg, append([]string{"prepare", controllerPublicationSurface}, prepareOptions...)...)
	} else {
		exit, output, diagnostics = runTypedParentActionCLI(t, cfg, append([]string{"prepare", controllerPublicationSurface}, prepareOptions...)...)
	}
	if exit != 0 {
		t.Fatalf("prepare typed publication: %s", diagnostics)
	}
	var prepared struct {
		Path        string                               `json:"path"`
		Slots       []struct{ Name, Placeholder string } `json:"slots"`
		NextCommand []string                             `json:"next_command"`
	}
	if err := json.Unmarshal([]byte(output), &prepared); err != nil {
		t.Fatal(err)
	}
	if len(prepared.Slots) != len(semantic) {
		t.Fatalf("prepared slots %#v disagree with semantic inputs %#v", prepared.Slots, semantic)
	}
	body, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepareOptions) == 0 {
		assertStagedPublicationTemplate(t, body, parameters["controller_command_json"])
	}
	staged := string(body)
	for _, slot := range prepared.Slots {
		value, ok := semantic[slot.Name]
		if !ok {
			t.Fatalf("machine-declared slot %q lacks a semantic decision", slot.Name)
		}
		staged = strings.Replace(staged, slot.Placeholder, value, 1)
	}
	if err := os.WriteFile(prepared.Path, []byte(staged), 0600); err != nil {
		t.Fatal(err)
	}
	if executeInProcess {
		return runParentActionCLIInProcess(t, cfg, prepared.NextCommand[1:]...)
	}
	return runTypedParentActionCLI(t, cfg, prepared.NextCommand[1:]...)
}

func typedSemanticSlotValues(t *testing.T, parameters map[string]string, requireInstall bool) map[string]string {
	t.Helper()
	var command controller.PublicationParentCommand
	if err := json.Unmarshal([]byte(parameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	semantic := map[string]string{}
	switch command.Action {
	case "complete":
		semantic["message"] = "typed completion accepted"
		semantic["require_install"] = fmt.Sprintf("%t", requireInstall)
		if requireInstall {
			semantic["required_validations"] = `{"forms":[{"form":"go-test","working_dir":"glm-worker"}],"probes":[{"probe_id":"typed-worker-path","verifies":"the deployed worker path executes the declared probe argv","argv":["/bin/sh","-c","exit 0"]}]}`
		} else {
			semantic["required_validations"] = `{"forms":[]}`
		}
	case "install-completion", "resolve-validation-failure":
		semantic["failure_disposition"] = "retry"
	case "promote", "publish", "recover":
	default:
		t.Fatalf("unexpected typed publication action %q", command.Action)
	}
	return semantic
}

func runTypedFinalValidationStep(t *testing.T, cfg config.AppConfig, goShim string, parameters map[string]string) {
	t.Helper()
	form := parameters["form"]
	if form != "go-test" && form != "go-test-race" {
		t.Fatalf("finalize-check advertised unknown form %q", form)
	}
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
	originalPath := os.Getenv("PATH")
	if goShim != "" {
		if err := os.Setenv("PATH", goShim+string(os.PathListSeparator)+originalPath); err != nil {
			t.Fatal(err)
		}
	}
	var output, diagnostics bytes.Buffer
	code := parentactioncmd.Run([]string{"finalize-check", form}, &output, &diagnostics)
	if goShim != "" {
		if err := os.Setenv("PATH", originalPath); err != nil {
			t.Fatal(err)
		}
	}
	if code != 0 {
		t.Fatalf("finalize-check %s: %d %s", form, code, diagnostics.String())
	}
	var result struct {
		Status  string `json:"status"`
		Routing *struct {
			SelectedDir string `json:"selected_dir"`
			Basis       string `json:"basis"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready_for_parent_decision" || result.Routing == nil || result.Routing.Basis != "accepted_candidate" {
		t.Fatalf("final validation result: %s", output.String())
	}
}

func assertTypedCompletionTerminalState(t *testing.T, cfg config.AppConfig, runtime *state.StateStore) {
	t.Helper()
	if runtime.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("published task did not complete the runtime: %s", runtime.TaskStatus())
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.ExecutionTaskRef != nil || head.LiveLeaseID != "" || head.MetadataLineageRef == nil {
		t.Fatalf("typed completion did not retire the task authority: %#v", head)
	}
	remote := appTestGitOutput(t, store.Identity().PrimaryRoot, "ls-remote", "origin", "refs/heads/main")
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
	if candidate.CommitOID != head.IntegrationTip || terminal.CandidateOID != candidate.CommitOID || candidate.BundleCheckRef == nil {
		t.Fatal("terminal source and frozen Bundle differ")
	}
	if commits := strings.TrimSpace(string(appTestGitOutput(t, store.Identity().PrimaryRoot, "rev-list", "--count", candidate.BaseOID+".."+candidate.CommitOID))); commits != "1" {
		t.Fatalf("Task published %s commits", commits)
	}
	frozen, err := store.LoadEvidenceObject(*candidate.BundleCheckRef)
	if err != nil {
		t.Fatal(err)
	}
	var bundle controller.CompletionBundleCheck
	if err := json.Unmarshal(frozen, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.FinalOID != candidate.CommitOID || len(bundle.FinalRuns) != len(candidate.RequiredValidations) || bundle.ModelCallsByRole[string(state.ReviewerRole)] == 0 {
		t.Fatal("Bundle lost final-source validation or independent review")
	}
	for _, final := range bundle.FinalRuns {
		run, err := qualitygate.Read(runtime, final.ValidationRunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Head != candidate.CommitOID || run.Status != qualitygate.StatusPass {
			t.Fatal("final validation is not a PASS on the published source")
		}
	}
	installed := false
	for _, ref := range candidate.Evidence {
		data, err := store.LoadEvidenceObject(ref)
		if err != nil {
			t.Fatal(err)
		}
		var evidence controller.CandidateEvidence
		if err := json.Unmarshal(data, &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence.Kind != "install" {
			continue
		}
		artifact, err := store.LoadEvidenceObject(evidence.Artifact)
		if err != nil {
			t.Fatal(err)
		}
		var proof controller.ParentCompletionInstallProof
		if err := json.Unmarshal(artifact, &proof); err != nil {
			t.Fatal(err)
		}
		if proof.CommitOID != candidate.CommitOID || proof.TreeOID != candidate.TreeOID || proof.Installed.SourceOID != candidate.CommitOID || proof.Smoke.Status != "pass" || proof.InstallLog == "" || proof.SmokeLog == "" {
			t.Fatal("actual install/smoke proof differs from the final candidate")
		}
		for _, source := range bundle.RunStores {
			for _, run := range source.Runs {
				if source.Store == "runtime-task-smoke" && run.RunID == proof.Smoke.ValidationRunID {
					installed = true
				}
			}
		}
	}
	if candidate.Policy.RequireInstall != installed {
		t.Fatal("Bundle froze without the required source-bound install smoke")
	}
	successorCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if successor := state.AttachStateStore(successorCfg); successor.Exists("task.id") {
		t.Fatal("typed completion started the NEXT task")
	}
}

func (r *typedCompletionRunner) Run(role state.SessionRole, phase, model string, readOnly bool, effort, prompt, resume string) (runner.RunResult, error) {
	result, err := r.canonicalWorkflowRunner.Run(role, phase, model, readOnly, effort, prompt, resume)
	if err != nil {
		return result, err
	}
	var value packet.Result
	if err := json.Unmarshal(result.StructuredOutput, &value); err != nil {
		return result, err
	}
	if role == state.WorkerRole && r.sourceChange {
		r.workerRounds++
		writeAppTestFile(r.t, r.cfg.RepoRoot, r.path, fmt.Sprintf("worker round %d\n", r.workerRounds))
		value.SetParentValidationRequest(&packet.ParentValidationRequest{Form: packet.ParentValidationGoTest, WorkingDir: "glm-worker"})
	}
	if role == state.ReviewerRole && !r.sourceChange {
		value.Targets = []string{r.task + ":1"}
	}
	body := appPacketBody(value)
	result.StructuredOutput = json.RawMessage(body)
	result.Response = body
	return result, nil
}

func typedCompletionWorkflowFixture(t *testing.T, binary string, sourceChange bool) (config.AppConfig, *state.StateStore, string) {
	t.Helper()
	cfg, runtime, shim, _, _ := typedCompletionReviewFixture(t, binary, sourceChange)
	started := time.Now()
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	logAppFixturePhase(t, "typed/parent-accept", started)
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("parent accept left Runtime at %s", runtime.TaskStatus())
	}
	return cfg, runtime, shim
}

func typedCompletionReviewFixture(t *testing.T, binary string, sourceChange bool) (config.AppConfig, *state.StateStore, string, controller.PublicationPolicy, *typedCompletionRunner) {
	t.Helper()
	started := time.Now()
	cfg, task, _, policy := newCanonicalWorkflowCorpus(t)
	logAppFixturePhase(t, "typed/corpus-git-remote", started)
	cfg.TelemetryContent = true
	body, err := os.ReadFile(filepath.Join(cfg.RepoRoot, task))
	if err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, cfg.RepoRoot, task, strings.Replace(string(body), "- `IMPLEMENTATION_TASKS/f11.md`", "none", 1))
	resultPath := ""
	goShim := ""
	binaryDir := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(binaryDir, "glm-worker")); err != nil {
		t.Fatal(err)
	}
	if sourceChange {
		writeAppTestFile(t, cfg.RepoRoot, "tests/install_smoke.sh", "#!/bin/sh\nset -eu\ntest -f typed-result.txt\ngit diff --exit-code\nprintf 'source smoke passed\\n'\n")
		for _, script := range []string{"install.sh", "tests/install_smoke.sh"} {
			if err := os.Chmod(filepath.Join(cfg.RepoRoot, script), 0755); err != nil {
				t.Fatal(err)
			}
		}
		resultPath = "typed-result.txt"
		writeAppTestFile(t, cfg.RepoRoot, resultPath, "base\n")
		tools := strings.SplitN(qualityToolsBinDirEnv(t)[0], "=", 2)[1]
		shim, _ := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "unused-fail-flag"))
		goShim = shim
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
		if err := execfile.Write(shimPath, []byte(forward+strings.TrimPrefix(string(shimBody), "#!/bin/sh\n")), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("QUALITY_TOOLS_BIN_DIR", tools)
	}
	path := binaryDir + string(os.PathListSeparator) + os.Getenv("PATH")
	if goShim != "" {
		path = goShim + string(os.PathListSeparator) + path
	}
	t.Setenv("PATH", path)
	started = time.Now()
	runMeasuredCompletionFixtureGit(t, cfg.RepoRoot, "add", ".")
	runMeasuredCompletionFixtureGit(t, cfg.RepoRoot, "commit", "-qm", "typed completion corpus")
	runMeasuredCompletionFixtureGit(t, cfg.RepoRoot, "push", "-q", "-u", "origin", "HEAD:refs/heads/main")
	logAppFixturePhase(t, "typed/fixture-git-publish", started)

	scripted := &typedCompletionRunner{canonicalWorkflowRunner: canonicalWorkflowRunner{complete: true, t: t, task: task, path: resultPath}, sourceChange: sourceChange}
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		scripted.cfg = actual
		return scripted
	}
	var output bytes.Buffer
	started = time.Now()
	if err := Execute(Command{Mode: ModeNewTask, Payload: "typed completion request"}, cfg, factory, &output, io.Discard); err != nil {
		t.Fatalf("worker round: %v %s", err, output.String())
	}
	logAppFixturePhase(t, "typed/worker-review", started)
	cfg = scripted.cfg
	runtime := state.AttachStateStore(cfg)
	if runtime.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("typed workflow did not open review: %s", runtime.TaskStatus())
	}
	if binding, err := runtime.CurrentParentReviewBinding(); err != nil || binding == nil {
		var returned packet.Result
		_ = json.Unmarshal(output.Bytes(), &returned)
		t.Fatalf("workflow did not produce bound independent review: binding=%#v err=%v status=%s summary=%s targets=%v question=%s issues=%s", binding, err, returned.Status, returned.Summary, returned.Targets, returned.SolQuestion, returned.Issues)
	}
	reviewedPath := resultPath
	if !sourceChange {
		reviewedPath = task
	}
	request := parentevidence.Manifest{Version: parentevidence.ManifestVersion, Reason: "verify typed completion source", Source: []parentevidence.SourceRequest{{Question: "inspect the independent review target", Path: reviewedPath, LineStart: 1, LineEnd: 1, BudgetBytes: 4096}}}
	if sourceChange {
		request.Diff = []parentevidence.DiffRequest{{Question: "review the implementation change", Paths: []string{resultPath}, BudgetBytes: 4096}}
	}
	manifest := writeParentReviewEvidenceManifest(t, request)
	var proof bytes.Buffer
	started = time.Now()
	if err := printParentEvidence(Command{EvidenceManifest: manifest}, cfg, runtime, &proof); err != nil {
		t.Fatal(err)
	}
	logAppFixturePhase(t, "typed/evidence-snapshot", started)
	return cfg, runtime, goShim, policy, scripted
}

func stringPointerValue(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return *value
}
