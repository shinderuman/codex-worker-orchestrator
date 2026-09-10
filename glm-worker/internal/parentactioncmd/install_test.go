package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newInstallActionRepo(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg, st := newParentActionTestState(t)
	initInstallActionGitRepo(t, cfg.RepoRoot)
	writeRepositoryHarnessMarker(t, cfg.RepoRoot)
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, installScriptName), []byte("#!/bin/sh\n# baseline install fixture\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", cfg.RepoRoot, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add baseline: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", cfg.RepoRoot, "commit", "-q", "-m", "baseline").CombinedOutput(); err != nil {
		t.Fatalf("git commit baseline: %v: %s", err, output)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	writeInstallRuntimeProbeStub(t)
	return cfg, st
}

func writeRepositoryHarnessMarker(t *testing.T, repoRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, repositoryharness.MarkerPath), []byte(repositoryharness.MarkerContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repoRoot, "add", "--", repositoryharness.MarkerPath).CombinedOutput(); err != nil {
		t.Fatalf("git add harness marker: %v: %s", err, output)
	}
}

func initInstallActionGitRepo(t *testing.T, repoRoot string) {
	t.Helper()
	if output, err := exec.Command("git", "-C", repoRoot, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", repoRoot, "config", "user.email", "install-test@example.invalid").CombinedOutput(); err != nil {
		t.Fatalf("git config user.email: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", repoRoot, "config", "user.name", "install-test").CombinedOutput(); err != nil {
		t.Fatalf("git config user.name: %v: %s", err, output)
	}
}

func writeInstallActionScript(t *testing.T, repoRoot, body string, mode os.FileMode) {
	t.Helper()
	script := filepath.Join(repoRoot, installScriptName)
	if err := os.WriteFile(script, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, mode); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repoRoot, "add", "--", installScriptName).CombinedOutput(); err != nil {
		t.Fatalf("git add install.sh: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", repoRoot, "commit", "-q", "-m", "runtime change").CombinedOutput(); err != nil {
		t.Fatalf("git commit install.sh: %v: %s", err, output)
	}
}

func writeInstallRuntimeProbeStub(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	stub := `#!/bin/sh
case "${1:-}" in
--status)
  head=$(git -C "$PWD" rev-parse HEAD)
  printf '{"runtime_build":{"vcs_revision":"%s","vcs_modified":false,"repository_head":"%s","relationship":"same"}}\n' "$head" "$head"
  ;;
--install-smoke)
  printf '%s\n' '{"status":"executed","result":"pass","role":"parent","duration_ms":1}'
  ;;
*)
  exit 2
  ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "glm-worker"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runInstallAction(t *testing.T, cfg config.AppConfig, args ...string) (installOutput, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := execute(cfg, append([]string{"install"}, args...), &stdout, &stderr)
	var output installOutput
	if jsonErr := json.Unmarshal(stdout.Bytes(), &output); jsonErr != nil && err == nil {
		t.Fatalf("install stdout is not machine JSON: %v: %q", jsonErr, stdout.String())
	}
	return output, stderr.String(), err
}

func TestExecuteInstallRunsTrackedRepositoryInstallScript(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nprintf 'install child output\\n'\nexit 0\n", 0o755)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output, _, err := runInstallAction(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != installStatusInstalled || !output.Required || output.Failure != nil {
		t.Fatalf("first install output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("install mutated task status: %s", st.TaskStatus())
	}
	if _, err := st.LoadRuntimeInstallEvidence(); err != nil {
		t.Fatalf("runtime install evidence = %v", err)
	}

	repeated, _, err := runInstallAction(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Status != installStatusInstalled || !repeated.Required || repeated.Failure != nil {
		t.Fatalf("repeated install output = %#v", repeated)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("repeated install mutated task status: %s", st.TaskStatus())
	}
}

func TestExecuteInstallSkipsMetadataOnlyTask(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("metadata\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", cfg.RepoRoot, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add metadata: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", cfg.RepoRoot, "commit", "-q", "-m", "metadata").CombinedOutput(); err != nil {
		t.Fatalf("git commit metadata: %v: %s", err, output)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output, stderr, err := runInstallAction(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != installStatusNotRequired || output.Required || output.Failure != nil {
		t.Fatalf("metadata install output = %#v", output)
	}
	if stderr != "" {
		t.Fatalf("metadata install executed child output: %q", stderr)
	}
}

func TestExecuteInstallRejectsDirtyRuntimeSource(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, installScriptName), []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output, _, err := runInstallAction(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != installStatusGuardRejected || !output.Required || output.Failure == nil || output.Failure.Reason != runtimeInstallFailureDirty {
		t.Fatalf("dirty runtime output = %#v", output)
	}
}

func TestExecuteInstallIndependentOfCodexSessionIdentity(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CODEX_SESSION_ID", "")
	cfg, st := newInstallActionRepo(t)
	writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nexit 0\n", 0o755)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output, _, err := runInstallAction(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != installStatusInstalled {
		t.Fatalf("identity-free install output = %#v", output)
	}
}

func TestExecuteInstallRejectsExtraArguments(t *testing.T) {
	cfg, _ := newInstallActionRepo(t)
	writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nexit 0\n", 0o755)

	_, _, err := runInstallAction(t, cfg, "--force")
	if err == nil || !strings.Contains(err.Error(), "usage: glm-parent-action install") {
		t.Fatalf("extra argument error = %v", err)
	}
}

func TestExecuteInstallAdmissionRestrictedToAwaitingParentCompletion(t *testing.T) {
	cases := []struct {
		name   string
		status state.TaskStatus
	}{
		{name: "no task", status: state.TaskStatusNone},
		{name: "waiting decision", status: state.TaskStatusWaitingDecision},
		{name: "complete", status: state.TaskStatusComplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, st := newInstallActionRepo(t)
			writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nexit 0\n", 0o755)
			if tc.status == state.TaskStatusNone {
				if err := st.Remove("task.id", "task.status"); err != nil {
					t.Fatal(err)
				}
			} else if err := st.SetTaskStatus(tc.status); err != nil {
				t.Fatal(err)
			}

			output, _, err := runInstallAction(t, cfg)
			if err == nil {
				t.Fatalf("install was admitted for status %s: %#v", tc.status, output)
			}
			if output.Status == installStatusInstalled || output.Status == installStatusFailed {
				t.Fatalf("install executed for status %s: %#v", tc.status, output)
			}
		})
	}
}

func TestExecuteInstallRejectsForeignRepositoryWithSameLifecycleState(t *testing.T) {
	foreignRoot := t.TempDir()
	initInstallActionGitRepo(t, foreignRoot)
	executed := filepath.Join(foreignRoot, "foreign-install-executed")
	if err := os.WriteFile(filepath.Join(foreignRoot, installScriptName), []byte("#!/bin/sh\ntouch '"+executed+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", foreignRoot, "add", installScriptName).CombinedOutput(); err != nil {
		t.Fatalf("git add foreign install: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", foreignRoot, "commit", "-q", "-m", "foreign").CombinedOutput(); err != nil {
		t.Fatalf("git commit foreign install: %v: %s", err, output)
	}
	foreignCfg, _ := newParentActionTestState(t)
	foreignCfg.RepoRoot = foreignRoot
	foreignCfg.RepoHash = strings.Repeat("b", 64)
	foreignState, err := state.NewStateStore(foreignCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := foreignState.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output, _, err := runInstallAction(t, foreignCfg)
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != installStatusGuardRejected || output.Failure == nil || output.Failure.Reason != "repository_harness_inactive" {
		t.Fatalf("foreign repository output = %#v", output)
	}
	if _, statErr := os.Stat(executed); statErr == nil {
		t.Fatal("foreign repository install script was executed")
	}
	if foreignState.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("foreign rejection mutated task status: %s", foreignState.TaskStatus())
	}
}

func TestExecuteInstallBlocksUnsafeInstallScript(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		setup  func(t *testing.T, repoRoot string)
	}{
		{
			name:   "missing script",
			reason: "install_script_missing",
			setup: func(t *testing.T, repoRoot string) {
				if err := os.Remove(filepath.Join(repoRoot, installScriptName)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "symlink script",
			reason: "install_script_symlink",
			setup: func(t *testing.T, repoRoot string) {
				if err := os.Remove(filepath.Join(repoRoot, installScriptName)); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(repoRoot, "install-target.sh")
				if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(repoRoot, installScriptName)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "untracked script",
			reason: "install_script_untracked",
			setup: func(t *testing.T, repoRoot string) {
				if output, err := exec.Command("git", "-C", repoRoot, "rm", "-q", "--cached", installScriptName).CombinedOutput(); err != nil {
					t.Fatalf("git rm cached install.sh: %v: %s", err, output)
				}
			},
		},
		{
			name:   "not a regular file",
			reason: "install_script_not_regular",
			setup: func(t *testing.T, repoRoot string) {
				if err := os.Remove(filepath.Join(repoRoot, installScriptName)); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(repoRoot, installScriptName), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, st := newInstallActionRepo(t)
			tc.setup(t, cfg.RepoRoot)
			if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
				t.Fatal(err)
			}

			output, _, err := runInstallAction(t, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if output.Status != installStatusGuardRejected || output.Failure == nil || output.Failure.Reason != tc.reason {
				t.Fatalf("guard output = %#v want reason %s", output, tc.reason)
			}
			if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
				t.Fatalf("guard mutated task status: %s", st.TaskStatus())
			}
		})
	}
}

func TestExecuteInstallReturnsChildFailureWithoutTaskTransition(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nexit 3\n", 0o755)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output, _, err := runInstallAction(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != installStatusFailed || !output.Required || output.Failure == nil || output.Failure.Reason != "install_script_failed" || output.Failure.ExitCode != 3 {
		t.Fatalf("child failure output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("child failure mutated task status: %s", st.TaskStatus())
	}
	if _, err := st.LoadRuntimeInstallEvidence(); err == nil {
		t.Fatal("failed install retained completion evidence")
	}

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allows(state.ParentActionInstall) || !plan.Allows(state.ParentActionComplete) {
		t.Fatalf("post-failure plan = %#v", plan)
	}
}

func TestExecuteInstallReturnsStartFailureWithoutTaskTransition(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nexit 0\n", 0o644)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output, _, err := runInstallAction(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != installStatusFailed || !output.Required || output.Failure == nil || output.Failure.Reason != "install_script_start_failed" {
		t.Fatalf("start failure output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("start failure mutated task status: %s", st.TaskStatus())
	}
}
