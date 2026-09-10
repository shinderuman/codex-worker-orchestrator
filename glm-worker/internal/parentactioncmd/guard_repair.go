package parentactioncmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeResumeWithGuardRepair(cfg config.AppConfig, stdout, stderr io.Writer, extraEnv []string) error {
	st := state.AttachStateStore(cfg)
	before, beforeErr := st.LoadGuardRepairRecord()

	var initialStdout, initialStderr bytes.Buffer
	initialEnv := appendEnv(extraEnv, state.GuardRepairParentActionEnv, state.GuardRepairParentActionResume)
	initialErr := runWorker(cfg.RepoRoot, []string{"--resume"}, nil, &initialStdout, &initialStderr, initialEnv)
	if initialErr == nil {
		copyOutput(stdout, &initialStdout)
		copyOutput(stderr, &initialStderr)
		return nil
	}

	record, err := st.LoadGuardRepairRecord()
	if err != nil || record.TaskID != st.ReadOr("task.id", "") {
		copyOutput(stdout, &initialStdout)
		copyOutput(stderr, &initialStderr)
		return initialErr
	}
	if beforeErr == nil && sameGuardRepairRecord(before, record) && before.Status == state.GuardRepairRequested && record.Status == state.GuardRepairRequested {
		copyOutput(stdout, &initialStdout)
		copyOutput(stderr, &initialStderr)
		return fmt.Errorf("guard repair strategy already requested for unchanged failure and evidence: %w", initialErr)
	}

	switch record.Status {
	case state.GuardRepairRequested:
		record, err = performBoundedGuardRepair(cfg, st, record)
		if err != nil {
			return errors.Join(initialErr, err)
		}
	case state.GuardRepairReady:
		currentDigest, digestErr := guardrepair.RelevantDigest(cfg.RepoRoot)
		if digestErr != nil || currentDigest != record.RepairedDigest {
			return errors.Join(initialErr, fmt.Errorf("guard repair ready state no longer matches repaired source"))
		}
	case state.GuardRepairRunning:
		return errors.Join(initialErr, fmt.Errorf("guard repair strategy is already running; unchanged recovery is not repeated"))
	case state.GuardRepairFailed:
		return errors.Join(initialErr, fmt.Errorf("guard repair strategy already failed for this evidence; unchanged recovery is not repeated"))
	case state.GuardRepairComplete:
		return errors.Join(initialErr, fmt.Errorf("guard repair is already complete but the original task is still not resumable"))
	default:
		return errors.Join(initialErr, fmt.Errorf("unsupported guard repair status %q", record.Status))
	}

	worker, cleanup, err := buildGuardRepairWorker(cfg)
	if err != nil {
		return markGuardRepairFailed(st, record, errors.Join(initialErr, err))
	}
	defer cleanup()

	resumeEnv := appendEnv(extraEnv, state.GuardRepairParentActionEnv, state.GuardRepairRebuiltResume)
	resumeErr := runResolvedWorker(worker, cfg.RepoRoot, []string{"--resume"}, nil, stdout, stderr, resumeEnv)
	if resumeErr == nil || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		record.Status = state.GuardRepairComplete
		record.OriginalResumeObserved = true
		if err := st.SaveGuardRepairRecord(record); err != nil {
			return errors.Join(resumeErr, err)
		}
		return resumeErr
	}
	return markGuardRepairFailed(st, record, errors.Join(resumeErr, fmt.Errorf("repaired worker did not leave guard-recoverable state")))
}

func sameGuardRepairRecord(a, b state.GuardRepairRecord) bool {
	return a.TaskID == b.TaskID && a.Fingerprint == b.Fingerprint && a.Strategy == b.Strategy &&
		a.RelevantDigest == b.RelevantDigest && a.UpdatedAt.Equal(b.UpdatedAt)
}

func appendEnv(env []string, key, value string) []string {
	result := append([]string(nil), env...)
	prefix := key + "="
	for i, item := range result {
		if strings.HasPrefix(item, prefix) {
			result[i] = prefix + value
			return result
		}
	}
	return append(result, prefix+value)
}

func copyOutput(dst io.Writer, src *bytes.Buffer) {
	if dst != nil && src != nil {
		_, _ = io.Copy(dst, src)
	}
}

func performBoundedGuardRepair(cfg config.AppConfig, st *state.StateStore, record state.GuardRepairRecord) (state.GuardRepairRecord, error) {
	checkpoint, originalBoundary, err := validateGuardRepairOrigin(cfg, st, record)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	record.Status = state.GuardRepairRunning
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return record, err
	}

	worktree, cleanup, err := createGuardRepairWorktree(cfg)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	defer cleanup()
	if err := overlayGuardRepairWorktree(cfg.RepoRoot, worktree); err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}

	beforeDirty, err := state.CaptureStopDirtyFiles(worktree)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	beforeParent, err := state.CaptureParentFileStates(worktree)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	beforeGit, err := state.CaptureGitSnapshot(worktree)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}

	if err := invokeGuardRepairWorker(cfg, worktree, record); err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	afterDirty, err := state.CaptureStopDirtyFiles(worktree)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	afterParent, err := state.CaptureParentFileStates(worktree)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	afterGit, err := state.CaptureGitSnapshot(worktree)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}

	changed := guardrepair.ChangedDirtyPaths(beforeDirty, afterDirty)
	if err := validateGuardRepairChanges(worktree, changed, beforeParent, afterParent, beforeGit, afterGit); err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	if err := validateGuardRepairTests(worktree, changed); err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	if err := reviewGuardRepair(cfg, worktree, changed); err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}

	currentBoundary, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	if !sameRepositoryBoundary(originalBoundary, currentBoundary) {
		return record, markGuardRepairFailed(st, record, fmt.Errorf("original repository changed during guard repair"))
	}
	currentDigest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	if currentDigest != record.RelevantDigest {
		return record, markGuardRepairFailed(st, record, fmt.Errorf("guard repair source changed during repair"))
	}

	if err := copyGuardRepairChanges(worktree, cfg.RepoRoot, changed); err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	currentTask := st.ReadOr("task.id", "")
	currentCheckpoint, err := st.LoadResumeCheckpoint()
	if err != nil || currentTask != record.TaskID || currentCheckpoint.StopKind != state.ResumeStopGuardRecoverable || currentCheckpoint.Phase != checkpoint.Phase {
		return record, markGuardRepairFailed(st, record, fmt.Errorf("original guard recovery checkpoint changed before repair integration"))
	}
	postRepairDirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	currentCheckpoint.StopDirtyFiles = postRepairDirty
	if err := st.SaveResumeCheckpoint(currentCheckpoint); err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	repairedDigest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	if repairedDigest == record.RelevantDigest {
		return record, markGuardRepairFailed(st, record, fmt.Errorf("guard repair produced no relevant source change"))
	}
	record.Status = state.GuardRepairReady
	record.RepairedDigest = repairedDigest
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

func validateGuardRepairOrigin(cfg config.AppConfig, st *state.StateStore, record state.GuardRepairRecord) (state.ResumeCheckpoint, state.GitSnapshot, error) {
	if record.Strategy != guardrepair.StrategySourcePatch {
		return state.ResumeCheckpoint{}, state.GitSnapshot{}, fmt.Errorf("unsupported guard repair strategy %q", record.Strategy)
	}
	if st.ReadOr("task.id", "") != record.TaskID || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		return state.ResumeCheckpoint{}, state.GitSnapshot{}, fmt.Errorf("guard repair requires the original guard-recoverable task")
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil || checkpoint.StopKind != state.ResumeStopGuardRecoverable || checkpoint.StopGitSnapshot == nil {
		return state.ResumeCheckpoint{}, state.GitSnapshot{}, fmt.Errorf("guard repair requires the original guard-recoverable checkpoint")
	}
	currentDirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
	if err != nil {
		return state.ResumeCheckpoint{}, state.GitSnapshot{}, err
	}
	if diff := state.DescribeStopDirtyDiff(checkpoint.StopDirtyFiles, currentDirty); diff != "" {
		return state.ResumeCheckpoint{}, state.GitSnapshot{}, fmt.Errorf("guard repair refuses dirty drift after stop: %s", diff)
	}
	currentDigest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return state.ResumeCheckpoint{}, state.GitSnapshot{}, err
	}
	if currentDigest != record.RelevantDigest {
		return state.ResumeCheckpoint{}, state.GitSnapshot{}, fmt.Errorf("guard repair evidence no longer matches current source")
	}
	boundary, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return state.ResumeCheckpoint{}, state.GitSnapshot{}, err
	}
	return checkpoint, boundary, nil
}

func createGuardRepairWorktree(cfg config.AppConfig) (string, func(), error) {
	id, err := state.NewUUID()
	if err != nil {
		return "", func() {}, err
	}
	worktree := filepath.Join(cfg.WorktreeBase, cfg.RepoShort, "guard-repair-"+id)
	if err := os.MkdirAll(filepath.Dir(worktree), 0o700); err != nil {
		return "", func() {}, err
	}
	command := exec.Command("git", "-C", cfg.RepoRoot, "worktree", "add", "--quiet", "--detach", worktree, "HEAD")
	if output, err := command.CombinedOutput(); err != nil {
		return "", func() {}, fmt.Errorf("create guard repair worktree: %w: %s", err, strings.TrimSpace(string(output)))
	}
	cleanup := func() {
		_ = exec.Command("git", "-C", cfg.RepoRoot, "worktree", "remove", "--force", worktree).Run()
	}
	return worktree, cleanup, nil
}

func overlayGuardRepairWorktree(repoRoot, worktree string) error {
	args := []string{"-C", repoRoot, "diff", "--binary", "HEAD", "--"}
	args = append(args, state.ParentExcludePathspecs()...)
	patch, err := exec.Command("git", args...).Output()
	if err != nil {
		return fmt.Errorf("capture current worktree overlay: %w", err)
	}
	if len(patch) > 0 {
		command := exec.Command("git", "-C", worktree, "apply", "--binary", "--whitespace=nowarn", "-")
		command.Stdin = bytes.NewReader(patch)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("apply current worktree overlay: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	return copyUntrackedOverlay(repoRoot, worktree)
}

func copyUntrackedOverlay(repoRoot, worktree string) error {
	args := []string{"-C", repoRoot, "ls-files", "-z", "--others", "--exclude-standard", "--"}
	args = append(args, state.ParentExcludePathspecs()...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return fmt.Errorf("enumerate current untracked overlay: %w", err)
	}
	for _, path := range strings.Split(strings.TrimRight(string(output), "\x00"), "\x00") {
		if path == "" {
			continue
		}
		src, err := joinRepairRoot(repoRoot, path)
		if err != nil {
			return err
		}
		dst, err := joinRepairRoot(worktree, path)
		if err != nil {
			return err
		}
		if err := copyRepairEntry(src, dst, true); err != nil {
			return fmt.Errorf("copy untracked overlay %s: %w", path, err)
		}
	}
	return nil
}

func invokeGuardRepairWorker(cfg config.AppConfig, worktree string, record state.GuardRepairRecord) error {
	repairCfg := repairConfig(cfg, worktree)
	repairState, err := state.NewStateStore(repairCfg)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(repairState.Path("")) }()
	taskID, err := repairState.StartNewTask()
	if err != nil {
		return err
	}
	base := runner.NewClaudeRunner(repairCfg, repairState)
	guarded := runner.NewInstructionSurfaceGuardRunner(base)
	temp, err := os.MkdirTemp("", "glm-guard-repair-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	result, runErr := guarded.Run(state.WorkerRole, "worker-guard-repair", repairCfg.WorkerModel, false, repairCfg.EscalatedEffort, guardRepairPrompt(record), filepath.Join(temp, "worker.log"))
	if runErr != nil {
		return fmt.Errorf("guard repair worker failed: %w", runErr)
	}
	parsed, err := packet.ParseStructured(result.StructuredOutput)
	if err != nil {
		return err
	}
	if err := packet.ValidateWorkerResult(parsed); err != nil {
		return err
	}
	if err := packet.ValidateArtifacts(parsed.Artifacts, repairState.ArtifactDir(taskID)); err != nil {
		return err
	}
	if parsed.Status != packet.StatusImplemented {
		return fmt.Errorf("guard repair worker returned %s instead of IMPLEMENTED", parsed.Status)
	}
	return nil
}

func repairConfig(cfg config.AppConfig, worktree string) config.AppConfig {
	repairCfg := cfg
	repairCfg.RepoRoot = worktree
	repairCfg.RepoHash = config.RepoHashFor(worktree)
	repairCfg.RepoShort = repairCfg.RepoHash[:12]
	return repairCfg
}

func guardRepairPrompt(record state.GuardRepairRecord) string {
	return "MODE: BOUNDED_GUARD_REPAIR\n\n" +
		"The normal task remains authoritative and must not be replaced. Repair only the control-plane guard/recovery source needed to remove this self-blocking failure.\n\n" +
		"FAILURE:\n" + record.Failure + "\n\n" +
		"ALLOWED_PATHS:\n- " + strings.Join(guardrepair.AllowedPaths(), "\n- ") + "\n\n" +
		"REQUIREMENTS:\n" +
		"- Change only ALLOWED_PATHS.\n" +
		"- Change at least one non-test source file and at least one corresponding _test.go file.\n" +
		"- Do not modify HEAD, refs, index, Git config, IMPLEMENTATION_PLAN.local.md, IMPLEMENTATION_TASKS, or any other parent-managed metadata.\n" +
		"- Do not create or switch tasks, branches, or plans.\n" +
		"- Preserve existing authority protections; do not disable the Git or instruction guards.\n" +
		"- Run focused Go tests for the repair.\n" +
		"- Return IMPLEMENTED only after the bounded repair and tests are complete.\n"
}

func validateGuardRepairChanges(worktree string, changed []string, beforeParent, afterParent state.ParentFileStates, beforeGit, afterGit state.GitSnapshot) error {
	if beforeGit.Head != afterGit.Head || beforeGit.IndexDigest != afterGit.IndexDigest {
		return fmt.Errorf("guard repair modified Git HEAD or index")
	}
	if !state.SameParentFileStates(beforeParent, afterParent) {
		return fmt.Errorf("guard repair modified parent-managed implementation metadata")
	}
	if len(changed) == 0 {
		return fmt.Errorf("guard repair produced no source changes")
	}
	hasSource := false
	hasTest := false
	for _, path := range changed {
		if !guardrepair.IsAllowed(path) {
			return fmt.Errorf("guard repair changed out-of-scope path %s", path)
		}
		full, err := joinRepairRoot(worktree, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("guard repair path must remain a regular file: %s", path)
		}
		if output, err := exec.Command("git", "-C", worktree, "ls-files", "--error-unmatch", "--", path).CombinedOutput(); err != nil {
			return fmt.Errorf("guard repair cannot create untracked repair files: %s: %s", path, strings.TrimSpace(string(output)))
		}
		if strings.HasSuffix(path, "_test.go") {
			hasTest = true
		} else {
			hasSource = true
		}
	}
	if !hasSource || !hasTest {
		return fmt.Errorf("guard repair requires both production source and corresponding test changes")
	}
	return nil
}

func validateGuardRepairTests(worktree string, changed []string) error {
	var goFiles []string
	for _, path := range changed {
		if strings.HasSuffix(path, ".go") {
			goFiles = append(goFiles, path)
		}
	}
	if len(goFiles) > 0 {
		args := append([]string{"-l"}, goFiles...)
		command := exec.Command("gofmt", args...)
		command.Dir = worktree
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("gofmt validation failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
		if strings.TrimSpace(string(output)) != "" {
			return fmt.Errorf("guard repair contains non-gofmt files: %s", strings.TrimSpace(string(output)))
		}
	}
	command := exec.Command("go", "test", "./internal/runner", "./internal/workflow", "./internal/guardrepair")
	command.Dir = filepath.Join(worktree, "glm-worker")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("guard repair Go tests failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func reviewGuardRepair(cfg config.AppConfig, worktree string, changed []string) error {
	diff, err := guardRepairDiff(worktree, changed)
	if err != nil {
		return err
	}
	repairCfg := repairConfig(cfg, worktree)
	repairState, err := state.NewStateStore(repairCfg)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(repairState.Path("")) }()
	if _, err := repairState.StartNewTask(); err != nil {
		return err
	}
	base := runner.NewClaudeRunner(repairCfg, repairState)
	guarded := runner.NewInstructionSurfaceGuardRunner(base)
	temp, err := os.MkdirTemp("", "glm-guard-repair-review-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	prompt := "Review this bounded guard-recovery repair. Verify that it fixes the reported control-plane self-block without weakening Git/instruction authority protections, changing parent-managed task state, or expanding the allowed repair scope. Return PASS only if the implementation and regression test are correct.\n\nDIFF:\n" + diff
	result, runErr := guarded.Run(state.ReviewerRole, "reviewer-guard-repair-high-floor", repairCfg.HighRiskReviewerModel, true, repairCfg.EscalatedEffort, prompt, filepath.Join(temp, "reviewer.log"))
	if runErr != nil {
		return fmt.Errorf("guard repair reviewer failed: %w", runErr)
	}
	parsed, err := packet.ParseStructured(result.StructuredOutput)
	if err != nil {
		return err
	}
	if err := packet.ValidateReviewerResult(parsed); err != nil {
		return err
	}
	if parsed.Status != packet.StatusPass {
		return fmt.Errorf("guard repair reviewer did not PASS: %s", parsed.Status)
	}
	return nil
}

func guardRepairDiff(worktree string, changed []string) (string, error) {
	args := []string{"-C", worktree, "diff", "--no-ext-diff", "--", }
	args = append(args, changed...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("capture guard repair diff: %w", err)
	}
	return string(output), nil
}

func copyGuardRepairChanges(worktree, repoRoot string, changed []string) error {
	for _, path := range changed {
		if !guardrepair.IsAllowed(path) {
			return fmt.Errorf("refuse out-of-scope guard repair integration: %s", path)
		}
		src, err := joinRepairRoot(worktree, path)
		if err != nil {
			return err
		}
		dst, err := joinRepairRoot(repoRoot, path)
		if err != nil {
			return err
		}
		if err := copyRepairEntry(src, dst, false); err != nil {
			return fmt.Errorf("integrate guard repair %s: %w", path, err)
		}
	}
	return nil
}

func copyRepairEntry(src, dst string, allowSymlink bool) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		content, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if err := os.WriteFile(dst, content, mode); err != nil {
			return err
		}
		return os.Chmod(dst, mode)
	}
	if allowSymlink && info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.Remove(dst)
		return os.Symlink(target, dst)
	}
	return fmt.Errorf("unsupported repair entry type %s", info.Mode().Type())
}

func joinRepairRoot(root, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid repository-relative path %q", rel)
	}
	full := filepath.Join(root, clean)
	relCheck, err := filepath.Rel(root, full)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes repository root: %q", rel)
	}
	return full, nil
}

func sameRepositoryBoundary(a, b state.GitSnapshot) bool {
	if a.Head != b.Head || a.IndexDigest != b.IndexDigest || a.WorktreeDigest != b.WorktreeDigest {
		return false
	}
	if a.ParentFiles == nil || b.ParentFiles == nil {
		return false
	}
	return state.SameParentFileStates(*a.ParentFiles, *b.ParentFiles)
}

func buildGuardRepairWorker(cfg config.AppConfig) (string, func(), error) {
	temp, err := os.MkdirTemp("", "glm-guard-repair-build-*")
	if err != nil {
		return "", func() {}, err
	}
	worker := filepath.Join(temp, "glm-worker")
	command := exec.Command("go", "build", "-trimpath", "-o", worker, "./cmd/glm-worker")
	command.Dir = filepath.Join(cfg.RepoRoot, "glm-worker")
	if output, err := command.CombinedOutput(); err != nil {
		_ = os.RemoveAll(temp)
		return "", func() {}, fmt.Errorf("build repaired glm-worker: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return worker, func() { _ = os.RemoveAll(temp) }, nil
}

func markGuardRepairFailed(st *state.StateStore, record state.GuardRepairRecord, cause error) error {
	record.Status = state.GuardRepairFailed
	record.OriginalResumeObserved = false
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
