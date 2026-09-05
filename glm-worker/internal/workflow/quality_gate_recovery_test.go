package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const qualityGateVersionMismatchFailure = "quality tool version mismatch: golangci-lint=2.6.0, required=2.7.0"

func TestQualityGateFailureAfterWorkerResultStopsRecoverable(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("implemented")}}}
	w := newGitWorkflowT(t, st, r, repo)
	w.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{}, errors.New(qualityGateVersionMismatchFailure)
	}

	err := w.ExecuteNewTask("request")
	var stopped *QualityGateRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("QualityGateRecoverableErrorを期待: %v", err)
	}
	if !stopped.ResultSaved || stopped.Phase != "worker-new" {
		t.Fatalf("stop error = %#v", stopped)
	}
	if !strings.Contains(stopped.Failure, "quality tool version mismatch") {
		t.Fatalf("stop failure evidence = %q", stopped.Failure)
	}
	if st.TaskStatus() != state.TaskStatusQualityGateRecoverable {
		t.Fatalf("status = %s want quality-gate-recoverable", st.TaskStatus())
	}
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.StopKind != state.ResumeStopQualityGate ||
		checkpoint.Stage != state.ResumeStageWorker ||
		checkpoint.Role != state.WorkerRole ||
		checkpoint.Phase != "worker-new" ||
		checkpoint.ReviewNumber != 1 ||
		checkpoint.AutoFixes != 0 {
		t.Fatalf("checkpoint identity = %#v", checkpoint)
	}
	if checkpoint.CompletedResult == nil || checkpoint.CompletedResult.Status != packet.StatusImplemented {
		t.Fatalf("completed worker resultが保存されていません: %#v", checkpoint.CompletedResult)
	}
	if !strings.Contains(checkpoint.QualityGateFailure, "quality tool version mismatch") {
		t.Fatalf("gate failure evidence = %q", checkpoint.QualityGateFailure)
	}
	if checkpoint.StopGitSnapshot == nil || checkpoint.StopGitSnapshot.Head == "" || checkpoint.StopDirtyFiles == nil {
		t.Fatalf("recovery retention baselineがありません: %#v", checkpoint)
	}

	plan, planErr := st.ParentActionPlan()
	if planErr != nil {
		t.Fatal(planErr)
	}
	if plan.RequiredAction != state.ParentActionRepairQualityGateThenResume ||
		len(plan.AllowedActions) != 1 ||
		plan.AllowedActions[0] != state.ParentActionResume ||
		plan.ResumeKind != string(state.ResumeStopQualityGate) {
		t.Fatalf("recovery plan = %#v", plan)
	}
	if st.TaskStatus() == state.TaskStatusActive {
		t.Fatal("停止後にactive/stale + required_action:noneの行き止まりを残しています")
	}
}

func TestQualityGateViolationAfterWorkerResultRoutesWorkerFixNotStop(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("initial")},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	w := newGitWorkflowT(t, st, r, repo)
	w.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	calls := 0
	w.qualityGate = func(string) (harnesslint.Report, error) {
		calls++
		if calls == 1 {
			return harnesslint.Report{Status: "fail", Violations: []harnesslint.Violation{{Rule: "funlen", Path: "a.go", Line: 3, Column: 1, Message: "too long"}}}, nil
		}
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() == state.TaskStatusQualityGateRecoverable {
		t.Fatal("lint violationは実装defectとして同一session fix経路へ流すべきでありrecoverable stopにしない")
	}
	if len(r.phases) != 3 || r.phases[1] != "worker-auto-fix-1" {
		t.Fatalf("phases = %v", r.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}
}

func stopQualityGateWithVersionMismatch(t *testing.T, st *state.StateStore, repo string) {
	t.Helper()
	stopRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("implemented")}}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	stopWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{}, errors.New(qualityGateVersionMismatchFailure)
	}
	if err := stopWorkflow.ExecuteNewTask("request"); err == nil {
		t.Fatal("quality gate stopを期待")
	}
}

func TestQualityGateRecoveryResumeReviewsSavedResultWithoutWorkerRecall(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopQualityGateWithVersionMismatch(t, st, repo)

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	resumeWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("修復後resumeが失敗: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s want complete", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1" {
		t.Fatalf("保存済みworker resultからreviewだけ再開すべき: phases=%v", resumeRunner.phases)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("成功resumeはcheckpointを削除すべき")
	}
}

func TestQualityGateRecoveryResumeViolationFixesInSameWorkerSession(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopQualityGateWithVersionMismatch(t, st, repo)

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	calls := 0
	resumeWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		calls++
		if calls == 1 {
			return harnesslint.Report{Status: "fail", Violations: []harnesslint.Violation{{Rule: "funlen", Path: "a.go", Line: 3, Column: 1, Message: "too long"}}}, nil
		}
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("violation fix resumeが失敗: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 2 || resumeRunner.phases[0] != "worker-auto-fix-1" || resumeRunner.phases[1] != "reviewer-2-high-floor" {
		t.Fatalf("同一worker sessionのfixと独立review再実行を期待: phases=%v", resumeRunner.phases)
	}
	if !strings.Contains(resumeRunner.prompts[0], "harnesslint") {
		t.Fatalf("fix prompt = %s", resumeRunner.prompts[0])
	}
}

func TestQualityGateRecoveryResumeStillFailingReStopsWithoutModelCalls(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopQualityGateWithVersionMismatch(t, st, repo)

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("must not run")}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	resumeWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{}, errors.New("required quality tool is missing: golangci-lint")
	}
	err := resumeWorkflow.ExecuteResume()
	var stopped *QualityGateRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("再stopを期待: %v", err)
	}
	if !strings.Contains(stopped.Failure, "required quality tool is missing") {
		t.Fatalf("再stopのfailure evidence = %q", stopped.Failure)
	}
	if st.TaskStatus() != state.TaskStatusQualityGateRecoverable {
		t.Fatalf("status = %s want quality-gate-recoverable", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 0 {
		t.Fatalf("未修復の環境ではmodel callを実行しない: phases=%v", resumeRunner.phases)
	}
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.StopKind != state.ResumeStopQualityGate || checkpoint.CompletedResult == nil {
		t.Fatalf("再stopはcheckpointを保持すべき: %#v", checkpoint)
	}
}

func TestQualityGateRecoveryDecisionContinuationPreservesDecisionAndResumes(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-decision", "decision-body"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("decision done")}}}
	w := newGitWorkflowT(t, st, r, repo)
	w.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{}, errors.New(qualityGateVersionMismatchFailure)
	}

	err := w.ExecuteDecision("decision-body")
	var stopped *QualityGateRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("QualityGateRecoverableErrorを期待: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusQualityGateRecoverable {
		t.Fatalf("status = %s want quality-gate-recoverable", st.TaskStatus())
	}
	if st.Exists("pending-decision") {
		t.Fatal("quality-gate stopがpending-decisionを残して行き止まり状態にしています")
	}
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.Decision != "decision-body" {
		t.Fatalf("保存済みdecisionがcheckpointへ保持されていません: %q", checkpoint.Decision)
	}
	if checkpoint.CompletedResult == nil || checkpoint.CompletedResult.Status != packet.StatusImplemented {
		t.Fatalf("completed worker resultが保存されていません: %#v", checkpoint.CompletedResult)
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil {
		t.Fatal(planErr)
	}
	if plan.RequiredAction != state.ParentActionRepairQualityGateThenResume ||
		len(plan.AllowedActions) != 1 ||
		plan.AllowedActions[0] != state.ParentActionResume {
		t.Fatalf("decision継続のrecovery plan = %#v", plan)
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: needsSolReviewPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	resumeWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("decision継続resumeが失敗: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1-high-floor" {
		t.Fatalf("保存済みdecision結果から独立reviewだけ再開すべき: phases=%v", resumeRunner.phases)
	}
}

func TestQualityGateRecoveryDirtyDriftFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("implemented")}}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	stopWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{}, errors.New("harnesslint internal failure")
	}
	if err := stopWorkflow.ExecuteNewTask("request"); err == nil {
		t.Fatal("quality gate stopを期待")
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.md"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	err := resumeWorkflow.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("dirty driftはfail closedすべき: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusQualityGateRecoverable {
		t.Fatalf("status = %s want quality-gate-recoverable", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 0 {
		t.Fatalf("dirty driftでmodel callを実行しました: %v", resumeRunner.phases)
	}
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.StopKind != state.ResumeStopQualityGate {
		t.Fatal("fail closedはquality gate recovery checkpointを保持すべき")
	}
}
