package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestContinuationMetadataHookRejectsRawDivergenceWithTrustedDriver(t *testing.T) {
	for _, mode := range []string{"environment-external", "repository-external", "textconv"} {
		t.Run(mode, func(t *testing.T) {
			repo := continuationHookRepo(t)
			bin, calls := continuationHookBin(t, true)
			marker := filepath.Join(t.TempDir(), "driver-called")
			driver := filepath.Join(t.TempDir(), "driver")
			if err := execfile.Write(driver, []byte("#!/bin/sh\nprintf called > '"+marker+"'\nprintf 'constant presentation\\n'\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, ".git/info/attributes"), []byte("IMPLEMENTATION_PLAN.local.md diff=raw-audit\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			env := continuationHookEnv(bin, calls)
			switch mode {
			case "environment-external":
				env = append(env, "GIT_EXTERNAL_DIFF="+driver, "GIT_EXTERNAL_DIFF_TRUST_EXIT_CODE=1")
			case "repository-external":
				runContinuationHookGit(t, repo, "config", "diff.raw-audit.command", driver)
				runContinuationHookGit(t, repo, "config", "diff.raw-audit.trustExitCode", "true")
			case "textconv":
				runContinuationHookGit(t, repo, "config", "diff.raw-audit.textconv", driver)
			}
			plan := filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md")
			if err := os.WriteFile(plan, []byte("# Plan\nstaged metadata\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runContinuationHookGit(t, repo, "add", "IMPLEMENTATION_PLAN.local.md")
			if err := os.WriteFile(plan, []byte("# Plan\nunstaged metadata\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("sh", continuationHookPath(t, ".githooks", "pre-commit"))
			command.Dir = repo
			command.Env = env
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "differs from worktree") {
				t.Fatalf("hookがraw不一致を拒否していません: error=%v output=%s", err, output)
			}
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Fatalf("不一致metadataが後段guardへ渡りました: %v", err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("hookがdriverを起動しました: %v", err)
			}
			control := exec.Command("git", "-C", repo, "diff", "--ext-diff", "--textconv", "--exit-code", "--", "IMPLEMENTATION_PLAN.local.md")
			control.Env = env
			if output, err := control.CombinedOutput(); err != nil {
				t.Fatalf("trusted driver対照が変更を隠していません: %v %s", err, output)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("対照diffがdriverを起動していません: %v", err)
			}
		})
	}
}
