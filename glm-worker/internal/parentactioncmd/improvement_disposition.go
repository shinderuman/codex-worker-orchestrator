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
	actionImprovementDisposition = "improvement-disposition"
	improvementSignalKindOption  = "--signal-kind"
	improvementDispositionOption = "--disposition"
	improvementTaskOption        = "--task"
)

func requireImprovementSignalDisposition(cfg config.AppConfig, action string) error {
	if action == actionImprovementDisposition {
		return nil
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	signal, err := app.CurrentImprovementSignal(st)
	if err != nil {
		return fmt.Errorf("improvement signal state is unreadable: %w", err)
	}
	if signal == nil {
		return nil
	}
	return fmt.Errorf("machine-visible improvement signal %s requires parent disposition before %s", signal.Kind, action)
}

func executeImprovementDisposition(cfg config.AppConfig, args []string, stdout io.Writer) error {
	kind, disposition, targetTask, err := parseImprovementDispositionArgs(args)
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
		return writeExistingImprovementDisposition(st, kind, disposition, targetTask, stdout)
	}
	if signal.Kind != kind {
		return fmt.Errorf("pending improvement signal is %s, not %s", signal.Kind, kind)
	}
	if err := applyImprovementDisposition(st, state.ImprovementSignalDisposition(disposition), targetTask); err != nil {
		return err
	}
	return recordImprovementDisposition(st, *signal, disposition, targetTask, stdout)
}

func writeExistingImprovementDisposition(st *state.StateStore, kind, disposition, targetTask string, stdout io.Writer) error {
	record, created, err := st.RecordImprovementSignalDisposition(kind, disposition, targetTask)
	if err != nil {
		return fmt.Errorf("no matching pending improvement signal: %w", err)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	status := "already-recorded"
	if created {
		status = parentActionStatusRecorded
	}
	signal := state.ImprovementSignal{Kind: kind, Count: record.SignalCount, SourceCallID: record.SourceCallID}
	return writeImprovementDispositionOutput(stdout, status, signal, record, plan)
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
	record, created, err := st.RecordImprovementSignalDisposition(signal.Kind, disposition, targetTask)
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

func parseImprovementDispositionArgs(args []string) (string, string, string, error) {
	if len(args) != 5 && len(args) != 7 {
		return "", "", "", fmt.Errorf("usage: glm-parent-action improvement-disposition --signal-kind <kind> --disposition <adopt|existing-owner|duplicate|reject|awaiting-evidence> [--task <IMPLEMENTATION_TASKS/...md>]")
	}
	if args[0] != actionImprovementDisposition || args[1] != improvementSignalKindOption || args[3] != improvementDispositionOption {
		return "", "", "", fmt.Errorf("usage: glm-parent-action improvement-disposition --signal-kind <kind> --disposition <adopt|existing-owner|duplicate|reject|awaiting-evidence> [--task <IMPLEMENTATION_TASKS/...md>]")
	}
	kind := args[2]
	disposition := args[4]
	targetTask := ""
	if len(args) == 7 {
		if args[5] != improvementTaskOption {
			return "", "", "", fmt.Errorf("usage: glm-parent-action improvement-disposition --signal-kind <kind> --disposition <adopt|existing-owner|duplicate|reject|awaiting-evidence> [--task <IMPLEMENTATION_TASKS/...md>]")
		}
		targetTask = args[6]
	}
	resolved := state.ImprovementSignalDisposition(disposition)
	if !resolved.Valid() {
		return "", "", "", fmt.Errorf("unknown improvement signal disposition %q", disposition)
	}
	if state.ImprovementDispositionNeedsTask(resolved) != (targetTask != "") {
		if state.ImprovementDispositionNeedsTask(resolved) {
			return "", "", "", fmt.Errorf("improvement signal disposition %s requires --task", disposition)
		}
		return "", "", "", fmt.Errorf("improvement signal disposition %s does not accept --task", disposition)
	}
	return kind, disposition, targetTask, nil
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
