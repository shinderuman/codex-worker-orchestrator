package authoritybootstrapcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSnapshotAndRenderParts(t *testing.T) {
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
		var out bytes.Buffer
		if err := writeSnapshotPart(&out, kind, snap); err != nil {
			t.Fatalf("writeSnapshotPart(%s): %v", kind, err)
		}
		text := out.String()
		if !strings.Contains(text, "authority_snapshot_sha256="+snap.hash+"\n") {
			t.Fatalf("%s output missing snapshot hash: %q", kind, text)
		}
		if !strings.Contains(text, "authority_kind="+kind+"\n") {
			t.Fatalf("%s output missing kind: %q", kind, text)
		}
		if !strings.HasSuffix(text, want) {
			t.Fatalf("%s output suffix = %q, want %q", kind, text, want)
		}
	}
}

func TestParseActivePathRejectsAmbiguousAndInvalidPlan(t *testing.T) {
	tests := map[string]string{
		"missing":   "# Plan\n## NEXT\n- `IMPLEMENTATION_TASKS/x.md`\n",
		"empty":     "# Plan\n## ACTIVE\n\n## NEXT\n",
		"multiple":  "# Plan\n## ACTIVE\n- `IMPLEMENTATION_TASKS/a.md`\n- `IMPLEMENTATION_TASKS/b.md`\n## NEXT\n",
		"unexpected": "# Plan\n## ACTIVE\nACTIVE: IMPLEMENTATION_TASKS/a.md\n## NEXT\n",
		"traversal": "# Plan\n## ACTIVE\n- `IMPLEMENTATION_TASKS/../secret.md`\n## NEXT\n",
	}
	for name, plan := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseActivePath([]byte(plan)); err == nil {
				t.Fatal("parseActivePath succeeded, want error")
			}
		})
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
		t.Fatalf("findRepoRoot: %v", err)
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
