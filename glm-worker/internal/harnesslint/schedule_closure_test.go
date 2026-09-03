package harnesslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskScheduleClosureHoldsForExactOnceCorpus(t *testing.T) {
	root := t.TempDir()
	const activePath = "IMPLEMENTATION_TASKS/active.md"
	const nextPath = "IMPLEMENTATION_TASKS/next.md"
	const blockedPath = "IMPLEMENTATION_TASKS/blocked.md"
	plan := "## ACTIVE\n\n- `" + activePath + "`\n\n## NEXT（優先順）\n\n- `" + nextPath + "`\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n- `" + blockedPath + "`\n"
	writeActiveTaskContractFile(t, root, implementationPlanPath, plan)
	writeActiveTaskContractFile(t, root, activePath, "# active\n\n## External feasibility\n\nstatus: not-applicable\n")
	writeActiveTaskContractFile(t, root, nextPath, "# next\n")
	writeActiveTaskContractFile(t, root, blockedPath, "# blocked\n")

	violations, err := taskScheduleClosureViolations(root, []string{implementationPlanPath, activePath, nextPath, blockedPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %+v", violations)
	}
}

func TestTaskScheduleClosureViolations(t *testing.T) {
	const activePath = "IMPLEMENTATION_TASKS/active.md"
	cases := []struct {
		name        string
		plan        string
		files       map[string]string
		symlink     string
		wantPath    string
		wantMessage string
	}{
		{
			name:        "unscheduled task",
			plan:        "## ACTIVE\n\n- `" + activePath + "`\n\n## NEXT\n\n## BLOCKED\n",
			files:       map[string]string{activePath: "# active\n", "IMPLEMENTATION_TASKS/stray.md": "# stray\n"},
			wantPath:    "IMPLEMENTATION_TASKS/stray.md",
			wantMessage: "いずれにも列挙",
		},
		{
			name:        "duplicate schedule entry",
			plan:        "## ACTIVE\n\n- `" + activePath + "`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/next.md`\n- `IMPLEMENTATION_TASKS/next.md`\n\n## BLOCKED\n",
			files:       map[string]string{activePath: "# active\n", "IMPLEMENTATION_TASKS/next.md": "# next\n"},
			wantPath:    "IMPLEMENTATION_TASKS/next.md",
			wantMessage: "重複して列挙",
		},
		{
			name:        "missing task",
			plan:        "## ACTIVE\n\n- `" + activePath + "`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/ghost.md`\n\n## BLOCKED\n",
			files:       map[string]string{activePath: "# active\n"},
			wantPath:    "IMPLEMENTATION_TASKS/ghost.md",
			wantMessage: "存在しません",
		},
		{
			name:        "non-regular corpus entry",
			plan:        "## ACTIVE\n\n- `" + activePath + "`\n\n## NEXT\n\n## BLOCKED\n",
			files:       map[string]string{activePath: "# active\n"},
			symlink:     "IMPLEMENTATION_TASKS/link.md",
			wantPath:    "IMPLEMENTATION_TASKS/link.md",
			wantMessage: "regular fileではありません",
		},
		{
			name:        "malformed schedule section",
			plan:        "## ACTIVE\n\n- `" + activePath + "`\n\n## NEXT\n\n## BLOCKED\n\nplain prose line\n",
			files:       map[string]string{activePath: "# active\n"},
			wantPath:    implementationPlanPath,
			wantMessage: "closureを検証できません",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeActiveTaskContractFile(t, root, implementationPlanPath, tc.plan)
			paths := []string{implementationPlanPath}
			for path, content := range tc.files {
				writeActiveTaskContractFile(t, root, path, content)
				paths = append(paths, path)
			}
			if tc.symlink != "" {
				absolute := filepath.Join(root, filepath.FromSlash(tc.symlink))
				if err := os.Symlink(activePath, absolute); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, tc.symlink)
			}
			violations, err := taskScheduleClosureViolations(root, paths)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) != 1 || violations[0].Rule != scheduleClosureRule || violations[0].Path != tc.wantPath {
				t.Fatalf("violations = %+v", violations)
			}
			if !strings.Contains(violations[0].Message, tc.wantMessage) {
				t.Fatalf("message = %q want %q", violations[0].Message, tc.wantMessage)
			}
		})
	}
}

func TestTaskScheduleClosureSkippedWithoutPlan(t *testing.T) {
	root := t.TempDir()
	writeActiveTaskContractFile(t, root, "IMPLEMENTATION_TASKS/stray.md", "# stray\n")
	violations, err := taskScheduleClosureViolations(root, []string{"IMPLEMENTATION_TASKS/stray.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %+v", violations)
	}
}
