package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

// newIsolationGitRepoはHEAD commitを持つgit repositoryを用意する。
func newIsolationGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため隔離testをskipします: %v", err)
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = repo
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v失敗: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.email", "isolate@example.invalid")
	run("config", "user.name", "isolate test")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("commit", "-q", "-m", "initial")
	return repo
}

func newIsolationConfig(t *testing.T, repo string) config.AppConfig {
	t.Helper()
	return config.AppConfig{
		StateBase:    t.TempDir(),
		RepoHash:     config.RepoHashFor(repo),
		RepoShort:    config.RepoHashFor(repo)[:12],
		RepoRoot:     repo,
		WorktreeBase: filepath.Join(t.TempDir(), "worktrees"),
	}
}

// seedInterruptedIsolationStateは--stop直後の元repo stateを用意する。
func seedInterruptedIsolationState(t *testing.T, st *state.StateStore, checkpoint state.ResumeCheckpoint) {
	t.Helper()
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusInterrupted); err != nil {
		t.Fatal(err)
	}
}

func interruptedCheckpoint() state.ResumeCheckpoint {
	return state.ResumeCheckpoint{
		Stage:           state.ResumeStageWorker,
		Phase:           "worker-new",
		Role:            state.WorkerRole,
		Model:           "opus",
		Effort:          "high",
		Prompt:          "p",
		Request:         "req",
		UserInterrupted: true,
	}
}

// TestIsolateRejectsNonUserInterruptedStateは--isolateの前提検証をfail closedに
// することを固定する。task不在・status違い・停止理由違いはworktreeを作らない。
func TestIsolateRejectsNonUserInterruptedState(t *testing.T) {
	repo := newIsolationGitRepo(t)
	tests := []struct {
		name       string
		hasTask    bool
		status     state.TaskStatus
		checkpoint func() state.ResumeCheckpoint
	}{
		{name: "task不在", hasTask: false, status: state.TaskStatusInterrupted, checkpoint: interruptedCheckpoint},
		{name: "status active", hasTask: true, status: state.TaskStatusActive, checkpoint: interruptedCheckpoint},
		{
			name:    "rate limited停止",
			hasTask: true,
			status:  state.TaskStatusInterrupted,
			checkpoint: func() state.ResumeCheckpoint {
				checkpoint := interruptedCheckpoint()
				checkpoint.RateLimited = true
				return checkpoint
			},
		},
		{
			name:    "provider unavailable停止",
			hasTask: true,
			status:  state.TaskStatusInterrupted,
			checkpoint: func() state.ResumeCheckpoint {
				checkpoint := interruptedCheckpoint()
				checkpoint.ProviderUnavailable = true
				return checkpoint
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newIsolationConfig(t, repo)
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if test.hasTask {
				seedInterruptedIsolationState(t, st, interruptedCheckpoint())
			} else if err := st.SetTaskStatus(state.TaskStatusInterrupted); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(test.status); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveResumeCheckpoint(test.checkpoint()); err != nil {
				t.Fatal(err)
			}

			err = isolateInterruptedTask(st, cfg, io.Discard)
			var workerErr *workflow.WorkerError
			if !errors.As(err, &workerErr) {
				t.Fatalf("前提違反がWorkerErrorになりません: %v", err)
			}
			if _, recErr := st.LoadIsolationRecord(); !errors.Is(recErr, state.ErrNoIsolationRecord) {
				t.Fatalf("前提違反で隔離記録が書かれています: %v", recErr)
			}
			output, listErr := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
			if listErr != nil {
				t.Fatal(listErr)
			}
			for _, line := range strings.Split(string(output), "\n") {
				if strings.Contains(line, cfg.WorktreeBase) {
					t.Fatalf("前提違反でworktreeが作成されています: %s", line)
				}
			}
		})
	}
}

// TestIsolateCreatesWorktreeAndSymmetricRecordsは--isolate成功経路を固定する:
// worktree+branch作成、元repo側隔離記録、worktree側出自記録、元task stateの不変、
// 隔離先stateのrepo-hash分離。
func TestIsolateCreatesWorktreeAndSymmetricRecords(t *testing.T) {
	repo := newIsolationGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("作業中\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := newIsolationConfig(t, repo)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seedInterruptedIsolationState(t, st, interruptedCheckpoint())
	taskID := st.ReadOr("task.id", "")

	var out strings.Builder
	if err := isolateInterruptedTask(st, cfg, &out); err != nil {
		t.Fatal(err)
	}
	var result isolateOutput
	if err := json.Unmarshal([]byte(out.String()), &result); err != nil {
		t.Fatalf("隔離結果JSONを解析できません: %v: %s", err, out.String())
	}
	if result.Result != "isolated" || result.TaskID != taskID || result.RepoRoot != repo {
		t.Fatalf("隔離結果 = %#v", result)
	}
	if info, err := os.Stat(filepath.Join(result.Worktree, "tracked.txt")); err != nil || info.IsDir() {
		t.Fatalf("隔離worktreeがHEAD内容をcheck outしていません: %v", err)
	}
	branchRev, err := exec.Command("git", "-C", repo, "rev-parse", result.Branch).Output()
	if err != nil {
		t.Fatalf("隔離branchが作成されていません: %v", err)
	}
	headRev, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(branchRev)) != strings.TrimSpace(string(headRev)) {
		t.Fatal("隔離branchが停止時HEADから始まっていません")
	}

	record, err := st.LoadIsolationRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.IsolationID != result.IsolationID || record.Worktree != result.Worktree ||
		record.Branch != result.Branch || record.OriginTaskID != taskID || record.OriginRepoRoot != repo ||
		record.OriginHead != strings.TrimSpace(string(headRev)) {
		t.Fatalf("元repo側隔離記録 = %#v", record)
	}

	// 元task stateは隔離操作で書き換えない。
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("task status = %s want interrupted", st.TaskStatus())
	}
	if st.ReadOr("worker.id", "") != "" {
		t.Fatal("隔離操作が元taskのsessionを書き換えています")
	}
	if checkpoint, cerr := st.LoadResumeCheckpoint(); cerr != nil || !checkpoint.UserInterrupted {
		t.Fatalf("隔離操作が元taskのcheckpointを書き換えています: %#v err=%v", checkpoint, cerr)
	}
	if !st.Exists("task.id") {
		t.Fatal("隔離操作が元task識別を消しています")
	}

	// 隔離先stateはrepo-hash分離の上、出自記録を持つ。
	worktreeStore := state.AttachStateStore(config.AppConfig{
		StateBase: cfg.StateBase,
		RepoHash:  config.RepoHashFor(result.Worktree),
	})
	origin, err := worktreeStore.LoadIsolationOrigin()
	if err != nil {
		t.Fatalf("隔離先stateへ出自記録が保存されていません: %v", err)
	}
	if origin.IsolationID != result.IsolationID || origin.OriginRepoRoot != repo ||
		origin.OriginTaskID != taskID || origin.Branch != result.Branch {
		t.Fatalf("隔離先出自記録 = %#v", origin)
	}
	if _, recErr := worktreeStore.LoadIsolationRecord(); !errors.Is(recErr, state.ErrNoIsolationRecord) {
		t.Fatalf("隔離先へ元repo側記録が混入しています: %v", recErr)
	}

	// 元checkoutのworking tree状態は隔離操作の前後で不変。
	statusOut, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(statusOut), "?? uncommitted.txt") {
		t.Fatalf("元checkoutのuntracked作業が隔離操作で変化しています: %q", statusOut)
	}
}

// TestIsolateReplayIsIdempotentは隔離済みstateへの再--isolateが同じmachine結果を
// 冪等に返し、先行隔離先(worktree・branch・記録)を孤児化する上書きを作らないことを固定する。
func TestIsolateReplayIsIdempotent(t *testing.T) {
	repo := newIsolationGitRepo(t)
	cfg := newIsolationConfig(t, repo)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seedInterruptedIsolationState(t, st, interruptedCheckpoint())

	var first strings.Builder
	if err := isolateInterruptedTask(st, cfg, &first); err != nil {
		t.Fatal(err)
	}
	recordBefore, err := os.ReadFile(st.Path("isolation.json"))
	if err != nil {
		t.Fatal(err)
	}
	worktreesBefore, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}

	var replay strings.Builder
	if err := isolateInterruptedTask(st, cfg, &replay); err != nil {
		t.Fatalf("再--isolateが失敗しました: %v", err)
	}
	if replay.String() != first.String() {
		t.Fatalf("再--isolateの結果が初回と異なります: first=%s replay=%s", first.String(), replay.String())
	}
	recordAfter, err := os.ReadFile(st.Path("isolation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(recordBefore) != string(recordAfter) {
		t.Fatal("再--isolateが隔離記録を書き換えています")
	}
	worktreesAfter, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(worktreesBefore) != string(worktreesAfter) {
		t.Fatalf("再--isolateがworktreeを追加しています:\nbefore:\n%s\nafter:\n%s", worktreesBefore, worktreesAfter)
	}
}

// TestIsolateReplayFailsClosedOnStaleRecordは生きた隔離として成立しない既存記録への
// 再--isolateを、記録を上書きせずfail closedにすることを固定する。
func TestIsolateReplayFailsClosedOnStaleRecord(t *testing.T) {
	repo := newIsolationGitRepo(t)
	prepare := func(t *testing.T) (config.AppConfig, *state.StateStore, isolateOutput) {
		cfg := newIsolationConfig(t, repo)
		st, err := state.NewStateStore(cfg)
		if err != nil {
			t.Fatal(err)
		}
		seedInterruptedIsolationState(t, st, interruptedCheckpoint())
		var out strings.Builder
		if err := isolateInterruptedTask(st, cfg, &out); err != nil {
			t.Fatal(err)
		}
		var result isolateOutput
		if err := json.Unmarshal([]byte(out.String()), &result); err != nil {
			t.Fatalf("隔離結果JSONを解析できません: %v: %s", err, out.String())
		}
		return cfg, st, result
	}
	tests := []struct {
		name   string
		damage func(t *testing.T, st *state.StateStore, result isolateOutput)
		wantIn string
	}{
		{
			name: "worktree消失",
			damage: func(t *testing.T, st *state.StateStore, result isolateOutput) {
				if output, err := exec.Command("git", "-C", repo, "worktree", "remove", "--force", result.Worktree).CombinedOutput(); err != nil {
					t.Fatalf("worktree削除失敗: %v: %s", err, output)
				}
			},
			wantIn: "隔離先worktreeが存在しないため",
		},
		{
			// branchだけが削除済みのstale記録。gitはworktreeが掴むbranchの削除を拒否するため、
			// worktree解除→branch削除→dirのみ復元でこの状態を作る。
			name: "branch削除",
			damage: func(t *testing.T, st *state.StateStore, result isolateOutput) {
				if output, err := exec.Command("git", "-C", repo, "worktree", "remove", "--force", result.Worktree).CombinedOutput(); err != nil {
					t.Fatalf("worktree削除失敗: %v: %s", err, output)
				}
				if output, err := exec.Command("git", "-C", repo, "branch", "-D", result.Branch).CombinedOutput(); err != nil {
					t.Fatalf("branch削除失敗: %v: %s", err, output)
				}
				if err := os.MkdirAll(result.Worktree, 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantIn: "branchが解決できないため",
		},
		{
			name: "出自記録の単独改変",
			damage: func(t *testing.T, st *state.StateStore, result isolateOutput) {
				if err := st.AttachSiblingStore(config.RepoHashFor(result.Worktree)).SaveIsolationOrigin(state.IsolationOrigin{
					IsolationID:    "tampered",
					OriginRepoRoot: repo,
					OriginTaskID:   st.ReadOr("task.id", ""),
					Branch:         result.Branch,
				}); err != nil {
					t.Fatal(err)
				}
			},
			wantIn: "出自記録が一致しないため",
		},
		{
			name: "破損記録",
			damage: func(t *testing.T, st *state.StateStore, result isolateOutput) {
				if err := os.WriteFile(st.Path("isolation.json"), []byte("{broken"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantIn: "隔離記録を読み込めません",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, st, result := prepare(t)
			recordBefore, err := os.ReadFile(st.Path("isolation.json"))
			if err != nil {
				t.Fatal(err)
			}
			test.damage(t, st, result)

			err = isolateInterruptedTask(st, cfg, io.Discard)
			var workerErr *workflow.WorkerError
			if !errors.As(err, &workerErr) {
				t.Fatalf("stale記録への再--isolateがWorkerErrorになりません: %v", err)
			}
			if !strings.Contains(workerErr.Message, test.wantIn) {
				t.Fatalf("fail closed理由 %q が %q を含みません", workerErr.Message, test.wantIn)
			}
			if test.name == "破損記録" {
				return
			}
			recordAfter, err := os.ReadFile(st.Path("isolation.json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(recordBefore) != string(recordAfter) {
				t.Fatal("stale記録への再--isolateが記録を上書きしています")
			}
			if st.TaskStatus() != state.TaskStatusInterrupted {
				t.Fatalf("task status = %s want interrupted", st.TaskStatus())
			}
		})
	}
}

// TestStatusExposesIsolationRecordsは--statusが元repo側・隔離先側の記録を出す側だけへ
// 出すことを固定する。
func TestStatusExposesIsolationRecords(t *testing.T) {
	repo := newIsolationGitRepo(t)
	cfg := newIsolationConfig(t, repo)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var plain strings.Builder
	if err := printStatus(st, &plain); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "isolation") {
		t.Fatalf("記録がない状態でisolation fieldを出しています: %s", plain.String())
	}

	seedInterruptedIsolationState(t, st, interruptedCheckpoint())
	var isolated strings.Builder
	if err := isolateInterruptedTask(st, cfg, &isolated); err != nil {
		t.Fatal(err)
	}
	var result isolateOutput
	if err := json.Unmarshal([]byte(isolated.String()), &result); err != nil {
		t.Fatal(err)
	}

	var originStatus strings.Builder
	if err := printStatus(st, &originStatus); err != nil {
		t.Fatal(err)
	}
	var originBody struct {
		Isolation *statusIsolation `json:"isolation"`
	}
	if err := json.Unmarshal([]byte(originStatus.String()), &originBody); err != nil {
		t.Fatal(err)
	}
	if originBody.Isolation == nil || originBody.Isolation.IsolationID != result.IsolationID ||
		originBody.Isolation.Worktree != result.Worktree {
		t.Fatalf("元repo側--statusのisolation = %#v", originBody.Isolation)
	}

	worktreeStore := state.AttachStateStore(config.AppConfig{
		StateBase: cfg.StateBase,
		RepoHash:  config.RepoHashFor(result.Worktree),
	})
	var worktreeStatus strings.Builder
	if err := printStatus(worktreeStore, &worktreeStatus); err != nil {
		t.Fatal(err)
	}
	var worktreeBody struct {
		IsolationOrigin *statusIsolationOrigin `json:"isolation_origin"`
	}
	if err := json.Unmarshal([]byte(worktreeStatus.String()), &worktreeBody); err != nil {
		t.Fatal(err)
	}
	if worktreeBody.IsolationOrigin == nil || worktreeBody.IsolationOrigin.OriginRepoRoot != repo ||
		worktreeBody.IsolationOrigin.IsolationID != result.IsolationID {
		t.Fatalf("隔離先--statusのisolation_origin = %#v", worktreeBody.IsolationOrigin)
	}
}
