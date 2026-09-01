package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func TestTaskCorpusScheduleStateConformance(t *testing.T) {
	root := scenarioRepoRoot(t)
	planBytes, err := os.ReadFile(filepath.Join(root, implementationPlanFile))
	if err != nil {
		t.Fatalf("read %s: %v", implementationPlanFile, err)
	}
	activeTask, err := taskcontract.ParsePlanSchedule(string(planBytes)).ActiveTask()
	if err != nil {
		t.Fatalf("plan ACTIVE解決が成立していません: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(activeTask)))
	if err != nil {
		t.Fatalf("planのACTIVE task file %s を確認できません: %v", activeTask, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("planのACTIVE task file %s はregular fileではありません", activeTask)
	}
}
