package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type CodexLimitError struct {
	Phase  string
	Reason string
}

func (e *CodexLimitError) Error() string {
	return fmt.Sprintf("codex rate limits unreadable at %s: %s", e.Phase, e.Reason)
}

func printCodexLimit(cfg config.AppConfig, stdout io.Writer) error {
	snapshot, err := codexlimit.Read(cfg.CodexBin)
	if err != nil {
		return &CodexLimitError{Phase: codexLimitPhase(err), Reason: err.Error()}
	}
	return writeJSON(stdout, snapshot)
}

func codexLimitPhase(err error) string {
	switch {
	case errors.Is(err, codexlimit.ErrCodexBinaryNotFound):
		return "codex_binary"
	case errors.Is(err, codexlimit.ErrAppServerStart):
		return "app_server_start"
	case errors.Is(err, codexlimit.ErrAppServerTimeout):
		return "app_server_timeout"
	case errors.Is(err, codexlimit.ErrAppServerProtocol):
		return "app_server_protocol"
	case errors.Is(err, codexlimit.ErrRateLimitsRead):
		return "rate_limits_read"
	case errors.Is(err, codexlimit.ErrFiveHourWindowMissing),
		errors.Is(err, codexlimit.ErrFiveHourResetsAtMissing),
		errors.Is(err, codexlimit.ErrWindowAmbiguous):
		return "window_selection"
	default:
		return "internal"
	}
}
