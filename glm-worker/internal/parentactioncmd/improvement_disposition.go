package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type improvementDispositionOutput struct {
	Status                   string                                   `json:"status"`
	Signal                   state.ImprovementSignal                  `json:"signal"`
	Disposition              state.ImprovementSignalDispositionRecord `json:"disposition"`
	RequiredAction           state.ParentAction                       `json:"required_action"`
	AllowedActions           []state.ParentAction                     `json:"allowed_actions"`
	RequiredActionParameters map[string]string                        `json:"required_action_parameters,omitempty"`
}

const (
	actionImprovementDisposition  = "improvement-disposition"
	improvementSignalKindOption   = "--signal-kind"
	improvementSignalCallIDOption = "--source-call-id"
	improvementDispositionOption  = "--disposition"
	improvementTaskOption         = "--task"
)

func executeImprovementDisposition(cfg config.AppConfig, args []string, stdout io.Writer) error {
	kind, sourceCallID, disposition, targetTask, err := parseImprovementDispositionArgs(args)
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

	signal, err := app.CurrentImprovementSignal(st)
	if err != nil {
		return err
	}
	if signal == nil {
		return writeExistingImprovementDisposition(st, kind, sourceCallID, disposition, targetTask, stdout)
	}
	if signal.Kind != kind || signal.SourceCallID != sourceCallID {
		return fmt.Errorf("pending improvement signal is %s/%s, not %s/%s", signal.Kind, signal.SourceCallID, kind, sourceCallID)
	}
	if err := applyImprovementDisposition(st, state.ImprovementSignalDisposition(disposition), targetTask); err != nil {
		return err
	}
	return recordImprovementDisposition(st, *signal, disposition, targetTask, stdout)
}

func writeExistingImprovementDisposition(st *state.StateStore, kind, sourceCallID, disposition, targetTask string, stdout io.Writer) error {
	records, err := st.CurrentImprovementSignalDispositions()
	if err != nil {
		return err
	}
	resolved := state.ImprovementSignalDisposition(disposition)
	for _, record := range records {
		if record.SignalKind != kind || record.SourceCallID != sourceCallID {
			continue
		}
		if record.Disposition != resolved || record.TargetTask != targetTask {
			return fmt.Errorf("improvement signal %s/%s already has disposition %s", kind, sourceCallID, record.Disposition)
		}
		recordImprovementDispositionEvent(st, record)
		plan, err := st.ParentActionPlan()
		if err != nil {
			return err
		}
		signal := state.ImprovementSignal{Kind: kind, Count: record.SignalCount, SourceCallID: sourceCallID}
		return writeImprovementDispositionOutput(stdout, "already-recorded", signal, record, plan)
	}
	return fmt.Errorf("no matching current improvement signal for %s/%s", kind, sourceCallID)
}

func applyImprovementDisposition(st *state.StateStore, disposition state.ImprovementSignalDisposition, targetTask string) error {
	sourceActive := st.ReadOr("active-task", "")
	if sourceActive == "" {
		return fmt.Errorf("improvement signal disposition requires a current ACTIVE task binding")
	}
	if err := taskcontract.ValidateActiveTaskPath(sourceActive); err != nil {
		return fmt.Errorf("current ACTIVE task binding is invalid: %w", err)
	}
	if state.ImprovementDispositionNeedsTask(disposition) {
		if err := taskcontract.ValidateActiveTaskPath(targetTask); err != nil {
			return err
		}
	}
	if disposition != state.ImprovementSignalDispositionAdopt {
		return nil
	}
	if targetTask == sourceActive {
		return fmt.Errorf("adopted improvement finding requires an independent target task")
	}
	_, _, err := st.RecordPendingDefectRegistration(targetTask, sourceActive)
	return err
}

func recordImprovementDisposition(st *state.StateStore, signal state.ImprovementSignal, disposition, targetTask string, stdout io.Writer) error {
	record, created, err := st.RecordImprovementSignalDisposition(signal, disposition, targetTask)
	if err != nil {
		return err
	}
	recordImprovementDispositionEvent(st, record)
	plan, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	status := "already-recorded"
	if created {
		status = parentActionStatusRecorded
	}
	return writeImprovementDispositionOutput(stdout, status, signal, record, plan)
}

func parseImprovementDispositionArgs(args []string) (string, string, string, string, error) {
	if err := validateImprovementDispositionArgShape(args); err != nil {
		return "", "", "", "", err
	}
	kind := args[2]
	sourceCallID := args[4]
	disposition := args[6]
	if kind == "" || sourceCallID == "" {
		return "", "", "", "", improvementDispositionUsageError()
	}
	targetTask, err := improvementDispositionTargetArg(args)
	if err != nil {
		return "", "", "", "", err
	}
	resolved := state.ImprovementSignalDisposition(disposition)
	if !resolved.Valid() {
		return "", "", "", "", fmt.Errorf("unknown improvement signal disposition %q", disposition)
	}
	if err := validateImprovementDispositionTarget(resolved, targetTask); err != nil {
		return "", "", "", "", err
	}
	return kind, sourceCallID, disposition, targetTask, nil
}

func validateImprovementDispositionArgShape(args []string) error {
	if len(args) != 7 && len(args) != 9 {
		return improvementDispositionUsageError()
	}
	if args[0] != actionImprovementDisposition || args[1] != improvementSignalKindOption || args[3] != improvementSignalCallIDOption || args[5] != improvementDispositionOption {
		return improvementDispositionUsageError()
	}
	return nil
}

func improvementDispositionTargetArg(args []string) (string, error) {
	if len(args) == 7 {
		return "", nil
	}
	if args[7] != improvementTaskOption {
		return "", improvementDispositionUsageError()
	}
	return args[8], nil
}

func validateImprovementDispositionTarget(disposition state.ImprovementSignalDisposition, targetTask string) error {
	needsTask := state.ImprovementDispositionNeedsTask(disposition)
	if needsTask && targetTask == "" {
		return fmt.Errorf("improvement signal disposition %s requires --task", disposition)
	}
	if !needsTask && targetTask != "" {
		return fmt.Errorf("improvement signal disposition %s does not accept --task", disposition)
	}
	return nil
}

func improvementDispositionUsageError() error {
	return fmt.Errorf("usage: glm-parent-action improvement-disposition --signal-kind <kind> --source-call-id <call-id> --disposition <adopt|existing-owner|duplicate|reject|awaiting-evidence> [--task <IMPLEMENTATION_TASKS/...md>]")
}

func recordImprovementDispositionEvent(st *state.StateStore, record state.ImprovementSignalDispositionRecord) {
	now := time.Now().UTC()
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:      record.TaskID,
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       actionImprovementDisposition,
		Outcome:     string(record.Disposition),
	})
}

func writeImprovementDispositionOutput(stdout io.Writer, status string, signal state.ImprovementSignal, record state.ImprovementSignalDispositionRecord, plan state.ParentActionPlan) error {
	return json.NewEncoder(stdout).Encode(improvementDispositionOutput{
		Status:                   status,
		Signal:                   signal,
		Disposition:              record,
		RequiredAction:           plan.RequiredAction,
		AllowedActions:           plan.AllowedActions,
		RequiredActionParameters: plan.RequiredActionParameters,
	})
}
