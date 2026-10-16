package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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

func TestPartialCompletionProcessDeathRecoversThroughCanonicalParentCLI(t *testing.T) {
	cfg, store, promoted, policy, candidate := promotedWorkflowFixture(t)
	t.Setenv("GLM_WORKER_HOME", filepath.Dir(cfg.StateBase))
	hooks := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "controller-pid")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nset -eu\nif [ \"$1\" = committed ]; then\nwhile read old new ref; do\nif [ \"$ref\" = " + quote(policy.LocalRef) + " ]; then kill -KILL \"$(cat " + quote(pidFile) + ")\"; fi\ndone\nfi\n"
	if err := os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runControllerActivationGit(t, cfg.RepoRoot, "config", "core.hooksPath", hooks)
	command := completionCLICommand(t, cfg, controllerPublicationSurface, controllerPublicationCommand{Action: "publish", ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	command.Env = append(command.Env, "GLM_COMPLETION_PID_FILE="+pidFile)
	if data, err := command.CombinedOutput(); err == nil {
		t.Fatalf("local effect process death did not interrupt CLI: %s", data)
	}
	runControllerActivationGit(t, cfg.RepoRoot, "config", "--unset", "core.hooksPath")
	head, err := store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("process death lost its journal: %v", err)
	}
	_, phase, err := store.LoadTransition(head.PendingTransitionID)
	if err != nil || phase.Completion != nil {
		t.Fatalf("local process died after publication intent rather than during preparation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.RepoRoot, candidate.TaskRef.TaskPath)); err != nil {
		t.Fatal("injected death occurred after final Metadata was applied")
	}
	before := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")))
	if before == candidate.CommitOID {
		t.Fatal("injected death occurred before the local ref effect")
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cfg.RepoRoot); err != nil {
		t.Fatal(err)
	}
	var bootstrap bytes.Buffer
	_, bootstrapErr := runAuthorityBootstrap([]string{"--authority", "bootstrap"}, &bootstrap)
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}
	if bootstrapErr == nil {
		t.Fatal("partial local preparation was reported as successful bootstrap")
	}
	recoverCompletionThroughParentCLI(t, cfg)
	recovered, err := store.LoadHead()
	if err != nil || recovered.PendingTransitionID != "" || recovered.ExecutionTaskRef != nil || recovered.IntegrationTip != before {
		t.Fatalf("CLI did not complete the exact saved journal: %v", err)
	}
	assertRetiredBootstrap(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md")
	actual := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef)
	if !strings.HasPrefix(string(actual), before+"\t") {
		t.Fatal("recovery did not publish the planned final commit")
	}
}

func installRetirementCLIProcess(t *testing.T) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nGLM_RETIREMENT_RECOVERY_TEST_PROCESS=1 exec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' -test.run '^TestRetirementRecoveryCLIProcess$' -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runStagedRetirementRecovery(t *testing.T, command string) {
	t.Helper()
	var out, diagnostics bytes.Buffer
	if code := parentactioncmd.Run([]string{"prepare", controllerExecutionSurface}, &out, &diagnostics); code != 0 {
		t.Fatalf("prepare recovery: exit=%d %s", code, diagnostics.String())
	}
	var prepared struct {
		Path        string                               `json:"path"`
		Slots       []struct{ Name, Placeholder string } `json:"slots"`
		NextCommand []string                             `json:"next_command"`
	}
	if err := json.Unmarshal(out.Bytes(), &prepared); err != nil || len(prepared.Slots) != 1 || prepared.Slots[0].Name != "controller_command_json" || len(prepared.NextCommand) < 2 {
		t.Fatalf("recovery preparation contract: %v", err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), prepared.Slots[0].Placeholder) != 1 {
		t.Fatal("recovery slot is not unique")
	}
	if err := os.WriteFile(prepared.Path, []byte(strings.Replace(string(data), prepared.Slots[0].Placeholder, command, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	diagnostics.Reset()
	if code := parentactioncmd.Run(prepared.NextCommand[1:], io.Discard, &diagnostics); code != 0 {
		t.Fatalf("canonical CLI recovery: exit=%d %s", code, diagnostics.String())
	}
}
