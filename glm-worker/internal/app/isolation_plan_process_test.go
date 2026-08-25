//go:build unix

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolatePlanLifecycleProcessSeries(t *testing.T) {
	env := newMultiRepoEnv(t)

	const originalTaskPath = "IMPLEMENTATION_TASKS/original-task.md"
	const interruptionTaskPath = "IMPLEMENTATION_TASKS/interruption-task.md"
	originalTaskBody := []byte("# 元task\n\n割り込み前に実行中だったtask本文。\n\n## External feasibility\n\nstatus: not-applicable\n")
	planBody := []byte("# 計画\n\n## ACTIVE\n\n- `" + originalTaskPath + "`\n")
	if err := os.MkdirAll(filepath.Join(env.repoA, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.repoA, originalTaskPath), originalTaskBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.repoA, "IMPLEMENTATION_PLAN.local.md"), planBody, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", env.repoA, "add", "IMPLEMENTATION_PLAN.local.md", originalTaskPath},
		{"-C", env.repoA, "commit", "-q", "-m", "parent metadata"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v失敗: %v: %s", args, err, output)
		}
	}

	env.setStubMode(t, env.stubA, "dirty-hold")
	holdCtx, cancelHold := context.WithTimeout(context.Background(), multiRepoRunTimeout)
	t.Cleanup(cancelHold)
	holder := env.start(t, holdCtx, env.repoA, "plan lifecycle worker marker ISOP1")
	stateA := env.waitStateDir(t, env.repoA, holder)
	env.waitHeldWithWorkerSession(t, stateA)
	waitStopFile(t, filepath.Join(env.repoA, "uncommitted.txt"))
	stopResult := env.run(t, env.repoA, "--stop")
	if stopResult.code != 0 || !strings.Contains(stopResult.stdout, `"result":"interrupted"`) {
		t.Fatalf("--stopがinterruptedになりません: code=%d stdout=%s stderr=%s", stopResult.code, stopResult.stdout, stopResult.stderr)
	}
	holder.waitFailure(t)
	if got := readStateFile(t, stateA, "active-task"); got != originalTaskPath {
		t.Fatalf("元taskの要求正本固定 = %q want %q", got, originalTaskPath)
	}

	isolate := env.run(t, env.repoA, "--isolate")
	if isolate.code != 0 || !strings.Contains(isolate.stdout, `"result":"isolated"`) {
		t.Fatalf("--isolateが失敗しました: code=%d stdout=%s stderr=%s", isolate.code, isolate.stdout, isolate.stderr)
	}
	worktree := isolationOutputField(t, isolate.stdout, "worktree")
	branch := isolationOutputField(t, isolate.stdout, "branch")

	interruptionTaskBody := []byte("# 割り込みtask\n\n隔離worktreeで実行するtask本文。\n\n## External feasibility\n\nstatus: not-applicable\n")
	switchedPlan := []byte("# 計画\n\n## ACTIVE\n\n- `" + interruptionTaskPath + "`\n")
	if err := os.WriteFile(filepath.Join(worktree, interruptionTaskPath), interruptionTaskBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "IMPLEMENTATION_PLAN.local.md"), switchedPlan, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", worktree, "add", interruptionTaskPath},
		{"-C", worktree, "commit", "-q", "-m", "interruption task file"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v失敗: %v: %s", args, err, output)
		}
	}
	env.setStubMode(t, env.stubA, "success")
	interrupted := env.run(t, worktree, "割り込みtask marker ISOP2")
	if interrupted.code != 0 || !strings.Contains(interrupted.stdout, `"status":"NEEDS_SOL_REVIEW"`) ||
		!strings.Contains(interrupted.stdout, "risk floor") {
		t.Fatalf("割り込みtaskがPlan編集込みの実risk HIGH経路でSol確認へ昇格しません: code=%d stdout=%s stderr=%s", interrupted.code, interrupted.stdout, interrupted.stderr)
	}
	worktreeState := env.waitStateDir(t, worktree, nil)
	if status := env.status(t, worktree); !strings.Contains(status, `"task_status":"waiting-sol-review"`) {
		t.Fatalf("割り込みtaskの終了状態 = %s", status)
	}
	if got := readStateFile(t, worktreeState, "active-task"); got != interruptionTaskPath {
		t.Fatalf("隔離taskの要求正本 = %q want %q(Plan切替が隔離側task開始へ反映されていません)", got, interruptionTaskPath)
	}
	if got := readStateFile(t, stateA, "active-task"); got != originalTaskPath {
		t.Fatalf("元taskの要求正本固定が隔離実行で変化しています: %q", got)
	}

	if err := os.WriteFile(filepath.Join(worktree, "deliverable.txt"), []byte("割り込み成果\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", worktree, "add", "deliverable.txt"},
		{"-C", worktree, "commit", "-q", "-m", "interruption task result"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v失敗: %v: %s", args, err, output)
		}
	}
	worktreeStatus, err := exec.Command("git", "-C", worktree, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(worktreeStatus), " M IMPLEMENTATION_PLAN.local.md") {
		t.Fatalf("統合commitがPlan編集を含めていないことを確認できません: %q", worktreeStatus)
	}

	if output, err := exec.Command("git", "-C", env.repoA, "merge", "--quiet", "--no-edit", branch).CombinedOutput(); err != nil {
		t.Fatalf("隔離branchの統合に失敗しました: %v: %s", err, output)
	}
	planAfter, err := os.ReadFile(filepath.Join(env.repoA, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(planAfter) != string(planBody) {
		t.Fatalf("統合が元repoのPlanを変化させています: %q", planAfter)
	}
	originalAfter, err := os.ReadFile(filepath.Join(env.repoA, originalTaskPath))
	if err != nil {
		t.Fatalf("統合後に元task fileが存在しません: %v", err)
	}
	if string(originalAfter) != string(originalTaskBody) {
		t.Fatal("統合が元task fileの本文を変化させています")
	}
	if !fileExists(filepath.Join(env.repoA, interruptionTaskPath)) {
		t.Fatal("統合後に割り込みtask fileが存在しません")
	}
	if !fileExists(filepath.Join(env.repoA, "uncommitted.txt")) {
		t.Fatal("統合が元taskの未commit作業を消しています")
	}

	resumed := env.run(t, env.repoA, "--resume")
	if resumed.code != 0 || !strings.Contains(resumed.stdout, `"status":"NEEDS_SOL_REVIEW"`) {
		t.Fatalf("統合後の元task resumeが保持照合を通過しません: code=%d stdout=%s stderr=%s", resumed.code, resumed.stdout, resumed.stderr)
	}
	if status := env.status(t, env.repoA); !strings.Contains(status, `"task_status":"waiting-sol-review"`) {
		t.Fatalf("統合後resumeの元task終了状態 = %s", status)
	}
}
