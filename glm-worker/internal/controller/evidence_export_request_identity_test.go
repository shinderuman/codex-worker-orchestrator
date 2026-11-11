package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExportEvidenceRequestRuntimeUsesCanonicalTaskIdentity(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	cfg := controllerTestConfig(repo, filepath.Join(t.TempDir(), "sessions"))
	admission, err := ActivateRequest(cfg, "export a request-mode task bundle")
	if err != nil {
		t.Fatal(err)
	}
	task := admission.Attempt.SemanticTaskRef
	if task.RequestID == "" || task.TaskPath != "" {
		t.Fatalf("request authority did not create a request task: %#v", task)
	}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtimeCfg := WorkflowTaskConfig(cfg, store.Identity(), task)
	if runtimeCfg.RepoHash == digestStrings(store.Identity().LineageID, task.TaskPath) {
		t.Fatal("request runtime accidentally shares the task-path-only location")
	}
	runtime, err := state.NewStateStore(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	const taskID = "runtime-request-export-1"
	if err := runtime.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	binding := state.ControllerRuntimeBinding{
		AttemptID:          admission.Attempt.AttemptID,
		RequestID:          task.RequestID,
		TaskContractDigest: task.ContractDigest,
	}
	if err := runtime.SaveControllerRuntimeBinding(binding); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		request EvidenceExportRequest
		kind    string
	}{
		{name: "default live request", request: EvidenceExportRequest{}, kind: evidenceExportTargetRunning},
		{name: "explicit runtime task", request: EvidenceExportRequest{TaskID: taskID}, kind: evidenceExportTargetTaskID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			export, result, err := store.ExportEvidence(tc.request)
			if err != nil {
				t.Fatalf("canonical request runtime export failed: %v", err)
			}
			if result.Target.Kind != tc.kind || result.Target.Task.RequestID != task.RequestID ||
				result.Target.AttemptID != admission.Attempt.AttemptID ||
				result.Target.RuntimeTaskID != taskID || !result.Target.Live {
				t.Fatalf("export used the wrong task or runtime: %#v", result.Target)
			}
			if result.RuntimeStatus != evidenceExportRuntimeCollected ||
				result.ManifestDigest != digestBytes(exportFileByPath(t, export, "manifest.json")) {
				t.Fatalf("export bundle status or manifest digest is invalid: %#v", result)
			}
			if !exportHasEntry(export.Manifest, "live/state/controller-runtime-binding.json") {
				t.Fatal("request bundle lost the source-bound runtime binding")
			}
		})
	}
	t.Run("foreign request authority fails closed", func(t *testing.T) {
		foreign := binding
		foreign.RequestID = "01a0463c-d477-4410-9efd-cb34ff2e0b0e"
		if foreign.RequestID == task.RequestID {
			foreign.RequestID = "01a0244a-4ee4-4e71-b2e1-dec3bdda2120"
		}
		if err := runtime.SaveControllerRuntimeBinding(foreign); err != nil {
			t.Fatal(err)
		}
		for _, request := range []EvidenceExportRequest{{}, {TaskID: taskID}} {
			if _, _, err := store.ExportEvidence(request); err == nil ||
				!strings.Contains(err.Error(), "not the target attempt") {
				t.Fatalf("foreign request binding was accepted or misreported: %v", err)
			}
		}
	})
}

func TestExportEvidenceExplicitSealedTaskRejectsUnboundLiveRuntimeOfSameTask(t *testing.T) {
	t.Parallel()
	f := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
	})
	if err := f.runtime.Remove("controller-runtime-binding.json"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask}); err == nil {
		t.Fatal("unusable live Runtime must not fall back to a seal of the same task")
	}
}

func TestExportEvidenceExplicitSealedTaskSurvivesUnrelatedUnboundLiveRuntime(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	store := fixture.store
	source := publicationTestEdit(t, fixture, "published.txt", "sealed task implementation\n")
	completedRuntime := acceptedCompletionRuntime(t, store, source)
	taskID, err := completedRuntime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	promoted, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	published, err := store.PublishAcceptedCandidate(PublicationInput{
		ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID,
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: published.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if next.Admission == nil || next.Admission.Attempt.SemanticTaskRef.Equal(source.Attempt.SemanticTaskRef) {
		t.Fatal("publication did not hand off to a distinct live Task")
	}
	admission := *next.Admission
	runtime := state.AttachStateStore(WorkflowTaskConfig(store.config, store.identity, admission.Attempt.SemanticTaskRef))
	if err := os.MkdirAll(runtime.Path("."), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("default export selected a different task with an unbound live Runtime")
	}
	if _, _, err := store.ExportEvidence(EvidenceExportRequest{TaskID: "unbound-task"}); err == nil {
		t.Fatal("export accepted an unknown Task ID")
	}
	request := EvidenceExportRequest{TaskID: taskID}
	_, result, err := store.ExportEvidence(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.AttemptID != source.Attempt.AttemptID || result.Target.Live || !result.Target.Task.Equal(source.Attempt.SemanticTaskRef) {
		t.Fatalf("explicit historical export lost sealed authority: %#v", result.Target)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ExecutionLease)
		want   string
	}{
		{name: "foreign-attempt", mutate: func(lease *ExecutionLease) { lease.AttemptID = source.Attempt.AttemptID }, want: "live attempt/lease identity"},
		{name: "foreign-task", mutate: func(lease *ExecutionLease) { lease.SemanticTaskRef = source.Attempt.SemanticTaskRef }, want: "live attempt/lease semantic task"},
		{name: "stale-generation", mutate: func(lease *ExecutionLease) { lease.ControllerGeneration-- }, want: "execution lease generation is stale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lease := admission.Lease
			tc.mutate(&lease)
			if err := store.writeLease(lease); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := store.writeLease(admission.Lease); err != nil {
					t.Error(err)
				}
			})
			if _, _, err := store.ExportEvidence(request); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("explicit historical export accepted inconsistent lease: %v", err)
			}
		})
	}
}
