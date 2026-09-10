package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func requestSelfBlockedGuardRepair(cfg config.AppConfig, st *state.StateStore, stderr []byte) error {
	failure, ok := preCallGuardFailureMessage(stderr)
	if !ok || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		return nil
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return fmt.Errorf("load guard-recoverable checkpoint after pre-call failure: %w", err)
	}
	if checkpoint.StopKind != state.ResumeStopGuardRecoverable {
		return fmt.Errorf("guard-recoverable task has non-guard resume checkpoint")
	}
	if !runner.SameGuardFailureFamilyText(checkpoint.GuardFailure, failure) {
		return nil
	}
	taskID, err := st.TaskID()
	if err != nil {
		return err
	}
	record, err := guardrepair.NewRecord(cfg.RepoRoot, taskID, checkpoint.Phase, failure)
	if err != nil {
		return err
	}
	return st.RequestGuardRepair(record)
}

func preCallGuardFailureMessage(stderr []byte) (string, bool) {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stderr), &envelope); err != nil {
		return "", false
	}
	failure := strings.TrimSpace(envelope.Error.Message)
	return failure, runner.IsPreCallGuardFailureText(failure)
}
