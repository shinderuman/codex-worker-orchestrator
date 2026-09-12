package workflow

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestRepositoryHarnessActivationPinRejectsInconsistentState(t *testing.T) {
	cases := []struct {
		name          string
		activation    *string
		wantSubstring string
	}{
		{name: "missing", wantSubstring: "欠落"},
		{name: "inactive", activation: stringPtr(repositoryharness.ActivationInactiveValue), wantSubstring: "inactive"},
		{name: "unknown", activation: stringPtr("unexpected"), wantSubstring: "不正"},
		{name: "padded-active", activation: stringPtr(" 1 "), wantSubstring: "不正"},
		{name: "whitespace", activation: stringPtr(" "), wantSubstring: "不正"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)
			if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
				t.Fatal(err)
			}
			if tc.activation == nil {
				if err := st.Remove(repositoryharness.ActivationStateKey); err != nil {
					t.Fatal(err)
				}
			} else if err := st.Write(repositoryharness.ActivationStateKey, *tc.activation); err != nil {
				t.Fatal(err)
			}

			active, err := w.repositoryHarnessActive()
			if err == nil || active {
				t.Fatalf("inconsistent activation pin accepted: active=%v err=%v", active, err)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("error = %q want substring %q", err, tc.wantSubstring)
			}
		})
	}
}

func TestRepositoryHarnessActivationPinRejectsUnreadableState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("state file permission injection is Unix-oriented")
	}
	repoRoot := initMutationRepo(t)
	w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
	path := st.Path(repositoryharness.ActivationStateKey)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	active, err := w.repositoryHarnessActive()
	if err == nil || active {
		t.Fatalf("unreadable activation pin accepted: active=%v err=%v", active, err)
	}
	if !strings.Contains(err.Error(), "読み込めません") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryHarnessActivationPinAcceptsValidActiveState(t *testing.T) {
	repoRoot := initMutationRepo(t)
	w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}

	active, err := w.repositoryHarnessActive()
	if err != nil || !active {
		t.Fatalf("valid active pin rejected: active=%v err=%v", active, err)
	}
}

func TestRepositoryHarnessInactivePinKeepsForeignRepositoryGeneric(t *testing.T) {
	repoRoot := initForeignRepository(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)
	if err := st.Write(activeTaskStateKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationInactiveValue); err != nil {
		t.Fatal(err)
	}

	trackRepositoryHarnessMarker(t, repoRoot)
	active, err := w.repositoryHarnessActive()
	if err != nil || active {
		t.Fatalf("valid inactive pin did not preserve generic mode: active=%v err=%v", active, err)
	}
}

func TestRepositoryHarnessMissingActivationDoesNotReevaluatePinnedGenericTask(t *testing.T) {
	repoRoot := initForeignRepository(t)
	w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)
	if err := st.Write(activeTaskStateKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.Remove(repositoryharness.ActivationStateKey); err != nil {
		t.Fatal(err)
	}
	trackRepositoryHarnessMarker(t, repoRoot)

	active, err := w.repositoryHarnessActive()
	if err == nil || active {
		t.Fatalf("missing activation pin silently reevaluated generic task: active=%v err=%v", active, err)
	}
	if !strings.Contains(err.Error(), "欠落") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func stringPtr(value string) *string {
	return &value
}
