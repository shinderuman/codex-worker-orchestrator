package runner

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestGitAuthorityDockerBoundaryKeepsWorkerDockerReachDeniedLive(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Docker test backend契約のlive境界検証はDarwin ARM64 host向け")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	guard, err := prepareGitAuthorityGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	if !guard.dockerPolicy {
		t.Fatal("Docker backend契約が解決されていません")
	}
	t.Run("wrapper-env", func(t *testing.T) {
		probeWrapperEnvironment(t, guard)
	})
	t.Run("managed-sandbox-dial", func(t *testing.T) {
		probeManagedSandboxUnixSocketDial(t, guard)
	})
}

func probeWrapperEnvironment(t *testing.T, guard *gitAuthorityGuard) {
	t.Helper()
	observations := filepath.Join(t.TempDir(), "observations")
	probe := filepath.Join(t.TempDir(), "probe.sh")
	if err := execfile.Write(probe, []byte(gitAuthorityDockerBoundaryProbeScript()), 0o700); err != nil {
		t.Fatal(err)
	}
	wrapped, err := guard.prepareClaudeWrapper(probe)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(wrapped, observations, t.TempDir())
	command.Env = append(os.Environ(), "DOCKER_HOST=unix:///boundary-live-check.sock")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapped probeが失敗しました: %v: %s", err, output)
	}
	data, err := os.ReadFile(observations)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"docker_host=unset",
		"docker_cli_exit=97",
		"go_test_exit=97",
		"go_dash_dash_c_exit=97",
		"staging_env=present",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("live観測 %q が得られません: %s", want, text)
		}
	}
	attempts, err := os.ReadFile(guard.attemptLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(attempts), "docker version") {
		t.Fatalf("docker CLI拒否が機械記録に残っていません: %q", string(attempts))
	}
}

func probeManagedSandboxUnixSocketDial(t *testing.T, guard *gitAuthorityGuard) {
	t.Helper()
	claudeBin := claudeLiveBinaryPath(t)
	socketPath := resolveLiveDockerSocket(t)
	wrapped, err := guard.prepareClaudeWrapper(claudeBin)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := isolationSettings(t.TempDir(), guard.bashSandboxPolicy())
	if err != nil {
		t.Fatal(err)
	}
	evidence := "live-sandbox-dial.txt"
	dialCommand := "{ echo bash=ok\n" +
		"python3 -c 'import socket,sys\n" +
		"try:\n" +
		"    socket.socket(socket.AF_UNIX).connect(sys.argv[1])\n" +
		"    print(\"dial=connected\")\n" +
		"except OSError as err:\n" +
		"    print(\"dial=denied\", type(err).__name__, err)\n" +
		"' " + shellSingleQuote(socketPath) + "\n} >\"$GLM_WORKER_GIT_TEMP_ROOT/" + evidence + "\" 2>&1"

	fixture := startClaudeLocalAPIFixture(t, dialCommand)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	claudeCommand := exec.CommandContext(ctx, wrapped, "-p", "--safe-mode", "--setting-sources", "",
		"--model", "claude-sonnet-4-5", "--dangerously-skip-permissions", "--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`, "--disable-slash-commands", "--settings", settings,
		"--tools", "Bash", "--", "Docker daemon socketへ直接dialしてください。")
	claudeCommand.Env = append(os.Environ(),
		"CLAUDE_CONFIG_DIR="+t.TempDir(),
		"ANTHROPIC_BASE_URL="+fixture.url,
		"ANTHROPIC_AUTH_TOKEN=fixture-local-token",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	)
	outputBytes, runErr := claudeCommand.CombinedOutput()
	output := string(outputBytes)
	if runErr != nil {
		t.Fatalf("claude fixture実行が失敗しました: %v: %s", runErr, output)
	}
	data, err := os.ReadFile(filepath.Join(guard.workerTempDir, evidence))
	if err != nil {
		t.Fatalf("sandbox Bash成果物がありません(claude出力: %s): %v", output, err)
	}
	text := string(data)
	if !strings.Contains(text, "bash=ok") {
		t.Fatalf("managed sandbox内でBashが実行されていません: %s\nclaude出力: %s", text, output)
	}
	if !strings.Contains(text, "dial=denied") {
		t.Fatalf("生Unix socket dialが拒否されませんでした: %s", text)
	}
	t.Logf("managed sandbox dial evidence socket=%s result=%s", socketPath, strings.TrimSpace(text))
}

func claudeLiveBinaryPath(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("claude"); err == nil {
		return path
	}
	if _, err := os.Stat("/opt/homebrew/bin/claude"); err == nil {
		return "/opt/homebrew/bin/claude"
	}
	t.Skip("claude CLIが利用できないためmanaged sandbox検証をskipします")
	return ""
}

func resolveLiveDockerSocket(t *testing.T) string {
	t.Helper()
	candidates := make([]string, 0, 3)
	if output, err := exec.Command("docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output(); err == nil {
		if endpoint := strings.TrimSpace(string(output)); strings.HasPrefix(endpoint, "unix://") {
			candidates = append(candidates, strings.TrimPrefix(endpoint, "unix://"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".docker", "run", "docker.sock"))
	}
	candidates = append(candidates, "/var/run/docker.sock")
	for _, candidate := range candidates {
		connection, err := net.DialTimeout("unix", candidate, 2*time.Second)
		if err == nil {
			_ = connection.Close()
			return candidate
		}
	}
	t.Skip("Docker daemonのUnix socketに接続できません(Owner正caseと同じsocketの生存確認が不能)")
	return ""
}

func gitAuthorityDockerBoundaryProbeScript() string {
	return `#!/bin/sh
set -u
out=$1
scratch=$2
{
  if [ "${DOCKER_HOST+set}" = set ]; then
    printf '%s\n' "docker_host=present"
  else
    printf '%s\n' "docker_host=unset"
  fi
  docker version >/dev/null 2>&1
  printf '%s\n' "docker_cli_exit=$?"
  (cd "$scratch" && go test ./... >/dev/null 2>&1)
  printf '%s\n' "go_test_exit=$?"
  (cd "$scratch" && go --C . test ./... >/dev/null 2>&1)
  printf '%s\n' "go_dash_dash_c_exit=$?"
  if [ -n "${GLM_WORKER_DOCKER_TEST_STAGING:-}" ] && [ -n "${GLM_WORKER_DOCKER_TEST_RESPONSES:-}" ]; then
    printf '%s\n' "staging_env=present"
  else
    printf '%s\n' "staging_env=absent"
  fi
} >"$out" 2>&1
`
}
