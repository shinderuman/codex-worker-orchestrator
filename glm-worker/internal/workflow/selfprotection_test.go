package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsCriticalPath(t *testing.T) {
	cases := []struct {
		path     string
		critical bool
		category string
	}{
		{"glm-worker/internal/workflow/workflow.go", true, "workflow-package"},
		{"glm-worker/internal/autoresume/verify.go", true, "autoresume-package"},
		{"glm-worker/internal/harnesslint/run.go", true, "quality-policy"},
		{"glm-worker/cmd/harnesslint/main.go", true, "worker-entrypoint"},
		{"harnesslint", true, "quality-policy"},
		{".golangci.yml", true, "quality-policy"},
		{"quality-tools.yml", true, "quality-policy"},
		{".github/workflows/quality.yml", true, "quality-policy"},
		{"install-quality-tools.sh", true, "quality-policy"},
		{"IMPLEMENTATION_PLAN.local.md", true, "implementation-plan"},
		{"codex/glm-worker/prompts/WORKER.md", true, "managed-prompts"},
		{"glm-worker/internal/workflow/workflow_test.go", false, "test"},
		{"tests/install_smoke.sh", false, testHarnessPathCategory},
		{"README.md", false, "docs"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			critical, category := IsCriticalPath(tc.path)
			if critical != tc.critical || category != tc.category {
				t.Fatalf("got %v,%q want %v,%q", critical, category, tc.critical, tc.category)
			}
		})
	}
}

func TestIsQualitySurface(t *testing.T) {
	for _, path := range []string{
		".golangci.yml",
		"quality-tools.yml",
		".github/workflows/quality.yml",
		"install-quality-tools.sh",
		"harnesslint",
		"commentlint",
		"glm-worker/internal/harnesslint/run.go",
		"glm-worker/internal/commentlint/commentlint.go",
		"glm-worker/cmd/harnesslint/main.go",
	} {
		if !IsQualitySurface(path) {
			t.Fatalf("quality surface not protected: %s", path)
		}
	}
	if IsQualitySurface("glm-worker/internal/workflow/workflow.go") {
		t.Fatal("ordinary workflow implementation must not be classified as linter surface")
	}
}

func TestClassifySelfProtectionAggregatesCategories(t *testing.T) {
	decision := classifySelfProtection([]string{
		"README.md",
		"glm-worker/internal/workflow/workflow.go",
		"codex/glm-worker/prompts/WORKER.md",
	})
	if !decision.High || decision.Source != "managed-prompts,workflow-package" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestCollectChangedPaths(t *testing.T) {
	dir := t.TempDir()
	runGitTest(t, dir, "init")
	runGitTest(t, dir, "config", "user.email", "t@example.com")
	runGitTest(t, dir, "config", "user.name", "tester")
	writeGitTestFile(t, dir, "README.md", "base")
	runGitTest(t, dir, "add", ".")
	runGitTest(t, dir, "commit", "-m", "base")
	baseline := runGitTest(t, dir, "rev-parse", "HEAD")
	writeGitTestFile(t, dir, "README.md", "changed")
	writeGitTestFile(t, dir, "harnesslint", "new")
	paths, err := collectChangedPaths(dir, baseline)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		seen[path] = true
	}
	if !seen["README.md"] || !seen["harnesslint"] {
		t.Fatalf("paths = %v", paths)
	}
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeGitTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
