package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type ParentFileState struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256"`
}

type ParentFileStates []ParentFileState

type GitSnapshot struct {
	Head        string `json:"head"`
	IndexDigest string `json:"index_digest"`

	WorktreeDigest string `json:"worktree_digest"`

	WorktreeDigestExcludingParent string `json:"worktree_digest_excluding_parent,omitempty"`

	ParentFiles *ParentFileStates `json:"parent_files,omitempty"`
}

type SnapshotStage string

type SnapshotComparison struct {
	Stage         SnapshotStage `json:"stage"`
	Matched       bool          `json:"matched"`
	HeadMatch     bool          `json:"head_match"`
	IndexMatch    bool          `json:"index_match"`
	WorktreeMatch bool          `json:"worktree_match"`

	ParentUpdateAccepted bool   `json:"parent_update_accepted,omitempty"`
	Reason               string `json:"reason,omitempty"`
}

type SnapshotDigest struct {
	Head           string `json:"head,omitempty"`
	IndexDigest    string `json:"index_digest,omitempty"`
	WorktreeDigest string `json:"worktree_digest,omitempty"`

	WorktreeDigestExcludingParent string `json:"worktree_digest_excluding_parent,omitempty"`
}

type SnapshotDiagnostic struct {
	Stage        string          `json:"stage"`
	Previous     *SnapshotDigest `json:"previous,omitempty"`
	Current      *SnapshotDigest `json:"current,omitempty"`
	Matched      *bool           `json:"matched,omitempty"`
	MismatchAxis string          `json:"mismatch_axis,omitempty"`
	Reason       string          `json:"reason,omitempty"`
}

const (
	workerEndSnapshotFile       = "snapshot-worker-end.json"
	reviewStartSnapshotFile     = "snapshot-review-start.json"
	reportOnlyStartSnapshotFile = "snapshot-report-only-start.json"
	poCStartSnapshotFile        = "snapshot-poc-start.json"
	snapshotComparisonFile      = "snapshot-comparison.json"
)

const (
	ParentRulesFile   = "IMPLEMENTATION_RULES.md"
	ParentPlanFile    = "IMPLEMENTATION_PLAN.local.md"
	ParentTasksDir    = "IMPLEMENTATION_TASKS"
	ParentHistoryFile = "IMPLEMENTATION_HISTORY.md"
)

const (
	SnapshotStageWorkerEnd       SnapshotStage = "worker-end"
	SnapshotStageReviewStart     SnapshotStage = "review-start"
	SnapshotStageReviewResume    SnapshotStage = "review-resume"
	SnapshotStageReviewEnd       SnapshotStage = "review-end"
	SnapshotStageReportOnlyStart SnapshotStage = "report-only-start"
	SnapshotStageReportOnlyEnd   SnapshotStage = "report-only-end"
	SnapshotStagePoCStart        SnapshotStage = "poc-start"
	SnapshotStagePoCEnd          SnapshotStage = "poc-end"
)

var parentManagedFiles = []string{ParentRulesFile, ParentPlanFile, ParentHistoryFile}

func SameParentFileStates(a, b ParentFileStates) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func FindParentFileState(states ParentFileStates, path string) ParentFileState {
	for _, s := range states {
		if s.Path == path {
			return s
		}
	}
	return ParentFileState{Path: path}
}

func CaptureGitSnapshot(repoRoot string) (GitSnapshot, error) {
	head, err := captureSnapshotHead(repoRoot)
	if err != nil {
		return GitSnapshot{}, err
	}
	indexDigest, err := captureSnapshotIndexDigest(repoRoot)
	if err != nil {
		return GitSnapshot{}, err
	}
	worktreeDigest, excludingParent, err := captureSnapshotWorktreeDigest(repoRoot)
	if err != nil {
		return GitSnapshot{}, err
	}
	return GitSnapshot{
		Head:                          head,
		IndexDigest:                   indexDigest,
		WorktreeDigest:                worktreeDigest,
		WorktreeDigestExcludingParent: excludingParent,
	}, nil
}

func captureSnapshotHead(repoRoot string) (string, error) {
	output, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", nil
		}
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func captureSnapshotIndexDigest(repoRoot string) (string, error) {
	output, err := exec.Command("git", "-C", repoRoot, "ls-files", "-s").Output()
	if err != nil {
		return "", fmt.Errorf("git ls-files: %w", err)
	}
	sum := sha256.Sum256(output)
	return hex.EncodeToString(sum[:]), nil
}

func captureSnapshotWorktreeDigest(repoRoot string) (string, string, error) {
	full, err := captureWorktreeDigestVariant(repoRoot, nil)
	if err != nil {
		return "", "", err
	}
	excluding, err := captureWorktreeDigestVariant(repoRoot, ParentExcludePathspecs())
	if err != nil {
		return "", "", err
	}
	return full, excluding, nil
}

func ParentExcludePathspecs() []string {
	specs := make([]string, 0, len(parentManagedFiles)+1)
	for _, name := range parentManagedFiles {
		specs = append(specs, ":(top,exclude)"+name)
	}
	specs = append(specs, ":(top,exclude)"+ParentTasksDir)
	return specs
}

func captureWorktreeDigestVariant(repoRoot string, excludePathspecs []string) (string, error) {
	diffArgs := []string{"diff", "--binary", "--no-ext-diff"}
	untrackedArgs := []string{"ls-files", "-z", "--others", "--exclude-standard"}
	if len(excludePathspecs) > 0 {
		diffArgs = append(diffArgs, "--")
		diffArgs = append(diffArgs, excludePathspecs...)
		untrackedArgs = append(untrackedArgs, "--")
		untrackedArgs = append(untrackedArgs, excludePathspecs...)
	}
	diffOutput, err := gitSnapshotOutput(repoRoot, diffArgs)
	if err != nil {
		return "", err
	}
	untrackedOutput, err := gitSnapshotOutput(repoRoot, untrackedArgs)
	if err != nil {
		return "", err
	}
	return buildWorktreeDigest(diffOutput, untrackedOutput, repoRoot)
}

func gitSnapshotOutput(repoRoot string, args []string) ([]byte, error) {
	output, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return output, nil
}

func buildWorktreeDigest(diffOutput, untrackedOutput []byte, repoRoot string) (string, error) {
	hasher := sha256.New()
	hasher.Write([]byte("diff\n"))
	hasher.Write(diffOutput)
	hasher.Write([]byte("\nuntracked\n"))

	paths := strings.Split(strings.TrimRight(string(untrackedOutput), "\x00"), "\x00")
	sort.Strings(paths)
	for _, path := range paths {
		if path == "" {
			continue
		}
		absPath, err := joinWithinRoot(repoRoot, path)
		if err != nil {
			return "", fmt.Errorf("untracked %s: %w", path, err)
		}
		info, err := os.Lstat(absPath)
		if err != nil {
			return "", fmt.Errorf("untracked file %sをstatできません: %w", path, err)
		}
		hasher.Write([]byte(path))
		hasher.Write([]byte{0})
		if err := hashUntrackedEntry(hasher, absPath, info.Mode()); err != nil {
			return "", err
		}
		hasher.Write([]byte("\n"))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func hashUntrackedEntry(hasher hash.Hash, absPath string, mode os.FileMode) error {
	switch {
	case mode.IsRegular():
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("untracked file %sを読めません: %w", absPath, err)
		}
		sum := sha256.Sum256(content)
		hasher.Write([]byte("regular\x00"))
		hasher.Write([]byte(hex.EncodeToString(sum[:])))
	case mode&os.ModeSymlink != 0:
		target, err := os.Readlink(absPath)
		if err != nil {
			return fmt.Errorf("untracked symlink %sを読めません: %w", absPath, err)
		}
		sum := sha256.Sum256([]byte(target))
		hasher.Write([]byte("symlink\x00"))
		hasher.Write([]byte(hex.EncodeToString(sum[:])))
	default:
		return fmt.Errorf("untracked file %sは取り扱えないfile type %sです", absPath, mode.Type())
	}
	return nil
}

func joinWithinRoot(root, rel string) (string, error) {
	abs := filepath.Join(root, rel)
	relToRoot, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if relToRoot == "." || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("pathがrepository境界を越えています: %s", rel)
	}
	return abs, nil
}

func EqualGitSnapshot(a, b GitSnapshot) bool {
	return a.Head == b.Head && a.IndexDigest == b.IndexDigest && a.WorktreeDigest == b.WorktreeDigest
}

func CompareGitSnapshot(previous, current GitSnapshot, stage SnapshotStage, reason string) SnapshotComparison {
	return SnapshotComparison{
		Stage:         stage,
		Matched:       EqualGitSnapshot(previous, current),
		HeadMatch:     previous.Head == current.Head,
		IndexMatch:    previous.IndexDigest == current.IndexDigest,
		WorktreeMatch: previous.WorktreeDigest == current.WorktreeDigest,
		Reason:        reason,
	}
}

func writeSnapshot(path string, snap GitSnapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshotをJSON化できません: %w", err)
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

func readSnapshot(path string) (GitSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GitSnapshot{}, err
	}
	var snap GitSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return GitSnapshot{}, fmt.Errorf("snapshotを読めません: %w", err)
	}
	return snap, nil
}

func (s *StateStore) SaveWorkerEndSnapshot(snap GitSnapshot) error {
	if err := writeSnapshot(s.Path(workerEndSnapshotFile), snap); err != nil {
		return fmt.Errorf("worker-end snapshotを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadWorkerEndSnapshot() (GitSnapshot, error) {
	return readSnapshot(s.Path(workerEndSnapshotFile))
}

func (s *StateStore) SaveReviewStartSnapshot(snap GitSnapshot) error {
	if err := writeSnapshot(s.Path(reviewStartSnapshotFile), snap); err != nil {
		return fmt.Errorf("review-start snapshotを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadReviewStartSnapshot() (GitSnapshot, error) {
	return readSnapshot(s.Path(reviewStartSnapshotFile))
}

func (s *StateStore) SaveReportOnlyStartSnapshot(snap GitSnapshot) error {
	if err := writeSnapshot(s.Path(reportOnlyStartSnapshotFile), snap); err != nil {
		return fmt.Errorf("report-only開始前snapshotを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadReportOnlyStartSnapshot() (GitSnapshot, error) {
	return readSnapshot(s.Path(reportOnlyStartSnapshotFile))
}

func (s *StateStore) SavePoCStartSnapshot(snap GitSnapshot) error {
	if err := writeSnapshot(s.Path(poCStartSnapshotFile), snap); err != nil {
		return fmt.Errorf("PoC開始前snapshotを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadPoCStartSnapshot() (GitSnapshot, error) {
	return readSnapshot(s.Path(poCStartSnapshotFile))
}

func (s *StateStore) SaveSnapshotComparison(comparison SnapshotComparison) error {
	data, err := json.MarshalIndent(comparison, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshot comparisonをJSON化できません: %w", err)
	}
	return writeFileAtomic(s.Path(snapshotComparisonFile), append(data, '\n'), 0o600)
}

func (s *StateStore) LoadSnapshotComparison() (SnapshotComparison, error) {
	data, err := os.ReadFile(s.Path(snapshotComparisonFile))
	if err != nil {
		return SnapshotComparison{}, err
	}
	var comparison SnapshotComparison
	if err := json.Unmarshal(data, &comparison); err != nil {
		return SnapshotComparison{}, fmt.Errorf("snapshot comparisonを読めません: %w", err)
	}
	return comparison, nil
}

func snapshotDigest(s GitSnapshot) SnapshotDigest {
	return SnapshotDigest{
		Head:                          s.Head,
		IndexDigest:                   s.IndexDigest,
		WorktreeDigest:                s.WorktreeDigest,
		WorktreeDigestExcludingParent: s.WorktreeDigestExcludingParent,
	}
}

func MismatchAxis(c SnapshotComparison) string {
	if c.Matched {
		return ""
	}
	var axes []string
	if !c.HeadMatch {
		axes = append(axes, "head")
	}
	if !c.IndexMatch {
		axes = append(axes, "index")
	}
	if !c.WorktreeMatch {
		axes = append(axes, "worktree")
	}
	return strings.Join(axes, ",")
}

func BuildSnapshotDiagnostic(stage SnapshotStage, previous, current GitSnapshot, comparison SnapshotComparison, reason string) SnapshotDiagnostic {
	diag := SnapshotDiagnostic{Stage: string(stage), Reason: reason}
	if !isEmptySnapshot(previous) {
		d := snapshotDigest(previous)
		diag.Previous = &d
	}
	if !isEmptySnapshot(current) {
		d := snapshotDigest(current)
		diag.Current = &d
	}
	if diag.Previous != nil && diag.Current != nil {
		matched := comparison.Matched
		diag.Matched = &matched
		diag.MismatchAxis = MismatchAxis(comparison)
	}
	return diag
}

func isEmptySnapshot(s GitSnapshot) bool {
	return s.Head == "" && s.IndexDigest == "" && s.WorktreeDigest == ""
}
