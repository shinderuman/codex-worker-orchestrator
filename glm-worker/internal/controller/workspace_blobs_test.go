package controller

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceBlobBatchPreservesBinaryEmptyAndSymlinkContents(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	runControllerGit(t, repo, "init", "-q")
	cases := []struct {
		path string
		mode string
		data []byte
	}{
		{path: "nested/raw\n.bin", mode: "100644", data: []byte{0, '\n', 0xff, '\r'}},
		{path: "empty", mode: "100644", data: []byte{}},
		{path: "link", mode: "120000", data: []byte("nested/raw\n.bin")},
	}
	var entries []workspaceTreeEntry
	for _, tc := range cases {
		oid, err := runGitBinary(repo, tc.data, "hash-object", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, workspaceTreeEntry{path: tc.path, mode: tc.mode, oid: strings.TrimSpace(string(oid))})
	}
	blobs, err := readWorkspaceBlobs(repo, entries, entries)
	if err != nil {
		t.Fatal(err)
	}
	for index, entry := range entries {
		if err := materializeExecutionEntry(repo, entry, blobs); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(repo, entry.path)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, mode, err := suspensionFileBytes(path, info)
		if err != nil || mode != entry.mode || !bytes.Equal(data, cases[index].data) {
			t.Fatalf("materialized raw object differs at %q: mode=%s error=%v", entry.path, mode, err)
		}
	}
}

func TestWorkspaceBlobBatchRejectsMissingAndNonBlobObjects(t *testing.T) {
	t.Parallel()
	repo, _ := newControllerLinkedWorktree(t)
	for _, oid := range []string{strings.Repeat("0", 40), controllerGitOutput(t, repo, "rev-parse", "HEAD")} {
		if _, err := readWorkspaceBlobs(repo, []workspaceTreeEntry{{path: "file", mode: "100644", oid: oid}}); err == nil {
			t.Fatal("workspace accepted missing or non-blob content")
		}
	}
}
