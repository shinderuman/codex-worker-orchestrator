package parentactioncmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type defectRegistrationOutput struct {
	Status                   string                           `json:"status"`
	Registration             *state.PendingDefectRegistration `json:"registration,omitempty"`
	RequiredAction           state.ParentAction               `json:"required_action"`
	AllowedActions           []state.ParentAction             `json:"allowed_actions"`
	RequiredActionParameters map[string]string                `json:"required_action_parameters,omitempty"`
}

const (
	actionRecordDefectFinding = "record-defect-finding"
	actionBindDefectTask      = "bind-defect-task"
	defectTaskOption          = "--task"
)

func executeDefectRegistrationAction(cfg config.AppConfig, args []string, stdout io.Writer) error {
	action, taskPath, err := parseDefectRegistrationArgs(args)
	if err != nil {
		return err
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	sourceActive := st.ReadOr("active-task", "")
	if sourceActive == "" {
		return fmt.Errorf("defect registration requires a current ACTIVE task binding")
	}
	if err := taskcontract.ValidateActiveTaskPath(sourceActive); err != nil {
		return fmt.Errorf("current ACTIVE task binding is invalid: %w", err)
	}
	if taskPath == sourceActive {
		return fmt.Errorf("current-task finding %s does not require independent defect registration", taskPath)
	}

	switch action {
	case actionRecordDefectFinding:
		return executeRecordDefectFinding(cfg, st, sourceActive, taskPath, stdout)
	case actionBindDefectTask:
		return executeBindDefectTask(cfg, st, sourceActive, taskPath, stdout)
	default:
		return fmt.Errorf("unsupported defect registration action %s", action)
	}
}

func executeRecordDefectFinding(cfg config.AppConfig, st *state.StateStore, sourceActive, taskPath string, stdout io.Writer) error {
	declared, err := defectTaskBindingDeclared(cfg.RepoRoot, sourceActive, taskPath)
	if err != nil {
		return err
	}
	if declared && validateDefectTaskBindingContract(cfg.RepoRoot, taskPath) == nil {
		plan, err := st.ParentActionPlan()
		if err != nil {
			return err
		}
		return writeDefectRegistrationOutput(stdout, "already-registered", nil, plan)
	}
	registration, created, err := st.RecordPendingDefectRegistration(taskPath, sourceActive)
	if err != nil {
		return err
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	if plan.RequiredAction != state.ParentActionBindDefectTask || !plan.Allows(state.ParentActionBindDefectTask) || plan.RequiredActionParameters["task"] == "" {
		return fmt.Errorf("recorded defect finding did not force machine-required task binding")
	}
	status := "pending"
	if created {
		status = "recorded"
	}
	return writeDefectRegistrationOutput(stdout, status, &registration, plan)
}

func executeBindDefectTask(cfg config.AppConfig, st *state.StateStore, sourceActive, taskPath string, stdout io.Writer) error {
	plan, admitted, err := st.AdmitParentAction(state.ParentActionBindDefectTask)
	if err != nil {
		return err
	}
	if !admitted || plan.RequiredAction != state.ParentActionBindDefectTask || plan.RequiredActionParameters["task"] != taskPath {
		return fmt.Errorf("bind-defect-task is not machine-required for %s", taskPath)
	}
	declared, err := defectTaskBindingDeclared(cfg.RepoRoot, sourceActive, taskPath)
	if err != nil {
		return err
	}
	if !declared {
		return fmt.Errorf("defect task %s is not yet bound to a task file and Plan NEXT/BLOCKED entry", taskPath)
	}
	if err := validateDefectTaskBindingContract(cfg.RepoRoot, taskPath); err != nil {
		return err
	}
	bound, err := st.BindPendingDefectRegistration(taskPath)
	if err != nil {
		return err
	}
	after, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	return writeDefectRegistrationOutput(stdout, "bound", &bound, after)
}

func parseDefectRegistrationArgs(args []string) (string, string, error) {
	if len(args) != 3 || (args[0] != actionRecordDefectFinding && args[0] != actionBindDefectTask) || args[1] != defectTaskOption {
		return "", "", fmt.Errorf("usage: glm-parent-action <record-defect-finding|bind-defect-task> --task <IMPLEMENTATION_TASKS/...md>")
	}
	if err := taskcontract.ValidateActiveTaskPath(args[2]); err != nil {
		return "", "", err
	}
	return args[0], args[2], nil
}

func defectTaskBindingDeclared(repoRoot, sourceActive, taskPath string) (bool, error) {
	if err := taskcontract.ValidateActiveTaskPath(taskPath); err != nil {
		return false, err
	}
	taskInfo, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(taskPath)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect defect task %s: %w", taskPath, err)
	}
	if !taskInfo.Mode().IsRegular() {
		return false, fmt.Errorf("defect task %s is not a regular file", taskPath)
	}
	planData, err := os.ReadFile(filepath.Join(repoRoot, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		return false, fmt.Errorf("read IMPLEMENTATION_PLAN.local.md: %w", err)
	}
	schedule := taskcontract.ParsePlanSchedule(string(planData))
	active, err := schedule.ValidateComplete()
	if err != nil {
		return false, err
	}
	if active != sourceActive {
		return false, fmt.Errorf("plan ACTIVE changed from defect finding source %s to %s", sourceActive, active)
	}
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return false, err
	}
	matches := 0
	for _, entry := range append(next, blocked...) {
		if entry == taskPath {
			matches++
		}
	}
	if matches > 1 {
		return false, fmt.Errorf("defect task %s appears multiple times in Plan NEXT/BLOCKED", taskPath)
	}
	return matches == 1, nil
}

func validateDefectTaskBindingContract(repoRoot, taskPath string) error {
	taskData, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(taskPath)))
	if err != nil {
		return fmt.Errorf("read defect task %s: %w", taskPath, err)
	}
	if err := repositoryproject.ValidateActiveTaskContent(taskData); err != nil {
		return fmt.Errorf("defect task %s does not satisfy task content contract: %w", taskPath, err)
	}
	if _, err := taskcontract.ParseTaskDependencyState(taskData); err != nil {
		return fmt.Errorf("defect task %s does not satisfy dependency contract: %w", taskPath, err)
	}
	planData, err := os.ReadFile(filepath.Join(repoRoot, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		return fmt.Errorf("read IMPLEMENTATION_PLAN.local.md: %w", err)
	}
	schedule := taskcontract.ParsePlanSchedule(string(planData))
	entries, err := taskcontract.EnumerateTaskCorpus(repoRoot)
	if err != nil {
		return err
	}
	failures := schedule.ClosureFailures(entries)
	if len(failures) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(failures))
	for _, failure := range failures {
		reasons = append(reasons, failure.Reason)
	}
	return fmt.Errorf("defect task binding does not preserve task schedule closure: %s", strings.Join(reasons, "; "))
}

func writeDefectRegistrationOutput(stdout io.Writer, status string, registration *state.PendingDefectRegistration, plan state.ParentActionPlan) error {
	return json.NewEncoder(stdout).Encode(defectRegistrationOutput{
		Status:                   status,
		Registration:             registration,
		RequiredAction:           plan.RequiredAction,
		AllowedActions:           plan.AllowedActions,
		RequiredActionParameters: plan.RequiredActionParameters,
	})
}
