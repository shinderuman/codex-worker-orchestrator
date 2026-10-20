package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/publicationguard"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/publicationguardcmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCompletionGuardCLIProcess(_ *testing.T) {
	if os.Getenv("GLM_COMPLETION_GUARD_TEST_PROCESS") != "1" {
		return
	}
	for index, arg := range os.Args {
		if arg == "--" {
			if err := publicationguardcmd.Run(os.Args[index+1:], os.Stdin, os.Stdout); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func completionCLICommand(t *testing.T, cfg config.AppConfig, surface string, input any) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run", "^TestRetirementRecoveryCLIProcess$", "--", "--authority", surface)
	command.Dir = cfg.RepoRoot
	home := filepath.Dir(cfg.StateBase)
	command.Env = append(append(os.Environ(), "GLM_RETIREMENT_RECOVERY_TEST_PROCESS=1", "GLM_WORKER_HOME="+home, "HOME="+home, "CODEX_CONFIG_DIR="+filepath.Join(home, "codex"), "CLAUDE_CONFIG_DIR="+filepath.Join(home, "claude")), qualityToolsBinDirEnv(t)...)
	command.Stdin = bytes.NewReader(raw)
	return command
}

func installCompletionGuard(t *testing.T, cfg config.AppConfig) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hooks := t.TempDir()
	guard := filepath.Join(hooks, "glm-publication-guard")
	script := "#!/bin/sh\nGLM_COMPLETION_GUARD_TEST_PROCESS=1 exec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' -test.run '^TestCompletionGuardCLIProcess$' -- \"$@\"\n"
	if err := os.WriteFile(guard, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pre-push", "reference-transaction"} {
		data, err := exec.Command("git", "-C", cfg.RepoRoot, "show", "HEAD:.githooks/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooks, name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	head := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")))
	if err := os.WriteFile(filepath.Join(hooks, "glm-publication-guard.path"), []byte(guard+"\nsnapshot="+head+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runControllerActivationGit(t, cfg.RepoRoot, "config", "core.hooksPath", hooks)
	report, err := publicationguard.InspectPublicationGuardSetup(cfg.RepoRoot)
	if err != nil || len(report.Defects) != 0 {
		t.Fatalf("canonical Git Guard setup: %v %#v", err, report.Defects)
	}
}

func appTestGitOutput(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	data, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCompletionCLIRecoversProcessDeathAndPublishedACKWithCanonicalGitGuard(t *testing.T) {
	cfg, store, promoted, policy, candidate := promotedWorkflowFixture(t)
	planned := interruptCompletionBeforePush(t, cfg, store, promoted, policy, candidate)
	installCompletionGuard(t, cfg)
	direct := exec.Command("git", "-C", cfg.RepoRoot, "push", policy.Remote, policy.LocalRef+":"+policy.RemoteRef)
	if data, err := direct.CombinedOutput(); err == nil || !strings.Contains(string(data), "push rejected") {
		t.Fatalf("unfinished Task direct Push was not rejected by canonical Guard: %v %s", err, data)
	}
	remote := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "remote", "get-url", policy.Remote)))
	storeDir := filepath.Join(filepath.Dir(cfg.StateBase), "controllers", store.Identity().LineageID)
	log := filepath.Join(t.TempDir(), "push-count")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	hook := "#!/bin/sh\nset -eu\nprintf x >> " + quote(log) + "\nchmod 0500 " + quote(storeDir) + "\n"
	if err := os.WriteFile(filepath.Join(remote, "hooks", "post-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(storeDir, 0o700) })
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	transitionID := head.PendingTransitionID
	exit, interruption := runCompletionRecoveryThroughParentCLI(t, cfg)
	if exit == 0 {
		t.Fatalf("ACK write fault did not interrupt CLI: %s", interruption)
	}
	head, err = store.LoadHead()
	if err != nil || head.PendingTransitionID != transitionID || head.MetadataLineageRef != nil {
		t.Fatalf("ACK fault completed or lost authority: %v; CLI=%v %s", err, exit, interruption)
	}
	_, phase, err := store.LoadTransition(head.PendingTransitionID)
	if err != nil || phase.Completion == nil || phase.Completion.ObservationRef == nil {
		t.Fatalf("CLI push did not save remote observation: %v; CLI=%v %s", err, exit, interruption)
	}
	before, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	final := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")))
	if final != planned {
		t.Fatal("ACK recovery changed the commit planned before local process death")
	}
	if data := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef); !strings.HasPrefix(string(data), final+"\t") {
		t.Fatal("ACK fault occurred before successful real Push")
	}
	if err := os.Chmod(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if exit, diagnostics := runCompletionRecoveryThroughParentCLI(t, cfg); exit != 0 {
		t.Fatalf("canonical CLI recovery: exit=%d %s", exit, diagnostics)
	}
	after, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil || after != before {
		t.Fatalf("journal recovery changed source, Metadata, index or commit: %v", err)
	}
	recovered, err := store.LoadHead()
	if err != nil || recovered.PendingTransitionID != "" || recovered.ExecutionTaskRef != nil || recovered.IntegrationTip != final || recovered.MetadataLineageRef == nil {
		t.Fatalf("CLI journal replay did not finalize authority: %v", err)
	}
	pushes, err := os.ReadFile(log)
	if err != nil || string(pushes) != "x" {
		t.Fatalf("CLI ACK recovery repeated Push: %q %v", pushes, err)
	}
	assertRetiredBootstrap(t, cfg, "IMPLEMENTATION_TASKS/successor.md")
}

func runCompletionRecoveryThroughParentCLI(t *testing.T, cfg config.AppConfig) (int, string) {
	t.Helper()
	handoff := buildParentHandoffWithConfig(cfg, state.AttachStateStore(cfg))
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface || containsParentAction(handoff.AllowedActions, "start") {
		t.Fatalf("ACK recovery canonical handoff: %#v", handoff)
	}
	return runStagedRetirementRecovery(t, cfg, handoff.RequiredActionParameters["controller_command_json"])
}
