package workflow

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newQualitySurfaceDecisionWorkflow(t *testing.T, steps []runnerStep) (string, *state.StateStore, *scriptedRunner, *Workflow) {
	t.Helper()
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "quality-decision@example.invalid")
	gitScope(t, repo, "config", "user.name", "quality-decision-test")
	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 0\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")

	codexDir := t.TempDir()
	workerRules := filepath.Join(codexDir, "instructions", "worker")
	if err := os.MkdirAll(workerRules, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workerRules, "go.md"), []byte("apply go contract\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.AppConfig{
		RepoRoot:              repo,
		RepoHash:              strings.Repeat("f", 64),
		StateBase:             t.TempDir(),
		CodexConfigDir:        codexDir,
		WorkerModel:           "worker",
		ReviewerModel:         "reviewer",
		HighRiskReviewerModel: "reviewer-high",
		RoutineEffort:         "low",
		MaxAutoFixRounds:      1,
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	pinRepositoryHarnessInactiveT(t, st)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, "baseline"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-decision", "decision-body"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}

	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 1\n")
	writeScopeFile(t, repo, "worker_change.go", "package sample\n")

	scripted := &scriptedRunner{steps: steps}
	w := NewWorkflow(cfg, st, scripted, &bytes.Buffer{})
	w.captureQualitySurface = func(string) (string, error) { return "changed", nil }
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass"}, nil
	}
	return repo, st, scripted, w
}

func qualitySurfaceDecisionContinuationCheckpoint() state.ResumeCheckpoint {
	return state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-decision",
		Role:           state.WorkerRole,
		Model:          "worker",
		Effort:         "low",
		Prompt:         "work",
		OriginalPrompt: "work",
		Request:        "task",
		Decision:       "decision-body",
	}
}

func stopDecisionContinuationForQualitySurface(t *testing.T, st *state.StateStore, w *Workflow) state.ResumeCheckpoint {
	t.Helper()
	result := packet.Result{
		Status: packet.StatusImplemented, Risk: packet.RiskLow, Summary: "implemented",
		RequirementCoverage: "covered", Tests: "pass", Unverified: "none",
	}
	if _, err := w.convergeWorkerRuleActivation(qualitySurfaceDecisionContinuationCheckpoint(), result, map[workerRule]struct{}{}); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	if st.Exists("pending-decision") {
		t.Fatal("decision継続のquality-surface停止がpending-decisionを残しました")
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.QualitySurfaceApprovalPending || saved.CompletedResult == nil ||
		saved.CompletedResult.Status != packet.StatusImplemented {
		t.Fatalf("checkpoint = %#v", saved)
	}
	if label := st.OpenParentReviewLabel(); label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("open parent review = %q want NEEDS_SOL_REVIEW", label)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionApproveSurface ||
		plan.RequiredActionParameters["accepted-scope"] != "current-diff" ||
		!plan.Allows(state.ParentActionFix) || !plan.Allows(state.ParentActionPark) {
		t.Fatalf("plan = %#v", plan)
	}
	return saved
}

func TestDecisionContinuationQualitySurfaceStopConvergesToApprovalWait(t *testing.T) {
	_, st, scripted, w := newQualitySurfaceDecisionWorkflow(t, []runnerStep{
		{structured: implementedPacket("rules applied")},
		{structured: passPacket()},
	})

	stopDecisionContinuationForQualitySurface(t, st, w)
	if len(scripted.phases) != 0 {
		t.Fatalf("early stop phases = %v", scripted.phases)
	}

	if err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if len(scripted.phases) != 2 || scripted.phases[0] != "worker-decision-rule-activation-1" ||
		scripted.phases[1] != "reviewer-1" {
		t.Fatalf("phases = %v", scripted.phases)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s", st.TaskStatus())
	}
}

func TestApprovedQualitySurfaceSurvivesRateLimitStopInAutoFix(t *testing.T) {
	_, st, scripted, w := newQualitySurfaceDecisionWorkflow(t, []runnerStep{
		{structured: implementedPacket("rules applied")},
		{structured: fixRequiredPacket()},
		{
			runErr: errors.New("exit status 1"),
			result: runner.RunResult{PlainFailure: runner.ProviderFailureClass{
				Kind:          runner.ProviderFailureZaiFiveHour,
				FiveHourLimit: runner.ZaiFiveHourLimit{ResetAtRFC3339: "2026-09-09T14:06:34+08:00"},
			}},
		},
	})

	stopDecisionContinuationForQualitySurface(t, st, w)

	err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff)
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("rate limit errorを期待: %v", err)
	}
	if len(scripted.phases) != 3 || scripted.phases[0] != "worker-decision-rule-activation-1" ||
		scripted.phases[1] != "reviewer-1" || scripted.phases[2] != "worker-auto-fix-1" {
		t.Fatalf("phases = %v", scripted.phases)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %s want rate-limited", st.TaskStatus())
	}
	if label := st.OpenParentReviewLabel(); label != "none" {
		t.Fatalf("承認済みreviewがauto-fix停止へ持ち越されました: %s", label)
	}
	saved, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil || saved.StopKind != state.ResumeStopRateLimited || saved.Stage != state.ResumeStageAutoFix {
		t.Fatalf("checkpoint = %#v err=%v", saved, loadErr)
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil {
		t.Fatal(planErr)
	}
	if plan.RequiredAction != state.ParentActionResume || !plan.Allows(state.ParentActionResume) {
		t.Fatalf("plan = %#v", plan)
	}
}
