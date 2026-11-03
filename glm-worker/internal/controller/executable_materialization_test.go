package controller

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestImportedAndMaterializedExecutablesRun(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	runControllerGit(t, repo, "init")
	input := filepath.Join(repo, "input.sh")
	if err := execfile.Write(input, []byte("#!/bin/sh\nprintf runnable\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oid, err := gitTrimmed(repo, "hash-object", "-w", input)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"imported.sh", "materialized.sh"} {
		t.Run(name, func(t *testing.T) {
			entry := workspaceTreeEntry{path: name, mode: "100755", oid: oid}
			path := filepath.Join(repo, name)
			blobs, err := readWorkspaceBlobs(repo, []workspaceTreeEntry{entry})
			if err != nil {
				t.Fatal(err)
			}
			if name == "imported.sh" {
				err = writeParentSourceEntry(path, entry, blobs)
			} else {
				err = materializeExecutionEntry(repo, entry, blobs)
			}
			if err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(path).Output(); err != nil || string(output) != "runnable" {
				t.Fatalf("executable = %q, %v", output, err)
			}
		})
	}
}
