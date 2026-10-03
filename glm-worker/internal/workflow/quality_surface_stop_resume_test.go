package workflow

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestTransientStopResumeRestoresQualitySurfaceApprovalBoundary(t *testing.T) {
	w, st, checkpoint, runner := qualitySurfaceStoppedResumeFixture(t)

	_, restored, err := w.prepareResumeCheckpoint(checkpoint, externalFeasibility{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !restored {
		t.Fatal("resume did not terminate at the restored approval boundary")
	}
	if len(runner.phases) != 0 {
		t.Fatalf("resume reran semantic model work: %v", runner.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}

	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if saved.StopKind != state.ResumeStopNone || !saved.QualitySurfaceApprovalPending || saved.CompletedResult == nil {
		t.Fatalf("restored checkpoint = %#v", saved)
	}
	if saved.QualitySurfaceApprovalActivated {
		t.Fatalf("pre-activation stop restored as activated: %#v", saved)
	}
	if saved.StopGitSnapshot == nil || saved.StopGitSnapshot.Head == "" || saved.StopDirtyFiles == nil {
		t.Fatalf("restored approval retention = %#v", saved)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionApproveSurface || !plan.Allows(state.ParentActionApproveSurface) {
		t.Fatalf("parent action = %#v", plan)
	}
}

func TestActivatedQualitySurfaceRateLimitResumesContinuationWithoutReapproval(t *testing.T) {
	_, st, scripted, w := newQualitySurfaceDecisionWorkflow(t, []runnerStep{{
		runErr: errors.New("exit status 1"),
		result: runner.RunResult{PlainFailure: runner.ProviderFailureClass{
			Kind: runner.ProviderFailureZaiFiveHour,
			FiveHourLimit: runner.ZaiFiveHourLimit{
				ResetAtRFC3339: "2026-09-09T14:06:34+08:00",
			},
		}},
	}})
	stopDecisionContinuationForQualitySurface(t, st, w)

	err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff)
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("rate limit errorを期待: %v", err)
	}
	if len(scripted.phases) != 1 || scripted.phases[0] != "worker-decision-rule-activation-1" {
		t.Fatalf("phases = %v", scripted.phases)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %s want rate-limited", st.TaskStatus())
	}

	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if saved.StopKind != state.ResumeStopRateLimited || !saved.QualitySurfaceApprovalPending ||
		!saved.QualitySurfaceApprovalActivated || saved.CompletedResult == nil {
		t.Fatalf("activated stop checkpoint = %#v", saved)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionResume || !plan.Allows(state.ParentActionResume) {
		t.Fatalf("stopped parent action = %#v", plan)
	}

	prepared, restored, err := w.prepareResumeCheckpoint(saved, externalFeasibility{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if restored {
		t.Fatal("activated continuation incorrectly restored approve-surface")
	}
	if prepared.QualitySurfaceApprovalPending || prepared.QualitySurfaceApprovalActivated {
		t.Fatalf("activated continuation retained approval obligation after resume admission: %#v", prepared)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("resume status = %s want active", st.TaskStatus())
	}
}

func TestTransientStopResumeRejectsStaleQualitySurfaceRetention(t *testing.T) {
	w, st, checkpoint, _ := qualitySurfaceStoppedResumeFixture(t)
	writeScopeFile(t, w.config.RepoRoot, "worker.go", "package sample\n\nvar changed = 2\n")

	_, restored, err := w.prepareResumeCheckpoint(checkpoint, externalFeasibility{}, false)
	if err == nil {
		t.Fatal("stale quality-surface retention unexpectedly resumed")
	}
	if restored {
		t.Fatal("stale quality-surface retention reported restored")
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("stale resume changed status = %s", st.TaskStatus())
	}
}

func qualitySurfaceStoppedResumeFixture(
	t *testing.T,
) (*Workflow, *state.StateStore, state.ResumeCheckpoint, *scriptedRunner) {
	t.Helper()
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "quality-stop-resume@example.invalid")
	gitScope(t, repo, "config", "user.name", "quality-stop-resume-test")
	writeScopeFile(t, repo, "worker.go", "package sample\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")
	writeScopeFile(t, repo, "worker.go", "package sample\n\nvar changed = 1\n")

	cfg := config.AppConfig{
		RepoRoot:              repo,
		RepoHash:              strings.Repeat("e", 64),
		StateBase:             t.TempDir(),
		WorkerModel:           "worker",
		ReviewerModel:         "reviewer",
		HighRiskReviewerModel: "reviewer-high",
		RoutineEffort:         "low",
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	runner := &scriptedRunner{}
	w := newUnitWorkflow(cfg, st, runner, io.Discard)
	result := packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "implemented",
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	}
	checkpoint := state.ResumeCheckpoint{
		Stage:                         state.ResumeStageWorker,
		Phase:                         "worker-new",
		Role:                          state.WorkerRole,
		Model:                         "worker",
		Effort:                        "low",
		Prompt:                        "work",
		OriginalPrompt:                "work",
		Request:                       "task",
		CompletedResult:               &result,
		QualitySurfaceApprovalPending: true,
	}
	if err := w.captureStopRetention(&checkpoint); err != nil {
		t.Fatal(err)
	}
	checkpoint.SetStopKind(state.ResumeStopRateLimited)
	checkpoint.ResetAtRFC3339 = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if err := st.EnterStop(checkpoint); err != nil {
		t.Fatal(err)
	}
	return w, st, checkpoint, runner
}
