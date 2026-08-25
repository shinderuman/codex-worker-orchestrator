package workflow

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// attachStopは--stop観測経路を接続したworkflowを返す。
func attachStop(t *testing.T, w *Workflow) *runner.StopController {
	t.Helper()
	stop := runner.NewStopController()
	w.AttachStopController(stop)
	return stop
}

// TestStopDuringRunningCallSavesInterruptedStateは実行中呼出の--stop停止を固定する:
// 中断errorの型、呼出実行分のtelemetry(outcome=interrupted)、既存停止理由との排他、
// checkpointのrole/phase/prompt/request保持、task status=interrupted、sessionのresumable化。
func TestStopDuringRunningCallSavesInterruptedState(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "test-session"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()
	stop := attachStop(t, w)
	r.onRun = func() { stop.Request() }
	// 実runnerは呼出前にsession IDを採番する。中断時のsession扱いを同じ前提で固定する。
	if err := st.Write("worker.id", "test-session"); err != nil {
		t.Fatal(err)
	}

	_, err := w.runModel(workerCheckpoint())
	var stopped *runner.InterruptedCallError
	if !errors.As(err, &stopped) {
		t.Fatalf("InterruptedCallErrorを期待: %v", err)
	}
	if stopped.Phase != "worker-new" || stopped.TaskID == "" || stopped.RepoRoot == "" {
		t.Fatalf("中断errorの識別field = %#v", stopped)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("実行中呼出の回数 = %d want 1", len(r.prompts))
	}

	checkpoint, cerr := st.LoadResumeCheckpoint()
	if cerr != nil {
		t.Fatal(cerr)
	}
	if !checkpoint.UserInterrupted || checkpoint.RateLimited || checkpoint.ProviderUnavailable {
		t.Fatalf("停止理由がuser interruption単独になっていません: %#v", checkpoint)
	}
	if checkpoint.Phase != "worker-new" || checkpoint.Role != state.WorkerRole ||
		!strings.HasPrefix(checkpoint.Prompt, "p\n") || checkpoint.Request != "req" || checkpoint.Model != "opus" {
		t.Fatalf("中断checkpointが呼出前の識別fieldを保持していません: %#v", checkpoint)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("task status = %s want interrupted", st.TaskStatus())
	}
	if !st.Exists("worker.ready") {
		t.Fatal("中断時にworker sessionがresumable化されていません")
	}
	if got := st.ReadOr("worker.id", ""); got != "test-session" {
		t.Fatalf("中断保存がsession IDを置き換えています: %q want test-session", got)
	}

	logs := taskLogs(t, st)
	interruptedCalls := 0
	for _, log := range logs {
		if log.CallType == state.CallTypeTask {
			interruptedCalls++
			if log.Outcome != "interrupted" {
				t.Fatalf("task呼出記録のoutcome = %s want interrupted", log.Outcome)
			}
		}
	}
	if interruptedCalls != 1 {
		t.Fatalf("中断した実呼出の記録 = %d want 1", interruptedCalls)
	}
	if stats := currentStats(t, st); stats.ModelCalls != 1 {
		t.Fatalf("stats model_calls = %d want 1", stats.ModelCalls)
	}
	outcome := stop.WaitOutcome()
	if !outcome.Interrupted || outcome.TaskID != stopped.TaskID {
		t.Fatalf("stop outcome = %#v", outcome)
	}
}

// TestStopCleanupResidualCarriesWarningIntoStopOutcomeは停止後のprocess group残存診断
// (terminateProcessGroupが返すwarningを載せたInterruptedCallError)がinterrupted保存を
// 経てstop outcomeへ渡ることを固定する。endpointはこのoutcome値で安全停止ackと残存時の
// typed outcomeを分けるため、ここでの受け渡しが失われると残存時も安全停止ackへ戻る。
// macOSのkill(-pgid,0)はzombieのみのgroupをEPERM=非残存として扱い、KILL後のlive残存は
// userspaceから決定論的に作れないため、残存観測はrunnerが返すerror値で注入する。
func TestStopCleanupResidualCarriesWarningIntoStopOutcome(t *testing.T) {
	st := newStateStoreT(t)
	warning := "process group 424242に残存processがあります"
	r := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "test-session"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new", CleanupWarning: warning},
	}}}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()
	stop := attachStop(t, w)
	r.onRun = func() { stop.Request() }
	if err := st.Write("worker.id", "test-session"); err != nil {
		t.Fatal(err)
	}

	_, err := w.runModel(workerCheckpoint())
	var stopped *runner.InterruptedCallError
	if !errors.As(err, &stopped) {
		t.Fatalf("InterruptedCallErrorを期待: %v", err)
	}
	if stopped.CleanupWarning != warning {
		t.Fatalf("中断errorの残存診断 = %q want %q", stopped.CleanupWarning, warning)
	}
	outcome := stop.WaitOutcome()
	if !outcome.Interrupted || outcome.TaskID != stopped.TaskID || outcome.CleanupWarning != warning {
		t.Fatalf("残存診断を載せたstop outcome = %#v", outcome)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("task status = %s want interrupted", st.TaskStatus())
	}
}

// TestStopBeforeCallSavesInterruptedWithoutCallRecordは呼出前の--stop観測を固定する:
// childを起動せず、call記録を作らない(event記録だけ)、checkpoint・statusは中断状態へ保存する。
func TestStopBeforeCallSavesInterruptedWithoutCallRecord(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done")}}}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()
	stop := attachStop(t, w)
	stop.Request()

	_, err := w.runModel(workerCheckpoint())
	var stopped *runner.InterruptedCallError
	if !errors.As(err, &stopped) {
		t.Fatalf("InterruptedCallErrorを期待: %v", err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("停止済みでmodel呼出を実行しています: %v", r.prompts)
	}

	checkpoint, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !checkpoint.UserInterrupted {
		t.Fatalf("中断checkpointが保存されていません: %#v err=%v", checkpoint, cerr)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("task status = %s want interrupted", st.TaskStatus())
	}
	logs := taskLogs(t, st)
	if len(logs) != 1 || logs[0].CallType != state.CallTypeEvent || logs[0].Outcome != "user_interrupted" {
		t.Fatalf("呼出前停止はevent記録だけを残すべき: %#v", logs)
	}
	if stats := currentStats(t, st); stats.ModelCalls != 0 {
		t.Fatalf("実行していない呼出が計上されています: %d", stats.ModelCalls)
	}
	// session ID未採番での停止はsessionをresumable化しない。readyだけ残るとresume時の
	// 新規採番UUIDが存在しないsessionへの--resume起動になる。
	if st.Exists("worker.ready") || st.ReadOr("worker.id", "") != "" {
		t.Fatal("session未採番の呼出前停止がsessionを採番・resumable化しています")
	}
}

// TestResumeAfterPreFirstCallStopStartsFreshSessionは初回call前停止→--resumeの境界を
// 固定する。session ID未採番のまま保存された中断stateは、resumeで同一checkpointのphaseを
// 実行し直し、新規session起動の前提(session未採番・未ready)を保ったまま完結する。
// 採番済みsessionの--resume引数境界はrunner testが固定する。
func TestResumeAfterPreFirstCallStopStartsFreshSession(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done")}}}
	w := newGitWorkflowT(t, st, r, repo)
	stop := attachStop(t, w)
	stop.Request()

	_, err := w.runModel(workerCheckpoint())
	var stopped *runner.InterruptedCallError
	if !errors.As(err, &stopped) {
		t.Fatalf("InterruptedCallErrorを期待: %v", err)
	}
	if st.Exists("worker.ready") || st.ReadOr("worker.id", "") != "" {
		t.Fatal("呼出前停止がsessionを採番・resumable化しています")
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("task status = %s want complete", st.TaskStatus())
	}
	if checkpoint, cerr := st.LoadResumeCheckpoint(); cerr == nil && checkpoint.UserInterrupted {
		t.Fatalf("再開済みcheckpointが中断状態のままです: %#v", checkpoint)
	}
	if len(resumeRunner.phases) == 0 || resumeRunner.phases[0] != "worker-new" {
		t.Fatalf("resumeが中断phaseを実行し直していません: %v", resumeRunner.phases)
	}
}

// TestStopDuringBackoffSleepInterruptsWithoutCallRecordはtransient backoff待機中の--stopを
// 固定する。再開task呼出を実行していないためcall記録を増やさず、初期transient呼出と
// user_interrupted eventだけがtelemetryへ残る。
func TestStopDuringBackoffSleepInterruptsWithoutCallRecord(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:     []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeErrs: []error{errProbeTransient},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()
	stop := attachStop(t, w)
	release := make(chan struct{})
	defer close(release)
	w.sleep = func(time.Duration) {
		stop.Request()
		<-release
	}

	_, err := w.runModel(workerCheckpoint())
	var stopped *runner.InterruptedCallError
	if !errors.As(err, &stopped) {
		t.Fatalf("InterruptedCallErrorを期待: %v", err)
	}
	if len(r.probes) != 0 {
		t.Fatalf("backoff停止後にprobeを実行しています: %v", r.probes)
	}
	checkpoint, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !checkpoint.UserInterrupted || checkpoint.ProviderUnavailable {
		t.Fatalf("停止状態 = %#v err=%v", checkpoint, cerr)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("task status = %s want interrupted", st.TaskStatus())
	}
	taskRecords := 0
	eventRecords := 0
	for _, log := range taskLogs(t, st) {
		switch {
		case log.CallType == state.CallTypeTask && log.Outcome == "transient_error":
			taskRecords++
		case log.CallType == state.CallTypeEvent && log.Outcome == "user_interrupted":
			eventRecords++
		default:
			t.Fatalf("予期しないtelemetry記録: %#v", log)
		}
	}
	if taskRecords != 1 || eventRecords != 1 {
		t.Fatalf("telemetry = task %d/event %d want 1/1", taskRecords, eventRecords)
	}
	if stats := currentStats(t, st); stats.ModelCalls != 1 {
		t.Fatalf("stats model_calls = %d want 1(初期transient呼出のみ)", stats.ModelCalls)
	}
}

// seedInterruptedCheckpointは--stop停止直後のstateを用意する。worker sessionは既に
// 採番・resumable済みとし、resumeが同一sessionへ戻ることを検証可能にする。
// repoRootにgit repositoryを渡すと停止時保持基準も停止保存と同じ形で固定する。
func seedInterruptedCheckpoint(t *testing.T, st *state.StateStore, sessionID string, repoRoot string) {
	t.Helper()
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("worker.id", sessionID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReady(state.WorkerRole); err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{
		Stage:           state.ResumeStageWorker,
		Phase:           "worker-new",
		Role:            state.WorkerRole,
		Model:           "opus",
		Effort:          "high",
		Prompt:          "p",
		OriginalPrompt:  "p",
		Request:         "req",
		ReportOnly:      false,
		UserInterrupted: true,
	}
	if repoRoot != "" {
		snapshot, snapErr := state.CaptureGitSnapshot(repoRoot)
		files, filesErr := state.CaptureStopDirtyFiles(repoRoot)
		if snapErr == nil && filesErr == nil {
			checkpoint.StopGitSnapshot = &snapshot
			checkpoint.StopDirtyFiles = files
		}
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusInterrupted); err != nil {
		t.Fatal(err)
	}
}

// TestResumeFromInterruptedCheckpointはinterrupted checkpointが--resumeで同一worker session
// から再開し完結することを固定する。停止理由fieldは再開時に消え、statusは完結へ遷移する。
func TestResumeFromInterruptedCheckpoint(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedInterruptedCheckpoint(t, st, "sess-interrupted-before", repo)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	w := newGitWorkflowT(t, st, r, repo)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if got := st.ReadOr("worker.id", ""); got != "sess-interrupted-before" {
		t.Fatalf("resume後にworker session = %q want sess-interrupted-before", got)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("task status = %s want complete", st.TaskStatus())
	}
	if checkpoint, err := st.LoadResumeCheckpoint(); err == nil && checkpoint.UserInterrupted {
		t.Fatalf("再開済みcheckpointが中断状態のままです: %#v", checkpoint)
	}
}

// TestExecuteNewTaskRejectsInterruptedCheckpointは中断taskの新規task投入をfail closedする
// ことを固定する。中断stateを--resetなしで上書きさせない。
func TestExecuteNewTaskRejectsInterruptedCheckpoint(t *testing.T) {
	st := newStateStoreT(t)
	seedInterruptedCheckpoint(t, st, "sess-interrupted-before", "")
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done")}}}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	err := w.ExecuteNewTask("new task over interrupted state")
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("WorkerErrorを期待: %v", err)
	}
	if !strings.Contains(workerErr.Message, "--resume") {
		t.Fatalf("中断stateへの新規taskがresumeへ誘導されていません: %s", workerErr.Message)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("中断statusが新規task投入で上書きされています: %s", st.TaskStatus())
	}
}
