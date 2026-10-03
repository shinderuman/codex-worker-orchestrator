package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/authoritybootstrapcmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type canonicalWorkflowRunner struct {
	t       *testing.T
	cfg     config.AppConfig
	task    string
	path    string
	prompts []string
}

func (*canonicalWorkflowRunner) Probe(string) (runner.ProbeResult, error) {
	return runner.ProbeResult{Response: runner.ProbeSentinel}, nil
}

func (r *canonicalWorkflowRunner) Run(role state.SessionRole, phase, _ string, readOnly bool, _ string, prompt, _ string) (runner.RunResult, error) {
	r.prompts = append(r.prompts, prompt)
	if role != state.WorkerRole || readOnly {
		r.t.Fatalf("unexpected model route: %s read-only=%v", role, readOnly)
	}
	authority, err := authoritybootstrapcmd.BuildFromRoot(r.cfg.RepoRoot, "active", "")
	if err != nil || authority.ActiveTask != r.task {
		r.t.Fatalf("model received wrong execution authority: %#v %v", authority, err)
	}
	if r.path != "" {
		writeAppTestFile(r.t, r.cfg.RepoRoot, r.path, "preserved "+r.task+"\n")
	}
	return runner.RunResult{SessionID: "interrupted-test-session"}, &runner.InterruptedCallError{Phase: phase}
}

func runCanonicalWorkflow(t *testing.T, cfg config.AppConfig, mode CommandMode, request, task, path string) *canonicalWorkflowRunner {
	t.Helper()
	scripted := &canonicalWorkflowRunner{t: t, task: task, path: path}
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		scripted.cfg = actual
		return scripted
	}
	var output bytes.Buffer
	err := Execute(Command{Mode: mode, Payload: request}, cfg, factory, &output, io.Discard)
	var interrupted *runner.InterruptedCallError
	if !errors.As(err, &interrupted) || len(scripted.prompts) != 1 {
		t.Fatalf("canonical workflow did not reach the scripted call: %v calls=%d output=%s", err, len(scripted.prompts), output.String())
	}
	return scripted
}

func canonicalWorkflowAdmission(t *testing.T, cfg config.AppConfig, store *controller.Store) controller.Admission {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := controller.MutationAuthorityFromHead(head)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := controller.ResolveWorkspaceIdentity(cfg.RepoRoot, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := store.AdmitMutation(authority, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

func canonicalWorkflowEvidence(t *testing.T, store *controller.Store, admission controller.Admission, policy controller.PublicationPolicy) []controller.EvidenceObjectRef {
	t.Helper()
	tree, err := store.PreviewCandidateTree(admission, policy)
	if err != nil {
		t.Fatal(err)
	}
	var refs []controller.EvidenceObjectRef
	for _, kind := range []string{"review", "validation"} {
		artifact, err := store.PutEvidenceObject(kind, "text/plain", kind+":"+admission.Snapshot.ID, true, []byte("scripted integration evidence"))
		if err != nil {
			t.Fatal(err)
		}
		ref, err := store.StoreCandidateEvidence(controller.CandidateEvidence{SchemaVersion: 1, RepositoryIdentity: store.Identity().LineageID, AttemptID: admission.Attempt.AttemptID, SnapshotID: admission.Snapshot.ID, BaseOID: admission.Head.IntegrationTip, TreeOID: tree, Kind: kind, Result: "pass", Artifact: artifact})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}
	return refs
}

func newCanonicalWorkflowCorpus(t *testing.T) (config.AppConfig, string, string, controller.PublicationPolicy) {
	t.Helper()
	cfg := newCanonicalAppConfig(t)
	stateHome := t.TempDir()
	cfg.StateBase = filepath.Join(stateHome, "sessions")
	cfg.WorktreeBase = filepath.Join(stateHome, "worktrees")
	cfg.EscalatedEffort = "high"
	t.Setenv("GLM_WORKER_HOME", stateHome)
	rootTask := "IMPLEMENTATION_TASKS/root.md"
	blockerTask := "IMPLEMENTATION_TASKS/f11.md"
	writeAppTestFile(t, cfg.RepoRoot, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_RULES.md", "integration rules\n")
	writeAppTestFile(t, cfg.RepoRoot, "glm-worker/go.mod", "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n")
	stubQualityPreflight(t, func(string) error { return nil })
	writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `"+rootTask+"`\n\n## NEXT\n\n- `"+blockerTask+"`\n")
	writeAppTestFile(t, cfg.RepoRoot, rootTask, "# root\n\n## Contract\n\n606 work\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\n- `"+blockerTask+"`\n")
	writeAppTestFile(t, cfg.RepoRoot, blockerTask, "# blocker\n\n## Contract\n\nF11 work\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\nnone\n")
	runControllerActivationGit(t, cfg.RepoRoot, "config", "user.name", "Canonical Workflow Test")
	runControllerActivationGit(t, cfg.RepoRoot, "config", "user.email", "canonical@example.invalid")
	runControllerActivationGit(t, cfg.RepoRoot, "add", ".")
	runControllerActivationGit(t, cfg.RepoRoot, "commit", "-q", "-m", "canonical workflow corpus")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runControllerActivationGit(t, cfg.RepoRoot, "init", "--bare", remote)
	runControllerActivationGit(t, cfg.RepoRoot, "remote", "add", "origin", remote)
	runControllerActivationGit(t, cfg.RepoRoot, "push", "-q", "origin", "HEAD:refs/heads/main")
	localRef, err := exec.Command("git", "-C", cfg.RepoRoot, "symbolic-ref", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	policy := controller.PublicationPolicy{Remote: "origin", RemoteRef: "refs/heads/main", LocalRef: strings.TrimSpace(string(localRef))}
	return cfg, rootTask, blockerTask, policy
}

func TestCanonicalWorkflow606F11DispatchAndRootResume(t *testing.T) {
	cfg, rootTask, blockerTask, policy := newCanonicalWorkflowCorpus(t)
	binary, err := buildMultiRepoWorkerBinary(t)
	if err != nil {
		t.Fatal(err)
	}
	rootRunner := runCanonicalWorkflow(t, cfg, ModeNewTask, "606 original request", rootTask, "606-result.txt")
	cfg = rootRunner.cfg
	rootState := state.AttachStateStore(cfg)
	rootTaskID := rootState.ReadOr("task.id", "")
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := canonicalWorkflowAdmission(t, cfg, store)
	episode := planCanonicalWorkflowBlocker(t, store, source, blockerTask)
	suspended := canonicalCLIExecution(t, binary, cfg, controllerExecutionCommand{Action: "suspend", ExpectedGeneration: source.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	lane := canonicalCLIExecution(t, binary, cfg, controllerExecutionCommand{Action: "materialize", ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	blockerCfg := cfg
	blockerCfg.RepoRoot = lane.Admission.Workspace.Root
	blockerRunner := runCanonicalWorkflow(t, blockerCfg, ModeNewTask, "F11 request", blockerTask, "f11-result.txt")
	blockerCfg = blockerRunner.cfg
	if blockerCfg.RepoHash == cfg.RepoHash {
		t.Fatal("blocker reused root session")
	}
	blockerState := state.AttachStateStore(blockerCfg)
	handoff := buildParentHandoffWithConfig(blockerCfg, blockerState)
	if !handoff.Consistent || handoff.ParentRequest == nil || !handoff.ParentRequest.TaskAttribution.Matches || handoff.ParentRequest.TaskAttribution.ActiveTask != blockerTask {
		t.Fatalf("blocker handoff followed Plan focus: %#v", handoff)
	}
	blockedSource := canonicalWorkflowAdmission(t, blockerCfg, store)
	accepted := canonicalCLIPublication(t, binary, blockerCfg, controllerPublicationCommand{Action: "accept", ExpectedGeneration: blockedSource.Head.ControllerGeneration, Message: "F11 result", Policy: policy, Evidence: canonicalWorkflowEvidence(t, store, blockedSource, policy)})
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted := canonicalCLIPublication(t, binary, blockerCfg, controllerPublicationCommand{Action: "promote", ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	published := canonicalCLIPublication(t, binary, blockerCfg, controllerPublicationCommand{Action: "publish", ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	retired := canonicalCLIPublication(t, binary, blockerCfg, controllerPublicationCommand{Action: "retire", ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	cleaned := canonicalCLIExecution(t, binary, blockerCfg, controllerExecutionCommand{Action: "cleanup", ExpectedGeneration: retired.Head.ControllerGeneration, WorkspaceID: blockedSource.Workspace.ID, SealRef: &candidate.SealRef})
	assertCanonicalTransitionHandoff(t, cfg, rootState)
	resumed := canonicalCLIExecution(t, binary, cfg, controllerExecutionCommand{Action: "materialize", ExpectedGeneration: cleaned.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: retired.Head.ActiveEpisodeRevision, SuspensionID: suspended.Suspension.SnapshotID})
	rootCfg := cfg
	rootCfg.RepoRoot = resumed.Admission.Workspace.Root
	resumedRunner := runCanonicalWorkflow(t, rootCfg, ModeResume, "", rootTask, "")
	resumedState := state.AttachStateStore(resumedRunner.cfg)
	if resumedRunner.cfg.RepoHash != cfg.RepoHash || resumedState.ReadOr("task.id", "") != rootTaskID {
		t.Fatal("root resume lost original workflow session")
	}
	cp, err := resumedState.LoadResumeCheckpoint()
	if err != nil || cp.Request != "606 original request" || cp.Role != state.WorkerRole {
		t.Fatalf("root resume checkpoint: %#v %v", cp, err)
	}
	for _, path := range []string{"606-result.txt", "f11-result.txt"} {
		if _, err := os.Stat(filepath.Join(rootCfg.RepoRoot, path)); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := store.BuildAttemptEvidenceBundle(candidate.SealRef)
	if err != nil || bundle.EvidenceGraphDigest == "" {
		t.Fatalf("historical F11 Bundle unavailable: %v", err)
	}
	var out bytes.Buffer
	lockPath, err := controller.WorkflowLockPath(rootCfg)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireRepoLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runControllerOperations([]string{"--authority", "controller-execution"}, func() (config.AppConfig, error) { return rootCfg, nil }, strings.NewReader(`{"action":"materialize","expected_generation":0}`), &out)
	_ = lock.Close()
	if err == nil {
		t.Fatal("controller transition raced an active workflow lock")
	}
}

func planCanonicalWorkflowBlocker(t *testing.T, store *controller.Store, source controller.Admission, blockerTask string) *controller.BlockerEpisodeRevision {
	t.Helper()
	project, err := store.LoadProjectSnapshot(source.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	var blocker controller.SemanticTaskRef
	for _, task := range project.Tasks {
		if task.TaskPath == blockerTask {
			blocker = task
		}
	}
	if blocker.Empty() {
		t.Fatal("committed blocker task missing")
	}
	finding, err := store.ObserveFinding(source, controller.FindingObservationInput{Producer: "scripted reviewer", ProofClass: controller.FindingProofUnverified, ProblemKey: "606-f11-dispatch"})
	if err != nil {
		t.Fatal(err)
	}
	disposition, err := store.ResolveFindingWithProjectAuthority(finding.FindingID, controller.FindingDecision{Kind: controller.FindingDecisionIndependentBlocking, TargetTaskRef: &blocker, BlockingBoundary: "606 requires F11"})
	if err != nil {
		t.Fatal(err)
	}
	return disposition.Episode
}

func canonicalCLIExecution(t *testing.T, binary string, cfg config.AppConfig, input controllerExecutionCommand) controller.ExecutionOperationResult {
	t.Helper()
	return canonicalCLIOperation(t, binary, cfg, "controller-execution", input)
}

func canonicalCLIPublication(t *testing.T, binary string, cfg config.AppConfig, input controllerPublicationCommand) controller.ExecutionOperationResult {
	t.Helper()
	return canonicalCLIOperation(t, binary, cfg, "controller-publication", input)
}

func canonicalCLIOperation(t *testing.T, binary string, cfg config.AppConfig, surface string, input any) controller.ExecutionOperationResult {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--authority", surface)
	command.Dir = cfg.RepoRoot
	home := filepath.Dir(cfg.StateBase)
	command.Env = append(os.Environ(), "GLM_WORKER_HOME="+home, "HOME="+home, "CODEX_CONFIG_DIR="+filepath.Join(home, "codex"), "CLAUDE_CONFIG_DIR="+filepath.Join(home, "claude"))
	command.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("source CLI %s failed: %v %s", surface, err, stderr.String())
	}
	var result controller.ExecutionOperationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertCanonicalTransitionHandoff(t *testing.T, cfg config.AppConfig, st *state.StateStore) {
	t.Helper()
	handoff := buildParentHandoffWithConfig(cfg, st)
	if !handoff.Consistent || handoff.Controller == nil || handoff.RequiredAction == nil || *handoff.RequiredAction != "controller-execution" {
		t.Fatalf("quiescent controller handoff: %#v", handoff)
	}
	if containsParentAction(handoff.AllowedActions, "resume") || containsParentAction(handoff.AllowedActions, "unpark") {
		t.Fatalf("noncanonical resume offered: %#v", handoff.AllowedActions)
	}
}
