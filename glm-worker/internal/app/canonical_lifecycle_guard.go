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
	case ModePark:
		operation, replacement = "park", "--authority controller-execution (suspend)"
	case ModeIsolate:
		operation, replacement = "isolate", "--authority controller-execution (suspend)"
	case ModeUnpark:
		operation, replacement = "unpark", "--authority controller-execution (materialize/cleanup)"
	default:
		return nil
	}
	return fmt.Errorf("legacy %s lifecycle is unavailable after canonical controller cutover; use %s", operation, replacement)
}

func rejectLegacyBundleProjection(_ config.AppConfig) error {
	return fmt.Errorf("legacy StateStore bundle projection is unavailable after canonical controller cutover; use --authority controller-evidence")
}
