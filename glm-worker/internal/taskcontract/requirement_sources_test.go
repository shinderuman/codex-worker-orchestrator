package taskcontract

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseRequirementSourcesClassifiesCurrentAndPinnedWithoutHistoricalNarrative(t *testing.T) {
	sha := strings.Repeat("a", 40)
	old := "IMPLEMENTATION_TASKS/retired.md"
	current := "IMPLEMENTATION_TASKS/current.md"
	body := fmt.Sprintf("## Original instruction\n\n````text\n- requirement-source: `task:%s`\n````\n\n## Resolved references\n\n- Historical locator: `%s`\n- requirement-source: `task:%s`\n- requirement-source: `git:%s:%s`\n- arbitrary task filename `%s` in prose\n\n## Contract\n\nNo further requirements.\n", old, old, current, sha, old, old)
	got, err := ParseRequirementSources([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != RequirementSourceTask || got[0].Path != current ||
		got[1].Kind != RequirementSourceGit || got[1].Path != old || got[1].Commit != sha {
		t.Fatalf("parsed sources = %#v", got)
	}
	if got[0].Line <= 0 || got[1].Line <= got[0].Line {
		t.Fatal("source line locations missing")
	}
}

func TestParseRequirementSourcesRejectsMalformedSourceAndDuplicate(t *testing.T) {
	tests := []string{
		"task:../outside.md",
		"task:IMPLEMENTATION_TASKS/../../outside.md",
		"file:/absolute.md",
		"file:.git/config",
		"git:deadbeef:IMPLEMENTATION_TASKS/a.md",
		"other:IMPLEMENTATION_TASKS/a.md",
	}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			body := fmt.Sprintf("## Resolved references\n- requirement-source: `%s`\n", value)
			if _, err := ParseRequirementSources([]byte(body)); err == nil {
				t.Fatalf("accepted invalid source: %s", value)
			}
		})
	}
	body := "## Resolved references\n- requirement-source: task:IMPLEMENTATION_TASKS/a.md\n"
	if _, err := ParseRequirementSources([]byte(body)); err == nil {
		t.Fatal("accepted unquoted requirement source")
	}
	line := "- requirement-source: `task:IMPLEMENTATION_TASKS/a.md`\n"
	if _, err := ParseRequirementSources([]byte("## Resolved references\n" + line + line)); err == nil {
		t.Fatal("accepted duplicate requirement source")
	}
}

func TestRequirementSourceRetirementBlocksCurrentOnly(t *testing.T) {
	target := "IMPLEMENTATION_TASKS/retiring.md"
	other := "IMPLEMENTATION_TASKS/other.md"
	tasks := map[string][]byte{
		target: []byte("## Original instruction\nimmutable\n"),
		other:  []byte(fmt.Sprintf("## Resolved references\n- Historical locator: `%s`\n", target)),
	}
	if err := RequirementSourcesRetirementError(tasks, target); err != nil {
		t.Fatal("historical-only locator blocked retirement:", err)
	}
	tasks[other] = []byte(fmt.Sprintf("## Original instruction\n`%s`\n## Resolved references\n- requirement-source: `task:%s`\n", target, target))
	if err := RequirementSourcesRetirementError(tasks, target); err == nil {
		t.Fatal("current task requirement silently retired")
	}
	tasks[other] = []byte(fmt.Sprintf("## Resolved references\n- requirement-source: `file:%s`\n", target))
	if err := RequirementSourcesRetirementError(tasks, target); err == nil {
		t.Fatal("current file alias silently retired")
	}
	tasks[other] = []byte(fmt.Sprintf("## Resolved references\n- requirement-source: `git:%s:%s`\n", strings.Repeat("a", 40), target))
	if err := RequirementSourcesRetirementError(tasks, target); err != nil {
		t.Fatal("fixed Git source should not be a live retirement edge:", err)
	}
	tasks[other] = []byte("## Resolved references\n- requirement-source: `task:IMPLEMENTATION_TASKS/missing.md`\n")
	if err := RequirementSourcesRetirementError(tasks, target); err == nil {
		t.Fatal("missing current requirement source accepted")
	}
}
