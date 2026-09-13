package parentidentity

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func ReadSessionLimitForIdentityBind(codexBin string, now func() time.Time) *state.SessionLimitReading {
	return readSessionLimitForIdentityBind(codexBin, now, codexlimit.Read)
}

func readSessionLimitForIdentityBind(
	codexBin string,
	now func() time.Time,
	read func(string) (codexlimit.Snapshot, error),
) *state.SessionLimitReading {
	snapshot, err := read(codexBin)
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
		CapturedAt:  now().UTC(),
	}
}
