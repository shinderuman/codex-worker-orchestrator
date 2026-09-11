package harnesslint

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func TestFinalVerificationOrderingViolations(t *testing.T) {
	const (
		activePath   = "IMPLEMENTATION_TASKS/active.md"
		semanticPath = "IMPLEMENTATION_TASKS/exhaustive-search-query-persistence-review.md"
		blockedPath  = "IMPLEMENTATION_TASKS/prerequisite.md"
	)
	finalPath := taskcontract.FinalVerificationTaskPath
	noDependencies := "# task\n\n## Dependencies\n\nnone\n"
	cases := []struct {
		name          string
		plan          string
		files         map[string]string
		wantViolation bool
	}{
		{
			name: "runnable semantic task after final verification",
			plan: planSchedule(activePath, []string{finalPath, semanticPath}, nil),
			files: map[string]string{
				activePath:   "# active\n",
				finalPath:    noDependencies,
				semanticPath: noDependencies,
			},
			wantViolation: true,
		},
		{
			name: "runnable semantic task before final verification",
			plan: planSchedule(activePath, []string{semanticPath, finalPath}, nil),
			files: map[string]string{
				activePath:   "# active\n",
				finalPath:    noDependencies,
				semanticPath: noDependencies,
			},
		},
		{
			name: "blocked task after final verification is allowed",
			plan: planSchedule(activePath, []string{finalPath}, []string{semanticPath}),
			files: map[string]string{
				activePath:   "# active\n",
				finalPath:    noDependencies,
				semanticPath: noDependencies,
			},
		},
		{
			name: "task with outstanding dependency after final verification is not runnable",
			plan: planSchedule(activePath, []string{finalPath, semanticPath}, []string{blockedPath}),
			files: map[string]string{
				activePath:  "# active\n",
				finalPath:   noDependencies,
				blockedPath: noDependencies,
				semanticPath: "# task\n\n## Dependencies\n\n- `" + blockedPath + "`\n",
			},
		},
		{
			name: "final verification active rejects runnable next task",
			plan: planSchedule(finalPath, []string{semanticPath}, nil),
			files: map[string]string{
				finalPath:    noDependencies,
				semanticPath: noDependencies,
			},
			wantViolation: true,
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
			violations, err := taskScheduleClosureViolations(root, paths)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.wantViolation {
				if len(violations) != 0 {
					t.Fatalf("violations = %+v", violations)
				}
				return
			}
			if len(violations) != 1 {
				t.Fatalf("violations = %+v", violations)
			}
			violation := violations[0]
			if violation.Rule != scheduleClosureRule || violation.Path != implementationPlanPath {
				t.Fatalf("violation = %+v", violation)
			}
			if !strings.Contains(violation.Message, finalPath) || !strings.Contains(violation.Message, semanticPath) {
				t.Fatalf("message = %q", violation.Message)
			}
		})
	}
}

func planSchedule(active string, next, blocked []string) string {
	var builder strings.Builder
	builder.WriteString("## ACTIVE\n\n- `" + active + "`\n\n## NEXT（優先順）\n\n")
	for _, path := range next {
		builder.WriteString("- `" + path + "`\n")
	}
	builder.WriteString("\n## BLOCKED / USER_PERMISSION_WAIT\n\n")
	for _, path := range blocked {
		builder.WriteString("- `" + path + "`\n")
	}
	return builder.String()
}
