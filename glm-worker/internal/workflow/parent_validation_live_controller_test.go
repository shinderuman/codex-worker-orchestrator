package workflow

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type liveControllerStopShape struct {
	name      string
	status    state.TaskStatus
	stopKind  state.ResumeStopKind
	step      runnerStep
	probes    bool
	interrupt bool
}

type liveControllerStopScenario struct {
	repo      string
	cfg       config.AppConfig
	store     *controller.Store
	st        *state.StateStore
	activated controller.Admission
	taskID    string
}

var liveControllerStopShapes = []liveControllerStopShape{
	{
		name:     "rate-limited",
		status:   state.TaskStatusRateLimited,
		stopKind: state.ResumeStopRateLimited,
		step:     runnerStep{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
	},
	{
		name:     "provider-unavailable",
		status:   state.TaskStatusProviderUnavailable,
		stopKind: state.ResumeStopProviderUnavailable,
		step:     runnerStep{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
		probes:   true,
	},
	{
		name:      "interrupted",
		status:    state.TaskStatusInterrupted,
		stopKind:  state.ResumeStopInterrupted,
		step:      runnerStep{result: runner.RunResult{SessionID: "auto-fix-session"}, runErr: &runner.InterruptedCallError{Phase: "worker-auto-fix-1"}},
		interrupt: true,
	},
}

func liveControllerWorkflowT(t *testing.T, cfg config.AppConfig, st *state.StateStore, r *scriptedRunner, out io.Writer) *Workflow {
	t.Helper()
	w := NewWorkflow(cfg, st, r, out)
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	return w
}

func (shape liveControllerStopShape) assertStopError(t *testing.T, err error) {
	t.Helper()
	switch shape.stopKind {
	case state.ResumeStopRateLimited:
		var limit runner.ZaiRateLimitError
		if !errors.As(err, &limit) {
			t.Fatalf("rate limit errorを期待: %v", err)
		}
	case state.ResumeStopProviderUnavailable:
		var unavailable *runner.ProviderUnavailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("provider unavailable errorを期待: %v", err)
		}
	default:
		var interrupted *runner.InterruptedCallError
		if !errors.As(err, &interrupted) {
			t.Fatalf("interrupt errorを期待: %v", err)
		}
	}
}

func stopLiveControllerDecisionAtAutoFix(t *testing.T, shape liveControllerStopShape) liveControllerStopScenario {
	t.Helper()
	repo, cfg, store, st, _, _ := newQualityMutationFixture(t)
	activated, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	waitOutput := &bytes.Buffer{}
	waitRunner := &scriptedRunner{steps: []runnerStep{{structured: needsSolDecisionPacket()}}}
	if err := liveControllerWorkflowT(t, cfg, st, waitRunner, waitOutput).ExecuteNewTask("integration request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("new task did not stop at a pending Sol decision: status=%q pending=%t output=%s", st.TaskStatus(), st.Exists("pending-decision"), waitOutput.String())
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}

	stopRunner := &scriptedRunner{steps: []runnerStep{
		{structured: parentValidationObligatedPacket("decision implementation")},
		shape.step,
	}}
	if shape.probes {
		stopRunner.probeErrs = []error{errProbeTransient, errProbeTransient, errProbeTransient, errProbeTransient}
	}
	stopOutput := &bytes.Buffer{}
	stopWorkflow := liveControllerWorkflowT(t, cfg, st, stopRunner, stopOutput)
	if shape.probes {
		clock := newFakeClock()
		stopWorkflow.now = clock.nowFunc
		stopWorkflow.sleep = clock.sleepFunc
		stopWorkflow.jitter = identityJitter
	}
	if shape.interrupt {
		stop := attachStop(t, stopWorkflow)
		stopRunner.onRun = func() {
			if len(stopRunner.prompts) == 2 {
				stop.Request()
			}
		}
	}
	stopWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "fail", Violations: []harnesslint.Violation{{Path: "glm-worker/internal/workflow/parent_validation.go", Line: 1, Column: 1, Rule: "cyclop", Message: "decision continuation violation"}}}, nil
	}
	shape.assertStopError(t, stopWorkflow.ExecuteDecision("A案で進める"))
	if st.TaskStatus() != shape.status {
		t.Fatalf("auto-fix stop status = %q want %q output=%s", st.TaskStatus(), shape.status, stopOutput.String())
	}
	return liveControllerStopScenario{repo: repo, cfg: cfg, store: store, st: st, activated: activated, taskID: taskID}
}

func TestDecisionAutoFixStopPreservesLiveControllerAuthority(t *testing.T) {
	for _, shape := range liveControllerStopShapes {
		t.Run(shape.name, func(t *testing.T) {
			scenario := stopLiveControllerDecisionAtAutoFix(t, shape)
			if scenario.st.Exists("pending-decision") {
				t.Fatal("accepted decision-phase worker result left its pending decision behind the auto-fix stop")
			}
			checkpoint, cerr := scenario.st.LoadResumeCheckpoint()
			if cerr != nil || checkpoint.Stage != state.ResumeStageAutoFix || checkpoint.Phase != "worker-auto-fix-1" ||
				checkpoint.StopKind != shape.stopKind || checkpoint.AutoFixes != 1 ||
				checkpoint.Decision != "A案で進める" || checkpoint.Request != "integration request" {
				t.Fatalf("live-controller auto-fix stop checkpoint = %#v err=%v", checkpoint, cerr)
			}
			if obligation := loadFinalValidationObligation(t, scenario.st); obligation.Form != packet.ParentValidationGoTest || obligation.WorkingDir != "glm-worker" {
				t.Fatalf("declared final validation obligation = %#v", obligation)
			}
			plan, perr := scenario.st.ParentActionPlan()
			if perr != nil || plan.RequiredAction != state.ParentActionResume || !plan.Allows(state.ParentActionResume) {
				t.Fatalf("canonical handoff after the auto-fix stop = %#v err=%v", plan, perr)
			}
			head, err := scenario.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if head.LiveLeaseID == "" || head.LiveAttemptID != scenario.activated.Attempt.AttemptID {
				t.Fatalf("auto-fix stop changed the live controller execution: %#v", head)
			}
			if head.ControllerGeneration <= scenario.activated.Head.ControllerGeneration {
				t.Fatalf("decision/auto-fix model calls did not advance the live lease: %#v", head)
			}
			binding, berr := scenario.st.LoadControllerRuntimeBinding()
			if berr != nil || binding.AttemptID != head.LiveAttemptID {
				t.Fatalf("runtime binding left the live attempt: %#v err=%v", binding, berr)
			}
			if afterTask, terr := scenario.st.TaskID(); terr != nil || afterTask != scenario.taskID {
				t.Fatalf("auto-fix stop changed the workflow task identity: %s want %s err=%v", afterTask, scenario.taskID, terr)
			}
		})
	}
}

func TestDecisionAutoFixStopResumesThroughLiveControllerLease(t *testing.T) {
	for _, shape := range liveControllerStopShapes {
		t.Run(shape.name, func(t *testing.T) {
			scenario := stopLiveControllerDecisionAtAutoFix(t, shape)
			head, err := scenario.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			resumeRunner := &scriptedRunner{steps: []runnerStep{
				{structured: parentValidationObligatedPacket("fixed")},
				{structured: needsSolReviewPacket()},
			}}
			resumeOutput := &bytes.Buffer{}
			resumeWorkflow := liveControllerWorkflowT(t, scenario.cfg, scenario.st, resumeRunner, resumeOutput)
			resumeWorkflow.qualityGate = func(string) (harnesslint.Report, error) {
				return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
			}
			if err := resumeWorkflow.ExecuteResume(); err != nil {
				t.Fatal(err)
			}
			if scenario.st.TaskStatus() != state.TaskStatusWaitingSolReview || scenario.st.Exists("pending-decision") {
				t.Fatalf("resume後のstate: status=%q pending=%t output=%s", scenario.st.TaskStatus(), scenario.st.Exists("pending-decision"), resumeOutput.String())
			}
			if !reflect.DeepEqual(resumeRunner.phases, []string{"worker-auto-fix-1", "reviewer-2-high-floor"}) {
				t.Fatalf("resume phases = %v", resumeRunner.phases)
			}
			if !strings.Contains(resumeRunner.prompts[0], "A案で進める") {
				t.Fatal("resumed auto-fix prompt lost the decision context")
			}
			if strings.Contains(resumeRunner.prompts[1], "parent_validation_evidence") {
				t.Fatalf("round review prompt must not carry parent validation evidence: %s", resumeRunner.prompts[1])
			}
			if !strings.Contains(resumeRunner.prompts[1], "parent process validation required") {
				t.Fatalf("review prompt lost the worker's unverified claim: %s", resumeRunner.prompts[1])
			}
			if _, rerr := scenario.st.LoadResumeCheckpoint(); !errors.Is(rerr, state.ErrNoResumeCheckpoint) {
				t.Fatalf("completed resume left a checkpoint behind: %v", rerr)
			}
			review, rvErr := scenario.st.CurrentParentReview()
			if rvErr != nil || review == nil {
				t.Fatalf("resume did not land in a bound parent review: %#v err=%v", review, rvErr)
			}
			resumedHead, err := scenario.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if resumedHead.LiveLeaseID == "" || resumedHead.LiveAttemptID != scenario.activated.Attempt.AttemptID {
				t.Fatalf("resume lost the live controller execution: %#v", resumedHead)
			}
			if resumedHead.ControllerGeneration <= head.ControllerGeneration {
				t.Fatalf("resumed auto-fix did not re-admit through the live lease: %#v", resumedHead)
			}
			if afterTask, terr := scenario.st.TaskID(); terr != nil || afterTask != scenario.taskID {
				t.Fatalf("resume changed the workflow task identity: %s want %s err=%v", afterTask, scenario.taskID, terr)
			}
			if err := admitQualityMutationFixtureSnapshot(t, scenario.store, scenario.repo); err != nil {
				t.Fatalf("post-resume workspace must still match the live lease: %v", err)
			}
			unattributed := filepath.Join(scenario.repo, "unattributed.txt")
			if err := os.WriteFile(unattributed, []byte("unknown workspace mutation\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := admitQualityMutationFixtureSnapshot(t, scenario.store, scenario.repo); err == nil {
				t.Fatal("unknown workspace mutation was admitted against the live lease")
			}
			if err := os.Remove(unattributed); err != nil {
				t.Fatal(err)
			}
		})
	}
}
