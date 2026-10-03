package harnesslint

import (
	"strings"
	"testing"
)

func TestParentMaintenanceDirectEditAuthorityIsScoped(t *testing.T) {
	agents := readExecutionPermissionFile(t, "codex", "AGENTS.md")
	for _, token := range []string{
		"parent maintenance",
		"parent-managed metadata edit",
		"production code・test・設定・prompt・production wiringの直接編集へ拡張しない",
	} {
		if !strings.Contains(agents, token) {
			t.Fatalf("codex/AGENTS.md missing parent-maintenance authority token %q", token)
		}
	}
}
