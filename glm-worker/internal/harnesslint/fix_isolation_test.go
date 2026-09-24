package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunWithIsolatedFixesAppliesVerifiedPostimage(t *testing.T) {
	root := newFixIsolationRepo(t)
	fixture := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(fixture, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAddFixIsolationRepo(t, root)

	report, err := runWithIsolatedFixes(root, func(workspace string) (Report, error) {
		if err := os.WriteFile(filepath.Join(workspace, "fixture.go"), []byte("after\n"), 0o644); err != nil {
			return Report{}, err
		}
		return Report{Status: "pass", Fixed: 1, Violations: []Violation{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.FixEvidence == nil || report.FixEvidence.Method != FixProvenanceIsolatedPostimageV1 || report.FixEvidence.Input == nil || report.FixEvidence.Output == nil {
		t.Fatalf("fix evidence = %#v", report.FixEvidence)
	}
	if report.FixEvidence.Input.WorktreeDigest == report.FixEvidence.Output.WorktreeDigest {
		t.Fatal("fix evidence input/output worktree digests did not change")
	}
	got, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "after\n" {
		t.Fatalf("postimage = %q", got)
	}
}

func TestVerifyFixInputSnapshotRejectsChangeBetweenSnapshotAndManifest(t *testing.T) {
	root := newFixIsolationRepo(t)
	fixture := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(fixture, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAddFixIsolationRepo(t, root)
	input, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := repositoryPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := captureFixManifest(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFixInputSnapshot(root, input, before); err == nil || !strings.Contains(err.Error(), "input changed during manifest capture") {
		t.Fatalf("snapshot-to-manifest race was not rejected: %v", err)
	}
}

func TestRunWithIsolatedFixesRejectsConcurrentLiveChange(t *testing.T) {
	root := newFixIsolationRepo(t)
	fixture := filepath.Join(root, "fixture.go")
	other := filepath.Join(root, "other.txt")
	if err := os.WriteFile(fixture, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("stable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAddFixIsolationRepo(t, root)

	_, err := runWithIsolatedFixes(root, func(workspace string) (Report, error) {
		if err := os.WriteFile(filepath.Join(workspace, "fixture.go"), []byte("after\n"), 0o644); err != nil {
			return Report{}, err
		}
		if err := os.WriteFile(other, []byte("external\n"), 0o644); err != nil {
			return Report{}, err
		}
		return Report{Status: "pass", Fixed: 1, Violations: []Violation{}}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "input changed while isolated fixer ran") {
		t.Fatalf("concurrent live change was not rejected: %v", err)
	}
	gotFixture, readErr := os.ReadFile(fixture)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotFixture) != "before\n" {
		t.Fatalf("unverified fixer postimage reached live tree: %q", gotFixture)
	}
	gotOther, readErr := os.ReadFile(other)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotOther) != "external\n" {
		t.Fatalf("external change was overwritten: %q", gotOther)
	}
}

func newFixIsolationRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "-C", root, "init", "-q")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return root
}

func gitAddFixIsolationRepo(t *testing.T, root string) {
	t.Helper()
	command := exec.Command("git", "-C", root, "add", "-f", "--all")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
}
