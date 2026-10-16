package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuspensionBatchPreservesRawBlobsAndModes(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	runControllerGit(t, repo, "init", "-q")
	runControllerGit(t, repo, "config", "filter.must-not-run.clean", "exit 77")
	runControllerGit(t, repo, "config", "filter.must-not-run.required", "true")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.data filter=must-not-run text eol=lf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wanted := map[string]struct {
		data []byte
		mode string
	}{}
	var entries []workspaceTreeEntry
	for index := range 131 {
		path := fmt.Sprintf("file-%03d.data", index)
		wanted[path] = struct {
			data []byte
			mode string
		}{[]byte("raw\r\nblob\x00\n"), "100644"}
	}
	for _, path := range []string{"line\nbreak.data", "継続.data", "-option.data"} {
		wanted[path] = struct {
			data []byte
			mode string
		}{[]byte(path + "\r\n"), "100644"}
	}
	wanted["executable.data"] = struct {
		data []byte
		mode string
	}{[]byte("executable\n"), "100755"}
	for path, file := range wanted {
		mode := os.FileMode(0o644)
		if file.mode == "100755" {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(repo, path), file.data, mode); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, workspaceTreeEntry{path: path})
	}
	if err := os.Symlink("executable.data", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	wanted["link"] = struct {
		data []byte
		mode string
	}{[]byte("executable.data"), "120000"}
	entries = append(entries, workspaceTreeEntry{path: "link"}, workspaceTreeEntry{path: "deleted", mode: "100644"})
	captured, err := captureSuspensionEntries(repo, entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(captured) != len(wanted) {
		t.Fatalf("captured %d files, want %d", len(captured), len(wanted))
	}
	objects := map[string]string{}
	for _, entry := range captured {
		file, ok := wanted[entry.path]
		if !ok || entry.mode != file.mode {
			t.Fatalf("file identity differs: %#v", entry)
		}
		key := string(file.data)
		oid, ok := objects[key]
		if !ok {
			data, err := runGitBinary(repo, file.data, "hash-object", "--stdin")
			if err != nil {
				t.Fatal(err)
			}
			oid = strings.TrimSpace(string(data))
			objects[key] = oid
		}
		if entry.oid != oid {
			t.Fatalf("raw blob differs: %s", entry.path)
		}
	}
}
