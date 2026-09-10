package repositoryharness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModuleDirectivePathAcceptsEquivalentModuleForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{name: "plain", line: "module " + repositoryModulePath},
		{name: "trailing-comment", line: "module " + repositoryModulePath + " // repository module"},
		{name: "quoted", line: "module \"" + repositoryModulePath + "\""},
		{name: "quoted-comment", line: "module \"" + repositoryModulePath + "\" // repository module"},
		{name: "raw-quoted", line: "module `" + repositoryModulePath + "`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := moduleDirectivePath(tc.line)
			if !ok || got != repositoryModulePath {
				t.Fatalf("moduleDirectivePath(%q) = %q, %v", tc.line, got, ok)
			}
		})
	}
}

func TestModuleDirectivePathRejectsDifferentOrMalformedDirective(t *testing.T) {
	for _, line := range []string{
		"module example.com/foreign",
		"module",
		"modulex " + repositoryModulePath,
		"module \"" + repositoryModulePath,
		"module " + repositoryModulePath + " unexpected",
	} {
		got, ok := moduleDirectivePath(line)
		if ok && got == repositoryModulePath {
			t.Fatalf("moduleDirectivePath(%q) accepted repository identity", line)
		}
	}
}

func TestQualityToolsApplyAcceptsEquivalentModuleDirective(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	trackMarker(t, root)
	moduleDir := filepath.Join(root, "glm-worker")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goMod := "module \"" + repositoryModulePath + "\" // repository module\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	applies, err := QualityToolsApply(root)
	if err != nil {
		t.Fatal(err)
	}
	if !applies {
		t.Fatal("equivalent module directive should preserve repository quality scope")
	}
}
