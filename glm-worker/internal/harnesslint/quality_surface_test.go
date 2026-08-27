package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDirtyQualitySurfaceRejectsWorkerModification(t *testing.T) {
	root := t.TempDir()
	runQualityGit(t, root, "init")
	runQualityGit(t, root, "config", "user.email", "t@example.com")
	runQualityGit(t, root, "config", "user.name", "tester")
	writeQualityFile(t, root, ".golangci.yml", "version: one\n")
	runQualityGit(t, root, "add", ".golangci.yml")
	runQualityGit(t, root, "commit", "-m", "base")
	writeQualityFile(t, root, ".golangci.yml", "version: two\n")
	violations, err := dirtyQualitySurface(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "quality-surface-dirty" || violations[0].Path != ".golangci.yml" {
		t.Fatalf("violations = %+v", violations)
	}
}

func TestDirtyQualitySurfaceAllowsCommittedBaseline(t *testing.T) {
	root := t.TempDir()
	runQualityGit(t, root, "init")
	runQualityGit(t, root, "config", "user.email", "t@example.com")
	runQualityGit(t, root, "config", "user.name", "tester")
	writeQualityFile(t, root, "harnesslint", "#!/bin/sh\n")
	runQualityGit(t, root, "add", "harnesslint")
	runQualityGit(t, root, "commit", "-m", "base")
	violations, err := dirtyQualitySurface(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %+v", violations)
	}
}

func TestQualityWiringRequiresReviewerGate(t *testing.T) {
	root := t.TempDir()
	path := "glm-worker/internal/workflow/workflow.go"
	writeQualityFile(t, root, path, "package workflow\n")
	violations, err := qualityWiringViolations(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 6 {
		t.Fatalf("violations = %+v", violations)
	}
}

func runQualityGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeQualityFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
