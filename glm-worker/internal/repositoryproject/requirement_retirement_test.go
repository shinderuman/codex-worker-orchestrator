package repositoryproject

import (
	"fmt"
	"strings"
	"testing"
)

func TestTerminalMetadataRetirementProtectsCurrentRequirementSource(t *testing.T) {
	active := "IMPLEMENTATION_TASKS/active.md"
	next := "IMPLEMENTATION_TASKS/next.md"
	plan := []byte(fmt.Sprintf("## ACTIVE\n- \`%s\`\n\n## NEXT\n- \`%s\`\n", active, next))
	current := fmt.Sprintf("## Original instruction\n\`%s\`\n\n## Resolved references\n- requirement-source: \`task:%s\`\n\n## Dependencies\nnone\n", active, active)
	tasks := map[string][]byte{
		active: []byte("## Dependencies\nnone\n"),
		next:   []byte(current),
	}
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err == nil {
		t.Fatal("retired current requirement source without transfer")
	}
	tasks[next] = []byte(fmt.Sprintf("## Original instruction\n\`%s\`\n\n## Resolved references\n- Historical locator: \`%s\`\n\n## Dependencies\nnone\n", active, active))
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err != nil {
		t.Fatal("historical-only locator blocked retirement:", err)
	}
	pinned := fmt.Sprintf("git:%s:%s", strings.Repeat("a", 40), active)
	tasks[next] = []byte(strings.ReplaceAll(current, "task:"+active, pinned))
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err != nil {
		t.Fatal("explicit fixed source blocked retirement:", err)
	}
}
