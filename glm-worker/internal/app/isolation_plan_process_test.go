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

// TestIsolatePlanLifecycleProcessSeriesはPlan・IMPLEMENTATION_TASKSを持つ実repoでの
// 割り込みtask隔離lifecycleを固定する。Plan ACTIVEは元task fileを指したままcheck outされる
// ため、USER_REQUESTだけでは割り込みtaskの要求正本を特定できない。親Codexが隔離worktree側で
// (a)割り込みtask fileを明示pathでcommitし、(b)Plan ACTIVEを割り込みtask fileへ切替えて
// (こちらは未commitのまま)割り込みtaskを開始し、(c)成果物だけを明示pathでcommitして通常mergeで
// 戻す。Plan ACTIVE切替は実行中も未commitの親管理metadata編集として残るため実効riskはHIGHに
// 固定され、reviewer PASSはrisk floorで拒否されtaskはwaiting-sol-reviewへ昇格する(親Codexが
// packetを確認して統合する)。統合後の元task resumeも同様にimplementation-tasks critical path
// を含むdiffでHIGH reviewとなりwaiting-sol-reviewへ昇格するが、保持照合(隔離branch統合の実質
// 検証)を通過することが前提になる。元repoのPlan・元task fileはbyte不変に保たれる。
func TestIsolatePlanLifecycleProcessSeries(t *testing.T) {
	env := newMultiRepoEnv(t)

	// 0. 親管理metadataを持つrepo A。Plan ACTIVEは元taskを指し、両者ともtracked。
	const originalTaskPath = "IMPLEMENTATION_TASKS/original-task.md"
	const interruptionTaskPath = "IMPLEMENTATION_TASKS/interruption-task.md"
	originalTaskBody := []byte("# 元task\n\n割り込み前に実行中だったtask本文。\n")
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

	// 1. 未commit作業を残したままの--stop停止。元taskの要求正本は開始時に元task fileへ固定済み。
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

	// 2. --isolateで割り込み実行checkoutを作る。worktree側Plan ACTIVEも元taskを指す。
	isolate := env.run(t, env.repoA, "--isolate")
	if isolate.code != 0 || !strings.Contains(isolate.stdout, `"result":"isolated"`) {
		t.Fatalf("--isolateが失敗しました: code=%d stdout=%s stderr=%s", isolate.code, isolate.stdout, isolate.stderr)
	}
	worktree := isolationOutputField(t, isolate.stdout, "worktree")
	branch := isolationOutputField(t, isolate.stdout, "branch")

	// 3. 親Codexの割り込みtask開始手順: (a)割り込みtask fileをworktree側TASKS配下へ置き、
	// (b)worktree側Plan ACTIVEを割り込みtaskへ切替え、(c)task fileだけを明示pathでcommitして
	// からtaskを実行する。Plan切替はworktree側の作業指示のためcommitへ含めない(含めると統合で
	// 元repoのPlan ACTIVE参照が壊れる)。task fileをcommitせずに実行すると実効riskが
	// implementation-tasks critical pathでHIGHへ固定され、reviewer PASSがrisk floorで拒否される。
	interruptionTaskBody := []byte("# 割り込みtask\n\n隔離worktreeで実行するtask本文。\n")
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

	// 4. 親Codexによる統合commit。成果物だけを明示pathでcommitし、Plan ACTIVE切替は未commitの
	// まま残す。
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

	// 5. 通常mergeで元repoへ戻す。branchはPlan・元task fileを変更していないため、mergeは
	// 元repoのPlan ACTIVE参照と元task fileを壊さない。
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

	// 6. 元taskのresumeが保持照合(隔離branch統合の実質検証・出自記録対称)を通過する。統合後の
	// diffには割り込みtask file(implementation-tasks critical path)が含まれるため実risk HIGHで
	// reviewされ、reviewer PASSはrisk floorで拒否されwaiting-sol-reviewへ昇格する。保持照合を
	// 通過していなければworker_errorで停止するため、ここまで到達することが保持承認の証明になる。
	resumed := env.run(t, env.repoA, "--resume")
	if resumed.code != 0 || !strings.Contains(resumed.stdout, `"status":"NEEDS_SOL_REVIEW"`) {
		t.Fatalf("統合後の元task resumeが保持照合を通過しません: code=%d stdout=%s stderr=%s", resumed.code, resumed.stdout, resumed.stderr)
	}
	if status := env.status(t, env.repoA); !strings.Contains(status, `"task_status":"waiting-sol-review"`) {
		t.Fatalf("統合後resumeの元task終了状態 = %s", status)
	}
}
