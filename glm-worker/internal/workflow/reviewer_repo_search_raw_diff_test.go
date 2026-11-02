package workflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
)

func TestReviewerDiffFirstUsesRawImpactTerms(t *testing.T) {
	for _, mode := range []string{"environment-external", "repository-external", "textconv"} {
		t.Run(mode, func(t *testing.T) {
			path := "glm-worker/internal/workflow/review.go"
			w, st, _ := newReviewerSearchWorkflow(t, []string{path})
			root := w.config.RepoRoot
			gitScope(t, root, "init")
			gitScope(t, root, "config", "user.email", "review@example.invalid")
			gitScope(t, root, "config", "user.name", "review")
			writeScopeFile(t, root, path, "package workflow\nvar baselineImpactNeedle = 1\n")
			gitScope(t, root, "add", ".")
			gitScope(t, root, "commit", "-m", "baseline")
			baseline, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Write("baseline-head", strings.TrimSpace(string(baseline))); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "driver-called")
			driver := filepath.Join(t.TempDir(), "driver")
			if err := execfile.Write(driver, []byte("#!/bin/sh\nprintf called > '"+marker+"'\nprintf 'constant presentation\\n'\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeScopeFile(t, root, ".git/info/attributes", "*.go diff=raw-audit\n")
			switch mode {
			case "environment-external":
				t.Setenv("GIT_EXTERNAL_DIFF", driver)
			case "repository-external":
				gitScope(t, root, "config", "diff.raw-audit.command", driver)
			case "textconv":
				gitScope(t, root, "config", "diff.raw-audit.textconv", driver)
			}
			writeScopeFile(t, root, path, "package workflow\nvar changedImpactNeedle = 2\n")
			calls := 0
			w.repoSearch = func(_ context.Context, _ string, query string, _ reposearch.Options) (reposearch.Report, error) {
				calls++
				if !strings.Contains(query, "changedImpactNeedle") || !strings.Contains(query, "baselineImpactNeedle") {
					t.Fatalf("実変更のimpact termがありません: %q", query)
				}
				return reposearch.Report{}, nil
			}
			w.reviewerDiffFirstContext("review routing", 1)
			if calls != 1 {
				t.Fatalf("独立search呼出数 = %d", calls)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("impact抽出がdriverを起動しました: %v", err)
			}
			control, err := exec.Command("git", "-C", root, "diff", "--ext-diff", "--textconv", "HEAD", "--", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "textconv" && len(control) != 0 {
				t.Fatalf("定数textconvが変更を隠していません: %q", control)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("対照diffがdriverを起動していません: %v", err)
			}
		})
	}
}
