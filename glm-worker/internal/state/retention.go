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
// IndexIdentity・WorktreeIdentityはmode・file type・conflict stageを含むlossless識別子で、
// 旧binaryのcheckpointでは空のまま比較から除外されlegacy 2 hashだけで照合される。
type StopDirtyFile struct {
	Path             string `json:"path"`
	IndexSHA         string `json:"index_sha"`
	WorktreeSHA      string `json:"worktree_sha"`
	IndexIdentity    string `json:"index_identity,omitempty"`
	WorktreeIdentity string `json:"worktree_identity,omitempty"`
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
		worktreeSHA, worktreeIdentity, err := worktreeContentHash(repoRoot, path)
		if err != nil {
			return nil, err
		}
		files = append(files, StopDirtyFile{
			Path:             path,
			IndexSHA:         indexHashes[path].BlobSHA,
			WorktreeSHA:      worktreeSHA,
			IndexIdentity:    indexHashes[path].Identity,
			WorktreeIdentity: worktreeIdentity,
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

// indexEntryIdentityは1 path分のindex識別子。BlobSHAは最初のentryのblob hash(旧保持基準
// との比較互換)、Identityは同pathの全stage entryのmode・blob・stageを並べたhashであり、
// merge conflictのstage構成とexecutable modeの変化も識別する。
type indexEntryIdentity struct {
	BlobSHA  string
	Identity string
}

// indexBlobHashesは親管理metadata除外のindex entry識別子をpath毎に返す。untracked fileや
// worktree削除はmapに現れない。
func indexBlobHashes(repoRoot string) (map[string]indexEntryIdentity, error) {
	args := append([]string{"-C", repoRoot, "ls-files", "-s", "-z", "--"}, ParentExcludePathspecs()...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	hashes := make(map[string]indexEntryIdentity)
	entryByPath := make(map[string]*strings.Builder)
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
		builder, seen := entryByPath[path]
		if !seen {
			builder = &strings.Builder{}
			entryByPath[path] = builder
			hashes[path] = indexEntryIdentity{BlobSHA: meta[1]}
		}
		builder.WriteString(record[:tab])
		builder.WriteByte(0)
	}
	for path, builder := range entryByPath {
		identity := hashes[path]
		sum := sha256.Sum256([]byte(builder.String()))
		identity.Identity = hex.EncodeToString(sum[:])
		hashes[path] = identity
	}
	return hashes, nil
}

// worktreeContentHashはworking treeの実内容hashと、type discriminator・git意味のmode
// (executable bit)・内容hashを含むlossless識別子を返す。内容hashだけではregular fileと
// 同じbyte列のsymlink target・executable bitの変化を区別できないため、保持照合は識別子
// を使う。symlinkはtarget文字列だけを対象とし、FIFO・device・socket等はhangや無制限読込を
// 避けるため失敗にする。これはsnapshotのuntracked列挙と同じ制約である。worktree側削除は
// 両方とも空文字を返す。
func worktreeContentHash(repoRoot string, rel string) (string, string, error) {
	absPath, err := joinWithinRoot(repoRoot, rel)
	if err != nil {
		return "", "", fmt.Errorf("dirty file %s: %w", rel, err)
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("dirty file %sをstatできません: %w", rel, err)
	}

	hasher := sha256.New()
	identity := sha256.New()
	switch {
	case info.Mode().IsRegular():
		content, err := os.ReadFile(absPath)
		if err != nil {
			return "", "", fmt.Errorf("dirty file %sを読めません: %w", rel, err)
		}
		hasher.Write(content)
		identity.Write([]byte("regular\x00"))
		if info.Mode().Perm()&0o100 != 0 {
			identity.Write([]byte("exec\x00"))
		} else {
			identity.Write([]byte("noexec\x00"))
		}
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(absPath)
		if err != nil {
			return "", "", fmt.Errorf("dirty symlink %sを読めません: %w", rel, err)
		}
		hasher.Write([]byte(target))
		identity.Write([]byte("symlink\x00"))
	default:
		return "", "", fmt.Errorf("dirty file %sは取り扱えないfile type %sです", rel, info.Mode().Type())
	}
	contentSHA := hex.EncodeToString(hasher.Sum(nil))
	identity.Write([]byte(contentSHA))
	return contentSHA, hex.EncodeToString(identity.Sum(nil)), nil
}

// DescribeStopDirtyDiffは停止時と現在の保持基準の差異を1行へ要約する。一致時は空文字。
// 停止時基準がlossless識別子(旧binaryの停止)を持つときだけにmode・type・stage差も比較し、
// 旧形式checkpointはlegacy 2 hashの比較のまま再開できる。
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
		case !sameStopDirtyFile(before, after):
			changed = append(changed, path+"(内容変化)")
		}
	}
	if len(changed) == 0 {
		return ""
	}
	sort.Strings(changed)
	return strings.Join(changed, ", ")
}

func sameStopDirtyFile(before, after StopDirtyFile) bool {
	if before.IndexIdentity != "" || before.WorktreeIdentity != "" {
		return before.IndexIdentity == after.IndexIdentity && before.WorktreeIdentity == after.WorktreeIdentity
	}
	return before.IndexSHA == after.IndexSHA && before.WorktreeSHA == after.WorktreeSHA
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
