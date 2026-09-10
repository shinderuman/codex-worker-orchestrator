package repositoryharness

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, root string) {
	t.Helper()
	run := func(args ...string) {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
}

func writeMarker(t *testing.T, root string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, MarkerPath), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func trackMarker(t *testing.T, root string) {
	t.Helper()
	writeMarker(t, root, MarkerContent)
	command := exec.Command("git", "-C", root, "add", "--", MarkerPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
}

func TestEvaluateAbsentMarkerIsInactive(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	decision, err := Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Active || decision.Reason != ReasonAbsent {
		t.Fatalf("decision = %+v want inactive absent", decision)
	}
}

func TestEvaluateExactTrackedMarkerIsActive(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	trackMarker(t, root)

	decision, err := Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Active {
		t.Fatalf("decision = %+v want active", decision)
	}
}

func TestEvaluateRejectsInvalidMarkerForms(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		setup  func(t *testing.T, root string)
	}{
		{"wrong-content", ReasonContentMismatch, func(t *testing.T, root string) {
			writeMarker(t, root, "github.com/other/harness/v9\n")
			gitAddMarker(t, root)
		}},
		{"missing-newline", ReasonContentMismatch, func(t *testing.T, root string) {
			writeMarker(t, root, "github.com/shinderuman/codex-worker-orchestrator/repository-harness/v1")
			gitAddMarker(t, root)
		}},
		{"untracked", ReasonUntracked, func(t *testing.T, root string) {
			writeMarker(t, root, MarkerContent)
		}},
		{"symlink", ReasonNotRegularFile, func(t *testing.T, root string) {
			target := filepath.Join(root, "target")
			if err := os.WriteFile(target, []byte(MarkerContent), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("target", filepath.Join(root, MarkerPath)); err != nil {
				t.Fatal(err)
			}
			gitAddMarker(t, root)
		}},
		{"directory", ReasonNotRegularFile, func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, MarkerPath), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			gitInit(t, root)
			tc.setup(t, root)

			decision, err := Evaluate(root)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Active || decision.Reason != tc.reason {
				t.Fatalf("decision = %+v want inactive %s", decision, tc.reason)
			}
		})
	}
}

func gitAddMarker(t *testing.T, root string) {
	t.Helper()
	command := exec.Command("git", "-C", root, "add", "--", MarkerPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
}

func TestEvaluateOutsideGitWorktreeIsInactive(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, MarkerContent)

	decision, err := Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Active || decision.Reason != ReasonUntracked {
		t.Fatalf("decision = %+v want inactive untracked", decision)
	}
}

func TestEvaluateEmptyRootIsInactive(t *testing.T) {
	decision, err := Evaluate("")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Active || decision.Reason != ReasonAbsent {
		t.Fatalf("decision = %+v want inactive absent", decision)
	}
}

func TestQualityToolsApplyRequiresMarkerAndModule(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	trackMarker(t, root)

	applies, err := QualityToolsApply(root)
	var scopeErr *QualityScopeError
	if applies || !errors.As(err, &scopeErr) || scopeErr.Reason != QualityScopeModuleMissing {
		t.Fatalf("missing module result = applies:%v err:%v", applies, err)
	}

	goModDir := filepath.Join(root, filepath.FromSlash("glm-worker"))
	if err := os.MkdirAll(goModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goMod := "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n\ngo 1.24\n"
	if err := os.WriteFile(filepath.Join(goModDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	applies, err = QualityToolsApply(root)
	if err != nil {
		t.Fatal(err)
	}
	if !applies {
		t.Fatal("markerとmodule identityが揃えばquality toolchainを適用する")
	}
}

func TestQualityToolsApplyRejectsMismatchedModuleAfterOptIn(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	trackMarker(t, root)
	if err := os.MkdirAll(filepath.Join(root, "glm-worker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "glm-worker", "go.mod"), []byte("module example.com/foreign\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	applies, err := QualityToolsApply(root)
	var scopeErr *QualityScopeError
	if applies || !errors.As(err, &scopeErr) || scopeErr.Reason != QualityScopeModuleMismatch {
		t.Fatalf("mismatched module result = applies:%v err:%v", applies, err)
	}
}

func TestQualityToolsApplyReportsHarnessEvaluationFailure(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	trackMarker(t, root)
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	applies, err := QualityToolsApply(root)
	var scopeErr *QualityScopeError
	if applies || !errors.As(err, &scopeErr) || scopeErr.Reason != QualityScopeEvaluationFailed || scopeErr.Cause == nil {
		t.Fatalf("evaluation failure result = applies:%v err:%v", applies, err)
	}
}

func TestQualityToolsApplyModuleWithoutMarkerIsInactive(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	goModDir := filepath.Join(root, filepath.FromSlash("glm-worker"))
	if err := os.MkdirAll(goModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goMod := "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n"
	if err := os.WriteFile(filepath.Join(goModDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	applies, err := QualityToolsApply(root)
	if err != nil {
		t.Fatal(err)
	}
	if applies {
		t.Fatal("module identityだけではquality toolchainを適用しない")
	}
}

func TestCaptureMarkerGuardsMarkerState(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	trackMarker(t, root)

	before, err := CaptureMarker(root)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Exists || !before.Regular || before.SHA256 == "" {
		t.Fatalf("before = %+v want regular hashed marker", before)
	}
	same, err := CaptureMarker(root)
	if err != nil {
		t.Fatal(err)
	}
	if !SameMarkerGuard(before, same) {
		t.Fatalf("unchanged marker must compare equal: %+v %+v", before, same)
	}

	writeMarker(t, root, "tampered\n")
	after, err := CaptureMarker(root)
	if err != nil {
		t.Fatal(err)
	}
	if SameMarkerGuard(before, after) {
		t.Fatal("内容変化を検出できません")
	}

	if err := os.Remove(filepath.Join(root, MarkerPath)); err != nil {
		t.Fatal(err)
	}
	removed, err := CaptureMarker(root)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Exists || SameMarkerGuard(before, removed) {
		t.Fatalf("欠損を検出できません: %+v", removed)
	}
}
