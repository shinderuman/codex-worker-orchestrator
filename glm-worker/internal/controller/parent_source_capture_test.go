package controller

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestParentSourceCaptureKeepsMetadataAndProductionBytes(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	runControllerGit(t, repo, "init", "-q")
	wanted := map[string]struct {
		data []byte
		mode string
	}{
		"IMPLEMENTATION_PLAN.local.md": {[]byte("current\r\nplan\x00\n"), "100644"},
		"IMPLEMENTATION_TASKS/継続.md":   {[]byte("current\r\ntask\n"), "100644"},
		"source.txt":                   {[]byte("unstaged source\n"), "100644"},
		"script.sh":                    {[]byte("#!/bin/sh\nexit 0\n"), "100755"},
		".gitattributes":               {[]byte("*.md filter=must-not-run text eol=lf\n"), "100644"},
		"IMPLEMENTATION_TASKS/link.md": {[]byte("../script.sh"), "120000"},
	}
	for _, path := range []string{"IMPLEMENTATION_PLAN.local.md", "IMPLEMENTATION_RULES.md", "source.txt", "script.sh"} {
		writeSuspensionTestFile(t, repo, path, "indexed content\n")
	}
	runControllerGit(t, repo, "add", ".")
	indexBefore := controllerGitOutput(t, repo, "write-tree")
	if err := os.Remove(filepath.Join(repo, "IMPLEMENTATION_RULES.md")); err != nil {
		t.Fatal(err)
	}
	for path, file := range wanted {
		if file.mode == "120000" {
			continue
		}
		writeSuspensionTestFile(t, repo, path, string(file.data))
		if file.mode == "100755" {
			if err := os.Chmod(filepath.Join(repo, path), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Symlink("../script.sh", filepath.Join(repo, "IMPLEMENTATION_TASKS/link.md")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "config", "filter.must-not-run.clean", "exit 77")
	runControllerGit(t, repo, "config", "filter.must-not-run.required", "true")
	tree, err := captureParentSourceTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := sourceImportTreeEntries(repo, tree)
	if err != nil || len(entries) != len(wanted) {
		t.Fatalf("captured tree differs: %d %v", len(entries), err)
	}
	for _, entry := range entries {
		file, ok := wanted[entry.path]
		if !ok || entry.mode != file.mode {
			t.Fatalf("captured path/mode differs: %#v", entry)
		}
		data, err := runGitBinary(repo, nil, "cat-file", "blob", entry.oid)
		if err != nil || !bytes.Equal(data, file.data) {
			t.Fatalf("captured raw bytes differ for %s: %v", entry.path, err)
		}
	}
	if controllerGitOutput(t, repo, "write-tree") != indexBefore {
		t.Fatal("source capture changed the real index")
	}
}
