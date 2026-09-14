//go:build unix

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
	if err := os.Symlink(realGo, filepath.Join(wrapperDir, "real-go")); err != nil {
		_ = os.RemoveAll(wrapperDir)
		panic(err)
	}
	goWrapper := `#!/bin/sh
set -eu
if [ "${1:-}" = version ] && [ "${2:-}" = -m ]; then
	case "${3:-}" in
	*codex-worker-orchestrator-deadcode-*)
		version=${3##*-}
		printf '\tmod\tgolang.org/x/tools\tv%s\n' "$version"
		exit 0
		;;
	esac
fi
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$dir/real-go" "$@"
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
