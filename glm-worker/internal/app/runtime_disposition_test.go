package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestActiveCompletionPreservesAndDisposesUnfinishedSuccessor(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, store, promoted, policy, candidate := promotedWorkflowFixture(t)
	authority, err := controller.ResolveCommittedTaskAuthorityAt(cfg.RepoRoot, candidate.CommitOID)
	if err != nil {
		t.Fatal(err)
	}
	runtimeCfg := controller.WorkflowTaskConfig(cfg, store.Identity(), authority.Task)
	old, err := state.NewStateStore(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	id, err := old.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := old.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if err := old.Write("last-request", "unfinished older successor work"); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(old.ArtifactDir(id), "unpublished.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("preserved evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeRemote := string(mustCommandOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef))
	if _, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("unfinished successor bypassed publication readiness")
	}
	if old.TaskStatus() != state.TaskStatusWaitingDecision || old.ReadOr("task.id", "") != id || string(mustCommandOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef)) != beforeRemote {
		t.Fatal("blocked publication mutated successor or remote")
	}
	handoff := executeParentHandoff(t, cfg)
	var command controllerExecutionCommand
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface || json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command) != nil || command.Action != "dispose-runtime" || command.RuntimeDisposition == nil || command.RuntimeDisposition.RuntimeTaskID != id {
		t.Fatalf("missing exact unfinished Runtime disposition: %#v", handoff)
	}
	if handoff.RequiredActionParameters["semantic_slot"] != "runtime_disposition.reason" || command.RuntimeDisposition.Reason != "" || containsParentAction(handoff.AllowedActions, "start") {
		t.Fatal("Runtime termination lacks explicit approval boundary")
	}
	if _, err := executeControllerExecution(cfg, store, command); err == nil {
		t.Fatal("termination without reason was admitted")
	}
	runTypedRuntimeDispositionCLI(t, cfg, "明示承認: 旧successor実行を未完了のまま終了し証拠を保全する")
	if old.TaskStatus() != state.TaskStatusUserTerminated || old.ReadOr("task.id", "") != id || !old.Exists("pending-decision") || old.ReadOr("last-request", "") != "unfinished older successor work" {
		t.Fatal("Runtime disposition lost unfinished evidence or fabricated success")
	}
	if data, err := os.ReadFile(artifact); err != nil || string(data) != "preserved evidence" {
		t.Fatal("Runtime disposition lost artifact")
	}
	if _, admitted, _ := old.AdmitParentAction(state.ParentActionResume); admitted {
		t.Fatal("terminated Runtime resumed")
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	head, err = store.LoadHead()
	if err != nil || head.ExecutionTaskRef != nil || head.LiveLeaseID != "" || head.LiveAttemptID != "" || head.RootTaskRef.TaskPath != authority.Task.TaskPath {
		t.Fatalf("completion authority: %#v %v", head, err)
	}
	if old.ReadOr("task.id", "") != id || old.TaskStatus() != state.TaskStatusUserTerminated || old.Exists("worker.id") {
		t.Fatal("completion started successor")
	}
	if _, err := os.Stat(filepath.Join(cfg.RepoRoot, candidate.TaskRef.TaskPath)); !os.IsNotExist(err) {
		t.Fatal("completed Task metadata remains")
	}
	commits := mustCommandOutput(t, cfg.RepoRoot, "rev-list", "--count", candidate.BaseOID+".."+candidate.CommitOID)
	if string(commits) != "1\n" {
		t.Fatalf("completion created extra commit: %s", commits)
	}
	assertRetiredBootstrap(t, cfg, authority.Task.TaskPath)
}
