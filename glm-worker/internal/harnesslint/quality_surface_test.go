package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQualityWiringRequiresReviewerGate(t *testing.T) {
	root := t.TempDir()
	path := "glm-worker/internal/workflow/workflow.go"
	writeQualityFile(t, root, path, "package workflow\n")
	violations, err := qualityWiringViolations(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 8 {
		t.Fatalf("violations = %+v", violations)
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
