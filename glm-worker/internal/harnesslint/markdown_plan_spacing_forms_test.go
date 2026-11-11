package harnesslint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func TestMarkdownPlanSpacingAllSupportedTaskItems(t *testing.T) {
	const path = implementationPlanPath
	const a = "IMPLEMENTATION_TASKS/first.md"
	const b = "IMPLEMENTATION_TASKS/second.md"
	forms := []struct {
		name  string
		first string
		last  string
	}{
		{"plain-plain", "- " + a, "- " + b},
		{"backtick-backtick", "- `" + a + "`", "- `" + b + "`"},
		{"link-link", "- [`" + a + "`](" + a + ")", "- [`" + b + "`](" + b + ")"},
		{"plain-backtick", "- " + a, "- `" + b + "`"},
		{"backtick-link", "- `" + a + "`", "- [`" + b + "`](" + b + ")"},
		{"link-plain", "- [`" + a + "`](" + a + ")", "- " + b},
	}
	for _, tt := range forms {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			original := "## NEXT（優先順）\n\n" + tt.first + "\n \t\n" + tt.last + "\n"
			expected := "## NEXT（優先順）\n\n" + tt.first + "\n" + tt.last + "\n"
			writeMarkdownAuthorityFixture(t, root, path, original)
			violations, err := markdownDerivedStatePathViolations(root, path)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) != 1 || violations[0].Rule != "markdown-plan-spacing" || !violations[0].Fixable {
				t.Fatalf("spacing violation not detected: %+v", violations)
			}
			if err := fixPlanSpacing(root, []string{path}); err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != expected {
				t.Fatalf("unexpected fixed schedule:\n%q\nwant:\n%q", contents, expected)
			}
			schedule := taskcontract.ParsePlanSchedule(string(contents))
			next, blocked, err := schedule.NonActiveEntries()
			if err != nil || !reflect.DeepEqual(next, []string{a, b}) || len(blocked) != 0 {
				t.Fatalf("schedule entries = %v, %v, err %v", next, blocked, err)
			}
			if err := fixPlanSpacing(root, []string{path}); err != nil {
				t.Fatal(err)
			}
			again, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != expected {
				t.Fatalf("fix not idempotent: %q", again)
			}
			violations, err = markdownDerivedStatePathViolations(root, path)
			if err != nil || len(violations) != 0 {
				t.Fatalf("violations remain after fix: %+v, %v", violations, err)
			}
		})
	}
}

func TestMarkdownPlanSpacingPreservesFencesAndNonScheduleSections(t *testing.T) {
	const path = implementationPlanPath
	goal := "## GOAL\n\n\x60\x60\x60\x60md\n## NEXT（優先順）\n\n- \x60IMPLEMENTATION_TASKS/example-a.md\x60\n\n- \x60IMPLEMENTATION_TASKS/example-b.md\x60\n\x60\x60\x60\x60\n\n"
	fence := "\x60\x60\x60md\n- \x60IMPLEMENTATION_TASKS/quoted-a.md\x60\n\n\n- \x60IMPLEMENTATION_TASKS/quoted-b.md\x60\n## BLOCKED / USER_PERMISSION_WAIT\n\x60\x60\x60\n"
	first := "- IMPLEMENTATION_TASKS/first.md"
	second := "- \x60IMPLEMENTATION_TASKS/second.md\x60"
	third := "- [\x60IMPLEMENTATION_TASKS/third.md\x60](IMPLEMENTATION_TASKS/third.md)"
	fourth := "- IMPLEMENTATION_TASKS/fourth.md"
	original := "# Plan\n\n" + goal + "## NEXT（優先順）\n\n" +
		first + "\n\n" + second + "\n\n" + fence + "\n" +
		third + "\n\n" + fourth + "\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n- IMPLEMENTATION_TASKS/fifth.md\n"
	expected := strings.ReplaceAll(strings.ReplaceAll(original, first+"\n\n"+second, first+"\n"+second), third+"\n\n"+fourth, third+"\n"+fourth)
	if got := string(compactPlanSpacing([]byte(original))); got != expected {
		t.Fatalf("Plan rewrite crossed a fence or section boundary:\n%s\nwant:\n%s", got, expected)
	}
	if got := string(compactPlanSpacing([]byte(expected))); got != expected {
		t.Fatalf("normalized Plan is not a fixed point:\n%s", got)
	}
}

func TestMarkdownPlanSpacingIgnoresNonTaskBullets(t *testing.T) {
	content := "## NEXT（優先順）\n\n- arbitrary text\n\n- another item\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n- IMPLEMENTATION_TASKS/blocked.md\n"
	if got := string(compactPlanSpacing([]byte(content))); got != content {
		t.Fatalf("non-task bullets or heading gap unexpectedly changed: %q", got)
	}
}
