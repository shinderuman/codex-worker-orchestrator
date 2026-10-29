package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactioncmd"
)

func TestRetirementRecoveryCLIProcess(_ *testing.T) {
	if os.Getenv("GLM_RETIREMENT_RECOVERY_TEST_PROCESS") != "1" {
		return
	}
	if path := os.Getenv("GLM_COMPLETION_PID_FILE"); path != "" {
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(2)
		}
	}
	for index, arg := range os.Args {
		if arg == "--" {
			if err := Run(os.Args[index+1:]); err != nil {
				if err := WriteProcessError(os.Stderr, err); err != nil {
					os.Exit(2)
				}
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func TestParentActionCLIProcess(_ *testing.T) {
	if os.Getenv("GLM_PARENT_ACTION_TEST_PROCESS") != "1" {
		return
	}
	for index, arg := range os.Args {
		if arg == "--" {
			os.Exit(parentactioncmd.Run(os.Args[index+1:], os.Stdout, os.Stderr))
		}
	}
	os.Exit(2)
}

func writeRetirementCLIShimDir(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nGLM_RETIREMENT_RECOVERY_TEST_PROCESS=1 exec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' -test.run '^TestRetirementRecoveryCLIProcess$' -- \"$@\"\n"
	if err := execfile.Write(filepath.Join(dir, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func parentActionChildEnv(t *testing.T, cfg config.AppConfig, shimDir string) []string {
	t.Helper()
	home := filepath.Dir(cfg.StateBase)
	return append(append(os.Environ(),
		"GLM_PARENT_ACTION_TEST_PROCESS=1",
		"GLM_WORKER_HOME="+home,
		"HOME="+home,
		"CODEX_CONFIG_DIR="+filepath.Join(home, "codex"),
		"CLAUDE_CONFIG_DIR="+filepath.Join(home, "claude"),
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	), qualityToolsBinDirEnv(t)...)
}

func childExitCode(runErr error) int {
	if runErr == nil {
		return 0
	}
	exitErr := &exec.ExitError{}
	if errors.As(runErr, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
	}
	return 1
}

func runParentActionCLI(t *testing.T, cfg config.AppConfig, args ...string) (int, string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, append([]string{"-test.run", "^TestParentActionCLIProcess$", "--"}, args...)...)
	command.Dir = cfg.RepoRoot
	command.Env = parentActionChildEnv(t, cfg, writeRetirementCLIShimDir(t))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	exit := childExitCode(command.Run())
	return exit, stdout.String(), stderr.String()
}

func runAuthorityBootstrapChild(t *testing.T, cfg config.AppConfig) (int, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run", "^TestRetirementRecoveryCLIProcess$", "--", "--authority", "bootstrap")
	command.Dir = cfg.RepoRoot
	command.Env = append(parentActionChildEnv(t, cfg, writeRetirementCLIShimDir(t)), "GLM_RETIREMENT_RECOVERY_TEST_PROCESS=1")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	return childExitCode(command.Run()), output.String()
}

func interruptCompletionBeforePush(t *testing.T, cfg config.AppConfig, store *controller.Store, promoted controller.ExecutionOperationResult, policy controller.PublicationPolicy, candidate controller.AcceptedCandidate) string {
	t.Helper()
	hooks := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "controller-pid")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nset -eu\nkill -KILL \"$(cat " + quote(pidFile) + ")\"\nexit 1\n"
	if err := execfile.Write(filepath.Join(hooks, "pre-push"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runControllerActivationGit(t, cfg.RepoRoot, "config", "core.hooksPath", hooks)
	command := completionCLICommand(t, cfg, controllerPublicationSurface, controllerPublicationCommand{Action: "publish", ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	command.Env = append(command.Env, "GLM_COMPLETION_PID_FILE="+pidFile)
	data, runErr := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		t.Fatalf("process death did not interrupt CLI: %v %s", runErr, data)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("controller did not die by SIGKILL: %v %s", runErr, data)
	}
	runControllerActivationGit(t, cfg.RepoRoot, "config", "--unset", "core.hooksPath")
	head, err := store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("process death lost its journal: %v", err)
	}
	_, phase, err := store.LoadTransition(head.PendingTransitionID)
	if err != nil || phase.Completion == nil || !phase.Completion.WriteIntent || phase.Completion.ObservationRef != nil {
		t.Fatalf("process did not die after durable push intent and before observation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.RepoRoot, candidate.TaskRef.TaskPath)); !os.IsNotExist(err) {
		t.Fatalf("final Metadata was not applied before publication: %v", err)
	}
	before := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")))
	if before != candidate.CommitOID {
		t.Fatal("process death changed the final candidate commit")
	}
	if remote := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef); !strings.HasPrefix(string(remote), candidate.BaseOID+"\t") {
		t.Fatal("injected death allowed a remote write")
	}
	assertRetiredBootstrap(t, cfg, "IMPLEMENTATION_TASKS/successor.md")
	return before
}

func runStagedRetirementRecovery(t *testing.T, cfg config.AppConfig, command string) (int, string) {
	t.Helper()
	exit, out, diagnostics := runParentActionCLI(t, cfg, "prepare", controllerExecutionSurface)
	if exit != 0 {
		t.Fatalf("prepare recovery: exit=%d %s", exit, diagnostics)
	}
	var prepared struct {
		Path        string                               `json:"path"`
		Slots       []struct{ Name, Placeholder string } `json:"slots"`
		NextCommand []string                             `json:"next_command"`
	}
	if err := json.Unmarshal([]byte(out), &prepared); err != nil || len(prepared.Slots) != 0 || len(prepared.NextCommand) < 2 {
		t.Fatalf("recovery preparation contract: %v %s", err, out)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	assertStagedPublicationTemplate(t, data, command)
	exit, _, diagnostics = runParentActionCLI(t, cfg, prepared.NextCommand[1:]...)
	return exit, diagnostics
}
