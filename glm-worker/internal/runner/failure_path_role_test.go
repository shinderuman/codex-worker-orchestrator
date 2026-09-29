package runner

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathadvisory"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunWithDeadlineRequiresNonZeroDeadline(t *testing.T) {
	r := newDeadlineOnlyFixture(t)
	if _, err := r.RunWithDeadline(
		state.FailurePathReviewerRole, "failure-path-reviewer-1", "sonnet", true, "high",
		"prompt", filepath.Join(t.TempDir(), "out.log"), time.Time{},
	); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("zero deadline error = %v", err)
	}
}

func TestFailurePathReviewerPromptFileAndSchema(t *testing.T) {
	if got := promptFileName(state.FailurePathReviewerRole); got != "FAILURE_PATH_REVIEWER.md" {
		t.Fatalf("prompt file = %q", got)
	}
	schema, err := structuredSchema(state.FailurePathReviewerRole, "failure-path-reviewer-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range failurepathadvisory.Classes {
		if !strings.Contains(schema, class) {
			t.Fatalf("schemaにclass %sがありません", class)
		}
	}
}

func newDeadlineOnlyFixture(t *testing.T) *ClaudeRunner {
	t.Helper()
	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	return NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		RepoShort:       "abcdef123456",
		PromptDir:       t.TempDir(),
		ClaudeBin:       "claude",
		ClaudeConfigDir: filepath.Join(t.TempDir(), "claude-home"),
	}, st)
}
