package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardRepairEnforcementDoesNotRequireAuthoritySnapshot(t *testing.T) {
	repoRoot := t.TempDir()
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git.log")
	gitPath := filepath.Join(binDir, "git")
	script := `#!/bin/sh
printf '%s\n' "$*" >>"$FAKE_GIT_LOG"
case " $* " in
  *" rev-parse --show-toplevel "*) printf '%s\n' "$FAKE_REPO_ROOT" ;;
  *" rev-parse --verify HEAD "*) printf '%s\n' 0123456789012345678901234567890123456789 ;;
  *" symbolic-ref -q HEAD "*) printf '%s\n' refs/heads/main ;;
  *" for-each-ref "*) exit 91 ;;
  *" rev-parse --absolute-git-dir "*) printf '%s\n' "$FAKE_GIT_DIR" ;;
  *" rev-parse --path-format=absolute --git-common-dir "*) printf '%s\n' "$FAKE_GIT_DIR" ;;
  *" ls-files -s -z "*) exit 0 ;;
  *" config --local --null --list "*) exit 0 ;;
  *) exit 92 ;;
esac
`
	if err := os.WriteFile(gitPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("FAKE_GIT_LOG", logPath)
	t.Setenv("FAKE_REPO_ROOT", repoRoot)
	t.Setenv("FAKE_GIT_DIR", gitDir)

	if guard, err := prepareGitAuthorityGuard(repoRoot); err == nil {
		guard.cleanup()
		t.Fatal("normal authority snapshot unexpectedly succeeded")
	}
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	guard, err := prepareGuardRepairGitEnforcement(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "for-each-ref") {
		t.Fatalf("repair enforcement reused the failing authority snapshot: %s", calls)
	}

	policy := guardRepairSandboxPolicy(guard, repoRoot)
	for _, path := range []string{
		gitDir,
		filepath.Join(repoRoot, state.ParentRulesFile),
		filepath.Join(repoRoot, state.ParentPlanFile),
		filepath.Join(repoRoot, state.ParentTasksDir),
		filepath.Join(repoRoot, state.ParentHistoryFile),
	} {
		if !repairPolicyDenies(policy, path) {
			t.Fatalf("repair sandbox does not deny %s", path)
		}
	}
}

func repairPolicyDenies(policy *gitBashSandboxPolicy, path string) bool {
	want := filepath.Clean(path)
	for _, denied := range policy.denyWrite {
		if filepath.Clean(denied) == want {
			return true
		}
	}
	return false
}
