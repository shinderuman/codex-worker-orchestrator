//go:build unix

package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopCommandProcessSeries(t *testing.T) {
	env := newMultiRepoEnv(t)

	stopWorkerCallWithToolChild(t, env)
	resumeInterruptedWorkerCall(t, env)
	stopReviewerCall(t, env)
	stopWithoutOwnerFailsAbsent(t, env)
}

func stopWorkerCallWithToolChild(t *testing.T, env *multiRepoEnv) {
	t.Helper()
	env.setStubMode(t, env.stubA, "hold-with-tool")
	holdCtx, cancel := context.WithTimeout(context.Background(), multiRepoRunTimeout)
	defer cancel()
	holder := env.start(t, holdCtx, env.repoA, "stop series worker marker STOPW1")

	stateA := env.waitStateDir(t, env.repoA, holder)
	env.waitHeldWithWorkerSession(t, stateA)
	toolPID := waitStopToolPID(t, env.stubA)

	stopResult := env.run(t, env.repoA, "--stop")
	if stopResult.code != 0 {
		t.Fatalf("--stopが失敗しました: code=%d stdout=%s stderr=%s", stopResult.code, stopResult.stdout, stopResult.stderr)
	}
	taskID := readStateFile(t, stateA, "task.id")
	for _, want := range []string{
		`"result":"interrupted"`,
		`"task_status":"interrupted"`,
		`"resume_available":true`,
	} {
		if !strings.Contains(stopResult.stdout, want) {
			t.Fatalf("停止ack %s が %s を含みません: %s", want, "ack", stopResult.stdout)
		}
	}
	if !strings.Contains(stopResult.stdout, `"task_id":"`+taskID+`"`) {
		t.Fatalf("停止ackのtask_idが現在taskと一致しません: %s (task %s)", stopResult.stdout, taskID)
	}

	holder.waitFailure(t)
	if !strings.Contains(holder.stderr.String(), `"kind":"interrupted"`) {
		t.Fatalf("停止されたinvocationがkind=interruptedで終端しません: %s", holder.stderr.String())
	}
	if strings.Contains(holder.stdout.String(), `"status":"PASS"`) {
		t.Fatalf("停止されたinvocationがPASSを出力しています: %s", holder.stdout.String())
	}
	assertStopProcessGone(t, toolPID)

	statusA := env.status(t, env.repoA)
	if !strings.Contains(statusA, `"task_status":"interrupted"`) || !strings.Contains(statusA, `"resume_available":true`) {
		t.Fatalf("停止後の--status = %s", statusA)
	}
	checkpoint := parseStateJSON(t, stateA, "resume-state.json")
	if checkpoint["user_interrupted"] != true || checkpoint["rate_limited"] == true {
		t.Fatalf("停止checkpoint = %#v", checkpoint)
	}
	if got, ok := checkpoint["prompt"].(string); !ok || !strings.Contains(got, "STOPW1") {
		t.Fatalf("停止checkpointが要求正本を保持していません: %#v", checkpoint["prompt"])
	}
}

func resumeInterruptedWorkerCall(t *testing.T, env *multiRepoEnv) {
	t.Helper()
	stateA := env.waitStateDir(t, env.repoA, nil)
	workerSession := readStateFile(t, stateA, "worker.id")
	env.setStubMode(t, env.stubA, "success")

	resumed := env.run(t, env.repoA, "--resume")
	if resumed.code != 0 || !strings.Contains(resumed.stdout, `"status":"PASS"`) {
		t.Fatalf("interrupted resumeが完結しません: code=%d stdout=%s stderr=%s", resumed.code, resumed.stdout, resumed.stderr)
	}
	if got := readStateFile(t, stateA, "worker.id"); got != workerSession {
		t.Fatalf("resume後のworker session = %s want %s", got, workerSession)
	}
}

func stopReviewerCall(t *testing.T, env *multiRepoEnv) {
	t.Helper()
	env.setStubMode(t, env.stubB, "reviewer-hold")
	holdCtx, cancel := context.WithTimeout(context.Background(), multiRepoRunTimeout)
	defer cancel()
	holder := env.start(t, holdCtx, env.repoB, "stop series reviewer marker STOPR1")

	stateB := env.waitStateDir(t, env.repoB, holder)
	waitStopFile(t, filepath.Join(stateB, "reviewer.id"))

	stopResult := env.run(t, env.repoB, "--stop")
	if stopResult.code != 0 || !strings.Contains(stopResult.stdout, `"result":"interrupted"`) {
		t.Fatalf("reviewer停止ackが成立しません: code=%d stdout=%s stderr=%s", stopResult.code, stopResult.stdout, stopResult.stderr)
	}
	holder.waitFailure(t)
	if !strings.Contains(holder.stderr.String(), `"kind":"interrupted"`) {
		t.Fatalf("reviewer停止invocationの終端 = %s", holder.stderr.String())
	}
	statusB := env.status(t, env.repoB)
	if !strings.Contains(statusB, `"task_status":"interrupted"`) {
		t.Fatalf("reviewer停止後の--status = %s", statusB)
	}

	env.setStubMode(t, env.stubB, "success")
	resumed := env.run(t, env.repoB, "--resume")
	if resumed.code != 0 || !strings.Contains(resumed.stdout, `"status":"PASS"`) {
		t.Fatalf("reviewer停止後のresumeが完結しません: code=%d stdout=%s stderr=%s", resumed.code, resumed.stdout, resumed.stderr)
	}
}

func stopWithoutOwnerFailsAbsent(t *testing.T, env *multiRepoEnv) {
	t.Helper()
	stopResult := env.run(t, env.repoB, "--stop")
	if stopResult.code == 0 {
		t.Fatalf("owner不在の--stopが成功しました: %s", stopResult.stdout)
	}
	if !strings.Contains(stopResult.stderr, `"kind":"stop_endpoint_absent"`) {
		t.Fatalf("owner不在の--stop error = %s", stopResult.stderr)
	}
}

func waitStopToolPID(t *testing.T, stub string) int {
	t.Helper()
	path := filepath.Join(filepath.Dir(stub), "tool.pid")
	waitStopFile(t, path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("tool child PIDを読めません: %q", string(data))
	}
	return pid
}

func waitStopFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(multiRepoWaitTimeout)
	for time.Now().Before(deadline) {
		if fileExists(path) {
			return
		}
		time.Sleep(multiRepoPollInterval)
	}
	t.Fatalf("file %s が現れません", path)
}

func assertStopProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if syscall.Kill(pid, syscall.Signal(0)) != nil {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("停止後もprocess %dが残存しています", pid)
		}
		time.Sleep(multiRepoPollInterval)
	}
}
