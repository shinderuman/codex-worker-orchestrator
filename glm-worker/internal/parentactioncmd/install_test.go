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
	if output.Status != installStatusInstalled || output.Failure != nil {
		t.Fatalf("first install output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("install mutated task status: %s", st.TaskStatus())
	}

	repeated, _, err := runInstallAction(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Status != installStatusInstalled || repeated.Failure != nil {
		t.Fatalf("repeated install output = %#v", repeated)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("repeated install mutated task status: %s", st.TaskStatus())
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
			if tc.status != state.TaskStatusNone {
				if err := st.SetTaskStatus(tc.status); err != nil {
					t.Fatal(err)
				}
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
	writeInstallActionScript(t, foreignRoot, "#!/bin/sh\ntouch '"+executed+"'\nexit 0\n", 0o755)
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
			setup:  func(*testing.T, string) {},
		},
		{
			name:   "symlink script",
			reason: "install_script_symlink",
			setup: func(t *testing.T, repoRoot string) {
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
				if err := os.WriteFile(filepath.Join(repoRoot, installScriptName), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "not a regular file",
			reason: "install_script_not_regular",
			setup: func(t *testing.T, repoRoot string) {
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
	if output.Status != installStatusFailed || output.Failure == nil || output.Failure.Reason != "install_script_failed" || output.Failure.ExitCode != 3 {
		t.Fatalf("child failure output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("child failure mutated task status: %s", st.TaskStatus())
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
	if output.Status != installStatusFailed || output.Failure == nil || output.Failure.Reason != "install_script_start_failed" {
		t.Fatalf("start failure output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("start failure mutated task status: %s", st.TaskStatus())
	}
}
