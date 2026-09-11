package app

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type watchSyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

const watchOrphanTaskID = "12345678-aaaa-bbbb-cccc-dddddddddddd"

func (w *watchSyncBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *watchSyncBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func seedOrphanTerminalTelemetry(t *testing.T, st *state.StateStore, taskID string) {
	t.Helper()
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:     "call-error",
		CallType:   state.CallTypeTask,
		TaskID:     taskID,
		Phase:      "worker-explicit-fix",
		Role:       state.WorkerRole,
		ModelAlias: "opus",
		Outcome:    "error",
		Error:      "exec: \"claude\": executable file not found in $PATH",
	})
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:   "probe-after",
		CallType: state.CallTypeProbe,
		TaskID:   taskID,
		Phase:    "worker-explicit-fix",
		Outcome:  "success",
	})
}

func fakeWatchOrphanLease(_ string) (*repoLockLease, error) {
	return &repoLockLease{}, nil
}

func requireOrphanExitEvent(t *testing.T, events []map[string]any) map[string]any {
	t.Helper()
	exit := requireWatchEvent(t, events, "watch_exit")
	if watchString(t, exit, "status") != watchStatusOrphanTerminal {
		t.Fatalf("watch_exit = %#v", exit)
	}
	if watchString(t, exit, "task_id") != watchOrphanTaskID {
		t.Fatalf("watch_exit task_id = %#v", exit)
	}
	if exit["consistent"] != true {
		t.Fatalf("watch_exit consistent = %#v", exit["consistent"])
	}
	if inconsistency, exists := exit["inconsistency"]; !exists || inconsistency != nil {
		t.Fatalf("watch_exit inconsistency = %#v", exit["inconsistency"])
	}
	if watchString(t, exit, "required_action") != string(state.ParentActionNone) {
		t.Fatalf("watch_exit required_action = %#v", exit)
	}
	if actions, ok := exit["allowed_actions"].([]any); !ok || len(actions) != 0 {
		t.Fatalf("watch_exit allowed_actions = %#v", exit["allowed_actions"])
	}
	if resumeKind, exists := exit["resume_kind"]; !exists || resumeKind != nil {
		t.Fatalf("watch_exit resume_kind = %#v", exit["resume_kind"])
	}
	material, ok := exit["last_material"].(map[string]any)
	if !ok {
		t.Fatalf("watch_exit last_material = %#v", exit["last_material"])
	}
	if material["call_id"] != "call-error" || material["call_type"] != "task" ||
		material["phase"] != "worker-explicit-fix" || material["outcome"] != "error" {
		t.Fatalf("watch_exit last_material = %#v", material)
	}
	return exit
}

func watchOrphanExitForVocabulary(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	opts := defaultWatchOptions(false)
	opts.acquireLease = fakeWatchOrphanLease
	out := &bytes.Buffer{}
	terminal, err := watchTerminal(st, watchOrphanTaskID, out, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !terminal {
		t.Fatal("orphan stateがterminal判定されません")
	}
	return requireWatchEvent(t, parseWatchEvents(t, out.String()), "watch_exit")
}

func TestWatchOrphanExitMatchesHandoffRecoveryVocabulary(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", watchOrphanTaskID); err != nil {
		t.Fatal(err)
	}
	seedParentReviewStateForTest(t, st, watchOrphanTaskID)
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)

	exit := watchOrphanExitForVocabulary(t, st)
	handoff := buildParentHandoff(st)
	if !handoff.Consistent {
		t.Fatalf("handoff = %#v", handoff)
	}
	recovery := projectParentHandoffRecovery(handoff)
	if exit["required_action"] != *recovery.RequiredAction {
		t.Fatalf("required_action = %#v want %#v", exit["required_action"], *recovery.RequiredAction)
	}
	exitActions, ok := exit["allowed_actions"].([]any)
	if !ok || len(exitActions) != len(recovery.AllowedActions) {
		t.Fatalf("allowed_actions = %#v want %#v", exit["allowed_actions"], recovery.AllowedActions)
	}
	handoffResumeKind := any(nil)
	if handoff.ResumeKind != nil {
		handoffResumeKind = *handoff.ResumeKind
	}
	if exit["resume_kind"] != handoffResumeKind {
		t.Fatalf("resume_kind = %#v want %#v", exit["resume_kind"], handoffResumeKind)
	}
	wantMaterial, err := json.Marshal(recovery.LastMaterial)
	if err != nil {
		t.Fatal(err)
	}
	wantMaterialRaw := map[string]any{}
	if err := json.Unmarshal(wantMaterial, &wantMaterialRaw); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exit["last_material"], wantMaterialRaw) {
		t.Fatalf("last_material = %#v want %#v", exit["last_material"], wantMaterialRaw)
	}
	if exit["artifact_dir"] != *handoff.ArtifactDir {
		t.Fatalf("artifact_dir = %#v want %#v", exit["artifact_dir"], *handoff.ArtifactDir)
	}
	if exit["consistent"] != bool(handoff.Consistent) {
		t.Fatalf("consistent = %#v want %#v", exit["consistent"], handoff.Consistent)
	}
}

func TestWatchOrphanPlanFailureIsExplicitlyInconsistent(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", watchOrphanTaskID); err != nil {
		t.Fatal(err)
	}
	seedParentReviewStateForTest(t, st, watchOrphanTaskID)
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("pending-decision", "{}"); err != nil {
		t.Fatal(err)
	}
	seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)

	exit := watchOrphanExitForVocabulary(t, st)
	if exit["consistent"] != false {
		t.Fatalf("plan不整合がconsistentとして出ました: %#v", exit)
	}
	inconsistency, ok := exit["inconsistency"].(string)
	if !ok || inconsistency == "" {
		t.Fatalf("watch_exit inconsistency = %#v", exit["inconsistency"])
	}
	if exit["required_action"] != nil {
		t.Fatalf("plan不整合時のrequired_action = %#v", exit["required_action"])
	}
	if exit["resume_kind"] != nil {
		t.Fatalf("plan不整合時のresume_kind = %#v", exit["resume_kind"])
	}
	if actions, ok := exit["allowed_actions"].([]any); !ok || len(actions) != 0 {
		t.Fatalf("plan不整合時のallowed_actions = %#v", exit["allowed_actions"])
	}

	handoff := buildParentHandoff(st)
	if handoff.Consistent {
		t.Fatalf("handoff = %#v", handoff)
	}
	if handoff.Inconsistency == nil || *handoff.Inconsistency != inconsistency {
		t.Fatalf("inconsistency = %q want %#v", inconsistency, handoff.Inconsistency)
	}
}

func TestWatchOrphanTerminalConservativeGates(t *testing.T) {
	t.Run("unknown lock probe keeps waiting", func(t *testing.T) {
		st, _ := watchTestStore(t)
		seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)
		opts := watchTestOptions(false, time.Millisecond, nil)
		opts.probeLock = func(_ string) LockProbe {
			return LockProbe{State: LockUnknown, PID: "unknown"}
		}
		out := &bytes.Buffer{}
		terminal, err := watchTerminal(st, watchOrphanTaskID, out, opts)
		if err != nil {
			t.Fatal(err)
		}
		if terminal {
			t.Fatal("lock probe不能時にorphan terminalを出しました")
		}
	})
	t.Run("lease acquisition failure keeps waiting", func(t *testing.T) {
		st, _ := watchTestStore(t)
		seedOrphanTerminalTelemetry(t, st, watchOrphanTaskID)
		opts := watchTestOptions(false, time.Millisecond, nil)
		opts.acquireLease = func(_ string) (*repoLockLease, error) {
			return nil, ErrRepoLockLeaseUnavailable
		}
		out := &bytes.Buffer{}
		terminal, err := watchTerminal(st, watchOrphanTaskID, out, opts)
		if err != nil {
			t.Fatal(err)
		}
		if terminal {
			t.Fatal("lease取得不能時にorphan terminalを出しました")
		}
		if watchEventIndex(parseWatchEvents(t, out.String()), "watch_exit") >= 0 {
			t.Fatalf("lease取得不能時にwatch_exitが出ています: %s", out.String())
		}
	})
}

func TestWatchKeepsFollowingWhenMaterialIsNotTerminalError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		telemetry func(st *state.StateStore, taskID string)
	}{
		{
			name: "success material",
			telemetry: func(st *state.StateStore, taskID string) {
				st.RecordModelCallLog(state.ModelCallLog{
					CallID:   "call-ok",
					CallType: state.CallTypeTask,
					TaskID:   taskID,
					Phase:    "worker-new",
					Role:     state.WorkerRole,
					Outcome:  "success",
				})
			},
		},
		{
			name: "probe only",
			telemetry: func(st *state.StateStore, taskID string) {
				st.RecordModelCallLog(state.ModelCallLog{
					CallID:   "probe-only",
					CallType: state.CallTypeProbe,
					TaskID:   taskID,
					Phase:    "worker-new",
					Outcome:  "success",
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, _ := watchTestStore(t)
			writeTaskEventLines(t, st, watchOrphanTaskID,
				state.TaskEventRecord{TaskID: watchOrphanTaskID, CallID: "call-ok", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init"},
			)
			tc.telemetry(st, watchOrphanTaskID)

			out := &watchSyncBuffer{}
			stop := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- printWatch(st, out, watchTestOptions(false, 5*time.Millisecond, stop))
			}()
			time.Sleep(60 * time.Millisecond)
			select {
			case err := <-done:
				t.Fatalf("terminal errorでないmaterialでwatchが終端しました: %v", err)
			default:
			}
			close(stop)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("watchがstopで終了しません")
			}
			if watchEventIndex(parseWatchEvents(t, out.String()), "watch_exit") >= 0 {
				t.Fatalf("terminal errorでないmaterialでwatch_exitが出ています: %s", out.String())
			}
		})
	}
}
