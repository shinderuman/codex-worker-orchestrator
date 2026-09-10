package guardrepair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepairScopeIsExplicitAndDoesNotIncludeParentMetadata(t *testing.T) {
	for _, path := range []string{
		"glm-worker/internal/runner/git_authority_guard.go",
		"glm-worker/internal/workflow/guard_recovery.go",
		"glm-worker/internal/workflow/guard_recovery_test.go",
	} {
		if !IsAllowed(path) {
			t.Fatalf("required repair path is not allowed: %s", path)
		}
	}
	for _, path := range []string{
		"IMPLEMENTATION_PLAN.local.md",
		"IMPLEMENTATION_TASKS/next.md",
		"glm-worker/internal/app/app.go",
		"glm-worker/internal/state/resume.go",
		"README.md",
	} {
		if IsAllowed(path) {
			t.Fatalf("out-of-scope path is allowed: %s", path)
		}
	}
}

func TestRelevantDigestChangesOnlyWithRepairScope(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "glm-worker", "internal", "workflow", "guard_recovery.go")
	if err := os.MkdirAll(filepath.Dir(allowed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowed, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := RelevantDigest(root)
	if err != nil {
		t.Fatal(err)
	}

	unrelated := filepath.Join(root, "README.md")
	if err := os.WriteFile(unrelated, []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterUnrelated, err := RelevantDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if afterUnrelated != before {
		t.Fatal("unrelated source changed repair-relevant digest")
	}

	if err := os.WriteFile(allowed, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterAllowed, err := RelevantDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if afterAllowed == before {
		t.Fatal("allowed repair source did not change relevant digest")
	}
}
