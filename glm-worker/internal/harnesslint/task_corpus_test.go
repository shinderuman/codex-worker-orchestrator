package harnesslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskCorpusViolationsRejectDanglingDependency(t *testing.T) {
	root := t.TempDir()
	path := "IMPLEMENTATION_TASKS/014-test-impact-evaluation.md"
	writeTaskCorpusFile(t, root, path, "# Task\n\n## Dependencies\n\n- `IMPLEMENTATION_TASKS/011-operation-category-telemetry.md`\n")

	violations, err := taskCorpusViolations(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].Rule != "task-corpus-integrity" {
		t.Fatalf("violations = %#v", violations)
	}
	if !strings.Contains(violations[0].Message, "011-operation-category-telemetry.md") {
		t.Fatalf("message = %q", violations[0].Message)
	}
}

func TestTaskCorpusViolationsAcceptExistingDependency(t *testing.T) {
	root := t.TempDir()
	path := "IMPLEMENTATION_TASKS/014-test-impact-evaluation.md"
	dependency := "IMPLEMENTATION_TASKS/011-operation-category-telemetry.md"
	writeTaskCorpusFile(t, root, path, "# Task\n\n## Dependencies\n\n- `"+dependency+"`\n")
	writeTaskCorpusFile(t, root, dependency, "# Task\n")

	violations, err := taskCorpusViolations(root, []string{path, dependency})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestTaskCorpusViolationsIgnoreFencedInstructionExamples(t *testing.T) {
	existing := map[string]bool{"IMPLEMENTATION_TASKS/016-worker-repo-search-integration.md": true}
	body := "# Task\n" +
		"## Original instruction\n" +
		"`````text\n" +
		"## Status\n" +
		"## Dependencies\n" +
		"- `IMPLEMENTATION_TASKS/missing-inside-instruction.md`\n" +
		"```\n" +
		"nested fence example\n" +
		"`````\n" +
		"## Dependencies\n" +
		"- `IMPLEMENTATION_TASKS/016-worker-repo-search-integration.md`\n"

	violations := taskFileCorpusViolations("IMPLEMENTATION_TASKS/fixture.md", body, existing)
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestTaskCorpusViolationsRejectTopLevelStatus(t *testing.T) {
	body := "# Task\n\n## Status\n\n- ACTIVE\n\n## Dependencies\n\nnone\n"
	violations := taskFileCorpusViolations("IMPLEMENTATION_TASKS/fixture.md", body, map[string]bool{})
	if len(violations) != 1 || violations[0].Rule != "task-corpus-integrity" {
		t.Fatalf("violations = %#v", violations)
	}
}

func writeTaskCorpusFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
