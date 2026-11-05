package repositoryproject

import (
	"strings"
	"testing"
)

func TestTerminalMetadataRetirementProtectsCurrentRequirementSource(t *testing.T) {
	tick := string(rune(96))
	active := "IMPLEMENTATION_TASKS/active.md"
	next := "IMPLEMENTATION_TASKS/next.md"
	plan := []byte("## ACTIVE\n- " + tick + active + tick + "\n\n## NEXT\n- " + tick + next + tick + "\n")
	current := "## Original instruction\n" + tick + active + tick +
		"\n\n## Resolved references\n- requirement-source: " + tick + "task:" + active + tick +
		"\n\n## Dependencies\nnone\n"
	tasks := map[string][]byte{
		active: []byte("## Dependencies\nnone\n"),
		next: []byte(current),
	}
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err == nil {
		t.Fatal("retired current requirement source without transfer")
	}
	// Historical mentions in immutable source are not live dependencies.
	tasks[next] = []byte("## Original instruction\n" + tick + active + tick +
		"\n\n## Resolved references\n- Historical locator: " + tick + active + tick +
		"\n\n## Dependencies\nnone\n")
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err != nil {
		t.Fatal("historical-only locator blocked retirement:", err)
	}
	// A fixed, content-addressed source remains independently retrievable.
	tasks[next] = []byte(strings.ReplaceAll(current, "task:" + active,
		"git:" + strings.Repeat("a", 40) + ":" + active))
	if _, err := RetireTerminalMetadata(plan, tasks, active, false); err != nil {
		t.Fatal("explicit fixed source blocked retirement:", err)
	}
}
