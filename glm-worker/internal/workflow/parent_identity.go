package workflow

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) persistParentActionCodexIdentity() error {
	threadID := os.Getenv(state.ParentActionCodexThreadIDEnv)
	sessionID := os.Getenv(state.ParentActionCodexSessionIDEnv)
	if !state.ValidUUIDFormat(threadID) || !state.ValidUUIDFormat(sessionID) {
		return nil
	}
	return w.state.SetParentCodexIdentity(threadID, sessionID, func() *state.SessionLimitReading {
		return w.readSessionLimitForIdentityBind()
	})
}

func (w *Workflow) readSessionLimitForIdentityBind() *state.SessionLimitReading {
	snapshot, err := codexlimit.Read(w.config.CodexBin)
	if err != nil {
		return nil
	}
	if snapshot.LimitID == "" || snapshot.FiveHour.UsedPercent == nil || snapshot.FiveHour.ResetsAt == nil {
		return nil
	}
	return &state.SessionLimitReading{
		LimitID:     snapshot.LimitID,
		UsedPercent: *snapshot.FiveHour.UsedPercent,
		ResetsAt:    *snapshot.FiveHour.ResetsAt,
		CapturedAt:  w.now().UTC(),
	}
}
