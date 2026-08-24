package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

const (
	stopWorktreePatchFile = "stop-worktree.patch"
	stopIndexPatchFile    = "stop-index.patch"
)

// StopDirtyFileは--stop停止時点でHEADに対してdirty/untrackedだった非親管理file 1件の
// 保持識別子。IndexSHAはgit index上のblob hash(untrackedは空)、WorktreeSHAは同じ関数で
// 計算したworking tree内容のhash(worktree側削除は空)であり、停止時とresume時の2点間で
// path集合とhash組が一致すれば元taskの未commit作業がbyte保持されていることを意味する。
type StopDirtyFile struct {
	Path        string `json:"path"`
	IndexSHA    string `json:"index_sha"`
	WorktreeSHA string `json:"worktree_sha"`
}

// CaptureStopDirtyFilesはrepoRootの親管理metadata除外dirty/untracked状態を停止時保持の
// 基準として列挙する。git失敗・取り扱えないfile typeはerrorにし、呼出元で保持情報欠損の
// fail closedへ流す。
func CaptureStopDirtyFiles(repoRoot string) ([]StopDirtyFile, error) {
	paths, err := dirtyStatusPaths(repoRoot)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return []StopDirtyFile{}, nil
	}
	indexHashes, err := indexBlobHashes(repoRoot)
	if err != nil {
		return nil, err
	}

	files := make([]StopDirtyFile, 0, len(paths))
	for _, path := range paths {
		worktreeSHA, err := worktreeContentHash(repoRoot, path)
		if err != nil {
			return nil, err
		}
		files = append(files, StopDirtyFile{
			Path:        path,
			IndexSHA:    indexHashes[path],
			WorktreeSHA: worktreeSHA,
		})
	}
	return files, nil
}

// dirtyStatusPathsは親管理metadata集合を除外した porcelain v1 -z のpath集合を返す。
// rename/copyの2 record分(orig・new)も両pathとも列挙へ含め、停止時・再観測時で同じ
// 規則を通すことでhash組比較をpath集合の一致だけに載せる。
func dirtyStatusPaths(repoRoot string) ([]string, error) {
	args := append([]string{"-C", repoRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--"}, ParentExcludePathspecs()...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}

	records := strings.Split(strings.TrimRight(string(output), "\x00"), "\x00")
	paths := make([]string, 0, len(records))
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if len(record) < 4 {
			return nil, fmt.Errorf("git status recordが解析できません: %q", record)
		}
		paths = append(paths, record[3:])
		if record[0] == 'R' || record[0] == 'C' {
			i++
			if i >= len(records) || records[i] == "" {
				return nil, fmt.Errorf("git status rename/copyのorig pathが解析できません: %q", record)
			}
			paths = append(paths, records[i])
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// indexBlobHashesは親管理metadata除外のindex上blob hashをpath毎に返す。merge conflict等で
// stage 0以外のentryだけが存在するpathも識別できるよう、最初に現れたentryを採用する。
func indexBlobHashes(repoRoot string) (map[string]string, error) {
	args := append([]string{"-C", repoRoot, "ls-files", "-s", "-z", "--"}, ParentExcludePathspecs()...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	hashes := make(map[string]string)
	for _, record := range strings.Split(strings.TrimRight(string(output), "\x00"), "\x00") {
		if record == "" {
			continue
		}
		tab := strings.IndexByte(record, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("git ls-files recordが解析できません: %q", record)
		}
		meta := strings.SplitN(record[:tab], " ", 3)
		path := record[tab+1:]
		if len(meta) != 3 || path == "" {
			return nil, fmt.Errorf("git ls-files recordが解析できません: %q", record)
		}
		if _, seen := hashes[path]; !seen {
			hashes[path] = meta[1]
		}
	}
	return hashes, nil
}

// worktreeContentHashはworking treeの実内容をhashする。symlinkはtarget文字列だけを
// 対象とし、FIFO・device・socket等はhangや無制限読込を避けるため失敗にする。これは
// snapshotのuntracked列挙と同じ制約である。
func worktreeContentHash(repoRoot string, rel string) (string, error) {
	absPath, err := joinWithinRoot(repoRoot, rel)
	if err != nil {
		return "", fmt.Errorf("dirty file %s: %w", rel, err)
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("dirty file %sをstatできません: %w", rel, err)
	}

	hasher := sha256.New()
	switch {
	case info.Mode().IsRegular():
		content, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("dirty file %sを読めません: %w", rel, err)
		}
		hasher.Write(content)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(absPath)
		if err != nil {
			return "", fmt.Errorf("dirty symlink %sを読めません: %w", rel, err)
		}
		hasher.Write([]byte(target))
	default:
		return "", fmt.Errorf("dirty file %sは取り扱えないfile type %sです", rel, info.Mode().Type())
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// DescribeStopDirtyDiffは停止時と現在の保持基準の差異を1行へ要約する。一致時は空文字。
func DescribeStopDirtyDiff(stopped, current []StopDirtyFile) string {
	stoppedByPath := stopDirtyFileByPath(stopped)
	currentByPath := stopDirtyFileByPath(current)

	var changed []string
	for _, path := range stopDirtyPaths(stoppedByPath, currentByPath) {
		before, inBefore := stoppedByPath[path]
		after, inAfter := currentByPath[path]
		switch {
		case !inBefore:
			changed = append(changed, path+"(停止後に新規dirty)")
		case !inAfter:
			changed = append(changed, path+"(保持対象が消失)")
		case before != after:
			changed = append(changed, path+"(内容変化)")
		}
	}
	if len(changed) == 0 {
		return ""
	}
	sort.Strings(changed)
	return strings.Join(changed, ", ")
}

func stopDirtyFileByPath(files []StopDirtyFile) map[string]StopDirtyFile {
	byPath := make(map[string]StopDirtyFile, len(files))
	for _, file := range files {
		byPath[file.Path] = file
	}
	return byPath
}

func stopDirtyPaths(groups ...map[string]StopDirtyFile) []string {
	seen := make(map[string]struct{})
	for _, group := range groups {
		for path := range group {
			seen[path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// CaptureStopPatchesは--stop停止時点のtracked diffを親Codexのconflict recovery資材として
// stateへ保存する。untracked file本文はpatchに含まれず、保持はStopDirtyFilesのhash検証のみで
// 親が別途保持する原本なしには復元できない。git取得失敗時はbaselineと同じく取り下げてerrorとはしない。
func CaptureStopPatches(cfg config.AppConfig, st *StateStore) error {
	commands := []struct {
		name string
		args []string
	}{
		{name: stopWorktreePatchFile, args: []string{"diff", "--binary", "--no-ext-diff"}},
		{name: stopIndexPatchFile, args: []string{"diff", "--cached", "--binary", "--no-ext-diff"}},
	}

	for _, item := range commands {
		command := exec.Command("git", item.args...)
		command.Dir = cfg.RepoRoot
		output, err := command.Output()
		if err != nil {
			return st.Remove(stopWorktreePatchFile, stopIndexPatchFile)
		}
		if err := st.Write(item.name, string(output)); err != nil {
			return err
		}
	}
	return nil
}
