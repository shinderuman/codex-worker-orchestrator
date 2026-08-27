package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	implementationPlanFile    = state.ParentPlanFile
	implementationRulesFile   = state.ParentRulesFile
	implementationTasksDir    = state.ParentTasksDir
	implementationHistoryFile = state.ParentHistoryFile
)

var errParentFileGuardStopped = errors.New("parent-owned file guard stopped workflow")

type parentFileGuard struct {
	files    state.ParentFileStates
	guarded  bool
	planOnly bool
}

type guardSurface struct {
	label         string
	files         string
	eventSuffix   string
	outcomePrefix string
	invariants    string
	targets       string
}

var parentMetadataGuardSurface = guardSurface{
	label:         "親管理implementation metadata",
	files:         "IMPLEMENTATION_RULES.md・IMPLEMENTATION_PLAN.local.md・IMPLEMENTATION_TASKS/配下全file・IMPLEMENTATION_HISTORY.md",
	eventSuffix:   "parent-metadata-check",
	outcomePrefix: "parent_metadata",
	invariants:    "IMPLEMENTATION_RULES.md・IMPLEMENTATION_PLAN.local.md・IMPLEMENTATION_TASKS/配下・IMPLEMENTATION_HISTORY.mdは親Codexだけが編集するparent-managed implementation metadataの単一集合であり、GLM worker/reviewerは編集・生成・復元・削除を行わず更新候補を結果fieldで報告する。model呼出中は集合全体が不変である",
	targets:       "IMPLEMENTATION_RULES.md・IMPLEMENTATION_PLAN.local.md・IMPLEMENTATION_TASKS/配下全file・IMPLEMENTATION_HISTORY.mdの現在内容とgit index/working tree状態、およびPlan ACTIVE欄から解決したACTIVE task file",
}

func (s guardSurface) unavailableOutcome() string { return s.outcomePrefix + "_unavailable" }
func (s guardSurface) missingOutcome() string     { return s.outcomePrefix + "_missing" }
func (s guardSurface) mismatchOutcome() string    { return s.outcomePrefix + "_mismatch" }
func (s guardSurface) violationOutcome() string   { return s.outcomePrefix + "_violation" }
func (s guardSurface) malformedOutcome() string   { return s.outcomePrefix + "_malformed" }
func (s guardSurface) activeUnresolvableOutcome() string {
	return s.outcomePrefix + "_active_unresolvable"
}

func readParentFileState(repoRoot string, name string) (state.ParentFileState, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(name)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state.ParentFileState{Path: name}, nil
		}
		return state.ParentFileState{}, fmt.Errorf("read %s: %w", name, err)
	}
	sum := sha256.Sum256(b)
	return state.ParentFileState{Path: name, Exists: true, SHA256: hex.EncodeToString(sum[:])}, nil
}

func readParentFileStates(repoRoot string) (state.ParentFileStates, error) {
	rootFiles := []string{implementationRulesFile, implementationPlanFile, implementationHistoryFile}
	states := make(state.ParentFileStates, 0, len(rootFiles)+8)
	for _, name := range rootFiles {
		s, err := readParentFileState(repoRoot, name)
		if err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	taskStates, err := readParentTaskFileStates(repoRoot)
	if err != nil {
		return nil, err
	}
	states = append(states, taskStates...)
	sort.Slice(states, func(i, j int) bool { return states[i].Path < states[j].Path })
	return states, nil
}

func readParentTaskFileStates(repoRoot string) (state.ParentFileStates, error) {
	dir := filepath.Join(repoRoot, implementationTasksDir)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", implementationTasksDir, err)
	}
	var states state.ParentFileStates
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		s, readErr := readParentFileState(repoRoot, filepath.ToSlash(rel))
		if readErr != nil {
			return readErr
		}
		states = append(states, s)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate %s: %w", implementationTasksDir, err)
	}
	return states, nil
}

func captureStopParentFiles(repoRoot string) *state.ParentFileStates {
	states, err := readParentFileStates(repoRoot)
	if err != nil {
		return nil
	}
	return &states
}

func parentFileChangeReason(before, after state.ParentFileState) string {
	switch {
	case before.Exists && after.Exists:
		return "内容が変化しました"
	case !before.Exists && after.Exists:
		return "存在しない状態から新規作成されました"
	default:
		return "削除されました"
	}
}

func describeParentFileChanges(before, after state.ParentFileStates) string {
	paths := make(map[string]struct{}, len(before)+len(after))
	for _, s := range before {
		paths[s.Path] = struct{}{}
	}
	for _, s := range after {
		paths[s.Path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	reasons := make([]string, 0, len(ordered))
	for _, p := range ordered {
		b := state.FindParentFileState(before, p)
		a := state.FindParentFileState(after, p)
		if b == a {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("%s(%s)", p, parentFileChangeReason(b, a)))
	}
	return strings.Join(reasons, ", ")
}

func quietWhenParentFileGuardStopped(err error) error {
	if errors.Is(err, errParentFileGuardStopped) {
		return nil
	}
	return err
}

type parentFileTracking int

const (
	parentFileTrackingTracked parentFileTracking = iota + 1
	parentFileTrackingUntracked
	parentFileTrackingOutsideGit
)

func gitWorktreePresent(repoRoot string) (bool, error) {
	for dir := repoRoot; ; dir = filepath.Dir(dir) {
		_, err := os.Stat(filepath.Join(dir, ".git"))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("stat %s: %w", filepath.Join(dir, ".git"), err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, nil
		}
	}
}

func classifyParentFileTracking(repoRoot string, name string) (parentFileTracking, error) {
	insideGit, err := gitWorktreePresent(repoRoot)
	if err != nil {
		return 0, err
	}
	if !insideGit {
		return parentFileTrackingOutsideGit, nil
	}
	output, err := exec.Command("git", "-C", repoRoot, "ls-files", "--", name).Output()
	if err != nil {
		return 0, fmt.Errorf("git ls-files: %w", err)
	}
	if strings.TrimSpace(string(output)) != "" {
		return parentFileTrackingTracked, nil
	}
	return parentFileTrackingUntracked, nil
}

func missingTrackedTaskFiles(repoRoot string, worktreeStates state.ParentFileStates) ([]string, error) {
	insideGit, err := gitWorktreePresent(repoRoot)
	if err != nil {
		return nil, err
	}
	if !insideGit {
		return nil, nil
	}
	output, err := exec.Command("git", "-C", repoRoot, "ls-files", "-z", "--", implementationTasksDir).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	present := make(map[string]bool, len(worktreeStates))
	for _, s := range worktreeStates {
		if s.Exists {
			present[s.Path] = true
		}
	}
	var missing []string
	for _, p := range strings.Split(string(output), "\x00") {
		if p == "" || present[p] {
			continue
		}
		missing = append(missing, p)
	}
	return missing, nil
}

func (w *Workflow) captureParentFileGuard(role state.SessionRole) (parentFileGuard, bool, error) {
	if role != state.WorkerRole {
		return parentFileGuard{}, false, nil
	}
	plan, err := readParentFileState(w.config.RepoRoot, implementationPlanFile)
	if err != nil {
		return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.unavailableOutcome(), "plan file baseline取得失敗のため不変性を確認できません", err)
	}
	if !plan.Exists {
		tracking, trackErr := classifyParentFileTracking(w.config.RepoRoot, implementationPlanFile)
		switch {
		case trackErr != nil:
			return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.unavailableOutcome(), "plan fileのGit追跡判定に失敗したため欠損を安全に扱えません", trackErr)
		case tracking == parentFileTrackingTracked:
			return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.missingOutcome(), "Git indexで追跡されている"+implementationPlanFile+"がworking treeへ存在しません", nil)
		}

		return parentFileGuard{files: state.ParentFileStates{{Path: implementationPlanFile}}, guarded: true, planOnly: true}, false, nil
	}
	states, err := readParentFileStates(w.config.RepoRoot)
	if err != nil {
		return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.unavailableOutcome(), "親管理metadata baseline取得失敗のため不変性を確認できません", err)
	}
	for _, name := range []string{implementationRulesFile, implementationHistoryFile} {
		if state.FindParentFileState(states, name).Exists {
			continue
		}
		tracking, trackErr := classifyParentFileTracking(w.config.RepoRoot, name)
		switch {
		case trackErr != nil:
			return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.unavailableOutcome(), name+"のGit追跡判定に失敗したため欠損を安全に扱えません", trackErr)
		case tracking == parentFileTrackingTracked:
			return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.missingOutcome(), "Git indexで追跡されている"+name+"がworking treeへ存在しません", nil)
		}
	}
	missing, err := missingTrackedTaskFiles(w.config.RepoRoot, states)
	if err != nil {
		return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.unavailableOutcome(), "IMPLEMENTATION_TASKS配下のGit追跡判定に失敗したため欠損を安全に扱えません", err)
	}
	if len(missing) > 0 {
		return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.missingOutcome(), "Git indexで追跡されているIMPLEMENTATION_TASKS配下fileがworking treeへ存在しません: "+strings.Join(missing, ", "), nil)
	}
	if activePath := w.readActiveTaskState(); activePath != "" && !activeTaskFileExists(w.config.RepoRoot, activePath) {
		return parentFileGuard{}, true, w.failClosedParentFileGuard("parent-metadata-capture", parentMetadataGuardSurface, parentMetadataGuardSurface.missingOutcome(), "task開始時に固定したACTIVE task file "+activePath+"がworking treeへ存在しません", nil)
	}
	return parentFileGuard{files: states, guarded: true}, false, nil
}

func (w *Workflow) verifyParentFileAfterCall(
	checkpoint state.ResumeCheckpoint,
	before parentFileGuard,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	runErr error,
	outputPath string,
) (bool, error) {
	if checkpoint.Role != state.WorkerRole || !before.guarded {
		return false, nil
	}
	after, err := readParentFileStates(w.config.RepoRoot)
	if err != nil {
		w.recordModelCall(checkpoint, runResult, startedAt, completedAt, parentMetadataGuardSurface.unavailableOutcome(), "", err, outputPath, callDiagnostics{})
		return true, w.failClosedParentFileGuard(checkpoint.Phase, parentMetadataGuardSurface, parentMetadataGuardSurface.unavailableOutcome(), "親管理metadata終了状態取得失敗のため不変性を確認できません", err)
	}
	if before.planOnly {

		if plan := state.FindParentFileState(after, implementationPlanFile); plan != state.FindParentFileState(before.files, implementationPlanFile) {
			violation := fmt.Errorf("worker呼出開始前に対し親管理implementation metadataが変化しました: %s(%s)", implementationPlanFile, parentFileChangeReason(state.FindParentFileState(before.files, implementationPlanFile), plan))
			if runErr != nil {
				violation = fmt.Errorf("%w; 呼出error: %w", violation, runErr)
			}
			w.recordModelCall(checkpoint, runResult, startedAt, completedAt, parentMetadataGuardSurface.violationOutcome(), "", violation, outputPath, callDiagnostics{})
			return true, w.failClosedParentFileGuard(checkpoint.Phase, parentMetadataGuardSurface, parentMetadataGuardSurface.mismatchOutcome(), violation.Error(), nil)
		}
		return false, nil
	}
	if state.SameParentFileStates(before.files, after) {
		return false, nil
	}
	violation := fmt.Errorf("worker呼出開始前に対し親管理implementation metadataが変化しました: %s", describeParentFileChanges(before.files, after))
	if runErr != nil {
		violation = fmt.Errorf("%w; 呼出error: %w", violation, runErr)
	}
	w.recordModelCall(checkpoint, runResult, startedAt, completedAt, parentMetadataGuardSurface.violationOutcome(), "", violation, outputPath, callDiagnostics{})
	return true, w.failClosedParentFileGuard(checkpoint.Phase, parentMetadataGuardSurface, parentMetadataGuardSurface.mismatchOutcome(), violation.Error(), nil)
}

func (w *Workflow) failClosedParentFileGuard(phase string, surface guardSurface, outcome string, reason string, cause error) error {
	w.recordParentFileEvent(phase, surface, outcome, reason, cause)
	if err := w.state.ClearResumeCheckpoint(); err != nil {
		return err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		return err
	}
	if cause != nil {
		reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	if err := w.emitResult(parentFileFailClosedResult(phase, surface, reason)); err != nil {
		return err
	}
	return errParentFileGuardStopped
}

func (w *Workflow) failClosedActiveTaskResolution(phase string, cause error) error {
	return w.failClosedParentFileGuard(phase, parentMetadataGuardSurface, parentMetadataGuardSurface.activeUnresolvableOutcome(), "PlanのACTIVE欄からACTIVE task fileを一意に解決できません", cause)
}

func (w *Workflow) failClosedDecisionRejection(phase string, outcome string, reason string, cause error) error {
	w.recordParentFileEvent(phase, parentMetadataGuardSurface, outcome, reason, cause)
	if err := w.state.ClearResumeCheckpoint(); err != nil {
		return err
	}
	if cause != nil {
		reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	if err := w.emitResult(parentFileFailClosedResult(phase, parentMetadataGuardSurface, reason)); err != nil {
		return err
	}
	return errParentFileGuardStopped
}

func (w *Workflow) recordParentFileEvent(phase string, surface guardSurface, outcome string, reason string, cause error) {
	now := w.now().UTC()
	errorText := reason
	if cause != nil {
		errorText = fmt.Sprintf("%s: %v", reason, cause)
	}
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       phase + "-" + surface.eventSuffix,
		Role:        state.WorkerRole,
		Outcome:     outcome,
		Error:       boundedText(errorText, packet.MaxDiagnosticBytes),
	})
}

func parentFileFailClosedResult(phase string, surface guardSurface, reason string) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             fmt.Sprintf("worker呼出(%s)の前後で親管理implementation metadata(%s)の不変を確認できず、reviewerを呼ばずSol確認へ昇格", phase, surface.files),
		RequirementCoverage: "親管理metadata読み取り専用契約、またはACTIVE task file要求正本の特定を機械強制できなかったため親Codexが直接確認する必要あり",
		Invariants:          surface.invariants,
		TestEvidence:        "worker呼出開始前後の親管理metadata集合存在・内容比較、Plan ACTIVE欄解決で欠損・不一致・非一意・読込失敗を検出",
		Issues:              reason,
		ResidualRisk:        "親管理metadataの現在状態(変更・生成・削除・欠損)はorchestratorが復元せずそのまま残っている",
		Targets:             []string{surface.targets},
		SolQuestion:         "変更・欠損した親管理metadata内容とACTIVE task fileの取扱い(親Codexによる再編集・復元)をSolが判断する",
	}
}
