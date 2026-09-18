package workflow

import (
	"strings"
	"testing"
)

func TestResolveAndPinActiveTaskRejectsStalePinnedTask(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)
	pinRepositoryHarnessActiveResumeT(t, st)
	if err := st.Write(activeTaskStateKey, "IMPLEMENTATION_TASKS/stale.md"); err != nil {
		t.Fatal(err)
	}

	if _, err := w.resolveAndPinActiveTask(); err == nil || !strings.Contains(err.Error(), "no longer matches Plan ACTIVE") {
		t.Fatalf("stale pinned ACTIVE task was not rejected: %v", err)
	}
}

func TestResolveAndPinActiveTaskKeepsMatchingPinnedTask(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)
	pinRepositoryHarnessActiveResumeT(t, st)
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}

	got, err := w.resolveAndPinActiveTask()
	if err != nil {
		t.Fatal(err)
	}
	if got != activeTaskGuardPath {
		t.Fatalf("ACTIVE task = %q want %q", got, activeTaskGuardPath)
	}
}
