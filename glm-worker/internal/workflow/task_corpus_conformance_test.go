package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func TestTaskCorpusScheduleStateConformance(t *testing.T) {
	t.Parallel()
	root := scenarioRepoRoot(t)
	planBytes, err := os.ReadFile(filepath.Join(root, implementationPlanFile))
	if err != nil {
		t.Fatalf("read %s: %v", implementationPlanFile, err)
	}
	prepared, err := repositoryproject.PrepareFinalHead(string(planBytes))
	if err != nil {
		t.Fatalf("plan scheduleが成立していません: %v", err)
	}
	entries, err := taskcontract.EnumerateTaskCorpus(root)
	if err != nil {
		t.Fatalf("Task corpusを確認できません: %v", err)
	}
	if err := repositoryproject.ValidateClosure(prepared.Schedule, entries, "Plan/Task corpusが一致しません"); err != nil {
		t.Fatal(err)
	}
}
