//go:build unix

package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopControllerRequestIsIdempotent(t *testing.T) {
	controller := NewStopController()
	if controller.StopRequested() {
		t.Fatal("初期状態で停止要求を観測しています")
	}
	controller.Request()
	controller.Request()
	if !controller.StopRequested() {
		t.Fatal("停止要求を観測しません")
	}
	select {
	case <-controller.Requested():
	default:
		t.Fatal("要求channelがcloseされていません")
	}
}

func TestStopControllerOutcomeFirstNotifyWins(t *testing.T) {
	controller := NewStopController()
	controller.NotifyInterrupted("task-1", "")
	controller.NotifyFinished()
	outcome := controller.WaitOutcome()
	if !outcome.Interrupted || outcome.TaskID != "task-1" {
		t.Fatalf("先頭確定 = %#v", outcome)
	}
	if got := controller.WaitOutcome(); got != outcome {
		t.Fatalf("再観測が最初の確定と一致しません: %#v", got)
	}
}

func TestStopControllerConcurrentWaitersShareOutcome(t *testing.T) {
	controller := NewStopController()
	const waiters = 3
	results := make(chan StopOutcome, waiters)
	started := make(chan struct{}, waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			started <- struct{}{}
			results <- controller.WaitOutcome()
		}()
	}
	for i := 0; i < waiters; i++ {
		<-started
	}

	time.Sleep(50 * time.Millisecond)
	controller.NotifyInterrupted("task-broadcast-1", "")
	controller.NotifyFinished()

	want := StopOutcome{Interrupted: true, TaskID: "task-broadcast-1"}
	for i := 0; i < waiters; i++ {
		select {
		case got := <-results:
			if got != want {
				t.Fatalf("waiter %dのoutcome = %#v want %#v", i, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("同時waiterが確定outcomeを受け取っていません")
		}
	}
	if got := controller.WaitOutcome(); got != want {
		t.Fatalf("確定後の待機 = %#v want %#v", got, want)
	}
}

func stopTestStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "term-ignoring-stub")
	script := `#!/bin/sh
trap '' TERM
echo $$ > "$1"
(
  trap '' TERM
  : > "$3"
  while :; do sleep 0.2; done
) &
echo $! > "$2"
while :; do sleep 0.2; done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitStopStubPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stubがPID file %sを書きません", path)
	return 0
}

func waitStopStubFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stubがfile %s を書きません", path)
}

func TestRunCommandStopsProcessGroupWithTermIgnoringChildren(t *testing.T) {
	stub := stopTestStub(t)
	pidFile := filepath.Join(t.TempDir(), "parent.pid")
	grandchildFile := filepath.Join(t.TempDir(), "grandchild.pid")
	readyFile := filepath.Join(t.TempDir(), "grandchild.ready")

	originalGrace, originalSettle, originalGap := stopTermGrace, killSettleTimeout, processGroupPollGap
	stopTermGrace, killSettleTimeout, processGroupPollGap = 150*time.Millisecond, time.Second, 10*time.Millisecond
	t.Cleanup(func() {
		stopTermGrace, killSettleTimeout, processGroupPollGap = originalGrace, originalSettle, originalGap
	})

	runner := &ClaudeRunner{}
	stop := NewStopController()
	runner.AttachStopController(stop)
	command := newProcessGroupCmd(stub, pidFile, grandchildFile, readyFile)

	result := make(chan error, 1)
	go func() { result <- runner.runCommand(command) }()
	pgid := waitStopStubPID(t, pidFile)
	grandchild := waitStopStubPID(t, grandchildFile)
	waitStopStubFile(t, readyFile)
	stop.Request()

	select {
	case err := <-result:
		var interrupted *InterruptedCallError
		if !errors.As(err, &interrupted) {
			t.Fatalf("InterruptedCallErrorを期待: %v", err)
		}
		if interrupted.CleanupWarning != "" {
			t.Fatalf("group非残存なのにwarningを出しています: %s", interrupted.CleanupWarning)
		}
	case <-time.After(15 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatal("停止が完了しません")
	}

	for _, target := range []int{-pgid, grandchild} {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if syscall.Kill(target, syscall.Signal(0)) != nil {
				break
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("process %dが停止後もgroupへ残存しています", target)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRunCommandPrefersCompletedNaturalExit(t *testing.T) {
	stub := filepath.Join(t.TempDir(), "quick-exit")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &ClaudeRunner{}
	stop := NewStopController()
	runner.AttachStopController(stop)
	if err := runner.runCommand(newProcessGroupCmd(stub)); err != nil {
		t.Fatalf("自然終了がerrorになりました: %v", err)
	}
}
