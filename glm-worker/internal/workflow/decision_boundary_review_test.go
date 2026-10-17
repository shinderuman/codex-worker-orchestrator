package workflow

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDecisionBoundaryReviewContextOnlyForUnresolvedAxes(t *testing.T) {
	t.Parallel()
	authority := semanticDecisionAuthority{fixed: map[semanticDecisionAxis]string{
		decisionAxisResponsibility:      "existing telemetry responsibility",
		decisionAxisDependencyDirection: "workflow to telemetry",
		decisionAxisCompatibility:       "preserve compatibility",
		decisionAxisValidationError:     "existing validation semantics",
	}}
	block := decisionBoundaryReviewContextBlock("IMPLEMENTATION_TASKS/Task011.md", authority)
	if !strings.Contains(block, "UNRESOLVED_AXES: public-surface") ||
		!strings.Contains(block, "actual git diff") ||
		!strings.Contains(block, "NEEDS_SOL_DECISION") {
		t.Fatalf("review boundary block = %s", block)
	}

	authority.fixed[decisionAxisPublicSurface] = "no new public surface"
	if got := decisionBoundaryReviewContextBlock("IMPLEMENTATION_TASKS/Task011.md", authority); got != "" {
		t.Fatalf("all-fixed authority must not add review boundary context: %s", got)
	}
}

func TestReviewerDecisionBoundaryRejectsAllFixedAuthority(t *testing.T) {
	t.Parallel()
	st := newStateStoreT(t)
	r := &scriptedRunner{}
	w := newWorkflowT(t, st, r)
	task := writeDecisionBoundaryTask(t, w.config.RepoRoot, true)
	if err := st.Write(activeTaskStateKey, task); err != nil {
		t.Fatal(err)
	}
	if err := w.validateReviewerDecisionBoundary(); err == nil {
		t.Fatal("reviewer decision must be rejected when every axis is fixed")
	}

	task = writeDecisionBoundaryTask(t, w.config.RepoRoot, false)
	if err := st.Write(activeTaskStateKey, task); err != nil {
		t.Fatal(err)
	}
	if err := w.validateReviewerDecisionBoundary(); err != nil {
		t.Fatalf("unresolved public-surface must allow reviewer decision verdict: %v", err)
	}
}

func TestTask011PublicSurfaceChoiceRoutesReviewerDecisionWithoutExtraCall(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "test")

	task := writeDecisionBoundaryTask(t, repo, false)
	timeline := filepath.Join(repo, "internal", "timeline", "timeline.go")
	if err := os.MkdirAll(filepath.Dir(timeline), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(timeline, []byte("package timeline\n\nfunc fields() []string { return []string{\"status\"} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-qm", "baseline")
	baseline := strings.TrimSpace(gitRun(t, repo, "rev-parse", "HEAD"))

	if err := os.WriteFile(timeline, []byte("package timeline\n\nfunc fields() []string { return []string{\"status\", \"operations\", \"operation_totals\"} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := newStateStoreT(t)
	writeCleanTaskBaselineState(t, st, baseline)
	if err := st.Write(activeTaskStateKey, task); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	r := &scriptedRunner{}
	w := newWorkflowTWithOutput(t, st, r, &output)
	w.config.RepoRoot = repo
	w.temp = t.TempDir()

	workerResult := packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "operation telemetry implemented",
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	}
	paths, err := collectTaskChangedPaths(repo, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "internal/timeline/timeline.go" {
		t.Fatalf("actual changed paths = %v", paths)
	}
	boundary, err := w.reviewerDecisionBoundaryContext(task)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("boundary evidence collection must not add a model call: phases=%v", r.phases)
	}
	if !strings.Contains(boundary, solDecisionBoundaryReviewMarker) ||
		!strings.Contains(boundary, "UNRESOLVED_AXES: public-surface") {
		t.Fatalf("reviewer boundary context = %s", boundary)
	}

	reviewResult := packet.Result{
		Status:          packet.StatusNeedsSolDecision,
		Risk:            packet.RiskHigh,
		Decision:        "public-surface semantics are unresolved",
		Evidence:        "timeline diff adds operations and operation_totals",
		Options:         "keep fields internal or expose them",
		Recommendation:  "ask Sol before accepting the public contract",
		TestObligations: "verify the selected public contract",
		Targets:         []string{"internal/timeline/timeline.go"},
	}
	if err := w.validateReviewerDecisionBoundary(); err != nil {
		t.Fatal(err)
	}
	if err := w.handleReviewResult("add operation telemetry", workerResult, reviewResult, 1, 0); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("decision route status=%s pending=%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if !strings.Contains(output.String(), `"status":"NEEDS_SOL_DECISION"`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestReviewerDecisionBoundaryUsesControllerTaskWhenLegacyStateEmpty(t *testing.T) {
	t.Parallel()
	cfg, st := newWorkflowGuardFixture(t, t.TempDir())
	root := filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_TASKS", "root.md")
	authority := "# root\n\n## Contract\n\nroot\n\n## Dependencies\n\nnone\n\n## Sol decision authority\n" +
		"- responsibility: existing telemetry responsibility\n" +
		"- dependency-direction: workflow to telemetry\n" +
		"- compatibility: preserve compatibility\n" +
		"- validation-error-semantics: existing validation semantics\n"
	if err := os.WriteFile(root, []byte(authority), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, cfg.RepoRoot, "add", ".")
	gitRun(t, cfg.RepoRoot, "commit", "-qm", "decision authority")
	admission, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          admission.Attempt.AttemptID,
		TaskPath:           admission.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}

	w := NewWorkflow(cfg, st, nil, io.Discard)
	if err := w.validateReviewerDecisionBoundary(); err != nil {
		t.Fatalf("controller task decision boundary must survive empty legacy active-task state: %v", err)
	}
}

func TestReviewerDecisionBoundaryRejectionSavesResumableStop(t *testing.T) {
	t.Parallel()
	cfg, st := newWorkflowGuardFixture(t, t.TempDir())
	w := NewWorkflow(cfg, st, nil, io.Discard)
	task := writeDecisionBoundaryTask(t, cfg.RepoRoot, true)
	if err := st.Write(activeTaskStateKey, task); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	dirtyPath := filepath.Join(cfg.RepoRoot, "worker-notes.md")
	if err := os.WriteFile(dirtyPath, []byte("uncommitted worker output\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "review-output")
	if err := os.WriteFile(outputPath, []byte("structured output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{
		Stage:   state.ResumeStageReview,
		Phase:   "reviewer-1",
		Role:    state.ReviewerRole,
		Model:   "reviewer",
		Prompt:  "review",
		Request: "request",
	}

	boundaryErr := w.validateReviewerDecisionBoundary()
	if boundaryErr == nil {
		t.Fatal("all-fixed authority must reject the reviewer decision")
	}
	_, callErr := w.handleInvalidModelResult(checkpoint, outputPath, modelCallExecution{}, boundaryErr)
	var interrupted *runner.InterruptedCallError
	if !errors.As(callErr, &interrupted) {
		t.Fatalf("decision-boundary rejection must surface the interrupted-call error: %v", callErr)
	}
	if got := st.TaskStatus(); got != state.TaskStatusInterrupted {
		t.Fatalf("rejection left runtime status %q without a durable stop", got)
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil || saved.StopKind != state.ResumeStopInterrupted || saved.Phase != "reviewer-1" || saved.Role != state.ReviewerRole || saved.Request != "request" {
		t.Fatalf("durable reviewer stop checkpoint = %#v %v", saved, err)
	}
	if saved.StopGitSnapshot == nil || saved.StopGitSnapshot.Head == "" {
		t.Fatalf("reviewer stop lost the repository boundary: %#v", saved.StopGitSnapshot)
	}
	if len(saved.StopDirtyFiles) == 0 {
		t.Fatal("reviewer stop lost the dirty-file retention for uncommitted work")
	}
	foundDirty := false
	for _, file := range saved.StopDirtyFiles {
		if file.Path == "worker-notes.md" && file.WorktreeSHA != "" {
			foundDirty = true
		}
	}
	if !foundDirty {
		t.Fatalf("dirty retention missed the uncommitted worktree file: %#v", saved.StopDirtyFiles)
	}

	if err := st.BeginResume(saved); err != nil {
		t.Fatalf("canonical resume rejected the durable reviewer stop: %v", err)
	}
	if got, err := st.TaskID(); err != nil || got != taskID || st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("resume admission changed the task identity or status: task=%s status=%s err=%v", got, st.TaskStatus(), err)
	}
}

func writeDecisionBoundaryTask(t *testing.T, repo string, fixPublicSurface bool) string {
	t.Helper()
	rel := filepath.ToSlash(filepath.Join("IMPLEMENTATION_TASKS", "Task011.md"))
	path := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"# Task011",
		"",
		"## Sol decision authority",
		"- responsibility: existing telemetry responsibility",
		"- dependency-direction: workflow to telemetry",
		"- compatibility: preserve compatibility",
		"- validation-error-semantics: existing validation semantics",
	}
	if fixPublicSurface {
		lines = append(lines, "- public-surface: no new public surface")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, data)
	}
	return string(data)
}
