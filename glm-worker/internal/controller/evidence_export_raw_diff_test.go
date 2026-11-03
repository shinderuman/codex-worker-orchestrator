package controller

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestExportEvidenceKeepsRawStagedAndUnstagedBytes(t *testing.T) {
	for _, mode := range []string{"environment-external", "repository-external", "repository-textconv"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "driver-executed")
			driver := filepath.Join(root, "driver.sh")
			if err := os.WriteFile(driver, []byte(fmt.Sprintf("#!/bin/sh\nprintf invoked >> %q\nprintf 'constant\\n'\n", marker)), 0o700); err != nil {
				t.Fatal(err)
			}
			fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
				f.skipSealedEvidence = true
				for name, content := range map[string]string{"raw.txt": "committed bytes\n", ".gitattributes": "raw.txt diff=raw-audit\n"} {
					if err := os.WriteFile(filepath.Join(f.repo, name), []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				runControllerGit(t, f.repo, "add", "raw.txt", ".gitattributes")
				runControllerGit(t, f.repo, "commit", "-qm", "raw diff fixture")
			})
			switch mode {
			case "environment-external":
				t.Setenv("GIT_EXTERNAL_DIFF", driver)
			case "repository-external":
				runControllerGit(t, fixture.repo, "config", "diff.raw-audit.command", driver)
			case "repository-textconv":
				runControllerGit(t, fixture.repo, "config", "diff.raw-audit.textconv", driver)
			}
			before, err := CaptureWorkspaceSnapshot(fixture.repo)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(fixture.repo, "raw.txt"), []byte("staged bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runControllerGit(t, fixture.repo, "add", "raw.txt")
			staged, err := CaptureWorkspaceSnapshot(fixture.repo)
			if err != nil {
				t.Fatal(err)
			}
			if staged.ID == before.ID || staged.IndexDigest == before.IndexDigest {
				t.Fatal("staged raw bytes did not change workspace identity")
			}
			if err := os.WriteFile(filepath.Join(fixture.repo, "raw.txt"), []byte("unstaged bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			after, err := CaptureWorkspaceSnapshot(fixture.repo)
			if err != nil {
				t.Fatal(err)
			}
			if after.ID == staged.ID || after.WorktreeDigest == staged.WorktreeDigest || after.IndexDigest != staged.IndexDigest {
				t.Fatal("unstaged raw bytes did not independently change workspace identity")
			}
			export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Target.Live || result.RuntimeStatus != evidenceExportRuntimeCollected {
				t.Fatalf("live export was not collected: %#v", result)
			}
			for _, check := range []struct{ path, removed, added string }{
				{"live/git/diff-staged.patch", "committed bytes", "staged bytes"},
				{"live/git/diff-unstaged.patch", "staged bytes", "unstaged bytes"},
			} {
				patch := exportFileByPath(t, export, check.path)
				for _, want := range []string{"diff --git a/raw.txt b/raw.txt", "\n-" + check.removed + "\n", "\n+" + check.added + "\n"} {
					if !bytes.Contains(patch, []byte(want)) {
						t.Fatalf("%s lacks raw patch content %q: %s", check.path, want, patch)
					}
				}
			}
			if export.Manifest.Git == nil || export.Manifest.Git.Snapshot == nil || export.Manifest.Git.Snapshot.ID != after.ID {
				t.Fatal("export does not bind the changed raw workspace snapshot")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("machine evidence executed a presentation driver: %v", err)
			}
			output := controllerGitOutput(t, fixture.repo, "diff", "--ext-diff", "--textconv", "--", "raw.txt")
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("presentation control did not execute configured driver: %v", err)
			}
			if mode == "repository-textconv" && output != "" {
				t.Fatalf("constant textconv control did not hide raw change: %q", output)
			}
		})
	}
}
