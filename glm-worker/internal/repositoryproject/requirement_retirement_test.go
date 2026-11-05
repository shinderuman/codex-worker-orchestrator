package repositoryproject

import (
	"fmt"
	"strings"
	"testing"
)

func TestTerminalMetadataRetirementProtectsCurrentRequirementSource(t *testing.T) {
	tick := string(rune(96))
	active := "IMPLEMENTATION_TASKS/active.md"
	next := "IMPLEMENTATION_TASKS/next.md"
	plan := []byte("## ACTIVE\n- " + tick + active + tick + "\n\n## NEXT\n- " + tick + next + tick + "\n")
	current := fmt.Sprintf("## Original instruction\n%s%s%s\n\n## Resolved references\n- requirement-source: %stask:%s%s\n\n## Dependencies\nnone\n", tick, active, tick, tick, active, tick)
	tasks := map[string][]byte{
		active: []byte("## Dependencies\nnone\n"),
		next: []byte(current),
	}
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err == nil {
		t.Fatal("retired current requirement source without transfer")
	}
	tasks[next] = []byte(fmt.Sprintf("## Original instruction\n%s%s%s\n\n## Resolved references\n- Historical locator: %s%s%s\n\n## Dependencies\nnone\n", tick, active, tick, tick, active, tick))
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err != nil {
		t.Fatal("historical-only locator blocked retirement:", err)
	}
	tasks[next] = []byte(strings.ReplaceAll(current, "task:" + active,
		"git:" + strings.Repeat("a", 40) + ":" + active))
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err != nil {
		t.Fatal("explicit fixed source blocked retirement:", err)
	}
}
