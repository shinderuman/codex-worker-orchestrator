package parentactioncmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func invokeGuardRepairWorker(cfg config.AppConfig, worktree string, record state.GuardRepairRecord) error {
	repairCfg := repairConfig(cfg, worktree)
	repairState, cleanup, err := newGuardRepairModelState(repairCfg)
	if err != nil {
		return err
	}
	defer cleanup()
	taskID := repairState.ReadOr("task.id", "")
	result, err := runGuardRepairModel(repairCfg, repairState, state.WorkerRole, "worker-guard-repair", repairCfg.WorkerModel, false, guardRepairPrompt(record))
	if err != nil {
		return fmt.Errorf("guard repair worker failed: %w", err)
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

func newGuardRepairModelState(cfg config.AppConfig) (*state.StateStore, func(), error) {
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(st.Path("")) }
	if _, err := st.StartNewTask(); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return st, cleanup, nil
}

func runGuardRepairModel(
	cfg config.AppConfig,
	st *state.StateStore,
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	prompt string,
) (runner.RunResult, error) {
	base := runner.NewClaudeRunner(cfg, st)
	guarded := runner.NewGuardRepairRunner(base)
	temp, err := os.MkdirTemp("", "glm-guard-repair-model-*")
	if err != nil {
		return runner.RunResult{}, err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	return guarded.Run(role, phase, model, readOnly, cfg.EscalatedEffort, prompt, filepath.Join(temp, phase+".log"))
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

func validateGuardRepairTests(worktree string, changed []string) error {
	if err := validateGuardRepairFormatting(worktree, changed); err != nil {
		return err
	}
	output, err := runGuardRepairCommand(
		filepath.Join(worktree, "glm-worker"),
		"guard repair Go tests",
		"go", "test", "./internal/runner", "./internal/workflow", "./internal/guardrepair",
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("guard repair Go tests failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func validateGuardRepairFormatting(worktree string, changed []string) error {
	var goFiles []string
	for _, path := range changed {
		if strings.HasSuffix(path, ".go") {
			goFiles = append(goFiles, path)
		}
	}
	if len(goFiles) == 0 {
		return nil
	}
	args := append([]string{"-l"}, goFiles...)
	output, err := runGuardRepairCommand(worktree, "gofmt validation", "gofmt", args...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("gofmt validation failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if names := strings.TrimSpace(string(output)); names != "" {
		return fmt.Errorf("guard repair contains non-gofmt files: %s", names)
	}
	return nil
}

func reviewGuardRepair(cfg config.AppConfig, worktree string, changed []string) error {
	diff, err := guardRepairDiff(worktree, changed)
	if err != nil {
		return err
	}
	repairCfg := repairConfig(cfg, worktree)
	repairState, cleanup, err := newGuardRepairModelState(repairCfg)
	if err != nil {
		return err
	}
	defer cleanup()
	prompt := "Review this bounded guard-recovery repair. Verify that it fixes the reported control-plane self-block without weakening Git/instruction authority protections, changing parent-managed task state, or expanding the allowed repair scope. Return PASS only if the implementation and regression test are correct.\n\nDIFF:\n" + diff
	result, err := runGuardRepairModel(repairCfg, repairState, state.ReviewerRole, "reviewer-guard-repair-high-floor", repairCfg.HighRiskReviewerModel, true, prompt)
	if err != nil {
		return fmt.Errorf("guard repair reviewer failed: %w", err)
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
	args := []string{"-C", worktree, "diff", "--no-ext-diff", "--"}
	args = append(args, changed...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("capture guard repair diff: %w", err)
	}
	return string(output), nil
}

func buildGuardRepairWorker(cfg config.AppConfig) (string, func(), error) {
	temp, err := os.MkdirTemp("", "glm-guard-repair-build-*")
	if err != nil {
		return "", func() {}, err
	}
	worker := filepath.Join(temp, "glm-worker")
	output, err := runGuardRepairCommand(
		filepath.Join(cfg.RepoRoot, "glm-worker"),
		"build repaired glm-worker",
		"go", "build", "-trimpath", "-o", worker, "./cmd/glm-worker",
	)
	if err != nil {
		_ = os.RemoveAll(temp)
		if errors.Is(err, context.DeadlineExceeded) {
			return "", func() {}, err
		}
		return "", func() {}, fmt.Errorf("build repaired glm-worker: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return worker, func() { _ = os.RemoveAll(temp) }, nil
}
