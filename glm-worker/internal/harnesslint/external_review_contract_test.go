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

func TestIsolationInstructionRetainsBranchUntilOriginalTaskCompletes(t *testing.T) {
	contract := readExecutionPermissionFile(t, "codex", "instructions", "glm-stop-isolate.md")
	for _, token := range []string{
		"隔離branch",
		"元taskのresume保持照合が完了し元taskが完了するまで削除しない",
	} {
		if !strings.Contains(contract, token) {
			t.Fatalf("glm-stop-isolate.md missing branch lifetime token %q", token)
		}
	}
}
