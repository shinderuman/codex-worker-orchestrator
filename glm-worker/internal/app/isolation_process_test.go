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

// TestIsolateCommandProcessSeriesは--isolateの実binary契約を1連串で固定する:
// 未commit作業を持つ--stop停止、保持基準のcheckpoint固定、--isolateによるworktree作成と
// 元state・元checkoutの不変、隔離済みstateへの再--isolateの冪等再観測、隔離worktreeでの
// 割り込みtask完結、隔離branch統合後の--resume通過、統合後掃除でstale化した記録への
// 再--isolateのfail closed、保持対象を壊したresumeのfail closedと復元後の再開。
func TestIsolateCommandProcessSeries(t *testing.T) {
	env := newMultiRepoEnv(t)
	env.setStubMode(t, env.stubA, "dirty-hold")

	// 1. 未commit作業を残したままの--stop停止。
	holder := startIsolationHolder(t, env)
	stateA := env.waitStateDir(t, env.repoA, holder)
	env.waitHeldWithWorkerSession(t, stateA)
	// stubの未commit作業書込みが観測されてから停止する。停止はfile書込みと競合しない。
	waitStopFile(t, filepath.Join(env.repoA, "uncommitted.txt"))
	stopResult := env.run(t, env.repoA, "--stop")
	if stopResult.code != 0 || !strings.Contains(stopResult.stdout, `"result":"interrupted"`) {
		t.Fatalf("--stopがinterruptedになりません: code=%d stdout=%s stderr=%s", stopResult.code, stopResult.stdout, stopResult.stderr)
	}
	holder.waitFailure(t)
	taskID := readStateFile(t, stateA, "task.id")

	checkpoint := parseStateJSON(t, stateA, "resume-state.json")
	dirty, ok := checkpoint["stop_dirty_files"].([]any)
	if !ok || len(dirty) == 0 {
		t.Fatalf("停止checkpointが停止時dirty保持基準を固定していません: %#v", checkpoint["stop_dirty_files"])
	}
	foundUncommitted := false
	for _, entry := range dirty {
		file, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if file["path"] == "uncommitted.txt" {
			foundUncommitted = true
		}
	}
	if !foundUncommitted {
		t.Fatalf("停止時dirty保持基準にstubの未commit作業がありません: %#v", dirty)
	}
	snapshotField, ok := checkpoint["stop_git_snapshot"].(map[string]any)
	if !ok || snapshotField["head"] == nil || snapshotField["head"] == "" {
		t.Fatalf("停止checkpointが停止時snapshotを固定していません: %#v", checkpoint["stop_git_snapshot"])
	}
	if !fileExists(filepath.Join(stateA, "stop-worktree.patch")) {
		t.Fatal("停止時recovery patchが保存されていません")
	}

	// 2. --isolateは元state・元checkoutへ隔離記録だけを加える。
	before := snapshotIsolationState(t, stateA)
	isolate := env.run(t, env.repoA, "--isolate")
	if isolate.code != 0 || !strings.Contains(isolate.stdout, `"result":"isolated"`) {
		t.Fatalf("--isolateが失敗しました: code=%d stdout=%s stderr=%s", isolate.code, isolate.stdout, isolate.stderr)
	}
	assertIsolationStateDeltaIsRecordOnly(t, stateA, before)
	worktree := isolationOutputField(t, isolate.stdout, "worktree")
	branch := isolationOutputField(t, isolate.stdout, "branch")
	if worktree == "" || !strings.HasPrefix(branch, "glm-worker/isolation/") {
		t.Fatalf("隔離出力 = %s", isolate.stdout)
	}
	if !strings.Contains(isolate.stdout, `"task_id":"`+taskID+`"`) {
		t.Fatalf("隔離出力が元taskを指しません: %s (task %s)", isolate.stdout, taskID)
	}
	if !fileExists(filepath.Join(worktree, "corpus.md")) {
		t.Fatalf("隔離worktreeがHEAD内容をcheck outしていません: %s", worktree)
	}
	statusOut, err := exec.Command("git", "-C", env.repoA, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(statusOut), "?? uncommitted.txt") {
		t.Fatalf("--isolateが元checkoutの未commit作業を変化させています: %q", statusOut)
	}

	// 2b. 隔離済みstateへの再--isolateは同じmachine結果を冪等に返し、単一pointer
	// (isolation.json)・worktreeを上書きしない。
	recordBefore := readStateFile(t, stateA, "isolation.json")
	replay := env.run(t, env.repoA, "--isolate")
	if replay.code != 0 || !strings.Contains(replay.stdout, `"result":"isolated"`) {
		t.Fatalf("再--isolateが冪等に成功しません: code=%d stdout=%s stderr=%s", replay.code, replay.stdout, replay.stderr)
	}
	if replay.stdout != isolate.stdout {
		t.Fatalf("再--isolateのmachine結果が初回と異なります: first=%s replay=%s", isolate.stdout, replay.stdout)
	}
	if got := readStateFile(t, stateA, "isolation.json"); got != recordBefore {
		t.Fatal("再--isolateが隔離記録を上書きしています")
	}
	assertIsolationStateDeltaIsRecordOnly(t, stateA, before)

	// 3. 隔離worktreeで割り込みtaskが独立state・lockで完結する間、元stateは不変。
	env.setStubMode(t, env.stubA, "success")
	interrupted := env.run(t, worktree, "割り込みtask marker ISOW1")
	if interrupted.code != 0 || !strings.Contains(interrupted.stdout, `"status":"PASS"`) {
		t.Fatalf("隔離worktreeでの割り込みtaskが完結しません: code=%d stdout=%s stderr=%s", interrupted.code, interrupted.stdout, interrupted.stderr)
	}
	assertIsolationStateDeltaIsRecordOnly(t, stateA, before)
	worktreeState := env.waitStateDir(t, worktree, nil)
	if worktreeState == stateA {
		t.Fatal("隔離worktreeのstate dirが元repo stateと分離されていません")
	}
	if readStateFile(t, worktreeState, "task.id") == taskID {
		t.Fatal("隔離worktree taskが元task IDを再利用しています")
	}
	originStatus := env.status(t, worktree)
	if !strings.Contains(originStatus, `"isolation_origin"`) || !strings.Contains(originStatus, `"origin_repo_root":"`+env.repoA+`"`) {
		t.Fatalf("隔離先--statusが出自を機械照合可能な形で出していません: %s", originStatus)
	}

	// 4. 親Codexによる隔離branchの統合。dirty保持対象とは触れない。
	if output, err := exec.Command("git", "-C", worktree, "commit", "--quiet", "--allow-empty", "-m", "isolation task result").CombinedOutput(); err != nil {
		t.Fatalf("隔離worktreeのcommitに失敗しました: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", env.repoA, "merge", "--quiet", "--no-edit", branch).CombinedOutput(); err != nil {
		t.Fatalf("隔離branchの統合に失敗しました: %v: %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(env.repoA, "uncommitted.txt")); err != nil {
		t.Fatalf("統合が元taskの未commit作業を消しています: %v", err)
	}
	originRepoStatus := env.status(t, env.repoA)
	if !strings.Contains(originRepoStatus, `"isolation"`) || !strings.Contains(originRepoStatus, `"worktree":"`+worktree+`"`) {
		t.Fatalf("元repo --statusが隔離記録を出していません: %s", originRepoStatus)
	}

	// 4b. 統合後の親Codexによるworktree掃除でstale化した記録への再--isolateは上書きせず
	// fail closedする。隔離側state dir(出自記録)はstate home配下に残るため、以降のresume
	// 保持照合には影響しない。
	staleRecordBefore := readStateFile(t, stateA, "isolation.json")
	if output, err := exec.Command("git", "-C", env.repoA, "worktree", "remove", "--force", worktree).CombinedOutput(); err != nil {
		t.Fatalf("統合後のworktree掃除に失敗しました: %v: %s", err, output)
	}
	staleReplay := env.run(t, env.repoA, "--isolate")
	if staleReplay.code == 0 || !strings.Contains(staleReplay.stderr, `"kind":"worker_error"`) {
		t.Fatalf("stale記録への再--isolateがfail closedになりません: code=%d stdout=%s stderr=%s", staleReplay.code, staleReplay.stdout, staleReplay.stderr)
	}
	if got := readStateFile(t, stateA, "isolation.json"); got != staleRecordBefore {
		t.Fatal("stale再--isolateが隔離記録を上書きしています")
	}

	// 5. 保持対象を壊したresumeはfail closed。state・checkpointはinterruptedのまま残る。
	uncommittedPath := filepath.Join(env.repoA, "uncommitted.txt")
	original, err := os.ReadFile(uncommittedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uncommittedPath, []byte("外部から解決済み\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	violated := env.run(t, env.repoA, "--resume")
	if violated.code == 0 || !strings.Contains(violated.stderr, `"kind":"worker_error"`) {
		t.Fatalf("保持違反のresumeがfail closedになりません: code=%d stdout=%s stderr=%s", violated.code, violated.stdout, violated.stderr)
	}
	if status := env.status(t, env.repoA); !strings.Contains(status, `"task_status":"interrupted"`) {
		t.Fatalf("fail closed後のtask status = %s", status)
	}
	if got, err := os.ReadFile(uncommittedPath); err != nil || string(got) != "外部から解決済み\n" {
		t.Fatalf("fail closedがworking treeへ干渉しています: %q %v", got, err)
	}

	// 6. 停止時内容へ復元すると同じ--resumeが通過する。
	if err := os.WriteFile(uncommittedPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	resumed := env.run(t, env.repoA, "--resume")
	if resumed.code != 0 || !strings.Contains(resumed.stdout, `"status":"PASS"`) {
		t.Fatalf("統合後の元task resumeが完結しません: code=%d stdout=%s stderr=%s", resumed.code, resumed.stdout, resumed.stderr)
	}
}

// startIsolationHolderはdirty-hold stubで元taskの呼出を起動する。
func startIsolationHolder(t *testing.T, env *multiRepoEnv) *multiRepoHolder {
	t.Helper()
	holdCtx, cancel := context.WithTimeout(context.Background(), multiRepoRunTimeout)
	t.Cleanup(cancel)
	return env.start(t, holdCtx, env.repoA, "isolation series worker marker ISOO1")
}

// snapshotIsolationStateはstate dirの比較snapshotを返す。lock fileはflock解放後も
// 内容が残るため除外する。
func snapshotIsolationState(t *testing.T, stateDir string) map[string]string {
	t.Helper()
	snapshot := snapshotStateDir(t, stateDir)
	delete(snapshot, "lock")
	return snapshot
}

// assertIsolationStateDeltaIsRecordOnlyはsnapshot以降のstate dir変化が隔離記録
// (isolation.json)の追加だけであることを検査する。
func assertIsolationStateDeltaIsRecordOnly(t *testing.T, stateDir string, before map[string]string) {
	t.Helper()
	after := snapshotIsolationState(t, stateDir)
	for name, content := range before {
		got, ok := after[name]
		if !ok {
			t.Fatalf("state file %sが隔離操作で消えています", name)
		}
		if got != content {
			t.Fatalf("state file %sが隔離操作で書き換わっています", name)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok && name != "isolation.json" {
			t.Fatalf("隔離操作がstate file %sを追加しています", name)
		}
	}
}

// isolationOutputFieldは--isolate出力のmachine JSONからstring fieldを取り出す。
func isolationOutputField(t *testing.T, output string, key string) string {
	t.Helper()
	value, ok := statusJSONField(t, output, key).(string)
	if !ok || value == "" {
		t.Fatalf("隔離出力の%sが読めません: %s", key, output)
	}
	return value
}
