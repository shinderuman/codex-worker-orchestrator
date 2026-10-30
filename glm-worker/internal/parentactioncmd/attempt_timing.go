package parentactioncmd

import (
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentActionAttemptScope struct {
	attemptID string
	enteredAt time.Time
}

func beginParentActionAttempt() parentActionAttemptScope {
	attemptID, err := state.NewUUID()
	if err != nil {
		return parentActionAttemptScope{}
	}
	return parentActionAttemptScope{attemptID: attemptID, enteredAt: time.Now()}
}

func (s parentActionAttemptScope) exportEnv() func() {
	if s.attemptID == "" {
		return func() {}
	}
	previousID, hadID := os.LookupEnv(state.ParentActionAttemptIDEnv)
	previousEnter, hadEnter := os.LookupEnv(state.ParentActionAttemptEnterEnv)
	_ = os.Setenv(state.ParentActionAttemptIDEnv, s.attemptID)
	_ = os.Setenv(state.ParentActionAttemptEnterEnv, s.enteredAt.Format(time.RFC3339Nano))
	return func() {
		if hadID {
			_ = os.Setenv(state.ParentActionAttemptIDEnv, previousID)
		} else {
			_ = os.Unsetenv(state.ParentActionAttemptIDEnv)
		}
		if hadEnter {
			_ = os.Setenv(state.ParentActionAttemptEnterEnv, previousEnter)
		} else {
			_ = os.Unsetenv(state.ParentActionAttemptEnterEnv)
		}
	}
}

func attemptEnteredAt() time.Time {
	enteredAt, _ := time.Parse(time.RFC3339Nano, os.Getenv(state.ParentActionAttemptEnterEnv))
	return enteredAt
}

func (s parentActionAttemptScope) recordExit(cfg config.AppConfig, action string, actionErr error) {
	if s.attemptID == "" {
		return
	}
	exitedAt := time.Now()
	record := state.ParentActionAttemptRecord{
		AttemptID:  s.attemptID,
		Action:     action,
		EnteredAt:  s.enteredAt,
		ExitedAt:   exitedAt,
		DurationMS: exitedAt.Sub(s.enteredAt).Milliseconds(),
		Outcome:    state.ParentActionAttemptOutcomeSuccess,
		TaskID:     state.AttachStateStore(cfg).ReadOr("task.id", ""),
	}
	if actionErr != nil {
		record.Outcome = state.ParentActionAttemptOutcomeFailed
		record.Error = "action-failed"
	}
	if action == actionStart || action == "start-milestones" {
		record.TaskID = ""
	}
	state.AttachStateStore(cfg).RecordParentActionAttempt(record)
}
