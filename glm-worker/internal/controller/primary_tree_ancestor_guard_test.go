package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceTreeWriterRejectsSymlinkAncestorWithoutTouchingOutside(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	outside := t.TempDir()
	writeSuspensionTestFile(t, outside, "child", "outside must survive\n")
	if err := os.Symlink(outside, filepath.Join(repo, "replacement")); err != nil {
		t.Fatal(err)
	}
	data, err := runGitBinary(repo, []byte("candidate bytes\n"), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	after := []workspaceTreeEntry{{
		path: "replacement/child", mode: "100644", oid: strings.TrimSpace(string(data)),
	}}
	if err := applySourceTreeEntries(repo, nil, after); err == nil ||
		!strings.Contains(err.Error(), "ambiguous suspension path ancestor") {
		t.Fatalf("symlink ancestor unexpectedly admitted: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(outside, "child"))
	if err != nil || string(content) != "outside must survive\n" {
		t.Fatalf("rejected promotion changed external target: %q %v", content, err)
	}
}
