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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
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

	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 1\n")
	writeScopeFile(t, repo, "worker_change.go", "package sample\n")

	runner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("rules applied")},
		{structured: passPacket()},
	}}
	var output bytes.Buffer
	w := newUnitWorkflow(cfg, st, runner, &output)
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

func TestQualitySurfaceApprovalStopsAgainAfterLaterOutOfScopeMutation(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "quality-restop@example.invalid")
	gitScope(t, repo, "config", "user.name", "quality-restop-test")
	writeScopeFile(t, repo, "glm-worker/go.mod", "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n")
	writeScopeFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
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
		RepoHash:              strings.Repeat("c", 64),
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
	pinRepositoryHarnessActiveT(t, st)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	baseline, err := captureQualitySurfaceDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, baseline); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, ""); err != nil {
		t.Fatal(err)
	}

	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 1\n")
	writeScopeFile(t, repo, "worker_change.go", "package sample\n")

	runCount := 0
	runner := &scriptedRunner{
		steps: []runnerStep{
			{structured: implementedPacket("rules applied")},
			{structured: passPacket()},
		},
		onRun: func() {
			runCount++
			if runCount == 1 {
				writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 2\n")
			}
		},
	}
	var output bytes.Buffer
	w := newUnitWorkflow(cfg, st, runner, &output)
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
	if _, err := w.convergeWorkerRuleActivation(checkpoint, result, map[workerRule]struct{}{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.phases) != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("initial stop phases=%v status=%s", runner.phases, st.TaskStatus())
	}

	output.Reset()
	if err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if len(runner.phases) != 1 || runner.phases[0] != "worker-new-rule-activation-1" {
		t.Fatalf("later mutation should stop before reviewer: phases=%v", runner.phases)
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
}

func TestQualitySurfaceApprovalGeneratesTaskScopeOnProductionDirtyBaseline(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "dirty-scope@example.invalid")
	gitScope(t, repo, "config", "user.name", "dirty-scope-test")
	writeScopeFile(t, repo, "glm-worker/go.mod", "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n")
	writeScopeFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 0\n")
	writeScopeFile(t, repo, "preexisting_code.go", "package sample\n\nvar retained = 1\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")

	writeScopeFile(t, repo, "preexisting_code.go", "package sample\n\nvar retained = 1\nvar recoveredImplementation = 2\n")

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
		RepoHash:              strings.Repeat("d", 64),
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
	pinRepositoryHarnessActiveT(t, st)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	baseline, err := captureQualitySurfaceDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, baseline); err != nil {
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
	w := newUnitWorkflow(cfg, st, runner, &output)
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
	if _, err := w.convergeWorkerRuleActivation(checkpoint, result, map[workerRule]struct{}{}); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}

	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	scope, err := os.ReadFile(st.Path(acceptedFixScopeStateFile))
	if err != nil {
		t.Fatalf("production dirty baselineで承認scopeが生成されませんでした: %v", err)
	}
	scopeText := string(scope)
	if !strings.Contains(scopeText, "commentlint") || !strings.Contains(scopeText, "worker_change.go") {
		t.Fatalf("承認scopeにtask diffが含まれていません: %s", scopeText)
	}
	if strings.Contains(scopeText, "preexisting_code.go") {
		t.Fatalf("承認scopeへbaseline以前のdiffが混入しました: %s", scopeText)
	}

	output.Reset()
	if err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	for _, phase := range runner.phases {
		if phase == "worker-explicit-fix" {
			t.Fatalf("承認actionがworkerを再実行しました: %v", runner.phases)
		}
	}
	if len(runner.phases) != 2 || runner.phases[0] != "worker-new-rule-activation-1" || runner.phases[1] != "reviewer-1" {
		t.Fatalf("phases = %v", runner.phases)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s", st.TaskStatus())
	}
}

func TestQualitySurfaceApprovalScopesPostBaselinePreexistingEdits(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "dirty-restop@example.invalid")
	gitScope(t, repo, "config", "user.name", "dirty-restop-test")
	writeScopeFile(t, repo, "glm-worker/go.mod", "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n")
	writeScopeFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 0\n")
	writeScopeFile(t, repo, "preexisting_code.go", "package sample\n\nvar retained = 1\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")

	writeScopeFile(t, repo, "preexisting_code.go", "package sample\n\nvar retained = 1\nvar recoveredImplementation = 2\n")

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
		RepoHash:              strings.Repeat("e", 64),
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
	pinRepositoryHarnessActiveT(t, st)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	baseline, err := captureQualitySurfaceDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, baseline); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, ""); err != nil {
		t.Fatal(err)
	}

	writeScopeFile(t, repo, "commentlint", "#!/bin/sh\nexit 1\n")
	writeScopeFile(t, repo, "preexisting_code.go", "package sample\n\nvar retained = 1\nvar recoveredImplementation = 2\nvar recoveredTaskEdit = 3\n")

	runner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("rules applied")},
		{structured: passPacket()},
	}}
	var output bytes.Buffer
	w := newUnitWorkflow(cfg, st, runner, &output)
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
	if _, err := w.convergeWorkerRuleActivation(checkpoint, result, map[workerRule]struct{}{}); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}

	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	scope, err := os.ReadFile(st.Path(acceptedFixScopeStateFile))
	if err != nil {
		t.Fatalf("pre-existing fileへのtask追記があってもscopeが生成されません: %v", err)
	}
	scopeText := string(scope)
	if !strings.Contains(scopeText, "recoveredTaskEdit") {
		t.Fatalf("承認scopeにpre-existing fileへのtask追記が含まれていません: %s", scopeText)
	}
	if strings.Contains(scopeText, "recoveredImplementation") {
		t.Fatalf("承認scopeへbaseline以前のdiffが混入しました: %s", scopeText)
	}

	output.Reset()
	if err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatalf("pre-existing fileへのtask追記が承認できない: %v", err)
	}
	if len(runner.phases) != 2 || runner.phases[0] != "worker-new-rule-activation-1" || runner.phases[1] != "reviewer-1" {
		t.Fatalf("phases = %v", runner.phases)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s", st.TaskStatus())
	}
}

func vanishedUntrackedQualitySurfaceFixture(t *testing.T) (*Workflow, *state.StateStore, *scriptedRunner, *bytes.Buffer, string) {
	t.Helper()
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "vanished-surface@example.invalid")
	gitScope(t, repo, "config", "user.name", "vanished-surface-test")
	writeScopeFile(t, repo, "glm-worker/go.mod", "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n")
	writeScopeFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
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
	pinRepositoryHarnessActiveT(t, st)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}

	probePath := filepath.Join(repo, "glm-worker", "internal", "harnesslint", "zzz_probe_test.go")
	if err := os.MkdirAll(filepath.Dir(probePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(probePath, []byte("package harnesslint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseline, err := captureQualitySurfaceDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, baseline); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, ""); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(probePath); err != nil {
		t.Fatal(err)
	}

	runner := &scriptedRunner{steps: []runnerStep{
		{structured: passPacket()},
	}}
	var output bytes.Buffer
	w := newUnitWorkflow(cfg, st, runner, &output)
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
	if _, err := w.convergeWorkerRuleActivation(checkpoint, result, map[workerRule]struct{}{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.phases) != 0 {
		t.Fatalf("fixture must stop before dispatch: %v", runner.phases)
	}
	return w, st, runner, &output, baseline
}

func TestVanishedUntrackedQualitySurfaceStopsWithOwnerLocatorAndDigestEvidence(t *testing.T) {
	_, st, _, output, baseline := vanishedUntrackedQualitySurfaceFixture(t)

	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.QualitySurfaceApprovalPending || saved.CompletedResult == nil ||
		saved.StopGitSnapshot == nil || saved.StopDirtyFiles == nil {
		t.Fatalf("approval checkpoint retention = %#v", saved)
	}

	pkt := lastPacketFromOutput(t, output.String())
	if pkt.Status != packet.StatusNeedsSolReview {
		t.Fatalf("emitted status = %s", pkt.Status)
	}
	targets := strings.Join(pkt.Targets, "\n")
	if strings.Contains(targets, "zzz_probe_test.go") {
		t.Fatalf("vanished untracked path must not be fabricated as an approval target: %v", pkt.Targets)
	}
	if !strings.Contains(targets, "glm-worker/internal/workflow/quality_gate.go") {
		t.Fatalf("guard owner locator is missing from the approval targets: %v", pkt.Targets)
	}
	if !strings.Contains(pkt.Issues, "changed pathの外で変化") || !strings.Contains(pkt.Issues, baseline) {
		t.Fatalf("digest deletion evidence is missing: %s", pkt.Issues)
	}

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionApproveSurface {
		t.Fatalf("unapproved removal must keep the finite approval handoff: %#v", plan)
	}
}

func TestVanishedUntrackedQualitySurfaceApprovalAdvancesBaselineAndCompletes(t *testing.T) {
	w, st, runner, output, _ := vanishedUntrackedQualitySurfaceFixture(t)

	output.Reset()
	if err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatalf("authorizing the removal must resume the retained result: %v phases=%v", err, runner.phases)
	}
	if len(runner.phases) != 1 || runner.phases[0] != "reviewer-1" {
		t.Fatalf("phases = %v", runner.phases)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	approved, err := captureQualitySurfaceDigest(w.config.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(st.ReadOr(qualitySurfaceBaselineStateKey, "")) != approved {
		t.Fatalf("approved removal must advance the quality surface baseline to the current digest")
	}
}

func TestQualitySurfaceApprovalTargetFailureKeepsCheckpointHandoff(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "target-failure@example.invalid")
	gitScope(t, repo, "config", "user.name", "target-failure-test")
	writeScopeFile(t, repo, "glm-worker/go.mod", "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n")
	writeScopeFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
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
		RepoHash:              strings.Repeat("1", 64),
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
	pinRepositoryHarnessActiveT(t, st)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, "stale-baseline"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, ""); err != nil {
		t.Fatal(err)
	}

	runner := &scriptedRunner{}
	var output bytes.Buffer
	w := newUnitWorkflow(cfg, st, runner, &output)
	w.captureQualitySurface = func(string) (string, error) { return "drifted", nil }
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass"}, nil
	}
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return nil, errors.New("changed paths unavailable")
	}

	checkpoint := state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole,
		Model: "worker", Effort: "low", Prompt: "work", OriginalPrompt: "work", Request: "task",
	}
	result := packet.Result{
		Status: packet.StatusImplemented, Risk: packet.RiskLow, Summary: "implemented",
		RequirementCoverage: "covered", Tests: "pass", Unverified: "none",
	}
	if _, err := w.convergeWorkerRuleActivation(checkpoint, result, map[workerRule]struct{}{}); err != nil {
		t.Fatalf("target derivation failure must not strand the completed result: %v", err)
	}
	if len(runner.phases) != 0 {
		t.Fatalf("target failure must not dispatch model work: %v", runner.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.QualitySurfaceApprovalPending || saved.CompletedResult == nil {
		t.Fatalf("target failure checkpoint = %#v", saved)
	}
	pkt := lastPacketFromOutput(t, output.String())
	if pkt.Status != packet.StatusNeedsSolReview {
		t.Fatalf("emitted status = %s", pkt.Status)
	}
	targets := strings.Join(pkt.Targets, "\n")
	if !strings.Contains(targets, "glm-worker/internal/workflow/quality_gate.go") {
		t.Fatalf("guard owner locator is missing from the fallback targets: %v", pkt.Targets)
	}
	if !strings.Contains(pkt.Issues, "changed paths unavailable") {
		t.Fatalf("target failure reason is not preserved: %s", pkt.Issues)
	}
}
