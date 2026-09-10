from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one match, got {count}")
    p.write_text(text.replace(old, new, 1))


# StateStore owns cleanup of state that cannot cross a new task identity.
replace_once(
    "glm-worker/internal/state/store.go",
    '''\tif err := s.Remove(\n\t\t"task.status",\n\t\tparentCodexIdentityFile,\n\t\t"isolation.policy",\n\t\t"baseline-head",\n\t\tExecutionMilestonesStateFile,\n\t\tstopWorktreePatchFile,\n\t\tstopIndexPatchFile,\n\t\tisolationStateFile,\n\t\tworkerEndSnapshotFile,\n\t\treviewStartSnapshotFile,\n\t\treportOnlyStartSnapshotFile,\n\t\tsnapshotComparisonFile,\n\t\tguardRepairStateFile,\n\t); err != nil {\n''',
    '''\tif err := s.Remove(newTaskTransitionStateFileNames()...); err != nil {\n''',
)
replace_once(
    "glm-worker/internal/state/store.go",
    '''func taskStateFileNames() []string {\n\treturn []string{\n\t\t"task.id",\n\t\t"worker.id",\n\t\t"worker.ready",\n\t\t"reviewer.id",\n\t\t"reviewer.ready",\n\t\t"task.status",\n\t\tparentCodexIdentityFile,\n\t\t"isolation.policy",\n\n\t\t"active-task",\n\t\t"last-request",\n\t\t"last-decision",\n\t\t"pending-decision",\n\t\t"last-review",\n\t\t"baseline-status",\n\t\t"baseline-head",\n\t\t"baseline-worktree.patch",\n\t\t"baseline-index.patch",\n\t\t"accepted-fix-scope.json",\n\t\tExecutionMilestonesStateFile,\n\t\tstopWorktreePatchFile,\n\t\tstopIndexPatchFile,\n\t\tisolationStateFile,\n\t\tisolationOriginStateFile,\n\t\tresumeStateFile,\n\t\tworkerEndSnapshotFile,\n\t\treviewStartSnapshotFile,\n\t\treportOnlyStartSnapshotFile,\n\t\tsnapshotComparisonFile,\n\t\tguardRepairStateFile,\n\t}\n}\n''',
    '''func newTaskTransitionStateFileNames() []string {\n\treturn []string{\n\t\t"task.status",\n\t\tparentCodexIdentityFile,\n\t\t"isolation.policy",\n\t\t"active-task",\n\t\t"last-request",\n\t\t"last-decision",\n\t\t"pending-decision",\n\t\t"last-review",\n\t\t"baseline-status",\n\t\t"baseline-head",\n\t\t"baseline-worktree.patch",\n\t\t"baseline-index.patch",\n\t\t"accepted-fix-scope.json",\n\t\tExecutionMilestonesStateFile,\n\t\tstopWorktreePatchFile,\n\t\tstopIndexPatchFile,\n\t\tisolationStateFile,\n\t\tresumeStateFile,\n\t\tworkerEndSnapshotFile,\n\t\treviewStartSnapshotFile,\n\t\treportOnlyStartSnapshotFile,\n\t\tsnapshotComparisonFile,\n\t\tguardRepairStateFile,\n\t}\n}\n\nfunc taskStateFileNames() []string {\n\tnames := []string{\n\t\t"task.id",\n\t\t"worker.id",\n\t\t"worker.ready",\n\t\t"reviewer.id",\n\t\t"reviewer.ready",\n\t\tisolationOriginStateFile,\n\t}\n\treturn append(names, newTaskTransitionStateFileNames()...)\n}\n''',
)

# Workflow no longer owns cleanup of task-scoped state; repository harness activation remains workflow-owned.
replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''\tif err := w.state.Remove("last-decision", "last-review", activeTaskStateKey, acceptedFixScopeStateFile, repositoryharness.ActivationStateKey); err != nil {\n''',
    '''\tif err := w.state.Remove(repositoryharness.ActivationStateKey); err != nil {\n''',
)

# Command inventory is centralized: no magic mode values and no init-time mutation of the parser registry.
replace_once(
    "glm-worker/internal/app/app.go",
    '''\tModeProjectState\n\tModeEvidence\n)\n''',
    '''\tModeProjectState\n\tModeEvidence\n\tmodeRotateInstructionBaseline\n\tmodeRecoverParentAction\n\tmodeRecoverQualitySurface\n)\n''',
)
replace_once(
    "glm-worker/internal/app/app.go",
    '''\t"--reset": func(args []string) (Command, error) {\n\t\treturn singleArgCommand(args, ModeReset, "usage: glm-worker --reset")\n\t},\n\t"--verify-auto-resume":  verifyAutoResumeCommand,\n''',
    '''\t"--reset": func(args []string) (Command, error) {\n\t\treturn singleArgCommand(args, ModeReset, "usage: glm-worker --reset")\n\t},\n\t"--rotate-instruction-baseline": func(args []string) (Command, error) {\n\t\treturn singleArgCommand(args, modeRotateInstructionBaseline, "usage: glm-worker --rotate-instruction-baseline")\n\t},\n\t"--recover-parent-action": func(args []string) (Command, error) {\n\t\treturn singleArgCommand(args, modeRecoverParentAction, "usage: glm-worker --recover-parent-action")\n\t},\n\t"--recover-quality-surface": func(args []string) (Command, error) {\n\t\treturn requiredPayloadCommand(args, modeRecoverQualitySurface, "usage: glm-worker --recover-quality-surface <task-id>")\n\t},\n\t"--verify-auto-resume":  verifyAutoResumeCommand,\n''',
)

replace_once(
    "glm-worker/internal/app/instruction_baseline.go",
    '''const modeRotateInstructionBaseline CommandMode = 100\n\nfunc init() {\n\tcommandParsers["--rotate-instruction-baseline"] = func(args []string) (Command, error) {\n\t\treturn singleArgCommand(args, modeRotateInstructionBaseline, "usage: glm-worker --rotate-instruction-baseline")\n\t}\n}\n\n''',
    '',
)
replace_once(
    "glm-worker/internal/app/parent_action_recovery.go",
    '''const modeRecoverParentAction CommandMode = 101\n\nconst modelCallOutcomeError = "error"\n\nfunc init() {\n\tcommandParsers["--recover-parent-action"] = func(args []string) (Command, error) {\n\t\treturn singleArgCommand(args, modeRecoverParentAction, "usage: glm-worker --recover-parent-action")\n\t}\n}\n\n''',
    '''const modelCallOutcomeError = "error"\n\n''',
)
replace_once(
    "glm-worker/internal/app/quality_surface_recovery.go",
    '''const modeRecoverQualitySurface CommandMode = 102\n\nconst (\n''',
    '''const (\n''',
)
replace_once(
    "glm-worker/internal/app/quality_surface_recovery.go",
    '''func init() {\n\tcommandParsers["--recover-quality-surface"] = func(args []string) (Command, error) {\n\t\treturn requiredPayloadCommand(args, modeRecoverQualitySurface, "usage: glm-worker --recover-quality-surface <task-id>")\n\t}\n}\n\n''',
    '',
)

# Regression coverage: a new task identity must not inherit task-scoped context.
test = Path("glm-worker/internal/state/task_transition_context_test.go")
test.write_text(r'''package state

import "testing"

func TestStartNewTaskClearsPreviousTaskContext(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	firstTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	stale := []string{
		"active-task",
		"last-request",
		"last-decision",
		"pending-decision",
		"last-review",
		"baseline-status",
		"baseline-head",
		"baseline-worktree.patch",
		"baseline-index.patch",
		"accepted-fix-scope.json",
		ExecutionMilestonesStateFile,
		resumeStateFile,
	}
	for _, name := range stale {
		if err := st.Write(name, "stale"); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := st.Write(parentCodexIdentityFile, "stale"); err != nil {
		t.Fatal(err)
	}

	secondTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if secondTask == firstTask {
		t.Fatalf("task identity was reused: %s", secondTask)
	}
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	for _, name := range append(stale, parentCodexIdentityFile) {
		if st.Exists(name) {
			t.Errorf("new task inherited %s", name)
		}
	}
}

func TestTaskResetIncludesNewTaskTransitionState(t *testing.T) {
	transition := make(map[string]bool)
	for _, name := range newTaskTransitionStateFileNames() {
		transition[name] = true
	}
	reset := make(map[string]bool)
	for _, name := range taskStateFileNames() {
		reset[name] = true
	}
	for name := range transition {
		if !reset[name] {
			t.Errorf("task reset omits transition state %s", name)
		}
	}
}
''')
