package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

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

func loadFinalValidationObligation(t *testing.T, st *state.StateStore) packet.ParentValidationRequest {
	t.Helper()
	recorded, err := st.Read(state.FinalValidationObligationKey)
	if err != nil {
		t.Fatalf("final validation obligation was not recorded: %v", err)
	}
	var obligation packet.ParentValidationRequest
	if err := json.Unmarshal([]byte(recorded), &obligation); err != nil {
		t.Fatalf("recorded obligation = %q err=%v", recorded, err)
	}
	return obligation
}

func TestFinalValidationObligationRecordsOnceAndProceedsToReview(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("initial")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	if err := os.MkdirAll(filepath.Join(w.config.RepoRoot, "glm-worker"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.phases, []string{"worker-new", "reviewer-1"}) {
		t.Fatalf("declared validation must not gate the round before review: %v", r.phases)
	}
	obligation := loadFinalValidationObligation(t, st)
	if obligation.Form != packet.ParentValidationGoTest || obligation.WorkingDir != "glm-worker" {
		t.Fatalf("recorded obligation = %#v", obligation)
	}
	if strings.Contains(r.prompts[1], "parent_validation_evidence") {
		t.Fatalf("round review prompt must not carry parent validation evidence: %s", r.prompts[1])
	}
	if !strings.Contains(r.prompts[1], "parent process validation required") {
		t.Fatalf("review prompt lost the worker's unverified claim: %s", r.prompts[1])
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
}

func TestFinalValidationObligationRejectsMidTaskChange(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("initial")},
		{structured: fixRequiredPacket()},
		{structured: packetBody(packet.Result{
			Status:                     packet.StatusImplemented,
			Risk:                       packet.RiskLow,
			Summary:                    "fixed",
			RequirementCoverage:        "covered",
			Tests:                      "targeted tests pass",
			Unverified:                 "parent process validation required",
			ParentValidation:           packet.ParentValidationGoTestRace,
			ParentValidationWorkingDir: "glm-worker",
		})},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	if err := os.MkdirAll(filepath.Join(w.config.RepoRoot, "glm-worker"), 0o700); err != nil {
		t.Fatal(err)
	}

	err := w.ExecuteNewTask("request")
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(workerErr.Message, "changed the final boundary validation obligation") {
		t.Fatalf("mid-task obligation change must fail closed: %v", err)
	}
}

func TestFinalValidationObligationRecordsExplicitNone(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("no declared obligation")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	if err := os.MkdirAll(filepath.Join(w.config.RepoRoot, "glm-worker"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	recorded, err := st.Read(state.FinalValidationObligationKey)
	if err != nil || recorded != `{"None":true}` {
		t.Fatalf("explicit no-obligation record = %q err=%v", recorded, err)
	}
}

func TestFinalValidationObligationSurvivesFixWithoutRedeclaration(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("initial")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed without delegation")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	if err := os.MkdirAll(filepath.Join(w.config.RepoRoot, "glm-worker"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if obligation := loadFinalValidationObligation(t, st); obligation.Form != packet.ParentValidationGoTest || obligation.WorkingDir != "glm-worker" {
		t.Fatalf("Fix lost the final validation obligation: %#v", obligation)
	}
}

func TestDecisionAutoFixStopResumesRegularly(t *testing.T) {
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
	plan, perr := st.ParentActionPlan()
	if perr != nil || plan.RequiredAction != state.ParentActionResume || !plan.Allows(state.ParentActionResume) {
		t.Fatalf("rate-limited auto-fix resume admission = %#v err=%v", plan, perr)
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
	if strings.Contains(resumeRunner.prompts[1], "parent_validation_evidence") {
		t.Fatalf("round review prompt must not carry parent validation evidence: %s", resumeRunner.prompts[1])
	}
	obligation := loadFinalValidationObligation(t, st)
	if obligation.Form != packet.ParentValidationGoTest || obligation.WorkingDir != "glm-worker" {
		t.Fatalf("resumed obligation = %#v", obligation)
	}
}
