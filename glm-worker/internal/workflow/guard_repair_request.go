package workflow

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) requestGuardRepair(checkpoint state.ResumeCheckpoint, failure error) {
	if failure == nil || checkpoint.StopKind != state.ResumeStopGuardRecoverable ||
		os.Getenv(state.GuardRepairParentActionEnv) != state.GuardRepairParentActionResume {
		return
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return
	}
	text := boundedText(failure.Error(), packet.MaxDiagnosticBytes)
	record, err := guardrepair.NewRecord(w.config.RepoRoot, taskID, checkpoint.Phase, text)
	if err != nil {
		return
	}
	_ = w.state.RequestGuardRepair(record)
}
