package failurepathadvisory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestClassifyTriggerUsesRawGitChanges(t *testing.T) {
	for _, mode := range []string{"environment-external", "repository-external", "textconv"} {
		for _, staged := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/unstaged", true: "/staged"}[staged], func(t *testing.T) {
				path := "glm-worker/internal/runner/call.go"
				root, baseline := newTriggerRepo(t, map[string]string{path: "package runner\n"})
				marker := filepath.Join(t.TempDir(), "driver-called")
				driver := filepath.Join(t.TempDir(), "driver")
				body := "#!/bin/sh\nprintf called > '" + marker + "'\nprintf 'constant presentation\\n'\n"
				if err := execfile.Write(driver, []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
				writeTriggerFile(t, root, ".git/info/attributes", "*.go diff=raw-audit\n")
				switch mode {
				case "environment-external":
					t.Setenv("GIT_EXTERNAL_DIFF", driver)
				case "repository-external":
					runTriggerGit(t, root, "config", "diff.raw-audit.command", driver)
				case "textconv":
					runTriggerGit(t, root, "config", "diff.raw-audit.textconv", driver)
				}
				writeTriggerFile(t, root, path, "package runner\n\nvar callSite = newProcessGroupCmd\n")
				if staged {
					runTriggerGit(t, root, "add", path)
				}
				decision, err := ClassifyTrigger(root, baseline, []string{path})
				if err != nil {
					t.Fatal(err)
				}
				if !decision.Triggered || len(decision.Classes) != 1 || decision.Classes[0] != ClassExternalModelInvocation {
					t.Fatalf("raw変更が分類されません: %+v", decision)
				}
				patch, err := DiffTextForPaths(root, baseline, []string{path}, 4096)
				if err != nil || !strings.Contains(patch, "+var callSite = newProcessGroupCmd") {
					t.Fatalf("raw patch = %q, error = %v", patch, err)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("機械用diffがdriverを起動しました: %v", err)
				}
				control := runTriggerGit(t, root, "diff", "--ext-diff", "--textconv", baseline, "--", path)
				if mode == "textconv" && control != "" {
					t.Fatalf("定数textconvが変更を隠していません: %q", control)
				}
				if _, err := os.Stat(marker); err != nil {
					t.Fatalf("対照diffがdriverを起動していません: %v", err)
				}
			})
		}
	}
}
