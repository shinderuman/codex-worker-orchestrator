package runner

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type InstructionBaselineRotation struct {
	PreviousDigest string
	CurrentDigest  string
}

func RotateInstructionSurfaceBaseline(cfg config.AppConfig, st *state.StateStore) (InstructionBaselineRotation, error) {
	taskID, err := st.TaskID()
	if err != nil {
		return InstructionBaselineRotation{}, err
	}
	current, err := captureInstructionSurfaceSnapshot(cfg.RepoRoot)
	if err != nil {
		return InstructionBaselineRotation{}, &InstructionSurfaceGuardError{Stage: "capture-parent-rotation", Cause: err}
	}
	if err := validateInstructionSurfaceSnapshot(current); err != nil {
		return InstructionBaselineRotation{}, err
	}
	baseline, err := st.Read(instructionSurfaceBaselineStateKey)
	if err != nil {
		return InstructionBaselineRotation{}, &InstructionSurfaceGuardError{Stage: "read-parent-rotation-baseline", Cause: err}
	}
	baselineTaskID, previousDigest, ok := strings.Cut(strings.TrimSpace(baseline), " ")
	if !ok || baselineTaskID == "" || previousDigest == "" || baselineTaskID != taskID {
		return InstructionBaselineRotation{}, &InstructionSurfaceGuardError{Stage: "read-parent-rotation-baseline", Cause: fmt.Errorf("instruction surface baseline does not belong to the active task")}
	}
	if previousDigest == current.digest {
		return InstructionBaselineRotation{}, &InstructionSurfaceGuardError{Stage: "parent-rotation-no-change", Cause: fmt.Errorf("instruction surface has not changed")}
	}
	if err := st.InvalidateAllSessions(); err != nil {
		return InstructionBaselineRotation{}, &InstructionSurfaceGuardError{Stage: "invalidate-parent-rotation-sessions", Cause: err}
	}
	if err := st.Write(instructionSurfaceBaselineStateKey, taskID+" "+current.digest); err != nil {
		return InstructionBaselineRotation{}, &InstructionSurfaceGuardError{Stage: "persist-parent-rotation-baseline", Cause: err}
	}
	return InstructionBaselineRotation{PreviousDigest: previousDigest, CurrentDigest: current.digest}, nil
}
