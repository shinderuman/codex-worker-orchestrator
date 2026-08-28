package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDebugIssue61ProxyPath(t *testing.T) {
	root := newGitAuthorityRepo(t)
	command := "echo proxy=$(command -v git) >&2; echo cwd=$(pwd -P) >&2; echo guardtemp=$GLM_WORKER_GIT_TEMP_ROOT >&2; git commit --allow-empty -m blocked; echo gitstatus=$? >&2"
	guarded, _ := newGitAuthorityProductionRunner(t, root, command, nil)
	output := filepath.Join(t.TempDir(), "output")
	_, runErr := guarded.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", output)
	stderr, _ := os.ReadFile(output + ".stderr")
	t.Fatalf("diagnostic runErr=%v stderr=%q", runErr, string(stderr))
}
