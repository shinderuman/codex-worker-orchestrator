package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualitySurfaceApprovalStopsBeforeConvergenceAndReusesWorkerResult(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "quality-stop@example.invalid")
	gitScope(t, repo, "config", "user.name", "quality-stop-test")
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
		RepoHash:              strings.Repeat("b", 64),
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
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, "baseline"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, ""); err != nil {
		t.Fatal(err)
	}

	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 1\n")
	writeScopeFile(t, repo, "worker_change.go", "package sample\n")

	runner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("rules applied")},
		{structured: passPacket()},
	}}
	var output bytes.Buffer
	w := NewWorkflow(cfg, st, runner, &output)
	w.captureQualitySurface = func(string) (string, error) { return "changed", nil }
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass"}, nil
	}

	checkpoint := state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole,
		Model: "worker", Effort: "low", Prompt: "work", OriginalPrompt: "work", Request: "task",
	}
	result := packet.Result{
		Status: packet.StatusImplemented, Risk: packet.RiskLow, Summary: "implemented",
		RequirementCoverage: "covered", Tests: "pass", Unverified: "none",
	}
	got, err := w.convergeWorkerRuleActivation(checkpoint, result, map[workerRule]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != packet.StatusImplemented || len(runner.phases) != 0 {
		t.Fatalf("early stop result=%s phases=%v", got.Status, runner.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.QualitySurfaceApprovalPending || saved.CompletedResult == nil {
		t.Fatalf("checkpoint = %#v", saved)
	}

	output.Reset()
	if err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if len(runner.phases) != 2 || runner.phases[0] != "worker-new-rule-activation-1" || runner.phases[1] != "reviewer-1" {
		t.Fatalf("phases = %v", runner.phases)
	}
	for _, phase := range runner.phases {
		if phase == "worker-explicit-fix" {
			t.Fatalf("approval-only redispatched worker: %v", runner.phases)
		}
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s", st.TaskStatus())
	}
}
