package repositoryproject

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func TestTerminalMetadataRetirementKeepsSourceAndRejectsSemanticScheduleChoices(t *testing.T) {
	root, child, unrelated := "IMPLEMENTATION_TASKS/root.md", "IMPLEMENTATION_TASKS/child.md", "IMPLEMENTATION_TASKS/unrelated.md"
	plan := []byte("## ACTIVE\n- `" + root + "`\n\n## NEXT\n- `" + unrelated + "`\n- `" + child + "`\n")
	source := []byte("# root\n\n## Original instruction\n\n````text\n## Dependencies\n- `" + child + "`\n````\n\n## Contract\nroot\n\n## Dependencies\n\n- `" + child + "`\n\n## Fulfilled dependencies\n\nnone\n")
	tasks := map[string][]byte{root: source, child: []byte("# child\n## Dependencies\nnone\n"), unrelated: []byte("# unrelated\n## Dependencies\nnone\n")}
	result, err := RetireTerminalMetadata(plan, tasks, child, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Tasks[child]; ok {
		t.Fatal("terminal Task not removed")
	}
	deps, err := taskcontract.ParseTaskDependencyState(result.Tasks[root])
	if err != nil || len(deps.Outstanding) != 0 || len(deps.Fulfilled) != 1 || deps.Fulfilled[0] != child {
		t.Fatal("dependency retirement mismatch")
	}
	quoted := "````text\n## Dependencies\n- `" + child + "`\n````"
	if !strings.Contains(string(result.Tasks[root]), quoted) || !bytes.Equal(tasks[root], source) {
		t.Fatal("lossless source or input was rewritten")
	}
	active, err := taskcontract.ParsePlanSchedule(string(result.Plan)).ActiveTask()
	if err != nil || active != root {
		t.Fatal("blocker retirement replaced root")
	}
	tasks[root] = []byte("# root\n## Dependencies\n- `" + child + "`\nsemantic prerequisite note\n")
	if _, err := RetireTerminalMetadata(plan, tasks, child, true); err == nil {
		t.Fatal("ambiguous dependency section admitted")
	}
	tasks[root] = source
	if _, err := RetireTerminalMetadata(plan, tasks, root, false); err != nil {
		t.Fatal(err)
	}
	tasks[unrelated] = []byte("# unrelated\n## Dependencies\n- `" + child + "`\n")
	if _, err := RetireTerminalMetadata(plan, tasks, root, false); err == nil {
		t.Fatal("nonrunnable first NEXT skipped")
	}
	if _, err := RetireTerminalMetadata(plan, tasks, child, false); err == nil {
		t.Fatal("arbitrary nonfocus promoted as terminal root")
	}
}
