package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
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
	installRuntimeManagedConfigs(t, cfg.RepoRoot, &cfg)
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
	buildDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range []string{"glm-parent-action", "glm-codex-context", "commentlint", "harnesslint"} {
		if err := os.WriteFile(filepath.Join(buildDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
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
	if err := os.WriteFile(filepath.Join(buildDir, "glm-worker"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := cliinstall.Install(buildDir, binDir); err != nil {
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
