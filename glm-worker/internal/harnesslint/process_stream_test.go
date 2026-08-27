package harnesslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessStreamBoundaryAllowsDeclaredOwnerAndRejectsRogueReference(t *testing.T) {
	root := t.TempDir()
	allowed := "glm-worker/cmd/harnesslint/main.go"
	rogue := "glm-worker/internal/rogue/rogue.go"
	writeProcessStreamFixture(t, root, allowed, "package main\nimport \"os\"\nfunc main() { _, _ = os.Stdout.Write(nil); _, _ = os.Stderr.Write(nil) }\n")
	writeProcessStreamFixture(t, root, rogue, "package rogue\nimport \"os\"\nvar _ = os.Stdout\n")

	violations, err := processStreamViolations(root, []string{allowed, rogue})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Path != rogue || !strings.Contains(violations[0].Message, "found 1 want 0") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestProcessStreamBoundaryRejectsDirectPrintAndDotImport(t *testing.T) {
	root := t.TempDir()
	direct := "glm-worker/internal/rogue/direct.go"
	dotted := "glm-worker/internal/rogue/dotted.go"
	writeProcessStreamFixture(t, root, direct, "package rogue\nimport f \"fmt\"\nfunc leak() { f.Println(\"x\") }\n")
	writeProcessStreamFixture(t, root, dotted, "package rogue\nimport . \"os\"\nvar _ = Stdout\n")

	violations, err := processStreamViolations(root, []string{direct, dotted})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 2 {
		t.Fatalf("violations = %#v", violations)
	}
	if !strings.Contains(violations[0].Message+violations[1].Message, "direct process print") ||
		!strings.Contains(violations[0].Message+violations[1].Message, "dot import") {
		t.Fatalf("violations = %#v", violations)
	}
}

func writeProcessStreamFixture(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
