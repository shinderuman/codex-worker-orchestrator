package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRecoverableGuardFailurePersistsTaskCheckpoint(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	r := guardFailureRunner("done")
	w := newGitWorkflowT(t, st, r, repo)

	_, err := w.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("GuardRecoverableErrorを期待: %v", err)
	}
	checkpoint := retentionCheckpoint(t, st)
	if st.TaskStatus() != state.TaskStatusGuardRecoverable || checkpoint.StopKind != state.ResumeStopGuardRecoverable {
		t.Fatalf("guard recovery state = status:%s checkpoint:%#v", st.TaskStatus(), checkpoint)
	}
	if checkpoint.CompletedResult == nil || checkpoint.CompletedResult.Status != "IMPLEMENTED" {
		t.Fatalf("completed worker resultが保存されていません: %#v", checkpoint.CompletedResult)
	}
	if checkpoint.StopGitSnapshot == nil || checkpoint.StopGitSnapshot.Head == "" || checkpoint.StopDirtyFiles == nil {
		t.Fatalf("recovery retention baselineがありません: %#v", checkpoint)
	}
}

func TestGuardRecoveryReusesCompletedWorkerAfterRepairCommit(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := guardFailureRunner("done")
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	if _, err := stopWorkflow.runModel(workerCheckpoint()); err == nil {
		t.Fatal("guard failure stopを期待")
	}

	writeRetentionFile(t, filepath.Join(repo, "guard-repair.txt"), []byte("fixed\n"), 0o644)
	runRetentionGit(t, repo, "add", "guard-repair.txt")
	runRetentionGit(t, repo, "commit", "-q", "-m", "repair guard")

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("guard recovery resume failed: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s want complete", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1" {
		t.Fatalf("workerを重複実行しました: phases=%v", resumeRunner.phases)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("successful recovery must clear checkpoint")
	}
}

func TestGuardRecoveryDirtyDriftFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopWorkflow := newGitWorkflowT(t, st, guardFailureRunner("done"), repo)
	if _, err := stopWorkflow.runModel(workerCheckpoint()); err == nil {
		t.Fatal("guard failure stopを期待")
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.md"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeWorkflow.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("dirty drift must fail closed: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status = %s want guard-recoverable", st.TaskStatus())
	}
	if len(resumeRunner.prompts) != 0 {
		t.Fatalf("dirty driftでmodel callを実行しました: %d", len(resumeRunner.prompts))
	}
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.StopKind != state.ResumeStopGuardRecoverable {
		t.Fatal("fail closed must preserve guard recovery checkpoint")
	}
}

func guardFailureRunner(summary string) *scriptedRunner {
	return &scriptedRunner{steps: []runnerStep{{
		structured: implementedPacket(summary),
		runErr: &runner.GitAuthorityGuardError{
			Stage:     "blocked-command",
			Mutations: []string{"command:branch"},
		},
	}}}
}

func restoredInstructionMutationErr() *runner.InstructionSurfaceGuardError {
	return &runner.InstructionSurfaceGuardError{
		Stage:        "after-call-mutation",
		ChangedPaths: []string{"codex/AGENTS.md"},
		Restored:     true,
	}
}

func TestRestoredInstructionMutationConvergesDecisionContinuationToGuardRecovery(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-decision", "A案で進める"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	checkpoint := workerCheckpoint()
	checkpoint.Phase = "worker-decision"
	checkpoint.Decision = "A案で進める"
	w := newGitWorkflowT(t, st, &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("decision done"), runErr: restoredInstructionMutationErr()},
	}}, repo)

	_, err := w.runModel(checkpoint)
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("GuardRecoverableErrorを期待: %v", err)
	}
	if !stopped.ResultSaved {
		t.Fatal("有効なworker resultがあるためResultSavedであるべき")
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status = %s want guard-recoverable", st.TaskStatus())
	}
	if st.Exists("pending-decision") {
		t.Fatal("guard recovery stateがpending-decisionを残しています")
	}
	saved := retentionCheckpoint(t, st)
	if saved.StopKind != state.ResumeStopGuardRecoverable || saved.Decision != "A案で進める" {
		t.Fatalf("checkpoint = %#v", saved)
	}
	if saved.CompletedResult == nil || saved.CompletedResult.Status != "IMPLEMENTED" {
		t.Fatalf("completed worker resultが保存されていません: %#v", saved.CompletedResult)
	}
	if saved.StopGitSnapshot == nil || saved.StopGitSnapshot.Head == "" || saved.StopDirtyFiles == nil {
		t.Fatalf("recovery retention baselineがありません: %#v", saved)
	}

	gateRunner := &scriptedRunner{}
	gateWorkflow := newGitWorkflowT(t, st, gateRunner, repo)
	assertGuardRecoveryRejected(t, gateRunner, "decision", gateWorkflow.ExecuteDecision("再送"), "no pending Sol decision for this repository")
	assertGuardRecoveryRejected(t, gateRunner, "fix", gateWorkflow.ExecuteExplicitFix("fix", ""), "--fix is only available after NEEDS_SOL_REVIEW; start a new task after PASS")
	assertGuardRecoveryRejected(t, gateRunner, "new task", gateWorkflow.ExecuteNewTask("replacement"), "previous task stopped on a recoverable guard failure; repair the guard then use --resume or --reset")

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("same-task resumeが失敗: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 2 || resumeRunner.phases[0] != "reviewer-1-high-floor" || resumeRunner.phases[1] != "reviewer-1-risk-floor" {
		t.Fatalf("保存済みworker resultを再利用し独立reviewとrisk floor再出力だけで閉じるべき: phases=%v", resumeRunner.phases)
	}
}

func assertGuardRecoveryRejected(t *testing.T, gateRunner *scriptedRunner, label string, err error, wantMessage string) {
	t.Helper()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || workerErr.Message != wantMessage {
		t.Fatalf("guard停止後の%sを拒否すべき(%q): %v", label, wantMessage, err)
	}
	if calls := len(gateRunner.prompts); calls != 0 {
		t.Fatalf("拒否された%sがmodel呼出を実行しました: %d", label, calls)
	}
}

func TestRestoredInstructionMutationWithoutResultRerunsWorkerOnResume(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	w := newGitWorkflowT(t, st, &scriptedRunner{steps: []runnerStep{
		{runErr: restoredInstructionMutationErr()},
	}}, repo)

	_, err := w.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("GuardRecoverableErrorを期待: %v", err)
	}
	if stopped.ResultSaved {
		t.Fatal("resultを保存していない停止でResultSavedがtrue")
	}
	saved := retentionCheckpoint(t, st)
	if saved.CompletedResult != nil || saved.StopKind != state.ResumeStopGuardRecoverable {
		t.Fatalf("checkpoint = %#v", saved)
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("re-run")},
		{structured: passPacket()},
	}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("same-task resumeが失敗: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s want complete", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 2 || resumeRunner.phases[0] != "worker-new" || resumeRunner.phases[1] != "reviewer-1" {
		t.Fatalf("worker呼出を最初からやり直すべき: phases=%v", resumeRunner.phases)
	}
	assertGuardRecoveryResumePrompt(t, resumeRunner.prompts[0])
}

func TestReviewerPhaseRestoredInstructionMutationStopsAndResumesToTerminal(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done"), runErr: restoredInstructionMutationErr()},
	}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	if _, err := stopWorkflow.runModel(workerCheckpoint()); err == nil {
		t.Fatal("worker phaseのguard停止を期待")
	}

	reviewStopRunner := &scriptedRunner{steps: []runnerStep{
		{structured: passPacket(), runErr: restoredInstructionMutationErr()},
	}}
	reviewStopWorkflow := newGitWorkflowT(t, st, reviewStopRunner, repo)
	err := reviewStopWorkflow.ExecuteResume()
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("reviewer phaseもGuardRecoverableErrorを期待: %v", err)
	}
	if stopped.ResultSaved {
		t.Fatal("reviewer phaseの停止でworker resultを保存済み扱いしてはいけない")
	}
	saved := retentionCheckpoint(t, st)
	if saved.Stage != state.ResumeStageReview || saved.Role != state.ReviewerRole {
		t.Fatalf("reviewer checkpointが保持されていません: %#v", saved)
	}
	if saved.WorkerResult == nil || saved.CompletedResult != nil {
		t.Fatalf("review resume context = worker:%#v completed:%#v", saved.WorkerResult, saved.CompletedResult)
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status = %s want guard-recoverable", st.TaskStatus())
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("reviewer resumeが失敗: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s want complete", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1" {
		t.Fatalf("reviewerだけを再実行すべき: phases=%v", resumeRunner.phases)
	}
	assertGuardRecoveryResumePrompt(t, resumeRunner.prompts[0])
}

func TestTransientRetryRestoredInstructionMutationStopsGuardRecoverable(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	r := &scriptedRunner{steps: []runnerStep{
		{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
		{structured: implementedPacket("recovered"), runErr: restoredInstructionMutationErr()},
	}}
	r.probeErrs = []error{nil}
	w := newGitWorkflowT(t, st, r, repo)

	_, err := w.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("transient再試行中のinstruction mutationもGuardRecoverableErrorを期待: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status = %s want guard-recoverable", st.TaskStatus())
	}
	saved := retentionCheckpoint(t, st)
	if saved.StopKind != state.ResumeStopGuardRecoverable || saved.CompletedResult == nil {
		t.Fatalf("checkpoint = %#v", saved)
	}
}

func TestUnrestoredInstructionMutationFailsClosedWithoutGuardRecovery(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	r := &scriptedRunner{steps: []runnerStep{{
		runErr: &runner.InstructionSurfaceGuardError{
			Stage:        "verify-restored",
			ChangedPaths: []string{"codex/AGENTS.md"},
			Cause:        errors.New("restored digest mismatch"),
		},
	}}}
	w := newGitWorkflowT(t, st, r, repo)

	_, err := w.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if errors.As(err, &stopped) {
		t.Fatalf("復元未確認のinstruction mutationはguard recoveryへ入れません: %v", err)
	}
	if err == nil {
		t.Fatal("fail closedを期待")
	}
	if _, loadErr := st.LoadResumeCheckpoint(); loadErr == nil {
		t.Fatal("fail closed時はresume checkpointを保持しません")
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
}

func assertGuardRecoveryResumePrompt(t *testing.T, prompt string) {
	t.Helper()
	if !strings.Contains(prompt, "RESUME_REASON: guard-recovery") ||
		!strings.Contains(prompt, "新しいsession") ||
		!strings.Contains(prompt, "同じtask") ||
		!strings.Contains(prompt, "保存済みcheckpoint") {
		t.Fatalf("guard recovery resume promptが新sessionと保存済みcheckpointによる同じtask継続を明示しません: %q", boundedText(prompt, 400))
	}
	for _, stale := range []string{"5時間", "plan-limit", "同じsession"} {
		if strings.Contains(prompt, stale) {
			t.Fatalf("guard recovery resume promptがrate-limit resumeの表現%qを含みます: %q", stale, boundedText(prompt, 400))
		}
	}
}
