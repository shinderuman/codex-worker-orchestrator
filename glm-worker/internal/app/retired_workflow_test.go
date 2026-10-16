package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactioncmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func prepareCompletionQualityCorpus(t *testing.T, root string) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("quality fixture source is unavailable")
	}
	if os.Getenv("QUALITY_TOOLS_BIN_DIR") == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("QUALITY_TOOLS_BIN_DIR", filepath.Join(home, ".local", "share", "codex-worker-orchestrator", "quality-tools", "bin"))
	}
	moduleCache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", strings.TrimSpace(string(moduleCache)))
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	paths, err := exec.Command("git", "-C", repository, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range bytes.Split(paths, []byte{0}) {
		path := string(name)
		if path == "" || (strings.HasPrefix(path, "IMPLEMENTATION_") && path != "IMPLEMENTATION_RULES.md") {
			continue
		}
		target := filepath.Join(root, path)
		if _, err := os.Lstat(target); err == nil {
			if !strings.HasPrefix(path, "glm-worker/") && path != "IMPLEMENTATION_RULES.md" {
				continue
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(repository, path))
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(repository, path))
		if err != nil {
			t.Fatal(err)
		}
		writeAppTestFile(t, root, path, string(data))
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	if err := harnesslint.PreflightQualityTools(root); err != nil {
		t.Fatal(err)
	}
	tasks, err := filepath.Glob(filepath.Join(root, "IMPLEMENTATION_TASKS", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		data, err := os.ReadFile(task)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("## External feasibility")) {
			data = append(data, []byte("\n## External feasibility\n\nstatus: not-applicable\n")...)
		}
		if err := os.WriteFile(task, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompletionLifecycleRecoveryAndSuccessorAdmission(t *testing.T) {
	cfg, store, retired, policy := interruptedCompletionWorkflowFixture(t)
	t.Setenv("GLM_WORKER_HOME", filepath.Dir(cfg.StateBase))
	assertRetiredBootstrap(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md")
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	st := state.AttachStateStore(bound)
	handoff := buildParentHandoffWithConfig(bound, st)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "controller-execution" || containsParentAction(handoff.AllowedActions, "start") {
		t.Fatalf("local retirement did not retain exclusive publication authority: %#v", handoff)
	}
	if handoff.ParentRequest != nil && handoff.ParentRequest.CompletionAdmitted {
		t.Fatal("unpublished Task was reported complete")
	}
	var command controllerExecutionCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != "recover" || command.ExpectedGeneration != retired.Head.ControllerGeneration || command.TransitionID != retired.TransitionID {
		t.Fatal("publication action lost retired Task/candidate identity")
	}
	published, err := store.RecoverExecutionOperation(command.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	handoff = buildParentHandoffWithConfig(bound, st)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "start" {
		t.Fatalf("published successor is not normally startable: %#v", handoff)
	}
	if st.ReadOr("repo-root", "") != "" {
		t.Fatal("successor fixture already initialized runtime")
	}
	content, err := os.ReadFile(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md", string(content)+"\n親metadataの追加要求\n")
	runControllerActivationGit(t, cfg.RepoRoot, "add", "IMPLEMENTATION_TASKS/successor.md")
	assertRetiredParentMaintenanceGateAdmits(t, cfg.RepoRoot)
	runControllerActivationGit(t, cfg.RepoRoot, "commit", "-qm", "successor amendment")
	assertRetiredBootstrap(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md")
	handoff = buildParentHandoffWithConfig(bound, st)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "controller-publication" {
		t.Fatalf("metadata synchronization handoff: %#v", handoff)
	}
	if containsParentAction(handoff.AllowedActions, "start") || containsParentAction(handoff.AllowedActions, "start-milestones") {
		t.Fatal("start bypassed pending project synchronization")
	}
	adopted, err := store.AdoptExternalAdvancement(controller.ExternalAdvancementInput{ExpectedGeneration: published.Head.ControllerGeneration, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	handoff = buildParentHandoffWithConfig(bound, st)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "start" || adopted.Head.LiveLeaseID != "" {
		t.Fatalf("synchronized successor handoff: %#v", handoff)
	}
	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: published.Head.ControllerGeneration, CandidateID: "stale"}); err == nil {
		t.Fatal("retired controller accepted a stale terminal operation")
	}
	for name, value := range map[string]string{"task.id": "previous-task", "repo-root": cfg.RepoRoot, "task.status": string(state.TaskStatusUserTerminated)} {
		if err := st.Write(name, value); err != nil {
			t.Fatal(err)
		}
	}
	record := state.ControllerTaskTermination{Version: 1, TransitionID: "previous-user-termination", SourceGeneration: 1, TaskPath: published.Head.RootTaskRef.TaskPath, TaskContractDigest: published.Head.RootTaskRef.ContractDigest, AttemptID: "previous-attempt", Reason: "user terminated incomplete execution", PreviousRepoRoot: cfg.RepoRoot, PrimaryRoot: cfg.RepoRoot, PreviousStatus: state.TaskStatusAwaitingParentCompletion}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("controller-task-termination.json", string(data)); err != nil {
		t.Fatal(err)
	}
	handoff = buildParentHandoffWithConfig(bound, st)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "start" {
		t.Fatalf("terminated predecessor prevented fresh Task start: %#v", handoff)
	}
	if _, admitted, err := st.AdmitParentAction(state.ParentActionResume); err != nil || admitted {
		t.Fatalf("terminated execution was admitted for resume: %v %v", admitted, err)
	}
	if _, admitted, err := st.AdmitNewTask(); err != nil || !admitted {
		t.Fatalf("fresh Task start was not admitted: %v", err)
	}
	if st.ReadOr("task.id", "") != "previous-task" || st.TaskStatus() != state.TaskStatusUserTerminated {
		t.Fatal("handoff inspection changed terminated runtime")
	}
}

func assertRetiredParentMaintenanceGateAdmits(t *testing.T, root string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	code := parentactioncmd.Run([]string{"continuation-metadata-guard"}, io.Discard, io.Discard)
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("continuation metadata guard rejected post-retire parent maintenance: exit=%d", code)
	}
}

func assertRetiredBootstrap(t *testing.T, root, task string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, runErr := runAuthorityBootstrap([]string{"--authority", "bootstrap"}, &out)
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	var result struct {
		ActiveTask string `json:"active_task"`
		Hash       string `json:"authority_snapshot_sha256"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ActiveTask != task || result.Hash == "" {
		t.Fatalf("bootstrap authority: %#v", result)
	}
}

func promotedWorkflowFixture(t *testing.T) (config.AppConfig, *controller.Store, controller.ExecutionOperationResult, controller.PublicationPolicy, controller.AcceptedCandidate) {
	t.Helper()
	cfg, admission, store, policy := retiredWorkflowCorpus(t, true)
	evidence := canonicalWorkflowEvidence(t, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{Message: "root result", Policy: policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	return cfg, store, promoted, policy, candidate
}

func interruptedCompletionWorkflowFixture(t *testing.T) (config.AppConfig, *controller.Store, controller.ExecutionOperationResult, controller.PublicationPolicy) {
	t.Helper()
	cfg, store, promoted, policy, candidate := promotedWorkflowFixture(t)
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runControllerActivationGit(t, cfg.RepoRoot, "config", "core.hooksPath", hooks)
	_, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	runControllerActivationGit(t, cfg.RepoRoot, "config", "--unset", "core.hooksPath")
	if err == nil {
		t.Fatal("fault did not interrupt completion publication")
	}
	interruption := err
	head, err := store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("completion journal missing: %v; interruption=%v", err, interruption)
	}
	return cfg, store, controller.ExecutionOperationResult{Head: head, TransitionID: head.PendingTransitionID}, policy
}
