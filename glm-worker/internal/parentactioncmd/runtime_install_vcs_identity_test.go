package parentactioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestInstalledRuntimeStatusRejectsModifiedVCSIdentity(t *testing.T) {
	revision := strings.Repeat("a", 40)
	binDir := t.TempDir()
	worker := filepath.Join(binDir, "glm-worker")
	script := `#!/bin/sh
if [ "${1:-}" != "--status" ]; then
	exit 2
fi
printf '%s\n' '{"runtime_build":{"vcs_revision":"` + revision + `","vcs_modified":true,"relationship":"unknown"}}'
`
	if err := os.WriteFile(worker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, _, failure := installedRuntimeStatus(config.AppConfig{RepoRoot: t.TempDir()}, revision)
	if failure == nil || failure.Reason != runtimeInstallFailureInstalled {
		t.Fatalf("modified runtime identity accepted: %#v", failure)
	}
}
