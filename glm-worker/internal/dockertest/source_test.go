package dockertest

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
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
		if header.Typeflag == tar.TypeSymlink {
			if err := os.Symlink(header.Linkname, target); err != nil {
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

func TestSourceArchiveRunsNoMutatingGitAgainstProtectedRoot(t *testing.T) {
	root := sourceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git-invocations")
	recorder := "#!/bin/sh\n" +
		"printf '%s\\n' \"$PWD|$*\" >>\"" + logPath + "\"\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	if err := execfile.Write(filepath.Join(bin, "git"), []byte(recorder), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sourceArchive(context.Background(), root, t.TempDir(), snapshot); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	mutationsOutsideRoot := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		dir, argString, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("git呼び出し記録が不正です: %q", line)
		}
		fields := strings.Fields(argString)
		if len(fields) > 1 && fields[0] == "-C" {
			dir, fields = fields[1], fields[2:]
		}
		if len(fields) == 0 {
			t.Fatalf("git呼び出しにsubcommandがありません: %q", line)
		}
		if dir == root {
			switch fields[0] {
			case "clone", "write-tree", "read-tree", "update-ref":
				t.Fatalf("保護対象rootでgit %sが実行されました: %q", fields[0], line)
			}
			continue
		}
		mutationsOutsideRoot[fields[0]] = true
	}
	if !mutationsOutsideRoot["clone"] || !mutationsOutsideRoot["update-ref"] {
		t.Fatalf("退避先でのsource構築git呼び出しが観測されません: %v", mutationsOutsideRoot)
	}
}

func TestSourceArchiveRejectsExternalSymlink(t *testing.T) {
	root := sourceFixture(t)
	if err := os.Symlink("../outside-target", filepath.Join(root, "external")); err != nil {
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

func TestSourceArchiveRejectsAbsoluteSymlinkInsideRepository(t *testing.T) {
	root := sourceFixture(t)
	if err := os.Symlink(filepath.Join(root, "staged.txt"), filepath.Join(root, "absolute-link")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = sourceArchive(context.Background(), root, t.TempDir(), snapshot)
	if err == nil || !strings.Contains(err.Error(), "絶対path") {
		t.Fatalf("repo内絶対path symlinkを拒否しませんでした: %v", err)
	}
}

func TestSourceArchivePreservesRelativeSymlink(t *testing.T) {
	root := sourceFixture(t)
	if err := os.Symlink("staged.txt", filepath.Join(root, "relative-link")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	archive, _, err := sourceArchive(context.Background(), root, t.TempDir(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	extractFixtureArchive(t, archive, restored)
	link, err := os.Readlink(filepath.Join(restored, "relative-link"))
	if err != nil || link != "staged.txt" {
		t.Fatalf("相対symlinkが保存されていません: %q %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(restored, "relative-link")); err != nil {
		t.Fatalf("相対symlinkの参照先が復元されていません: %v", err)
	}
}

func initNestedRepositoryFixture(t *testing.T, root, name string) {
	t.Helper()
	nested := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(nested, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "inner.txt"), []byte("inner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "sub", "deep.txt"), []byte("deep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inner.txt", filepath.Join(nested, "link")); err != nil {
		t.Fatal(err)
	}
	innard := filepath.Join(nested, "innard")
	if err := os.MkdirAll(innard, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(innard, ".git"), []byte("gitdir: ../.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(innard, "data.txt"), []byte("innard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(nested, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "inner-file.txt"), []byte("nested nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "."},
		{"config", "user.name", "fixture"},
		{"config", "user.email", "fixture@example.invalid"},
		{"add", "inner.txt"},
		{"commit", "-qm", "nested"},
	} {
		if _, err := output(context.Background(), nested, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := output(context.Background(), inner, "git", "init", "-q", "."); err != nil {
		t.Fatal(err)
	}
}

func TestSourceArchiveIncludesUntrackedNestedRepositoryWorktree(t *testing.T) {
	root := sourceFixture(t)
	initNestedRepositoryFixture(t, root, "nested")
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatalf("untracked nested repositoryでsnapshotが失敗しました: %v", err)
	}
	archive, digest, err := sourceArchive(context.Background(), root, t.TempDir(), before)
	if err != nil {
		t.Fatalf("untracked nested repositoryでarchive構築が失敗しました: %v", err)
	}
	if len(digest) != 64 {
		t.Fatalf("archive SHA-256 = %q", digest)
	}
	restored := t.TempDir()
	extractFixtureArchive(t, archive, restored)
	if body, err := os.ReadFile(filepath.Join(restored, "nested", "inner.txt")); err != nil || string(body) != "inner\n" {
		t.Fatalf("nested repositoryのworktree fileが転送されていません: %q %v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(restored, "nested", "sub", "deep.txt")); err != nil || string(body) != "deep\n" {
		t.Fatalf("nested repositoryのsub directory内容が転送されていません: %q %v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(restored, "nested", "inner", "inner-file.txt")); err != nil || string(body) != "nested nested\n" {
		t.Fatalf("nested nested repositoryのworktree内容が転送されていません: %q %v", body, err)
	}
	if link, err := os.Readlink(filepath.Join(restored, "nested", "link")); err != nil || link != "inner.txt" {
		t.Fatalf("nested repository内の相対symlinkが保存されていません: %q %v", link, err)
	}
	if body, err := os.ReadFile(filepath.Join(restored, "nested", "innard", "data.txt")); err != nil || string(body) != "innard\n" {
		t.Fatalf("nested repository内の.git file隣接worktree fileが転送されていません: %q %v", body, err)
	}
	for _, metadata := range []string{filepath.Join("nested", ".git"), filepath.Join("nested", "inner", ".git"), filepath.Join("nested", "innard", ".git")} {
		if _, err := os.Stat(filepath.Join(restored, metadata)); !os.IsNotExist(err) {
			t.Fatalf("nested repositoryの.git metadataが転送されました: %s: %v", metadata, err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(restored, "plain.txt")); err != nil || string(body) != "plain\n" {
		t.Fatalf("通常のuntracked fileが転送されていません: %q %v", body, err)
	}
	scoped := filepath.Join(t.TempDir(), "scoped")
	if out, err := exec.Command("cp", "-Ra", root, scoped).CombinedOutput(); err != nil {
		t.Fatalf("scoped比較treeの複製に失敗しました: %v: %s", err, out)
	}
	for _, metadata := range []string{filepath.Join("nested", ".git"), filepath.Join("nested", "inner", ".git"), filepath.Join("nested", "innard", ".git")} {
		if err := os.RemoveAll(filepath.Join(scoped, metadata)); err != nil {
			t.Fatal(err)
		}
	}
	scopedSnapshot, err := state.CaptureGitSnapshot(scoped)
	if err != nil {
		t.Fatal(err)
	}
	after, err := state.CaptureGitSnapshot(restored)
	if err != nil {
		t.Fatal(err)
	}
	if scopedSnapshot.Head != after.Head || scopedSnapshot.IndexDigest != after.IndexDigest || scopedSnapshot.WorktreeDigest != after.WorktreeDigest {
		t.Fatalf("転送sourceがscoped snapshotと一致しません: scoped=%+v restored=%+v", scopedSnapshot, after)
	}
}

func TestSourceArchiveRejectsTrackedSubmodule(t *testing.T) {
	root := sourceFixture(t)
	initNestedRepositoryFixture(t, root, "submodule")
	if _, err := output(context.Background(), root, "git", "add", "submodule"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = sourceArchive(context.Background(), root, t.TempDir(), snapshot)
	if err == nil || !strings.Contains(err.Error(), "submodule") {
		t.Fatalf("tracked submoduleを拒否しませんでした: %v", err)
	}
}

func TestSourceArchivePreservesSplitIndexStagedState(t *testing.T) {
	root := sourceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("split staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := output(context.Background(), root, "git", "add", "staged.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := output(context.Background(), root, "git", "update-index", "--split-index"); err != nil {
		t.Fatal(err)
	}
	shared, err := filepath.Glob(filepath.Join(root, ".git", "sharedindex.*"))
	if err != nil || len(shared) == 0 {
		t.Fatalf("split indexのshared indexが作成されていません: %v %v", shared, err)
	}
	before, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	archive, _, err := sourceArchive(context.Background(), root, t.TempDir(), before)
	if err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	extractFixtureArchive(t, archive, restored)
	after, err := state.CaptureGitSnapshot(restored)
	if err != nil {
		t.Fatalf("復元先でGit indexを読めません: %v", err)
	}
	if before.Head != after.Head || before.IndexDigest != after.IndexDigest || before.WorktreeDigest != after.WorktreeDigest {
		t.Fatalf("split indexのstaged状態が保存されていません: before=%+v after=%+v", before, after)
	}
}

func TestSourceArchiveIgnoresGitLocationEnvironment(t *testing.T) {
	root := sourceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := sourceFixture(t)
	if err := os.WriteFile(filepath.Join(other, "foreign.txt"), []byte("foreign\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := output(context.Background(), other, "git", "add", "foreign.txt"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other, ".git", "index"))
	archive, _, err := sourceArchive(context.Background(), root, t.TempDir(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	extractFixtureArchive(t, archive, restored)
	after, err := state.CaptureGitSnapshot(restored)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Head != after.Head || snapshot.IndexDigest != after.IndexDigest || snapshot.WorktreeDigest != after.WorktreeDigest {
		t.Fatalf("Git位置環境変数の汚染がsource転送へ影響しました: before=%+v after=%+v", snapshot, after)
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


func TestSourceArchiveNestedExecutableModeMatchesSourceIdentity(t *testing.T) {
	root := sourceFixture(t)
	initNestedRepositoryFixture(t, root, "nested")
	tool := filepath.Join(root, "nested", "tool.sh")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\\nexit 0\\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var previous state.GitSnapshot
	for _, permission := range []os.FileMode{0o644, 0o755, 0o644} {
		if err := os.Chmod(tool, permission); err != nil {
			t.Fatal(err)
		}
		snapshot, err := state.CaptureGitSnapshot(root)
		if err != nil {
			t.Fatal(err)
		}
		if previous.WorktreeDigest != "" {
			if previous.Head != snapshot.Head || previous.IndexDigest != snapshot.IndexDigest {
				t.Fatalf("chmod以外にHEAD/indexが変化しました: before=%+v after=%+v", previous, snapshot)
			}
			if previous.WorktreeDigest == snapshot.WorktreeDigest {
				t.Fatalf("nested mode変更%#oがsource identityへ反映されません", permission)
			}
		}
		archive, _, err := sourceArchive(context.Background(), root, t.TempDir(), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		restored := t.TempDir()
		extractFixtureArchive(t, archive, restored)
		info, err := os.Stat(filepath.Join(restored, "nested", "tool.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != permission {
			t.Fatalf("Docker転送tarのmodeがsourceと一致しません: got=%#o want=%#o", info.Mode().Perm(), permission)
		}
		previous = snapshot
	}
}
