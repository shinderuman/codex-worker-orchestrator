package workflow

import (
	"bytes"
	"errors"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityFixerMutationKeepsSubsequentModelCallAdmitted(t *testing.T) {
	t.Parallel()
	repo, cfg, store, st, path, formatted := newQualityMutationFixture(t)
	activated, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("initial")},
		{structured: implementedPacket("fixed")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	var out bytes.Buffer
	w := NewWorkflow(cfg, st, r, &out)
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	fixed := false
	w.qualityGate = func(root string) (harnesslint.Report, error) {
		if fixed {
			return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
		}
		fixed = true
		before, err := controller.CaptureWorkspaceSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return harnesslint.Report{}, err
		}
		after, err := controller.CaptureWorkspaceSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		report := qualityFixReportForSnapshots(workspaceRuntimeSnapshot(before), workspaceRuntimeSnapshot(after))
		report.Status = "fail"
		report.Violations = append(report.Violations, harnesslint.Violation{Rule: "fixture", Path: "fixture.go", Line: 1, Column: 1, Message: "still invalid"})
		return report, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if !fixed {
		t.Fatalf("quality gate did not run its machine fixer: %s", out.String())
	}
	if len(r.prompts) != 3 {
		t.Fatalf("auto-fix model call after the machine fixer did not run: prompts=%d", len(r.prompts))
	}
	if err := admitQualityMutationFixtureSnapshot(t, store, repo); err != nil {
		t.Fatalf("quality-fixer change was not attributed to the live lease: %v", err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.LiveLeaseID == "" || head.ControllerGeneration <= activated.Head.ControllerGeneration {
		t.Fatalf("quality mutation did not advance the controller lease: %#v", head)
	}
	if !st.Exists(qualityFixEvidenceStateKey) {
		t.Fatal("machine quality fixer evidence was not persisted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(formatted) {
		t.Fatalf("review target is not the fixer postimage: %q", got)
	}
	workerEnd, err := st.LoadWorkerEndSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	current, err := state.CaptureGitSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if workerEnd.WorktreeDigest != current.WorktreeDigest {
		t.Fatalf("worker-end snapshot was not synced to the fixer postimage: %#v", workerEnd)
	}
}

func TestQualityFixerProvenanceMismatchFailsClosedWithoutMutation(t *testing.T) {
	t.Parallel()
	repo, cfg, store, st, path, formatted := newQualityMutationFixture(t)
	if _, err := controller.Activate(cfg); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
	var out bytes.Buffer
	w := NewWorkflow(cfg, st, r, &out)
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	w.qualityGate = func(root string) (harnesslint.Report, error) {
		before, err := controller.CaptureWorkspaceSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return harnesslint.Report{}, err
		}
		report := qualityFixReportForSnapshots(workspaceRuntimeSnapshot(before), state.GitSnapshot{
			Head:           before.Head,
			IndexDigest:    before.IndexDigest,
			WorktreeDigest: "fabricated-output-digest",
		})
		report.Status = "pass"
		return report, nil
	}

	err := w.ExecuteNewTask("request")
	var stopped *QualityGateRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("provenance mismatch must stop in quality-gate recovery: %v output=%s", err, out.String())
	}
	if st.TaskStatus() != state.TaskStatusQualityGateRecoverable {
		t.Fatalf("provenance mismatch must stop in quality-gate recovery: %s output=%s", st.TaskStatus(), out.String())
	}
	if len(r.prompts) != 1 {
		t.Fatalf("unproven machine fix dispatched further model calls: %d", len(r.prompts))
	}
	if st.Exists(qualityFixEvidenceStateKey) {
		t.Fatal("unproven machine fix persisted evidence")
	}
	if err := admitQualityMutationFixtureSnapshot(t, store, repo); err == nil {
		t.Fatal("unproven machine fix was attributed to the live lease")
	}
}

func TestQualityGateWithoutFixRecordsNoMutationEvidence(t *testing.T) {
	t.Parallel()
	repo, cfg, store, st, _, _ := newQualityMutationFixture(t)
	if _, err := controller.Activate(cfg); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("initial")},
		{structured: passPacket()},
	}}
	var out bytes.Buffer
	w := NewWorkflow(cfg, st, r, &out)
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("worker and reviewer calls: %d output=%s", len(r.prompts), out.String())
	}
	if st.Exists(qualityFixEvidenceStateKey) {
		t.Fatal("fixed=0 gate persisted fixer evidence")
	}
	if err := admitQualityMutationFixtureSnapshot(t, store, repo); err != nil {
		t.Fatalf("fixed=0 gate disturbed the live lease: %v", err)
	}
}

func TestQualityGateNoFixUnclaimedChangeFailsClosed(t *testing.T) {
	t.Parallel()
	repo, cfg, store, st, path, formatted := newQualityMutationFixture(t)
	if _, err := controller.Activate(cfg); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
	var out bytes.Buffer
	w := NewWorkflow(cfg, st, r, &out)
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	w.qualityGate = func(string) (harnesslint.Report, error) {
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return harnesslint.Report{}, err
		}
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}

	err := w.ExecuteNewTask("request")
	var stopped *QualityGateRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("unclaimed fixed=0 change must stop in quality-gate recovery: %v output=%s", err, out.String())
	}
	if !strings.Contains(stopped.Failure, "without reporting fixes") {
		t.Fatalf("stop failure must name the unclaimed change: %q", stopped.Failure)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("unclaimed change dispatched further model calls: %d", len(r.prompts))
	}
	if st.Exists(qualityFixEvidenceStateKey) {
		t.Fatal("unclaimed change persisted fixer evidence")
	}
	if err := admitQualityMutationFixtureSnapshot(t, store, repo); err == nil {
		t.Fatal("unclaimed change was attributed to the live lease")
	}
}

func TestQualityGateErrorAfterWorkspaceDriftFailsClosed(t *testing.T) {
	t.Parallel()
	repo, cfg, store, st, path, formatted := newQualityMutationFixture(t)
	if _, err := controller.Activate(cfg); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
	var out bytes.Buffer
	w := NewWorkflow(cfg, st, r, &out)
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	w.qualityGate = func(string) (harnesslint.Report, error) {
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return harnesslint.Report{}, err
		}
		return harnesslint.Report{}, errors.New("tool crashed mid-fix")
	}

	err := w.ExecuteNewTask("request")
	var stopped *QualityGateRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("mid-failure drift must stop in quality-gate recovery: %v output=%s", err, out.String())
	}
	if !strings.Contains(stopped.Failure, "failed after changing the workspace") {
		t.Fatalf("stop failure must preserve the mid-failure drift evidence: %q", stopped.Failure)
	}
	if !strings.Contains(stopped.Failure, "tool crashed mid-fix") {
		t.Fatalf("stop failure must preserve the original gate error: %q", stopped.Failure)
	}
	if err := admitQualityMutationFixtureSnapshot(t, store, repo); err == nil {
		t.Fatal("mid-failure drift was attributed to the live lease")
	}
}

func TestRealHarnesslintIsolatedFixerFeedsLiveLeaseMutation(t *testing.T) {
	t.Parallel()
	repo, cfg, store, st, path, formatted := newQualityMutationFixture(t)
	activated, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := NewWorkflow(cfg, st, &scriptedRunner{}, &out)
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot

	report, err := w.runQualityGate()
	if err != nil {
		t.Fatalf("real harnesslint gate failed: %v output=%s", err, out.String())
	}
	if report.Fixed == 0 {
		t.Fatalf("real isolated fixer did not fix the unformatted fixture: %#v", report)
	}
	if report.FixEvidence == nil || report.FixEvidence.Method != harnesslint.FixProvenanceIsolatedPostimageV1 {
		t.Fatalf("real fixer provenance is missing: %#v", report.FixEvidence)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(formatted) {
		t.Fatalf("real fixer postimage was not applied: %q", got)
	}
	if !st.Exists(qualityFixEvidenceStateKey) {
		t.Fatal("real fixer evidence was not persisted")
	}
	if err := admitQualityMutationFixtureSnapshot(t, store, repo); err != nil {
		t.Fatalf("real fixer postimage was not attributed to the live lease: %v", err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.ControllerGeneration <= activated.Head.ControllerGeneration {
		t.Fatalf("real fixer mutation did not advance the controller lease: %#v", head)
	}

	second, err := w.runQualityGate()
	if err != nil {
		t.Fatalf("second real gate run failed: %v", err)
	}
	if second.Fixed != 0 {
		t.Fatalf("real fixer is not idempotent: %#v", second)
	}
	if err := admitQualityMutationFixtureSnapshot(t, store, repo); err != nil {
		t.Fatalf("fixed=0 real gate disturbed the live lease: %v", err)
	}
}

func TestQualityMutationPartialFailureSettlesThroughTypedOwner(t *testing.T) {
	t.Parallel()
	repo, cfg, store, st, path, formatted := newQualityMutationFixture(t)
	activated, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := NewWorkflow(cfg, st, &scriptedRunner{}, &out)
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot

	guard, err := w.admitQualityGateMutation()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := controller.CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	report := qualityFixReportForSnapshots(workspaceRuntimeSnapshot(guard.before), workspaceRuntimeSnapshot(after))
	provenance, err := w.persistQualityFixEvidence(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PreserveQualityMutationOutput(guard.admission, after, provenance); err != nil {
		t.Fatal(err)
	}

	if _, err := store.AdmitMutation(guard.admission.MutationAuthority(), guard.admission.Workspace, after); err == nil {
		t.Fatal("post-crash workspace must not match the live lease before settlement")
	}

	resumed := NewWorkflow(cfg, st, &scriptedRunner{}, &out)
	resumed.captureSnapshot = state.CaptureGitSnapshot
	resumed.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	resumed.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	second, err := resumed.runQualityGate()
	if err != nil {
		t.Fatalf("settlement resume failed: %v", err)
	}
	if second.Fixed != 0 {
		t.Fatalf("settlement resume must observe a clean rerun: %#v", second)
	}

	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.ControllerGeneration != activated.Head.ControllerGeneration+1 {
		t.Fatalf("settlement advanced the generation to %d, want exactly %d", head.ControllerGeneration, activated.Head.ControllerGeneration+1)
	}
	if err := admitQualityMutationFixtureSnapshot(t, store, repo); err != nil {
		t.Fatalf("subsequent model-call admission must accept the settled postimage: %v", err)
	}
	if !st.Exists(qualityFixEvidenceStateKey) {
		t.Fatal("settlement must keep the preserved provenance evidence")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(formatted) {
		t.Fatalf("settled postimage must remain applied: %q", got)
	}
	if _, _, err := controller.WorkflowExecutionTask(cfg); err != nil {
		t.Fatalf("startup entry must heal after settlement: %v", err)
	}
}

func admitQualityMutationFixtureSnapshot(t *testing.T, store *controller.Store, repo string) error {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil {
		return err
	}
	authority, err := controller.MutationAuthorityFromHead(head)
	if err != nil {
		return err
	}
	workspace, err := controller.ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		return err
	}
	snapshot, err := controller.CaptureWorkspaceSnapshot(repo)
	if err != nil {
		return err
	}
	_, err = store.AdmitMutation(authority, workspace, snapshot)
	return err
}

func newQualityMutationFixture(t *testing.T) (string, config.AppConfig, *controller.Store, *state.StateStore, string, []byte) {
	t.Helper()
	repo := newControllerGuardRepo(t)
	trackRepositoryHarnessMarker(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md"), []byte("# root\n\n## Contract\n\ncontroller guard task\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeQualityMutationFixtureFile(t, repo, "commentlint", "#!/bin/sh\nexit 0\n", 0o755)
	copyQualityMutationFixtureFile(t, repo, "quality-tools.yml")
	copyQualityMutationFixtureFile(t, repo, ".golangci.yml")
	if err := os.MkdirAll(filepath.Join(repo, "glm-worker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "glm-worker", "go.mod"), []byte("module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "glm-worker", "pkg.go"), []byte("package glmworker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, "fixture.go")
	original := []byte("package fixture\n\nfunc f( ){ }\n")
	formatted, err := format.Source(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGuardGit(t, repo, "add", ".")
	runControllerGuardGit(t, repo, "commit", "-q", "-m", "quality mutation fixture")
	cfg := controllerGuardConfig(repo, filepath.Join(t.TempDir(), "state", "sessions"))
	cfg.WorkerModel = "opus"
	cfg.ReviewerModel = "haiku"
	cfg.HighRiskReviewerModel = "sonnet"
	cfg.RoutineEffort = "high"
	cfg.MaxAutoFixRounds = 2
	cfg.TelemetryContent = true
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	pinRepositoryHarnessActiveT(t, st)
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return repo, cfg, store, st, path, formatted
}

func writeQualityMutationFixtureFile(t *testing.T, repo, name, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func copyQualityMutationFixtureFile(t *testing.T, repo, name string) {
	t.Helper()
	sourceRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(sourceRoot, name))
	if err != nil {
		t.Fatalf("read fixture source %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(repo, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
