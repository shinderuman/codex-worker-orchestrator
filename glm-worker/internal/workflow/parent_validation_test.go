package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func parentValidationTerminalPassT(t *testing.T, record qualitygate.RunRecord) qualitygate.RunRecord {
	t.Helper()
	completed := time.Now().UTC()
	record.CompletedAt = &completed
	record.Status = qualitygate.StatusPass
	record.ExitCode = 0
	record.ExitSource = state.ValidationExitSourceTarget
	record.Log = filepath.Join(t.TempDir(), "gate.log")
	if err := os.WriteFile(record.Log, []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestParentValidationFailureFixesBeforeIndependentReview(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: packetBody(packet.Result{
			Status:                     packet.StatusImplemented,
			Risk:                       packet.RiskLow,
			Summary:                    "initial",
			RequirementCoverage:        "covered",
			Tests:                      "sandbox could not execute required process test",
			Unverified:                 "parent process validation required",
			ParentValidation:           packet.ParentValidationGoTest,
			ParentValidationWorkingDir: "glm-worker",
		})},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}

	previous := parentValidationGateRunner
	defer func() { parentValidationGateRunner = previous }()
	gateCalls := 0
	parentValidationGateRunner = func(_ *Workflow, request packet.ParentValidationRequest) (qualitygate.RunRecord, error) {
		gateCalls++
		if request.Form != packet.ParentValidationGoTest || request.WorkingDir != "glm-worker" {
			t.Fatalf("parent validation request = %#v", request)
		}
		record := qualitygate.RunRecord{
			Form:           request.Form,
			Repository:     w.config.RepoRoot,
			WorkingDir:     workingDir,
			Head:           fixedSnapshot.Head,
			IndexDigest:    fixedSnapshot.IndexDigest,
			WorktreeDigest: fixedSnapshot.WorktreeDigest,
		}
		switch gateCalls {
		case 1:
			if !reflect.DeepEqual(r.phases, []string{"worker-new"}) {
				t.Fatalf("first gate ran after unexpected model phases: %v", r.phases)
			}
			record.ValidationRunID = "run-fail"
			record.Status = qualitygate.StatusFail
			record.ExitCode = 1
			record.Log = "/evidence/run-fail/gate.log"
			return record, nil
		case 2:
			if !reflect.DeepEqual(r.phases, []string{"worker-new", "worker-auto-fix-1"}) {
				t.Fatalf("second gate ran after reviewer or unexpected model phase: %v", r.phases)
			}
			record.ValidationRunID = "run-pass"
			return parentValidationTerminalPassT(t, record), nil
		default:
			t.Fatalf("unexpected parent validation call %d", gateCalls)
			return qualitygate.RunRecord{}, nil
		}
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if gateCalls != 2 {
		t.Fatalf("parent validation calls = %d", gateCalls)
	}
	if !reflect.DeepEqual(r.phases, []string{"worker-new", "worker-auto-fix-1", "reviewer-1-high-floor"}) {
		t.Fatalf("model phases = %v", r.phases)
	}
	if !strings.Contains(r.prompts[1], "validation_run_id=run-fail") {
		t.Fatalf("fix prompt lacks exact failed validation evidence: %s", r.prompts[1])
	}
	if !strings.Contains(r.prompts[2], "parent_validation_evidence") || !strings.Contains(r.prompts[2], `"validation_run_id":"run-pass"`) {
		t.Fatalf("review prompt lacks passed parent validation evidence: %s", r.prompts[2])
	}
}

func parentValidationObligatedPacket(summary string) string {
	return packetBody(packet.Result{
		Status:                     packet.StatusImplemented,
		Risk:                       packet.RiskLow,
		Summary:                    summary,
		RequirementCoverage:        "covered",
		Tests:                      "sandbox could not execute required process test",
		Unverified:                 "parent process validation required",
		ParentValidation:           packet.ParentValidationGoTest,
		ParentValidationWorkingDir: "glm-worker",
	})
}

func exhaustParentValidationFixBudget(t *testing.T, st *state.StateStore) (string, []string) {
	t.Helper()
	r := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("initial")},
		{structured: implementedPacket("fixed")},
	}}
	var output bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &output)
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/parent_validation_test.go"}, nil
	}
	w.temp = t.TempDir()
	w.config.MaxAutoFixRounds = 1
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	gateCalls := 0
	parentValidationGateRunner = func(_ *Workflow, request packet.ParentValidationRequest) (qualitygate.RunRecord, error) {
		gateCalls++
		return qualitygate.RunRecord{
			ValidationRunID: fmt.Sprintf("run-fail-%d", gateCalls),
			Form:            request.Form,
			Repository:      w.config.RepoRoot,
			WorkingDir:      workingDir,
			Head:            fixedSnapshot.Head,
			IndexDigest:     fixedSnapshot.IndexDigest,
			WorktreeDigest:  fixedSnapshot.WorktreeDigest,
			Status:          qualitygate.StatusFail,
			ExitCode:        1,
			Log:             fmt.Sprintf("/evidence/run-fail-%d/gate.log", gateCalls),
		}, nil
	}
	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("budget exhaustion must land in waiting-sol-review, got %s", st.TaskStatus())
	}
	return output.String(), r.phases
}

func TestParentValidationBudgetExhaustionKeepsSameTaskContinuable(t *testing.T) {
	st := newStateStoreT(t)
	previous := parentValidationGateRunner
	defer func() { parentValidationGateRunner = previous }()
	emitted, phases := exhaustParentValidationFixBudget(t, st)

	if !reflect.DeepEqual(phases, []string{"worker-new", "worker-auto-fix-1"}) {
		t.Fatalf("model phases = %v", phases)
	}
	if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, state.ErrNoResumeCheckpoint) {
		t.Fatalf("non-convergence must not leave a resume checkpoint: %v", err)
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil {
		t.Fatal(planErr)
	}
	if plan.RequiredAction != state.ParentActionReview ||
		plan.Allows(state.ParentActionAccept) ||
		!plan.Allows(state.ParentActionFix) ||
		!plan.Allows(state.ParentActionPark) {
		t.Fatalf("non-convergence recovery plan = %#v", plan)
	}
	lastReview, err := st.Read("last-review")
	if err != nil || !strings.Contains(lastReview, "validation_run_id=run-fail-2") {
		t.Fatalf("explicit fix prompt evidence = %q err=%v", lastReview, err)
	}
	if !strings.Contains(emitted, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(emitted, "run-fail-2") {
		t.Fatalf("terminal packet lacks non-convergence evidence: %s", emitted)
	}
	if !strings.Contains(emitted, "worker fix budget exhausted: required parent-capability validation failed") {
		t.Fatalf("terminal packet summary misattributes the failure cause: %s", emitted)
	}
}

func TestParentValidationNonConvergenceFixRerunsGateBeforeReview(t *testing.T) {
	st := newStateStoreT(t)
	previous := parentValidationGateRunner
	defer func() { parentValidationGateRunner = previous }()
	exhaustParentValidationFixBudget(t, st)
	taskIDBefore, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}

	fixRunner := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("fixed for the gate")},
		{structured: needsSolReviewPacket()},
	}}
	var fixOutput bytes.Buffer
	fixWorkflow := newWorkflowTWithOutput(t, st, fixRunner, &fixOutput)
	fixWorkflow.temp = t.TempDir()
	fixWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	fixWorkingDir := filepath.Join(fixWorkflow.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(fixWorkingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	gateRuns := 0
	parentValidationGateRunner = func(_ *Workflow, request packet.ParentValidationRequest) (qualitygate.RunRecord, error) {
		gateRuns++
		if !reflect.DeepEqual(fixRunner.phases, []string{"worker-explicit-fix"}) {
			t.Fatalf("revalidation ran at unexpected phases: %v", fixRunner.phases)
		}
		return parentValidationTerminalPassT(t, qualitygate.RunRecord{
			ValidationRunID: "run-pass",
			Form:            request.Form,
			Repository:      fixWorkflow.config.RepoRoot,
			WorkingDir:      fixWorkingDir,
			Head:            fixedSnapshot.Head,
			IndexDigest:     fixedSnapshot.IndexDigest,
			WorktreeDigest:  fixedSnapshot.WorktreeDigest,
		}), nil
	}

	if err := fixWorkflow.ExecuteExplicitFixWithExecutionMilestones("make the failing gate pass", state.ParentOriginGLMReviewer, "", ""); err != nil {
		t.Fatal(err)
	}
	if gateRuns != 1 {
		t.Fatalf("parent validation runs after explicit fix = %d", gateRuns)
	}
	if !strings.Contains(fixRunner.prompts[0], "validation_run_id=run-fail-2") {
		t.Fatalf("explicit fix prompt lacks the failed gate evidence: %s", fixRunner.prompts[0])
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status after fix = %s want waiting-sol-review", st.TaskStatus())
	}
	open, openErr := st.CurrentParentReview()
	if openErr != nil || open == nil || open.ParentValidationNonConvergence {
		t.Fatalf("post-fix review must drop the non-convergence admission marker: %#v err=%v", open, openErr)
	}
	binding, bindingErr := st.CurrentParentReviewBinding()
	if bindingErr != nil || binding == nil {
		t.Fatalf("post-fix review must be a normal bound reviewer review: %#v err=%v", binding, bindingErr)
	}
	fixPlan, fixPlanErr := st.ParentActionPlan()
	if fixPlanErr != nil {
		t.Fatal(fixPlanErr)
	}
	if fixPlan.RequiredAction != state.ParentActionReview ||
		fixPlan.Allows(state.ParentActionAccept) ||
		!fixPlan.Allows(state.ParentActionFix) ||
		!fixPlan.Allows(state.ParentActionPark) {
		t.Fatalf("post-fix plan must be the normal evidence-gated review admission = %#v", fixPlan)
	}
	if taskID, err := st.TaskID(); err != nil || taskID != taskIDBefore {
		t.Fatalf("task identity changed across the fix round: %s want %s err=%v", taskID, taskIDBefore, err)
	}
	if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, state.ErrNoResumeCheckpoint) {
		t.Fatalf("fix continuation must not leave a resume checkpoint: %v", err)
	}
}

func TestParentValidationBudgetExhaustionKeepsFailureTargets(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("initial")},
		{structured: implementedPacket("fixed")},
	}}
	var output bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &output)
	w.temp = t.TempDir()
	w.config.MaxAutoFixRounds = 1
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "fail", Violations: []harnesslint.Violation{{Rule: "funlen", Path: "a.go", Line: 3, Column: 1, Message: "too long"}}}, nil
	}
	previous := parentValidationGateRunner
	defer func() { parentValidationGateRunner = previous }()
	parentValidationGateRunner = func(_ *Workflow, _ packet.ParentValidationRequest) (qualitygate.RunRecord, error) {
		t.Error("pre-gate violation must not reach the parent gate")
		return qualitygate.RunRecord{}, errors.New("unexpected parent gate call")
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	emitted := output.String()
	if !strings.Contains(emitted, `"targets":["a.go:3"]`) {
		t.Fatalf("terminal packet must keep the harnesslint failure targets: %s", emitted)
	}
	if !strings.Contains(emitted, "worker fix budget exhausted: machine quality gate") {
		t.Fatalf("terminal packet summary misattributes the harnesslint failure: %s", emitted)
	}
}

func TestCheckpointParentValidationCannotBeDroppedOrChanged(t *testing.T) {
	checkpoint := stateCheckpointWithParentValidation(packet.ParentValidationRequest{
		Form:       packet.ParentValidationGoTest,
		WorkingDir: "glm-worker",
	})

	got, err := applyCheckpointParentValidation(checkpoint, packet.Result{
		Status: packet.StatusImplemented,
		Risk:   packet.RiskLow,
	})
	if err != nil {
		t.Fatal(err)
	}
	gotRequest := got.ParentValidationRequest()
	if gotRequest == nil || !sameParentValidationRequest(*gotRequest, *checkpoint.ParentValidation) || got.Risk != packet.RiskHigh {
		t.Fatalf("checkpoint obligation was not preserved: %#v", got)
	}

	_, err = applyCheckpointParentValidation(checkpoint, packet.Result{
		Status:                     packet.StatusImplemented,
		Risk:                       packet.RiskLow,
		ParentValidation:           packet.ParentValidationGoTestRace,
		ParentValidationWorkingDir: "glm-worker",
	})
	if err == nil {
		t.Fatal("worker changed the checkpoint-owned parent validation obligation")
	}
}

func TestParentValidationRecordRejectsStaleSnapshot(t *testing.T) {
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, &scriptedRunner{})
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := qualitygate.RunRecord{
		ValidationRunID: "run-pass",
		Form:            packet.ParentValidationGoTest,
		Repository:      w.config.RepoRoot,
		WorkingDir:      workingDir,
		Head:            fixedSnapshot.Head,
		IndexDigest:     fixedSnapshot.IndexDigest,
		WorktreeDigest:  "stale-worktree",
		Status:          qualitygate.StatusPass,
	}
	err := w.validateParentValidationRecord(packet.ParentValidationRequest{
		Form:       packet.ParentValidationGoTest,
		WorkingDir: "glm-worker",
	}, record)
	if err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("stale parent validation evidence was accepted: %v", err)
	}
}

func TestParentValidationRecordRejectsStatusOnlyPass(t *testing.T) {
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, &scriptedRunner{})
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := qualitygate.RunRecord{
		ValidationRunID: "run-pass",
		Form:            packet.ParentValidationGoTest,
		Repository:      w.config.RepoRoot,
		WorkingDir:      workingDir,
		Head:            fixedSnapshot.Head,
		IndexDigest:     fixedSnapshot.IndexDigest,
		WorktreeDigest:  fixedSnapshot.WorktreeDigest,
		Status:          qualitygate.StatusPass,
	}
	err := w.validateParentValidationRecord(packet.ParentValidationRequest{
		Form:       packet.ParentValidationGoTest,
		WorkingDir: "glm-worker",
	}, record)
	if err == nil || !strings.Contains(err.Error(), "terminal integrity") {
		t.Fatalf("status-only parent validation PASS was accepted: %v", err)
	}
}

func stateCheckpointWithParentValidation(request packet.ParentValidationRequest) state.ResumeCheckpoint {
	return state.ResumeCheckpoint{ParentValidation: cloneParentValidationRequest(&request)}
}

func TestDecisionParentValidationAutoFixStopResumesRegularly(t *testing.T) {
	st := newStateStoreT(t)
	seedWaitingDecision(t, st)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("decision implementation")},
		{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "fail", Violations: []harnesslint.Violation{{Path: "glm-worker/internal/workflow/parent_validation.go", Line: 1, Column: 1, Rule: "cyclop", Message: "decision continuation violation"}}}, nil
	}

	err := w.ExecuteDecision("A案で進める")
	var limitErr runner.ZaiRateLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("rate limit errorを期待: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if st.Exists("pending-decision") {
		t.Fatal("accepted decision-phase worker result left its pending decision behind the auto-fix stop")
	}
	checkpoint, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || checkpoint.Stage != state.ResumeStageAutoFix || checkpoint.Phase != "worker-auto-fix-1" ||
		checkpoint.StopKind != state.ResumeStopRateLimited || checkpoint.AutoFixes != 1 || checkpoint.Decision != "A案で進める" {
		t.Fatalf("auto-fix stop checkpoint = %#v err=%v", checkpoint, cerr)
	}
	if checkpoint.ParentValidation == nil || checkpoint.ParentValidation.Form != packet.ParentValidationGoTest {
		t.Fatalf("auto-fix stop checkpoint lost the parent validation obligation: %#v", checkpoint.ParentValidation)
	}
	plan, perr := st.ParentActionPlan()
	if perr != nil || plan.RequiredAction != state.ParentActionResume || !plan.Allows(state.ParentActionResume) {
		t.Fatalf("rate-limited auto-fix resume admission = %#v err=%v", plan, perr)
	}

	previous := parentValidationGateRunner
	defer func() { parentValidationGateRunner = previous }()
	parentValidationGateRunner = func(w *Workflow, request packet.ParentValidationRequest) (qualitygate.RunRecord, error) {
		record := qualitygate.RunRecord{
			ValidationRunID: "run-pass",
			Form:            request.Form,
			Repository:      w.config.RepoRoot,
			WorkingDir:      filepath.Join(w.config.RepoRoot, "glm-worker"),
			Head:            fixedSnapshot.Head,
			IndexDigest:     fixedSnapshot.IndexDigest,
			WorktreeDigest:  fixedSnapshot.WorktreeDigest,
		}
		return parentValidationTerminalPassT(t, record), nil
	}
	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	resumeW := newWorkflowT(t, st, resumeRunner)
	resumeW.temp = t.TempDir()
	if err := os.MkdirAll(filepath.Join(resumeW.config.RepoRoot, "glm-worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("resume後のstate: status=%q pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if len(resumeRunner.phases) < 2 || resumeRunner.phases[0] != "worker-auto-fix-1" || resumeRunner.phases[1] != "reviewer-2-high-floor" {
		t.Fatalf("resume phases = %v", resumeRunner.phases)
	}
	if !strings.Contains(resumeRunner.prompts[0], "A案で進める") {
		t.Fatal("resumed auto-fix prompt lost the decision context")
	}
}
