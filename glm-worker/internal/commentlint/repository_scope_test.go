package commentlint

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestCheckFailsClosedWhenOptedInRepositoryModuleIsMissing(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, repositoryharness.MarkerPath), []byte(repositoryharness.MarkerContent), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "--", repositoryharness.MarkerPath)
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Check(root)
	var scopeErr *repositoryharness.QualityScopeError
	if !errors.As(err, &scopeErr) || scopeErr.Reason != repositoryharness.QualityScopeModuleMissing {
		t.Fatalf("Check result = report:%#v err:%v", report, err)
	}
	if report.Status != "" || len(report.Violations) != 0 {
		t.Fatalf("invalid scope fell through to generic lint: %#v", report)
	}
}
