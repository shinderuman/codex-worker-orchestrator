package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownDerivedStateRejectsPlanCurrentStateSections(t *testing.T) {
	root := t.TempDir()
	path := "IMPLEMENTATION_PLAN.local.md"
	writeMarkdownAuthorityFixture(t, root, path, "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## 現在のGit境界\n\n- branch: `main`\n")
	violations, err := markdownDerivedStatePathViolations(root, path)
	if err != nil {
		t.Fatal(err)
	}
	requireMarkdownDerivedStateViolation(t, violations, path)
}

func TestMarkdownDerivedStateIgnoresQuotedHistoricalHeadings(t *testing.T) {
	root := t.TempDir()
	path := "IMPLEMENTATION_TASKS/a.md"
	content := "# Task\n\n## Original instruction\n\n````text\n## Current boundary\n\n```text\n## Review findings\nnone\n```\n\n## Review findings\nnone\n````\n"
	writeMarkdownAuthorityFixture(t, root, path, content)
	violations, err := markdownDerivedStatePathViolations(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("quoted requirement was treated as live derived state: %+v", violations)
	}
}

func TestMarkdownDerivedStateRejectsTaskCurrentBoundary(t *testing.T) {
	root := t.TempDir()
	path := "IMPLEMENTATION_TASKS/a.md"
	writeMarkdownAuthorityFixture(t, root, path, "# Task\n\n## Contract\n\nstable\n\n## Current boundary\n\nblocked\n")
	violations, err := markdownDerivedStatePathViolations(root, path)
	if err != nil {
		t.Fatal(err)
	}
	requireMarkdownDerivedStateViolation(t, violations, path)
}

func TestMarkdownDerivedStateRejectsEmptyFindingsSnapshot(t *testing.T) {
	root := t.TempDir()
	path := "IMPLEMENTATION_TASKS/a.md"
	writeMarkdownAuthorityFixture(t, root, path, "# Task\n\n## Review findings\n\nnone\n")
	violations, err := markdownDerivedStatePathViolations(root, path)
	if err != nil {
		t.Fatal(err)
	}
	requireMarkdownDerivedStateViolation(t, violations, path)
}

func TestMarkdownDerivedStateAllowsActualUnresolvedFindings(t *testing.T) {
	root := t.TempDir()
	path := "IMPLEMENTATION_TASKS/a.md"
	writeMarkdownAuthorityFixture(t, root, path, "# Task\n\n## Review findings\n\n- unresolved defect\n")
	violations, err := markdownDerivedStatePathViolations(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("unresolved finding is durable task input and must be retained: %+v", violations)
	}
}

func TestMarkdownDerivedStateRejectsReadmeImplementationMirror(t *testing.T) {
	root := t.TempDir()
	path := "README.md"
	writeMarkdownAuthorityFixture(t, root, path, "# repo\n\n## 必要command\n\n- Go 1.25.4\n\n## CLI\n\ncurrent commands\n")
	violations, err := markdownDerivedStatePathViolations(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 2 {
		t.Fatalf("violations = %+v, want pinned-version and CLI mirror", violations)
	}
}

func writeMarkdownAuthorityFixture(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireMarkdownDerivedStateViolation(t *testing.T, violations []Violation, path string) {
	t.Helper()
	for _, violation := range violations {
		if violation.Rule == markdownDerivedStateRule && violation.Path == path {
			return
		}
	}
	t.Fatalf("%s violation not found: %+v", markdownDerivedStateRule, violations)
}
