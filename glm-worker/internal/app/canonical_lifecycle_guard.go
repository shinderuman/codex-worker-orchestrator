package app

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func rejectLegacyStateLifecycle(_ config.AppConfig, mode CommandMode) error {
	var operation string
	var replacement string
	switch mode {
	case ModeReset:
		operation, replacement = "reset", "--authority controller-semantic"
	default:
		return nil
	}
	return fmt.Errorf("legacy %s lifecycle is unavailable after canonical controller cutover; use %s", operation, replacement)
}

func rejectLegacyBundleProjection(_ config.AppConfig) error {
	return fmt.Errorf("legacy StateStore bundle projection is unavailable after canonical controller cutover; use --authority controller-evidence")
}
