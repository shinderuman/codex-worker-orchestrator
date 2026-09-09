package workflow

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func trackRepositoryHarnessMarker(t *testing.T, repoRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, repositoryharness.MarkerPath), []byte(repositoryharness.MarkerContent), 0o644); err != nil {
		t.Fatal(err)
	}
	markerGit(t, repoRoot, "add", "--", repositoryharness.MarkerPath)
}

func markerGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeForeignProductionFile(root string) error {
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "src", "app.go"), []byte("package src\n"), 0o644)
}

func initForeignRepository(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	markerGit(t, repoRoot, "init", "-q")
	markerGit(t, repoRoot, "config", "user.email", "test@example.com")
	markerGit(t, repoRoot, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repoRoot, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	markerGit(t, repoRoot, "add", ".")
	markerGit(t, repoRoot, "commit", "-q", "-m", "base")
	return repoRoot
}

func requireNoRepositoryPolicyInjection(t *testing.T, prompts []string) {
	t.Helper()
	for i, prompt := range prompts {
		if strings.Contains(prompt, "ACTIVE_TASK_FILE") ||
			strings.Contains(prompt, "SOL_DECISION_BOUNDARY") ||
			strings.Contains(prompt, "ACTIVE_TASK_CONTEXT") {
			t.Fatalf("marker不在repoのprompt %dへrepository harness blockが注入されています:\n%s", i, prompt)
		}
	}
}

func requireEmptyPinnedState(t *testing.T, st *state.StateStore, key string) {
	t.Helper()
	if !st.Exists(key) {
		t.Fatalf("state %s が空値で固定されていません", key)
	}
	if got, err := st.Read(key); err != nil || got != "" {
		t.Fatalf("state %s = (%q,%v) want 空値", key, got, err)
	}
}

func requireRepositoryHarnessFailClosed(t *testing.T, st *state.StateStore, out *bytes.Buffer, wantSubstring string) {
	t.Helper()
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("marker境界fail closed後にresume checkpointが残っています")
	}
	if !strings.Contains(out.String(), wantSubstring) {
		t.Fatalf("marker境界のfail closed理由が出力されていません:\n%s", out.String())
	}
}

func requireMarkerOutcomeEvent(t *testing.T, st *state.StateStore, outcome string) {
	t.Helper()
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeEvent && l.Outcome == outcome {
			return
		}
	}
	t.Fatalf("marker境界event outcome %sが記録されていません: %+v", outcome, phasesOf(taskLogs(t, st)))
}

func TestGenericForeignRepositoryRunsGenericHarnessWithoutRepositoryPolicy(t *testing.T) {
	repoRoot := initForeignRepository(t)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, writeForeignProductionFile)
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"src/app.go"}, nil
	}

	if err := w.ExecuteNewTask("foreign request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("marker不在repoのgeneric taskが完了していません: %q\n%s", st.TaskStatus(), out.String())
	}
	if len(r.prompts) != 2 {
		t.Fatalf("worker/reviewer 2呼出が必要: %d(%v)", len(r.prompts), r.phases)
	}
	requireNoRepositoryPolicyInjection(t, r.prompts)
	requireEmptyPinnedState(t, st, activeTaskStateKey)
	requireEmptyPinnedState(t, st, repositoryharness.ActivationStateKey)
}

func TestActivatedRepositoryKeepsPolicySurfaces(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}, "", 0, nil)
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"src/app.go"}, nil
	}

	if err := w.ExecuteNewTask("activated request"); err != nil {
		t.Fatal(err)
	}
	if got := st.ReadOr(activeTaskStateKey, ""); got != activeTaskGuardPath {
		t.Fatalf("active-task state = %q want %q", got, activeTaskGuardPath)
	}
	if got := st.ReadOr(repositoryharness.ActivationStateKey, ""); got != repositoryharness.ActivationActiveValue {
		t.Fatalf("activation pin = %q want %q", got, repositoryharness.ActivationActiveValue)
	}
	if len(r.prompts) != 3 {
		t.Fatalf("unknown-surface変更はrisk floorでreviewer PASSを許容しない: %d(%v)", len(r.prompts), r.phases)
	}
	for i, prompt := range r.prompts[:2] {
		if !strings.Contains(prompt, "ACTIVE_TASK_FILE: "+activeTaskGuardPath) {
			t.Fatalf("prompt %dがACTIVE task file読み込み指示を欠いています:\n%s", i, prompt)
		}
	}
	if !strings.Contains(r.prompts[1], "RISK_FLOOR_SOURCE: self-protection:unknown-surface") {
		t.Fatalf("reviewer promptがself-protection risk floorを欠いています:\n%s", r.prompts[1])
	}
	if !strings.Contains(r.prompts[2], "risk floor") {
		t.Fatalf("prompt 2がrisk floor再出力指示であるべきです:\n%s", r.prompts[2])
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("risk floor後のtask status = %q want waiting-sol-review", st.TaskStatus())
	}
	if !strings.Contains(out.String(), "NEEDS_SOL_REVIEW") {
		t.Fatalf("risk floorの最終packetが出力されていません:\n%s", out.String())
	}
}

func TestCoincidentalProtocolNamesDoNotActivateRepositoryPolicy(t *testing.T) {
	repoRoot := initForeignRepository(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("foreign request with same-named files"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("偶然同名file存在時もgeneric taskとして完了すべき: %q\n%s", st.TaskStatus(), out.String())
	}
	if len(r.prompts) != 2 {
		t.Fatalf("worker/reviewer 2呼出が必要: %d(%v)", len(r.prompts), r.phases)
	}
	requireNoRepositoryPolicyInjection(t, r.prompts)
	requireEmptyPinnedState(t, st, activeTaskStateKey)
}

func TestCoincidentalMalformedPlanDoesNotFailClosedInForeignRepository(t *testing.T) {
	repoRoot := initForeignRepository(t)
	writePlanFileContent(t, repoRoot, "# plan without active\n")
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("foreign request with malformed plan"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("marker不在repoのmalformed planはfail closedにしない: %q\n%s", st.TaskStatus(), out.String())
	}
	if len(r.prompts) != 2 {
		t.Fatalf("marker不在repoはACTIVE解決で停止しない: %d(%v)", len(r.prompts), r.phases)
	}
	requireNoRepositoryPolicyInjection(t, r.prompts)
}

func TestForeignPlanMutationDuringCallIsNotParentMetadataViolation(t *testing.T) {
	repoRoot := initForeignRepository(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, mutatePlanFile)

	if err := w.ExecuteNewTask("foreign request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("marker不在repoのplan変更は親管理metadata違反にしない: %q\n%s", st.TaskStatus(), out.String())
	}
	if strings.Contains(out.String(), "parent_metadata") {
		t.Fatalf("marker不在repoでparent_metadata違反が出力されています:\n%s", out.String())
	}
	if len(r.prompts) != 2 {
		t.Fatalf("marker不在repoはguard違反で停止しない: %d(%v)", len(r.prompts), r.phases)
	}
}

func TestMarkerLossDuringActivatedCallFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
	}, "worker-new", 0, func(root string) error {
		return os.Remove(filepath.Join(root, repositoryharness.MarkerPath))
	})

	if err := w.ExecuteNewTask("activated request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("marker削除後はreviewerを呼ばず停止すべき: %d(%v)", len(r.prompts), r.phases)
	}
	requireRepositoryHarnessFailClosed(t, st, out, "が変化しました")
	requireMarkerOutcomeEvent(t, st, repositoryHarnessGuardSurface.mismatchOutcome())
}

func TestMarkerRewriteDuringActivatedCallFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
	}, "worker-new", 0, func(root string) error {
		return os.WriteFile(filepath.Join(root, repositoryharness.MarkerPath), []byte("tampered\n"), 0o644)
	})

	if err := w.ExecuteNewTask("activated request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("marker書換後はreviewerを呼ばず停止すべき: %d(%v)", len(r.prompts), r.phases)
	}
	requireRepositoryHarnessFailClosed(t, st, out, "が変化しました")
	requireMarkerOutcomeEvent(t, st, repositoryHarnessGuardSurface.mismatchOutcome())
}

func TestMarkerSymlinkDuringActivatedCallFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
	}, "worker-new", 0, func(root string) error {
		marker := filepath.Join(root, repositoryharness.MarkerPath)
		if err := os.Remove(marker); err != nil {
			return err
		}
		return os.Symlink("elsewhere", marker)
	})

	if err := w.ExecuteNewTask("activated request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("marker symlink化後はreviewerを呼ばず停止すべき: %d(%v)", len(r.prompts), r.phases)
	}
	requireRepositoryHarnessFailClosed(t, st, out, "が変化しました")
	requireMarkerOutcomeEvent(t, st, repositoryHarnessGuardSurface.mismatchOutcome())
}

func TestMarkerUntrackedDuringActivatedCallFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
	}, "worker-new", 0, func(root string) error {
		markerGit(t, root, "rm", "-q", "--cached", "--", repositoryharness.MarkerPath)
		return nil
	})

	if err := w.ExecuteNewTask("activated request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("marker未追跡化後はreviewerを呼ばず停止すべき: %d(%v)", len(r.prompts), r.phases)
	}
	requireRepositoryHarnessFailClosed(t, st, out, "が無効化されました(untracked)")
	requireMarkerOutcomeEvent(t, st, repositoryHarnessGuardSurface.outcomePrefix+"_untracked")
}

func TestActivatedTaskMarkerInvalidBeforeCallFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
		mutate  func(t *testing.T, repoRoot string)
	}{
		{"missing", repositoryHarnessGuardSurface.missingOutcome(), func(t *testing.T, repoRoot string) {
			if err := os.Remove(filepath.Join(repoRoot, repositoryharness.MarkerPath)); err != nil {
				t.Fatal(err)
			}
		}},
		{"content-mismatch", repositoryHarnessGuardSurface.mismatchOutcome(), func(t *testing.T, repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, repositoryharness.MarkerPath), []byte("wrong\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"untracked", repositoryHarnessGuardSurface.outcomePrefix + "_untracked", func(t *testing.T, repoRoot string) {
			markerGit(t, repoRoot, "rm", "-q", "--cached", "--", repositoryharness.MarkerPath)
		}},
		{"symlink", repositoryHarnessGuardSurface.malformedOutcome(), func(t *testing.T, repoRoot string) {
			marker := filepath.Join(repoRoot, repositoryharness.MarkerPath)
			target := filepath.Join(repoRoot, "marker-target")
			if err := os.WriteFile(target, []byte(repositoryharness.MarkerContent), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("marker-target", marker); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writePlanFileContent(t, repoRoot, planGuardSeed)
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)
			if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
				t.Fatal(err)
			}
			if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
				t.Fatal(err)
			}
			if err := st.Write("last-request", "activated request"); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, repoRoot)

			if _, stopped, err := w.captureParentFileGuard(state.WorkerRole); !stopped || err == nil {
				t.Fatalf("activation固定taskのmarker無効化は呼出前にfail closedすべき: stopped=%t err=%v", stopped, err)
			}
			if len(r.prompts) != 0 {
				t.Fatalf("model呼出前に停止すべき: %d", len(r.prompts))
			}
			requireRepositoryHarnessFailClosed(t, st, out, repositoryharness.MarkerPath)
			requireMarkerOutcomeEvent(t, st, tc.outcome)
		})
	}
}

func TestPinnedActiveTaskWithoutMarkerFailsClosedBeforeCall(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	st := newStateStoreT(t)
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "activated request"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "worker-explicit-fix", nil)
	if err := os.Remove(filepath.Join(repoRoot, repositoryharness.MarkerPath)); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteExplicitFix("fix", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("marker欠損時はfixでもmodel呼出前に停止すべき: %d", len(r.prompts))
	}
	requireRepositoryHarnessFailClosed(t, st, out, "がrepository rootへ存在しません")
	requireMarkerOutcomeEvent(t, st, repositoryHarnessGuardSurface.missingOutcome())
}

func TestMarkerVariantsDoNotOptIn(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, repoRoot string)
	}{
		{"wrong-content", func(t *testing.T, repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, repositoryharness.MarkerPath), []byte("github.com/other/harness/v1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"empty-content", func(t *testing.T, repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, repositoryharness.MarkerPath), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"content-without-newline", func(t *testing.T, repoRoot string) {
			if err := os.WriteFile(filepath.Join(repoRoot, repositoryharness.MarkerPath), []byte(strings.TrimSuffix(repositoryharness.MarkerContent, "\n")), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"untracked-exact-content", func(t *testing.T, repoRoot string) {
			markerGit(t, repoRoot, "rm", "-q", "--cached", "--", repositoryharness.MarkerPath)
		}},
		{"symlink-exact-content", func(t *testing.T, repoRoot string) {
			marker := filepath.Join(repoRoot, repositoryharness.MarkerPath)
			target := filepath.Join(repoRoot, "marker-target")
			if err := os.WriteFile(target, []byte(repositoryharness.MarkerContent), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("marker-target", marker); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initForeignRepository(t)
			trackRepositoryHarnessMarker(t, repoRoot)
			markerGit(t, repoRoot, "commit", "-q", "-m", "marker")
			tc.mutate(t, repoRoot)
			writePlanFileContent(t, repoRoot, planGuardSeed)
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("done")},
				{structured: passPacket()},
			}, "", 0, nil)

			if err := w.ExecuteNewTask("foreign request"); err != nil {
				t.Fatal(err)
			}
			if st.TaskStatus() != state.TaskStatusComplete {
				t.Fatalf("marker不正時はopt-inせずgeneric完了すべき: %q\n%s", st.TaskStatus(), out.String())
			}
			if len(r.prompts) != 2 {
				t.Fatalf("marker不正時もgeneric 2呼出: %d(%v)", len(r.prompts), r.phases)
			}
			requireNoRepositoryPolicyInjection(t, r.prompts)
			requireEmptyPinnedState(t, st, activeTaskStateKey)
		})
	}
}
