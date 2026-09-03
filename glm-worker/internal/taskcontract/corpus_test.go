package taskcontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScheduleClosureAcceptsExactOnceCorpus(t *testing.T) {
	plan := "# plan\n\n個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。\n\n" +
		"## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n" +
		"## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/b.md`\n\n" +
		"## BLOCKED / USER_PERMISSION_WAIT\n\n- `IMPLEMENTATION_TASKS/c.md`\n"
	entries := []TaskCorpusEntry{
		{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true},
		{Path: "IMPLEMENTATION_TASKS/b.md", Regular: true},
		{Path: "IMPLEMENTATION_TASKS/c.md", Regular: true},
	}
	if failures := ParsePlanSchedule(plan).ClosureFailures(entries); len(failures) != 0 {
		t.Fatalf("failures = %#v", failures)
	}
}

func TestScheduleClosureRejectsUnscheduledTask(t *testing.T) {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n\n## BLOCKED\n"
	entries := []TaskCorpusEntry{
		{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true},
		{Path: "IMPLEMENTATION_TASKS/stray.md", Regular: true},
	}
	failures := ParsePlanSchedule(plan).ClosureFailures(entries)
	if len(failures) != 1 || failures[0].Kind != ScheduleClosureUnscheduledTask || failures[0].Path != "IMPLEMENTATION_TASKS/stray.md" {
		t.Fatalf("failures = %#v", failures)
	}
	if !strings.Contains(failures[0].Reason, "いずれにも列挙") {
		t.Fatalf("reason = %q", failures[0].Reason)
	}
}

func TestScheduleClosureEmptyScheduleRequiresEmptyCorpus(t *testing.T) {
	plan := "## ACTIVE\n\n## NEXT\n\n## BLOCKED\n"
	entries := []TaskCorpusEntry{{Path: "IMPLEMENTATION_TASKS/leftover.md", Regular: true}}
	failures := ParsePlanSchedule(plan).ClosureFailures(entries)
	if len(failures) != 1 || failures[0].Kind != ScheduleClosureUnscheduledTask {
		t.Fatalf("failures = %#v", failures)
	}
	if failures := ParsePlanSchedule(plan).ClosureFailures(nil); len(failures) != 0 {
		t.Fatalf("empty corpus failures = %#v", failures)
	}
}

func TestScheduleClosureRejectsDuplicateScheduleEntry(t *testing.T) {
	cases := []struct {
		name string
		plan string
	}{
		{
			name: "duplicate within NEXT",
			plan: "## ACTIVE\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n- `IMPLEMENTATION_TASKS/b.md`\n- `IMPLEMENTATION_TASKS/b.md`\n\n## BLOCKED\n",
		},
		{
			name: "duplicate across NEXT and BLOCKED",
			plan: "## ACTIVE\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n- `IMPLEMENTATION_TASKS/b.md`\n\n## BLOCKED\n- `IMPLEMENTATION_TASKS/b.md`\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := []TaskCorpusEntry{
				{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true},
				{Path: "IMPLEMENTATION_TASKS/b.md", Regular: true},
			}
			failures := ParsePlanSchedule(tc.plan).ClosureFailures(entries)
			if len(failures) != 1 || failures[0].Kind != ScheduleClosureDuplicateEntry || failures[0].Path != "IMPLEMENTATION_TASKS/b.md" {
				t.Fatalf("failures = %#v", failures)
			}
			if !strings.Contains(failures[0].Reason, "2回") {
				t.Fatalf("reason = %q", failures[0].Reason)
			}
		})
	}
}

func TestScheduleClosureRejectsMissingScheduledTask(t *testing.T) {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/ghost.md`\n\n## BLOCKED\n"
	entries := []TaskCorpusEntry{{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true}}
	failures := ParsePlanSchedule(plan).ClosureFailures(entries)
	if len(failures) != 1 || failures[0].Kind != ScheduleClosureMissingTask || failures[0].Path != "IMPLEMENTATION_TASKS/ghost.md" {
		t.Fatalf("failures = %#v", failures)
	}
}

func TestScheduleClosureRejectsNonRegularTask(t *testing.T) {
	cases := []struct {
		name    string
		plan    string
		entries []TaskCorpusEntry
	}{
		{
			name: "scheduled non-regular task",
			plan: "## ACTIVE\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n- `IMPLEMENTATION_TASKS/link.md`\n\n## BLOCKED\n",
			entries: []TaskCorpusEntry{
				{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true},
				{Path: "IMPLEMENTATION_TASKS/link.md", Regular: false},
			},
		},
		{
			name: "unscheduled non-regular task",
			plan: "## ACTIVE\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n\n## BLOCKED\n",
			entries: []TaskCorpusEntry{
				{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true},
				{Path: "IMPLEMENTATION_TASKS/link.md", Regular: false},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failures := ParsePlanSchedule(tc.plan).ClosureFailures(tc.entries)
			if len(failures) != 1 || failures[0].Kind != ScheduleClosureNonRegularTask || failures[0].Path != "IMPLEMENTATION_TASKS/link.md" {
				t.Fatalf("failures = %#v", failures)
			}
		})
	}
}

func TestScheduleClosureRejectsMalformedSchedule(t *testing.T) {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n\nplain prose line\n\n## BLOCKED\n"
	entries := []TaskCorpusEntry{{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true}}
	failures := ParsePlanSchedule(plan).ClosureFailures(entries)
	if len(failures) != 1 || failures[0].Kind != ScheduleClosureScheduleParse {
		t.Fatalf("failures = %#v", failures)
	}
	if !strings.Contains(failures[0].Reason, "closureを検証できません") {
		t.Fatalf("reason = %q", failures[0].Reason)
	}
}

func TestScheduleClosureRejectsInvalidScheduledPath(t *testing.T) {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n\n- `../outside.md`\n\n## BLOCKED\n"
	entries := []TaskCorpusEntry{{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true}}
	failures := ParsePlanSchedule(plan).ClosureFailures(entries)
	if len(failures) != 1 || failures[0].Kind != ScheduleClosureInvalidTaskPath || failures[0].Path != "../outside.md" {
		t.Fatalf("failures = %#v", failures)
	}
}

func TestScheduleClosureTreatsGlobScheduleEntryAsMissingTask(t *testing.T) {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/*.md`\n\n## BLOCKED\n"
	entries := []TaskCorpusEntry{{Path: "IMPLEMENTATION_TASKS/a.md", Regular: true}}
	failures := ParsePlanSchedule(plan).ClosureFailures(entries)
	if len(failures) != 1 || failures[0].Kind != ScheduleClosureMissingTask || failures[0].Path != "IMPLEMENTATION_TASKS/*.md" {
		t.Fatalf("failures = %#v", failures)
	}
}

func TestEnumerateTaskCorpus(t *testing.T) {
	root := t.TempDir()
	tasksDir := filepath.Join(root, TasksDir)
	if err := os.MkdirAll(filepath.Join(tasksDir, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, "one.md"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, "dir.md", "inner.md"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("one.md", filepath.Join(tasksDir, "link.md")); err != nil {
		t.Fatal(err)
	}
	entries, err := EnumerateTaskCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []TaskCorpusEntry{
		{Path: "IMPLEMENTATION_TASKS/dir.md", Regular: false},
		{Path: "IMPLEMENTATION_TASKS/dir.md/inner.md", Regular: true},
		{Path: "IMPLEMENTATION_TASKS/link.md", Regular: false},
		{Path: "IMPLEMENTATION_TASKS/one.md", Regular: true},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %#v", entries)
	}
	for index, entry := range entries {
		if entry != want[index] {
			t.Fatalf("entries[%d] = %#v want %#v", index, entry, want[index])
		}
	}
}

func TestEnumerateTaskCorpusWithoutTasksDirectory(t *testing.T) {
	entries, err := EnumerateTaskCorpus(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v", entries)
	}
}
