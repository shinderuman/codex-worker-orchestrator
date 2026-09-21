package authoritybootstrapcmd

import (
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestBuildBootstrapProjectsCanonicalAuthorityFromOneSnapshot(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, rulesFile, "rules-body\n")
	writeTestFile(t, root, planFile, "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/current.md`\n")
	writeTestFile(t, root, "IMPLEMENTATION_TASKS/current.md", "task-body\n")

	output, err := BuildBootstrapFromRoot(root)
	if err != nil {
		t.Fatalf("BuildBootstrapFromRoot: %v", err)
	}
	if output.AuthoritySnapshotSHA256 == "" {
		t.Fatal("authority snapshot hash is empty")
	}
	if output.ActiveTask != "IMPLEMENTATION_TASKS/current.md" {
		t.Fatalf("active task = %q", output.ActiveTask)
	}
	for name, part := range map[string]BootstrapPart{
		"rules":  output.Rules,
		"plan":   output.Plan,
		"active": output.Active,
	} {
		if part.ContentSHA256 == "" {
			t.Fatalf("%s content hash is empty", name)
		}
	}
	if output.Rules.Content != "rules-body\n" {
		t.Fatalf("rules content = %q", output.Rules.Content)
	}
	if output.Plan.Content != "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/current.md`\n" {
		t.Fatalf("plan content = %q", output.Plan.Content)
	}
	if output.Active.Content != "task-body\n" {
		t.Fatalf("active content = %q", output.Active.Content)
	}
}

func TestSameSnapshotRejectsMixedAuthorityReads(t *testing.T) {
	base := snapshot{
		rules:      []byte("rules\n"),
		plan:       []byte("plan\n"),
		active:     []byte("task\n"),
		activePath: "IMPLEMENTATION_TASKS/current.md",
	}
	if !sameSnapshot(base, snapshot{
		rules:      []byte("rules\n"),
		plan:       []byte("plan\n"),
		active:     []byte("task\n"),
		activePath: "IMPLEMENTATION_TASKS/current.md",
	}) {
		t.Fatal("identical authority reads were rejected")
	}
	for name, changed := range map[string]snapshot{
		"rules": {
			rules: []byte("rules-2\n"), plan: []byte("plan\n"), active: []byte("task\n"), activePath: "IMPLEMENTATION_TASKS/current.md",
		},
		"plan": {
			rules: []byte("rules\n"), plan: []byte("plan-2\n"), active: []byte("task\n"), activePath: "IMPLEMENTATION_TASKS/current.md",
		},
		"active": {
			rules: []byte("rules\n"), plan: []byte("plan\n"), active: []byte("task-2\n"), activePath: "IMPLEMENTATION_TASKS/current.md",
		},
		"active path": {
			rules: []byte("rules\n"), plan: []byte("plan\n"), active: []byte("task\n"), activePath: "IMPLEMENTATION_TASKS/other.md",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if sameSnapshot(base, changed) {
				t.Fatalf("mixed %s authority read was accepted", name)
			}
		})
	}
}

func TestBuildCommandBootstrapRequiresActiveRepositoryHarness(t *testing.T) {
	root := t.TempDir()
	initTestRepository(t, root)
	writeTestFile(t, root, rulesFile, "rules\n")
	writeTestFile(t, root, planFile, "# Plan\n## ACTIVE\n- `IMPLEMENTATION_TASKS/current.md`\n")
	writeTestFile(t, root, "IMPLEMENTATION_TASKS/current.md", "task\n")

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	if _, err := BuildCommand([]string{"bootstrap"}); err == nil || !strings.Contains(err.Error(), repositoryharness.ReasonAbsent) {
		t.Fatalf("markerless bootstrap error = %v", err)
	}
}

func TestBuildCommandBootstrapUsesActiveRepositoryHarness(t *testing.T) {
	root := t.TempDir()
	activateTestRepository(t, root)
	writeTestFile(t, root, rulesFile, "rules\n")
	writeTestFile(t, root, planFile, "# Plan\n## ACTIVE\n- `IMPLEMENTATION_TASKS/current.md`\n")
	writeTestFile(t, root, "IMPLEMENTATION_TASKS/current.md", "task\n")

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	value, err := BuildCommand([]string{"bootstrap"})
	if err != nil {
		t.Fatalf("BuildCommand bootstrap: %v", err)
	}
	output, ok := value.(BootstrapOutput)
	if !ok {
		t.Fatalf("bootstrap output type = %T", value)
	}
	if output.ActiveTask != "IMPLEMENTATION_TASKS/current.md" || output.Rules.Content != "rules\n" || output.Active.Content != "task\n" {
		t.Fatalf("bootstrap output = %#v", output)
	}
}
