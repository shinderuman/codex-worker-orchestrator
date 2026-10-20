package taskcontract

import (
	"strings"
	"testing"
)

func TestPlanScheduleParsesAllScheduleSections(t *testing.T) {
	plan := "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT（優先順）\n\n- IMPLEMENTATION_TASKS/b.md\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n- `IMPLEMENTATION_TASKS/c.md`\n"
	schedule := ParsePlanSchedule(plan)
	active, err := schedule.ValidateComplete()
	if err != nil {
		t.Fatal(err)
	}
	if active != "IMPLEMENTATION_TASKS/a.md" {
		t.Fatalf("active = %q", active)
	}
	if len(schedule.Next) != 1 || schedule.Next[0] != "IMPLEMENTATION_TASKS/b.md" {
		t.Fatalf("next = %v", schedule.Next)
	}
	if len(schedule.Blocked) != 1 || schedule.Blocked[0] != "IMPLEMENTATION_TASKS/c.md" {
		t.Fatalf("blocked = %v", schedule.Blocked)
	}
}

func TestPlanScheduleActiveEntriesRejectMalformedSyntax(t *testing.T) {
	invalid := []string{
		"plain text",
		"* `IMPLEMENTATION_TASKS/a.md`",
		"+ `IMPLEMENTATION_TASKS/a.md`",
		"1. `IMPLEMENTATION_TASKS/a.md`",
		"- `IMPLEMENTATION_TASKS/a.md",
		"- `IMPLEMENTATION_TASKS/a.md` suffix",
		"- prefix `IMPLEMENTATION_TASKS/a.md`",
		"- `a.md` `b.md`",
		"- [`IMPLEMENTATION_TASKS/a.md`](IMPLEMENTATION_TASKS/b.md)",
		"- [`IMPLEMENTATION_TASKS/a.md`](../a.md)",
		"- [`IMPLEMENTATION_TASKS/a.md`](IMPLEMENTATION_TASKS/a.md) suffix",
		"- [`IMPLEMENTATION_TASKS/a.md`]",
		"- [IMPLEMENTATION_TASKS/a.md](IMPLEMENTATION_TASKS/a.md)",
	}
	for _, line := range invalid {
		plan := "## ACTIVE\n\n" + line + "\n\n## NEXT\n"
		if _, err := ParsePlanSchedule(plan).ActiveEntries(); err == nil {
			t.Errorf("PlanSchedule.ActiveEntries accepted %q", line)
		}
	}
}

func TestPlanScheduleActiveEntriesAcceptCanonicalForms(t *testing.T) {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n- IMPLEMENTATION_TASKS/b.md\n- [`IMPLEMENTATION_TASKS/c.md`](IMPLEMENTATION_TASKS/c.md)\n\n## NEXT\n"
	entries, err := ParsePlanSchedule(plan).ActiveEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0] != "IMPLEMENTATION_TASKS/a.md" || entries[1] != "IMPLEMENTATION_TASKS/b.md" || entries[2] != "IMPLEMENTATION_TASKS/c.md" {
		t.Fatalf("entries = %v", entries)
	}
}

func TestPlanScheduleActiveAdmissionIgnoresOtherSectionSyntax(t *testing.T) {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n\nnot a bullet\n"
	schedule := ParsePlanSchedule(plan)
	active, err := schedule.ActiveTask()
	if err != nil || active != "IMPLEMENTATION_TASKS/a.md" {
		t.Fatalf("active = %q err=%v", active, err)
	}
	if _, err := schedule.ValidateComplete(); err == nil || !strings.Contains(err.Error(), "NEXT欄") {
		t.Fatalf("complete validation error = %v", err)
	}
}

func TestPlanScheduleActiveAdmissionOwnsCardinalityAndPath(t *testing.T) {
	cases := []struct {
		name string
		plan string
		want string
	}{
		{name: "missing", plan: "## ACTIVE\n", want: "task fileがありません"},
		{name: "multiple", plan: "## ACTIVE\n- IMPLEMENTATION_TASKS/a.md\n- IMPLEMENTATION_TASKS/b.md\n", want: "一意ではありません"},
		{name: "invalid path", plan: "## ACTIVE\n- ../a.md\n", want: "IMPLEMENTATION_TASKS配下"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePlanSchedule(tc.plan).ActiveTask()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v want %q", err, tc.want)
			}
		})
	}
}

func TestPlanScheduleCompleteAdmissionRejectsActiveInNextOrBlocked(t *testing.T) {
	for _, section := range []string{"NEXT", "BLOCKED"} {
		t.Run(section, func(t *testing.T) {
			plan := "## ACTIVE\n- IMPLEMENTATION_TASKS/a.md\n\n## " + section + "\n- IMPLEMENTATION_TASKS/a.md\n"
			_, err := ParsePlanSchedule(plan).ValidateComplete()
			if err == nil || !strings.Contains(err.Error(), "重複") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestPlanScheduleCompleteAdmissionAllowsNonActiveDuplicates(t *testing.T) {
	plan := "## ACTIVE\n- IMPLEMENTATION_TASKS/a.md\n\n## NEXT\n- IMPLEMENTATION_TASKS/b.md\n- IMPLEMENTATION_TASKS/b.md\n\n## BLOCKED\n- IMPLEMENTATION_TASKS/b.md\n"
	if _, err := ParsePlanSchedule(plan).ValidateComplete(); err != nil {
		t.Fatalf("existing NEXT/BLOCKED duplicate semantics changed: %v", err)
	}
}

func TestPlanScheduleCompleteAdmissionValidatesNonActivePaths(t *testing.T) {
	plan := "## ACTIVE\n- IMPLEMENTATION_TASKS/a.md\n\n## NEXT\n- ../outside.md\n"
	if _, err := ParsePlanSchedule(plan).ValidateComplete(); err == nil || !strings.Contains(err.Error(), "IMPLEMENTATION_TASKS配下") {
		t.Fatalf("error = %v", err)
	}
}

func TestPlanScheduleMarkdownLinksPreserveSectionsAndRetirement(t *testing.T) {
	root := "IMPLEMENTATION_TASKS/root.md"
	next := "IMPLEMENTATION_TASKS/next.md"
	blocked := "IMPLEMENTATION_TASKS/blocked.md"
	link := func(path string) string { return "- [`" + path + "`](" + path + ")" }
	plan := "## ACTIVE\n" + link(root) + "\n\n## NEXT\n" + link(next) + "\n\n## BLOCKED\n" + link(blocked) + "\n"
	schedule := ParsePlanSchedule(plan)
	if active, err := schedule.ValidateComplete(); err != nil || active != root ||
		len(schedule.Next) != 1 || schedule.Next[0] != next ||
		len(schedule.Blocked) != 1 || schedule.Blocked[0] != blocked {
		t.Fatalf("linked schedule = %#v, %v", schedule, err)
	}
	updated, err := RetirePlanTask(plan, root, next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "## ACTIVE\n"+link(next)) ||
		!strings.Contains(updated, "## BLOCKED\n"+link(blocked)) ||
		strings.Contains(updated, "## NEXT\n"+link(next)) {
		t.Fatalf("link retirement did not preserve schedule and links: %s", updated)
	}
	if active, err := ParsePlanSchedule(updated).ValidateComplete(); err != nil || active != next {
		t.Fatalf("retired linked plan: active=%q err=%v", active, err)
	}
}
