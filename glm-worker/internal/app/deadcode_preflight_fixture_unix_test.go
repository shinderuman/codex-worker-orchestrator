//go:build unix

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	realGo, err := exec.LookPath("go")
	if err != nil {
		panic(err)
	}
	wrapperDir, err := os.MkdirTemp("", "codex-worker-orchestrator-app-go-wrapper-")
	if err != nil {
		panic(err)
	}
	goWrapper := `#!/bin/sh
set -eu
if [ "${1:-}" = version ] && [ "${2:-}" = -m ]; then
	case "${3:-}" in
	*/codex-worker-orchestrator-deadcode-0.50.0)
		printf '\tmod\tgolang.org/x/tools\tv0.50.0\n'
		exit 0
		;;
	esac
fi
exec ` + "'" + strings.ReplaceAll(realGo, "'", "'\"'\"'") + "'" + ` "$@"
`
	if err := os.WriteFile(filepath.Join(wrapperDir, "go"), []byte(goWrapper), 0o755); err != nil {
		_ = os.RemoveAll(wrapperDir)
		panic(err)
	}
	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+originalPath); err != nil {
		_ = os.RemoveAll(wrapperDir)
		panic(err)
	}

	code := m.Run()
	_ = os.Setenv("PATH", originalPath)
	_ = os.RemoveAll(wrapperDir)
	os.Exit(code)
}

func TestGoPreflightNestedProcess(t *testing.T) {
	t.Parallel()
	if os.Getenv("GLM_GO_PREFLIGHT_NESTED_PROCESS") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := exec.CommandContext(ctx, "go", "version").Output(); err != nil {
		t.Fatal(err)
	}
}

func TestGoPreflightWrapperDelegatesAcrossProcesses(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run", "^TestGoPreflightNestedProcess$")
	command.Env = append(os.Environ(), "GLM_GO_PREFLIGHT_NESTED_PROCESS=1")
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("nested quality preflight command failed: %v %s", err, data)
	}
}
