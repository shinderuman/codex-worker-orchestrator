package controller

import (
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
