package runner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func writeGoProxyFixture(t *testing.T) (string, string) {
	t.Helper()
	realGo := filepath.Join(t.TempDir(), "real-go")
	argvLog := filepath.Join(t.TempDir(), "argv")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >>\"" + argvLog + "\"\n"
	if err := execfile.Write(realGo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	proxyPath := filepath.Join(t.TempDir(), "go")
	if err := execfile.Write(proxyPath, []byte(goTestProxyScript(realGo)), 0o700); err != nil {
		t.Fatal(err)
	}
	return proxyPath, argvLog
}

func runGoProxyCommand(t *testing.T, proxyPath string, args ...string) (int, string) {
	t.Helper()
	output, runErr := exec.Command(proxyPath, args...).CombinedOutput()
	code := 0
	var exit *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exit) {
		t.Fatalf("go proxy起動失敗: %v: %s", runErr, output)
	}
	if exit != nil {
		code = exit.ExitCode()
	}
	return code, string(output)
}

func TestGoTestProxyDeniesNativeTestSubcommand(t *testing.T) {
	t.Parallel()
	proxyPath, argvLog := writeGoProxyFixture(t)
	cases := map[string][]string{
		"plain":      {"test", "./..."},
		"with-dir":   {"-C", "glm-worker", "test", "-race", "./..."},
		"focused":    {"test", "-run", "^TestX$", "./internal/app"},
		"json-count": {"test", "-json", "-count=1", "./..."},
	}
	for name, args := range cases {
		code, output := runGoProxyCommand(t, proxyPath, args...)
		if code != 97 || !strings.Contains(output, "native_go_test_blocked") {
			t.Fatalf("%s: deny = code %d output %q", name, code, output)
		}
	}
	if data, err := os.ReadFile(argvLog); err == nil && len(data) > 0 {
		t.Fatalf("拒否したgo testが実際のtoolchainへ到達しました: %s", data)
	}
}

func TestGoTestProxyPassesNonTestSubcommandsThrough(t *testing.T) {
	t.Parallel()
	proxyPath, argvLog := writeGoProxyFixture(t)
	cases := map[string][]string{
		"env":        {"env", "GOVERSION"},
		"vet":        {"vet", "./..."},
		"build":      {"build", "./..."},
		"run":        {"run", "./cmd/goquality"},
		"version":    {"version"},
		"dir-vet":    {"-C", "glm-worker", "vet", "./..."},
		"tool-flags": {"test2json"},
	}
	for name, args := range cases {
		code, output := runGoProxyCommand(t, proxyPath, args...)
		if code != 0 {
			t.Fatalf("%s: passthrough = code %d output %q", name, code, output)
		}
	}
	if data, err := os.ReadFile(argvLog); err != nil || len(data) == 0 {
		t.Fatalf("許可したsubcommandが実際のtoolchainへ到達しません: %v", err)
	}
}

func TestResolveDockerDaemonSocketPrefersLocalUnixEndpoint(t *testing.T) {
	cases := []struct {
		name       string
		dockerHost string
		fakeDocker string
		want       string
	}{
		{name: "docker-host-unix", dockerHost: "unix:///var/run/docker.sock", want: "/var/run/docker.sock"},
		{name: "docker-host-user-socket", dockerHost: "unix:///Users/worker/.docker/run/docker.sock", want: "/Users/worker/.docker/run/docker.sock"},
		{name: "docker-host-tcp", dockerHost: "tcp://127.0.0.1:2375", want: ""},
		{name: "context-unix", fakeDocker: "unix:///Users/worker/.docker/run/docker.sock\n", want: "/Users/worker/.docker/run/docker.sock"},
		{name: "context-remote-ssh", fakeDocker: "ssh://remote\n", want: ""},
		{name: "docker-missing", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DOCKER_HOST", tc.dockerHost)
			bin := t.TempDir()
			if tc.fakeDocker != "" {
				fake := "#!/bin/sh\nprintf '%s' '" + strings.TrimRight(tc.fakeDocker, "\n") + "'\n"
				if err := execfile.Write(filepath.Join(bin, "docker"), []byte(fake), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)
			if got := resolveDockerDaemonSocket(); got != tc.want {
				t.Fatalf("socket = %q want %q", got, tc.want)
			}
		})
	}
}
