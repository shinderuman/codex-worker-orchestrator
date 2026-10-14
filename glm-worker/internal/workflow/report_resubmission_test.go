package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func terminalResultFormatStopFixture(t *testing.T, secondPacket string, prepare ...func(string)) (*state.StateStore, string) {
	t.Helper()
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	for _, setup := range prepare {
		setup(repo)
	}
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentDecision(); err != nil {
		t.Fatal(err)
	}
	if !st.Exists("pending-decision") {
		t.Fatal("fixture requires the pending-decision residue of the consumed decision")
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: secondPacket},
	}}
	w := newGitWorkflowT(t, st, r, repo)
	_, err := w.runModel(baseCorrectionCheckpoint())
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "repeated_violation" {
		t.Fatalf("repeated violation terminal failureを期待: err=%v failure=%#v", err, failure)
	}
	if err := st.MarkReady(state.WorkerRole); err != nil {
		t.Fatal(err)
	}
	return st, repo
}

func TestReportResubmissionRuleActivationUsesNormalWorkerContract(t *testing.T) {
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket(), func(repo string) {
		writeGitTestFile(t, repo, "worker_change.go", "package sample\n")
	})
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("accepted report")},
		{structured: implementedPacket("rules applied")},
		{structured: needsSolReviewPacket()},
	}}
	w := newGitWorkflowT(t, st, r, repo)
	w.config.CodexConfigDir = t.TempDir()
	writeRuleFile(t, w.config.CodexConfigDir, "go.md", "GO CONTRACT")
	r.onRun = func() {
		if strings.Contains(r.phases[len(r.phases)-1], "rule-activation") && st.Exists(state.ResultCorrectionStateFile) {
			t.Fatal("normal rule activation retained result-correction state")
		}
	}
	if err := w.ExecuteReportResubmission(); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 3 || r.phases[0] != "worker-new-result-correct" || r.phases[1] != "worker-new-result-correct-rule-activation-1" || !strings.HasPrefix(r.phases[2], "reviewer-") {
		t.Fatalf("phases = %v", r.phases)
	}
	if !r.readOnlyCalls[0] || r.readOnlyCalls[1] || !r.readOnlyCalls[2] {
		t.Fatalf("phase permissions = %v", r.readOnlyCalls)
	}
	if !strings.Contains(r.prompts[1], "GO CONTRACT") || st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("rule context or review completion missing: status=%s", st.TaskStatus())
	}
}

func TestTerminalResultCorrectionLeavesResubmittableStop(t *testing.T) {
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket())

	stop := assertResultFormatRecoveryAction(t, st)
	if stop.Phase != "worker-new"+resultCorrectionPhaseSuffix {
		t.Fatalf("stop checkpoint keeps the failed correction phase: %#v", stop)
	}
	record, err := loadResultCorrectionRecordForTest(t, st)
	if err != nil {
		t.Fatal(err)
	}
	if record.Terminal == nil || record.Resubmissions != 0 {
		t.Fatalf("terminal record = %#v", record)
	}

	resumeWorkflow := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	err = resumeWorkflow.ExecuteResume()
	if err == nil || !strings.Contains(err.Error(), "resubmit-report") {
		t.Fatalf("plain resume must point at the one-shot resubmission action: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusResultFormatRecoverable {
		t.Fatalf("denied resume must keep the result-format stop: %s", st.TaskStatus())
	}
}

func TestReportResubmissionRunsFreshModelCallThroughCurrentValidator(t *testing.T) {
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket())

	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resubmitted")},
		{structured: passPacket()},
	}}
	w := newGitWorkflowT(t, st, r, repo)
	if err := w.ExecuteReportResubmission(); err != nil {
		t.Fatalf("report resubmission failed: %v", err)
	}
	if len(r.phases) == 0 || r.phases[0] != "worker-new"+resultCorrectionPhaseSuffix {
		t.Fatalf("resubmission must re-run the same correction boundary: %v", r.phases)
	}
	if !strings.Contains(r.prompts[0], "意味検証に不合格") {
		t.Fatalf("resubmission must reuse the correction prompt: %s", r.prompts[0])
	}
	if len(r.readOnlyCalls) == 0 || !r.readOnlyCalls[0] {
		t.Fatalf("resubmission must stay read-only: %#v", r.readOnlyCalls)
	}
	if st.Exists(state.ResultCorrectionStateFile) {
		t.Fatal("accepted resubmission must clear the result correction record")
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("accepted resubmission must consume the stop checkpoint")
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status after resubmission = %s", st.TaskStatus())
	}
	if len(r.phases) != 2 || r.phases[1] != "reviewer-1" {
		t.Fatalf("resubmission must continue into the normal review flow: %v", r.phases)
	}
	if st.Exists("pending-decision") {
		t.Fatal("accepted resubmission must keep the pending decision closed")
	}
}

func TestReportResubmissionIsOneShotPerTask(t *testing.T) {
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket())

	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
	}}
	w := newGitWorkflowT(t, st, r, repo)
	err := w.ExecuteReportResubmission()
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "repeated_violation" {
		t.Fatalf("resubmitted violation must be terminal again: err=%v failure=%#v", err, failure)
	}
	assertResultFormatRecoveryAction(t, st)
	record, recordErr := loadResultCorrectionRecordForTest(t, st)
	if recordErr != nil {
		t.Fatal(recordErr)
	}
	if record.Terminal == nil || record.Resubmissions != 1 {
		t.Fatalf("post-resubmission terminal record = %#v", record)
	}
	if record.Terminal.Attempts != 1 || len(record.SeenViolationKeys) == 0 {
		t.Fatalf("terminal record must retain the correction history: %#v", record)
	}

	again := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	err = again.ExecuteReportResubmission()
	if err == nil || !strings.Contains(err.Error(), "already authorized once") {
		t.Fatalf("second authorization must be rejected: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusResultFormatRecoverable {
		t.Fatalf("rejected re-authorization must keep the stop: %s", st.TaskStatus())
	}
}

func TestParentValidationPreGateStopSavesResultAndResumesThroughConvergence(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	r := &scriptedRunner{steps: []runnerStep{{structured: parentValidationObligatedPacket("initial")}}}
	w := newGitWorkflowT(t, st, r, repo)
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{}, errors.New(qualityGateVersionMismatchFailure)
	}
	workingDir := filepath.Join(repo, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	previous := parentValidationGateRunner
	defer func() { parentValidationGateRunner = previous }()
	parentValidationGateRunner = func(_ *Workflow, _ packet.ParentValidationRequest) (qualitygate.RunRecord, error) {
		t.Error("pre-gate tool failure must not reach the parent validation gate")
		return qualitygate.RunRecord{}, errors.New("unexpected parent gate call")
	}

	err := w.ExecuteNewTask("request")
	var stopped *QualityGateRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("converge-entry gate failure must stop recoverably: %v", err)
	}
	if !strings.Contains(stopped.Failure, qualityGateVersionMismatchFailure) {
		t.Fatalf("stop failure = %q", stopped.Failure)
	}
	if st.TaskStatus() != state.TaskStatusQualityGateRecoverable {
		t.Fatalf("status = %s want quality-gate-recoverable", st.TaskStatus())
	}
	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if checkpoint.StopKind != state.ResumeStopQualityGate || checkpoint.CompletedResult == nil {
		t.Fatalf("stop checkpoint = %#v", checkpoint)
	}
	saved := checkpoint.CompletedResult
	if saved.ParentValidationRequest() == nil || saved.ParentValidationEvidence != nil {
		t.Fatalf("saved result must keep the unconverged parent validation obligation: %#v", saved)
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	gateRuns := 0
	parentValidationGateRunner = func(_ *Workflow, request packet.ParentValidationRequest) (qualitygate.RunRecord, error) {
		gateRuns++
		if len(resumeRunner.phases) != 0 {
			t.Fatalf("parent validation must converge before any reviewer recall: %v", resumeRunner.phases)
		}
		snapshot, snapErr := state.CaptureGitSnapshot(repo)
		if snapErr != nil {
			return qualitygate.RunRecord{}, snapErr
		}
		return parentValidationTerminalPassT(t, qualitygate.RunRecord{
			ValidationRunID: "run-pass",
			Form:            request.Form,
			Repository:      repo,
			WorkingDir:      workingDir,
			Head:            snapshot.Head,
			IndexDigest:     snapshot.IndexDigest,
			WorktreeDigest:  snapshot.WorktreeDigest,
		}), nil
	}

	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("resume through parent validation convergence failed: %v", err)
	}
	if gateRuns != 1 {
		t.Fatalf("parent validation runs = %d want 1", gateRuns)
	}
	if len(resumeRunner.phases) == 0 || !strings.HasPrefix(resumeRunner.phases[0], "reviewer-1") {
		t.Fatalf("resume must continue to review without worker recall: %v", resumeRunner.phases)
	}
	if !strings.Contains(resumeRunner.prompts[0], "parent_validation_evidence") {
		t.Fatalf("review prompt lacks the converged parent validation evidence: %s", resumeRunner.prompts[0])
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("successful resume must consume the stop checkpoint")
	}
}

func TestReportResubmissionInterruptedResumeIsNotANewAuthorization(t *testing.T) {
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket())

	interrupted := &scriptedRunner{steps: []runnerStep{
		{runErr: errors.New("provider crashed mid-report")},
	}}
	w := newGitWorkflowT(t, st, interrupted, repo)
	err := w.ExecuteReportResubmission()
	if err == nil || !strings.Contains(err.Error(), "provider crashed mid-report") {
		t.Fatalf("interrupted resubmission must surface the mid-call failure: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusResultFormatRecoverable {
		t.Fatalf("interrupted resubmission must restore the result-format stop: %s", st.TaskStatus())
	}
	record, recordErr := loadResultCorrectionRecordForTest(t, st)
	if recordErr != nil {
		t.Fatal(recordErr)
	}
	if record.Terminal != nil || record.Resubmissions != 1 {
		t.Fatalf("interrupted resubmission must keep the consumed authorization without a new terminal: %#v", record)
	}

	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resubmitted after interrupt")},
		{structured: passPacket()},
	}}
	continued := newGitWorkflowT(t, st, r, repo)
	if err := continued.ExecuteReportResubmission(); err != nil {
		t.Fatalf("authorized-boundary continuation failed: %v", err)
	}
	if st.Exists(state.ResultCorrectionStateFile) {
		t.Fatal("accepted continuation must clear the result correction record")
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status after continuation = %s", st.TaskStatus())
	}
}

func TestReportResubmissionRejectsSnapshotMismatch(t *testing.T) {
	st, repo := terminalResultFormatStopFixture(t, constraintViolatingImplementedPacket())

	stray := filepath.Join(repo, "stray-change.txt")
	if err := os.WriteFile(stray, []byte("concurrent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(stray) }()

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	err := w.ExecuteReportResubmission()
	if err == nil || !strings.Contains(err.Error(), "repository snapshot changed") {
		t.Fatalf("snapshot mismatch must reject the resubmission boundary: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusResultFormatRecoverable {
		t.Fatalf("rejected resubmission must keep the result-format stop: %s", st.TaskStatus())
	}
}

func loadResultCorrectionRecordForTest(t *testing.T, st *state.StateStore) (*resultCorrectionRecord, error) {
	t.Helper()
	w := &Workflow{state: st}
	return w.loadResultCorrectionRecord()
}
