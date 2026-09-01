package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActiveTaskContractViolations(t *testing.T) {
	const taskPath = "IMPLEMENTATION_TASKS/014-test.md"
	cases := []struct {
		name       string
		plan       string
		task       string
		wantCount  int
		wantTarget string
	}{
		{
			name:       "missing feasibility",
			plan:       "## ACTIVE\n\n- `" + taskPath + "`\n",
			task:       "# task\n\n## Contract\n\n- seed\n",
			wantCount:  1,
			wantTarget: taskPath,
		},
		{
			name:       "malformed feasibility",
			plan:       "## ACTIVE\n\n- `" + taskPath + "`\n",
			task:       "# task\n\n## External feasibility\n\nstatus: verified\n",
			wantCount:  1,
			wantTarget: taskPath,
		},
		{
			name:      "valid not applicable",
			plan:      "## ACTIVE\n\n- `" + taskPath + "`\n",
			task:      "# task\n\n## External feasibility\n\nstatus: not-applicable\n",
			wantCount: 0,
		},
		{
			name:       "multiple active tasks",
			plan:       "## ACTIVE\n\n- `" + taskPath + "`\n- `IMPLEMENTATION_TASKS/015-other.md`\n",
			task:       "# task\n\n## External feasibility\n\nstatus: not-applicable\n",
			wantCount:  1,
			wantTarget: implementationPlanPath,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeActiveTaskContractFile(t, root, implementationPlanPath, tc.plan)
			writeActiveTaskContractFile(t, root, taskPath, tc.task)
			paths := []string{implementationPlanPath, taskPath}
			violations, err := activeTaskContractViolations(root, paths)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) != tc.wantCount {
				t.Fatalf("violations = %+v, want count %d", violations, tc.wantCount)
			}
			if tc.wantCount > 0 {
				if violations[0].Rule != "active-task-contract" || violations[0].Path != tc.wantTarget {
					t.Fatalf("violation = %+v", violations[0])
				}
			}
		})
	}
}

func TestActiveTaskContractIgnoresNonActivePlaceholder(t *testing.T) {
	root := t.TempDir()
	const activePath = "IMPLEMENTATION_TASKS/014-active.md"
	const blockedPath = "IMPLEMENTATION_TASKS/102-blocked.md"
	writeActiveTaskContractFile(t, root, implementationPlanPath, "## ACTIVE\n\n- `"+activePath+"`\n\n## BLOCKED\n\n- `"+blockedPath+"`\n")
	writeActiveTaskContractFile(t, root, activePath, "# task\n\n## External feasibility\n\nstatus: not-applicable\n")
	writeActiveTaskContractFile(t, root, blockedPath, "# placeholder without declaration\n")

	violations, err := activeTaskContractViolations(root, []string{implementationPlanPath, activePath, blockedPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("non-ACTIVE placeholder caused violations: %+v", violations)
	}
}

func TestActiveTaskContractIgnoresMalformedNonActiveSchedule(t *testing.T) {
	root := t.TempDir()
	const activePath = "IMPLEMENTATION_TASKS/014-active.md"
	plan := "## ACTIVE\n\n- `" + activePath + "`\n\n## BLOCKED\n\nnot a schedule bullet\n"
	writeActiveTaskContractFile(t, root, implementationPlanPath, plan)
	writeActiveTaskContractFile(t, root, activePath, "# task\n\n## External feasibility\n\nstatus: not-applicable\n")

	violations, err := activeTaskContractViolations(root, []string{implementationPlanPath, activePath})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("non-ACTIVE schedule syntax changed active-task-contract admission: %+v", violations)
	}
}

func writeActiveTaskContractFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
