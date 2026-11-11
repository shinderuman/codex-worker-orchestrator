package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type requestTaskDispatchRunner struct {
	t            *testing.T
	cfg          config.AppConfig
	request      string
	calls        int
	ordinaryPlan bool
	complete     bool
	mutateReview bool
}

func (*requestTaskDispatchRunner) Probe(string) (runner.ProbeResult, error) {
	return runner.ProbeResult{Response: runner.ProbeSentinel}, nil
}

func (r *requestTaskDispatchRunner) Run(role state.SessionRole, phase, _ string, readOnly bool, _ string, prompt, _ string) (runner.RunResult, error) {
	if (!r.complete && role != state.WorkerRole) || (role == state.WorkerRole && readOnly) || (role == state.ReviewerRole && !readOnly) || !strings.Contains(prompt, r.request) {
		r.t.Fatalf("wrong request model dispatch: %s read-only=%v", role, readOnly)
	}
	store, err := controller.Open(r.cfg)
	if err != nil {
		r.t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		r.t.Fatal(err)
	}
	if head.ExecutionTaskRef == nil || head.ExecutionTaskRef.RequestID == "" || head.ExecutionTaskRef.TaskPath != "" || head.LiveLeaseID == "" {
		r.t.Fatalf("missing real request authority: %+v", head)
	}
	if err := store.ValidateRequestTask(*head.ExecutionTaskRef, r.request); err != nil {
		r.t.Fatal(err)
	}
	st := state.AttachStateStore(r.cfg)
	binding, err := st.LoadControllerRuntimeBinding()
	if err != nil || binding.RequestID != head.ExecutionTaskRef.RequestID || binding.TaskPath != "" || binding.AttemptID != head.LiveAttemptID {
		r.t.Fatalf("request runtime binding: %+v %v", binding, err)
	}
	r.calls++
	if role == state.WorkerRole {
		writeAppTestFile(r.t, r.cfg.RepoRoot, "result.txt", fmt.Sprintf("request mutation %d\n", r.calls))
		if r.ordinaryPlan {
			writeAppTestFile(r.t, r.cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", fmt.Sprintf("ordinary data mutation %d\n", r.calls))
		}
	}
	if role == state.ReviewerRole && r.mutateReview {
		writeAppTestFile(r.t, r.cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "ordinary data changed during review\n")
	}
	id, err := state.NewUUID()
	if err != nil {
		r.t.Fatal(err)
	}
	if r.complete {
		body := implementedPacketApp("request mutation completed")
		if role == state.ReviewerRole {
			body = passPacketApp()
		}
		transcript, err := json.Marshal(map[string]string{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "sessionId": id, "role": string(role), "prompt": prompt, "response": body})
		if err != nil {
			r.t.Fatal(err)
		}
		writeAppTestFile(r.t, r.cfg.ClaudeConfigDir, filepath.Join("projects", "request", id+".jsonl"), string(transcript)+"\n")
		return runner.RunResult{SessionID: id, StructuredOutput: []byte(body), Response: body}, nil
	}
	return runner.RunResult{SessionID: id}, &runner.InterruptedCallError{Phase: phase}
}

func TestNormalTaskCLIRequestAuthorityDispatchAndResume(t *testing.T) {
	for _, ordinaryPlan := range []bool{false, true} {
		t.Run(fmt.Sprintf("ordinary-plan-%v", ordinaryPlan), func(t *testing.T) {
			cfg := newRequestTaskAppConfig(t)
			if ordinaryPlan {
				writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "ordinary project data, not a schedule\n")
				runCanonicalGuardGit(t, cfg.RepoRoot, "add", "IMPLEMENTATION_PLAN.local.md")
				runCanonicalGuardGit(t, cfg.RepoRoot, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "ordinary data")
			}
			scripted := &requestTaskDispatchRunner{t: t, request: "create a result for this request", ordinaryPlan: ordinaryPlan}
			factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
				scripted.cfg = actual
				return scripted
			}
			for _, mode := range []CommandMode{ModeNewTask, ModeResume} {
				payload := scripted.request
				if mode == ModeResume {
					payload = ""
				}
				var stdout bytes.Buffer
				err := Execute(Command{Mode: mode, Payload: payload}, cfg, factory, &stdout, io.Discard)
				var interrupted *runner.InterruptedCallError
				if !errors.As(err, &interrupted) {
					t.Fatalf("request did not reach real dispatch: %v output=%s", err, stdout.String())
				}
			}
			if scripted.calls != 2 {
				t.Fatalf("model calls=%d", scripted.calls)
			}
			if _, err := os.Stat(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_TASKS")); !os.IsNotExist(err) {
				t.Fatalf("Task files were generated: %v", err)
			}
			if !ordinaryPlan {
				if _, err := os.Stat(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md")); !os.IsNotExist(err) {
					t.Fatalf("Plan was generated: %v", err)
				}
			}
			store, err := controller.Open(scripted.cfg)
			if err != nil {
				t.Fatal(err)
			}
			head, err := store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if head.ControllerGeneration < 5 {
				t.Fatalf("model mutations were not journaled: %+v", head)
			}
			if ordinaryPlan {
				trees, err := controller.CaptureExecutionTrees(cfg.RepoRoot, head.IntegrationTip, *head.ExecutionTaskRef)
				if err != nil {
					t.Fatal(err)
				}
				content, err := exec.Command("git", "-C", cfg.RepoRoot, "show", trees.WorktreeTree+":IMPLEMENTATION_PLAN.local.md").Output()
				if err != nil || string(content) != "ordinary data mutation 2\n" {
					t.Fatalf("ordinary Plan-named data lost: %q %v", content, err)
				}
			}
			st := state.AttachStateStore(scripted.cfg)
			if err := st.Write("last-request", "tampered request"); err != nil {
				t.Fatal(err)
			}
			err = Execute(Command{Mode: ModeResume}, cfg, factory, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "request differs") || scripted.calls != 2 {
				t.Fatalf("changed runtime request admitted: calls=%d err=%v", scripted.calls, err)
			}
		})
	}
}

func TestNormalTaskRequestReachesReviewAndCanonicalAcceptanceWithoutPlan(t *testing.T) {
	cfg := newRequestTaskAppConfig(t)
	cfg.ClaudeConfigDir = t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	runCanonicalGuardGit(t, cfg.RepoRoot, "init", "--bare", remote)
	runCanonicalGuardGit(t, cfg.RepoRoot, "remote", "add", "origin", remote)
	branch, err := exec.Command("git", "-C", cfg.RepoRoot, "symbolic-ref", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	ref := strings.TrimSpace(string(branch))
	runCanonicalGuardGit(t, cfg.RepoRoot, "push", "origin", "HEAD:"+ref)
	scripted := &requestTaskDispatchRunner{t: t, request: "create a result without project Task files", complete: true}
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		scripted.cfg = actual
		return scripted
	}
	var output bytes.Buffer
	if err := Execute(Command{Mode: ModeNewTask, Payload: scripted.request}, cfg, factory, &output, io.Discard); err != nil {
		t.Fatalf("request pipeline: %v output=%s", err, output.String())
	}
	st := state.AttachStateStore(scripted.cfg)
	if st.TaskStatus() != state.TaskStatusComplete || st.OpenParentReviewLabel() != "PASS" || scripted.calls != 2 {
		t.Fatalf("request review: status=%s review=%s calls=%d", st.TaskStatus(), st.OpenParentReviewLabel(), scripted.calls)
	}
	if err := Execute(Command{Mode: ModeAccept}, cfg, factory, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("parent acceptance status=%s", st.TaskStatus())
	}
	store, err := controller.Open(scripted.cfg)
	if err != nil {
		t.Fatal(err)
	}
	admission := canonicalWorkflowAdmission(t, scripted.cfg, store)
	accepted, err := store.AcceptParentCompletion(admission, controller.CandidateAcceptanceInput{Message: "request result", Policy: controller.PublicationPolicy{Remote: "origin", RemoteRef: ref, LocalRef: ref}, RequiredValidations: &controller.RequiredValidationDeclaration{Forms: []controller.FinalValidationRequest{}}})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Head.AcceptedCandidateRef == nil {
		t.Fatal("no canonical accepted request candidate")
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.Head.AcceptedCandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.TaskRef.RequestID == "" || candidate.TaskRef.TaskPath != "" || len(candidate.TerminalFiles) != 0 {
		t.Fatalf("request candidate invented project metadata: %+v", candidate.TaskRef)
	}
	promoted, err := runRequestPublication(t, scripted.cfg, "promote", accepted.Head.ControllerGeneration, candidate.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	published, err := runRequestPublication(t, scripted.cfg, "publish", promoted.Head.ControllerGeneration, candidate.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete || published.Head.ExecutionTaskRef != nil || published.Head.RootTaskRef != nil {
		t.Fatalf("request completion did not release canonical execution: status=%s head=%+v", st.TaskStatus(), published.Head)
	}
	operation, err := store.ProjectParentCompletion()
	if err != nil || operation.Operation == controller.PublicationOperationSuccessorStart {
		t.Fatalf("completed request inferred a project successor: %+v %v", operation, err)
	}
	oldID := candidate.TaskRef.RequestID

	scripted.complete = false
	err = Execute(Command{Mode: ModeNewTask, Payload: scripted.request}, cfg, factory, io.Discard, io.Discard)
	var interrupted *runner.InterruptedCallError
	if !errors.As(err, &interrupted) {
		t.Fatalf("next request could not start: %v", err)
	}
	next, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if next.ExecutionTaskRef == nil || next.ExecutionTaskRef.RequestID == oldID || st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("next request reused completed authority: %+v", next)
	}
	if _, err := os.Stat(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md")); !os.IsNotExist(err) {
		t.Fatalf("Plan was generated: %v", err)
	}
}

func newRequestTaskAppConfig(t *testing.T) config.AppConfig {
	t.Helper()
	cfg := newAppConfig(t)
	cfg.ClaudeConfigDir = t.TempDir()
	runCanonicalGuardGit(t, cfg.RepoRoot, "rm", "quality-tools.yml")
	writeAppTestFile(t, cfg.RepoRoot, "README.md", "ordinary Git repository\n")
	runCanonicalGuardGit(t, cfg.RepoRoot, "add", "README.md")
	runCanonicalGuardGit(t, cfg.RepoRoot, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "README only")
	return cfg
}

func TestNormalTaskRequestRejectsHarnessChangeBeforeResumeDispatch(t *testing.T) {
	cfg := newRequestTaskAppConfig(t)
	scripted := &requestTaskDispatchRunner{t: t, request: "preserve request scope"}
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		scripted.cfg = actual
		return scripted
	}
	err := Execute(Command{Mode: ModeNewTask, Payload: scripted.request}, cfg, factory, io.Discard, io.Discard)
	var interrupted *runner.InterruptedCallError
	if !errors.As(err, &interrupted) {
		t.Fatal(err)
	}
	writeAppTestFile(t, cfg.RepoRoot, ".glm-worker-repository-harness", "new marker\n")
	err = Execute(Command{Mode: ModeResume}, cfg, factory, io.Discard, io.Discard)
	if err == nil || scripted.calls != 1 {
		t.Fatalf("changed harness reached model: calls=%d err=%v", scripted.calls, err)
	}
}

func runRequestPublication(t *testing.T, cfg config.AppConfig, action string, generation uint64, candidate string) (controller.ExecutionOperationResult, error) {
	t.Helper()
	body, err := json.Marshal(controllerPublicationCommand{Action: action, ExpectedGeneration: generation, CandidateID: candidate})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runEntry([]string{"--authority", "controller-publication"}, func() (config.AppConfig, error) { return cfg, nil }, nil, bytes.NewReader(body), &output, io.Discard)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	var result controller.ExecutionOperationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result, nil
}

func TestNormalTaskRequestReadOnlyReviewCannotAlterPlanNamedData(t *testing.T) {
	cfg := newRequestTaskAppConfig(t)
	writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", "ordinary data\n")
	runCanonicalGuardGit(t, cfg.RepoRoot, "add", "IMPLEMENTATION_PLAN.local.md")
	runCanonicalGuardGit(t, cfg.RepoRoot, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "ordinary data")
	scripted := &requestTaskDispatchRunner{t: t, request: "review ordinary data without changing it", complete: true, mutateReview: true}
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		scripted.cfg = actual
		return scripted
	}
	err := Execute(Command{Mode: ModeNewTask, Payload: scripted.request}, cfg, factory, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "read-only") || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("read-only ordinary-data mutation accepted: %v", err)
	}
	store, err := controller.Open(scripted.cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.Status == controller.ControllerStatusActive || head.FailureID == "" {
		t.Fatalf("read-only mutation did not fail closed: %+v", head)
	}
}

func TestNormalTaskRequestPublicationThroughParentActionTerminal(t *testing.T) {
	cfg := newRequestTaskAppConfig(t)
	cfg.StateBase = filepath.Join(t.TempDir(), "sessions")
	binary := typedCompletionWorkerBinary(t)
	workerDir := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(workerDir, "glm-worker")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workerDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	remote := filepath.Join(t.TempDir(), "remote.git")
	runCanonicalGuardGit(t, cfg.RepoRoot, "init", "--bare", remote)
	runCanonicalGuardGit(t, cfg.RepoRoot, "remote", "add", "origin", remote)
	ref := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "symbolic-ref", "HEAD")))
	runCanonicalGuardGit(t, cfg.RepoRoot, "push", "-u", "origin", "HEAD:"+ref)
	hook := filepath.Join(remote, "hooks", "post-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ncat >> request-publication-receipts\n"), 0755); err != nil {
		t.Fatal(err)
	}

	scripted := &requestTaskDispatchRunner{t: t, request: "complete a request and publish it", complete: true}
	factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		scripted.cfg = actual
		return scripted
	}
	if err := Execute(Command{Mode: ModeNewTask, Payload: scripted.request}, cfg, factory, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Execute(Command{Mode: ModeAccept}, cfg, factory, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	store, err := controller.Open(scripted.cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil || before.ExecutionTaskRef == nil || before.ExecutionTaskRef.RequestID == "" {
		t.Fatalf("request was not admitted for publication: head=%+v err=%v", before, err)
	}
	requestID := before.ExecutionTaskRef.RequestID
	handoff := executeParentHandoff(t, scripted.cfg)
	if !handoff.Consistent || stringPointerValue(handoff.RequiredAction) != controllerPublicationSurface {
		t.Fatalf("request handoff did not admit publication: %#v", handoff)
	}

	exit, output, diagnostics := runTypedPublicationStagedCLI(t, scripted.cfg,
		handoff.RequiredActionParameters, typedSemanticSlotValues(t, handoff.RequiredActionParameters, false))
	if exit != 0 {
		t.Fatalf("parent CLI publication failed: %s %s", output, diagnostics)
	}
	var terminal struct {
		Status   string `json:"status"`
		Handoff  struct {
			Consistent     bool     `json:"consistent"`
			RequiredAction *string  `json:"required_action"`
			AllowedActions []string `json:"allowed_actions"`
		} `json:"handoff"`
		Terminal struct {
			Status               string `json:"status"`
			RequestID            string `json:"request_id"`
			SourceOID            string `json:"source_oid"`
			FinalOID             string `json:"final_oid"`
			RemoteOID            string `json:"remote_oid"`
			TransitionID         string `json:"transition_id"`
			ControllerGeneration uint64 `json:"controller_generation"`
			StopBoundary         string `json:"stop_boundary"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(output), &terminal); err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	proof := terminal.Terminal
	if terminal.Status != "parent_action_terminal" || !terminal.Handoff.Consistent ||
		stringPointerValue(terminal.Handoff.RequiredAction) != string(state.ParentActionNone) ||
		containsParentAction(terminal.Handoff.AllowedActions, "start") || proof.Status != "completed" ||
		proof.RequestID != requestID || proof.SourceOID == "" ||
		proof.SourceOID != proof.FinalOID || proof.RemoteOID != proof.FinalOID ||
		proof.FinalOID != head.IntegrationTip || proof.RemoteOID != head.ObservedRemoteTip ||
		head.MetadataLineageRef == nil || proof.TransitionID != head.MetadataLineageRef.LogicalIdentity ||
		proof.ControllerGeneration != head.ControllerGeneration || proof.StopBoundary != "next-task-not-started" {
		t.Fatalf("parent CLI did not hand off matching request publication: %s", output)
	}
	if head.RootTaskRef != nil || head.ExecutionTaskRef != nil || head.LiveLeaseID != "" || head.LiveAttemptID != "" {
		t.Fatalf("published request started another request: %+v", head)
	}
	if state.AttachStateStore(scripted.cfg).TaskStatus() != state.TaskStatusComplete {
		t.Fatal("request Runtime did not acknowledge successful publication")
	}
	receipt, err := store.ProjectParentCompletion()
	if err != nil || receipt.Operation != controller.PublicationOperationNone || receipt.CompletedRequest == nil ||
		receipt.CompletedRequest.RequestID != requestID || receipt.CompletedRequest.TransitionID != proof.TransitionID ||
		receipt.CompletedRequest.ControllerGeneration != proof.ControllerGeneration ||
		receipt.CompletedRequest.FinalOID != proof.FinalOID || receipt.CompletedRequest.RemoteOID != proof.RemoteOID {
		t.Fatalf("controller projected different request terminal authority: %+v err=%v", receipt, err)
	}
	remoteOID := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "ls-remote", "origin", ref)))
	if !strings.HasPrefix(remoteOID, proof.FinalOID+"\t") {
		t.Fatalf("remote differs from terminal OID: %s", remoteOID)
	}
	receipts, err := os.ReadFile(filepath.Join(remote, "request-publication-receipts"))
	if err != nil || len(strings.Split(strings.TrimSpace(string(receipts)), "\n")) != 1 {
		t.Fatalf("request publication was not unique: %q err=%v", receipts, err)
	}
}
