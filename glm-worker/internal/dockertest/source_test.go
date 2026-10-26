package dockertest

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func sourceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		if _, err := output(context.Background(), root, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"staged.txt", "dirty.txt", "deleted.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("original\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "fixture"}} {
		if _, err := output(context.Background(), root, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSourceArchivePreservesIndexDirtyFilesAndDeletion(t *testing.T) {
	root := sourceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := output(context.Background(), root, "git", "add", "staged.txt"); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"staged.txt": "working tree\n", "dirty.txt": "changed\n", "untracked file.txt": "new\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	before, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	archive, digest, err := sourceArchive(context.Background(), root, t.TempDir(), before)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 {
		t.Fatalf("archive SHA-256 = %q", digest)
	}
	restored := t.TempDir()
	extractFixtureArchive(t, archive, restored)
	after, err := state.CaptureGitSnapshot(restored)
	if err != nil {
		t.Fatal(err)
	}
	if before.Head != after.Head || before.IndexDigest != after.IndexDigest || before.WorktreeDigest != after.WorktreeDigest {
		t.Fatalf("source転送でsnapshotが変化しました: before=%+v after=%+v", before, after)
	}
	if _, err := os.Stat(filepath.Join(restored, "deleted.txt")); !os.IsNotExist(err) {
		t.Fatalf("削除fileが復活しました: %v", err)
	}
}

func extractFixtureArchive(t *testing.T, path, destination string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(destination, header.Name)
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, body, os.FileMode(header.Mode)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSourceArchiveRejectsExternalSymlink(t *testing.T) {
	root := sourceFixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "external")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = sourceArchive(context.Background(), root, t.TempDir(), snapshot)
	if err == nil || !strings.Contains(err.Error(), "repository外") {
		t.Fatalf("外部symlinkを拒否しませんでした: %v", err)
	}
}

func TestDockerDiagnosticRejectsIgnoredInputBeforeExecution(t *testing.T) {
	root := sourceFixture(t)
	for name, body := range map[string]string{".gitignore": "private.go\n", "private.go": "package private\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var diagnosticOutput strings.Builder
	code, err := Run(context.Background(), root, []string{"./..."}, io.Discard, &diagnosticOutput)
	if code == 0 || err == nil || !strings.Contains(err.Error(), "ignored input") {
		t.Fatalf("ignored inputを持つsourceを拒否しませんでした: code=%d err=%v", code, err)
	}
	if !strings.Contains(diagnosticOutput.String(), `"ignored_inputs_absent":false`) {
		t.Fatalf("拒否したsource条件が診断にありません: %s", diagnosticOutput.String())
	}
}
