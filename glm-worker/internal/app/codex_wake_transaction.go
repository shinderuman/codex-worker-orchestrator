package app

import (
	"io"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func printCodexWakePlan(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	snapshot, err := readCodexLimitSnapshot(cfg)
	if err != nil {
		return err
	}
	automationsDir, _ := autoresume.CodexWakePersistencePaths(cfg.CodexConfigDir)
	output, err := autoresume.BuildCodexWakeTransaction(
		snapshot,
		cmd.CodexWake.ThreadID,
		cmd.CodexWake.AutomationID,
		automationsDir,
		time.Now(),
	)
	if err != nil {
		return err
	}
	return writeJSON(stdout, output)
}

func printCodexWakeResponse(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	automationsDir, dbPath := autoresume.CodexWakePersistencePaths(cfg.CodexConfigDir)
	output := autoresume.AdvanceCodexWakeTransaction(
		cmd.CodexWake.Token,
		[]byte(cmd.Payload),
		automationsDir,
		dbPath,
		autoresume.ReadDBRowSqlite3,
	)
	return writeJSON(stdout, output)
}
