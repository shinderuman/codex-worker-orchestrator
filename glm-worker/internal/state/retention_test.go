package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

// stopDirtyByPathは保持列挙をpath indexへ変換する。
func stopDirtyByPath(files []StopDirtyFile) map[string]StopDirtyFile {
	byPath := make(map[string]StopDirtyFile, len(files))
	for _, file := range files {
		byPath[file.Path] = file
	}
	return byPath
}

// TestCaptureStopDirtyFilesAxesは--stop保持基準の列挙対象を固定する: untracked・
// staged・unstaged trackedを含み、親管理metadata集合を除外し、index hashとworktree
// 内容hashの組を識別子として持つ。
func TestCaptureStopDirtyFilesAxes(t *testing.T) {
	repo := initCommittedRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("modified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "staged.txt")
	for _, parent := range []string{
		ParentRulesFile,
		ParentPlanFile,
		"IMPLEMENTATION_HISTORY.md",
	} {
		if err := os.WriteFile(filepath.Join(repo, parent), []byte("parent\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "next.md"), []byte("task\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	byPath := stopDirtyByPath(files)
	if _, ok := byPath["IMPLEMENTATION_PLAN.local.md"]; ok {
		t.Fatal("親管理metadataが保持列挙に混入しています")
	}
	if _, ok := byPath["IMPLEMENTATION_RULES.md"]; ok {
		t.Fatal("親管理rulesが保持列挙に混入しています")
	}
	if _, ok := byPath["IMPLEMENTATION_HISTORY.md"]; ok {
		t.Fatal("親管理historyが保持列挙に混入しています")
	}
	if _, ok := byPath[filepath.Join("IMPLEMENTATION_TASKS", "next.md")]; ok {
		t.Fatal("親管理task fileが保持列挙に混入しています")
	}

	untracked, ok := byPath["untracked.txt"]
	if !ok {
		t.Fatalf("untracked fileが保持列挙にありません: %#v", files)
	}
	if untracked.IndexSHA != "" || untracked.WorktreeSHA == "" {
		t.Fatalf("untrackedの識別子が不正です: %#v", untracked)
	}
	staged, ok := byPath["staged.txt"]
	if !ok {
		t.Fatalf("staged fileが保持列挙にありません: %#v", files)
	}
	if staged.IndexSHA == "" || staged.WorktreeSHA == "" {
		t.Fatalf("stagedの識別子が不正です: %#v", staged)
	}
	modified, ok := byPath["tracked.txt"]
	if !ok {
		t.Fatalf("unstaged変更が保持列挙にありません: %#v", files)
	}
	if modified.IndexSHA == "" || modified.WorktreeSHA == "" {
		t.Fatalf("unstaged変更の識別子が不正です: %#v", modified)
	}
	if modified.IndexSHA == modified.WorktreeSHA {
		t.Fatal("内容変化したfileのindex hashとworktree hashが同一です")
	}
}

// TestCaptureStopDirtyFilesEmptyIsNotNilはclean checkoutの保持列挙が空列(非nil)を
// 返すことを固定する。空列はresume gateが「dirtyなし」としてnil(旧形式)と区別する。
func TestCaptureStopDirtyFilesEmptyIsNotNil(t *testing.T) {
	repo := initCommittedRepo(t)
	files, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if files == nil || len(files) != 0 {
		t.Fatalf("clean checkoutの保持列挙 = %#v want 空列", files)
	}
}

// TestCaptureStopDirtyFilesStagedRenameはstaged renameが新旧両pathを列挙へ含むことを
// 固定する。
func TestCaptureStopDirtyFilesStagedRename(t *testing.T) {
	repo := initCommittedRepo(t)
	if err := os.Rename(filepath.Join(repo, "tracked.txt"), filepath.Join(repo, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	files, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	byPath := stopDirtyByPath(files)
	if _, ok := byPath["tracked.txt"]; !ok {
		t.Fatalf("rename元pathが保持列挙にありません: %#v", files)
	}
	if _, ok := byPath["renamed.txt"]; !ok {
		t.Fatalf("rename先pathが保持列挙にありません: %#v", files)
	}
}

// TestCaptureStopDirtyFilesSymlinkはsymlinkをtarget文字列のhashで識別することを固定する。
func TestCaptureStopDirtyFilesSymlink(t *testing.T) {
	repo := initCommittedRepo(t)
	if err := os.Symlink("tracked.txt", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	files, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	link, ok := stopDirtyByPath(files)["link"]
	if !ok {
		t.Fatalf("symlinkが保持列挙にありません: %#v", files)
	}
	if link.WorktreeSHA == "" {
		t.Fatal("symlinkのworktree hashが空です")
	}
}

// TestDescribeStopDirtyDiffScenariosは保持基準比較の全分岐を固定する。
func TestDescribeStopDirtyDiffScenarios(t *testing.T) {
	base := []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w1"}}
	tests := []struct {
		name    string
		stopped []StopDirtyFile
		current []StopDirtyFile
		want    string
	}{
		{name: "一致", stopped: base, current: base, want: ""},
		{
			name:    "内容変化",
			stopped: base,
			current: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w2"}},
			want:    "a.txt(内容変化)",
		},
		{
			name:    "停止後に新規dirty",
			stopped: base,
			current: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w1"}, {Path: "b.txt", WorktreeSHA: "w9"}},
			want:    "b.txt(停止後に新規dirty)",
		},
		{
			name:    "保持対象が消失",
			stopped: base,
			current: []StopDirtyFile{},
			want:    "a.txt(保持対象が消失)",
		},
		{
			name:    "index段階変化",
			stopped: base,
			current: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i2", WorktreeSHA: "w1"}},
			want:    "a.txt(内容変化)",
		},
		{
			name:    "lossless識別子の一致",
			stopped: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w1", IndexIdentity: "ii1", WorktreeIdentity: "wi1"}},
			current: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w1", IndexIdentity: "ii1", WorktreeIdentity: "wi1"}},
			want:    "",
		},
		{
			name:    "lossless識別子だけの変化(mode・type・stage差)",
			stopped: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w1", IndexIdentity: "ii1", WorktreeIdentity: "wi1"}},
			current: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w1", IndexIdentity: "ii1", WorktreeIdentity: "wi2"}},
			want:    "a.txt(内容変化)",
		},
		{
			name:    "lossless識別子を持つ停止基準とlegacyだけの現在",
			stopped: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w1", IndexIdentity: "ii1", WorktreeIdentity: "wi1"}},
			current: []StopDirtyFile{{Path: "a.txt", IndexSHA: "i1", WorktreeSHA: "w1"}},
			want:    "a.txt(内容変化)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DescribeStopDirtyDiff(test.stopped, test.current); got != test.want {
				t.Fatalf("差異 = %q want %q", got, test.want)
			}
		})
	}
}

// gitRetentionOutputWithStdinはstdin付きgit commandを実行してstdoutをtrimして返す。
// conflict index構築のupdate-index --index-infoとblob登録のhash-objectで使う。
func gitRetentionOutputWithStdin(t *testing.T, dir string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, output)
	}
	return strings.TrimSpace(string(output))
}

// stopDirtyFileForは保持列挙から指定pathの識別子を取り出す。
func stopDirtyFileFor(t *testing.T, files []StopDirtyFile, path string) StopDirtyFile {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("保持列挙に%sがありません: %#v", path, files)
	return StopDirtyFile{}
}

// TestCaptureStopDirtyFilesExecBitDriftDetectedはworktree内容が同じままexecutable bitだけ
// 変わったdirty fileを保持変化として検出することを固定する。legacyの内容hashだけでは
// 検出できない軸である。
func TestCaptureStopDirtyFilesExecBitDriftDetected(t *testing.T) {
	repo := initCommittedRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stopped, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repo, "tracked.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	current, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}

	before := stopDirtyFileFor(t, stopped, "tracked.txt")
	after := stopDirtyFileFor(t, current, "tracked.txt")
	if before.WorktreeSHA != after.WorktreeSHA || before.IndexSHA != after.IndexSHA {
		t.Fatalf("chmodだけの変化でlegacy hashが変わっています: %#v -> %#v", before, after)
	}
	if diff := DescribeStopDirtyDiff(stopped, current); !strings.Contains(diff, "tracked.txt(内容変化)") {
		t.Fatalf("executable bit変化を検出していません: %q %#v -> %#v", diff, before, after)
	}
}

// TestCaptureStopDirtyFilesTypeChangeDriftDetectedはregular fileと同じbyte列のsymlink targetへ
// 置き換わったdirty fileを保持変化として検出することを固定する。legacyの内容hashだけでは
// type変更を区別できない軸である。
func TestCaptureStopDirtyFilesTypeChangeDriftDetected(t *testing.T) {
	repo := initCommittedRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("link-target"), 0o644); err != nil {
		t.Fatal(err)
	}
	stopped, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("link-target", filepath.Join(repo, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	current, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}

	before := stopDirtyFileFor(t, stopped, "tracked.txt")
	after := stopDirtyFileFor(t, current, "tracked.txt")
	if before.WorktreeSHA != after.WorktreeSHA {
		t.Fatalf("同じbyte列のはずのworktree hashが変わっています: %#v -> %#v", before, after)
	}
	if diff := DescribeStopDirtyDiff(stopped, current); !strings.Contains(diff, "tracked.txt(内容変化)") {
		t.Fatalf("file type変化を検出していません: %q %#v -> %#v", diff, before, after)
	}
}

// TestCaptureStopDirtyFilesConflictStageDriftDetectedはmerge conflict indexのstage 2/3だけが
// 変わったdirty fileを保持変化として検出することを固定する。legacy IndexSHAは最初のentry
// (stage 1)だけを採用するため、この軸では一致してしまう。
func TestCaptureStopDirtyFilesConflictStageDriftDetected(t *testing.T) {
	repo := initCommittedRepo(t)
	base := gitRetentionOutputWithStdin(t, repo, "base\n", "hash-object", "-w", "--stdin")
	ours1 := gitRetentionOutputWithStdin(t, repo, "ours1\n", "hash-object", "-w", "--stdin")
	ours2 := gitRetentionOutputWithStdin(t, repo, "ours2\n", "hash-object", "-w", "--stdin")
	theirs := gitRetentionOutputWithStdin(t, repo, "theirs\n", "hash-object", "-w", "--stdin")
	if err := os.WriteFile(filepath.Join(repo, "conflict.txt"), []byte("worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stageBase := func(stage2 string) string {
		return strings.Join([]string{
			"100644 " + base + " 1\tconflict.txt",
			"100644 " + stage2 + " 2\tconflict.txt",
			"100644 " + theirs + " 3\tconflict.txt",
			"",
		}, "\n")
	}
	gitRetentionOutputWithStdin(t, repo, stageBase(ours1), "update-index", "--index-info")

	stopped, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	gitRetentionOutputWithStdin(t, repo, stageBase(ours2), "update-index", "--index-info")
	current, err := CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}

	before := stopDirtyFileFor(t, stopped, "conflict.txt")
	after := stopDirtyFileFor(t, current, "conflict.txt")
	if before.IndexSHA != after.IndexSHA || before.WorktreeSHA != after.WorktreeSHA {
		t.Fatalf("stage 2だけの変化でlegacy hashが変わっています: %#v -> %#v", before, after)
	}
	if diff := DescribeStopDirtyDiff(stopped, current); !strings.Contains(diff, "conflict.txt(内容変化)") {
		t.Fatalf("conflict stage変化を検出していません: %q %#v -> %#v", diff, before, after)
	}
}

// TestCaptureStopPatchesWritesRecoveryMaterialsは停止時dirty diffをbinary patch 2種へ
// 保存することを固定する。git失敗時は資材を取り下げてerrorにしない。
func TestCaptureStopPatchesWritesRecoveryMaterials(t *testing.T) {
	repo := initCommittedRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("recovery target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := &StateStore{dir: t.TempDir()}
	if err := CaptureStopPatches(config.AppConfig{RepoRoot: repo}, st); err != nil {
		t.Fatal(err)
	}
	worktreePatch, err := st.Read(stopWorktreePatchFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(worktreePatch, "recovery target") {
		t.Fatalf("worktree patchが停止時diffを含みません: %q", worktreePatch)
	}
	indexPatch, err := st.Read(stopIndexPatchFile)
	if err != nil {
		t.Fatal(err)
	}
	if indexPatch != "" {
		t.Fatalf("indexに変更がない場合のindex patch = %q", indexPatch)
	}

	nonGit := t.TempDir()
	failed := &StateStore{dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(nonGit, "x.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CaptureStopPatches(config.AppConfig{RepoRoot: nonGit}, failed); err != nil {
		t.Fatalf("git失敗がerrorを上げています: %v", err)
	}
	if failed.Exists(stopWorktreePatchFile) || failed.Exists(stopIndexPatchFile) {
		t.Fatal("git失敗時に中途半端なrecovery資材が残っています")
	}
}
