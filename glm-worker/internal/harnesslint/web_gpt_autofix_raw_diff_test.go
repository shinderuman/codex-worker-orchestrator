package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestWebGPTAutofixUsesRawPatchWithPresentationDrivers(t *testing.T) {
	for _, mode := range []string{"environment-external", "repository-external", "textconv"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newWebGPTAutofixFixture(t)
			marker := filepath.Join(fixture.base, "driver-called")
			driver := filepath.Join(fixture.base, "driver")
			if err := execfile.Write(driver, []byte("#!/bin/sh\nprintf called > '"+marker+"'\nprintf 'constant presentation\\n'\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeWebGPTAutofixFile(t, fixture.root, ".git/info/attributes", "fixture.txt diff=raw-audit\n", 0o644)
			switch mode {
			case "environment-external":
				t.Setenv("GIT_EXTERNAL_DIFF", driver)
				t.Setenv("GIT_EXTERNAL_DIFF_TRUST_EXIT_CODE", "1")
			case "repository-external":
				runWebGPTAutofixGit(t, fixture.root, "config", "diff.raw-audit.command", driver)
				runWebGPTAutofixGit(t, fixture.root, "config", "diff.raw-audit.trustExitCode", "true")
			case "textconv":
				runWebGPTAutofixGit(t, fixture.root, "config", "diff.raw-audit.textconv", driver)
			}
			prepared, patchPath, err := fixture.prepare(t, "fixable", fixture.branch, fixture.expected)
			if err != nil || !prepared.Changed || prepared.Publication != "prepared" || prepared.Validation != "pass" {
				t.Fatalf("prepare = %+v error=%v", prepared, err)
			}
			patch, err := os.ReadFile(patchPath)
			if err != nil || !strings.Contains(string(patch), "-bad\n+fixed\n") {
				t.Fatalf("実bytesのpatchがありません: %q error=%v", patch, err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("prepareがdriverを起動しました: %v", err)
			}
			fixture.resetForPublish(t)
			runWebGPTAutofixGit(t, fixture.root, "apply", "--index", patchPath)
			control := exec.Command("git", "-C", fixture.root, "diff", "--cached", "--ext-diff", "--textconv", "--exit-code", "--", "fixture.txt")
			if output, err := control.CombinedOutput(); err != nil {
				t.Fatalf("trusted driver対照がstaged変更を隠していません: %v %s", err, output)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("対照diffがdriverを起動していません: %v", err)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			published, err := fixture.publish(t, fixture.branch, fixture.expected, patchPath)
			if err != nil || !published.Changed || published.Publication != "pushed" || published.ResultingHead == fixture.expected {
				t.Fatalf("publish = %+v error=%v", published, err)
			}
			if got := runWebGPTAutofixGit(t, fixture.root, "show", "HEAD:fixture.txt"); got != "fixed" {
				t.Fatalf("published bytes = %q", got)
			}
			fixture.requireRemoteHead(t, published.ResultingHead)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("publishがdriverを起動しました: %v", err)
			}
		})
	}
}
