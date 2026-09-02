package authoritybootstrapcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSnapshotAndBuildParts(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, rulesFile, "rules-body\n")
	writeTestFile(t, root, planFile, "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/current.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/next.md`\n")
	writeTestFile(t, root, "IMPLEMENTATION_TASKS/current.md", "task-body\n")

	snap, err := loadSnapshot(root)
	if err != nil {
		t.Fatalf("loadSnapshot: %v", err)
	}
	if snap.activePath != "IMPLEMENTATION_TASKS/current.md" {
		t.Fatalf("activePath = %q", snap.activePath)
	}
	if snap.hash == "" {
		t.Fatal("snapshot hash is empty")
	}

	for kind, want := range map[string]string{
		"rules":  "rules-body\n",
		"plan":   "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/current.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/next.md`\n",
		"active": "task-body\n",
	} {
		output, err := snapshotPart(kind, snap)
		if err != nil {
			t.Fatalf("snapshotPart(%s): %v", kind, err)
		}
		if output.AuthoritySnapshotSHA256 != snap.hash {
			t.Fatalf("%s snapshot hash = %q, want %q", kind, output.AuthoritySnapshotSHA256, snap.hash)
		}
		if output.AuthorityKind != kind {
			t.Fatalf("%s authority kind = %q", kind, output.AuthorityKind)
		}
		if output.ActiveTask != "IMPLEMENTATION_TASKS/current.md" {
			t.Fatalf("%s active task = %q", kind, output.ActiveTask)
		}
		if output.Content != want {
			t.Fatalf("%s content = %q, want %q", kind, output.Content, want)
		}
	}
}

func TestLoadSnapshotRejectsInvalidActiveSchedule(t *testing.T) {
	tests := map[string]string{
		"missing":    "# Plan\n## NEXT\n- `IMPLEMENTATION_TASKS/x.md`\n",
		"empty":      "# Plan\n## ACTIVE\n\n## NEXT\n",
		"multiple":   "# Plan\n## ACTIVE\n- `IMPLEMENTATION_TASKS/a.md`\n- `IMPLEMENTATION_TASKS/b.md`\n## NEXT\n",
		"unexpected": "# Plan\n## ACTIVE\nACTIVE: IMPLEMENTATION_TASKS/a.md\n## NEXT\n",
		"traversal":  "# Plan\n## ACTIVE\n- `IMPLEMENTATION_TASKS/../secret.md`\n## NEXT\n",
	}
	for name, plan := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, rulesFile, "rules\n")
			writeTestFile(t, root, planFile, plan)
			if _, err := loadSnapshot(root); err == nil {
				t.Fatal("loadSnapshot succeeded, want error")
			}
		})
	}
}

func TestLoadSnapshotRejectsSymlinkActiveTask(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, rulesFile, "rules\n")
	writeTestFile(t, root, planFile, "# Plan\n## ACTIVE\n- `IMPLEMENTATION_TASKS/current.md`\n")
	writeTestFile(t, root, "target.md", "task-body\n")
	link := filepath.Join(root, "IMPLEMENTATION_TASKS", "current.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target.md"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSnapshot(root); err == nil {
		t.Fatal("loadSnapshot succeeded for symlink ACTIVE task")
	}
}

func TestFindRepoRootFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, rulesFile, "rules\n")
	writeTestFile(t, root, planFile, "plan\n")
	nested := filepath.Join(root, "glm-worker", "internal")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err := findRepoRoot(nested)
	if err != nil {
		t.Fatalf("findRepoRoot = %v", err)
	}
	if got != root {
		t.Fatalf("findRepoRoot = %q, want %q", got, root)
	}
}

func writeTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", relativePath, err)
	}
}
