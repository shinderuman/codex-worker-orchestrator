package taskcontract

import "testing"

func TestValidateActiveTaskPathContract(t *testing.T) {
	valid := []string{
		"IMPLEMENTATION_TASKS/x.md",
		"IMPLEMENTATION_TASKS/sub/deep/task.md",
		"IMPLEMENTATION_TASKS/.hidden/task.md",
		"IMPLEMENTATION_TASKS/.md",
		"IMPLEMENTATION_TASKS/..md",
	}
	invalid := []string{
		"tasks/task.md",
		"implementation_tasks/task.md",
		"IMPLEMENTATION_TASKS/task.txt",
		"IMPLEMENTATION_TASKS/",
		"IMPLEMENTATION_TASKS//task.md",
		"IMPLEMENTATION_TASKS/./task.md",
		"IMPLEMENTATION_TASKS/../task.md",
		`IMPLEMENTATION_TASKS\task.md`,
	}
	for _, path := range valid {
		if err := ValidateActiveTaskPath(path); err != nil {
			t.Errorf("ValidateActiveTaskPath(%q) = %v", path, err)
		}
	}
	for _, path := range invalid {
		if err := ValidateActiveTaskPath(path); err == nil {
			t.Errorf("ValidateActiveTaskPath(%q) accepted invalid path", path)
		}
	}
}

func TestActiveSectionEntriesRejectsMalformedScheduleSyntax(t *testing.T) {
	invalid := []string{
		"plain text",
		"* `IMPLEMENTATION_TASKS/a.md`",
		"+ `IMPLEMENTATION_TASKS/a.md`",
		"1. `IMPLEMENTATION_TASKS/a.md`",
		"- `IMPLEMENTATION_TASKS/a.md",
		"- `IMPLEMENTATION_TASKS/a.md` suffix",
		"- prefix `IMPLEMENTATION_TASKS/a.md`",
		"- `a.md` `b.md`",
	}
	for _, line := range invalid {
		plan := "## ACTIVE\n\n" + line + "\n\n## NEXT\n"
		if _, err := ActiveSectionEntries(plan); err == nil {
			t.Errorf("activeSectionEntries accepted %q", line)
		}
	}
}

func TestActiveSectionEntriesAcceptsCanonicalForms(t *testing.T) {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n- IMPLEMENTATION_TASKS/b.md\n\n## NEXT\n"
	entries, err := ActiveSectionEntries(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0] != "IMPLEMENTATION_TASKS/a.md" || entries[1] != "IMPLEMENTATION_TASKS/b.md" {
		t.Fatalf("entries = %v", entries)
	}
}
