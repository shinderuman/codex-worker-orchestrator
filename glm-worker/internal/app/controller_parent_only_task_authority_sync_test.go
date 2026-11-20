package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func TestParentOnlyTaskAmendmentSyncCompletesWithoutModelRuntime(t *testing.T) {
	cfg, store, policy, tasks, goShim := parentOnlyCompletionFixture(t, false)
	start := executeParentHandoff(t, cfg)
	if !start.Consistent || start.RequiredAction == nil || *start.RequiredAction != controllerExecutionSurface {
		t.Fatalf("parent-only start handoff: %#v", start)
	}
	runParentOnlyExecutionStep(t, cfg, start.RequiredActionParameters, true)
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime := state.AttachStateStore(bound)
	taskID, err := runtime.TaskID()
	if err != nil || taskID != before.LiveAttemptID {
		t.Fatalf("parent-only smoke identity: %q %v", taskID, err)
	}
	if _, err := runtime.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent-only execution unexpectedly attached a model Runtime: %v", err)
	}

	rejectParentOnlyModePromotion(t, cfg, store, tasks.parent, before)

	revisedContent := parentOnlyAppTaskContent(controller.ExecutionAuthorityParentOnly) +
		"\n## Amendments\n\n1. Preserve the parent-only attempt when accepting this amendment.\n"
	writeAppTestFile(t, cfg.RepoRoot, tasks.parent, revisedContent)
	revised := controller.SemanticTaskRef{TaskPath: tasks.parent, ContractDigest: taskAuthoritySyncDigest(t, []byte(revisedContent))}
	rejectUnrelatedParentOnlySource(t, cfg, store)
	rejectForgedParentOnlySync(t, cfg, store, before, revised)
	sync := executeParentHandoff(t, cfg)
	if !sync.Consistent || sync.RequiredAction == nil || *sync.RequiredAction != controllerExecutionSurface {
		reason := ""
		if sync.Inconsistency != nil {
			reason = *sync.Inconsistency
		}
		t.Fatalf("parent-only Amendment did not produce an executable handoff: reason=%q, handoff=%#v", reason, sync)
	}
	payload := sync.RequiredActionParameters["controller_command_json"]
	var command workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != "sync-task-authority" || command.ExpectedGeneration != before.ControllerGeneration ||
		command.RevisedTask == nil || !command.RevisedTask.Equal(revised) {
		t.Fatalf("parent-only Amendment command: %#v", command)
	}

	stale := command
	stale.ExpectedGeneration++
	if _, err := workflow.ExecuteControllerExecution(cfg, store, stale); err == nil {
		t.Fatal("stale parent-only Amendment generation was accepted")
	}
	foreign := command
	foreign.RevisedTask = &controller.SemanticTaskRef{TaskPath: tasks.successor, ContractDigest: revised.ContractDigest}
	if _, err := workflow.ExecuteControllerExecution(cfg, store, foreign); err == nil {
		t.Fatal("foreign parent-only Amendment Task was accepted")
	}
	executeStagedControllerAction(t, cfg, controllerExecutionSurface, payload, true)

	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.LiveAttemptID != before.LiveAttemptID || after.LiveLeaseID == before.LiveLeaseID ||
		after.RootTaskRef == nil || !after.RootTaskRef.Equal(revised) ||
		after.ExecutionTaskRef == nil || !after.ExecutionTaskRef.Equal(revised) {
		t.Fatalf("parent-only Amendment did not retain same-attempt revised Task authority: %#v", after)
	}
	assertParentOnlyRevisedSmoke(t, cfg, runtime, taskID)
	next := executeParentHandoff(t, cfg)
	if !next.Consistent || next.RequiredAction == nil || *next.RequiredAction != controllerPublicationSurface {
		t.Fatalf("parent-only Amendment did not proceed to completion: %#v", next)
	}
	driveParentOnlyCompletion(t, cfg, goShim, false, false, true)
	assertParentOnlyTerminalState(t, cfg, store, policy, tasks, false)
}

func rejectParentOnlyModePromotion(t *testing.T, cfg config.AppConfig, store *controller.Store, taskPath string, before controller.RepositoryControllerHead) {
	t.Helper()
	glmContent := parentOnlyAppTaskContent(controller.ExecutionAuthorityGLM)
	writeAppTestFile(t, cfg.RepoRoot, taskPath, glmContent)
	glmSnapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	glmTask := controller.SemanticTaskRef{TaskPath: taskPath, ContractDigest: taskAuthoritySyncDigest(t, []byte(glmContent))}
	if _, err := workflow.ExecuteControllerExecution(cfg, store, workflow.ControllerExecutionCommand{
		Action: "sync-task-authority", ExpectedGeneration: before.ControllerGeneration,
		SourceSnapshotID: glmSnapshot.ID, RevisedTask: &glmTask,
	}); err == nil || !strings.Contains(err.Error(), "designation") {
		t.Fatalf("parent-only Amendment illegally switched to GLM authority: %v", err)
	}
	unchanged, err := store.LoadHead()
	if err != nil || unchanged.ControllerGeneration != before.ControllerGeneration || unchanged.PendingTransitionID != "" {
		t.Fatalf("rejected GLM promotion changed controller authority: %#v %v", unchanged, err)
	}

}

func rejectUnrelatedParentOnlySource(t *testing.T, cfg config.AppConfig, store *controller.Store) {
	t.Helper()
	writeAppTestFile(t, cfg.RepoRoot, "unrelated-change.txt", "must not join Task-only sync\n")
	if _, err := store.ParentOnlyTaskAuthoritySyncAdmission(cfg); err == nil {
		t.Fatal("parent-only Task sync admitted unrelated primary source edits")
	}
	if err := os.Remove(filepath.Join(cfg.RepoRoot, "unrelated-change.txt")); err != nil {
		t.Fatal(err)
	}
}

func rejectForgedParentOnlySync(t *testing.T, cfg config.AppConfig, store *controller.Store, before controller.RepositoryControllerHead, revised controller.SemanticTaskRef) {
	t.Helper()
	source, err := store.ParentOnlyTaskAuthoritySyncAdmission(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tampered := source
	tampered.Attempt.BaselineTrees.WorktreeTree = "forged-baseline"
	if _, err := store.SyncTaskAuthority(tampered, source.Snapshot.ID, controller.EvidenceObjectRef{}, revised); err == nil || !strings.Contains(err.Error(), "source authority changed") {
		t.Fatalf("forged parent-only admission baseline was accepted: %v", err)
	}
	proof, err := store.PutEvidenceObject("runtime-artifact", "application/json",
		before.LiveAttemptID+":"+controller.TaskAuthoritySyncProducer+":"+source.Snapshot.ID,
		true, []byte(`{"runtime":"parent-only","task_id":"forged-attempt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SyncTaskAuthority(source, source.Snapshot.ID, proof, revised); err == nil || !strings.Contains(err.Error(), "smoke identity") {
		t.Fatalf("forged parent-only smoke evidence was accepted: %v", err)
	}
	unchanged, err := store.LoadHead()
	if err != nil || unchanged.ControllerGeneration != before.ControllerGeneration || unchanged.PendingTransitionID != "" {
		t.Fatalf("forged smoke proof prepared a transition: %#v %v", unchanged, err)
	}
}

func assertParentOnlyRevisedSmoke(t *testing.T, cfg config.AppConfig, runtime *state.StateStore, taskID string) {
	t.Helper()
	if _, err := runtime.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent-only Amendment manufactured a Runtime: %v", err)
	}
	if actual, err := runtime.TaskID(); err != nil || actual != taskID {
		t.Fatalf("parent-only Amendment lost old smoke attempt identity: %q %v", actual, err)
	}
	revisedConfig, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	revisedRuntime := state.AttachStateStore(revisedConfig)
	if current, err := revisedRuntime.TaskID(); err != nil || current != taskID {
		t.Fatalf("amended Task state lacks the same smoke attempt identity: %q %v", current, err)
	}
	if _, err := revisedRuntime.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("amended Task state created a GLM Runtime binding: %v", err)
	}
}

func TestParentOnlyTaskAuthoritySyncEvidenceRejectsRuntimeSpoofing(t *testing.T) {
	parent := controller.Admission{Attempt: controller.AttemptRecord{AttemptID: "parent-attempt", ExecutionAuthority: controller.ExecutionAuthorityParentOnly}}
	ordinary := controller.Admission{Attempt: controller.AttemptRecord{AttemptID: "worker-attempt"}}
	for _, scenario := range []struct {
		name      string
		admission controller.Admission
		source    workflow.TaskAuthoritySyncEvidence
	}{
		{"missing-smoke-id", parent, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeParentOnly}},
		{"wrong-smoke-id", parent, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeParentOnly, TaskID: "wrong-attempt"}},
		{"unstarted-parent-only", parent, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeUnstarted}},
		{"ordinary-runtime-parent-only", parent, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeAttached}},
		{"spoofed-parent-only-on-GLM", ordinary, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeParentOnly, TaskID: "worker-attempt"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			raw, err := json.Marshal(scenario.source)
			if err != nil {
				t.Fatal(err)
			}
			if err := workflow.VerifyTaskAuthoritySyncRuntime(raw, scenario.admission); err == nil {
				t.Fatal("incorrect Runtime evidence accepted")
			}
		})
	}
}
