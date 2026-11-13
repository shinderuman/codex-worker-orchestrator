package repositoryproject

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func TestTerminalMetadataRetirementKeepsSourceAndRejectsSemanticScheduleChoices(t *testing.T) {
	root, child, unrelated := "IMPLEMENTATION_TASKS/root.md", "IMPLEMENTATION_TASKS/child.md", "IMPLEMENTATION_TASKS/unrelated.md"
	plan := []byte("## ACTIVE\n- `" + root + "`\n\n## NEXT\n- `" + unrelated + "`\n- `" + child + "`\n")
	source := []byte("# root\n\n## Original instruction\n\n````text\n## Dependencies\n- `" + child + "`\n````\n\n## Contract\nroot\n\n## Dependencies\n\n- `" + child + "`\n\n## Fulfilled dependencies\n\nnone\n")
	tasks := map[string][]byte{root: source, child: []byte("# child\n## Dependencies\nnone\n"), unrelated: []byte("# unrelated\n## Dependencies\nnone\n")}
	result, err := RetireTerminalMetadata(plan, tasks, child, true)
	if err != nil {
		t.Fatal(err)
	}
	changes := terminalMetadataChangesByPath(t, result)
	if change := changes[child]; !change.Delete || change.NewBytes != nil {
		t.Fatalf("terminal Task change = %#v", change)
	}
	rootChange, ok := changes[root]
	if !ok || rootChange.Delete {
		t.Fatalf("dependency retirement change = %#v", rootChange)
	}
	deps, err := taskcontract.ParseTaskDependencyState(rootChange.NewBytes)
	if err != nil || len(deps.Outstanding) != 0 || len(deps.Fulfilled) != 1 || deps.Fulfilled[0] != child {
		t.Fatal("dependency retirement mismatch")
	}
	quoted := "````text\n## Dependencies\n- `" + child + "`\n````"
	if !strings.Contains(string(rootChange.NewBytes), quoted) || !bytes.Equal(tasks[root], source) {
		t.Fatal("lossless source or input was rewritten")
	}
	planChange, ok := changes["IMPLEMENTATION_PLAN.local.md"]
	if !ok || planChange.Delete {
		t.Fatalf("plan retirement change = %#v", planChange)
	}
	active, err := taskcontract.ParsePlanSchedule(string(planChange.NewBytes)).ActiveTask()
	if err != nil || active != root {
		t.Fatal("blocker retirement replaced root")
	}
	tasks[root] = []byte("# root\n## Dependencies\n- `" + child + "`\nsemantic prerequisite note\n")
	if _, err := RetireTerminalMetadata(plan, tasks, child, true); err == nil {
		t.Fatal("ambiguous dependency section admitted")
	}
	tasks[root] = source
	if _, err := RetireTerminalMetadata(plan, tasks, root, false); err != nil {
		t.Fatal(err)
	}
	tasks[unrelated] = []byte("# unrelated\n## Dependencies\n- `" + child + "`\n")
	if _, err := RetireTerminalMetadata(plan, tasks, root, false); err == nil {
		t.Fatal("nonrunnable first NEXT skipped")
	}
	if _, err := RetireTerminalMetadata(plan, tasks, child, false); err == nil {
		t.Fatal("arbitrary nonfocus promoted as terminal root")
	}
}

func terminalMetadataChangesByPath(t *testing.T, metadata TerminalMetadata) map[string]TerminalMetadataChange {
	t.Helper()
	changes := make(map[string]TerminalMetadataChange, len(metadata.Changes))
	for _, change := range metadata.Changes {
		if change.Path == "" || changes[change.Path].Path != "" {
			t.Fatalf("ambiguous terminal metadata change %#v", change)
		}
		changes[change.Path] = change
	}
	return changes
}

func TestLastTaskRetirementPreservesBlockedAndValidatesIdleCandidate(t *testing.T) {
	root, blocked := "IMPLEMENTATION_TASKS/root.md", "IMPLEMENTATION_TASKS/blocked.md"
	for _, hasBlocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "blocked"}[hasBlocked], func(t *testing.T) {
			plan := "## ACTIVE\n\n- [`" + root + "`](" + root + ")\n\n## NEXT\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n"
			tasks := map[string][]byte{root: []byte("# root\n\n## Dependencies\n\nnone\n")}
			if hasBlocked {
				plan += "- [`" + blocked + "`](" + blocked + ")\n"
				tasks[blocked] = []byte("# blocked\n\n## Dependencies\n\n- `" + root + "`\n")
			}
			result, err := RetireTerminalMetadata([]byte(plan), tasks, root, false)
			if err != nil {
				t.Fatal(err)
			}
			changes := terminalMetadataChangesByPath(t, result)
			if result.Successor != "" || !changes[root].Delete {
				t.Fatalf("last Task was not retired: %#v", result)
			}
			candidate := string(changes["IMPLEMENTATION_PLAN.local.md"].NewBytes)
			prepared, err := PrepareFinalHead(candidate)
			if err != nil || prepared.ActiveTask != "" || len(prepared.Tasks) != len(tasks)-1 {
				t.Fatalf("idle candidate: %#v %v", prepared, err)
			}
			if _, err := prepared.Schedule.ActiveTask(); err == nil {
				t.Fatal("idle project admitted a new Task without ACTIVE")
			}
			if hasBlocked {
				deps, err := taskcontract.ParseTaskDependencyState(changes[blocked].NewBytes)
				if err != nil || len(deps.Outstanding) != 0 || len(deps.Fulfilled) != 1 || deps.Fulfilled[0] != root || prepared.Tasks[0] != blocked {
					t.Fatalf("blocked requirement lost: %#v %v", deps, err)
				}
			}
			for _, invalid := range []string{"", "## ACTIVE\n\n## NEXT\n\n- `" + blocked + "`\n\n## BLOCKED\n", "## GOAL\n\nstatus: active\n\n" + candidate} {
				if _, err := PrepareFinalHead(invalid); err == nil {
					t.Fatalf("invalid idle project admitted: %q", invalid)
				}
			}
		})
	}
}

func TestRetireTerminalMetadataNormalizesPlanWithNextBoundaryAndDependencies(t *testing.T) {
	root, first, second, blocked := "IMPLEMENTATION_TASKS/root.md", "IMPLEMENTATION_TASKS/first.md",
		"IMPLEMENTATION_TASKS/second.md", "IMPLEMENTATION_TASKS/blocked.md"
	link := func(path string) string { return "- [`" + path + "`](" + path + ")" }
	for _, next := range [][]string{{first, second}, {first}} {
		plan := "# Plan\n\n## ACTIVE\n\n" + link(root) + "\n\n## NEXT\n\n"
		for _, path := range next {
			plan += link(path) + "\n"
		}
		plan += "\n## BLOCKED\n\n" + link(blocked) + "\n"
		tasks := map[string][]byte{
			root:    []byte("# root\n\n## Dependencies\n\nnone\n"),
			first:   []byte("# first\n\n## Dependencies\n\nnone\n"),
			blocked: []byte("# blocked\n\n## Dependencies\n\n- `" + root + "`\n"),
		}
		if len(next) == 2 {
			tasks[second] = []byte("# second\n\n## Dependencies\n\nnone\n")
		}
		result, err := RetireTerminalMetadata([]byte(plan), tasks, root, false)
		if err != nil {
			t.Fatal(err)
		}
		changes := terminalMetadataChangesByPath(t, result)
		if !changes[root].Delete || result.Successor != first {
			t.Fatalf("retirement deleted wrong Task or promoted wrong NEXT: %#v", result)
		}
		planResult := string(changes["IMPLEMENTATION_PLAN.local.md"].NewBytes)
		if strings.Contains(planResult, "\n\n\n") ||
			!strings.Contains(planResult, "## ACTIVE\n\n"+link(first)) ||
			!strings.Contains(planResult, "## BLOCKED\n\n"+link(blocked)) {
			t.Fatalf("generated Plan lost canonical spacing or links:\n%s", planResult)
		}
		parsed := taskcontract.ParsePlanSchedule(planResult)
		active, err := parsed.ValidateComplete()
		if err != nil || active != first || len(parsed.Next) != len(next)-1 ||
			len(parsed.Blocked) != 1 || parsed.Blocked[0] != blocked {
			t.Fatalf("generated Plan lost schedule: %#v, %v", parsed, err)
		}
		if len(next) == 2 && (parsed.Next[0] != second || !strings.Contains(planResult, link(second))) {
			t.Fatalf("generated Plan reordered NEXT: %s", planResult)
		}
		deps, err := taskcontract.ParseTaskDependencyState(changes[blocked].NewBytes)
		if err != nil || len(deps.Outstanding) != 0 || len(deps.Fulfilled) != 1 || deps.Fulfilled[0] != root {
			t.Fatalf("dependent Task did not record the original fulfilled dependency: %#v %v", deps, err)
		}
	}
}
