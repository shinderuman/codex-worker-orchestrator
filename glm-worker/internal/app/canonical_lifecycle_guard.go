package app

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func rejectLegacyBundleProjection(_ config.AppConfig) error {
	return fmt.Errorf("legacy StateStore bundle projection is unavailable after canonical controller cutover; use --authority controller-evidence")
}
