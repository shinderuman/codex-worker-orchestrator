//go:build unix

package app

import (
	"bytes"
	"os"
	"sort"
	"syscall"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type orphanWriteProbe struct {
	buf             bytes.Buffer
	lockPath        string
	orphanObserved  bool
	competitorSoFar bool
}

func (w *orphanWriteProbe) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	if bytes.Contains(p, []byte(watchStatusOrphanTerminal)) {
		w.orphanObserved = true
		w.competitorSoFar = !competitorCanFlock(w.lockPath)
	}
	return n, err
}

func competitorCanFlock(path string) bool {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

func stateEntryNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	return names
}

func changedEntryNames(before map[string]bool, after map[string]bool) ([]string, []string) {
	added := []string{}
	removed := []string{}
	for name := range after {
		if !before[name] {
			added = append(added, name)
		}
	}
	for name := range before {
		if !after[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func TestExecuteWatchOrphanTerminalExitsBounded(t *testing.T) {
	for _, fixture := range []string{"lock-absent", "stale-pid-lock"} {
		t.Run(fixture, func(t *testing.T) {
			base := t.TempDir()
			cfg := config.AppConfig{StateBase: base, RepoHash: "watchhash", RepoRoot: "/repo"}
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Write("task.id", watchOrphanTaskID); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
				t.Fatal(err)
			}
			writeTaskEventLines(t, st, watchOrphanTaskID,
				state.TaskEventRecord{TaskID: watchOrphanTaskID, CallID: "call-error", Role: "worker", Phase: "worker-explicit-fix", Kind: "result", Subtype: "error_max_turns"},
			)
			seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)
			if fixture == "stale-pid-lock" {
				if err := os.WriteFile(st.LockPath(), []byte("999999999\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			telemetryBefore, err := os.ReadFile(st.ModelCallLogPath(watchOrphanTaskID))
			if err != nil {
				t.Fatal(err)
			}
			entriesBefore := stateEntryNames(t, st.Path("."))

			cmd, err := ParseCommand([]string{"--watch"})
			if err != nil {
				t.Fatal(err)
			}
			out := &bytes.Buffer{}
			done := make(chan error, 1)
			go func() {
				done <- Execute(cmd, cfg, nil, out, &bytes.Buffer{})
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("orphan terminal stateで--watchがbounded時間内に終端しません")
			}

			events := parseWatchEvents(t, out.String())
			exit := requireOrphanExitEvent(t, events)
			if watchString(t, exit, "artifact_dir") != st.ArtifactDir(watchOrphanTaskID) {
				t.Fatalf("watch_exit artifact_dir = %#v", exit)
			}
			if watchEventIndex(events, "watch_exit") != len(events)-1 {
				t.Fatalf("終端eventより後に出力があります: %v", events)
			}
			if watchEventIndex(events, "watch_exit") < watchEventIndex(events, "watch_start")+1 {
				t.Fatalf("保存済みeventより先に終端eventが出ています: %v", events)
			}

			telemetryAfter, err := os.ReadFile(st.ModelCallLogPath(watchOrphanTaskID))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(telemetryBefore, telemetryAfter) {
				t.Fatal("watchがtelemetryへmodel callを追記しました")
			}
			entriesAfter := stateEntryNames(t, st.Path("."))
			added, removed := changedEntryNames(entriesBefore, entriesAfter)
			if len(removed) != 0 {
				t.Fatalf("watchがstate dirのfileを削除しました: %v", removed)
			}
			lockBytes, err := os.ReadFile(st.LockPath())
			if err != nil {
				t.Fatal(err)
			}
			if fixture == "lock-absent" {
				if len(added) != 1 || added[0] != "lock" {
					t.Fatalf("lock以外のstate fileが増えました: %v", added)
				}
				if len(lockBytes) != 0 {
					t.Fatalf("watchが作成したlock fileへ内容を書きました: %q", lockBytes)
				}
			} else {
				if len(added) != 0 {
					t.Fatalf("watchがstate dirへfileを追加しました: %v", added)
				}
				if !bytes.Equal(lockBytes, []byte("999999999\n")) {
					t.Fatalf("watchがlock fileの内容を変更しました: %q", lockBytes)
				}
			}
		})
	}
}

func TestWatchOrphanLeaseBlocksCompetitorUntilEventWritten(t *testing.T) {
	st, _ := watchTestStore(t)
	writeTaskEventLines(t, st, watchOrphanTaskID,
		state.TaskEventRecord{TaskID: watchOrphanTaskID, CallID: "call-error", Role: "worker", Phase: "worker-explicit-fix", Kind: "result", Subtype: "error_max_turns"},
	)
	seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)

	probe := &orphanWriteProbe{lockPath: st.LockPath()}
	terminal, err := watchTerminal(st, watchOrphanTaskID, probe, defaultWatchOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	if !terminal {
		t.Fatal("orphan stateがterminal判定されません")
	}
	if !probe.orphanObserved {
		t.Fatal("orphan terminal eventが出力されません")
	}
	if !probe.competitorSoFar {
		t.Fatal("watch_exit書込み完了前に競合がrepo lockを取得できました")
	}
	if !competitorCanFlock(st.LockPath()) {
		t.Fatal("watch_exit書込み完了後に競合がrepo lockを取得できません")
	}
	requireOrphanExitEvent(t, parseWatchEvents(t, probe.buf.String()))
}

func TestWatchOrphanDefersToCompetitorWinningLeaseRace(t *testing.T) {
	st, _ := watchTestStore(t)
	writeTaskEventLines(t, st, watchOrphanTaskID,
		state.TaskEventRecord{TaskID: watchOrphanTaskID, CallID: "call-error", Role: "worker", Phase: "worker-explicit-fix", Kind: "result", Subtype: "error_max_turns"},
	)
	seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)
	if err := os.WriteFile(st.LockPath(), []byte("999999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	leaseAttempts := 0
	opts := watchTestOptions(false, time.Millisecond, nil)
	opts.acquireLease = func(path string) (*repoLockLease, error) {
		leaseAttempts++
		if leaseAttempts > 1 {
			return AcquireRepoLockLease(path)
		}
		competitor, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(competitor.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = competitor.Close()
			return nil, err
		}
		lease, leaseErr := AcquireRepoLockLease(path)
		_ = syscall.Flock(int(competitor.Fd()), syscall.LOCK_UN)
		_ = competitor.Close()
		return lease, leaseErr
	}

	out := &bytes.Buffer{}
	terminal, err := watchTerminal(st, watchOrphanTaskID, out, opts)
	if err != nil {
		t.Fatal(err)
	}
	if terminal {
		t.Fatal("判定途中のlock取得競合にもかかわらずorphan終端しました")
	}
	if leaseAttempts != 1 {
		t.Fatalf("lease試行回数 = %d", leaseAttempts)
	}
	if watchEventIndex(parseWatchEvents(t, out.String()), "watch_exit") >= 0 {
		t.Fatalf("競合勝利時にwatch_exitが出ています: %s", out.String())
	}

	terminal, err = watchTerminal(st, watchOrphanTaskID, out, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !terminal {
		t.Fatal("競合不在後にorphan terminalへ出ません")
	}
	events := parseWatchEvents(t, out.String())
	requireOrphanExitEvent(t, events)
	if watchEventIndex(events, "watch_exit") != len(events)-1 {
		t.Fatalf("終端eventより後に出力があります: %v", events)
	}
	lockBytes, err := os.ReadFile(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lockBytes, []byte("999999999\n")) {
		t.Fatalf("watchがlock fileの内容を変更しました: %q", lockBytes)
	}
}

func TestWatchOrphanRevalidatesTaskStateAfterLeaseAcquisition(t *testing.T) {
	newTaskID := "87654321-aaaa-bbbb-cccc-dddddddddddd"
	tests := []struct {
		name        string
		mutate      func(*state.StateStore) error
		wantStatus  string
		wantNewTask string
	}{
		{
			name: "terminal-status",
			mutate: func(st *state.StateStore) error {
				return st.SetTaskStatus(state.TaskStatusComplete)
			},
			wantStatus: string(state.TaskStatusComplete),
		},
		{
			name: "task-switched",
			mutate: func(st *state.StateStore) error {
				return st.Write("task.id", newTaskID)
			},
			wantStatus:  "task-switched",
			wantNewTask: newTaskID,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, _ := watchTestStore(t)
			writeTaskEventLines(t, st, watchOrphanTaskID,
				state.TaskEventRecord{TaskID: watchOrphanTaskID, CallID: "call-error", Role: "worker", Phase: "worker-explicit-fix", Kind: "result", Subtype: "error_max_turns"},
			)
			seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)
			opts := watchTestOptions(false, time.Millisecond, nil)
			opts.acquireLease = func(path string) (*repoLockLease, error) {
				competitor, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
				if err != nil {
					return nil, err
				}
				if err := syscall.Flock(int(competitor.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
					_ = competitor.Close()
					return nil, err
				}
				if err := test.mutate(st); err != nil {
					_ = syscall.Flock(int(competitor.Fd()), syscall.LOCK_UN)
					_ = competitor.Close()
					return nil, err
				}
				if err := syscall.Flock(int(competitor.Fd()), syscall.LOCK_UN); err != nil {
					_ = competitor.Close()
					return nil, err
				}
				if err := competitor.Close(); err != nil {
					return nil, err
				}
				return AcquireRepoLockLease(path)
			}

			out := &bytes.Buffer{}
			terminal, err := watchTerminal(st, watchOrphanTaskID, out, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !terminal {
				t.Fatal("lease取得前のtask state変更がterminalへ反映されません")
			}
			events := parseWatchEvents(t, out.String())
			exit := requireWatchEvent(t, events, "watch_exit")
			if got := watchString(t, exit, "status"); got != test.wantStatus {
				t.Fatalf("watch_exit status = %q want %q", got, test.wantStatus)
			}
			if got, _ := exit["status"].(string); got == watchStatusOrphanTerminal {
				t.Fatalf("更新後stateをstale orphan-terminalとして出力しました: %#v", exit)
			}
			if test.wantNewTask != "" && watchString(t, exit, "new_task_id") != test.wantNewTask {
				t.Fatalf("watch_exit new_task_id = %#v", exit)
			}
		})
	}
}

func TestWatchHoldsAttachWhileRepoLockHeldThenOrphansAfterRelease(t *testing.T) {
	st, _ := watchTestStore(t)
	writeTaskEventLines(t, st, watchOrphanTaskID,
		state.TaskEventRecord{TaskID: watchOrphanTaskID, CallID: "call-error", Role: "worker", Phase: "worker-explicit-fix", Kind: "result", Subtype: "error_max_turns"},
	)
	seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)

	lockFile, err := os.OpenFile(st.LockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	out := &watchSyncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- printWatch(st, out, watchTestOptions(false, 5*time.Millisecond, nil))
	}()
	select {
	case err := <-done:
		t.Fatalf("live ownerがいる状態でwatchが終端しました: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if watchEventIndex(parseWatchEvents(t, out.String()), "watch_exit") >= 0 {
		t.Fatalf("repo lock held中にwatch_exitが出ています: %s", out.String())
	}

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := lockFile.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock解放後にorphan terminalへ終端しません")
	}
	events := parseWatchEvents(t, out.String())
	requireOrphanExitEvent(t, events)
	if watchEventIndex(events, "watch_exit") != len(events)-1 {
		t.Fatalf("終端eventより後に出力があります: %v", events)
	}
}
