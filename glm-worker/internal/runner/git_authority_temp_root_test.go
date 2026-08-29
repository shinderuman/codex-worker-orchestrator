package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitAuthorityClaudeWrapperOwnsProcessTempRoot(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	protected := t.TempDir()
	if output, err := exec.Command(realGit, "init", protected).CombinedOutput(); err != nil {
		t.Fatalf("git init protected: %v: %s", err, output)
	}

	guard, err := prepareGitAuthorityGuard(protected)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	if !guard.before.active {
		t.Fatal("git authority guard must be active")
	}

	external := t.TempDir()
	probe := filepath.Join(t.TempDir(), "probe.sh")
	script := `#!/bin/sh
set -eu
[ "$CLAUDE_CODE_TMPDIR" = "$GLM_WORKER_GIT_TEMP_ROOT" ] || exit 19
case "$TMPDIR" in
  "$GLM_WORKER_GIT_TEMP_ROOT"|"$GLM_WORKER_GIT_TEMP_ROOT"/*) ;;
  *) exit 20 ;;
esac
owned=$(mktemp -d)
git init "$owned/repo" >/dev/null 2>&1
sandbox_tmp="$GLM_WORKER_GIT_TEMP_ROOT/claude-test"
mkdir -p "$sandbox_tmp"
TMPDIR="$sandbox_tmp" git init "$sandbox_tmp/repo" >/dev/null 2>&1
if git init "$1/external-repo" >/dev/null 2>&1; then
  exit 21
else
  code=$?
  [ "$code" -eq 97 ] || exit 22
fi
if git -C "$2" branch forbidden >/dev/null 2>&1; then
  exit 23
else
  code=$?
  [ "$code" -eq 97 ] || exit 24
fi
`
	if err := os.WriteFile(probe, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	wrapped, err := guard.prepareClaudeWrapper(probe)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(wrapped, external, protected)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("wrapped probe failed: %v: %s", err, output)
	}
}
